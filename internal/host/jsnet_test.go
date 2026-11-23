package host

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"sync"
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

func TestJSFetchDoesNotRewireSharedClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	f := testJSFetcher(t, nil)
	called := false
	f.Client.CheckRedirect = func(*http.Request, []*http.Request) error {
		called = true
		return nil
	}
	if _, err := f.JSFetch(context.Background(), protocol.FetchRequest{
		URL: srv.URL, Method: "GET", Origin: srv.URL,
		Purpose: protocol.FetchFetch, Kind: "fetch", Credentials: true, PageLocal: true,
	}); err != nil {
		t.Fatal(err)
	}
	next := &http.Request{URL: &url.URL{Scheme: "http", Host: "other.example"}}
	if err := f.Client.CheckRedirect(next, nil); err != nil || !called {
		t.Fatalf("shared client redirect policy was replaced: called=%v err=%v", called, err)
	}
}

func TestFetchRedirectSurvivesPageXHR(t *testing.T) {
	landing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("landed"))
	}))
	defer landing.Close()
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/go" {
			http.Redirect(w, r, landing.URL, http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("xhr"))
	}))
	defer front.Close()
	f := testJSFetcher(t, nil)
	if _, err := f.JSFetch(context.Background(), protocol.FetchRequest{
		URL: front.URL + "/xhr", Method: "GET", Origin: front.URL,
		Purpose: protocol.FetchFetch, Kind: "fetch", Credentials: true, PageLocal: true,
	}); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(front.URL + "/go")
	data, _, _, status, err := f.FetchRequest(context.Background(), http.MethodGet, u, nil, "")
	if err != nil {
		t.Fatalf("navigation redirect after a page XHR failed: %v", err)
	}
	if status != http.StatusOK || string(data) != "landed" {
		t.Fatalf("response = %d %q", status, data)
	}
}

func TestJSFetchConcurrentUsesPrivateClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	f := testJSFetcher(t, nil)
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.JSFetch(context.Background(), protocol.FetchRequest{
				URL: srv.URL, Method: "GET", Origin: srv.URL,
				Purpose: protocol.FetchXHR, Kind: "xhr", Credentials: true, PageLocal: true,
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestJSFetchOmitCredentialsDropsCookie(t *testing.T) {
	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	jar, _ := cookiejar.New(nil)
	jar.SetCookies(u, []*http.Cookie{{Name: "a", Value: "1"}})
	f := testJSFetcher(t, jar)
	if _, err := f.JSFetch(context.Background(), protocol.FetchRequest{
		URL: srv.URL, Method: "GET", Origin: srv.URL,
		Purpose: protocol.FetchFetch, Kind: "fetch", PageLocal: true,
	}); err != nil {
		t.Fatal(err)
	}
	if gotCookie != "" {
		t.Fatalf("credentials-omitted request sent cookie %q", gotCookie)
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

func TestJSFetchFromDataDocumentBlocksLocalNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	f := testJSFetcher(t, nil)
	_, err := f.JSFetch(context.Background(), protocol.FetchRequest{
		URL: srv.URL, Method: "GET", Origin: "data:text/html,<p>x</p>",
		Purpose: protocol.FetchFetch, Kind: "fetch", PageLocal: false,
	})
	if !errors.Is(err, ErrLocalNetworkBlocked) {
		t.Fatalf("error = %v, want ErrLocalNetworkBlocked", err)
	}
}

func TestJSFetchRedirectToLocalBlocked(t *testing.T) {
	f := testJSFetcher(t, nil)
	calls := 0
	f.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return redirectResponse(r, "https://127.0.0.1/secret"), nil
	})}
	_, err := f.JSFetch(context.Background(), protocol.FetchRequest{
		URL: "https://public.example/start", Method: "GET", Origin: "https://public.example",
		Purpose: protocol.FetchFetch, Kind: "fetch", Credentials: true, PageLocal: false,
	})
	if !errors.Is(err, ErrLocalNetworkBlocked) {
		t.Fatalf("error = %v, want ErrLocalNetworkBlocked", err)
	}
	if calls != 1 {
		t.Fatalf("transport calls = %d, want the redirect blocked before dialing", calls)
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
