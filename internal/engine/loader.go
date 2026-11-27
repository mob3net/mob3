package engine

import (
	"context"
	"errors"
	"mime"
	"net/url"
	"strings"
	"sync"
	"time"

	"browser/internal/protocol"
)

// DefaultSearchURL is the template used when input looks like a search query.
// %s is replaced with the URL-escaped query; an empty template disables search.
// DuckDuckGo's html endpoint is server-rendered, unlike the JS-only main page.
const DefaultSearchURL = "https://html.duckduckgo.com/html/?q=%s"

// defaultUserAgent is the browser's identifying User-Agent.
const defaultUserAgent = "MOB3/1.0 (+terminal; +https://mob3.net)"

// Loader fetches documents and their resources.
type Loader struct {
	Host      protocol.Host
	NoScripts bool
	// CertStore labels the active TLS trust store ("system" or "embedded").
	// TLS itself stays host-side; see SetCertSource.
	CertStore string
	// HistoryFunc supplies the in-memory session history for about:history.
	HistoryFunc func() ([]string, int)
	// IgnoreBlocks disables anti-bot/CAPTCHA detection when set, so the
	// challenge page is loaded and its scripts run. -ignore-blocks sets it.
	IgnoreBlocks bool
	// NoPDF disables the built-in PDF text viewer, so a PDF response keeps
	// opening the download prompt instead of being rendered as text.
	// -no-pdf sets it.
	NoPDF bool
	// PDFPage restricts PDF rendering to one page (1-based); zero keeps every
	// page. It backs -pdf-page for dump and PNG output.
	PDFPage int
	// NoImages skips external image downloads and inline SVG rasterization.
	// It is the default so the browser stays fast; -graphics=none keeps it,
	// and any other graphics mode turns it off.
	NoImages bool
	// DeferImageLoad makes Load skip image downloads so the caller can show the
	// page first and fetch them in the background.
	DeferImageLoad bool
	// ViewportPx is the CSS-pixel viewport width used to choose srcset and
	// picture candidates. It is 800 when no browser width is available.
	ViewportPx int
	// ImageBudget bounds decoded image bytes in flight; SVGBudgetPixels and
	// the counters bound inline SVG rasterization for the current page.
	ImageBudget     *ByteLimiter
	SVGMu           sync.Mutex
	SVGBudgetPixels int64
	SVGPixels       int64
	SVGCount        int
}

// SetUserAgent replaces the User-Agent used by later requests.
func (l *Loader) SetUserAgent(ua string) {
	if l.Host != nil {
		l.Host.SetUserAgent(ua)
	}
}

// RequestUserAgent reports the current User-Agent.
func (l *Loader) RequestUserAgent() string {
	if l.Host == nil {
		return ""
	}
	return l.Host.UserAgent()
}

// SetSearchURL replaces the search template used for bare input.
func (l *Loader) SetSearchURL(template string) {
	if l.Host != nil {
		l.Host.SetSearchURL(template)
	}
}

// SearchTemplate reports the current search template.
func (l *Loader) SearchTemplate() string {
	if l.Host == nil {
		return ""
	}
	return l.Host.SearchURL()
}

// SetImageCap sets the decoded-image pixel cap.
func (l *Loader) SetImageCap(w, h int) {
	if l.Host != nil {
		l.Host.SetImageCaps(w, h)
	}
}

// ImageCap reports the decoded-image pixel cap.
func (l *Loader) ImageCap() (int, int) {
	if l.Host == nil {
		return 0, 0
	}
	return l.Host.ImageCaps()
}

// NewLoader creates a loader over the supplied host session. The platform
// trust store is used until SetCertSource selects another one; the client's
// transport is a stable wrapper so that swap never races a request.
func NewLoader(host protocol.Host) *Loader {
	l := &Loader{
		Host:       host,
		NoImages:   true,
		ViewportPx: 800,
		CertStore:  "system",
	}
	l.ImageBudget = NewByteLimiter(imageByteBudget(0))
	if host != nil {
		host.SetUserAgent(defaultUserAgent)
		host.SetSearchURL(DefaultSearchURL)
		host.SetCacheLimitBytes(resourceCacheBudget(EffectiveMemoryLimit()))
		l.CertStore = host.CertSource()
	}
	l.SetMemoryBudget(EffectiveMemoryLimit())
	return l
}

// Load fetches a page and all of its stylesheets and images.
func (l *Loader) Load(input string, base *url.URL) (*Page, error) {
	return l.LoadContext(context.Background(), input, base)
}

// LoadContext is Load with cancellation support. Canceling ctx aborts the
// page request, stylesheets, scripts and image downloads.
func (l *Loader) LoadContext(ctx context.Context, input string, base *url.URL) (*Page, error) {
	return l.LoadRequest(ctx, protocol.MethodGet, input, nil, "", base)
}

// LoadRequest loads a page with an explicit HTTP method and request body.
// Resources discovered while parsing the response are always fetched with GET.
func (l *Loader) LoadRequest(ctx context.Context, method, input string, body []byte, contentType string, base *url.URL) (*Page, error) {
	return l.loadRequest(ctx, method, input, body, contentType, base, l.IgnoreBlocks)
}

// LoadRequestBypass is LoadRequest with block detection disabled for this one
// load. It backs the "Try anyway" link on the blocked page.
func (l *Loader) LoadRequestBypass(ctx context.Context, method, input string, body []byte, contentType string, base *url.URL) (*Page, error) {
	return l.loadRequest(ctx, method, input, body, contentType, base, true)
}

func (l *Loader) loadRequest(ctx context.Context, method, input string, body []byte, contentType string, base *url.URL, ignoreBlocks bool) (*Page, error) {
	if l.Host == nil {
		return nil, errors.New("loader has no host session")
	}
	l.ResetSVGBudget()
	u, err := ResolveInput(input, base, l.SearchTemplate())
	if err != nil {
		return nil, err
	}
	if u.Scheme == "about" {
		if !strings.EqualFold(method, protocol.MethodGet) {
			return nil, errors.New("POST is not supported for " + u.Scheme + " URLs")
		}
		return l.LoadAbout(ctx, u)
	}
	ctx = protocol.WithRequestMeta(ctx, l.CurrentPageGeneration(), "doc")
	res, err := l.Host.Fetch(ctx, method, u, body, contentType)
	if err != nil {
		return nil, err
	}
	data, ctype, finalURL, status := res.Data, res.ContentType, res.FinalURL, res.Status
	pageLocal := res.PageLocal
	// Every subresource fetch inherits this context, so the guard knows
	// whether the document itself came from a local address.
	ctx = protocol.WithRequestPage(ctx, finalURL, pageLocal)
	mediaType := ""
	if mt, _, err := mime.ParseMediaType(ctype); err == nil {
		mediaType = strings.ToLower(mt)
	}
	// PDFs are decoded by their own reader, so they skip charset decoding and
	// the anti-bot heuristics that only make sense for HTML.
	if !l.NoPDF && LooksLikePDF(mediaType, data) {
		page := l.LoadPDFPage(ctx, data, finalURL, status, pageLocal)
		if page != nil {
			page.Local = pageLocal
		}
		return page, nil
	}
	text := decodeBody(data, ctype)
	rawHTML := text
	if !ignoreBlocks {
		if blocked, reason := BlockedResponse(status, text); blocked {
			return AssembleBlockedPage(ctx, loaderPageFetcher{l}, finalURL, rawHTML, reason, status, pageLocal), nil
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
		text = CannotDisplayPageHTML(mediaType, finalURL)
	}
	page := &Page{
		URL: finalURL, Base: finalURL, RawHTML: rawHTML, Status: status,
		Download: download, DownloadName: downloadName, Local: pageLocal,
	}
	AssemblePage(ctx, loaderPageFetcher{l}, page, text, l.NoScripts)
	root := page.Root
	l.LoadInlineSVGs(root)
	if !l.DeferImageLoad {
		l.LoadImages(ctx, root, page.Base)
	}
	return page, nil
}

func (l *Loader) LoadAbout(ctx context.Context, u *url.URL) (*Page, error) {
	var urls []string
	current := -1
	if l.HistoryFunc != nil {
		urls, current = l.HistoryFunc()
	}
	page := AssembleAboutPage(ctx, loaderPageFetcher{l}, u, BuildVersion, urls, current, l.NoScripts)
	l.LoadInlineSVGs(page.Root)
	return page, nil
}

// FetchCtx and FetchRequest are the raw host fetch path.
func (l *Loader) FetchCtx(ctx context.Context, u *url.URL) (data []byte, ctype string, final *url.URL, status int, err error) {
	if l.Host == nil {
		return nil, "", nil, 0, errors.New("loader has no host session")
	}
	res, err := l.Host.FetchCtx(ctx, u)
	return res.Data, res.ContentType, res.FinalURL, res.Status, err
}
func (l *Loader) FetchRequest(ctx context.Context, method string, u *url.URL, body []byte, contentType string) (data []byte, ctype string, final *url.URL, status int, err error) {
	if l.Host == nil {
		return nil, "", nil, 0, errors.New("loader has no host session")
	}
	res, err := l.Host.Fetch(ctx, method, u, body, contentType)
	return res.Data, res.ContentType, res.FinalURL, res.Status, err
}

func (l *Loader) BeginPage() int {
	if l.Host == nil {
		return 0
	}
	return l.Host.BeginPage()
}

func (l *Loader) CurrentPageGeneration() int {
	if l.Host == nil {
		return 0
	}
	return l.Host.CurrentPageGeneration()
}

func (l *Loader) StartRequest(gen int, method, rawURL, kind string) (int, bool) {
	if l.Host == nil {
		return 0, false
	}
	return l.Host.StartRequest(gen, method, rawURL, kind)
}

func (l *Loader) RecordCacheHit(gen int, method, rawURL, kind string, size int) {
	if l.Host != nil {
		l.Host.RecordCacheHit(gen, method, rawURL, kind, size)
	}
}

func (l *Loader) FinishRequest(id, gen, status, size int, elapsed time.Duration, err error) {
	if l.Host != nil {
		l.Host.FinishRequest(id, gen, status, size, elapsed, err)
	}
}

func (l *Loader) RequestSnapshot() []protocol.RequestRecord {
	if l.Host == nil {
		return nil
	}
	return l.Host.RequestSnapshot()
}

func (l *Loader) RequestCount() int {
	if l.Host == nil {
		return 0
	}
	return l.Host.RequestCount()
}

type requestRecord = protocol.RequestRecord

func (l *Loader) ImageCapW() int { w, _ := l.ImageCap(); return w }
func (l *Loader) ImageCapH() int { _, h := l.ImageCap(); return h }

func (l *Loader) RecordBlocked(gen int, method string, u *url.URL, kind string, err error) {
	if l.Host != nil {
		l.Host.RecordBlocked(gen, method, u, kind, err)
	}
}
