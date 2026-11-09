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
	for _, want := range []string{"JavaScript console", "open a URL", "Mouse", "Try it", "show an alert", "Command line", "-dump", "-width", "-graphics", "-no-js", "-ignore-blocks", "-bookmarks", "-js-memory", "-text-scale", "-color", "-no-color", "-ua", "-search", "-cpuprofile", "-version", "-help"} {
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
	if link := page.Root.FindFirst("a"); link == nil || link.Attr("href") != "about:retry" {
		t.Fatal("blocked page is missing the Try anyway link")
	}
	if !strings.Contains(page.Root.TextContent(), "Try anyway") {
		t.Fatal("Try anyway label missing")
	}
}

func TestRequestLogRecordsPageResources(t *testing.T) {
	loader := NewLoader()
	loader.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
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
	})}
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
		loader.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			h := make(http.Header)
			h.Set("Content-Type", "text/html")
			body := `<html><head><title>Just a moment...</title></head><body>
				<div id="cf-chl-widget"></div><script>window.challenge = true;</script>
			</body></html>`
			return &http.Response{
				StatusCode: http.StatusForbidden, Status: "403 Forbidden", Header: h,
				Body: io.NopCloser(strings.NewReader(body)), Request: req,
			}, nil
		})}
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

	visible := cookieMap(jar.documentCookies(u))
	if _, ok := visible["session"]; ok {
		t.Fatal("HttpOnly cookie was visible to document.cookie")
	}
	for _, name := range []string{"theme", "secure", "path"} {
		if _, ok := visible[name]; !ok {
			t.Errorf("document.cookie is missing %q: %v", name, visible)
		}
	}

	httpURL := mustURL(t, "http://example.com/foo/bar")
	if _, ok := cookieMap(jar.documentCookies(httpURL))["secure"]; ok {
		t.Fatal("Secure cookie was visible over HTTP")
	}
	root := mustURL(t, "https://example.com/")
	if _, ok := cookieMap(jar.documentCookies(root))["path"]; ok {
		t.Fatal("Path-scoped cookie leaked to the root path")
	}
}

func TestCookieJarHttpOnlyReplacement(t *testing.T) {
	jar := newCookieJar()
	u := mustURL(t, "https://example.com/")
	jar.SetCookies(u, []*http.Cookie{{Name: "token", Value: "visible", Path: "/"}})
	jar.SetCookies(u, []*http.Cookie{{Name: "token", Value: "hidden", Path: "/", HttpOnly: true}})

	if _, ok := cookieMap(jar.documentCookies(u))["token"]; ok {
		t.Fatal("replaced HttpOnly cookie remained visible to document.cookie")
	}
	if got := cookieMap(jar.Cookies(u))["token"]; got != "hidden" {
		t.Fatalf("HTTP cookie jar token = %q, want hidden", got)
	}
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
	if _, ok := cookieMap(jar.documentCookies(u))["gone"]; ok {
		t.Fatal("deleted cookie remained visible")
	}
}

func TestJSDocumentCookie(t *testing.T) {
	loader := NewLoader()
	var sentCookie string
	loader.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
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
