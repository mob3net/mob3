package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
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
	URL         *url.URL
	Base        *url.URL
	Root        *Node
	Title       string
	Author      []Rule
	Scripts     []Script
	Status      int
	Blocked     bool
	BlockReason string
}

// Script is one page script in document order. Src is a label for errors.
type Script struct {
	Code string
	Src  string
	Node *Node
}

// Loader fetches documents and their resources.
type Loader struct {
	client    *http.Client
	userAgent string
	searchURL string
	noScripts bool
	imgMu     sync.Mutex
	imgs      map[string]image.Image
}

// NewLoader creates a loader with cookie support and a sane timeout.
func NewLoader() *Loader {
	jar, _ := cookiejar.New(nil)
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          16,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	}
	return &Loader{
		imgs: make(map[string]image.Image),
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
		userAgent: "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0 MOB3/1.0 (+terminal)",
		searchURL: DefaultSearchURL,
	}
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
	// A bare path that exists is treated as a local file.
	if st, statErr := os.Stat(s); statErr == nil && !st.IsDir() {
		abs, err := filepath.Abs(s)
		if err == nil {
			return &url.URL{Scheme: "file", Path: abs}, nil
		}
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
	allowFile := u.Scheme == "file"
	data, ctype, finalURL, status, err := l.fetchRequest(ctx, method, u, body, contentType, allowFile)
	if err != nil {
		return nil, err
	}
	text := decodeBody(data, ctype)
	if blocked, reason := blockedResponse(status, text); blocked {
		root := ParseHTML(blockedPageHTML(finalURL.String(), reason, status))
		return &Page{
			URL: finalURL, Base: finalURL, Root: root,
			Title: "Site blocked", Status: status, Blocked: true, BlockReason: reason,
		}, nil
	}
	mediaType := ""
	if mt, _, err := mime.ParseMediaType(ctype); err == nil {
		mediaType = strings.ToLower(mt)
	}
	switch {
	case mediaType == "text/plain":
		text = "<html><body><pre>" + EscapeHTML(text) + "</pre></body></html>"
	case mediaType != "" && mediaType != "text/html" && mediaType != "application/xhtml+xml" && !strings.HasSuffix(mediaType, "+html"):
		text = "<html><head><title>" + EscapeHTML(mediaType) + "</title></head><body>" +
			"<h2>Cannot display " + EscapeHTML(mediaType) + "</h2>" +
			"<p>The browser only renders HTML pages. The URL was:</p><pre>" +
			EscapeHTML(finalURL.String()) + "</pre></body></html>"
	}
	root := ParseHTML(text)
	if !l.noScripts {
		// <noscript> content is inactive with scripting enabled; remove it
		// before collecting styles or scripts (some sites use it to hide the
		// whole body until hydration).
		removeNoscript(root)
	}
	page := &Page{URL: finalURL, Base: finalURL, Root: root, Status: status}
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
	page.Author = l.collectStylesheets(ctx, root, page.Base, allowFile)
	if !l.noScripts {
		page.Scripts = l.collectScripts(ctx, root, page.Base, allowFile)
	}
	l.loadInlineSVGs(root)
	l.loadImages(ctx, root, page.Base, allowFile)
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
	return `<!DOCTYPE html><html><head><title>Site blocked</title>
<style>
body { margin: 16px; max-width: 900px; }
h1 { font-size: 32px; color: #b42318; }
.warn { background:#fff3cd; border:1px solid #d4a72c; padding:8px; }
</style></head><body>
<h1>Site blocked</h1>
<p class="warn">This site blocked MOB³ or requires a browser check that it
cannot complete. Common causes are Cloudflare, CAPTCHA, anti-bot or firewall
protection, and login protections that need a full browser with
JavaScript/WASM, fingerprinting and browser APIs.</p>
<p>Detected: <b>` + EscapeHTML(reason) + `</b>` + ` (HTTP ` + strconv.Itoa(status) + `).</p>
<p>The requested page was not loaded. Try a different source, use the site's
direct or alternative URL, or open it in a full browser.</p>
<p>URL: <b>` + EscapeHTML(rawURL) + `</b></p>
</body></html>`
}

// collectScripts gathers inline and external JavaScript in document order.
// Modules and non-JavaScript script types are skipped.
func (l *Loader) collectScripts(ctx context.Context, root *Node, base *url.URL, allowFile bool) []Script {
	nodes := root.FindAll(func(n *Node) bool {
		if n.Tag != "script" {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(n.Attr("type"))) {
		case "", "text/javascript", "application/javascript", "text/ecmascript", "application/ecmascript":
			return true
		}
		return false
	})
	var out []Script
	total := 0
	for _, n := range nodes {
		src := strings.TrimSpace(n.Attr("src"))
		if src == "" {
			code := n.TextContent()
			if strings.TrimSpace(code) == "" {
				continue
			}
			total += len(code)
			out = append(out, Script{Code: code, Src: "inline script", Node: n})
			if total > maxResourceSize {
				break
			}
			continue
		}
		u, err := base.Parse(src)
		if err != nil {
			continue
		}
		data, ctype, _, status, err := l.fetchCtx(ctx, u, allowFile)
		if err != nil || status < 200 || status >= 400 {
			continue
		}
		code := decodeBody(data, ctype)
		total += len(code)
		out = append(out, Script{Code: code, Src: u.String(), Node: n})
		if total > maxResourceSize {
			break
		}
	}
	return out
}

func (l *Loader) loadAbout(ctx context.Context, u *url.URL) (*Page, error) {
	var doc string
	switch u.String() {
	case "about:blank":
		doc = "<html><body></body></html>"
	case "about:help":
		doc = helpPageHTML
	default:
		doc = startPageHTML
	}
	root := ParseHTML(doc)
	if !l.noScripts {
		removeNoscript(root)
	}
	title := "MOB³"
	if u.String() == "about:help" {
		title = "Help — MOB³"
	}
	page := &Page{URL: u, Base: u, Root: root, Title: title, Status: 200}
	page.Author = l.collectStylesheets(ctx, root, page.Base, false)
	l.loadInlineSVGs(root)
	return page, nil
}

func (l *Loader) fetchCtx(ctx context.Context, u *url.URL, allowFile bool) (data []byte, ctype string, final *url.URL, status int, err error) {
	return l.fetchRequest(ctx, http.MethodGet, u, nil, "", allowFile)
}

func (l *Loader) fetchRequest(ctx context.Context, method string, u *url.URL, body []byte, contentType string, allowFile bool) (data []byte, ctype string, final *url.URL, status int, err error) {
	if err := ctx.Err(); err != nil {
		return nil, "", u, 0, err
	}
	if method == "" {
		method = http.MethodGet
	}
	if len(body) > maxResourceSize {
		return nil, "", u, 0, errors.New("request body too large")
	}
	switch u.Scheme {
	case "file":
		if !allowFile {
			return nil, "", u, 0, errors.New("local file access is blocked from remote pages")
		}
		p := u.Path
		if p == "" {
			p = u.Opaque
		}
		if unescaped, e := url.PathUnescape(p); e == nil {
			p = unescaped
		}
		f, e := os.Open(p)
		if e != nil {
			return nil, "", u, 0, e
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil {
			return nil, "", u, 0, e
		}
		if !st.Mode().IsRegular() {
			return nil, "", u, 0, errors.New("only regular local files can be read")
		}
		if e := ctx.Err(); e != nil {
			return nil, "", u, 0, e
		}
		b, e := io.ReadAll(io.LimitReader(f, maxResourceSize))
		if e != nil {
			return nil, "", u, 0, e
		}
		if e := ctx.Err(); e != nil {
			return nil, "", u, 0, e
		}
		return b, mimeByExt(p), u, 200, nil
	case "data":
		b, ct, e := parseDataURI(u.String())
		return b, ct, u, 200, e
	case "http", "https":
		var reader io.Reader
		if len(body) > 0 {
			reader = bytes.NewReader(body)
		}
		req, e := http.NewRequestWithContext(ctx, method, u.String(), reader)
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
		resp, e := l.client.Do(req)
		if e != nil {
			return nil, "", u, 0, e
		}
		defer resp.Body.Close()
		b, e := io.ReadAll(io.LimitReader(resp.Body, maxResourceSize))
		if e != nil {
			return nil, "", u, resp.StatusCode, e
		}
		return b, resp.Header.Get("Content-Type"), resp.Request.URL, resp.StatusCode, nil
	}
	return nil, "", u, 0, errors.New("unsupported URL scheme: " + u.Scheme)
}

func mimeByExt(p string) string {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".html", ".htm", ".xhtml":
		return "text/html"
	case ".css":
		return "text/css"
	case ".txt":
		return "text/plain"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	}
	return "application/octet-stream"
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
	switch charset {
	case "iso-8859-1", "latin1", "latin-1", "windows-1252", "cp1252":
		return decodeLatin1(data, charset == "windows-1252" || charset == "cp1252")
	}
	return strings.ToValidUTF8(string(data), "\uFFFD")
}

func decodeLatin1(data []byte, windows1252 bool) string {
	var b strings.Builder
	b.Grow(len(data))
	for _, c := range data {
		r := rune(c)
		if windows1252 && c >= 0x80 && c <= 0x9f {
			if mapped, ok := cp1252Extra[c]; ok {
				r = mapped
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

var cp1252Extra = map[byte]rune{
	0x80: '€', 0x82: '‚', 0x83: 'ƒ', 0x84: '„', 0x85: '…', 0x86: '†', 0x87: '‡',
	0x88: 'ˆ', 0x89: '‰', 0x8a: 'Š', 0x8b: '‹', 0x8c: 'Œ', 0x8e: 'Ž',
	0x91: '‘', 0x92: '’', 0x93: '“', 0x94: '”', 0x95: '•', 0x96: '–', 0x97: '—',
	0x98: '˜', 0x99: '™', 0x9a: 'š', 0x9b: '›', 0x9c: 'œ', 0x9e: 'ž', 0x9f: 'Ÿ',
}

func (l *Loader) collectStylesheets(ctx context.Context, root *Node, base *url.URL, allowFile bool) []Rule {
	var out []Rule
	order := 0
	add := func(rules []Rule) {
		for i := range rules {
			rules[i].Order = order
			order++
			out = append(out, rules[i])
		}
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
			add(ParseStylesheetContext(ctx, n.TextContent()))
			continue
		}
		href := n.Attr("href")
		u, err := base.Parse(href)
		if err != nil {
			continue
		}
		data, _, _, status, err := l.fetchCtx(ctx, u, allowFile)
		if err != nil || status < 200 || status >= 400 {
			continue
		}
		add(ParseStylesheetContext(ctx, string(data)))
	}
	return out
}

func firstSrcset(v string) string {
	for _, part := range strings.Split(v, ",") {
		f := strings.Fields(strings.TrimSpace(part))
		if len(f) > 0 {
			return f[0]
		}
	}
	return ""
}

func (l *Loader) loadImages(ctx context.Context, root *Node, base *url.URL, allowFile bool) {
	imgs := root.FindAll(func(n *Node) bool {
		return n.Tag == "img" && (n.Attr("src") != "" || n.Attr("srcset") != "")
	})
	if len(imgs) == 0 {
		return
	}
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for _, n := range imgs {
		src := n.Attr("src")
		if src == "" {
			src = firstSrcset(n.Attr("srcset"))
		}
		n.ImgSrc = src
		u, err := base.Parse(src)
		if err != nil {
			n.ImgFailed = true
			continue
		}
		key := u.String()
		l.imgMu.Lock()
		cached, ok := l.imgs[key]
		l.imgMu.Unlock()
		if ok {
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
			data, ctype, _, status, err := l.fetchCtx(ctx, u, allowFile)
			if err != nil || status < 200 || status >= 400 {
				node.ImgFailed = true
				return
			}
			var img image.Image
			if isSVG(ctype, data) {
				img, err = decodeSVG(data)
			} else {
				cfg, _, cfgErr := image.DecodeConfig(bytes.NewReader(data))
				if cfgErr != nil || !imageConfigOK(cfg) {
					node.ImgFailed = true
					return
				}
				if ctx.Err() != nil {
					node.ImgFailed = true
					return
				}
				var decodeErr error
				img, _, decodeErr = image.Decode(bytes.NewReader(data))
				err = decodeErr
			}
			if err != nil {
				node.ImgFailed = true
				return
			}
			node.Img = img
			l.imgMu.Lock()
			l.imgs[key] = img
			l.imgMu.Unlock()
		}(n, u)
	}
	wg.Wait()
}

const helpPageHTML = `<!DOCTYPE html>
<html><head><title>Help — MOB³</title>
<style>
body { max-width: 900px; margin: 8px auto; }
h1 { font-size: 32px; margin-bottom: 4px; }
table { margin: 8px 0 16px; }
th, td { padding: 3px 10px; text-align: left; border-bottom: 1px solid #d0d7de; }
th { color: #57606a; }
.muted { color: #57606a; }
.warn { background:#fff3cd; border:1px solid #d4a72c; padding:8px; color:#442c00; }
</style></head><body>
<h1>MOB³ Help</h1>
<p class="muted">A small terminal browser. Keys are vim-first.</p>

<h2>Keyboard</h2>
<table>
<tr><th>Key</th><th>Action</th></tr>
<tr><td><b>j</b> / <b>k</b></td><td>scroll down / up one line</td></tr>
<tr><td><b>Space</b> / <b>b</b></td><td>scroll down / up one screen</td></tr>
<tr><td><b>Ctrl-D</b> / <b>Ctrl-U</b></td><td>scroll half a screen</td></tr>
<tr><td><b>g</b> / <b>G</b></td><td>top / bottom of page</td></tr>
<tr><td><b>Tab</b> / <b>Shift-Tab</b></td><td>next / previous link</td></tr>
<tr><td><b>Enter</b></td><td>follow the focused link or submit the focused field</td></tr>
<tr><td><b>h</b> / <b>Left</b></td><td>back</td></tr>
<tr><td><b>l</b> / <b>Right</b></td><td>forward</td></tr>
<tr><td><b>r</b></td><td>reload</td></tr>
<tr><td><b>o</b></td><td>open a URL (prefilled and selected; arrows/Home/End/Delete edit it)</td></tr>
<tr><td><b>u</b></td><td>show the current URL</td></tr>
<tr><td><b>?</b></td><td>this help page</td></tr>
<tr><td><b>!</b></td><td>JavaScript console / errors</td></tr>
<tr><td><b>Esc</b></td><td>stop loading, leave a field, or clear a selection</td></tr>
<tr><td><b>q</b> / <b>Ctrl-C</b></td><td>quit</td></tr>
</table>

<h2>Mouse</h2>
<table>
<tr><th>Input</th><th>Action</th></tr>
<tr><td>drag over page text</td><td>select it; release keeps the highlight</td></tr>
<tr><td>click a link</td><td>follow it</td></tr>
<tr><td>click a text field</td><td>focus and edit it</td></tr>
<tr><td>click the URL bar</td><td>open the URL prompt</td></tr>
<tr><td>click &#8592; / &#8635; / ?</td><td>back / reload / help</td></tr>
<tr><td>right click</td><td>go back</td></tr>
<tr><td>wheel</td><td>scroll</td></tr>
</table>

<h2>Forms</h2>
<p>Click a text input or textarea to type. <b>Backspace</b> deletes,
<b>Ctrl-U</b> clears, <b>Enter</b> submits, and <b>Esc</b> leaves the field.
GET and urlencoded POST forms work; multipart and file uploads are not
supported.</p>

<h2>Try it</h2>
<p>These links run JavaScript in this page:</p>
<ul>
<li><a href="#" onclick="alert('Hello from MOB³!'); return false;">show an alert</a></li>
<li><a href="#" onclick="return confirm('Do you like MOB³?')">ask a yes/no question</a></li>
<li><a href="#" onclick="var n = prompt('What is your name?', 'friend'); if (n) { alert('Hello, ' + n + '!'); } return false;">ask for a name</a></li>
</ul>
<p>You can also click this field and type in it:</p>
<p><input placeholder="try typing here"></p>

<h2>JavaScript console</h2>
<p>Press <b>!</b> to see <b>console</b> output and full script errors. While the
console is open, terminal mouse selection and copy work normally.
<b>j</b>/<b>k</b> scroll, <b>Esc</b> or <b>!</b> closes, <b>q</b> quits.</p>
<p>Script <b>alert</b>, <b>confirm</b> and <b>prompt</b> calls open a modal
dialog. Use <b>Enter</b>/<b>Esc</b> to answer; confirm accepts <b>Y</b>/<b>N</b>.</p>

<p><a href="about:start">Back to the start page</a></p>
</body></html>`

const startPageHTML = `<!DOCTYPE html>
<html><head><title>MOB³</title>
<style>
body { margin:16px; background:#ffffff; }
.brand { text-align:center; margin:4px 0 8px; }
.brand svg { width:64px; height:64px; }
.logo { font-size:30px; font-weight:bold; color:#0645ad; margin-bottom:2px; }
.tagline { font-style:italic; color:#57606a; margin-top:0; }
.k { color:#57606a; }
pre { background:#f6f8fa; padding:8px; border-left:3px solid #d0d7de; }
a { color:#0645ad; text-decoration:underline; }
.warn { background:#fff3cd; border:1px solid #d4a72c; padding:8px; color:#442c00; }
</style></head>
<body>
<div class="logo">MOB³</div>
<p><b>Warning:</b> MOB³ is not a secure browser in any way. Do not use it for passwords, accounts, payments or confidential data.</p>
<p>MOB³ is a small terminal web browser written in Go. It fetches pages over HTTP(S), renders HTML and a practical CSS subset in 24-bit color, and shows images inline with kitty or iTerm2 graphics, falling back to half-block art.</p>
<h3>Getting started</h3>
<ul>
<li>Press <b>o</b> and type a URL, then Enter.</li>
<li>Press <b>Tab</b> to cycle links, <b>Enter</b> to follow one.</li>
<li>Scroll with <b>j</b>/<b>k</b>, <b>Space</b>/<b>b</b>, or the arrow keys.</li>
<li><b>h</b> goes back, <b>l</b> goes forward, <b>r</b> reloads, <b>q</b> quits.</li>
<li>Press <b>?</b> for the full help page.</li>
</ul>
<p>Try <a href="https://example.com">example.com</a> or any page you like.</p>
<h3>What is supported</h3>
<ul>
<li>HTML with headings, paragraphs, lists, tables, links, forms (read-only), images.</li>
<li>CSS: colors, fonts, margins, padding, borders, backgrounds, widths, text alignment, descendant/child selectors, attribute selectors, <span class="k">:first-child</span> and friends.</li>
<li>Images are decoded and drawn inline with kitty/iTerm2 graphics, with color half-block art as a fallback.</li>
<li>JavaScript runs with goja: DOM updates, click handlers and timers. Use <span class="k">-no-js</span> to turn it off.</li>
</ul>
</body></html>`
