package browser

import (
	"browser/internal/protocol"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type countingTransport struct {
	count   int
	body    string
	noStore bool
}

func (c *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.count++
	header := make(http.Header)
	header.Set("Content-Type", "text/css")
	if c.noStore {
		header.Set("Cache-Control", "no-store")
	}
	return &http.Response{
		StatusCode: 200,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(c.body)),
		Request:    req,
	}, nil
}

func TestFetchCachesSubresources(t *testing.T) {
	l := NewLoader()
	tr := &countingTransport{body: "body { color: red; }"}
	l.client.HTTP().Transport = tr
	l.setCacheLimitMB(1)
	u := mustParse(t, "https://example.com/style.css")
	ctx := withRequestKind(context.Background(), "css")
	for i := 0; i < 2; i++ {
		data, ctype, _, status, err := l.fetchCtx(ctx, u)
		if err != nil || status != 200 || string(data) != tr.body || ctype != "text/css" {
			t.Fatalf("fetch %d = %q %q %d %v", i, data, ctype, status, err)
		}
	}
	if tr.count != 1 {
		t.Fatalf("HTTP requests = %d, want 1", tr.count)
	}
	records := l.requestSnapshot()
	if len(records) != 2 {
		t.Fatalf("request records = %+v, want network + cache", records)
	}
	if records[0].Cached || !records[1].Cached {
		t.Fatalf("cached flags = %+v", records)
	}
	if records[1].Kind != "css" || records[1].Status != 200 || records[1].Size != len(tr.body) {
		t.Fatalf("cache record = %+v", records[1])
	}

	tr2 := &countingTransport{body: "body { color: blue; }", noStore: true}
	l2 := NewLoader()
	l2.client.HTTP().Transport = tr2
	l2.setCacheLimitMB(1)
	ctx = withRequestKind(context.Background(), "css")
	for i := 0; i < 2; i++ {
		if _, _, _, _, err := l2.fetchCtx(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	if tr2.count != 2 {
		t.Fatalf("no-store HTTP requests = %d, want 2", tr2.count)
	}
	for _, r := range l2.requestSnapshot() {
		if r.Cached {
			t.Fatalf("no-store request was marked cached: %+v", r)
		}
	}
}

func TestHelpPage(t *testing.T) {
	p, err := NewLoader().Load("about:help", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Title, "Help") {
		t.Errorf("title = %q", p.Title)
	}
	text := p.Root.TextContent()
	if !strings.Contains(text, "Version") || !strings.Contains(text, buildVersion) {
		t.Errorf("help page is missing Version %q: %q", buildVersion, text[:minInt(len(text), 200)])
	}
	for _, want := range []string{"JavaScript console", "open a URL", "Mouse", "Try it", "show an alert", "Download a sample file", "File upload", "not supported", "Checkbox", "Radio A", "Select", "Multi-select", "Three", "Date", "Time", "Range", "Color", "Required", "Pattern", "Read-only", "Disabled", "Textarea", "Submit demo", "Command line", "-dump", "-width", "-graphics", "-no-js", "-reader", "-dark", "-ignore-blocks", "-home", "-text-scale", "-color", "-no-color", "-ua", "-search", "-version", "-help", "alias for -graphics", "session history"} {
		if !strings.Contains(text, want) {
			t.Errorf("help page is missing %q", want)
		}
	}
}

func TestHelpPageDemoInputPlaceholder(t *testing.T) {
	p, err := NewLoader().Load("about:help", nil)
	if err != nil {
		t.Fatal(err)
	}
	input := inputByName(p.Root, "demo-text")
	if input == nil {
		t.Fatal("demo text input is missing")
	}
	if got := input.Attr("value"); got != "" {
		t.Fatalf("demo input value = %q, want empty", got)
	}
	if got := input.Attr("placeholder"); got != "try typing here" {
		t.Fatalf("demo input placeholder = %q", got)
	}
	if label := formLabel(input); !strings.Contains(label, "try typing here") {
		t.Fatalf("unfocused input label = %q", label)
	}
	input.InputFocused = true
	if label := formLabel(input); strings.Contains(label, "try typing here") {
		t.Fatalf("focused input still shows placeholder: %q", label)
	}
}

func TestHelpPageNewInputTypes(t *testing.T) {
	p, err := NewLoader().Load("about:help", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"date": false, "time": false, "datetime-local": false, "month": false, "week": false, "range": false, "color": false}
	p.Root.FindAll(func(n *Node) bool {
		if n.Tag == "input" {
			typ := strings.ToLower(n.Attr("type"))
			if _, ok := want[typ]; ok {
				want[typ] = true
			}
		}
		return false
	})
	for typ, found := range want {
		if !found {
			t.Errorf("help page is missing input type %q", typ)
		}
	}
}

func TestHelpPageSelectControls(t *testing.T) {
	p, err := NewLoader().Load("about:help", nil)
	if err != nil {
		t.Fatal(err)
	}
	var single, multi *Node
	p.Root.FindAll(func(n *Node) bool {
		if n.Tag != "select" {
			return false
		}
		if n.HasAttr("multiple") && multi == nil {
			multi = n
		} else if !n.HasAttr("multiple") && single == nil {
			single = n
		}
		return false
	})
	if single == nil || len(selectOptions(single)) != 3 {
		t.Fatalf("single select = %+v", single)
	}
	if multi == nil || len(selectOptions(multi)) != 3 {
		t.Fatalf("multi-select = %+v", multi)
	}
}

func TestHelpPageDownloadAndUploadExamples(t *testing.T) {
	p, err := NewLoader().Load("about:help", nil)
	if err != nil {
		t.Fatal(err)
	}
	var download, upload *Node
	p.Root.FindAll(func(n *Node) bool {
		if n.Tag == "a" && n.HasAttr("download") && download == nil {
			download = n
		}
		if n.Tag == "input" && strings.EqualFold(n.Attr("type"), "file") && upload == nil {
			upload = n
		}
		return false
	})
	if download == nil {
		t.Fatal("help page has no download example link")
	}
	if download.Attr("download") != "index.html" {
		t.Fatalf("download example name = %q", download.Attr("download"))
	}
	if upload == nil {
		t.Fatal("help page has no file upload example")
	}
	if upload.Attr("name") != "demo-file" {
		t.Fatalf("upload example name = %q", upload.Attr("name"))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLoaderPOST(t *testing.T) {
	var gotMethod, gotBody, gotCT string
	loader := NewLoader()
	loader.client = newTestHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotMethod = req.Method
		gotCT = req.Header.Get("Content-Type")
		b, _ := io.ReadAll(req.Body)
		gotBody = string(b)
		h := make(http.Header)
		h.Set("Content-Type", "text/html")
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body:    io.NopCloser(strings.NewReader(`<html><head><title>Posted</title></head><body>ok</body></html>`)),
			Request: req,
		}, nil
	})})
	page, err := loader.LoadRequest(context.Background(), http.MethodPost, "https://example.com/submit", []byte("q=hi"), "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "Posted" {
		t.Fatalf("title = %q", page.Title)
	}
	if gotMethod != "POST" || gotBody != "q=hi" || !strings.HasPrefix(gotCT, "application/x-www-form-urlencoded") {
		t.Fatalf("request = method %q body %q content-type %q", gotMethod, gotBody, gotCT)
	}
}

func TestBlockedChallenge(t *testing.T) {
	loader := NewLoader()
	loader.client = newTestHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("Content-Type", "text/html")
		body := `<html><head><title>Just a moment...</title></head><body>
			<div id="cf-chl-widget"></div><script src="/challenge-platform/x.js"></script>
		</body></html>`
		return &http.Response{
			StatusCode: http.StatusForbidden, Status: "403 Forbidden", Header: h,
			Body: io.NopCloser(strings.NewReader(body)), Request: req,
		}, nil
	})})
	page, err := loader.LoadContext(context.Background(), "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !page.Blocked || page.BlockReason == "" {
		t.Fatal("blocker was not recognized")
	}
	if page.Title != "Site blocked" || len(page.Scripts) != 0 {
		t.Fatalf("blocked page = title %q scripts %d", page.Title, len(page.Scripts))
	}
	if !strings.Contains(page.Root.TextContent(), "blocked MOB³") {
		t.Fatal("blocked explanation missing")
	}
	if !strings.Contains(page.Root.TextContent(), "Cloudflare") {
		t.Fatal("detected reason missing")
	}
	if link := page.Root.FindFirst("a"); link == nil || link.Attr("href") != "about:retry" {
		t.Fatal("blocked page is missing the Try anyway link")
	}
	if !strings.Contains(page.Root.TextContent(), "Try anyway") {
		t.Fatal("Try anyway label missing")
	}
	// The blocked page's own stylesheet must be applied, or it renders at the
	// terminal width with no warning box instead of the shared page column.
	if len(page.Author) == 0 {
		t.Fatal("blocked page stylesheet was not collected")
	}
	ly := BuildLayout(page.Root, page.Author, 152)
	centered := false
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind == fragText && f.Node != nil && f.Node.Parent != nil &&
			f.Node.Parent.Tag == "h1" && f.X >= 8 {
			centered = true
			break
		}
	}
	if !centered {
		t.Fatal("blocked page heading is not in the centered page column")
	}
}

func TestJavaScriptMIME(t *testing.T) {
	cases := []struct {
		ctype string
		want  bool
	}{
		{"text/javascript", true},
		{"text/javascript; charset=utf-8", true},
		{"Application/JavaScript", true},
		{"text/x-javascript", true},
		{"", true},
		{"text/html", false},
		{"text/plain", false},
		{"application/json", false},
		{"image/png", false},
		{"text/csv", false},
	}
	for _, c := range cases {
		if got := isJavaScriptMIME(c.ctype); got != c.want {
			t.Errorf("isJavaScriptMIME(%q) = %v, want %v", c.ctype, got, c.want)
		}
	}
	if got := mimeEssence("Text/JavaScript; charset=utf-8"); got != "text/javascript" {
		t.Errorf("mimeEssence = %q", got)
	}
}

func TestScriptMIMEGate(t *testing.T) {
	loader := NewLoader()
	types := map[string]string{
		"/ok.js":      "application/javascript",
		"/upper.js":   "Application/JavaScript; charset=utf-8",
		"/legacy.js":  "text/x-javascript",
		"/html.js":    "text/html",
		"/plain.js":   "text/plain",
		"/json.js":    "application/json",
		"/image.js":   "image/png",
		"/csv.js":     "text/csv",
		"/missing.js": "",
	}
	loader.client = newTestHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		body := ""
		if req.URL.Path == "/" {
			h.Set("Content-Type", "text/html")
			body = `<html><body>
				<script src="/ok.js"></script>
				<script src="/upper.js"></script>
				<script src="/legacy.js"></script>
				<script src="/html.js"></script>
				<script src="/plain.js"></script>
				<script src="/json.js"></script>
				<script src="/image.js"></script>
				<script src="/csv.js"></script>
				<script src="/missing.js"></script>
				<script>var inline = 1;</script>
			</body></html>`
		} else if ct, ok := types[req.URL.Path]; ok {
			if ct != "" {
				h.Set("Content-Type", ct)
			}
			body = "var x = 1;"
		}
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(strings.NewReader(body)), Request: req,
		}, nil
	})})
	page, err := loader.LoadContext(context.Background(), "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, s := range page.Scripts {
		got[s.Src] = true
	}
	for _, want := range []string{
		"https://example.com/ok.js", "https://example.com/upper.js",
		"https://example.com/legacy.js", "https://example.com/missing.js",
		"inline script",
	} {
		if !got[want] {
			t.Errorf("allowed script %q missing: %+v", want, got)
		}
	}
	for _, bad := range []string{
		"https://example.com/html.js", "https://example.com/plain.js",
		"https://example.com/json.js", "https://example.com/image.js",
		"https://example.com/csv.js",
	} {
		if got[bad] {
			t.Errorf("blocked script %q executed", bad)
		}
	}
}

func TestRequestLogRecordsPageResources(t *testing.T) {
	loader := NewLoader()
	loader.client = newTestHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		body := ""
		switch req.URL.Path {
		case "/":
			h.Set("Content-Type", "text/html")
			body = `<html><head><link rel="stylesheet" href="/s.css"></head><body><script src="/a.js"></script></body></html>`
		case "/s.css":
			h.Set("Content-Type", "text/css")
			body = "body { color: red }"
		case "/a.js":
			h.Set("Content-Type", "application/javascript")
			body = "var x = 1;"
		default:
			h.Set("Content-Type", "text/plain")
		}
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(strings.NewReader(body)), Request: req,
		}, nil
	})})
	if _, err := loader.LoadContext(context.Background(), "https://example.com/", nil); err != nil {
		t.Fatal(err)
	}
	records := loader.requestSnapshot()
	if len(records) != 3 {
		t.Fatalf("requests = %+v", records)
	}
	want := map[string]bool{"doc": false, "css": false, "js": false}
	for _, r := range records {
		if !r.Done || r.Status != 200 {
			t.Errorf("request %+v not completed", r)
		}
		if _, ok := want[r.Kind]; ok {
			want[r.Kind] = true
		}
	}
	for kind, seen := range want {
		if !seen {
			t.Errorf("missing %s request in %+v", kind, records)
		}
	}

	loader.beginPage()
	if got := loader.requestSnapshot(); len(got) != 0 {
		t.Fatalf("beginPage did not clear requests: %+v", got)
	}
}

func TestBlockedBypass(t *testing.T) {
	newLoader := func() *Loader {
		loader := NewLoader()
		loader.client = newTestHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			h := make(http.Header)
			h.Set("Content-Type", "text/html")
			body := `<html><head><title>Just a moment...</title></head><body>
				<div id="cf-chl-widget"></div><script>window.challenge = true;</script>
			</body></html>`
			return &http.Response{
				StatusCode: http.StatusForbidden, Status: "403 Forbidden", Header: h,
				Body: io.NopCloser(strings.NewReader(body)), Request: req,
			}, nil
		})})
		return loader
	}

	loader := newLoader()
	page, err := loader.LoadContext(context.Background(), "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !page.Blocked || len(page.Scripts) != 0 {
		t.Fatal("default load did not block the challenge")
	}
	page, err = loader.LoadRequestBypass(context.Background(), http.MethodGet, "https://example.com/", nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.Blocked || len(page.Scripts) != 1 {
		t.Fatalf("bypass load = blocked %v scripts %d", page.Blocked, len(page.Scripts))
	}
	if page.Title != "Just a moment..." {
		t.Fatalf("title = %q", page.Title)
	}

	global := newLoader()
	global.ignoreBlocks = true
	page, err = global.LoadContext(context.Background(), "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.Blocked || len(page.Scripts) != 1 {
		t.Fatalf("ignoreBlocks load = blocked %v scripts %d", page.Blocked, len(page.Scripts))
	}
}

func TestNoscriptMetaRefreshRemoved(t *testing.T) {
	u := mustParse(t, "https://example.com/search")
	root := ParseHTML(`<html><head><noscript>
		<meta http-equiv="refresh" content="0;url=/httpservice/retry/enablejs">
	</noscript></head><body><p>results</p></body></html>`)
	page := &Page{URL: u, Base: u, Root: root}
	removeNoscript(root)
	if got := metaRefreshTarget(page); got != "" {
		t.Fatalf("meta refresh inside noscript was followed: %q", got)
	}
}

func TestNoscriptStylesRemovedWithJS(t *testing.T) {
	loader := NewLoader()
	loader.client = newTestHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("Content-Type", "text/html")
		body := `<html><head><noscript>
			<style>body { display: none }</style>
			<script>window.bad = true;</script>
		</noscript></head><body><p>visible</p></body></html>`
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(strings.NewReader(body)), Request: req,
		}, nil
	})})
	page, err := loader.LoadContext(context.Background(), "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Scripts) != 0 {
		t.Fatalf("noscript scripts were collected: %d", len(page.Scripts))
	}
	ly := BuildLayout(page.Root, page.Author, 40)
	body := page.Root.FindFirst("body")
	if body == nil || body.Style == nil || body.Style.Display == "none" {
		t.Fatal("noscript style hid the body")
	}
	if ly.Height == 0 {
		t.Fatal("page did not lay out")
	}
}

func TestLocalFilesRejected(t *testing.T) {
	if _, err := NewLoader().Load("file:///dev/zero", nil); err == nil {
		t.Fatal("local file was read as a page")
	}
	if _, err := ResolveURL("file:///etc/passwd", nil); err == nil {
		t.Fatal("file: URL was accepted")
	}
}

func TestRemotePageCannotLoadLocalFiles(t *testing.T) {
	loader := NewLoader()
	loader.noImages = false
	loader.client = newTestHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("Content-Type", "text/html")
		body := `<html><head><link rel="stylesheet" href="file:///etc/passwd"></head>
			<body><img src="file:///etc/passwd"></body></html>`
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(strings.NewReader(body)), Request: req,
		}, nil
	})})
	page, err := loader.LoadContext(context.Background(), "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	img := page.Root.FindFirst("img")
	if img == nil || !img.ImgFailed {
		t.Fatal("remote page was allowed to load a local file image")
	}
	if len(page.Author) != 0 {
		t.Fatalf("remote page loaded %d local-file stylesheet rules", len(page.Author))
	}
}

func TestOversizedResponseRejected(t *testing.T) {
	loader := NewLoader()
	loader.client = newTestHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("Content-Type", "text/html")
		body := strings.NewReader(strings.Repeat("x", maxResourceSize+1))
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(body), Request: req,
		}, nil
	})})
	if _, err := loader.Load("https://example.com/big", nil); !errors.Is(err, errResourceTooLarge) {
		t.Fatalf("error = %v, want errResourceTooLarge", err)
	}
}

func TestBlockedGeneric(t *testing.T) {
	loader := NewLoader()
	loader.client = newTestHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("Content-Type", "text/html")
		return &http.Response{
			StatusCode: http.StatusForbidden, Status: "403 Forbidden", Header: h,
			Body: io.NopCloser(strings.NewReader(`<html><body>Access Denied</body></html>`)), Request: req,
		}, nil
	})})
	page, err := loader.LoadContext(context.Background(), "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !page.Blocked || page.BlockReason != "access denied" {
		t.Fatalf("blocked=%v reason=%q", page.Blocked, page.BlockReason)
	}
}

func TestBMPDecodeRegistered(t *testing.T) {
	const fixture = "Qk1GAAAAAAAAADYAAAAoAAAAAgAAAAIAAAABABgAAAAAABAAAADEDgAAxA4AAAAAAAAAAAAA/wAAAP//AAAAAP8A/wAAAA=="
	data, err := base64.StdEncoding.DecodeString(fixture)
	if err != nil {
		t.Fatal(err)
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("BMP decode failed: %v", err)
	}
	if format != "bmp" {
		t.Fatalf("format = %q, want bmp", format)
	}
	if b := img.Bounds(); b.Dx() != 2 || b.Dy() != 2 {
		t.Fatalf("bounds = %v", b)
	}
}

func TestWebPDecodeRegistered(t *testing.T) {
	const fixture = "UklGRiwAAABXRUJQVlA4TB8AAAAvAUAAAB8gEEjaH3qN+RcQFPk/2vwHXyTgBwgR/Q8BAA=="
	data, err := base64.StdEncoding.DecodeString(fixture)
	if err != nil {
		t.Fatal(err)
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("WebP decode failed: %v", err)
	}
	if format != "webp" {
		t.Fatalf("format = %q, want webp", format)
	}
	if b := img.Bounds(); b.Dx() != 2 || b.Dy() != 2 {
		t.Fatalf("bounds = %v", b)
	}
}

func TestImageConfigLimit(t *testing.T) {
	if !imageConfigOK(image.Config{Width: 1000, Height: 1000}) {
		t.Error("normal image rejected")
	}
	if imageConfigOK(image.Config{Width: 10000, Height: 10000}) {
		t.Error("decompression bomb accepted")
	}
	if imageConfigOK(image.Config{}) {
		t.Error("empty image accepted")
	}
}

func TestBranding(t *testing.T) {
	for _, want := range []string{"MOB³"} {
		if !strings.Contains(startPageHTML, want) {
			t.Errorf("start page is missing %q", want)
		}
	}
	page, err := NewLoader().LoadContext(context.Background(), "about:home", nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.Root.FindFirst("article") == nil {
		t.Error("start page is not wrapped in an article for reader-style layout")
	}
	if len(page.Author) < len(readerRules) {
		t.Errorf("start page author rules = %d, want the reader stylesheet (%d)", len(page.Author), len(readerRules))
	}
	if hist := historyPageHTML(nil, -1); !strings.Contains(hist, "max-width: 900px") || !strings.Contains(hist, "margin: 8px auto") {
		t.Error("history page is not a narrow reading column")
	}
	ua := NewLoader().requestUserAgent()
	if ua != "MOB3/1.0 (+terminal; +https://mob3.net)" {
		t.Errorf("User-Agent = %q", ua)
	}
}

func TestResolveURL(t *testing.T) {
	base := mustParse(t, "https://example.com/a/b")
	cases := []struct {
		in, want string
		base     bool
	}{
		{"example.com", "https://example.com", false},
		{"https://go.dev/doc", "https://go.dev/doc", false},
		{"//cdn.example.com/x", "https://cdn.example.com/x", false},
		{"./doc", "https://example.com/a/doc", true},
		{"/root", "https://example.com/root", true},
		{"#frag", "https://example.com/a/b#frag", true},
		{"localhost:8080", "http://localhost:8080", false},
		{"about:start", "about:start", false},
	}
	for _, c := range cases {
		b := base
		if !c.base {
			b = nil
		}
		u, err := ResolveURL(c.in, b)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if u.String() != c.want {
			t.Errorf("%s => %s, want %s", c.in, u, c.want)
		}
	}
	if _, err := ResolveURL("javascript:alert(1)", nil); err == nil {
		t.Error("javascript URL should fail")
	}
}

func TestDecodeBodyCharsets(t *testing.T) {
	cases := []struct {
		name  string
		ctype string
		in    []byte
		want  string
	}{
		{"latin1", "text/html; charset=iso-8859-1", []byte{0xe9}, "é"},
		{"cp1252", "text/html; charset=windows-1252", []byte{0x93, 'h', 'i', 0x94}, "“hi”"},
		{"utf8", "text/html", []byte("hé"), "hé"},
		{"cp1250", "text/html; charset=windows-1250",
			[]byte{0xc8, 0x65, 0x73, 0x6b, 0xe1, 0x20, 0x63, 0xed, 0x72, 0x6b, 0x65, 0x76}, "Česká církev"},
		{"cp1251", "text/html; charset=windows-1251",
			[]byte{0xcf, 0xf0, 0xe8, 0xe2, 0xe5, 0xf2}, "Привет"},
		{"iso-8859-7", "text/html; charset=iso-8859-7",
			[]byte{0xc5, 0xeb, 0xeb, 0xdc, 0xe4, 0xe1}, "Ελλάδα"},
		{"iso-8859-2 alias", "text/html; charset=ISO_8859-2",
			[]byte{0x50, 0xf8, 0xed, 0x6c, 0x69, 0xb9, 0x20, 0xbe, 0x6c, 0x75, 0xbb, 0x6f, 0x75, 0xe8, 0x6b, 0xfd}, "Příliš žluťoučký"},
		{"meta charset", "text/html",
			append([]byte(`<meta charset=windows-1250>`), 0xc8, 0x65, 0x73, 0x6b, 0xe1), "<meta charset=windows-1250>Česká"},
		{"unknown", "text/html; charset=x-unknown", []byte("hé"), "hé"},
	}
	for _, c := range cases {
		if got := decodeBody(c.in, c.ctype); got != c.want {
			t.Errorf("%s = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestResolveInputSearch(t *testing.T) {
	cases := []struct {
		in, want string
		tmpl     string
	}{
		{"golang tutorial", "https://html.duckduckgo.com/html/?q=golang+tutorial", DefaultSearchURL},
		{"golang", "https://html.duckduckgo.com/html/?q=golang", DefaultSearchURL},
		{"example.com", "https://example.com", DefaultSearchURL},
		{"localhost:8080", "http://localhost:8080", DefaultSearchURL},
		{"https://go.dev/doc", "https://go.dev/doc", DefaultSearchURL},
		{"go.dev docs", "https://www.bing.com/search?q=go.dev+docs", "https://www.bing.com/search?q=%s"},
	}
	for _, c := range cases {
		u, err := ResolveInput(c.in, nil, c.tmpl)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if u.String() != c.want {
			t.Errorf("%s => %s, want %s", c.in, u, c.want)
		}
	}
	if _, err := ResolveInput("golang tutorial", nil, ""); err == nil {
		t.Error("disabled search should fail on a query")
	}
}

// TestLoaderSettingsConcurrent exercises the settings that about:config can
// change while navigation and image workers read them.
func TestLoaderSettingsConcurrent(t *testing.T) {
	l := NewLoader()
	im := image.NewRGBA(image.Rect(0, 0, 64, 64))
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			l.setUserAgent("agent")
			l.setSearchURL("https://search.example/?q=%s")
			l.setImageCap(32, 32)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_ = l.requestUserAgent()
			_ = l.searchTemplate()
			_, _ = l.imageCap()
			_ = l.fitImage(im)
		}
	}()
	wg.Wait()
}

func TestResolveRejectsLocalFiles(t *testing.T) {
	for _, in := range []string{"file:///etc/passwd", "FILE:///tmp/x", "file:/tmp/x"} {
		if u, err := ResolveURL(in, nil); err == nil {
			t.Errorf("%s resolved to %s, want rejection", in, u)
		}
	}
}

// TestResolveDoesNotProbeBarePaths pins that a bare path is never stat'ed on
// disk, even when a file with that name exists in the working directory.
func TestResolveDoesNotProbeBarePaths(t *testing.T) {
	dir := t.TempDir()
	const name = "local-only-page.html"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("<p>x</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	base := mustParse(t, "https://example.com/dir/")
	for _, b := range []*url.URL{base, nil} {
		u, err := ResolveURL(name, b)
		if err != nil {
			t.Fatal(err)
		}
		if u.Scheme == "file" {
			t.Fatalf("bare path resolved to local file %s", u)
		}
	}
}

func mustParse(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestLoadDataPageWithImage(t *testing.T) {
	im := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			im.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	imgURL := dataImageURL(t, im)
	htmlSrc := `<html><head><title>T</title></head><body><p>hi</p><img src="` + imgURL + `"></body></html>`
	loader := NewLoader()
	loader.noImages = false
	page, err := loader.Load("data:text/html,"+url.PathEscape(htmlSrc), nil)
	if err != nil {
		t.Fatal(err)
	}
	imgNode := page.Root.FindFirst("img")
	if imgNode == nil || imgNode.Img == nil {
		t.Fatal("image not decoded")
	}
	if page.Title != "T" {
		t.Fatalf("title = %q", page.Title)
	}
	ly := BuildLayout(page.Root, page.Author, 40)
	found := false
	for _, fr := range ly.Frags {
		if fr.Kind == fragImage && fr.W == 2 && fr.H == 1 {
			found = true
		}
	}
	if !found {
		t.Fatal("image fragment missing or wrong size")
	}
}

func TestImagesDisabledByDefault(t *testing.T) {
	imgURL := dataImageURL(t, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	src := `<html><body><img src="` + imgURL + `" alt="logo"></body></html>`
	loader := NewLoader()
	page, err := loader.Load("data:text/html,"+url.PathEscape(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	if img := page.Root.FindFirst("img"); img == nil || img.Img != nil {
		t.Fatal("image was loaded even though images are disabled by default")
	}
	for _, r := range loader.requestSnapshot() {
		if r.Kind == "img" {
			t.Fatalf("image request was made: %+v", r)
		}
	}
	ly := BuildLayout(page.Root, page.Author, 40)
	var text strings.Builder
	for _, fr := range ly.Frags {
		if fr.Kind == fragText {
			text.WriteString(fr.Text)
		}
	}
	if !strings.Contains(text.String(), "[image: logo]") {
		t.Fatalf("alt fallback missing from layout: %q", text.String())
	}
}

func TestPageKeepsRawHTML(t *testing.T) {
	source := "<!DOCTYPE html>\n<html><body><p>raw source</p></body></html>"
	page, err := NewLoader().Load("data:text/html,"+url.PathEscape(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.RawHTML != source {
		t.Fatalf("RawHTML = %q, want the fetched source", page.RawHTML)
	}
	about, err := NewLoader().Load("about:home", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(about.RawHTML, "MOB³") {
		t.Fatalf("about page RawHTML is empty: %q", about.RawHTML)
	}
}

func cookieViewMap(cookies []protocol.CookieView) map[string]string {
	m := make(map[string]string, len(cookies))
	for _, c := range cookies {
		m[c.Name] = c.Value
	}
	return m
}

func cookieMap(cookies []*http.Cookie) map[string]string {
	m := make(map[string]string, len(cookies))
	for _, c := range cookies {
		m[c.Name] = c.Value
	}
	return m
}

func TestCookieJarVisibility(t *testing.T) {
	jar := newCookieJar()
	u := mustURL(t, "https://example.com/foo/bar")
	jar.SetCookies(u, []*http.Cookie{
		{Name: "session", Value: "secret", Path: "/", HttpOnly: true},
		{Name: "theme", Value: "dark", Path: "/"},
		{Name: "secure", Value: "yes", Path: "/", Secure: true},
		{Name: "path", Value: "foo", Path: "/foo"},
	})

	visible := cookieViewMap(jar.DocumentCookieValues(u))
	if _, ok := visible["session"]; ok {
		t.Fatal("HttpOnly cookie was visible to document.cookie")
	}
	for _, name := range []string{"theme", "secure", "path"} {
		if _, ok := visible[name]; !ok {
			t.Errorf("document.cookie is missing %q: %v", name, visible)
		}
	}

	httpURL := mustURL(t, "http://example.com/foo/bar")
	if _, ok := cookieViewMap(jar.DocumentCookieValues(httpURL))["secure"]; ok {
		t.Fatal("Secure cookie was visible over HTTP")
	}
	root := mustURL(t, "https://example.com/")
	if _, ok := cookieViewMap(jar.DocumentCookieValues(root))["path"]; ok {
		t.Fatal("Path-scoped cookie leaked to the root path")
	}
}

func TestCookieJarHttpOnlyReplacement(t *testing.T) {
	jar := newCookieJar()
	u := mustURL(t, "https://example.com/")
	jar.SetCookies(u, []*http.Cookie{{Name: "token", Value: "visible", Path: "/"}})
	jar.SetCookies(u, []*http.Cookie{{Name: "token", Value: "hidden", Path: "/", HttpOnly: true}})

	if _, ok := cookieViewMap(jar.DocumentCookieValues(u))["token"]; ok {
		t.Fatal("replaced HttpOnly cookie remained visible to document.cookie")
	}
	if got := cookieMap(jar.Cookies(u))["token"]; got != "hidden" {
		t.Fatalf("HTTP cookie jar token = %q, want hidden", got)
	}
}

func TestLocalHostClassification(t *testing.T) {
	local := []string{
		"127.0.0.1", "127.1.2.3", "10.1.2.3", "172.16.0.1", "172.31.255.255",
		"192.168.1.1", "169.254.1.1", "100.64.0.1", "100.127.255.255",
		"192.0.0.1", "198.18.0.1", "198.19.255.255", "240.0.0.1",
		"0.0.0.0", "255.255.255.255", "::1", "fc00::1", "fdff::1", "fe80::1",
		"fe80::1%eth0", "::", "localhost", "foo.localhost", "printer.local", "LOCAL",
	}
	for _, host := range local {
		if !isLocalHost(host) {
			t.Errorf("isLocalHost(%q) = false, want true", host)
		}
	}
	public := []string{
		"8.8.8.8", "1.1.1.1", "172.15.0.1", "172.32.0.1", "100.63.255.255",
		"100.128.0.0", "192.0.1.1", "198.20.0.1", "example.com",
		"notlocalhost.com", "local.example.com",
	}
	for _, host := range public {
		if isLocalHost(host) {
			t.Errorf("isLocalHost(%q) = true, want false", host)
		}
	}
}

func TestLocalNetworkControl(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:80", "[::1]:8080", "192.168.1.1:443", "10.0.0.1:80"} {
		if err := localNetworkControl("tcp", addr, nil); !errors.Is(err, errLocalNetworkBlocked) {
			t.Errorf("localNetworkControl(%q) = %v, want blocked", addr, err)
		}
	}
	for _, addr := range []string{"8.8.8.8:443", "1.1.1.1:80"} {
		if err := localNetworkControl("tcp", addr, nil); err != nil {
			t.Errorf("localNetworkControl(%q) = %v, want allowed", addr, err)
		}
	}
}

func TestLocalNetworkGuardBlocksPublicSubresource(t *testing.T) {
	l := NewLoader()
	reached := false
	l.client.HTTP().Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		reached = true
		return nil, errors.New("transport should not run")
	})
	page := mustURL(t, "https://example.com/")
	ctx := withRequestKind(context.Background(), "img")
	ctx = withRequestPage(ctx, page, false)
	if _, _, _, _, err := l.fetchCtx(ctx, mustURL(t, "http://127.0.0.1:8080/x")); !errors.Is(err, errLocalNetworkBlocked) {
		t.Fatalf("fetch error = %v, want local-network block", err)
	}
	if reached {
		t.Fatal("blocked request reached the transport")
	}
	recs := l.requestSnapshot()
	if len(recs) != 1 || !strings.Contains(recs[0].Err, "local-network") {
		t.Fatalf("blocked request log = %+v, want a local-network error", recs)
	}
}

func TestLocalNetworkGuardAllowsLocalPageAndNavigation(t *testing.T) {
	l := NewLoader()
	l.client.HTTP().Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("Content-Type", "text/plain")
		h.Set("Access-Control-Allow-Origin", "*")
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(strings.NewReader("ok")), Request: req,
		}, nil
	})
	localPage := mustURL(t, "http://127.0.0.1:3000/")
	ctx := withRequestKind(context.Background(), "img")
	ctx = withRequestPage(ctx, localPage, true)
	if _, _, _, _, err := l.fetchCtx(ctx, mustURL(t, "http://127.0.0.1:8080/x")); err != nil {
		t.Fatalf("local page subresource was blocked: %v", err)
	}
	// A top-level navigation carries no page meta and stays allowed.
	if _, _, _, _, err := l.fetchCtx(context.Background(), mustURL(t, "http://127.0.0.1:8080/")); err != nil {
		t.Fatalf("top-level navigation was blocked: %v", err)
	}
}

func TestLoadRequestMarksLocalPage(t *testing.T) {
	l := NewLoader()
	l.client.HTTP().Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("Content-Type", "text/html")
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(strings.NewReader("<html><body>ok</body></html>")), Request: req,
		}, nil
	})
	local, err := l.Load("http://127.0.0.1:8080/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !local.Local {
		t.Fatal("loopback document is not marked local")
	}
	public, err := l.Load("https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if public.Local {
		t.Fatal("public document is marked local")
	}
}

func TestXHRLocalNetworkGuard(t *testing.T) {
	b := NewBrowser(40, 10)
	loader := NewLoader()
	b.loader = loader
	reached := false
	loader.client.HTTP().Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		reached = true
		h := make(http.Header)
		h.Set("Content-Type", "text/plain")
		h.Set("Access-Control-Allow-Origin", "*")
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(strings.NewReader("ok")), Request: req,
		}, nil
	})
	page := newJSPage(`<html><body></body></html>`, "")
	j := newJSRuntime(b, page)
	if _, _, _, _, _, err := j.HTTPXHR("fetch", "GET", mustURL(t, "http://127.0.0.1:8080/x"), nil, nil); !errors.Is(err, errLocalNetworkBlocked) {
		t.Fatalf("public-page XHR error = %v, want local-network block", err)
	}
	if reached {
		t.Fatal("blocked public-page XHR reached the transport")
	}
	// The same target is reachable from a page that itself loaded locally.
	page.Local = true
	page.URL = mustURL(t, "http://127.0.0.1:3000/")
	page.Base = page.URL
	if _, _, _, _, _, err := j.HTTPXHR("fetch", "GET", mustURL(t, "http://127.0.0.1:8080/x"), nil, nil); err != nil {
		t.Fatalf("local-page XHR was blocked: %v", err)
	}
	if !reached {
		t.Fatal("local-page XHR did not reach the transport")
	}
}

func TestLocalNetworkDialContext(t *testing.T) {
	blocked := withRequestPage(context.Background(), mustURL(t, "https://example.com/"), false)
	if _, err := localNetworkDialContext(blocked, "tcp", "127.0.0.1:1"); !errors.Is(err, errLocalNetworkBlocked) {
		t.Fatalf("literal loopback dial = %v, want blocked", err)
	}
	// A name that resolves locally is caught after DNS resolution.
	if _, err := localNetworkDialContext(blocked, "tcp", "localhost:1"); !errors.Is(err, errLocalNetworkBlocked) {
		t.Fatalf("localhost dial = %v, want blocked", err)
	}
	// A top-level navigation is not guarded; the dial is attempted normally.
	if _, err := localNetworkDialContext(context.Background(), "tcp", "127.0.0.1:1"); errors.Is(err, errLocalNetworkBlocked) {
		t.Fatal("top-level dial used the local-network guard")
	}
}

func TestLocalNetworkDialContextFromRequest(t *testing.T) {
	tr := &http.Transport{DialContext: localNetworkDialContext}
	target := mustURL(t, "http://localhost:1/")
	ctx := withRequestPage(context.Background(), mustURL(t, "https://example.com/"), false)
	ctx = withRequestTarget(ctx, target)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.RoundTrip(req); !errors.Is(err, errLocalNetworkBlocked) {
		t.Fatalf("RoundTrip error = %v, want local-network block", err)
	}
}

func TestLocalNetworkGuardSkipsLocalCacheForPublicPage(t *testing.T) {
	l := NewLoader()
	u := mustURL(t, "https://intranet.example/app.css")
	l.cache.putBytes(u.String(), []byte("local-secret"), "text/css", u.String(), true)
	reached := false
	l.client.HTTP().Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		reached = true
		h := make(http.Header)
		h.Set("Content-Type", "text/css")
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(strings.NewReader("network")), Request: req,
		}, nil
	})
	public := withRequestKind(context.Background(), "css")
	public = withRequestPage(public, mustURL(t, "https://example.com/"), false)
	data, _, _, _, err := l.fetchCtx(public, u)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "network" {
		t.Fatalf("public page read cached local body %q", data)
	}
	if !reached {
		t.Fatal("public page skipped the local cache entry but made no request")
	}

	// A local document may use the same entry without a request.
	local := withRequestKind(context.Background(), "css")
	local = withRequestPage(local, mustURL(t, "http://127.0.0.1:3000/"), true)
	l.cache.putBytes(u.String(), []byte("local-secret"), "text/css", u.String(), true)
	reached = false
	data, _, _, _, err = l.fetchCtx(local, u)
	if err != nil || string(data) != "local-secret" || reached {
		t.Fatalf("local page cache hit = %q, %v (request made: %v)", data, err, reached)
	}
}

func TestMixedContentBlocksActiveSubresources(t *testing.T) {
	l := NewLoader()
	reached := false
	l.client.HTTP().Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		reached = true
		return nil, errors.New("transport should not run")
	})
	secure := withRequestPage(context.Background(), mustURL(t, "https://example.com/"), false)
	for _, kind := range []string{"js", "css"} {
		ctx := withRequestKind(secure, kind)
		if _, _, _, _, err := l.fetchCtx(ctx, mustURL(t, "http://example.com/x")); !errors.Is(err, errMixedContentBlocked) {
			t.Fatalf("%s mixed content error = %v, want blocked", kind, err)
		}
	}
	if reached {
		t.Fatal("blocked mixed content reached the transport")
	}
}

func TestMixedContentUpgradesImages(t *testing.T) {
	l := NewLoader()
	var got string
	l.client.HTTP().Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		got = req.URL.String()
		h := make(http.Header)
		h.Set("Content-Type", "image/png")
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(strings.NewReader("png")), Request: req,
		}, nil
	})
	secure := withRequestPage(context.Background(), mustURL(t, "https://example.com/"), false)
	ctx := withRequestKind(secure, "img")
	if _, _, _, _, err := l.fetchCtx(ctx, mustURL(t, "http://cdn.example/a.png")); err != nil {
		t.Fatalf("image upgrade failed: %v", err)
	}
	if got != "https://cdn.example/a.png" {
		t.Fatalf("image URL = %q, want the HTTPS upgrade", got)
	}
	// An IP-literal image cannot be upgraded and is blocked instead.
	if _, _, _, _, err := l.fetchCtx(ctx, mustURL(t, "http://93.184.216.34/a.png")); !errors.Is(err, errMixedContentBlocked) {
		t.Fatalf("IP-literal image error = %v, want blocked", err)
	}
}

func TestMixedContentAllowsInsecureDocument(t *testing.T) {
	l := NewLoader()
	l.client.HTTP().Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("Content-Type", "text/javascript")
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(strings.NewReader("1")), Request: req,
		}, nil
	})
	page := withRequestPage(context.Background(), mustURL(t, "http://example.com/"), false)
	ctx := withRequestKind(page, "js")
	if _, _, _, _, err := l.fetchCtx(ctx, mustURL(t, "http://cdn.example/x.js")); err != nil {
		t.Fatalf("HTTP page subresource was blocked: %v", err)
	}
}

func TestMixedContentBlocksDowngradeRedirect(t *testing.T) {
	l := NewLoader()
	l.client.HTTP().Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("Location", "http://example.com/next.js")
		return &http.Response{
			StatusCode: http.StatusFound, Status: "302 Found", Header: h,
			Body: io.NopCloser(strings.NewReader("")), Request: req,
		}, nil
	})
	secure := withRequestPage(context.Background(), mustURL(t, "https://example.com/"), false)
	ctx := withRequestKind(secure, "js")
	_, _, _, _, err := l.fetchCtx(ctx, mustURL(t, "https://example.com/start.js"))
	if !errors.Is(err, errMixedContentBlocked) {
		t.Fatalf("downgrade redirect error = %v, want blocked", err)
	}
}

func TestXHRMixedContent(t *testing.T) {
	b := NewBrowser(40, 10)
	loader := NewLoader()
	b.loader = loader
	reached := false
	loader.client.HTTP().Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		reached = true
		h := make(http.Header)
		h.Set("Content-Type", "text/plain")
		h.Set("Access-Control-Allow-Origin", "*")
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(strings.NewReader("ok")), Request: req,
		}, nil
	})
	page := newJSPage(`<html><body></body></html>`, "")
	j := newJSRuntime(b, page)
	if _, _, _, _, _, err := j.HTTPXHR("fetch", "GET", mustURL(t, "http://example.com/x"), nil, nil); !errors.Is(err, errMixedContentBlocked) {
		t.Fatalf("secure-page XHR error = %v, want mixed-content block", err)
	}
	if reached {
		t.Fatal("blocked XHR reached the transport")
	}
	// The same request is allowed from an insecure document.
	page.URL = mustURL(t, "http://example.com/")
	if _, _, _, _, _, err := j.HTTPXHR("fetch", "GET", mustURL(t, "http://example.com/x"), nil, nil); err != nil {
		t.Fatalf("insecure-page XHR was blocked: %v", err)
	}
	if !reached {
		t.Fatal("insecure-page XHR did not reach the transport")
	}
}

func TestRemoteAddrLocal(t *testing.T) {
	if !remoteAddrLocal(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 80}) {
		t.Fatal("loopback peer not local")
	}
	if remoteAddrLocal(&net.TCPAddr{IP: net.ParseIP("8.8.8.8"), Port: 443}) {
		t.Fatal("public peer marked local")
	}
}

func TestCookieJarDocumentCookieCannotReplaceHttpOnly(t *testing.T) {
	jar := newCookieJar()
	u := mustURL(t, "https://example.com/foo/bar")
	jar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "server", Path: "/", HttpOnly: true}})

	jar.SetDocumentCookie(u, &http.Cookie{Name: "session", Value: "evil", Path: "/"})
	if got := cookieMap(jar.Cookies(u))["session"]; got != "server" {
		t.Fatalf("document.cookie replaced the HttpOnly value with %q", got)
	}
	if _, ok := cookieViewMap(jar.DocumentCookieValues(u))["session"]; ok {
		t.Fatal("replaced HttpOnly cookie became visible")
	}

	jar.SetDocumentCookie(u, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1})
	if got := cookieMap(jar.Cookies(u))["session"]; got != "server" {
		t.Fatalf("document.cookie deleted the HttpOnly cookie (got %q)", got)
	}
}

func TestCookieJarDocumentCookieOtherPath(t *testing.T) {
	jar := newCookieJar()
	u := mustURL(t, "https://example.com/foo/bar")
	jar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "server", Path: "/", HttpOnly: true}})
	jar.SetDocumentCookie(u, &http.Cookie{Name: "session", Value: "page", Path: "/foo"})
	if got := cookieViewMap(jar.DocumentCookieValues(u))["session"]; got != "page" {
		t.Fatalf("script cookie on another path = %q, want page", got)
	}
	root := cookieMap(jar.Cookies(mustURL(t, "https://example.com/")))
	if root["session"] != "server" {
		t.Fatalf("HttpOnly cookie on / = %q, want server", root["session"])
	}
}

func TestCookieJarHttpOnlyDisplayTracksIdentity(t *testing.T) {
	jar := newCookieJar()
	u := mustURL(t, "https://example.com/")
	jar.SetCookies(u, []*http.Cookie{{Name: "sid", Value: "a", Path: "/", HttpOnly: true}})
	jar.SetCookies(u, []*http.Cookie{{Name: "sid", Value: "b", Path: "/", HttpOnly: true}})
	if got := httpOnlyValue(jar.CookieViews(u), "sid"); got != "b" {
		t.Fatalf("displayed HttpOnly value = %q, want b", got)
	}
	// A server may replace the cookie without the flag; the inspector follows.
	jar.SetCookies(u, []*http.Cookie{{Name: "sid", Value: "c", Path: "/"}})
	if got := httpOnlyValue(jar.CookieViews(u), "sid"); got != "" {
		t.Fatalf("server-demoted cookie is still marked HttpOnly (value %q)", got)
	}
}

func httpOnlyValue(cookies []protocol.CookieView, name string) string {
	for _, c := range cookies {
		if c.Name == name && c.HttpOnly {
			return c.Value
		}
	}
	return ""
}

func TestCookieJarPublicSuffix(t *testing.T) {
	jar := newCookieJar()
	u := mustURL(t, "https://example.com/")
	jar.SetCookies(u, []*http.Cookie{{Name: "bad", Value: "1", Domain: "com", Path: "/"}})
	if _, ok := cookieMap(jar.Cookies(u))["bad"]; ok {
		t.Fatal("cookie for a public suffix was accepted")
	}
}

func TestCookieJarDeletion(t *testing.T) {
	jar := newCookieJar()
	u := mustURL(t, "https://example.com/")
	jar.SetCookies(u, []*http.Cookie{{Name: "gone", Value: "1", Path: "/"}})
	jar.SetCookies(u, []*http.Cookie{{Name: "gone", Value: "", Path: "/", MaxAge: -1}})
	if _, ok := cookieViewMap(jar.DocumentCookieValues(u))["gone"]; ok {
		t.Fatal("deleted cookie remained visible")
	}
}

func TestJSDocumentCookie(t *testing.T) {
	loader := NewLoader()
	var sentCookie string
	loader.client.HTTP().Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		sentCookie = req.Header.Get("Cookie")
		h := make(http.Header)
		h.Set("Content-Type", "text/plain")
		h.Set("X-Echo", sentCookie)
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(strings.NewReader("ok")), Request: req,
		}, nil
	})
	page := newJSPage(`<html><body></body></html>`, `
		var before = document.cookie;
		document.cookie = "client=1; Path=/";
		document.cookie = "evil=1; HttpOnly";
		var x = new XMLHttpRequest();
		x.open("GET", "/api");
		x.onload = function () {
			document.title = [
				before, document.cookie, String(navigator.cookieEnabled),
				x.getResponseHeader("X-Echo")
			].join("|");
		};
		x.send();
	`)
	b := NewBrowser(40, 10)
	b.loader = loader
	loader.cookies.SetCookies(page.URL, []*http.Cookie{
		{Name: "theme", Value: "dark", Path: "/"},
		{Name: "session", Value: "secret", Path: "/", HttpOnly: true},
	})
	b.startJS(page)
	fields := strings.Split(page.Title, "|")
	if len(fields) != 4 {
		t.Fatalf("title = %q, want four fields", page.Title)
	}
	if fields[0] != "theme=dark" {
		t.Errorf("initial document.cookie = %q", fields[0])
	}
	if strings.Contains(fields[0], "session") || strings.Contains(fields[1], "session") {
		t.Fatalf("HttpOnly cookie leaked to document.cookie: %q", page.Title)
	}
	for _, want := range []string{"theme=dark", "client=1", "evil=1"} {
		if !strings.Contains(fields[1], want) {
			t.Errorf("document.cookie = %q, want %q", fields[1], want)
		}
	}
	if fields[2] != "true" {
		t.Errorf("navigator.cookieEnabled = %q", fields[2])
	}
	if !strings.Contains(sentCookie, "session=secret") ||
		!strings.Contains(sentCookie, "theme=dark") ||
		!strings.Contains(sentCookie, "client=1") {
		t.Errorf("request Cookie header = %q", sentCookie)
	}
	if !strings.Contains(fields[3], "session=secret") {
		t.Errorf("XHR echoed Cookie header = %q", fields[3])
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// dataImageURL encodes an image as a data: URL so tests never touch the disk.
func dataImageURL(t *testing.T, im image.Image) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, im); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

type brTransport struct{}

func (brTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	h := make(http.Header)
	h.Set("Content-Type", "text/html")
	h.Set("Content-Encoding", "br")
	return &http.Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1", Header: h,
		Body: io.NopCloser(strings.NewReader("not really brotli")), Request: req,
	}, nil
}

func TestUnsupportedContentEncoding(t *testing.T) {
	loader := NewLoader()
	loader.client = newTestHTTPClient(&http.Client{Transport: brTransport{}})
	_, err := loader.Load("https://example.com/", nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported Content-Encoding") {
		t.Fatalf("err = %v, want an unsupported encoding error", err)
	}
}

func TestDeferredImagesLoadLater(t *testing.T) {
	imgURL := dataImageURL(t, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	src := `<html><body><img src="` + imgURL + `" alt="pic"></body></html>`

	loader := NewLoader()
	loader.noImages = false
	loader.deferImages = true
	page, err := loader.Load("data:text/html,"+url.PathEscape(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	img := page.Root.FindFirst("img")
	if img == nil {
		t.Fatal("image missing")
	}
	if img.Img != nil || img.ImgFailed {
		t.Fatal("deferred load fetched the image during Load")
	}

	tasks := loader.imageTasks(page.Root, page.Base)
	if len(tasks) != 1 {
		t.Fatalf("image tasks = %d, want 1", len(tasks))
	}
	results := loader.loadImageTaskResults(context.Background(), tasks)
	if len(results) != 1 || results[0].Img == nil || results[0].Failed {
		t.Fatalf("background results = %+v", results)
	}

	// Applying the batch installs the decoded image without a page reload.
	b := NewBrowser(60, 20)
	b.loader = loader
	b.Page = page
	b.URL = page.URL
	b.LoadSeq = 1
	b.DeferredImages = true
	if !b.applyDeferredImageBatch(imageBatch{Seq: 1, Results: results, Done: true}) {
		t.Fatal("batch did not apply")
	}
	if img.Img == nil {
		t.Fatal("image not installed")
	}
	if b.DeferredImages {
		t.Fatal("deferred flag stayed set")
	}
	found := false
	for _, f := range b.Layout.Frags {
		if f.Kind == fragImage && f.Node == img {
			found = true
		}
	}
	if !found {
		t.Fatal("layout has no image fragment after applying the batch")
	}
}

func TestLoaderResolvesCSSImports(t *testing.T) {
	files := map[string]string{
		"https://fixture.test/page.html": `<html><head><link rel="stylesheet" href="a.css"></head><body><p class="a">a</p><p class="b">b</p></body></html>`,
		"https://fixture.test/a.css":     `@import "b.css"; .a{color:#ff0000}`,
		"https://fixture.test/b.css":     `.b{color:#0000ff}`,
	}
	loader := NewLoader()
	loader.client = newTestHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, ok := files[req.URL.String()]
		if !ok {
			return nil, errors.New("unexpected request " + req.URL.String())
		}
		h := make(http.Header)
		if strings.HasSuffix(req.URL.Path, ".css") {
			h.Set("Content-Type", "text/css")
		} else {
			h.Set("Content-Type", "text/html")
		}
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(strings.NewReader(body)), Request: req,
		}, nil
	})})
	page, err := loader.Load("https://fixture.test/page.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Author) != 2 {
		t.Fatalf("author rules = %d, want imported + local", len(page.Author))
	}
	ly := BuildLayout(page.Root, page.Author, 40)
	a := page.Root.FindAll(func(n *Node) bool { return n.Attr("class") == "a" })[0]
	bb := page.Root.FindAll(func(n *Node) bool { return n.Attr("class") == "b" })[0]
	if ly.Styles[a].Color.R != 255 {
		t.Fatalf("local rule not applied: %+v", ly.Styles[a].Color)
	}
	if ly.Styles[bb].Color.B != 255 {
		t.Fatalf("imported rule not applied: %+v", ly.Styles[bb].Color)
	}
}

func TestPickSrcset(t *testing.T) {
	cases := []struct {
		srcset string
		pref   int
		want   string
	}{
		{"a.jpg 240w, b.jpg 480w, c.jpg 800w", 320, "b.jpg"},
		{"a.jpg 240w, b.jpg 480w, c.jpg 800w", 1024, "c.jpg"},
		{"a.jpg 1x, b.jpg 2x", 320, "a.jpg"},
		{"a.jpg", 320, "a.jpg"},
		{"", 320, ""},
	}
	for _, c := range cases {
		if got := pickSrcset(c.srcset, c.pref); got != c.want {
			t.Errorf("pickSrcset(%q, %d) = %q, want %q", c.srcset, c.pref, got, c.want)
		}
	}
}

func TestImageSourcePictureAndDataAttrs(t *testing.T) {
	pic := &Node{Type: ElementNode, Tag: "picture"}
	source := &Node{Type: ElementNode, Tag: "source", Parent: pic,
		Attrs: []Attr{{Key: "srcset", Value: "mobile.jpg 480w"}, {Key: "media", Value: "(max-width: 500px)"}}}
	img := &Node{Type: ElementNode, Tag: "img", Parent: pic,
		Attrs: []Attr{{Key: "src", Value: "fallback.jpg"}}}
	pic.Children = []*Node{source, img}

	if got := imageSource(img, 400); got != "mobile.jpg" {
		t.Fatalf("mobile picture = %q", got)
	}
	if got := imageSource(img, 1600); got != "fallback.jpg" {
		t.Fatalf("desktop picture = %q", got)
	}

	lazy := &Node{Type: ElementNode, Tag: "img",
		Attrs: []Attr{{Key: "data-src", Value: "lazy.jpg"}}}
	if got := imageSource(lazy, 800); got != "lazy.jpg" {
		t.Fatalf("data-src = %q", got)
	}

	// srcset wins over src.
	both := &Node{Type: ElementNode, Tag: "img",
		Attrs: []Attr{{Key: "src", Value: "small.jpg"}, {Key: "srcset", Value: "big.jpg 800w"}}}
	if got := imageSource(both, 800); got != "big.jpg" {
		t.Fatalf("srcset preference = %q", got)
	}
}
