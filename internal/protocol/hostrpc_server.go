package protocol

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sync"
)

// HostServer serves Host calls from a host RPC client. Calls run concurrently so
// one slow fetch cannot block settings, cookies or another fetch.
type HostServer struct {
	host     Host
	peer     *MuxPeer
	ownsPeer bool
	lim      Limits

	mu      sync.Mutex
	pending map[uint64]context.CancelFunc
	wg      sync.WaitGroup
}

// NewHostServer creates a host RPC server over a dedicated rw channel.
func NewHostServer(rw io.ReadWriteCloser, host Host, lim Limits) *HostServer {
	peer := NewMuxPeerReadWriter(rw, lim)
	s := NewHostServerPeer(peer, host)
	s.ownsPeer = true
	return s
}

// NewHostServerPeer mounts a host server on an existing peer. The caller runs
// the peer, so page and host traffic share one stream.
func NewHostServerPeer(peer *MuxPeer, host Host) *HostServer {
	s := &HostServer{
		host:    host,
		peer:    peer,
		lim:     peer.lim,
		pending: make(map[uint64]context.CancelFunc),
	}
	_ = peer.Handle(MuxHostCall, s.handleFrame)
	go func() {
		<-peer.Done()
		s.cancelAll()
	}()
	return s
}

// Serve runs a dedicated host channel until it closes or ctx is cancelled.
// In-flight calls are cancelled before it returns. A server mounted on a shared
// peer is driven by whatever runs that peer instead.
func (s *HostServer) Serve(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	err := s.peer.Run(ctx)
	s.cancelAll()
	s.wg.Wait()
	_ = s.peer.Close()
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || ctx.Err() != nil {
		return nil
	}
	return err
}

func (s *HostServer) handleFrame(f MuxFrame) error {
	call, err := DecodeHostCall(f.Body, s.lim)
	if err != nil {
		return err
	}
	call.ID = f.ID
	if call.Op == HostOpCancel {
		s.cancel(call.ID)
		return nil
	}
	callCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.pending[call.ID] = cancel
	s.mu.Unlock()
	s.wg.Add(1)
	go s.handle(callCtx, call, cancel)
	return nil
}

func (s *HostServer) handle(ctx context.Context, call HostCall, cancel context.CancelFunc) {
	defer s.wg.Done()
	defer cancel()
	defer s.remove(call.ID)
	result := HostResult{ID: call.ID}
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				result = HostResult{
					ID: call.ID, ErrKind: HostErrOther,
					ErrText: fmt.Sprintf("host call panic: %v", recovered),
				}
			}
		}()
		result = s.dispatch(ctx, call)
		result.ID = call.ID
	}()
	s.send(result)
}

func (s *HostServer) dispatch(ctx context.Context, call HostCall) HostResult {
	var result HostResult
	var err error
	switch call.Op {
	case HostOpFetch:
		var u *url.URL
		u, err = url.Parse(call.URL)
		if err == nil {
			result.Fetch, err = s.host.Fetch(hostContext(ctx, call.Meta), call.Method, u, call.Body, call.ContentType)
		}
	case HostOpJSFetch:
		result.JS, err = s.host.JSFetch(ctx, call.JS)
	case HostOpRecordBlocked:
		s.host.RecordBlocked(call.Gen, call.Method, parseURLString(call.URL), call.Kind, hostError(call.ErrKind, call.ErrText))
	case HostOpApplyMixedContent:
		var u *url.URL
		u, err = url.Parse(call.URL)
		if err == nil {
			var resolved *url.URL
			resolved, err = s.host.ApplyMixedContent(hostContext(ctx, call.Meta), u)
			result.URL = urlString(resolved)
		}
	case HostOpIsLocalURL:
		result.Bool = s.host.IsLocalURL(parseURLString(call.URL))
	case HostOpSetUserAgent:
		s.host.SetUserAgent(call.UserAgent)
	case HostOpUserAgent:
		result.Text = s.host.UserAgent()
	case HostOpSetSearchURL:
		s.host.SetSearchURL(call.SearchURL)
	case HostOpSearchURL:
		result.Text = s.host.SearchURL()
	case HostOpSetImageCaps:
		s.host.SetImageCaps(call.Width, call.Height)
	case HostOpImageCaps:
		result.Width, result.Height = s.host.ImageCaps()
	case HostOpBeginPage:
		result.Int = s.host.BeginPage()
	case HostOpCurrentPageGeneration:
		result.Int = s.host.CurrentPageGeneration()
	case HostOpStartRequest:
		result.Int, result.Bool = s.host.StartRequest(call.Gen, call.Method, call.URL, call.Kind)
	case HostOpRecordCacheHit:
		s.host.RecordCacheHit(call.Gen, call.Method, call.URL, call.Kind, call.Size)
	case HostOpFinishRequest:
		s.host.FinishRequest(call.RequestID, call.Gen, call.Status, call.Size, call.Elapsed, hostError(call.ErrKind, call.ErrText))
	case HostOpRequestSnapshot:
		result.Records = s.host.RequestSnapshot()
	case HostOpRequestCount:
		result.Int = s.host.RequestCount()
	case HostOpCacheStats:
		result.Used, result.Limit, result.Entries = s.host.CacheStats()
	case HostOpCacheLimitBytes:
		result.Limit = s.host.CacheLimitBytes()
	case HostOpSetCacheLimitBytes:
		s.host.SetCacheLimitBytes(call.Limit)
	case HostOpDocumentCookieValues:
		result.Cookies = s.host.DocumentCookieValues(parseURLString(call.URL))
	case HostOpSetDocumentCookieRaw:
		result.Bool = s.host.SetDocumentCookieRaw(parseURLString(call.URL), call.CookieRaw)
	case HostOpCookieViews:
		result.Cookies = s.host.CookieViews(parseURLString(call.URL))
	case HostOpSetCertSource:
		err = s.host.SetCertSource(call.Source)
	case HostOpCertSource:
		result.Text = s.host.CertSource()
	default:
		err = fmt.Errorf("%w: host op %d", ErrInvalid, call.Op)
	}
	if err != nil {
		result.ErrKind = hostErrorKind(err)
		result.ErrText = err.Error()
	}
	return result
}

func (s *HostServer) send(result HostResult) {
	body, err := AppendHostResult(nil, result, s.lim)
	if err == nil && len(body) <= s.lim.MaxMessageBytes {
		_ = s.peer.WriteFrame(MuxHostResult, result.ID, body)
		return
	}
	// A result that cannot fit is reported as an error instead of leaving the
	// caller waiting for a frame that will never arrive.
	small := HostResult{ID: result.ID, ErrKind: HostErrLimit, ErrText: ErrLimit.Error()}
	if body, err = AppendHostResult(nil, small, s.lim); err == nil {
		_ = s.peer.WriteFrame(MuxHostResult, small.ID, body)
	}
}

func (s *HostServer) remove(id uint64) {
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
}

func (s *HostServer) cancel(id uint64) {
	s.mu.Lock()
	cancel := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// hostContext restores the request metadata the child sent as data. PageSpace
// stays host-local: the host creates it when the fetch starts.
func hostContext(ctx context.Context, meta RequestMeta) context.Context {
	return context.WithValue(ctx, RequestMetaKey{}, meta)
}

func (s *HostServer) cancelAll() {
	s.mu.Lock()
	pending := s.pending
	s.pending = make(map[uint64]context.CancelFunc)
	s.mu.Unlock()
	for _, cancel := range pending {
		cancel()
	}
}
