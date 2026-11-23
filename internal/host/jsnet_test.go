package host

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	"browser/internal/protocol"
)

func testJSFetcher(t *testing.T, jar http.CookieJar) *Fetcher {
	t.Helper()
	return &Fetcher{
		Client:   &http.Client{Jar: jar},
		Settings: NewSettings("test-agent", ""),
		Requests: NewRequestLog(),
	}
}

func TestJSFetchSameOriginSendsCookie(t *testing.T) {
	var gotCookie, gotOrigin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		gotOrigin = r.Header.Get("Origin")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	jar, _ := cookiejar.New(nil)
	jar.SetCookies(u, []*http.Cookie{{Name: "a", Value: "1"}})
	f := testJSFetcher(t, jar)
	resp, err := f.JSFetch(context.Background(), protocol.FetchRequest{
		URL: srv.URL, Method: "GET", Origin: srv.URL,
		Purpose: protocol.FetchFetch, Kind: "fetch", Credentials: true, PageLocal: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 || string(resp.Body) != "ok" {
		t.Fatalf("response = %d %q", resp.Status, resp.Body)
	}
	if gotCookie != "a=1" {
		t.Fatalf("cookie = %q, want a=1", gotCookie)
	}
	if gotOrigin != "" {
		t.Fatalf("same-origin request sent Origin %q", gotOrigin)
	}
}

func TestJSFetchCrossOriginDropsCookieAndFiltersHeaders(t *testing.T) {
	var gotCookie, gotOrigin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		gotOrigin = r.Header.Get("Origin")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Secret", "hidden")
		http.SetCookie(w, &http.Cookie{Name: "b", Value: "2"})
		_, _ = w.Write([]byte("cross"))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	jar, _ := cookiejar.New(nil)
	jar.SetCookies(u, []*http.Cookie{{Name: "a", Value: "1"}})
	f := testJSFetcher(t, jar)
	resp, err := f.JSFetch(context.Background(), protocol.FetchRequest{
		URL: srv.URL, Method: "GET", Origin: "http://other.example",
		Purpose: protocol.FetchXHR, Kind: "xhr", PageLocal: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotCookie != "" {
		t.Fatalf("cross-origin request sent cookie %q", gotCookie)
	}
	if gotOrigin != "http://other.example" {
		t.Fatalf("origin = %q", gotOrigin)
	}
	for _, h := range resp.Headers {
		if h.Name == "X-Secret" || h.Name == "Set-Cookie" {
			t.Fatalf("unsafe cross-origin header exposed: %+v", h)
		}
	}
}

func TestJSFetchRejectsPreflightAndCORS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("no cors header"))
	}))
	defer srv.Close()
	f := testJSFetcher(t, nil)
	_, err := f.JSFetch(context.Background(), protocol.FetchRequest{
		URL: srv.URL, Method: "PUT", Origin: "http://other.example",
		Headers: []protocol.Header{{Name: "X-Custom", Value: "1"}},
		Purpose: protocol.FetchXHR, Kind: "xhr", PageLocal: true,
	})
	if err == nil {
		t.Fatal("preflight-requiring request was allowed")
	}
	_, err = f.JSFetch(context.Background(), protocol.FetchRequest{
		URL: srv.URL, Method: "GET", Origin: "http://other.example",
		Purpose: protocol.FetchXHR, Kind: "xhr", PageLocal: true,
	})
	if err == nil {
		t.Fatal("cross-origin response without CORS was allowed")
	}
}

func TestJSFetchBlocksPublicPageToLocal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	f := testJSFetcher(t, nil)
	_, err := f.JSFetch(context.Background(), protocol.FetchRequest{
		URL: srv.URL, Method: "GET", Origin: "https://public.example",
		Purpose: protocol.FetchFetch, Kind: "fetch", PageLocal: false,
	})
	if !errors.Is(err, ErrLocalNetworkBlocked) {
		t.Fatalf("error = %v, want ErrLocalNetworkBlocked", err)
	}
}

func TestJSFetchNoCORSSkipsCORSResponseCheck(t *testing.T) {
	var gotOrigin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOrigin = r.Header.Get("Origin")
		_, _ = w.Write([]byte("ping"))
	}))
	defer srv.Close()
	f := testJSFetcher(t, nil)
	resp, err := f.JSFetch(context.Background(), protocol.FetchRequest{
		URL: srv.URL, Method: "GET", Origin: "http://other.example",
		Purpose: protocol.FetchFetch, Kind: "fetch", NoCORS: true, PageLocal: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 || string(resp.Body) != "ping" {
		t.Fatalf("response = %d %q", resp.Status, resp.Body)
	}
	if gotOrigin != "" {
		t.Fatalf("no-cors request sent Origin %q", gotOrigin)
	}
}

func TestJSFetchNoCORSStillRejectsNonSimple(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	f := testJSFetcher(t, nil)
	_, err := f.JSFetch(context.Background(), protocol.FetchRequest{
		URL: srv.URL, Method: "PUT", Origin: "http://other.example",
		Purpose: protocol.FetchFetch, Kind: "fetch", NoCORS: true, PageLocal: true,
	})
	if err == nil {
		t.Fatal("non-simple no-cors request was allowed")
	}
}

func TestFetchAttestsDocumentFinalURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("page"))
	}))
	defer srv.Close()
	s := NewSession()
	u, _ := url.Parse(srv.URL)
	ctx := protocol.WithRequestMeta(context.Background(), 1, "doc")
	ctx = protocol.WithRequestEntry(ctx, 7)
	res, err := s.Fetch(ctx, "GET", u, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	attested, ok := s.AttestedURL(7)
	if !ok || attested != res.FinalURL.String() {
		t.Fatalf("attestation = %q, %v; final = %v", attested, ok, res.FinalURL)
	}
	if attested != srv.URL {
		t.Fatalf("attested %q, want %q", attested, srv.URL)
	}
}

func TestFetchDoesNotAttestSubresource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("img"))
	}))
	defer srv.Close()
	s := NewSession()
	u, _ := url.Parse(srv.URL)
	ctx := protocol.WithRequestMeta(context.Background(), 1, "img")
	ctx = protocol.WithRequestEntry(ctx, 8)
	if _, err := s.Fetch(ctx, "GET", u, nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.AttestedURL(8); ok {
		t.Fatal("subresource fetch was attested")
	}
}
