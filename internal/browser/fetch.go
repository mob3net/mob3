package browser

import (
	"browser/internal/engine"
	"browser/internal/host"
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
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

// errResourceTooLarge reports a response body over maxResourceSize. Reading
// the extra byte distinguishes an exact-limit body from a truncated one.
var errResourceTooLarge = host.ErrResourceTooLarge

// imageConfigOK rejects decompression bombs before decoding pixel data.
func imageConfigOK(cfg image.Config) bool {
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return false
	}
	return int64(cfg.Width)*int64(cfg.Height) <= maxImagePixels
}

// Loader fetches documents and their resources.
// defaultUserAgent is the browser's identifying User-Agent.
const defaultUserAgent = "MOB3/1.0 (+terminal; +https://mob3.net)"

type Loader struct {
	client    *http.Client
	cookies   *cookieJar
	settings  *host.Settings
	noScripts bool
	// transport is the stable RoundTripper whose base is replaced when the
	// certificate source changes; client.Transport always points at it.
	transport *certTransport
	// certSource labels the active TLS trust store ("system" or "embedded")
	// and certPool holds its roots. A nil pool means the platform store. See
	// SetCertSource.
	certSource string
	certPool   *x509.CertPool
	// historyFn supplies the in-memory session history for about:history.
	historyFn func() ([]string, int)
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
	// imageBudget bounds decoded image bytes in flight; svgBudgetPixels and
	// the counters bound inline SVG rasterization for the current page.
	imageBudget     *byteLimiter
	svgMu           sync.Mutex
	svgBudgetPixels int64
	svgPixels       int64
	svgCount        int
	// cache holds raw subresources and decoded images for this session.
	cache *resourceCache
	// requests is the in-memory console request log.
	requests *host.RequestLog
}

// setUserAgent replaces the User-Agent used by later requests.
func (l *Loader) setUserAgent(ua string)       { l.settings.SetUserAgent(ua) }
func (l *Loader) requestUserAgent() string     { return l.settings.UserAgent() }
func (l *Loader) setSearchURL(template string) { l.settings.SetSearchURL(template) }
func (l *Loader) searchTemplate() string       { return l.settings.SearchURL() }
func (l *Loader) setImageCap(w, h int)         { l.settings.SetImageCaps(w, h) }
func (l *Loader) imageCap() (int, int)         { return l.settings.ImageCaps() }

// NewLoader creates a loader with cookie support and a sane timeout. The
// platform trust store is used until SetCertSource selects another one; the
// client's transport is a stable wrapper so that swap never races a request.
func NewLoader() *Loader {
	jar := newCookieJar()
	tr := newCertTransport(newHTTPTransport(nil))
	l := &Loader{
		cache:           newResourceCache(resourceCacheBudget(effectiveMemoryLimit())),
		cookies:         jar,
		noImages:        true,
		imageViewportPx: 800,
		client:          host.NewHTTPClient(jar, tr),
		settings:        host.NewSettings(defaultUserAgent, DefaultSearchURL),
		transport:       tr,
		requests:        host.NewRequestLog(),
		certSource:      CertSourceSystem,
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
	u, err := ResolveInput(input, base, l.searchTemplate())
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
		(space.Local.Load() && !requestUsesProxy(finalURL.String()))
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
		text = engine.CannotDisplayPageHTML(mediaType, finalURL)
	}
	page := &Page{
		URL: finalURL, Base: finalURL, RawHTML: rawHTML, Status: status,
		Download: download, DownloadName: downloadName, Local: pageLocal,
	}
	engine.AssemblePage(ctx, loaderPageFetcher{l}, page, text, l.noScripts)
	root := page.Root
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

// newFetcher snapshots the loader's current host dependencies. A fresh value
// per request lets concurrent navigations and subresources fetch without
// mutating shared state.
func (l *Loader) newFetcher() *host.Fetcher {
	return &host.Fetcher{
		Client:   l.client,
		Cache:    l.cache.ResourceCache,
		Settings: l.settings,
		Requests: l.requests,
	}
}

// fetchCtx and fetchRequest are the raw host fetch path.
func (l *Loader) fetchCtx(ctx context.Context, u *url.URL) (data []byte, ctype string, final *url.URL, status int, err error) {
	return l.newFetcher().FetchCtx(ctx, u)
}
func (l *Loader) fetchRequest(ctx context.Context, method string, u *url.URL, body []byte, contentType string) (data []byte, ctype string, final *url.URL, status int, err error) {
	return l.newFetcher().FetchRequest(ctx, method, u, body, contentType)
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

// combineMedia joins an @import/link media condition with a rule's own @media
// condition. A comma-listed import condition is approximated.

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
	maxW, maxH := l.imageCap()
	if img == nil || (maxW <= 0 && maxH <= 0) {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 {
		return img
	}
	nw, nh := w, h
	if maxW > 0 && nw > maxW {
		f := float64(maxW) / float64(nw)
		nw = maxW
		nh = int(math.Round(float64(nh) * f))
	}
	if maxH > 0 && nh > maxH {
		f := float64(maxH) / float64(nh)
		nh = maxH
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
	if cached, rawSize, cachedLocal, ok := l.cache.getImage(key); ok && !(meta.BlockLocal && cachedLocal) {
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
	l.cache.putImage(key, img, len(data), !meta.BlockLocal)
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
	gen := requestMetaFrom(ctx).Gen
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
	gen := meta.Gen
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
		if cached, rawSize, cachedLocal, ok := l.cache.getImage(key); ok && !(meta.BlockLocal && cachedLocal) {
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
			l.cache.putImage(key, img, len(data), !meta.BlockLocal)
		}(n, u)
	}
	wg.Wait()
	return changed
}

// helpPageHTML wraps the help body in the shared page shell.

// historyPageHTML renders the in-memory session history as clickable links.

func (l *Loader) beginPage() int             { return l.requests.BeginPage() }
func (l *Loader) currentPageGeneration() int { return l.requests.CurrentPageGeneration() }
func (l *Loader) startRequest(gen int, method, rawURL, kind string) (int, bool) {
	return l.requests.Start(gen, method, rawURL, kind)
}
func (l *Loader) recordCacheHit(gen int, method, rawURL, kind string, size int) {
	l.requests.RecordCacheHit(gen, method, rawURL, kind, size)
}
func (l *Loader) finishRequest(id, gen, status, size int, elapsed time.Duration, err error) {
	l.requests.Finish(id, gen, status, size, elapsed, err)
}
func (l *Loader) requestSnapshot() []host.Record { return l.requests.Snapshot() }
func (l *Loader) requestCount() int              { return l.requests.Count() }

type requestRecord = host.Record

func (l *Loader) imageCapW() int { w, _ := l.imageCap(); return w }
func (l *Loader) imageCapH() int { _, h := l.imageCap(); return h }

func (l *Loader) recordBlocked(gen int, method string, u *url.URL, kind string, err error) {
	l.newFetcher().RecordBlocked(gen, method, u, kind, err)
}

// Built-in page rendering moved to the engine; these names keep the
// session-side call sites and tests stable during the migration.
const startPageHTML = engine.StartPageHTML

func helpPageHTML() string { return engine.HelpPageHTML(buildVersion) }

func historyPageHTML(urls []string, current int) string {
	return engine.HistoryPageHTML(urls, current)
}

func blockedPageHTML(rawURL, reason string, status int) string {
	return engine.BlockedPageHTML(rawURL, reason, status)
}
