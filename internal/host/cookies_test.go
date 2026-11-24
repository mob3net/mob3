package host

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
)

func cookieNames(cookies []*http.Cookie) string {
	names := make([]string, len(cookies))
	for i, c := range cookies {
		names[i] = c.Name
	}
	sort.Strings(names)
	return strings.Join(names, ",")
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
