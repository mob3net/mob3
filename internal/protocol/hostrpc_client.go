package protocol

import (
	"context"
	"errors"
	"io"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// ErrHostRPCClosed reports a host RPC channel that has stopped.
var ErrHostRPCClosed = errors.New("protocol: host RPC closed")

// HostClient is the front-end side of a host RPC channel. Calls may run
// concurrently; responses are matched by request ID.
type HostClient struct {
	peer     *MuxPeer
	ownsPeer bool
	lim      Limits

	next   atomic.Uint64
	mu     sync.Mutex
	pend   map[uint64]chan HostResult
	closed atomic.Bool
}

// NewHostClient starts a host RPC client over a dedicated rw channel.
func NewHostClient(rw io.ReadWriteCloser, lim Limits) *HostClient {
	peer := NewMuxPeerReadWriter(rw, lim)
	c := NewHostClientPeer(peer)
	c.ownsPeer = true
	go func() { _ = peer.Run(context.Background()) }()
	return c
}

// NewHostClientPeer mounts a host client on an existing peer. The caller runs
// the peer, which lets page and host traffic share one stream.
func NewHostClientPeer(peer *MuxPeer) *HostClient {
	c := &HostClient{
		peer: peer,
		lim:  peer.lim,
		pend: make(map[uint64]chan HostResult),
	}
	_ = peer.Handle(MuxHostResult, c.handleFrame)
	return c
}

// Call sends one host request and waits for its response or ctx cancellation.
func (c *HostClient) Call(ctx context.Context, call HostCall) (HostResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.closed.Load() {
		return HostResult{}, ErrHostRPCClosed
	}
	id := c.next.Add(1)
	call.ID = id
	ch := make(chan HostResult, 1)
	c.mu.Lock()
	c.pend[id] = ch
	c.mu.Unlock()
	if err := c.peer.WriteHostCall(call); err != nil {
		c.remove(id)
		return HostResult{}, err
	}
	select {
	case result := <-ch:
		return result, result.Error()
	case <-ctx.Done():
		c.remove(id)
		c.cancel(id)
		return HostResult{}, ctx.Err()
	case <-c.peer.Done():
		c.remove(id)
		return HostResult{}, c.clientError()
	}
}

// Close stops a dedicated client. A client mounted on a shared peer only stops
// accepting new calls; the session owner closes the peer.
func (c *HostClient) Close() error {
	c.closed.Store(true)
	if c.ownsPeer {
		_ = c.peer.Close()
		<-c.peer.Done()
	}
	return nil
}

func (c *HostClient) remove(id uint64) {
	c.mu.Lock()
	delete(c.pend, id)
	c.mu.Unlock()
}

func (c *HostClient) cancel(id uint64) {
	_ = c.peer.WriteHostCall(HostCall{Op: HostOpCancel, ID: id})
}

func (c *HostClient) handleFrame(f MuxFrame) error {
	result, err := DecodeHostResult(f.Body, c.lim)
	if err != nil {
		return err
	}
	result.ID = f.ID
	c.mu.Lock()
	ch := c.pend[f.ID]
	delete(c.pend, f.ID)
	c.mu.Unlock()
	if ch != nil {
		select {
		case ch <- result:
		default:
		}
	}
	return nil
}

func (c *HostClient) clientError() error {
	if err := c.peer.Err(); err != nil {
		return err
	}
	return ErrHostRPCClosed
}

// RemoteHost implements Host by forwarding calls to a HostClient. The optional
// ImageCache interface is intentionally not implemented: a process transport
// keeps decoded images on the engine side.
type RemoteHost struct {
	client *HostClient
}

var _ Host = (*RemoteHost)(nil)

// NewRemoteHost wraps a host client as a Host.
func NewRemoteHost(client *HostClient) *RemoteHost {
	return &RemoteHost{client: client}
}

func (h *RemoteHost) call(ctx context.Context, call HostCall) (HostResult, error) {
	if h == nil || h.client == nil {
		return HostResult{}, ErrHostRPCClosed
	}
	return h.client.Call(ctx, call)
}

func (h *RemoteHost) Fetch(ctx context.Context, method string, u *url.URL, body []byte, contentType string) (HostFetch, error) {
	result, err := h.call(ctx, HostCall{
		Op:          HostOpFetch,
		Meta:        RequestMetaFrom(ctx),
		URL:         urlString(u),
		Method:      method,
		Body:        body,
		ContentType: contentType,
	})
	return result.Fetch, err
}

func (h *RemoteHost) FetchCtx(ctx context.Context, u *url.URL) (HostFetch, error) {
	return h.Fetch(ctx, MethodGet, u, nil, "")
}

func (h *RemoteHost) JSFetch(ctx context.Context, req FetchRequest) (FetchResponse, error) {
	result, err := h.call(ctx, HostCall{Op: HostOpJSFetch, JS: req})
	return result.JS, err
}

func (h *RemoteHost) RecordBlocked(gen int, method string, u *url.URL, kind string, err error) {
	_, _ = h.call(context.Background(), HostCall{
		Op: HostOpRecordBlocked, Gen: gen, Method: method, URL: urlString(u),
		Kind: kind, ErrText: errorText(err), ErrKind: hostErrorKind(err),
	})
}

func (h *RemoteHost) ApplyMixedContent(ctx context.Context, u *url.URL) (*url.URL, error) {
	result, err := h.call(ctx, HostCall{
		Op: HostOpApplyMixedContent, Meta: RequestMetaFrom(ctx), URL: urlString(u),
	})
	if err != nil {
		return nil, err
	}
	return parseURLString(result.URL), nil
}

func (h *RemoteHost) IsLocalURL(u *url.URL) bool {
	result, err := h.call(context.Background(), HostCall{Op: HostOpIsLocalURL, URL: urlString(u)})
	return err == nil && result.Bool
}

func (h *RemoteHost) IsMixedContentError(err error) bool {
	return errors.Is(err, ErrMixedContentBlocked)
}

func (h *RemoteHost) IsLocalNetworkError(err error) bool {
	return errors.Is(err, ErrLocalNetworkBlocked)
}

func (h *RemoteHost) SetUserAgent(v string) {
	_, _ = h.call(context.Background(), HostCall{Op: HostOpSetUserAgent, UserAgent: v})
}

func (h *RemoteHost) UserAgent() string {
	result, err := h.call(context.Background(), HostCall{Op: HostOpUserAgent})
	if err != nil {
		return ""
	}
	return result.Text
}

func (h *RemoteHost) SetSearchURL(v string) {
	_, _ = h.call(context.Background(), HostCall{Op: HostOpSetSearchURL, SearchURL: v})
}

func (h *RemoteHost) SearchURL() string {
	result, err := h.call(context.Background(), HostCall{Op: HostOpSearchURL})
	if err != nil {
		return ""
	}
	return result.Text
}

func (h *RemoteHost) SetImageCaps(w, hgt int) {
	_, _ = h.call(context.Background(), HostCall{Op: HostOpSetImageCaps, Width: w, Height: hgt})
}

func (h *RemoteHost) ImageCaps() (int, int) {
	result, err := h.call(context.Background(), HostCall{Op: HostOpImageCaps})
	if err != nil {
		return 0, 0
	}
	return result.Width, result.Height
}

func (h *RemoteHost) BeginPage() int {
	result, err := h.call(context.Background(), HostCall{Op: HostOpBeginPage})
	if err != nil {
		return 0
	}
	return result.Int
}

func (h *RemoteHost) CurrentPageGeneration() int {
	result, err := h.call(context.Background(), HostCall{Op: HostOpCurrentPageGeneration})
	if err != nil {
		return 0
	}
	return result.Int
}

func (h *RemoteHost) StartRequest(gen int, method, rawURL, kind string) (int, bool) {
	result, err := h.call(context.Background(), HostCall{
		Op: HostOpStartRequest, Gen: gen, Method: method, URL: rawURL, Kind: kind,
	})
	if err != nil {
		return 0, false
	}
	return result.Int, result.Bool
}

func (h *RemoteHost) RecordCacheHit(gen int, method, rawURL, kind string, size int) {
	_, _ = h.call(context.Background(), HostCall{
		Op: HostOpRecordCacheHit, Gen: gen, Method: method, URL: rawURL, Kind: kind, Size: size,
	})
}

func (h *RemoteHost) FinishRequest(id, gen, status, size int, elapsed time.Duration, err error) {
	_, _ = h.call(context.Background(), HostCall{
		Op: HostOpFinishRequest, RequestID: id, Gen: gen, Status: status, Size: size,
		Elapsed: elapsed, ErrText: errorText(err), ErrKind: hostErrorKind(err),
	})
}

func (h *RemoteHost) RequestSnapshot() []RequestRecord {
	result, err := h.call(context.Background(), HostCall{Op: HostOpRequestSnapshot})
	if err != nil {
		return nil
	}
	return result.Records
}

func (h *RemoteHost) RequestCount() int {
	result, err := h.call(context.Background(), HostCall{Op: HostOpRequestCount})
	if err != nil {
		return 0
	}
	return result.Int
}

func (h *RemoteHost) CacheStats() (used, limit int64, entries int) {
	result, err := h.call(context.Background(), HostCall{Op: HostOpCacheStats})
	if err != nil {
		return 0, 0, 0
	}
	return result.Used, result.Limit, result.Entries
}

func (h *RemoteHost) CacheLimitBytes() int64 {
	result, err := h.call(context.Background(), HostCall{Op: HostOpCacheLimitBytes})
	if err != nil {
		return 0
	}
	return result.Limit
}

func (h *RemoteHost) SetCacheLimitBytes(n int64) {
	_, _ = h.call(context.Background(), HostCall{Op: HostOpSetCacheLimitBytes, Limit: n})
}

func (h *RemoteHost) DocumentCookieValues(u *url.URL) []CookieView {
	result, err := h.call(context.Background(), HostCall{Op: HostOpDocumentCookieValues, URL: urlString(u)})
	if err != nil {
		return nil
	}
	return result.Cookies
}

func (h *RemoteHost) SetDocumentCookieRaw(u *url.URL, raw string) bool {
	result, err := h.call(context.Background(), HostCall{
		Op: HostOpSetDocumentCookieRaw, URL: urlString(u), CookieRaw: raw,
	})
	return err == nil && result.Bool
}

func (h *RemoteHost) CookieViews(u *url.URL) []CookieView {
	result, err := h.call(context.Background(), HostCall{Op: HostOpCookieViews, URL: urlString(u)})
	if err != nil {
		return nil
	}
	return result.Cookies
}

func (h *RemoteHost) SetCertSource(source string) error {
	_, err := h.call(context.Background(), HostCall{Op: HostOpSetCertSource, Source: source})
	return err
}

func (h *RemoteHost) CertSource() string {
	result, err := h.call(context.Background(), HostCall{Op: HostOpCertSource})
	if err != nil {
		return ""
	}
	return result.Text
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
