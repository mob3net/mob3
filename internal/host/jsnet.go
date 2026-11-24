package host

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"browser/internal/protocol"
)

// JSFetch performs a script fetch/XHR under host policy: origin and CORS
// checks, credentials, redirects, mixed content, private networks and the
// request log all stay here.
func (f *Fetcher) JSFetch(ctx context.Context, req protocol.FetchRequest) (protocol.FetchResponse, error) {
	var out protocol.FetchResponse
	u, err := url.Parse(req.URL)
	if err != nil || u == nil {
		return out, errors.New("invalid request URL")
	}
	origin, _ := url.Parse(req.Origin)
	if origin == nil {
		return out, errors.New("cross-origin request blocked")
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	if len(req.Body) > maxResourceSize {
		return out, errors.New("request body too large")
	}
	crossOrigin := !sameOrigin(u, origin)
	headers := make(map[string]string, len(req.Headers))
	for _, h := range req.Headers {
		if h.Name == "" || isForbiddenRequestHeader(h.Name) {
			continue
		}
		headers[h.Name] = h.Value
	}
	if crossOrigin && !isSimpleCORSRequest(method, headers) {
		return out, errors.New("cross-origin request requires CORS preflight (unsupported)")
	}

	gen := f.Requests.CurrentPageGeneration()
	ctx = WithRequestMeta(ctx, gen, req.Kind)
	ctx = WithRequestPage(ctx, origin, req.PageLocal)
	ctx = WithRequestTarget(ctx, u)
	if RequestMetaFrom(ctx).BlockLocal && IsLocalURL(u) {
		f.RecordBlocked(gen, method, u, req.Kind, ErrLocalNetworkBlocked)
		return out, ErrLocalNetworkBlocked
	}
	if u, err = ApplyMixedContent(ctx, u); err != nil {
		f.RecordBlocked(gen, method, u, req.Kind, err)
		return out, err
	}
	blockLocal := RequestMetaFrom(ctx).BlockLocal
	var reqID int
	start := time.Now()
	if req.Kind != "" {
		reqID, _ = f.Requests.Start(gen, method, u.String(), req.Kind)
	}
	defer func() {
		if reqID != 0 {
			f.Requests.Finish(reqID, gen, out.Status, len(out.Body), time.Since(start), err)
		}
	}()

	var reader io.Reader
	if len(req.Body) > 0 {
		reader = bytes.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return out, err
	}
	httpReq.Header.Set("User-Agent", f.Settings.UserAgent())
	httpReq.Header.Set("Accept", "*/*")
	if crossOrigin && !req.NoCORS {
		httpReq.Header.Set("Origin", urlOrigin(origin))
	}
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}
	// Never write to the session client: the redirect policy and cookies are
	// per request. Cloning keeps the shared transport (and its pooling) while
	// leaving the one shared *http.Client untouched for navigations and
	// concurrent image loads. Cross-origin and credentialless requests never
	// send or store cookies.
	useCookies := !crossOrigin && req.Credentials
	policy := cookiePolicyFor(RequestMetaFrom(ctx))
	client := *f.Client
	client.Jar = nil
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if blockLocal && IsLocalURL(next.URL) {
			return ErrLocalNetworkBlocked
		}
		if origin.Scheme == "https" && next.URL.Scheme == "http" {
			return ErrMixedContentBlocked
		}
		if !crossOrigin && !sameOrigin(next.URL, origin) {
			return errors.New("cross-origin redirect blocked")
		}
		if useCookies {
			if next.Response != nil {
				f.storeCookies(next.Response)
			}
			f.applyCookies(next, policy)
		}
		// The dial guard and proxy decision must see the redirect target, not
		// the pre-redirect URL.
		*next = *next.WithContext(WithRequestTarget(next.Context(), next.URL))
		return nil
	}
	if useCookies {
		f.applyCookies(httpReq, policy)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		if IsLocalNetworkError(err) {
			err = ErrLocalNetworkBlocked
		}
		if IsMixedContentError(err) {
			err = ErrMixedContentBlocked
		}
		return out, err
	}
	defer resp.Body.Close()
	if useCookies {
		f.storeCookies(resp)
	}
	out.Status = resp.StatusCode
	if resp.Request != nil && resp.Request.URL != nil {
		out.FinalURL = resp.Request.URL.String()
	}
	out.ContentType = resp.Header.Get("Content-Type")
	out.Headers = responseHeaders(resp.Header, crossOrigin)
	if crossOrigin && !req.NoCORS && !corsAllowed(resp.Header.Get("Access-Control-Allow-Origin"), origin) {
		return out, errors.New("cross-origin response blocked by CORS")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResourceSize+1))
	if err != nil {
		return out, err
	}
	if len(data) > maxResourceSize {
		return out, ErrResourceTooLarge
	}
	out.Body = data
	return out, nil
}

func sameOrigin(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) &&
		effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	}
	return ""
}

func urlOrigin(u *url.URL) string {
	if u == nil {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Host)
	if (scheme == "http" && strings.HasSuffix(host, ":80")) ||
		(scheme == "https" && strings.HasSuffix(host, ":443")) {
		host = host[:strings.LastIndex(host, ":")]
	}
	return scheme + "://" + host
}

func corsAllowed(header string, origin *url.URL) bool {
	allowed := strings.TrimSpace(header)
	if allowed == "*" {
		return true
	}
	return allowed != "" && strings.EqualFold(strings.TrimRight(allowed, "/"), strings.TrimRight(urlOrigin(origin), "/"))
}

func isSimpleCORSRequest(method string, headers map[string]string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodPost:
	default:
		return false
	}
	simple := map[string]bool{
		"accept": true, "accept-language": true, "content-language": true,
		"content-type": true, "range": true,
	}
	for k, v := range headers {
		lk := strings.ToLower(strings.TrimSpace(k))
		if !simple[lk] {
			return false
		}
		if lk == "content-type" {
			media := strings.ToLower(strings.TrimSpace(strings.Split(v, ";")[0]))
			switch media {
			case "application/x-www-form-urlencoded", "multipart/form-data", "text/plain":
			default:
				return false
			}
		}
	}
	return true
}

func isForbiddenRequestHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "accept-charset", "accept-encoding", "access-control-request-headers",
		"access-control-request-method", "connection", "content-length", "cookie",
		"cookie2", "date", "dnt", "expect", "host", "keep-alive", "origin",
		"referer", "te", "trailer", "transfer-encoding", "upgrade", "via":
		return true
	}
	return false
}

func responseHeaders(h http.Header, crossOrigin bool) []protocol.Header {
	simple := map[string]bool{
		"cache-control": true, "content-language": true, "content-length": true,
		"content-type": true, "expires": true, "last-modified": true, "pragma": true,
	}
	out := make([]protocol.Header, 0, len(h))
	for name, values := range h {
		if crossOrigin && !simple[strings.ToLower(name)] &&
			!strings.EqualFold(name, "Access-Control-Allow-Origin") {
			continue
		}
		for _, v := range values {
			out = append(out, protocol.Header{Name: name, Value: v})
		}
	}
	return out
}
