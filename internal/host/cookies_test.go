package host

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"

	"browser/internal/protocol"
)

func cookieNames(cookies []*http.Cookie) string {
	names := make([]string, len(cookies))
	for i, c := range cookies {
		names[i] = c.Name
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

func TestDocumentCookieHttpOnlyProtection(t *testing.T) {
	j := NewCookieJar()
	u, _ := url.Parse("https://example.com/app/page")
	j.SetCookies(u, []*http.Cookie{
		{Name: "sid", Value: "server", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode},
		{Name: "theme", Value: "dark", Path: "/"},
	})
	if got := cookieNames(j.Cookies(u)); got != "sid,theme" {
		t.Fatalf("all cookies = %q", got)
	}
	if got := cookieNames(j.DocumentCookies(u)); got != "theme" {
		t.Fatalf("document cookies = %q", got)
	}
	if vals := j.DocumentCookieValues(u); len(vals) != 1 || vals[0].Name != "theme" || vals[0].Value != "dark" {
		t.Fatalf("document values = %+v", vals)
	}

	j.SetDocumentCookie(u, &http.Cookie{Name: "sid", Value: "evil", Path: "/"})
	if j.SetDocumentCookieRaw(u, "sid=evil2; Path=/") != true {
		t.Fatal("raw document cookie was rejected")
	}
	if got := cookieValue(j.RequestCookies(u, "GET", RequestCookiePolicy{}), "sid"); got != "server" {
		t.Fatalf("HttpOnly cookie was replaced by script: %q", got)
	}
	if got := cookieNames(j.DocumentCookies(u)); strings.Contains(got, "sid") {
		t.Fatalf("HttpOnly cookie became script-visible: %q", got)
	}

	views := j.CookieViews(u)
	var sid *protocol.CookieView
	for i := range views {
		if views[i].Name == "sid" {
			sid = &views[i]
		}
	}
	if sid == nil || !sid.HttpOnly || !sid.Secure || sid.SameSite != "lax" || sid.Domain != "example.com" {
		t.Fatalf("sid view = %+v", sid)
	}
	display := j.CookiesForDisplay(u)
	if len(display) != 2 {
		t.Fatalf("display cookies = %+v", display)
	}
}

func TestDocumentCookieSetAndDelete(t *testing.T) {
	j := NewCookieJar()
	u, _ := url.Parse("https://example.com/")
	if j.SetDocumentCookieRaw(u, "") {
		t.Fatal("empty document.cookie accepted")
	}
	if j.SetDocumentCookieRaw(nil, "a=1") {
		t.Fatal("nil URL accepted")
	}
	if !j.SetDocumentCookieRaw(u, "pref=1; Path=/; SameSite=Strict; Max-Age=60") {
		t.Fatal("valid document.cookie rejected")
	}
	if got := cookieValue(j.DocumentCookies(u), "pref"); got != "1" {
		t.Fatalf("document cookie = %q", got)
	}
	view := j.CookieViews(u)
	if len(view) != 1 || view[0].SameSite != "strict" || view[0].Expires.IsZero() {
		t.Fatalf("cookie view = %+v", view)
	}
	if !j.SetDocumentCookieRaw(u, "pref=; Path=/; Max-Age=-1") {
		t.Fatal("delete document.cookie rejected")
	}
	if got := cookieNames(j.DocumentCookies(u)); got != "" {
		t.Fatalf("deleted cookie remains: %q", got)
	}
}

func TestCookieDomainAndPathScoping(t *testing.T) {
	j := NewCookieJar()
	u, _ := url.Parse("https://sub.example.com/dir/page")
	j.SetCookies(u, []*http.Cookie{
		{Name: "host", Value: "1"},
		{Name: "dom", Value: "2", Domain: ".example.com", Path: "/"},
		{Name: "foreign", Value: "3", Domain: "other.com"},
	})
	if got := cookieNames(j.RequestCookies(u, "GET", RequestCookiePolicy{})); got != "dom,host" {
		t.Fatalf("same host cookies = %q", got)
	}
	otherPath, _ := url.Parse("https://sub.example.com/other")
	if got := cookieNames(j.RequestCookies(otherPath, "GET", RequestCookiePolicy{})); got != "dom" {
		t.Fatalf("path scoping = %q", got)
	}
	sibling, _ := url.Parse("https://other.example.com/dir/page")
	if got := cookieNames(j.RequestCookies(sibling, "GET", RequestCookiePolicy{})); got != "dom" {
		t.Fatalf("domain scoping = %q", got)
	}
	outside, _ := url.Parse("https://other.com/")
	if got := cookieNames(j.RequestCookies(outside, "GET", RequestCookiePolicy{})); got != "" {
		t.Fatalf("foreign cookie leaked: %q", got)
	}
}

func TestCookieDomainMatches(t *testing.T) {
	for _, tc := range []struct {
		host, domain string
		want         bool
	}{
		{"example.com", "example.com", true},
		{"sub.example.com", "example.com", true},
		{"badexample.com", "example.com", false},
		{"example.com", "com", false},
		{"127.0.0.1", "127.0.0.1", true},
		{"127.0.0.1", "0.0.1", false},
		{"xn--bcher-kva.com", "victim.com", false},
	} {
		if got := cookieDomainMatches(tc.host, tc.domain); got != tc.want {
			t.Errorf("cookieDomainMatches(%q, %q) = %v", tc.host, tc.domain, got)
		}
	}
}

func TestCanonicalCookieHost(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"Example.COM.", "example.com", true},
		{"bücher.com", "xn--bcher-kva.com", true},
		{"127.0.0.1", "127.0.0.1", true},
		{"", "", false},
	} {
		got, ok := canonicalCookieHost(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("canonicalCookieHost(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// An IDN setter must not be able to plant a Domain cookie for an ASCII victim.
func TestCookieDomainIsolationFromIDNHost(t *testing.T) {
	j := NewCookieJar()
	attacker, _ := url.Parse("https://bücher.com/")
	victim, _ := url.Parse("https://victim.com/")
	j.SetCookies(victim, []*http.Cookie{{Name: "sid", Value: "secret", Path: "/", HttpOnly: true}})
	j.SetDocumentCookie(attacker, &http.Cookie{Name: "script", Value: "evil", Domain: "victim.com", Path: "/"})
	j.SetCookies(attacker, []*http.Cookie{{Name: "server", Value: "evil", Domain: "victim.com", Path: "/"}})
	// A server-set cookie with an existing identity must not be overwritten by
	// the IDN host either.
	j.SetCookies(attacker, []*http.Cookie{{Name: "sid", Value: "evil", Domain: "victim.com", Path: "/", HttpOnly: true}})
	if got := cookieNames(j.RequestCookies(victim, "GET", RequestCookiePolicy{})); got != "sid" {
		t.Fatalf("cookies planted for %s from an IDN host: %q", victim, got)
	}
	if got := cookieValue(j.RequestCookies(victim, "GET", RequestCookiePolicy{}), "sid"); got != "secret" {
		t.Fatalf("HttpOnly cookie overwritten through an IDN host: %q", got)
	}
	if got := cookieNames(j.Cookies(victim)); got != "sid" {
		t.Fatalf("standard jar accepted a foreign IDN cookie: %q", got)
	}
	if got := cookieValue(j.Cookies(victim), "sid"); got != "secret" {
		t.Fatalf("standard jar stored an overwritten HttpOnly cookie: %q", got)
	}

	// The same IDN host must still be able to scope a cookie to itself and its
	// subdomains using the punycode Domain a server would send.
	own, _ := url.Parse("https://sub.bücher.com/")
	j.SetCookies(own, []*http.Cookie{{Name: "own", Value: "1", Domain: "xn--bcher-kva.com", Path: "/"}})
	if got := cookieNames(j.RequestCookies(own, "GET", RequestCookiePolicy{})); got != "own" {
		t.Fatalf("legitimate IDN domain cookie = %q", got)
	}
	if got := cookieNames(j.RequestCookies(attacker, "GET", RequestCookiePolicy{})); got != "own" {
		t.Fatalf("parent IDN host did not receive its domain cookie: %q", got)
	}
}

func TestCookieSecureAndPrefixRules(t *testing.T) {
	httpURL, _ := url.Parse("http://example.com/")
	httpsURL, _ := url.Parse("https://example.com/")

	j := NewCookieJar()
	j.SetCookies(httpURL, []*http.Cookie{{Name: "plain", Value: "1", Path: "/", Secure: true}})
	j.SetDocumentCookie(httpURL, &http.Cookie{Name: "script", Value: "1", Path: "/", Secure: true})
	if got := cookieNames(j.RequestCookies(httpsURL, "GET", RequestCookiePolicy{})); got != "" {
		t.Fatalf("Secure cookies planted over HTTP: %q", got)
	}

	j.SetCookies(httpsURL, []*http.Cookie{
		{Name: "__Secure-no", Value: "1", Path: "/"},
		{Name: "__Secure-yes", Value: "1", Path: "/", Secure: true},
		{Name: "__Host-yes", Value: "1", Path: "/", Secure: true},
		{Name: "__Host-domain", Value: "1", Path: "/", Secure: true, Domain: "example.com"},
		{Name: "__Host-path", Value: "1", Path: "/sub", Secure: true},
	})
	got := cookieNames(j.RequestCookies(httpsURL, "GET", RequestCookiePolicy{}))
	if got != "__Host-yes,__Secure-yes" {
		t.Fatalf("prefix cookies = %q", got)
	}
}

func TestCookieStorageCaps(t *testing.T) {
	u, _ := url.Parse("https://example.com/")

	// One cookie larger than the per-cookie bound is rejected outright.
	j := NewCookieJar()
	j.SetDocumentCookie(u, &http.Cookie{Name: "big", Value: strings.Repeat("a", maxCookieBytes), Path: "/"})
	if got := cookieNames(j.RequestCookies(u, "GET", RequestCookiePolicy{})); got != "" {
		t.Fatalf("oversized cookie was stored: %q", got)
	}

	// A domain keeps at most maxCookiesPerDomain cookies, but replacing an
	// existing identity at the cap still works.
	j = NewCookieJar()
	for i := 0; i < maxCookiesPerDomain+20; i++ {
		j.SetCookies(u, []*http.Cookie{{Name: fmt.Sprintf("c%d", i), Value: "1", Path: "/"}})
	}
	if got := len(j.RequestCookies(u, "GET", RequestCookiePolicy{})); got != maxCookiesPerDomain {
		t.Fatalf("per-domain cookies = %d, want %d", got, maxCookiesPerDomain)
	}
	j.SetCookies(u, []*http.Cookie{{Name: "c0", Value: "2", Path: "/"}})
	if got := cookieValue(j.RequestCookies(u, "GET", RequestCookiePolicy{}), "c0"); got != "2" {
		t.Fatalf("replacement at the domain cap = %q, want 2", got)
	}

	// The total count and byte caps hold across domains and oversized values.
	j = NewCookieJar()
	for d := 0; d < 8; d++ {
		host, _ := url.Parse(fmt.Sprintf("https://d%d.test/", d))
		for i := 0; i < maxCookiesPerDomain; i++ {
			j.SetCookies(host, []*http.Cookie{{
				Name: fmt.Sprintf("c%d", i), Value: strings.Repeat("v", maxCookieBytes-64), Path: "/",
			}})
		}
	}
	if len(j.cookies) > maxCookiesTotal {
		t.Fatalf("total cookies = %d, want at most %d", len(j.cookies), maxCookiesTotal)
	}
	if j.bytes > maxCookieJarBytes {
		t.Fatalf("cookie bytes = %d, want at most %d", j.bytes, maxCookieJarBytes)
	}
}

func cookieValue(cookies []*http.Cookie, name string) string {
	for _, c := range cookies {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func TestRequestCookiesSameSite(t *testing.T) {
	j := NewCookieJar()
	target, _ := url.Parse("https://b.example.net/img")
	j.SetCookies(target, []*http.Cookie{
		{Name: "lax", Value: "1", SameSite: http.SameSiteLaxMode},
		{Name: "strict", Value: "1", SameSite: http.SameSiteStrictMode},
		{Name: "none", Value: "1", SameSite: http.SameSiteNoneMode, Secure: true},
		{Name: "plain", Value: "1"},
	})
	cases := []struct {
		name   string
		method string
		policy RequestCookiePolicy
		want   string
	}{
		{"cross-site subresource", "GET", RequestCookiePolicy{TopSite: "https://example.com"}, "none,plain"},
		{"cross-site top-level GET", "GET", RequestCookiePolicy{TopSite: "https://example.com", TopLevel: true}, "lax,none,plain"},
		{"cross-site top-level POST", "POST", RequestCookiePolicy{TopSite: "https://example.com", TopLevel: true}, "none,plain"},
		{"same-site subresource", "GET", RequestCookiePolicy{TopSite: "https://example.net"}, "lax,none,plain,strict"},
		{"unknown top site", "GET", RequestCookiePolicy{}, "lax,none,plain,strict"},
	}
	for _, c := range cases {
		got := cookieNames(j.RequestCookies(target, c.method, c.policy))
		if got != c.want {
			t.Errorf("%s: cookies = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestRequestCookiesSecureNoneOverHTTP(t *testing.T) {
	j := NewCookieJar()
	u, _ := url.Parse("http://b.example.net/")
	j.SetCookies(u, []*http.Cookie{
		{Name: "none", Value: "1", SameSite: http.SameSiteNoneMode},
		{Name: "plain", Value: "1"},
	})
	if got := cookieNames(j.RequestCookies(u, "GET", RequestCookiePolicy{TopSite: "https://example.net"})); got != "plain" {
		t.Fatalf("cookies over http = %q", got)
	}
}

func TestFetchAppliesSameSiteAndStoresCookies(t *testing.T) {
	j := NewCookieJar()
	target, _ := url.Parse("https://b.example.net/x")
	j.SetCookies(target, []*http.Cookie{
		{Name: "lax", Value: "1", SameSite: http.SameSiteLaxMode},
	})
	var gotCookie string
	f := &Fetcher{
		Settings: NewSettings("ua", ""), Requests: NewRequestLog(),
		Cache: NewResourceCache(0), Cookies: j,
		Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotCookie = r.Header.Get("Cookie")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Set-Cookie": []string{"fresh=1; Path=/"}},
				Body:       io.NopCloser(strings.NewReader("ok")),
				Request:    r,
			}, nil
		})},
	}
	page, _ := url.Parse("https://a.example.com/")
	ctx := WithRequestMeta(context.Background(), 1, "img")
	ctx = WithRequestPage(ctx, page, false)
	if _, _, _, _, err := f.FetchRequest(ctx, http.MethodGet, target, nil, ""); err != nil {
		t.Fatal(err)
	}
	if gotCookie != "" {
		t.Fatalf("cross-site subresource sent %q", gotCookie)
	}
	got := cookieNames(j.RequestCookies(target, "GET", RequestCookiePolicy{TopSite: "https://example.net"}))
	if !strings.Contains(got, "fresh") {
		t.Fatalf("response cookie was not stored: %q", got)
	}
}
