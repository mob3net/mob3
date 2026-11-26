package host

import (
	"context"
	"image"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"browser/internal/protocol"
)

func TestParseDataURI(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		data  string
		ctype string
	}{
		{"data:text/plain,hello", "hello", "text/plain"},
		{"data:,hello", "hello", "text/plain"},
		{"data:text/html;base64,aGVsbG8=", "hello", "text/html"},
		{"data:text/plain,hello%20world", "hello world", "text/plain"},
	} {
		data, ctype, err := ParseDataURI(tc.raw)
		if err != nil || string(data) != tc.data || ctype != tc.ctype {
			t.Fatalf("ParseDataURI(%q) = %q %q %v; want %q %q", tc.raw, data, ctype, err, tc.data, tc.ctype)
		}
	}
	for _, raw := range []string{"data:text/plain", "data:;base64,***"} {
		if _, _, err := ParseDataURI(raw); err == nil {
			t.Errorf("ParseDataURI(%q) succeeded", raw)
		}
	}
}

func TestFetcherFetchCtxAndBlockedRecord(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("hello")),
			Request:    r,
		}, nil
	})}
	f := &Fetcher{
		Client: client, Settings: NewSettings("ua", ""),
		Requests: NewRequestLog(), Cache: NewResourceCache(0),
	}
	u, _ := url.Parse("https://public.example/data")
	data, ctype, final, status, err := f.FetchCtx(publicRequestContext(t, "doc"), u)
	if err != nil || string(data) != "hello" || ctype != "text/plain" || status != http.StatusCreated {
		t.Fatalf("FetchCtx = %q %q %v %d", data, ctype, err, status)
	}
	if final == nil || final.String() != u.String() {
		t.Fatalf("final URL = %v", final)
	}

	gen := f.Requests.BeginPage()
	f.RecordBlocked(gen, "GET", u, "img", ErrMixedContentBlocked)
	f.RecordBlocked(gen, "GET", nil, "img", ErrMixedContentBlocked)
	f.RecordBlocked(gen, "GET", u, "", ErrMixedContentBlocked)
	recs := f.Requests.Snapshot()
	if len(recs) != 1 || recs[0].Status != 0 || !recs[0].Done || recs[0].Err == "" {
		t.Fatalf("blocked records = %+v", recs)
	}
}

func TestCSSJSCachePartitionedByTopLevelSite(t *testing.T) {
	calls := 0
	f := &Fetcher{
		Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/javascript"}},
				Body:       io.NopCloser(strings.NewReader("code")),
				Request:    r,
			}, nil
		})},
		Settings: NewSettings("ua", ""), Requests: NewRequestLog(),
		Cache: NewResourceCache(1 << 20),
	}
	target, _ := url.Parse("https://cdn.example.net/app.js")
	one, _ := url.Parse("https://a.example.org/")
	two, _ := url.Parse("https://b.example.net/")
	fetch := func(page *url.URL) {
		ctx := WithRequestMeta(context.Background(), 1, "js")
		ctx = WithRequestPage(ctx, page, false)
		if _, _, _, _, err := f.FetchRequest(ctx, http.MethodGet, target, nil, ""); err != nil {
			t.Fatal(err)
		}
	}
	fetch(one)
	fetch(one)
	if calls != 1 {
		t.Fatalf("same-site fetch made %d requests, want one cache hit", calls)
	}
	fetch(two)
	if calls != 2 {
		t.Fatalf("cross-site fetch made %d requests, want a cache miss", calls)
	}
	fetch(two)
	if calls != 2 {
		t.Fatalf("second same-site fetch made %d requests, want one cache hit", calls)
	}
}

func TestCacheSkipsCookieVaryingResponses(t *testing.T) {
	calls := 0
	f := &Fetcher{
		Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/javascript"},
					"Vary":         []string{"Cookie"},
				},
				Body:    io.NopCloser(strings.NewReader("code")),
				Request: r,
			}, nil
		})},
		Settings: NewSettings("ua", ""), Requests: NewRequestLog(),
		Cache: NewResourceCache(1 << 20),
	}
	target, _ := url.Parse("https://cdn.example.net/app.js")
	page, _ := url.Parse("https://a.example.org/")
	for i := 0; i < 2; i++ {
		ctx := WithRequestMeta(context.Background(), 1, "js")
		ctx = WithRequestPage(ctx, page, false)
		if _, _, _, _, err := f.FetchRequest(ctx, http.MethodGet, target, nil, ""); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("Vary: Cookie response made %d requests, want no cache hit", calls)
	}
}

func TestSessionFetchAndPolicyWrappers(t *testing.T) {
	s := NewSession()
	s.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("page")),
			Request:    r,
		}, nil
	})})
	ctx := publicRequestContext(t, "doc")
	u := mustURL(t, "https://public.example/doc")
	fetched, err := s.FetchCtx(ctx, u)
	if err != nil || string(fetched.Data) != "page" || fetched.Status != http.StatusOK {
		t.Fatalf("FetchCtx = %+v %v", fetched, err)
	}
	gen := s.BeginPage()
	s.RecordBlocked(gen, "GET", u, "img", ErrMixedContentBlocked)
	if s.RequestCount() != 1 {
		t.Fatalf("RecordBlocked count = %d", s.RequestCount())
	}
	mixedCtx := WithRequestPage(WithRequestMeta(context.Background(), gen, "css"), mustURL(t, "https://page.example/"), false)
	if _, err := s.ApplyMixedContent(mixedCtx, mustURL(t, "http://cdn.example/x.css")); !s.IsMixedContentError(err) {
		t.Fatalf("ApplyMixedContent error = %v", err)
	}
}

func TestSessionAccessorsAndCachedImage(t *testing.T) {
	s := NewSession()
	s.SetUserAgent("ua")
	s.SetSearchURL("https://search.test/?q=%s")
	s.SetImageCaps(100, 50)
	if s.UserAgent() != "ua" || s.SearchURL() != "https://search.test/?q=%s" {
		t.Fatal("settings wrappers are wrong")
	}
	if w, h := s.ImageCaps(); w != 100 || h != 50 {
		t.Fatalf("caps = %dx%d", w, h)
	}

	s.SetCacheLimitBytes(4096)
	if s.CacheLimitBytes() != 4096 {
		t.Fatal("cache limit wrapper")
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	s.PutCachedImage("k", img, 12, true)
	got, raw, local, ok := s.GetCachedImage("k")
	if !ok || got == nil || raw != 12 || !local {
		t.Fatalf("cached image = %v %d %v %v", got, raw, local, ok)
	}
	if _, _, entries := s.CacheStats(); entries != 1 {
		t.Fatalf("cache entries = %d", entries)
	}

	gen := s.BeginPage()
	id, ok := s.StartRequest(gen, "GET", "https://example.test/", "doc")
	if !ok || id == 0 {
		t.Fatal("StartRequest failed")
	}
	s.FinishRequest(id, gen, 200, 5, 0, nil)
	s.RecordCacheHit(gen, "GET", "https://example.test/a.css", "css", 3)
	if s.RequestCount() != 2 || len(s.RequestSnapshot()) != 2 {
		t.Fatalf("request wrappers = %d", s.RequestCount())
	}
	if s.CurrentPageGeneration() != gen {
		t.Fatal("page generation wrapper")
	}

	u, _ := url.Parse("https://example.test/")
	if !s.SetDocumentCookieRaw(u, "a=1") {
		t.Fatal("document cookie wrapper")
	}
	if vals := s.DocumentCookieValues(u); len(vals) != 1 || vals[0].Name != "a" {
		t.Fatalf("document cookie values = %+v", vals)
	}
	if views := s.CookieViews(u); len(views) != 1 {
		t.Fatalf("cookie views = %+v", views)
	}

	if s.CertSource() != CertSourceSystem {
		t.Fatal("default cert source")
	}
	if err := s.SetCertSource("system"); err != nil || s.CertPool() != nil {
		t.Fatal("system cert source wrapper")
	}
	if !s.IsLocalURL(mustURL(t, "http://localhost/")) ||
		s.IsMixedContentError(nil) || s.IsLocalNetworkError(nil) {
		t.Fatal("network error wrappers")
	}
	if s.ConfigToken().Current() == "" {
		t.Fatal("config token wrapper")
	}
	if s.Client() == nil || s.Cookies() == nil || s.Settings() == nil || s.Cache() == nil || s.Requests() == nil {
		t.Fatal("debug accessors")
	}

	s.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Access-Control-Allow-Origin": []string{"*"}},
			Body:       http.NoBody, Request: r,
		}, nil
	})})
	req := protocol.FetchRequest{URL: "https://example.test/x", Method: "GET"}
	if _, err := s.JSFetch(context.Background(), req); err != nil {
		t.Fatalf("JSFetch wrapper: %v", err)
	}
}
