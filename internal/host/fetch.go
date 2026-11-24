package host

import (
	"browser/internal/protocol"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"time"
)

const maxResourceSize = 12 << 20

var ErrResourceTooLarge = protocol.ErrResourceTooLarge

// FetchResponse is one raw HTTP/data response.
type FetchResponse struct {
	Data        []byte
	ContentType string
	FinalURL    *url.URL
	Status      int
}

// Fetcher performs raw host requests with policy, cache and request logging.
type Fetcher struct {
	Client   *http.Client
	Cache    *ResourceCache
	Settings *Settings
	Requests *RequestLog
	Cookies  *CookieJar
}

// cookiePolicyFor derives the SameSite context of a request from its page
// metadata.
func cookiePolicyFor(meta RequestMeta) RequestCookiePolicy {
	u, _ := url.Parse(meta.PageURL)
	return RequestCookiePolicy{TopSite: siteOf(u), TopLevel: meta.Kind == "doc"}
}

// applyCookies replaces the Cookie header according to the SameSite policy.
func (f *Fetcher) applyCookies(req *http.Request, policy RequestCookiePolicy) {
	if f.Cookies == nil || req == nil {
		return
	}
	req.Header.Del("Cookie")
	for _, c := range f.Cookies.RequestCookies(req.URL, req.Method, policy) {
		req.AddCookie(c)
	}
}

// storeCookies records Set-Cookie headers from one response. Requests set the
// client jar to nil so cookies can be filtered per request, which also means
// every hop must store its own response cookies.
func (f *Fetcher) storeCookies(resp *http.Response) {
	if f.Cookies == nil || resp == nil || resp.Request == nil || resp.Request.URL == nil {
		return
	}
	f.Cookies.SetCookies(resp.Request.URL, resp.Cookies())
}

func (f *Fetcher) FetchCtx(ctx context.Context, u *url.URL) (data []byte, ctype string, final *url.URL, status int, err error) {
	return f.FetchRequest(ctx, http.MethodGet, u, nil, "")
}

// recordBlocked adds a blocked request to the console request log.
func (f *Fetcher) RecordBlocked(gen int, method string, u *url.URL, kind string, err error) {
	if kind == "" || u == nil {
		return
	}
	reqID, _ := f.Requests.Start(gen, method, u.String(), kind)
	f.Requests.Finish(reqID, gen, 0, 0, 0, err)
}

func (f *Fetcher) FetchRequest(ctx context.Context, method string, u *url.URL, body []byte, contentType string) (data []byte, ctype string, final *url.URL, status int, err error) {
	if err := ctx.Err(); err != nil {
		return nil, "", u, 0, err
	}
	if space := PageSpaceFrom(ctx); space != nil {
		// The document request records whether its own connection was local,
		// so an intranet page is not treated like a public one.
		ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
			GotConn: func(info httptrace.GotConnInfo) {
				if info.Conn != nil && RemoteAddrLocal(info.Conn.RemoteAddr()) {
					space.Local.Store(true)
				}
			},
		})
	}
	if method == "" {
		method = http.MethodGet
	}
	if len(body) > maxResourceSize {
		return nil, "", u, 0, errors.New("request body too large")
	}
	meta := RequestMetaFrom(ctx)
	gen := meta.Gen
	if gen == 0 {
		gen = f.Requests.CurrentPageGeneration()
	}
	if meta.BlockLocal && IsLocalURL(u) {
		// A public document may not address a literal local host. Names that
		// only resolve locally are caught by localNetworkControl at dial time.
		f.RecordBlocked(gen, method, u, meta.Kind, ErrLocalNetworkBlocked)
		return nil, "", u, 0, ErrLocalNetworkBlocked
	}
	if u, err = ApplyMixedContent(ctx, u); err != nil {
		f.RecordBlocked(gen, method, u, meta.Kind, err)
		return nil, "", u, 0, err
	}
	cacheable := method == http.MethodGet && len(body) == 0 &&
		(u.Scheme == "http" || u.Scheme == "https") &&
		(meta.Kind == "css" || meta.Kind == "js")
	if cacheable {
		if cached, ct, final, cachedLocal, ok := f.Cache.GetBytes(u.String()); ok && !(meta.BlockLocal && cachedLocal) {
			f.Requests.RecordCacheHit(gen, method, u.String(), meta.Kind, len(cached))
			finalURL := u
			if parsed, err := url.Parse(final); err == nil && parsed.IsAbs() {
				finalURL = parsed
			}
			return cached, ct, finalURL, 200, nil
		}
	}
	var reqID int
	start := time.Now()
	if meta.Kind != "" {
		reqID, _ = f.Requests.Start(gen, method, u.String(), meta.Kind)
	}
	defer func() {
		if reqID != 0 {
			f.Requests.Finish(reqID, gen, status, len(data), time.Since(start), err)
		}
	}()
	switch u.Scheme {
	case "file":
		return nil, "", u, 0, errors.New("local files are not supported")
	case "data":
		b, ct, e := ParseDataURI(u.String())
		return b, ct, u, 200, e
	case "http", "https":
		var reader io.Reader
		if len(body) > 0 {
			reader = bytes.NewReader(body)
		}
		reqCtx := WithRequestTarget(ctx, u)
		req, e := http.NewRequestWithContext(reqCtx, method, u.String(), reader)
		if e != nil {
			return nil, "", u, 0, e
		}
		if len(body) > 0 {
			req.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(body)), nil
			}
			if contentType == "" {
				contentType = "application/x-www-form-urlencoded"
			}
			req.Header.Set("Content-Type", contentType)
		}
		req.Header.Set("User-Agent", f.Settings.UserAgent())
		req.Header.Set("Accept", "text/html,application/xhtml+xml,text/css;q=0.9,image/*;q=0.8,*/*;q=0.5")
		req.Header.Set("Accept-Language", "en")
		if meta.Kind == "doc" && meta.Origin != "" &&
			method != http.MethodGet && method != http.MethodHead {
			// Browsers attach Origin to non-GET document requests, which lets
			// a server's Origin-based CSRF check see the submitting page.
			req.Header.Set("Origin", meta.Origin)
		}
		// The redirect policy and cookies are per request. Cloning (and
		// dropping the jar) also keeps the shared client untouched for
		// concurrent fetches.
		policy := cookiePolicyFor(meta)
		c := *f.Client
		c.Jar = nil
		c.CheckRedirect = f.redirectCheck(meta.BlockLocal, meta.PageSecure && meta.Kind != "" && meta.Kind != "doc", policy)
		f.applyCookies(req, policy)
		resp, e := c.Do(req)
		if e != nil {
			if IsLocalNetworkError(e) {
				e = ErrLocalNetworkBlocked
			}
			if IsMixedContentError(e) {
				e = ErrMixedContentBlocked
			}
			return nil, "", u, 0, e
		}
		defer resp.Body.Close()
		f.storeCookies(resp)
		if enc := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))); enc != "" && enc != "identity" && !resp.Uncompressed {
			// Go's transport decodes gzip transparently and removes the header.
			// Anything still marked (brotli, zstd, …) would be parsed as raw
			// bytes and produce meaningless output, so report it instead.
			return nil, "", u, resp.StatusCode, errors.New("unsupported Content-Encoding: " + enc)
		}
		b, e := io.ReadAll(io.LimitReader(resp.Body, maxResourceSize+1))
		if e != nil {
			return nil, "", u, resp.StatusCode, e
		}
		if len(b) > maxResourceSize {
			return nil, "", u, resp.StatusCode, ErrResourceTooLarge
		}
		ctype := resp.Header.Get("Content-Type")
		if cacheable && resp.StatusCode == 200 && CacheControlAllows(resp.Header.Get("Cache-Control")) {
			f.Cache.PutBytes(u.String(), b, ctype, resp.Request.URL.String(), !meta.BlockLocal)
		}
		return b, ctype, resp.Request.URL, resp.StatusCode, nil
	}
	return nil, "", u, 0, errors.New("unsupported URL scheme: " + u.Scheme)
}

// redirectCheck enforces the redirect limit, mixed-content rule and
// local-network policy on every hop, not just the initial URL. It also
// retargets the request metadata so the dial guard and the proxy decision see
// the host actually being contacted.
func (f *Fetcher) redirectCheck(blockLocal, secureSubresource bool, policy RequestCookiePolicy) func(*http.Request, []*http.Request) error {
	return func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if secureSubresource && next.URL.Scheme == "http" {
			return ErrMixedContentBlocked
		}
		if blockLocal && IsLocalURL(next.URL) {
			return ErrLocalNetworkBlocked
		}
		if next.Response != nil {
			f.storeCookies(next.Response)
		}
		f.applyCookies(next, policy)
		*next = *next.WithContext(WithRequestTarget(next.Context(), next.URL))
		return nil
	}
}

// ParseDataURI decodes a data: URL.
func ParseDataURI(s string) ([]byte, string, error) {
	if len(s) > maxResourceSize+maxResourceSize/2 {
		return nil, "", errors.New("data URI too large")
	}
	rest := strings.TrimPrefix(s, "data:")
	comma := strings.IndexByte(rest, ',')
	if comma < 0 {
		return nil, "", errors.New("bad data URI")
	}
	meta := rest[:comma]
	payload := rest[comma+1:]
	ctype := meta
	if i := strings.IndexByte(ctype, ';'); i >= 0 {
		ctype = ctype[:i]
	}
	if ctype == "" {
		ctype = "text/plain"
	}
	if strings.Contains(strings.ToLower(meta), ";base64") {
		b, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, "", err
		}
		if len(b) > maxResourceSize {
			return nil, "", errors.New("data URI too large")
		}
		return b, ctype, nil
	}
	b, err := url.PathUnescape(payload)
	if err != nil {
		return nil, "", err
	}
	if len(b) > maxResourceSize {
		return nil, "", errors.New("data URI too large")
	}
	return []byte(b), ctype, nil
}
