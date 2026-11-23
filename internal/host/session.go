package host

import (
	"context"
	"crypto/x509"
	"image"
	"net/http"
	"net/url"
	"sync"
	"time"

	"browser/internal/protocol"
)

var _ protocol.Host = (*Session)(nil)

// Session owns the host-side state one engine loader needs: HTTP client,
// cookies, settings, resource cache, request log and config token. It
// implements protocol.Host so the engine never imports this package.
type Session struct {
	client   *Client
	cookies  *CookieJar
	settings *Settings
	cache    *ResourceCache
	requests *RequestLog
	token    *ConfigToken
	// attestedID/URL hold the final URL of the latest document fetch. The
	// front end reads it by entry ID; the engine only triggers the fetch.
	attestedMu  sync.Mutex
	attestedID  uint64
	attestedURL string
}

// NewSession builds an empty host session. The caller applies UA, search,
// image caps and cache limits through the protocol.Host methods.
func NewSession() *Session {
	jar := NewCookieJar()
	return &Session{
		client:   NewClient(jar),
		cookies:  jar,
		settings: NewSettings("", ""),
		cache:    NewResourceCache(0),
		requests: NewRequestLog(),
		token:    NewConfigToken(),
	}
}

// AttestedURL reports the host's final URL for one document navigation. The
// front end calls it directly; the engine cannot write the value.
func (s *Session) AttestedURL(entryID uint64) (string, bool) {
	s.attestedMu.Lock()
	defer s.attestedMu.Unlock()
	if entryID == 0 || s.attestedID != entryID || s.attestedURL == "" {
		return "", false
	}
	return s.attestedURL, true
}

func (s *Session) fetcher() *Fetcher {
	return &Fetcher{
		Client:   s.client.HTTP(),
		Cache:    s.cache,
		Settings: s.settings,
		Requests: s.requests,
	}
}

// Fetch performs one raw request. It creates a page-space record when the
// caller has not supplied one, so the returned PageLocal flag covers the
// document connection.
func (s *Session) Fetch(ctx context.Context, method string, u *url.URL, body []byte, contentType string) (protocol.HostFetch, error) {
	space := protocol.PageSpaceFrom(ctx)
	if space == nil {
		space = &protocol.PageSpace{}
		ctx = protocol.WithPageSpace(ctx, space)
	}
	data, ctype, final, status, err := s.fetcher().FetchRequest(ctx, method, u, body, contentType)
	if err != nil {
		return protocol.HostFetch{}, err
	}
	local := IsLocalHost(final.Hostname()) || (space.Local.Load() && !RequestUsesProxy(final.String()))
	if final != nil {
		if meta := protocol.RequestMetaFrom(ctx); meta.EntryID != 0 && meta.Kind == "doc" {
			s.attestedMu.Lock()
			s.attestedID = meta.EntryID
			s.attestedURL = final.String()
			s.attestedMu.Unlock()
		}
	}
	return protocol.HostFetch{Data: data, ContentType: ctype, FinalURL: final, Status: status, PageLocal: local}, nil
}

func (s *Session) FetchCtx(ctx context.Context, u *url.URL) (protocol.HostFetch, error) {
	return s.Fetch(ctx, http.MethodGet, u, nil, "")
}

func (s *Session) JSFetch(ctx context.Context, req protocol.FetchRequest) (protocol.FetchResponse, error) {
	return s.fetcher().JSFetch(ctx, req)
}

func (s *Session) RecordBlocked(gen int, method string, u *url.URL, kind string, err error) {
	s.fetcher().RecordBlocked(gen, method, u, kind, err)
}

func (s *Session) ApplyMixedContent(ctx context.Context, u *url.URL) (*url.URL, error) {
	return ApplyMixedContent(ctx, u)
}
func (s *Session) IsLocalURL(u *url.URL) bool         { return IsLocalURL(u) }
func (s *Session) IsMixedContentError(err error) bool { return IsMixedContentError(err) }
func (s *Session) IsLocalNetworkError(err error) bool { return IsLocalNetworkError(err) }

func (s *Session) SetUserAgent(v string) { s.settings.SetUserAgent(v) }
func (s *Session) UserAgent() string     { return s.settings.UserAgent() }
func (s *Session) SetSearchURL(v string) { s.settings.SetSearchURL(v) }
func (s *Session) SearchURL() string     { return s.settings.SearchURL() }
func (s *Session) SetImageCaps(w, h int) { s.settings.SetImageCaps(w, h) }
func (s *Session) ImageCaps() (int, int) { return s.settings.ImageCaps() }

func (s *Session) BeginPage() int             { return s.requests.BeginPage() }
func (s *Session) CurrentPageGeneration() int { return s.requests.CurrentPageGeneration() }
func (s *Session) StartRequest(gen int, method, rawURL, kind string) (int, bool) {
	return s.requests.Start(gen, method, rawURL, kind)
}
func (s *Session) RecordCacheHit(gen int, method, rawURL, kind string, size int) {
	s.requests.RecordCacheHit(gen, method, rawURL, kind, size)
}
func (s *Session) FinishRequest(id, gen, status, size int, elapsed time.Duration, err error) {
	s.requests.Finish(id, gen, status, size, elapsed, err)
}
func (s *Session) RequestSnapshot() []protocol.RequestRecord { return s.requests.Snapshot() }
func (s *Session) RequestCount() int                         { return s.requests.Count() }

func (s *Session) CacheStats() (int64, int64, int) { return s.cache.Stats() }
func (s *Session) CacheLimitBytes() int64          { return s.cache.LimitBytes() }
func (s *Session) SetCacheLimitBytes(n int64)      { s.cache.SetLimit(n) }
func (s *Session) GetCachedImage(key string) (image.Image, int, bool, bool) {
	return s.cache.GetImage(key)
}
func (s *Session) PutCachedImage(key string, img image.Image, rawSize int, local bool) {
	s.cache.PutImage(key, img, rawSize, local)
}

func (s *Session) DocumentCookieValues(u *url.URL) []protocol.CookieView {
	return s.cookies.DocumentCookieValues(u)
}
func (s *Session) SetDocumentCookieRaw(u *url.URL, raw string) bool {
	return s.cookies.SetDocumentCookieRaw(u, raw)
}
func (s *Session) CookieViews(u *url.URL) []protocol.CookieView { return s.cookies.CookieViews(u) }

func (s *Session) SetCertSource(source string) error { return s.client.SetCertSource(source) }
func (s *Session) CertSource() string                { return s.client.CertSource() }
func (s *Session) CertPool() *x509.CertPool          { return s.client.CertPool() }

func (s *Session) ConfigToken() protocol.ConfigToken { return s.token }

// Test/debug accessors.
func (s *Session) Client() *Client       { return s.client }
func (s *Session) Cookies() *CookieJar   { return s.cookies }
func (s *Session) Settings() *Settings   { return s.settings }
func (s *Session) Cache() *ResourceCache { return s.cache }
func (s *Session) Requests() *RequestLog { return s.requests }
func (s *Session) SetHTTPClient(c *http.Client) {
	s.client = NewClientFromHTTP(c)
}
