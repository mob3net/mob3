package browser

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpPage(t *testing.T) {
	p, err := NewLoader().Load("about:help", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Title, "Help") {
		t.Errorf("title = %q", p.Title)
	}
	text := p.Root.TextContent()
	for _, want := range []string{"JavaScript console", "open a URL", "Mouse", "Try it", "show an alert"} {
		if !strings.Contains(text, want) {
			t.Errorf("help page is missing %q", want)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLoaderPOST(t *testing.T) {
	var gotMethod, gotBody, gotCT string
	loader := NewLoader()
	loader.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
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
	})}
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
	loader.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("Content-Type", "text/html")
		body := `<html><head><title>Just a moment...</title></head><body>
			<div id="cf-chl-widget"></div><script src="/challenge-platform/x.js"></script>
		</body></html>`
		return &http.Response{
			StatusCode: http.StatusForbidden, Status: "403 Forbidden", Header: h,
			Body: io.NopCloser(strings.NewReader(body)), Request: req,
		}, nil
	})}
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
	loader.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
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
	})}
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

func TestDeviceFilesRejected(t *testing.T) {
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("/dev/zero not available")
	}
	if _, err := NewLoader().Load("file:///dev/zero", nil); err == nil {
		t.Fatal("device file was read as a page")
	}
}

func TestLocalPageCanLoadLocalFiles(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "p.png")
	f, err := os.Create(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	htmlPath := filepath.Join(dir, "page.html")
	if err := os.WriteFile(htmlPath, []byte(`<html><body><img src="p.png"></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	page, err := NewLoader().Load("file://"+htmlPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	img := page.Root.FindFirst("img")
	if img == nil || img.Img == nil {
		t.Fatal("local page could not load its local image")
	}
}

func TestRemotePageCannotLoadLocalFiles(t *testing.T) {
	loader := NewLoader()
	loader.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("Content-Type", "text/html")
		body := `<html><head><link rel="stylesheet" href="file:///etc/passwd"></head>
			<body><img src="file:///etc/passwd"></body></html>`
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(strings.NewReader(body)), Request: req,
		}, nil
	})}
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

func TestBlockedGeneric(t *testing.T) {
	loader := NewLoader()
	loader.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("Content-Type", "text/html")
		return &http.Response{
			StatusCode: http.StatusForbidden, Status: "403 Forbidden", Header: h,
			Body: io.NopCloser(strings.NewReader(`<html><body>Access Denied</body></html>`)), Request: req,
		}, nil
	})}
	page, err := loader.LoadContext(context.Background(), "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !page.Blocked || page.BlockReason != "access denied" {
		t.Fatalf("blocked=%v reason=%q", page.Blocked, page.BlockReason)
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
	ua := NewLoader().userAgent
	if !strings.HasPrefix(ua, "Mozilla/5.0") || !strings.Contains(ua, "MOB3/1.0") {
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
	if got := decodeBody([]byte{0xe9}, "text/html; charset=iso-8859-1"); got != "é" {
		t.Fatalf("latin1 = %q", got)
	}
	if got := decodeBody([]byte{0x93, 'h', 'i', 0x94}, "text/html; charset=windows-1252"); got != "“hi”" {
		t.Fatalf("cp1252 = %q", got)
	}
	if got := decodeBody([]byte("hé"), "text/html"); got != "hé" {
		t.Fatalf("utf8 = %q", got)
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

func TestResolveLocalFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "page.html")
	if err := os.WriteFile(p, []byte("<p>x</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := ResolveURL(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "file" || u.Path != p {
		t.Fatalf("got %+v", u)
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

func TestLoadLocalPageWithImage(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "p.png")
	f, err := os.Create(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	im := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			im.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	if err := png.Encode(f, im); err != nil {
		t.Fatal(err)
	}
	f.Close()
	htmlPath := filepath.Join(dir, "p.html")
	os.WriteFile(htmlPath, []byte(`<html><head><title>T</title></head><body><p>hi</p><img src="p.png"></body></html>`), 0o644)
	loader := NewLoader()
	page, err := loader.Load("file://"+htmlPath, nil)
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
