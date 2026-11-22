package browser

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
	"golang.org/x/net/publicsuffix"
	"golang.org/x/text/encoding/charmap"
)

const (
	maxResourceSize = 12 << 20
	maxImagePixels  = 25_000_000 // 25 MP; bounds decompression cost
)

// DefaultSearchURL is the template used when input looks like a search query.
// %s is replaced with the URL-escaped query; an empty template disables search.
// DuckDuckGo's html endpoint is server-rendered, unlike the JS-only main page.
const DefaultSearchURL = "https://html.duckduckgo.com/html/?q=%s"

// imageConfigOK rejects decompression bombs before decoding pixel data.
func imageConfigOK(cfg image.Config) bool {
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return false
	}
	return int64(cfg.Width)*int64(cfg.Height) <= maxImagePixels
}

// Page is a loaded document plus the assets needed to display it.
type Page struct {
	URL     *url.URL
	Base    *url.URL
	Root    *Node
	Title   string
	Author  []Rule
	Scripts []Script
	// ImportMap holds the document's <script type="importmap"> imports, so
	// module scripts can resolve bare specifiers like "react".
	ImportMap   map[string]string
	RawHTML     string
	Status      int
	Blocked     bool
	BlockReason string
	// Local records that the document itself was fetched from a loopback,
	// private or link-local address. Its subresources may then reach other
	// local addresses; a public document may not.
	Local bool
	// Download marks a response that is not renderable (a binary or other
	// non-HTML resource). The browser shows a curl hint instead of applying it.
	Download     bool
	DownloadName string
	// PDF holds the extracted text when this page came from a PDF response.
	PDF *PDFDocument
	// parsedStyles records the inline <style> elements already represented in
	// Author, so component CSS injected by scripts is parsed once.
	parsedStyles map[*Node]string
}

// Script is one page script in document order. Src is a label for errors.
type Script struct {
	Code string
	Src  string
	Node *Node
	// Module marks an ES module script. Module scripts are parsed and linked
	// through the module loader instead of running as a classic script.
	Module bool
}

// Loader fetches documents and their resources.
// defaultUserAgent is the browser's identifying User-Agent.
const defaultUserAgent = "MOB3/1.0 (+terminal; +https://mob3.net)"

type Loader struct {
	client    *http.Client
	cookies   *cookieJar
	userAgent string
	searchURL string
	noScripts bool
	// transport is the stable RoundTripper whose base is replaced when the
	// certificate source changes; client.Transport always points at it.
	transport *certTransport
	// certSource labels the active TLS trust store ("system", "embedded", or a
	// bundle path) and certPool holds its roots. A nil pool means the platform
	// store. See SetCertSource.
	certSource string
	certPool   *x509.CertPool
	// historyFn supplies the in-memory session history for about:history.
	historyFn func() ([]string, int)
	bookmarks []Bookmark
	// bookmarksSet records that -bookmarks was supplied, so the toolbar can
	// hide its bookmark button when no bookmark file was requested.
	bookmarksSet bool
	// ignoreBlocks disables anti-bot/CAPTCHA detection when set, so the
	// challenge page is loaded and its scripts run. -ignore-blocks sets it.
	ignoreBlocks bool
	// noPDF disables the built-in PDF text viewer, so a PDF response keeps
	// opening the download prompt instead of being rendered as text.
	// -no-pdf sets it.
	noPDF bool
	// pdfPage restricts PDF rendering to one page (1-based); zero keeps every
	// page. It backs -pdf-page for dump and PNG output.
	pdfPage int
	// noImages skips external image downloads and inline SVG rasterization.
	// It is the default so the browser stays fast; -graphics=none keeps it,
	// and any other graphics mode turns it off.
	noImages bool
	// deferImages makes Load skip image downloads so the caller can show the
	// page first and fetch them in the background.
	deferImages bool
	// imageViewportPx is the CSS-pixel viewport width used to choose srcset and
	// picture candidates. It is 800 when no browser width is available.
	imageViewportPx int
	// maxImageW and maxImageH cap a decoded image's pixel dimensions. The
	// browser sets them from the terminal viewport so a full-resolution photo
	// is downscaled once at load instead of staying in memory until paint.
	// Zero disables the cap.
	maxImageW, maxImageH int
	// imageBudget bounds decoded image bytes in flight; svgBudgetPixels and
	// the counters bound inline SVG rasterization for the current page.
	imageBudget     *byteLimiter
	svgMu           sync.Mutex
	svgBudgetPixels int64
	svgPixels       int64
	svgCount        int
	// cache holds raw subresources and decoded images for this session.
	cache *resourceCache

	reqMu    sync.Mutex
	reqGen   int
	reqNext  int
	requests []requestRecord
}

// requestRecord is one network request shown in the JS console's request pane.
type requestRecord struct {
	ID      int
	Gen     int
	Method  string
	URL     string
	Kind    string
	Status  int
	Size    int
	Elapsed time.Duration
	Err     string
	Done    bool
	Cached  bool
}

type requestMetaKey struct{}

type requestMeta struct {
	gen  int
	kind string
	// pageURL is the document that initiated the request. blockLocal marks a
	// request from a public document that may therefore not reach loopback,
	// private or link-local addresses. targetURL lets the dial guard tell a
	// direct connection from one made through an HTTP proxy. pageSecure marks
	// an HTTPS document, whose HTTP subresources are upgraded or blocked.
	pageURL    string
	targetURL  string
	blockLocal bool
	pageSecure bool
}

// pageSpace records whether the document's own connection landed on a local
// address, so an intranet page keeps access to its other local hosts while a
// public page does not gain it.
type pageSpace struct {
	local atomic.Bool
}

type pageSpaceKey struct{}

func withPageSpace(ctx context.Context, space *pageSpace) context.Context {
	return context.WithValue(ctx, pageSpaceKey{}, space)
}

func pageSpaceFrom(ctx context.Context) *pageSpace {
	space, _ := ctx.Value(pageSpaceKey{}).(*pageSpace)
	return space
}

// withRequestPage records the initiating document and whether it may reach
// local-network addresses.
func withRequestPage(ctx context.Context, page *url.URL, pageLocal bool) context.Context {
	m := requestMetaFrom(ctx)
	if page != nil {
		m.pageURL = page.String()
		m.blockLocal = (page.Scheme == "http" || page.Scheme == "https") && !pageLocal
		m.pageSecure = page.Scheme == "https"
	}
	return context.WithValue(ctx, requestMetaKey{}, m)
}

// withRequestTarget records the URL a request will contact, so the dial guard
// can skip a connection that goes to an HTTP proxy instead of the target.
func withRequestTarget(ctx context.Context, target *url.URL) context.Context {
	if target == nil {
		return ctx
	}
	m := requestMetaFrom(ctx)
	m.targetURL = target.String()
	return context.WithValue(ctx, requestMetaKey{}, m)
}

// requestUsesProxy reports whether the environment routes this URL through an
// HTTP proxy. The proxy resolves the target, so the local-network guard cannot
// inspect it and must not mistake the proxy address for the target.
func requestUsesProxy(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return false
	}
	proxy, err := http.ProxyFromEnvironment(req)
	return err == nil && proxy != nil
}

// errLocalNetworkBlocked is returned when a public document tries to reach a
// loopback, private or link-local address.
var errLocalNetworkBlocked = errors.New("blocked local-network request")

// errMixedContentBlocked is returned when an HTTPS document requests a
// blockable HTTP subresource, or a redirect tries to downgrade one.
var errMixedContentBlocked = errors.New("blocked mixed content: HTTP subresource on an HTTPS page")

// applyMixedContent upgrades an HTTP image requested by an HTTPS document to
// HTTPS, and rejects every other HTTP subresource. Upgraded images are not
// retried over HTTP if the HTTPS request fails.
func applyMixedContent(ctx context.Context, u *url.URL) (*url.URL, error) {
	meta := requestMetaFrom(ctx)
	if u == nil || !meta.pageSecure || u.Scheme != "http" {
		return u, nil
	}
	if meta.kind == "img" && net.ParseIP(u.Hostname()) == nil {
		up := *u
		up.Scheme = "https"
		return &up, nil
	}
	return u, errMixedContentBlocked
}

func isMixedContentError(err error) bool {
	return errors.Is(err, errMixedContentBlocked)
}

// isLocalHost reports whether a URL host is a loopback, private, link-local or
// local name. Hosts that only resolve locally are caught after DNS resolution
// by localNetworkControl.
func isLocalHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return false
	}
	if i := strings.IndexByte(host, '%'); i >= 0 { // IPv6 zone
		host = host[:i]
	}
	if ip := net.ParseIP(host); ip != nil {
		return isLocalIP(ip)
	}
	return host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		host == "local" || strings.HasSuffix(host, ".local")
}

func isLocalIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4[0] == 100 && ip4[1]&0xc0 == 0x40: // 100.64.0.0/10 CGNAT
			return true
		case ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 0: // 192.0.0.0/24
			return true
		case ip4[0] == 198 && ip4[1]&0xfe == 18: // 198.18.0.0/15
			return true
		case ip4[0] >= 240: // 240.0.0.0/4 reserved
			return true
		}
	}
	return false
}

func isLocalURL(u *url.URL) bool {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return isLocalHost(u.Hostname())
}

// localNetworkControl runs after DNS resolution and before the connect, so a
// public name that resolves to a local address is still caught.
func localNetworkControl(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil
	}
	if isLocalHost(host) {
		return errLocalNetworkBlocked
	}
	return nil
}

var (
	plainDialer = &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	guardedDialer = &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   localNetworkControl,
	}
)

// localNetworkDialContext picks the guarded dialer for requests a public
// document initiated. The transport passes the request context through, so
// the request metadata survives into the dial.
func localNetworkDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	m := requestMetaFrom(ctx)
	if m.blockLocal && !requestUsesProxy(m.targetURL) {
		return guardedDialer.DialContext(ctx, network, addr)
	}
	return plainDialer.DialContext(ctx, network, addr)
}

func isLocalNetworkError(err error) bool {
	return errors.Is(err, errLocalNetworkBlocked)
}

// remoteAddrLocal reports whether a completed connection's peer is local.
func remoteAddrLocal(addr net.Addr) bool {
	if addr == nil {
		return false
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return false
	}
	return isLocalHost(host)
}

func withRequestMeta(ctx context.Context, gen int, kind string) context.Context {
	return context.WithValue(ctx, requestMetaKey{}, requestMeta{gen: gen, kind: kind})
}

func withRequestKind(ctx context.Context, kind string) context.Context {
	m, _ := ctx.Value(requestMetaKey{}).(requestMeta)
	m.kind = kind
	return context.WithValue(ctx, requestMetaKey{}, m)
}

func requestMetaFrom(ctx context.Context) requestMeta {
	m, _ := ctx.Value(requestMetaKey{}).(requestMeta)
	return m
}

// beginPage clears the request list and starts a new page generation.
func (l *Loader) beginPage() int {
	l.reqMu.Lock()
	defer l.reqMu.Unlock()
	l.reqGen++
	l.requests = nil
	return l.reqGen
}

func (l *Loader) currentPageGeneration() int {
	l.reqMu.Lock()
	defer l.reqMu.Unlock()
	return l.reqGen
}

func (l *Loader) startRequest(gen int, method, rawURL, kind string) (int, bool) {
	l.reqMu.Lock()
	defer l.reqMu.Unlock()
	if gen != l.reqGen {
		return 0, false
	}
	l.reqNext++
	id := l.reqNext
	l.requests = append(l.requests, requestRecord{
		ID: id, Gen: gen, Method: method, URL: rawURL, Kind: kind,
	})
	if len(l.requests) > 512 {
		l.requests = append([]requestRecord(nil), l.requests[len(l.requests)-512:]...)
	}
	return id, true
}

func (l *Loader) recordCacheHit(gen int, method, rawURL, kind string, size int) {
	l.reqMu.Lock()
	defer l.reqMu.Unlock()
	if gen != l.reqGen {
		return
	}
	l.reqNext++
	l.requests = append(l.requests, requestRecord{
		ID: l.reqNext, Gen: gen, Method: method, URL: rawURL, Kind: kind,
		Status: 200, Size: size, Done: true, Cached: true,
	})
	if len(l.requests) > 512 {
		l.requests = append([]requestRecord(nil), l.requests[len(l.requests)-512:]...)
	}
}

func (l *Loader) finishRequest(id, gen int, status, size int, elapsed time.Duration, err error) {
	l.reqMu.Lock()
	defer l.reqMu.Unlock()
	if gen != l.reqGen {
		return
	}
	for i := range l.requests {
		if l.requests[i].ID == id {
			l.requests[i].Status = status
			l.requests[i].Size = size
			l.requests[i].Elapsed = elapsed
			l.requests[i].Done = true
			if err != nil {
				l.requests[i].Err = err.Error()
			}
			return
		}
	}
}

func (l *Loader) requestSnapshot() []requestRecord {
	l.reqMu.Lock()
	defer l.reqMu.Unlock()
	out := make([]requestRecord, len(l.requests))
	copy(out, l.requests)
	return out
}

func (l *Loader) requestCount() int {
	l.reqMu.Lock()
	defer l.reqMu.Unlock()
	return len(l.requests)
}

// NewLoader creates a loader with cookie support and a sane timeout. The
// platform trust store is used until SetCertSource selects another one; the
// client's transport is a stable wrapper so that swap never races a request.
func NewLoader() *Loader {
	jar := newCookieJar()
	tr := &certTransport{base: newHTTPTransport(nil)}
	l := &Loader{
		cache:           newResourceCache(resourceCacheBudget(effectiveMemoryLimit())),
		cookies:         jar,
		noImages:        true,
		imageViewportPx: 800,
		client: &http.Client{
			Jar:       jar,
			Transport: tr,
			Timeout:   45 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("too many redirects")
				}
				return nil
			},
		},
		userAgent:  defaultUserAgent,
		searchURL:  DefaultSearchURL,
		transport:  tr,
		certSource: CertSourceSystem,
	}
	l.imageBudget = newByteLimiter(imageByteBudget(0))
	l.setMemoryBudget(effectiveMemoryLimit())
	return l
}

// ResolveURL turns user input into an absolute URL without search fallback.
func ResolveURL(input string, base *url.URL) (*url.URL, error) {
	return resolveURL(input, base, "")
}

// ResolveInput is like ResolveURL but falls back to searchURL when the input
// looks like a search query rather than a URL. An empty searchURL disables it.
func ResolveInput(input string, base *url.URL, searchURL string) (*url.URL, error) {
	return resolveURL(input, base, searchURL)
}

func resolveURL(input string, base *url.URL, searchURL string) (*url.URL, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return nil, errors.New("empty URL")
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "file:") {
		return nil, errors.New("local files are not supported")
	}
	if strings.HasPrefix(lower, "about:") || strings.HasPrefix(lower, "data:") {
		return url.Parse(s)
	}
	for _, bad := range []string{"javascript:", "mailto:", "tel:", "ftp:"} {
		if strings.HasPrefix(lower, bad) {
			return nil, errors.New("unsupported URL scheme: " + bad[:len(bad)-1])
		}
	}
	// Fragment/query/relative references only make sense against a base.
	if base != nil && (strings.HasPrefix(s, "#") || strings.HasPrefix(s, "?") || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../")) {
		if r, err := base.Parse(s); err == nil {
			return r, nil
		}
	}
	if strings.HasPrefix(s, "//") {
		scheme := "https"
		if base != nil && base.Scheme != "" {
			scheme = base.Scheme
		}
		s = scheme + ":" + s
	}
	if strings.Contains(s, "://") {
		return url.Parse(s)
	}
	if strings.Contains(s, ":") && !strings.ContainsAny(s, " /") {
		// host:port, most likely a local development server
		return url.Parse("http://" + s)
	}
	if searchURL != "" && looksLikeSearch(s) {
		return searchURLFor(searchURL, s)
	}
	return url.Parse("https://" + s)
}

// looksLikeSearch reports whether input is a query rather than a host or path:
// it contains whitespace, or has no dot to form a host name.
func looksLikeSearch(s string) bool {
	if strings.ContainsAny(s, " \t\n\r") {
		return true
	}
	return !strings.Contains(s, ".")
}

// searchURLFor substitutes the escaped query into the search template. A
// template without %s gets the query appended.
func searchURLFor(template, query string) (*url.URL, error) {
	q := url.QueryEscape(query)
	if strings.Contains(template, "%s") {
		return url.Parse(strings.ReplaceAll(template, "%s", q))
	}
	return url.Parse(template + q)
}

// Load fetches a page and all of its stylesheets and images.
func (l *Loader) Load(input string, base *url.URL) (*Page, error) {
	return l.LoadContext(context.Background(), input, base)
}

// LoadContext is Load with cancellation support. Canceling ctx aborts the
// page request, stylesheets, scripts and image downloads.
func (l *Loader) LoadContext(ctx context.Context, input string, base *url.URL) (*Page, error) {
	return l.LoadRequest(ctx, http.MethodGet, input, nil, "", base)
}

// LoadRequest loads a page with an explicit HTTP method and request body.
// Resources discovered while parsing the response are always fetched with GET.
func (l *Loader) LoadRequest(ctx context.Context, method, input string, body []byte, contentType string, base *url.URL) (*Page, error) {
	return l.loadRequest(ctx, method, input, body, contentType, base, l.ignoreBlocks)
}

// LoadRequestBypass is LoadRequest with block detection disabled for this one
// load. It backs the "Try anyway" link on the blocked page.
func (l *Loader) LoadRequestBypass(ctx context.Context, method, input string, body []byte, contentType string, base *url.URL) (*Page, error) {
	return l.loadRequest(ctx, method, input, body, contentType, base, true)
}

func (l *Loader) loadRequest(ctx context.Context, method, input string, body []byte, contentType string, base *url.URL, ignoreBlocks bool) (*Page, error) {
	l.resetSVGBudget()
	u, err := ResolveInput(input, base, l.searchURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "about" {
		if !strings.EqualFold(method, http.MethodGet) {
			return nil, errors.New("POST is not supported for " + u.Scheme + " URLs")
		}
		return l.loadAbout(ctx, u)
	}
	ctx = withRequestMeta(ctx, l.currentPageGeneration(), "doc")
	space := &pageSpace{}
	ctx = withPageSpace(ctx, space)
	data, ctype, finalURL, status, err := l.fetchRequest(ctx, method, u, body, contentType)
	if err != nil {
		return nil, err
	}
	pageLocal := isLocalHost(finalURL.Hostname()) ||
		(space.local.Load() && !requestUsesProxy(finalURL.String()))
	// Every subresource fetch inherits this context, so the guard knows
	// whether the document itself came from a local address.
	ctx = withRequestPage(ctx, finalURL, pageLocal)
	mediaType := ""
	if mt, _, err := mime.ParseMediaType(ctype); err == nil {
		mediaType = strings.ToLower(mt)
	}
	// PDFs are decoded by their own reader, so they skip charset decoding and
	// the anti-bot heuristics that only make sense for HTML.
	if !l.noPDF && looksLikePDF(mediaType, data) {
		page := l.loadPDFPage(ctx, data, finalURL, status)
		if page != nil {
			page.Local = pageLocal
		}
		return page, nil
	}
	text := decodeBody(data, ctype)
	rawHTML := text
	if !ignoreBlocks {
		if blocked, reason := blockedResponse(status, text); blocked {
			root := ParseHTML(blockedPageHTML(finalURL.String(), reason, status))
			page := &Page{
				URL: finalURL, Base: finalURL, Root: root, RawHTML: rawHTML,
				Title: "Site blocked", Status: status, Blocked: true, BlockReason: reason,
				Local: pageLocal,
			}
			// The generated page carries its own stylesheet. Collect it like any
			// other document, or the blocked page falls back to bare
			// user-agent styles: full-width text and no warning box.
			page.Author = l.collectStylesheets(ctx, root, page.Base)
			return page, nil
		}
	}
	download := false
	downloadName := ""
	switch {
	case mediaType == "text/plain":
		text = "<html><body><pre>" + EscapeHTML(text) + "</pre></body></html>"
	case mediaType != "" && mediaType != "text/html" && mediaType != "application/xhtml+xml" && !strings.HasSuffix(mediaType, "+html"):
		if finalURL.Scheme == "http" || finalURL.Scheme == "https" {
			download = true
			downloadName = finalURL.Path
			if i := strings.LastIndexByte(downloadName, '/'); i >= 0 {
				downloadName = downloadName[i+1:]
			}
			if unescaped, err := url.PathUnescape(downloadName); err == nil {
				downloadName = unescaped
			}
		}
		text = cannotDisplayPageHTML(mediaType, finalURL)
	}
	root := ParseHTML(text)
	if !l.noScripts {
		// <noscript> content is inactive with scripting enabled; remove it
		// before collecting styles or scripts (some sites use it to hide the
		// whole body until hydration).
		removeNoscript(root)
	}
	page := &Page{
		URL: finalURL, Base: finalURL, Root: root, RawHTML: rawHTML, Status: status,
		Download: download, DownloadName: downloadName, Local: pageLocal,
	}
	if baseEl := root.FindFirst("base"); baseEl != nil {
		if href := baseEl.Attr("href"); href != "" {
			if bu, err := finalURL.Parse(href); err == nil {
				page.Base = bu
			}
		}
	}
	if t := root.FindFirst("title"); t != nil {
		page.Title = strings.Join(strings.Fields(t.TextContent()), " ")
	}
	if page.Title == "" {
		page.Title = finalURL.String()
	}
	page.Author = l.collectStylesheets(ctx, root, page.Base)
	if !l.noScripts {
		page.Scripts = l.collectScripts(ctx, root, page.Base)
		page.ImportMap = collectImportMap(root)
	}
	l.loadInlineSVGs(root)
	if !l.deferImages {
		l.loadImages(ctx, root, page.Base)
	}
	return page, nil
}

// blockedResponse recognizes anti-bot/CAPTCHA/firewall roadblocks. Vendor
// names are only used as a hint; the page shown to the user stays generic.
func blockedResponse(status int, body string) (bool, string) {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable:
	default:
		return false, ""
	}
	head := strings.ToLower(body[:minInt(len(body), 24000)])
	signatures := []struct {
		key    string
		reason string
	}{
		{"cf-chl", "Cloudflare browser challenge"},
		{"challenge-platform", "Cloudflare browser challenge"},
		{"just a moment", "browser challenge"},
		{"enable javascript and cookies to continue", "browser challenge"},
		{"datadome", "DataDome bot protection"},
		{"captcha-delivery", "CAPTCHA challenge"},
		{"px-captcha", "PerimeterX bot protection"},
		{"perimeterx", "PerimeterX bot protection"},
		{"incapsula", "Imperva firewall"},
		{"sucuri website firewall", "Sucuri firewall"},
		{"pardon our interruption", "anti-bot check"},
		{"unusual traffic", "bot check"},
		{"verify you are a human", "human verification"},
		{"access denied", "access denied"},
	}
	for _, s := range signatures {
		if strings.Contains(head, s.key) {
			return true, s.reason
		}
	}
	return false, ""
}

func blockedPageHTML(rawURL, reason string, status int) string {
	if reason == "" {
		reason = "site protection"
	}
	body := `<h1>Site blocked</h1>
<p class="warn">This site blocked MOB³ or requires a browser check that it
cannot complete. Common causes are Cloudflare, CAPTCHA, anti-bot or firewall
protection, and login protections that need a full browser with
JavaScript/WASM, fingerprinting and browser APIs.</p>
<p>Detected: <b>` + EscapeHTML(reason) + `</b> (HTTP ` + strconv.Itoa(status) + `).</p>
<p class="try"><a href="about:retry">Try anyway</a> — reload this URL without block
detection and run the challenge page's scripts. This is a one-shot bypass; the
site may still refuse access. Challenge pages can run arbitrary script, and
MOB³ is not a secure browser, so only do this if you trust the site.</p>
<p>The requested page was not loaded. Try a different source, use the site's
direct or alternative URL, or open it in a full browser.</p>
<p>URL: <b>` + EscapeHTML(rawURL) + `</b></p>`
	const css = `h1 { color: #b42318; }
.try { margin: 16px 0; padding: 8px; border: 1px solid #2f6feb; background: #eef4ff; }
`
	return appPageHTML("Site blocked", css, body)
}

// importMapJSON is the subset of the import map format the engine reads.
// Scoped maps are accepted but ignored: they only change which copy of a
// package a nested dependency gets, which a terminal browser does not need.
type importMapJSON struct {
	Imports map[string]string            `json:"imports"`
	Scopes  map[string]map[string]string `json:"scopes"`
}

// collectImportMap returns the imports of the document's first valid
// <script type="importmap">. Later maps are ignored, as in browsers, and a
// malformed map leaves bare specifiers unresolved instead of failing the page.
func collectImportMap(root *Node) map[string]string {
	if root == nil {
		return nil
	}
	for _, n := range root.FindAll(func(n *Node) bool { return n.Tag == "script" }) {
		if !strings.EqualFold(strings.TrimSpace(n.Attr("type")), "importmap") {
			continue
		}
		var m importMapJSON
		if err := json.Unmarshal([]byte(n.TextContent()), &m); err != nil {
			continue
		}
		if len(m.Imports) > 0 {
			return m.Imports
		}
	}
	return nil
}

// collectScripts gathers inline and external JavaScript in document order.
// ES module scripts are collected too and marked as modules; every other
// script type is skipped.
func (l *Loader) collectScripts(ctx context.Context, root *Node, base *url.URL) []Script {
	ctx = withRequestKind(ctx, "js")
	nodes := root.FindAll(func(n *Node) bool {
		if n.Tag != "script" {
			return false
		}
		// A nomodule script is the no-module fallback. The engine now executes
		// modules, so running both bundles would double-execute the page.
		if n.HasAttr("nomodule") {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(n.Attr("type"))) {
		case "", "text/javascript", "application/javascript", "text/ecmascript", "application/ecmascript", "module":
			return true
		}
		return false
	})
	var out []Script
	total := 0
	for _, n := range nodes {
		module := strings.EqualFold(strings.TrimSpace(n.Attr("type")), "module")
		src := strings.TrimSpace(n.Attr("src"))
		if src == "" {
			code := n.TextContent()
			if strings.TrimSpace(code) == "" {
				continue
			}
			total += len(code)
			name := "inline script"
			if module {
				// An inline module is resolved against the document URL and has
				// no source URL of its own.
				name = ""
			}
			out = append(out, Script{Code: code, Src: name, Node: n, Module: module})
			if total > maxResourceSize {
				break
			}
			continue
		}
		u, err := base.Parse(src)
		if err != nil {
			continue
		}
		data, ctype, _, status, err := l.fetchCtx(ctx, u)
		if err != nil || status < 200 || status >= 400 {
			continue
		}
		// MOB³ applies strict MIME checking to external scripts: a response
		// typed as HTML, JSON, text/plain, an image or any other non-script
		// type is not executed. A missing Content-Type stays lenient.
		if !isJavaScriptMIME(ctype) {
			continue
		}
		code := decodeBody(data, ctype)
		total += len(code)
		out = append(out, Script{Code: code, Src: u.String(), Node: n, Module: module})
		if total > maxResourceSize {
			break
		}
	}
	return out
}

// aboutPageName returns the page part of an about: URL, ignoring a query.
func aboutPageName(u *url.URL) string {
	if u == nil || !strings.EqualFold(u.Scheme, "about") {
		return ""
	}
	name := u.Opaque
	if name == "" {
		name = strings.TrimPrefix(u.Path, "/")
	}
	return strings.ToLower(name)
}

func (l *Loader) loadAbout(ctx context.Context, u *url.URL) (*Page, error) {
	var doc string
	home := false
	switch aboutPageName(u) {
	case "blank":
		doc = "<html><body></body></html>"
	case "help":
		doc = helpPageHTML()
	case "history":
		var urls []string
		current := -1
		if l.historyFn != nil {
			urls, current = l.historyFn()
		}
		doc = historyPageHTML(urls, current)
	case "bookmarks":
		doc = bookmarksPageHTML(l.bookmarks)
	default:
		doc = startPageHTML
		home = true
	}
	root := ParseHTML(doc)
	if !l.noScripts {
		removeNoscript(root)
	}
	title := "MOB³"
	switch aboutPageName(u) {
	case "help":
		title = "Help — MOB³"
	case "history":
		title = "History — MOB³"
	case "bookmarks":
		title = "Bookmarks — MOB³"
	}
	page := &Page{URL: u, Base: u, Root: root, Title: title, RawHTML: doc, Status: 200}
	page.Author = l.collectStylesheets(ctx, root, page.Base)
	if home {
		// The start page uses the reader-mode stylesheet exactly, so it gets the
		// same narrow article column and typography as an extracted article.
		page.Author = append(page.Author, readerRules...)
	}
	l.loadInlineSVGs(root)
	return page, nil
}

func (l *Loader) fetchCtx(ctx context.Context, u *url.URL) (data []byte, ctype string, final *url.URL, status int, err error) {
	return l.fetchRequest(ctx, http.MethodGet, u, nil, "")
}

// recordBlocked adds a blocked request to the console request log.
func (l *Loader) recordBlocked(gen int, method string, u *url.URL, kind string, err error) {
	if kind == "" || u == nil {
		return
	}
	reqID, _ := l.startRequest(gen, method, u.String(), kind)
	l.finishRequest(reqID, gen, 0, 0, 0, err)
}

func (l *Loader) fetchRequest(ctx context.Context, method string, u *url.URL, body []byte, contentType string) (data []byte, ctype string, final *url.URL, status int, err error) {
	if err := ctx.Err(); err != nil {
		return nil, "", u, 0, err
	}
	if space := pageSpaceFrom(ctx); space != nil {
		// The document request records whether its own connection was local,
		// so an intranet page is not treated like a public one.
		ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
			GotConn: func(info httptrace.GotConnInfo) {
				if info.Conn != nil && remoteAddrLocal(info.Conn.RemoteAddr()) {
					space.local.Store(true)
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
	meta := requestMetaFrom(ctx)
	gen := meta.gen
	if gen == 0 {
		gen = l.currentPageGeneration()
	}
	if meta.blockLocal && isLocalURL(u) {
		// A public document may not address a literal local host. Names that
		// only resolve locally are caught by localNetworkControl at dial time.
		l.recordBlocked(gen, method, u, meta.kind, errLocalNetworkBlocked)
		return nil, "", u, 0, errLocalNetworkBlocked
	}
	if u, err = applyMixedContent(ctx, u); err != nil {
		l.recordBlocked(gen, method, u, meta.kind, err)
		return nil, "", u, 0, err
	}
	cacheable := method == http.MethodGet && len(body) == 0 &&
		(u.Scheme == "http" || u.Scheme == "https") &&
		(meta.kind == "css" || meta.kind == "js")
	if cacheable {
		if cached, ct, final, cachedLocal, ok := l.cache.getBytes(u.String()); ok && !(meta.blockLocal && cachedLocal) {
			l.recordCacheHit(gen, method, u.String(), meta.kind, len(cached))
			finalURL := u
			if parsed, err := url.Parse(final); err == nil && parsed.IsAbs() {
				finalURL = parsed
			}
			return cached, ct, finalURL, 200, nil
		}
	}
	var reqID int
	start := time.Now()
	if meta.kind != "" {
		reqID, _ = l.startRequest(gen, method, u.String(), meta.kind)
	}
	defer func() {
		if reqID != 0 {
			l.finishRequest(reqID, gen, status, len(data), time.Since(start), err)
		}
	}()
	switch u.Scheme {
	case "file":
		return nil, "", u, 0, errors.New("local files are not supported")
	case "data":
		b, ct, e := parseDataURI(u.String())
		return b, ct, u, 200, e
	case "http", "https":
		var reader io.Reader
		if len(body) > 0 {
			reader = bytes.NewReader(body)
		}
		reqCtx := withRequestTarget(ctx, u)
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
		req.Header.Set("User-Agent", l.userAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,text/css;q=0.9,image/*;q=0.8,*/*;q=0.5")
		req.Header.Set("Accept-Language", "en")
		client := l.client
		if meta.pageSecure && meta.kind != "" && meta.kind != "doc" {
			// A secure subresource may not redirect back down to HTTP.
			c := *l.client
			c.CheckRedirect = func(next *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("too many redirects")
				}
				if next.URL.Scheme == "http" {
					return errMixedContentBlocked
				}
				return nil
			}
			client = &c
		}
		resp, e := client.Do(req)
		if e != nil {
			if isLocalNetworkError(e) {
				e = errLocalNetworkBlocked
			}
			if isMixedContentError(e) {
				e = errMixedContentBlocked
			}
			return nil, "", u, 0, e
		}
		defer resp.Body.Close()
		if enc := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))); enc != "" && enc != "identity" && !resp.Uncompressed {
			// Go's transport decodes gzip transparently and removes the header.
			// Anything still marked (brotli, zstd, …) would be parsed as raw
			// bytes and produce meaningless output, so report it instead.
			return nil, "", u, resp.StatusCode, errors.New("unsupported Content-Encoding: " + enc)
		}
		b, e := io.ReadAll(io.LimitReader(resp.Body, maxResourceSize))
		if e != nil {
			return nil, "", u, resp.StatusCode, e
		}
		ctype := resp.Header.Get("Content-Type")
		if cacheable && resp.StatusCode == 200 && cacheControlAllows(resp.Header.Get("Cache-Control")) {
			l.cache.putBytes(u.String(), b, ctype, resp.Request.URL.String(), !meta.blockLocal)
		}
		return b, ctype, resp.Request.URL, resp.StatusCode, nil
	}
	return nil, "", u, 0, errors.New("unsupported URL scheme: " + u.Scheme)
}

// jsMIMEEssences lists the response types a classic script may execute as.
// Missing Content-Type is handled separately and stays lenient, matching the
// way file and data resources without metadata are treated.
var jsMIMEEssences = map[string]bool{
	"application/ecmascript":   true,
	"application/javascript":   true,
	"application/x-ecmascript": true,
	"application/x-javascript": true,
	"text/ecmascript":          true,
	"text/javascript":          true,
	"text/javascript1.0":       true,
	"text/javascript1.1":       true,
	"text/javascript1.2":       true,
	"text/javascript1.3":       true,
	"text/javascript1.4":       true,
	"text/javascript1.5":       true,
	"text/jscript":             true,
	"text/livescript":          true,
	"text/x-ecmascript":        true,
	"text/x-javascript":        true,
}

// mimeEssence returns the lowercased type/subtype of a Content-Type header,
// dropping parameters. It returns "" when no usable type is present.
func mimeEssence(contentType string) string {
	essence := ""
	for _, part := range strings.Split(contentType, ",") {
		part = strings.TrimSpace(part)
		if i := strings.IndexByte(part, ';'); i >= 0 {
			part = strings.TrimSpace(part[:i])
		}
		if strings.Contains(part, "/") {
			essence = strings.ToLower(part)
		}
	}
	return essence
}

// isJavaScriptMIME reports whether a script response may execute. An empty
// (missing) type is allowed so data resources without metadata keep working;
// any explicit non-JavaScript type is rejected.
func isJavaScriptMIME(contentType string) bool {
	essence := mimeEssence(contentType)
	return essence == "" || jsMIMEEssences[essence]
}

func parseDataURI(s string) ([]byte, string, error) {
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

func decodeBody(data []byte, ctype string) string {
	charset := ""
	if _, params, err := mime.ParseMediaType(ctype); err == nil {
		charset = strings.ToLower(params["charset"])
	}
	if charset == "" {
		head := strings.ToLower(string(data[:minInt(len(data), 2048)]))
		if i := strings.Index(head, "charset="); i >= 0 {
			rest := head[i+len("charset="):]
			rest = strings.TrimLeft(rest, `"' `)
			end := 0
			for end < len(rest) && (rest[end] == '-' || rest[end] >= 'a' && rest[end] <= 'z' || rest[end] >= '0' && rest[end] <= '9' || rest[end] == '_') {
				end++
			}
			charset = rest[:end]
		}
	}
	if cm, ok := lookupCharset(charset); ok {
		if decoded, err := cm.NewDecoder().Bytes(data); err == nil {
			return string(decoded)
		}
	}
	return strings.ToValidUTF8(string(data), "\uFFFD")
}

// singleByteCharsets maps legacy single-byte charset labels to their decoder.
// Labels are matched after lowercasing and treating '_' like '-'. Anything not
// listed here is read as UTF-8.
var singleByteCharsets = map[string]*charmap.Charmap{
	"iso-8859-1":   charmap.ISO8859_1,
	"iso8859-1":    charmap.ISO8859_1,
	"latin1":       charmap.ISO8859_1,
	"latin-1":      charmap.ISO8859_1,
	"l1":           charmap.ISO8859_1,
	"cp819":        charmap.ISO8859_1,
	"windows-1252": charmap.Windows1252,
	"cp1252":       charmap.Windows1252,
	"iso-8859-2":   charmap.ISO8859_2,
	"iso8859-2":    charmap.ISO8859_2,
	"latin2":       charmap.ISO8859_2,
	"latin-2":      charmap.ISO8859_2,
	"l2":           charmap.ISO8859_2,
	"windows-1250": charmap.Windows1250,
	"cp1250":       charmap.Windows1250,
	"iso-8859-3":   charmap.ISO8859_3,
	"latin3":       charmap.ISO8859_3,
	"iso-8859-4":   charmap.ISO8859_4,
	"latin4":       charmap.ISO8859_4,
	"iso-8859-5":   charmap.ISO8859_5,
	"cyrillic":     charmap.ISO8859_5,
	"windows-1251": charmap.Windows1251,
	"cp1251":       charmap.Windows1251,
	"iso-8859-7":   charmap.ISO8859_7,
	"greek":        charmap.ISO8859_7,
	"greek8":       charmap.ISO8859_7,
	"windows-1253": charmap.Windows1253,
	"cp1253":       charmap.Windows1253,
	"iso-8859-9":   charmap.ISO8859_9,
	"latin5":       charmap.ISO8859_9,
	"windows-1254": charmap.Windows1254,
	"cp1254":       charmap.Windows1254,
	"iso-8859-10":  charmap.ISO8859_10,
	"latin6":       charmap.ISO8859_10,
	"iso-8859-13":  charmap.ISO8859_13,
	"latin7":       charmap.ISO8859_13,
	"windows-1257": charmap.Windows1257,
	"cp1257":       charmap.Windows1257,
	"iso-8859-14":  charmap.ISO8859_14,
	"latin8":       charmap.ISO8859_14,
	"iso-8859-15":  charmap.ISO8859_15,
	"latin9":       charmap.ISO8859_15,
	"latin-9":      charmap.ISO8859_15,
	"iso-8859-16":  charmap.ISO8859_16,
	"latin10":      charmap.ISO8859_16,
}

// lookupCharset resolves a charset label to a single-byte decoder.
func lookupCharset(label string) (*charmap.Charmap, bool) {
	label = strings.ToLower(strings.TrimSpace(label))
	label = strings.ReplaceAll(label, "_", "-")
	cm, ok := singleByteCharsets[label]
	return cm, ok
}

func (l *Loader) collectStylesheets(ctx context.Context, root *Node, base *url.URL) []Rule {
	ctx = withRequestKind(ctx, "css")
	var out []Rule
	order := 0
	layers := newLayerRegistry()
	// Sheets are gathered first so every @layer name can be ranked in document
	// order before any rule is assigned to a layer.
	type sheetEntry struct {
		src   string
		media string
	}
	var sheets []sheetEntry
	add := func(rules []Rule, media string) {
		for i := range rules {
			if media != "" {
				rules[i].Media = combineMedia(media, rules[i].Media)
			}
			rules[i].Order = order
			order++
			out = append(out, rules[i])
		}
	}
	seen := make(map[string]bool)
	imported := 0
	const maxImportDepth = 8
	const maxImportedSheets = 40
	var addSheet func(src string, sheetURL *url.URL, depth int, media string)
	addSheet = func(src string, sheetURL *url.URL, depth int, media string) {
		if depth < maxImportDepth && imported < maxImportedSheets {
			for _, imp := range cssImports(src) {
				if imported >= maxImportedSheets {
					break
				}
				ref := sheetURL
				if ref == nil {
					ref = base
				}
				if ref == nil {
					continue
				}
				u, err := ref.Parse(imp.href)
				if err != nil {
					continue
				}
				key := u.String()
				if seen[key] {
					continue
				}
				seen[key] = true
				imported++
				data, _, final, status, err := l.fetchCtx(ctx, u)
				if err != nil || status < 200 || status >= 400 || final == nil {
					continue
				}
				addSheet(string(data), final, depth+1, combineMedia(media, imp.media))
			}
		}
		sheets = append(sheets, sheetEntry{src: src, media: media})
	}
	nodes := root.FindAll(func(n *Node) bool {
		if n.Tag == "style" {
			return true
		}
		if n.Tag == "link" {
			rel := strings.ToLower(n.Attr("rel"))
			return strings.Contains(rel, "stylesheet") && n.Attr("href") != ""
		}
		return false
	})
	for _, n := range nodes {
		media := strings.ToLower(n.Attr("media"))
		if strings.Contains(media, "print") && !strings.Contains(media, "screen") && !strings.Contains(media, "all") {
			continue
		}
		if n.Tag == "style" {
			addSheet(n.TextContent(), base, 0, media)
			continue
		}
		href := n.Attr("href")
		u, err := base.Parse(href)
		if err != nil {
			continue
		}
		key := u.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		data, _, final, status, err := l.fetchCtx(ctx, u)
		if err != nil || status < 200 || status >= 400 {
			continue
		}
		if final == nil {
			final = u
		}
		addSheet(string(data), final, 0, media)
	}
	for _, sh := range sheets {
		declareCSSLayers(sh.src, layers)
	}
	for _, sh := range sheets {
		add(parseStylesheetWithLayers(ctx, sh.src, layers), sh.media)
	}
	return out
}

// combineMedia joins an @import/link media condition with a rule's own @media
// condition. A comma-listed import condition is approximated.
func combineMedia(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " and " + b
}

// srcsetCandidate is one entry from a srcset attribute.
type srcsetCandidate struct {
	url string
	w   int
	x   float64
}

// imageSource chooses the URL to download for an img, honouring a surrounding
// picture's matching source elements, srcset width/density candidates, and
// common data-src lazy-loading fallbacks when no standard source is present.
func imageSource(n *Node, viewportPx int) string {
	if n == nil {
		return ""
	}
	if n.Tag != "img" {
		// A non-image element can carry a CSS background image. The style is
		// only available after the first layout pass, so these are picked up by
		// the deferred image batch.
		if n.Style == nil || n.Style.BackgroundImage == "" {
			return ""
		}
		return n.Style.BackgroundImage
	}
	preferred := viewportPx * 2 / 5
	if preferred < 160 {
		preferred = 160
	}
	if preferred > 1024 {
		preferred = 1024
	}
	if pic := pictureParent(n); pic != nil {
		for _, c := range pic.Children {
			if c.Type != ElementNode || c.Tag != "source" || !imageTypeAllowed(c.Attr("type")) {
				continue
			}
			if media := c.Attr("media"); media != "" && !mediaMatches(media, viewportPx, false) {
				continue
			}
			if src := pickSrcset(c.Attr("srcset"), preferred); src != "" {
				return src
			}
		}
	}
	if src := pickSrcset(n.Attr("srcset"), preferred); src != "" {
		return src
	}
	if src := n.Attr("src"); src != "" {
		return src
	}
	for _, attr := range []string{"data-srcset", "data-lazy-srcset"} {
		if src := pickSrcset(n.Attr(attr), preferred); src != "" {
			return src
		}
	}
	for _, attr := range []string{"data-src", "data-lazy-src", "data-original", "data-default-src"} {
		if src := n.Attr(attr); src != "" {
			return src
		}
	}
	return ""
}

func pictureParent(n *Node) *Node {
	if n == nil || n.Parent == nil || n.Parent.Tag != "picture" {
		return nil
	}
	return n.Parent
}

func imageTypeAllowed(typ string) bool {
	typ = strings.ToLower(strings.TrimSpace(typ))
	if typ == "" {
		return true
	}
	for _, bad := range []string{"avif", "jxl", "heic", "heif"} {
		if strings.Contains(typ, bad) {
			return false
		}
	}
	return true
}

// pickSrcset selects the smallest width candidate at least as wide as
// preferred, or the widest available; for density descriptors it picks the
// candidate closest to 1x.
func pickSrcset(s string, preferred int) string {
	candidates := parseSrcset(s)
	if len(candidates) == 0 {
		return ""
	}
	bestW, bestD := -1, ""
	for _, c := range candidates {
		if c.w <= 0 {
			continue
		}
		if c.w >= preferred {
			if bestW < 0 || c.w < bestW {
				bestW, bestD = c.w, c.url
			}
		}
	}
	if bestD != "" {
		return bestD
	}
	for _, c := range candidates {
		if c.w > bestW {
			bestW, bestD = c.w, c.url
		}
	}
	if bestD != "" {
		return bestD
	}
	bestDiff, bestX := -1.0, ""
	for _, c := range candidates {
		diff := c.x - 1
		if diff < 0 {
			diff = -diff
		}
		if bestX == "" || diff < bestDiff {
			bestDiff, bestX = diff, c.url
		}
	}
	return bestX
}

// isJSSpace reports the ASCII whitespace bytes a srcset candidate splits on.
func isJSSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

func parseSrcset(s string) []srcsetCandidate {
	var out []srcsetCandidate
	for i := 0; i < len(s); {
		for i < len(s) && (isJSSpace(s[i]) || s[i] == ',') {
			i++
		}
		start := i
		for i < len(s) && !isJSSpace(s[i]) && s[i] != ',' {
			i++
		}
		url := s[start:i]
		for i < len(s) && isJSSpace(s[i]) {
			i++
		}
		descStart := i
		for i < len(s) && s[i] != ',' {
			i++
		}
		desc := strings.TrimSpace(s[descStart:i])
		if url == "" {
			continue
		}
		c := srcsetCandidate{url: url}
		if strings.HasSuffix(desc, "w") {
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(desc, "w"))); err == nil {
				c.w = n
			}
		} else if strings.HasSuffix(desc, "x") {
			if f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(desc, "x")), 64); err == nil {
				c.x = f
			}
		}
		out = append(out, c)
		if i < len(s) && s[i] == ',' {
			i++
		}
	}
	return out
}

// loadImages downloads images referenced by the document. It reports whether
// any image gained a new source, so a post-script refresh can decide whether a
// rebuild is needed.
// imageTask is one image to fetch without touching the DOM, so the work can run
// on a background goroutine.
type imageTask struct {
	node *Node
	src  string
	url  *url.URL
}

// imageLoadResult is one decoded image (or failure) returned to the main loop.
type imageLoadResult struct {
	Node   *Node
	Src    string
	Img    image.Image
	Failed bool
}

// imageTasks collects images that still need loading. It must run on the main
// layout goroutine because it reads node attributes.
func (l *Loader) imageTasks(root *Node, base *url.URL) []imageTask {
	if l.noImages || root == nil || base == nil {
		return nil
	}
	var tasks []imageTask
	for _, n := range root.FindAll(func(n *Node) bool {
		if n.Tag == "img" {
			return true
		}
		return n.Style != nil && n.Style.BackgroundImage != ""
	}) {
		src := imageSource(n, l.imageViewportPx)
		if src == "" {
			continue
		}
		if n.ImgSrc == src && (n.Img != nil || n.ImgFailed) {
			continue
		}
		t := imageTask{node: n, src: src}
		if u, err := base.Parse(src); err == nil {
			t.url = u
		}
		tasks = append(tasks, t)
	}
	return tasks
}

// fitImage downscales a decoded image so neither side exceeds the loader's
// pixel cap, preserving aspect ratio. Images already inside the cap are
// returned unchanged, so only oversized photos pay for a resample.
func (l *Loader) fitImage(img image.Image) image.Image {
	if img == nil || (l.maxImageW <= 0 && l.maxImageH <= 0) {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 {
		return img
	}
	nw, nh := w, h
	if l.maxImageW > 0 && nw > l.maxImageW {
		f := float64(l.maxImageW) / float64(nw)
		nw = l.maxImageW
		nh = int(math.Round(float64(nh) * f))
	}
	if l.maxImageH > 0 && nh > l.maxImageH {
		f := float64(l.maxImageH) / float64(nh)
		nh = l.maxImageH
		nw = int(math.Round(float64(nw) * f))
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	if nw == w && nh == h {
		return img
	}
	return scaleImage(img, nw, nh)
}

// recoverImage runs a decoder and converts a panic into an error. Raster and
// SVG decoders parse untrusted bytes, and a panic in one of them would take
// down the whole process; the browser shows a broken image instead.
func recoverImage(fn func() (image.Image, error)) (img image.Image, err error) {
	defer func() {
		if r := recover(); r != nil {
			img, err = nil, fmt.Errorf("image decode failed: %v", r)
		}
	}()
	return fn()
}

func safeDecodeConfig(data []byte) (cfg image.Config, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("image config decode failed: %v", r)
		}
	}()
	cfg, _, err = image.DecodeConfig(bytes.NewReader(data))
	return cfg, err
}

func safeDecodeImage(data []byte) (image.Image, error) {
	return recoverImage(func() (image.Image, error) {
		img, _, err := image.Decode(bytes.NewReader(data))
		return img, err
	})
}

// decodeMedia decodes one image response: SVG through the vector rasterizer,
// everything else through the raster decoders with the megapixel limit applied.
// Both paths contain panics.
func decodeMedia(ctx context.Context, ctype string, data []byte) (image.Image, error) {
	if isSVG(ctype, data) {
		return decodeSVG(data)
	}
	cfg, err := safeDecodeConfig(data)
	if err != nil || !imageConfigOK(cfg) {
		return nil, fmt.Errorf("unsupported image")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return safeDecodeImage(data)
}

// imageDecodeWeight estimates the RGBA bytes a decode will hold so the budget
// can admit several small images but only one or two large ones.
func imageDecodeWeight(ctype string, data []byte) int64 {
	if isSVG(ctype, data) {
		return int64(maxSVGDim) * int64(maxSVGDim) * 4
	}
	cfg, err := safeDecodeConfig(data)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return 1 << 20
	}
	n := int64(cfg.Width) * int64(cfg.Height) * 4
	max := int64(maxImagePixels) * 4
	if n > max {
		n = max
	}
	if n < 1 {
		n = 1
	}
	return n
}

// decodeAndFit decodes one image under the in-flight byte budget and scales it
// to the terminal box before releasing the reservation.
func (l *Loader) decodeAndFit(ctx context.Context, ctype string, data []byte) (image.Image, error) {
	weight := imageDecodeWeight(ctype, data)
	if l.imageBudget != nil {
		admitted, ok := l.imageBudget.acquire(ctx, weight)
		if !ok {
			return nil, ctx.Err()
		}
		defer l.imageBudget.release(admitted)
	}
	img, err := decodeMedia(ctx, ctype, data)
	if err != nil {
		return nil, err
	}
	return l.fitImage(img), nil
}

// fetchImageResult downloads and decodes one image, using the session cache.
func (l *Loader) fetchImageResult(ctx context.Context, u *url.URL, gen int) (image.Image, bool) {
	u, err := applyMixedContent(ctx, u)
	if err != nil {
		l.recordBlocked(gen, "GET", u, "img", err)
		return nil, false
	}
	key := u.String()
	meta := requestMetaFrom(ctx)
	if cached, rawSize, cachedLocal, ok := l.cache.getImage(key); ok && !(meta.blockLocal && cachedLocal) {
		l.recordCacheHit(gen, "GET", key, "img", rawSize)
		return cached, true
	}
	data, ctype, _, status, err := l.fetchCtx(ctx, u)
	if err != nil || status < 200 || status >= 400 {
		return nil, false
	}
	img, err := l.decodeAndFit(ctx, ctype, data)
	if err != nil {
		return nil, false
	}
	l.cache.putImage(key, img, len(data), !meta.blockLocal)
	return img, true
}

// imageResultStream fetches the queued images concurrently and streams decoded
// results as they finish. It never writes to the DOM, so it can run off the
// main loop; the caller applies results when it owns the event loop.
func (l *Loader) imageResultStream(ctx context.Context, tasks []imageTask) <-chan imageLoadResult {
	out := make(chan imageLoadResult)
	if len(tasks) == 0 {
		close(out)
		return out
	}
	ctx = withRequestKind(ctx, "img")
	gen := requestMetaFrom(ctx).gen
	if gen == 0 {
		gen = l.currentPageGeneration()
	}
	go func() {
		defer close(out)
		// Six workers pull tasks in priority order. A worker takes the next
		// queued image as soon as its current download finishes.
		jobs := make(chan imageTask)
		go func() {
			defer close(jobs)
			for _, t := range tasks {
				select {
				case jobs <- t:
				case <-ctx.Done():
					return
				}
			}
		}()
		var wg sync.WaitGroup
		for w := 0; w < 6; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for t := range jobs {
					if t.url == nil {
						select {
						case out <- imageLoadResult{Node: t.node, Src: t.src, Failed: true}:
						case <-ctx.Done():
							return
						}
						continue
					}
					img, ok := l.fetchImageResult(ctx, t.url, gen)
					select {
					case out <- imageLoadResult{Node: t.node, Src: t.src, Img: img, Failed: !ok}:
					case <-ctx.Done():
						return
					}
				}
			}()
		}
		wg.Wait()
	}()
	return out
}

// loadImageTaskResults collects the stream for callers that want one batch.
func (l *Loader) loadImageTaskResults(ctx context.Context, tasks []imageTask) []imageLoadResult {
	var results []imageLoadResult
	for r := range l.imageResultStream(ctx, tasks) {
		results = append(results, r)
	}
	return results
}

func (l *Loader) loadImages(ctx context.Context, root *Node, base *url.URL) bool {
	if l.noImages {
		return false
	}
	ctx = withRequestKind(ctx, "img")
	imgs := root.FindAll(func(n *Node) bool {
		if n.Tag == "img" {
			return true
		}
		return n.Style != nil && n.Style.BackgroundImage != ""
	})
	if len(imgs) == 0 {
		return false
	}
	meta := requestMetaFrom(ctx)
	gen := meta.gen
	if gen == 0 {
		gen = l.currentPageGeneration()
	}
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	changed := false
	for _, n := range imgs {
		src := imageSource(n, l.imageViewportPx)
		if src == "" {
			continue
		}
		if n.ImgSrc == src && (n.Img != nil || n.ImgFailed) {
			continue
		}
		changed = true
		n.ImgSrc = src
		n.Img = nil
		n.ImgFailed = false
		u, err := base.Parse(src)
		if err != nil {
			n.ImgFailed = true
			continue
		}
		u, mcErr := applyMixedContent(ctx, u)
		if mcErr != nil {
			l.recordBlocked(gen, "GET", u, "img", mcErr)
			n.ImgFailed = true
			continue
		}
		key := u.String()
		if cached, rawSize, cachedLocal, ok := l.cache.getImage(key); ok && !(meta.blockLocal && cachedLocal) {
			l.recordCacheHit(gen, "GET", key, "img", rawSize)
			n.Img = cached
			continue
		}
		wg.Add(1)
		go func(node *Node, u *url.URL) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				node.ImgFailed = true
				return
			}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				node.ImgFailed = true
				return
			}
			data, ctype, _, status, err := l.fetchCtx(ctx, u)
			if err != nil || status < 200 || status >= 400 {
				node.ImgFailed = true
				return
			}
			img, err := l.decodeAndFit(ctx, ctype, data)
			if err != nil {
				node.ImgFailed = true
				return
			}
			node.Img = img
			l.cache.putImage(key, img, len(data), !meta.blockLocal)
		}(n, u)
	}
	wg.Wait()
	return changed
}

const helpPageBody = `<h1>MOB³ Help</h1>
<p class="muted">Version <b>{{VERSION}}</b> · a small terminal browser. Keys are vim-first.</p>
<p class="muted">MOB³ keeps history, cookies, storage and caches in memory only.
Nothing is written to disk.</p>

<h2>Command line</h2>
<p><b>mob3 [flags] [url|file]</b> opens the start page with no arguments.</p>
<table>
<tr><th>Option</th><th>Description</th></tr>
<tr><td><b>-dump</b></td><td>render the page to stdout and exit</td></tr>
<tr><td><b>-png</b></td><td>write a PNG screenshot of the dump to stdout (implies -dump)</td></tr>
<tr><td><b>-width</b> int</td><td>layout width in columns (default: terminal width or 100)</td></tr>
<tr><td><b>-height</b> int</td><td>layout viewport height in rows and output limit; 0 uses the full page (dump/PNG/MCP)</td></tr>
<tr><td><b>-pdf-page</b> string</td><td>PDF page for dump/PNG: auto, all, or a page number; -png defaults to page 1 (default "auto")</td></tr>
<tr><td><b>-graphics</b> string</td><td>image mode: none, auto, kitty, iterm or blocks (default "none")</td></tr>
<tr><td><b>-g</b> string</td><td>alias for -graphics</td></tr>
<tr><td><b>-no-js</b></td><td>disable page JavaScript</td></tr>
<tr><td><b>-no-pdf</b></td><td>disable the built-in PDF text viewer and show the download prompt instead</td></tr>
<tr><td><b>-reader</b></td><td>start in reader mode</td></tr>
<tr><td><b>-dark</b></td><td>start in dark mode</td></tr>
<tr><td><b>-ignore-blocks</b></td><td>load anti-bot/CAPTCHA pages instead of the blocked page</td></tr>
<tr><td><b>-bookmarks</b> string</td><td>import a Netscape bookmark HTML file</td></tr>
<tr><td><b>-home</b> string</td><td>home URL opened at startup and by the Home button (default about:home)</td></tr>
<tr><td><b>-mem-limit</b> int</td><td>soft process memory limit in MB; also sets the 25% JS, 12% image, 3% SVG, 3% cache and 12% history budgets, 0 leaves it unlimited and honors GOMEMLIMIT (default 0)</td></tr>
<tr><td><b>-layout-budget</b> string</td><td>style/layout safety limits: low, standard, high or none (default "standard")</td></tr>
<tr><td><b>-zoom</b> string</td><td>layout zoom percent from 25 to 200, with or without % (default "100"); + and - step the presets</td></tr>
<tr><td><b>-text-scale</b> string</td><td>kitty scaled text: auto or off (default "auto")</td></tr>
<tr><td><b>-color</b></td><td>force ANSI color when dumping</td></tr>
<tr><td><b>-no-color</b></td><td>disable ANSI colors in dump mode</td></tr>
<tr><td><b>-ua</b> string</td><td>custom User-Agent (default MOB3/1.0 (+terminal; +https://mob3.net))</td></tr>
<tr><td><b>-search</b> string</td><td>search URL template with %s (default DuckDuckGo; empty disables)</td></tr>
<tr><td><b>-certs</b> string</td><td>TLS trust store: system, embedded, or a PEM bundle file (default "system")</td></tr>
<tr><td><b>-mcp</b></td><td>run the MCP automation server on stdin/stdout</td></tr>
<tr><td><b>-version</b></td><td>print the version and exit</td></tr>
<tr><td><b>-h</b>, <b>-help</b></td><td>print the flags and exit</td></tr>
</table>
<p class="muted">The start page's search box works like the <b>o</b> prompt.
The URL prompt recalls earlier URLs and searches with <b>Up</b>/<b>Down</b> or
<b>Ctrl-P</b>/<b>Ctrl-N</b>.</p>

<h2>Keyboard</h2>
<table>
<tr><th>Key</th><th>Action</th></tr>
<tr><td><b>j</b> / <b>k</b></td><td>scroll down / up one line</td></tr>
<tr><td><b>Space</b> / <b>b</b></td><td>scroll down / up one screen</td></tr>
<tr><td><b>Ctrl-D</b> / <b>Ctrl-U</b></td><td>scroll half a screen</td></tr>
<tr><td><b>g</b> / <b>G</b></td><td>top / bottom of page</td></tr>
<tr><td><b>Tab</b> / <b>Shift-Tab</b></td><td>next / previous link or form control</td></tr>
<tr><td><b>Enter</b></td><td>follow a link, submit a field, or insert a textarea newline</td></tr>
<tr><td><b>Ctrl-O</b> / <b>[</b></td><td>back</td></tr>
<tr><td><b>Ctrl-I</b> / <b>]</b></td><td>forward; Ctrl-I needs kitty key reporting, legacy terminals send Tab</td></tr>
<tr><td><b>r</b></td><td>reload</td></tr>
<tr><td><b>R</b></td><td>toggle reader mode</td></tr>
<tr><td><b>D</b></td><td>toggle dark mode</td></tr>
<tr><td><b>J</b></td><td>toggle JavaScript and reload without or with scripts</td></tr>
<tr><td><b>i</b></td><td>cycle graphics: full graphics, blocks, none</td></tr>
<tr><td><b>+</b> / <b>-</b></td><td>zoom the page in / out (25% to 200%); fixed pixel layout, spacing, images and large kitty headings grow, body text keeps its size</td></tr>
<tr><td><b>0</b></td><td>reset zoom to 100%</td></tr>
<tr><td><b>Z</b></td><td>open the zoom window to choose a preset with the mouse or arrow keys (the badge at the right end of the URL row does the same with the mouse)</td></tr>
<tr><td><b>X</b></td><td>open the view options window (Fixed elements, Sticky elements, Background images) and toggle the selected switch</td></tr>
<tr><td><b>o</b> / <b>Ctrl-L</b> / <b>:</b></td><td>open a URL prompt; <b>:</b> also accepts <b>q</b> to quit immediately</td></tr>
<tr><td><b>/</b></td><td>find in the page, or in the console HTML tab</td></tr>
<tr><td><b>f</b></td><td>highlight letters in the visible links, then type them to follow a link</td></tr>
<tr><td><b>F</b></td><td>same as <b>f</b>, but show the destination and wait for <b>Enter</b>/<b>y</b></td></tr>
<tr><td><b>Enter</b> / <b>n</b></td><td>close the find bar / next find match afterwards</td></tr>
<tr><td><b>Down</b> / <b>N</b></td><td>next match in the find bar / previous match after closing it</td></tr>
<tr><td><b>u</b></td><td>show the current URL</td></tr>
<tr><td><b>d</b></td><td>open the curl download popup for the current PDF (PDF viewer only; does nothing elsewhere)</td></tr>
<tr><td><b>y</b></td><td>copy selected text, the current URL when nothing is selected, the download curl command, or the active console section</td></tr>
<tr><td><b>Y</b></td><td>copy the whole rendered page as plain text, without colors or browser chrome, for pasting into a bug report; the first line carries URL, sizes, zoom and modes</td></tr>
<tr><td><b>Ctrl-Y</b></td><td>copy only the page rows visible at the current scroll, with the same metadata line</td></tr>
<tr><td><b>H</b></td><td>session history</td></tr>
<tr><td><b>E</b></td><td>open the full warning or error message that the footer truncated; the E in Error: is underlined</td></tr>
<tr><td><b>?</b></td><td>this help page</td></tr>
<tr><td><b>!</b></td><td>JavaScript console / errors</td></tr>
<tr><td><b>Esc</b></td><td>stop loading or leave the current mode</td></tr>
<tr><td><b>q</b> / <b>Ctrl-C</b></td><td>quit after confirmation</td></tr>
</table>
<p class="muted">Kitty, WezTerm, Ghostty and foot get enhanced key reporting automatically.</p>

<h2>Find</h2>
<p><b>/</b> opens the find bar. Typing searches case-insensitively; <b>Enter</b>
closes it and <b>n</b>/<b>N</b> move between matches. <b>Esc</b> closes it; a
second <b>Esc</b> clears the highlights. In the console HTML tab, <b>/</b>
searches the raw source. If the current match is inside a link, the footer
shows <b>(f → resolved URL)</b> and <b>f</b> follows it directly; clearing the
search restores normal link hints.</p>

<h2>Link hints</h2>
<p>Press <b>f</b> to follow a link: it highlights a letter from each visible
link, and typing it follows that link. Form controls are highlighted too:
choosing a text field focuses it, checkboxes and radios toggle, a select opens
its options, multi-select options toggle, and buttons activate. The chosen
control stays focused for follow-up keys. Links sharing a
first letter use two adjacent letters. <b>F</b> shows the
destination in yellow first and waits for <b>Enter</b>/<b>y</b>; <b>Esc</b> or
<b>n</b> cancels. <b>Esc</b> or scrolling dismisses the hints.</p>

<h2>Reader mode</h2>
<p><b>R</b> toggles a clean article view and stays on across pages. Start with
<b>-reader</b> to enable it at launch. The config page is unavailable while
reader mode is on.</p>

<h2>Dark mode</h2>
<p><b>D</b> toggles a dark palette and asks sites for
<b>prefers-color-scheme: dark</b>. Start with <b>-dark</b> to enable it at
launch.</p>

<h2>JavaScript</h2>
<p><b>J</b> or clicking the footer <b>JavaScript:on</b>/<b>JavaScript:off</b>
label toggles scripting for the rest of the session and reloads the page, so
the no-script fallback is shown immediately. Start with <b>-no-js</b> to begin
with scripting disabled. The current state stays visible in the footer;
<b>JavaScript:Error</b> means a script failed. A page's load-time scripts share
an 8-second budget and a 2,000-script cap; the running script is interrupted and
the rest are skipped with a notice in the <b>!</b> console when either is
reached. Press <b>Esc</b> or <b>Ctrl-C</b> to interrupt a script that is blocking
the terminal.</p>

<h2>History and bookmarks</h2>
<p>The <b>≡ History</b> button lists the session history; <b>#</b> links and
scroll positions are kept. Up to ten visited pages stay in an in-memory
back/forward cache, so <b>Back</b> and <b>Forward</b> restore a page, its
JavaScript state and your scroll position without reloading it. The
<b>★ Bookmarks</b> button appears with
<b>-bookmarks file.html</b>. Nothing is saved after MOB³ exits. Script and
meta-refresh redirect chains stop after 20 hops with
<b>Redirect loop stopped</b>, so a page that redirects to itself cannot loop
forever.</p>

<h2>Config</h2>
<p>The <b>⚙</b> icon opens <b>about:config</b>. Changes apply to this session
only; <b>startup</b> and <b>dump only</b> options are read-only.</p>

<h2>Mouse</h2>
<table>
<tr><th>Input</th><th>Action</th></tr>
<tr><td>hover a link</td><td>show its target in the footer</td></tr>
<tr><td>drag over page text</td><td>select it; <b>y</b> copies and clears it</td></tr>
<tr><td>click a link</td><td>follow it; a download link shows the curl popup, except a .pdf download, which opens in the viewer</td></tr>
<tr><td>click a text field</td><td>focus and edit it</td></tr>
<tr><td>click the URL bar</td><td>open the URL prompt</td></tr>
<tr><td>click outside the URL prompt</td><td>close it</td></tr>
<tr><td>click in the URL prompt</td><td>place the caret</td></tr>
<tr><td>click the zoom badge in the URL bar</td><td>open the zoom window and choose a preset with the mouse; the current level is always shown</td></tr>
<tr><td>Shift+drag over the URL</td><td>select it with the terminal</td></tr>
<tr><td>click &#8592; / &#8635; / ?</td><td>back / reload / help; while a page loads, &#8635; becomes &#215; and clicking it cancels the load</td></tr>
<tr><td>right click a link</td><td>copy its URL</td></tr>
<tr><td>side back/forward button</td><td>go back / forward</td></tr>
<tr><td>click the footer status message</td><td>open the full warning or error text, like <b>E</b></td></tr>
<tr><td>click the footer <b>Graphics:…</b> label</td><td>cycle graphics mode, like <b>i</b></td></tr>
<tr><td>click the footer <b>JavaScript:on</b>/<b>JavaScript:off</b> label</td><td>toggle JavaScript and reload the page, like <b>J</b></td></tr>
<tr><td>click the footer <b>Dark:on</b>/<b>Dark:off</b> label</td><td>toggle dark mode</td></tr>
<tr><td>click the footer <b>Reader:on</b>/<b>Reader:off</b> label</td><td>toggle reader mode</td></tr>
<tr><td>click the footer &gt;_ icon</td><td>open or close the JS console</td></tr>
<tr><td>click a console tab</td><td>switch console sections; <b>y</b> copies the active section</td></tr>
<tr><td>click the ⚙ icon</td><td>open the config page</td></tr>
<tr><td>click the × or the dimmed backdrop</td><td>close the modal</td></tr>
<tr><td>wheel</td><td>scroll</td></tr>
<tr><td>click/drag the right-edge scrollbar</td><td>jump through a long page</td></tr>
</table>

<h2>Downloads</h2>
<p>MOB³ never saves files. Download links and non-HTML responses open a popup
with a <b>curl</b> command; <b>y</b> copies it, and <b>Esc</b>/<b>Enter</b>/<b>q</b>
closes it.</p>

<h2>PDF documents</h2>
<p>A PDF opens in a built-in, text-only viewer: every page is extracted into
one continuous document where each page ends with its <b>Page n</b> caption above
an underscore rule. The reading column is centered, every page is padded to the
same height so it scrolls like a book, two-column prose is read column by
column, vertical gaps become blank lines, and runs larger than the body text
become 32px headlines (two rows, scaled glyphs in kitty). Columns keep their
alignment. The footer shows <b>Page n/N</b> with a clickable
<b>(d -&gt; Download name)</b> hint; <b>d</b> opens the same curl popup. Find,
selection, <b>-dump</b> and <b>-png</b> work as on any other page. Figures,
images and annotations are not rendered, scanned PDFs have no text to show,
and encrypted PDFs are refused. <b>-no-pdf</b> disables the viewer and shows
the download popup instead.</p>

<h2>Forms</h2>
<p>Click a field to edit, or use <b>Tab</b>/<b>Shift-Tab</b> to move through
links and form controls. <b>Ctrl-A</b> selects the whole value; typing,
<b>Backspace</b> or <b>Delete</b> replaces it, and moving the caret cancels the
selection. <b>Ctrl-U</b> clears the field, <b>Ctrl-K</b> deletes to the end,
and <b>Ctrl-B</b>/<b>Ctrl-F</b>, <b>Alt-B</b>/<b>Alt-F</b> and <b>Ctrl-W</b>
edit by word. <b>Enter</b> submits, <b>Esc</b> leaves, <b>Space</b>
or <b>Enter</b> activates a focused checkbox, radio, select, button or link,
and arrows change radio groups, selects and ranges (<b>h</b>/<b>l</b> and
<b>j</b>/<b>k</b> also work there). Checkboxes,
radios, selects, date/time/month/week, range and color fields work. GET and
urlencoded POST forms submit; multipart and file uploads are not supported.</p>

<h2>Try it</h2>
<p>These links run JavaScript in this page:</p>
<ul>
<li><a href="#" onclick="alert('Hello from MOB³!'); return false;">show an alert</a></li>
<li><a href="#" onclick="return confirm('Do you like MOB³?')">ask a yes/no question</a></li>
<li><a href="#" onclick="var n = prompt('What is your name?', 'friend'); if (n) { alert('Hello, ' + n + '!'); } return false;">ask for a name</a></li>
</ul>
<h3>Download and upload</h3>
<p><a href="https://mob3.net/index.html" download="index.html">Download a sample file</a>
opens a popup with a <b>curl</b> command; press <b>y</b> to copy it and close
the popup, then run it in your terminal. MOB³ itself never saves the file.</p>
<p>File uploads are not supported. Click the file control below to see a notice
explaining this; it is rendered for layout only and is skipped when a form is
submitted.</p>
<h3>Form controls</h3>
<form action="about:help" method="get">
<p><label>Text input: <input name="demo-text" placeholder="try typing here"></label></p>
<p><label><input type="checkbox" name="demo-check" value="on" checked> Checkbox</label></p>
<p><label><input type="radio" name="demo-radio" value="a" checked> Radio A</label>
<label><input type="radio" name="demo-radio" value="b"> Radio B</label></p>
<p><label>Select: <select name="demo-select"><option value="one">One</option><option value="two" selected>Two</option><option value="three">Three</option></select></label></p>
<p><label>Multi-select:<br><select name="demo-multi" multiple><option value="red">Red</option><option value="green" selected>Green</option><option value="blue">Blue</option></select></label></p>
<p><label>Date: <input type="date" name="demo-date" value="2026-11-10" placeholder="YYYY-MM-DD"></label></p>
<p><label>Time: <input type="time" name="demo-time" value="14:30" placeholder="HH:MM"></label></p>
<p><label>Date and time: <input type="datetime-local" name="demo-datetime" value="2026-11-10T14:30" placeholder="YYYY-MM-DDTHH:MM"></label></p>
<p><label>Month: <input type="month" name="demo-month" value="2026-11" placeholder="YYYY-MM"></label></p>
<p><label>Week: <input type="week" name="demo-week" value="2026-W45" placeholder="YYYY-W01"></label></p>
<p><label>Range: <input type="range" name="demo-range" min="0" max="100" step="5" value="50"></label></p>
<p><label>Color: <input type="color" name="demo-color" value="#2f6fbf" maxlength="7"></label></p>
<p><label>Required (clear it to see the error): <input name="demo-required" value="ready" required></label></p>
<p><label>Pattern (three digits): <input name="demo-pattern" pattern="[0-9]{3}" maxlength="3" value="123"></label></p>
<p><label>Read-only: <input name="demo-readonly" value="cannot edit" readonly></label></p>
<p><label>Disabled: <input name="demo-disabled" value="not submitted" disabled></label></p>
<p><label>Textarea:<br><textarea name="demo-textarea">Some editable text</textarea></label></p>
<p><label>File upload: <input type="file" name="demo-file"></label> (not supported)</p>
<p><button name="demo-submit" value="1">Submit demo</button></p>
</form>

<h2>JavaScript console</h2>
<p><b>!</b> opens logs, requests, storage, the page HTML and Tools. Use
<b>Tab</b> or <b>h</b>/<b>l</b> to switch tabs, <b>:</b>/<b>Enter</b> to focus the input, <b>Enter</b>
to run JavaScript, <b>j</b>/<b>k</b> to scroll and <b>Esc</b> to close. <b>y</b>
copies the whole active tab — the logs, the raw HTML source, the request list
or the Tools scrollback — because selecting long output by dragging is
awkward. Use <b>Shift</b>-drag when you only want part of it. Script
<b>alert</b>, <b>confirm</b> and <b>prompt</b> calls open modal dialogs.</p>

<p><a href="about:start">Back to the start page</a></p>`

// helpPageHTML wraps the help body in the shared page shell.
func helpPageHTML() string {
	return appPageHTML("Help — MOB³", "",
		strings.Replace(helpPageBody, "{{VERSION}}", EscapeHTML(Version), 1))
}

// historyPageHTML renders the in-memory session history as clickable links.
func historyPageHTML(urls []string, current int) string {
	var b strings.Builder
	b.WriteString(`<h1>Session history</h1>
<p>Pages visited in this session, most recent first. History is kept in
memory only and disappears when MOB³ exits.</p>
`)
	const limit = 50
	start := 0
	if len(urls) > limit {
		start = len(urls) - limit
	}
	count := 0
	for i := len(urls) - 1; i >= start; i-- {
		raw := urls[i]
		if raw == "" || raw == "about:history" {
			continue
		}
		count++
		b.WriteString(`<li><a href="`)
		b.WriteString(EscapeHTML(raw))
		b.WriteString(`">`)
		b.WriteString(EscapeHTML(raw))
		b.WriteString(`</a>`)
		if i == current {
			b.WriteString(` <b>(current)</b>`)
		}
		b.WriteString(`</li>`)
	}
	if count == 0 {
		b.WriteString(`<p>No pages in this session yet.</p>`)
	}
	if len(urls) > limit {
		b.WriteString(`<p>Only the most recent 50 entries are shown.</p>`)
	}
	return appPageHTML("History — MOB³", "li { margin: 6px 0; }\n", b.String())
}

const startPageHTML = `<!DOCTYPE html>
<html><head><title>MOB³</title></head>
<body><article>
<h1>MOB³ - a small terminal web browser</h1>
<h3>Getting started</h3>
<ul>
<li>Press <b>o</b> and type a URL, then Enter.</li>
<li>Press <b>f</b> to follow a link.</li>
<li>Scroll with <b>j</b>/<b>k</b>, <b>Space</b>/<b>b</b>, or the arrow keys.</li>
<li><b>Ctrl-O</b> or <b>[</b> goes back, <b>Ctrl-I</b> or <b>]</b> goes forward, <b>r</b> reloads, <b>q</b> asks before quitting.</li>
<li>Press <b>?</b> for the full help page.</li>
<li>History, cookies, storage and caches are memory-only.</li>
</ul>
<form action="about:home" method="get">
<input type="search" name="q" placeholder="Search or enter address">
<button type="submit">Go</button>
</form>
<p><b>Warning:</b> MOB³ is not a secure browser in any way. Do not use it for passwords, accounts, payments or confidential data.</p>
</article></body></html>`

// cookieJar keeps all HTTP cookies in memory and maintains a second jar
// containing only cookies visible to document.cookie. Keeping the two jars
// separate makes HttpOnly enforcement fall out of the standard cookie rules.
type cookieJar struct {
	// mu guards cookies and is held across jar updates so a server response
	// arriving on a background goroutine and a document.cookie write on the
	// main goroutine cannot interleave between the visibility check and the
	// write.
	mu      sync.Mutex
	all     http.CookieJar
	visible http.CookieJar
	// cookies tracks the metadata cookiejar drops on read (domain, path,
	// flags, expiry) for the storage inspector, and marks HttpOnly cookies so
	// document.cookie can refuse to replace or delete them.
	cookies map[cookieIdentity]cookieMeta
}

// cookieIdentity is the (effective domain, path, name) triple cookiejar uses
// to replace a stored cookie.
type cookieIdentity struct {
	domain string
	path   string
	name   string
}

type cookieMeta struct {
	value    string
	httpOnly bool
	secure   bool
	sameSite http.SameSite
	hostOnly bool
	expires  time.Time // zero for a session cookie
	// asciiDomain reports that domain can be compared exactly. Cookies on a
	// non-ASCII host are stored punycoded by cookiejar, so their identity is
	// compared conservatively instead.
	asciiDomain bool
}

func newCookieJar() *cookieJar {
	options := &cookiejar.Options{PublicSuffixList: publicsuffix.List}
	all, _ := cookiejar.New(options)
	visible, _ := cookiejar.New(options)
	return &cookieJar{all: all, visible: visible, cookies: make(map[cookieIdentity]cookieMeta)}
}

// cookieIdentityFor computes the identity cookiejar will store for c on u.
// ok is false for a cookie the jar rejects, such as a foreign or public-suffix
// Domain attribute.
func cookieIdentityFor(u *url.URL, c *http.Cookie) (id cookieIdentity, ascii bool, ok bool) {
	if u == nil || c == nil {
		return id, false, false
	}
	path := c.Path
	if path == "" || path[0] != '/' {
		path = defaultCookiePath(u.Path)
	}
	domain := c.Domain
	if domain != "" {
		domain = strings.TrimPrefix(domain, ".")
		if domain == "" || domain[0] == '.' || domain[len(domain)-1] == '.' ||
			!isASCIIDomain(domain) || !cookieDomainMatches(u.Hostname(), domain) {
			return id, false, false
		}
		ascii = true
	} else {
		domain = strings.TrimSuffix(u.Hostname(), ".")
		ascii = isASCIIDomain(domain)
	}
	if !ascii {
		domain = strings.ToLower(domain)
	}
	return cookieIdentity{domain: strings.ToLower(domain), path: path, name: c.Name}, ascii, true
}

// cookieDomainMatches reports whether a Domain attribute is one cookiejar will
// accept for host. A non-ASCII host cannot be compared against the jar's
// punycoded form here, so it is accepted and the identity check falls back to
// a conservative comparison.
func cookieDomainMatches(host, domain string) bool {
	if !isASCIIDomain(host) {
		return true
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	domain = strings.ToLower(domain)
	if net.ParseIP(host) != nil {
		return host == domain
	}
	if host == domain {
		return true
	}
	if publicsuffix.List.PublicSuffix(domain) == domain {
		return false
	}
	return strings.HasSuffix(host, "."+domain)
}

func isASCIIDomain(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// defaultCookiePath is RFC 6265 section 5.1.4, matching cookiejar.
func defaultCookiePath(path string) string {
	if len(path) == 0 || path[0] != '/' {
		return "/"
	}
	if i := strings.LastIndex(path, "/"); i > 0 {
		return path[:i]
	}
	return "/"
}

// cookieDeletes reports whether c removes a stored cookie rather than setting
// it, matching cookiejar's MaxAge/Expires rules.
func cookieDeletes(c *http.Cookie) bool {
	if c.MaxAge < 0 {
		return true
	}
	if c.MaxAge > 0 {
		return false
	}
	return !c.Expires.IsZero() && !c.Expires.After(time.Now())
}

func (j *cookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.setCookiesLocked(u, cookies)
}

func (j *cookieJar) setCookiesLocked(u *url.URL, cookies []*http.Cookie) {
	j.all.SetCookies(u, cookies)
	visible := make([]*http.Cookie, 0, len(cookies))
	for _, c := range cookies {
		if id, ascii, ok := cookieIdentityFor(u, c); ok {
			if cookieDeletes(c) {
				delete(j.cookies, id)
			} else {
				m := cookieMeta{
					value:       c.Value,
					httpOnly:    c.HttpOnly,
					secure:      c.Secure,
					sameSite:    c.SameSite,
					hostOnly:    c.Domain == "",
					asciiDomain: ascii,
				}
				if c.MaxAge > 0 {
					m.expires = time.Now().Add(time.Duration(c.MaxAge) * time.Second)
				} else if c.MaxAge == 0 {
					m.expires = c.Expires
				}
				j.cookies[id] = m
			}
		}
		if c.HttpOnly {
			// A server may replace a JS-visible cookie with an HttpOnly one.
			// Remove any previous visible entry with the same identity.
			j.visible.SetCookies(u, []*http.Cookie{{
				Name:   c.Name,
				Domain: c.Domain,
				Path:   c.Path,
				MaxAge: -1,
			}})
			continue
		}
		visible = append(visible, c)
	}
	if len(visible) > 0 {
		j.visible.SetCookies(u, visible)
	}
}

func (j *cookieJar) Cookies(u *url.URL) []*http.Cookie {
	return j.all.Cookies(u)
}

func (j *cookieJar) documentCookies(u *url.URL) []*http.Cookie {
	return j.visible.Cookies(u)
}

// cookiesForDisplay returns every cookie that applies to u with its stored
// metadata, for the storage inspector. cookiejar drops the flags and scope on
// read, so the tracked copy is the source here.
func (j *cookieJar) cookiesForDisplay(u *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	out := make([]*http.Cookie, 0, len(j.cookies))
	for id, m := range j.cookies {
		if !m.expires.IsZero() && !m.expires.After(now) {
			delete(j.cookies, id)
			continue
		}
		if !cookieApplies(u, id, m) {
			continue
		}
		c := &http.Cookie{
			Name:     id.name,
			Value:    m.value,
			Path:     id.path,
			HttpOnly: m.httpOnly,
			Secure:   m.secure,
			SameSite: m.sameSite,
			Expires:  m.expires,
		}
		if m.hostOnly {
			c.Domain = id.domain
		} else {
			c.Domain = "." + id.domain
		}
		out = append(out, c)
	}
	return out
}

// cookieApplies reports whether a stored cookie would be sent to u.
func cookieApplies(u *url.URL, id cookieIdentity, m cookieMeta) bool {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if m.secure && u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if m.hostOnly {
		if host != id.domain {
			return false
		}
	} else if m.asciiDomain {
		if !cookieDomainMatches(host, id.domain) {
			return false
		}
	} else if host != id.domain && !strings.HasSuffix(host, "."+id.domain) {
		return false
	}
	return cookiePathMatches(u.Path, id.path)
}

// cookiePathMatches is RFC 6265 section 5.1.4.
func cookiePathMatches(requestPath, cookiePath string) bool {
	if requestPath == "" {
		requestPath = "/"
	}
	if requestPath == cookiePath {
		return true
	}
	if !strings.HasPrefix(requestPath, cookiePath) {
		return false
	}
	if strings.HasSuffix(cookiePath, "/") {
		return true
	}
	return len(requestPath) > len(cookiePath) && requestPath[len(cookiePath)] == '/'
}

// setDocumentCookie applies a document.cookie assignment. A script may neither
// replace nor delete a server-set HttpOnly cookie, per RFC 6265bis.
func (j *cookieJar) setDocumentCookie(u *url.URL, c *http.Cookie) {
	c.HttpOnly = false
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.httpOnlyProtectedLocked(u, c) {
		return
	}
	j.setCookiesLocked(u, []*http.Cookie{c})
}

// httpOnlyProtectedLocked reports whether c has the same identity as a stored
// HttpOnly cookie. Callers hold j.mu.
func (j *cookieJar) httpOnlyProtectedLocked(u *url.URL, c *http.Cookie) bool {
	id, ascii, ok := cookieIdentityFor(u, c)
	if !ok {
		return false
	}
	for key, e := range j.cookies {
		if !e.httpOnly || key.name != id.name || key.path != id.path {
			continue
		}
		if ascii && e.asciiDomain && key.domain != id.domain {
			continue
		}
		return true
	}
	return false
}
