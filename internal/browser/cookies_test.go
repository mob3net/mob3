package browser

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

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
