package host

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"browser/internal/protocol"
)

func TestIsLocalHostAndURL(t *testing.T) {
	for _, host := range []string{
		"localhost", "app.localhost", "printer.local", "127.0.0.1", "::1",
		"fe80::1%eth0", "10.0.0.1", "169.254.1.1", "100.64.0.1",
		"192.0.0.1", "198.18.0.1", "240.0.0.1", "0.0.0.0",
	} {
		if !IsLocalHost(host) {
			t.Errorf("IsLocalHost(%q) = false", host)
		}
	}
	for _, host := range []string{"", "example.com", "1.1.1.1", "8.8.8.8"} {
		if IsLocalHost(host) {
			t.Errorf("IsLocalHost(%q) = true", host)
		}
	}
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"http://localhost/", true},
		{"https://10.0.0.1/x", true},
		{"ftp://localhost/x", false},
		{"https://example.com/", false},
	} {
		u, _ := url.Parse(tc.raw)
		if got := IsLocalURL(u); got != tc.want {
			t.Errorf("IsLocalURL(%q) = %v", tc.raw, got)
		}
	}
	if IsLocalURL(nil) {
		t.Fatal("nil URL reported local")
	}
}

func TestIsLocalIPSecurityRanges(t *testing.T) {
	for _, raw := range []string{
		"10.0.0.1", "172.16.0.1", "192.168.1.1", "127.0.0.1", "169.254.1.1",
		"100.64.0.1", "100.127.255.254", "192.0.0.1", "198.18.0.1",
		"198.19.255.255", "240.0.0.1", "255.255.255.255", "0.0.0.0",
		"::1", "fc00::1", "fe80::1", "ff02::1",
	} {
		if !IsLocalIP(net.ParseIP(raw)) {
			t.Errorf("IsLocalIP(%q) = false", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "100.63.255.255", "2001:4860:4860::8888"} {
		if IsLocalIP(net.ParseIP(raw)) {
			t.Errorf("IsLocalIP(%q) = true", raw)
		}
	}
	if IsLocalIP(nil) {
		t.Fatal("nil IP reported local")
	}
}

func TestApplyMixedContentPolicy(t *testing.T) {
	u, _ := url.Parse("http://cdn.example/x.png")
	ctx := WithRequestMeta(context.Background(), 1, "img")
	ctx = WithRequestPage(ctx, mustURL(t, "https://page.example/"), false)

	got, err := ApplyMixedContent(ctx, u)
	if err != nil || got == nil || got.Scheme != "https" {
		t.Fatalf("image upgrade = %v %v", got, err)
	}
	if _, err := ApplyMixedContent(ctx, mustURL(t, "http://127.0.0.1/x.png")); !IsMixedContentError(err) {
		t.Fatalf("IP-literal image error = %v", err)
	}
	if _, err := ApplyMixedContent(WithRequestKind(ctx, "css"), mustURL(t, "http://cdn.example/x.css")); !IsMixedContentError(err) {
		t.Fatalf("stylesheet error = %v", err)
	}
	insecure := WithRequestMeta(context.Background(), 1, "css")
	if got, err := ApplyMixedContent(insecure, u); err != nil || got != u {
		t.Fatalf("insecure page = %v %v", got, err)
	}
	if got, err := ApplyMixedContent(ctx, nil); err != nil || got != nil {
		t.Fatalf("nil URL = %v %v", got, err)
	}
}

func TestLocalNetworkControlAndRemoteAddr(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:80", "localhost:443", "[::1]:80"} {
		if !errors.Is(LocalNetworkControl("tcp", addr, nil), ErrLocalNetworkBlocked) {
			t.Errorf("LocalNetworkControl(%q) did not block", addr)
		}
	}
	for _, addr := range []string{"8.8.8.8:443", "bad"} {
		if err := LocalNetworkControl("tcp", addr, nil); err != nil {
			t.Errorf("LocalNetworkControl(%q) = %v", addr, err)
		}
	}
	if !RemoteAddrLocal(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}) {
		t.Fatal("loopback peer not local")
	}
	if RemoteAddrLocal(&net.TCPAddr{IP: net.ParseIP("8.8.8.8"), Port: 1}) {
		t.Fatal("public peer reported local")
	}
	if RemoteAddrLocal(nil) || RemoteAddrLocal(net.Addr(badAddr("bad"))) {
		t.Fatal("invalid remote address reported local")
	}
}

type badAddr string

func (a badAddr) Network() string { return "tcp" }
func (a badAddr) String() string  { return string(a) }

func TestRequestContextWrappers(t *testing.T) {
	space := &PageSpace{}
	space.Local.Store(true)
	ctx := WithPageSpace(context.Background(), space)
	if PageSpaceFrom(ctx) != space {
		t.Fatal("page space was not preserved")
	}
	ctx = WithRequestMeta(ctx, 3, "css")
	ctx = WithRequestKind(ctx, "img")
	meta := RequestMetaFrom(ctx)
	if meta.Gen != 3 || meta.Kind != "img" {
		t.Fatalf("meta = %+v", meta)
	}
	if RequestUsesProxy("") {
		t.Fatal("empty URL reported as proxied")
	}
}

func TestNewHTTPTransportPoolAndProxy(t *testing.T) {
	tr := NewHTTPTransport(nil)
	if tr.TLSClientConfig != nil || !tr.ForceAttemptHTTP2 || tr.DialContext == nil {
		t.Fatalf("default transport = %+v", tr)
	}
	pool := x509.NewCertPool()
	tr = NewHTTPTransport(pool)
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.RootCAs != pool {
		t.Fatal("custom pool was not installed")
	}
}

func TestLocalDialUsesGuardForPublicPages(t *testing.T) {
	ctx := WithRequestMeta(context.Background(), 1, "img")
	ctx = WithRequestPage(ctx, mustURL(t, "https://page.example/"), false)
	_, err := LocalNetworkDialContext(ctx, "tcp", "127.0.0.1:1")
	if !errors.Is(err, ErrLocalNetworkBlocked) {
		t.Fatalf("guarded dial error = %v", err)
	}
}

// pooledLocalFetcher returns a fetcher whose transport resolves a
// public-looking host to a loopback test server. The custom dialer models a
// name that only resolves locally and deliberately skips the dial-time guard,
// so only the per-connection peer check can catch the reuse.
func pooledLocalFetcher(t *testing.T) (*Fetcher, *url.URL, *url.URL, *atomic.Int32) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	var dials atomic.Int32
	tr := &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		dials.Add(1)
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}}
	f := &Fetcher{
		Client:   &http.Client{Transport: tr},
		Settings: NewSettings("ua", ""),
		Requests: NewRequestLog(),
		Cache:    NewResourceCache(0),
	}
	target, _ := url.Parse("http://intranet.invalid/")
	return f, target, mustURL(t, "http://page.example/"), &dials
}

func TestLocalPeerGuardBlocksReusedPooledConnection(t *testing.T) {
	f, target, page, dials := pooledLocalFetcher(t)
	seed := WithRequestPage(WithRequestMeta(context.Background(), 1, "img"), page, true)
	if _, _, _, _, err := f.FetchRequest(seed, http.MethodGet, target, nil, ""); err != nil {
		t.Fatalf("seed request: %v", err)
	}
	guarded := WithRequestPage(WithRequestMeta(context.Background(), 2, "img"), page, false)
	if _, _, _, _, err := f.FetchRequest(guarded, http.MethodGet, target, nil, ""); !errors.Is(err, ErrLocalNetworkBlocked) {
		t.Fatalf("guarded request over a reused connection = %v, want blocked", err)
	}
	if got := dials.Load(); got != 1 {
		t.Fatalf("guarded request dialed %d times, want the pooled connection reused", got)
	}
}

func TestLocalPeerGuardBlocksReusedConnectionForJSFetch(t *testing.T) {
	f, target, page, dials := pooledLocalFetcher(t)
	seed := WithRequestPage(WithRequestMeta(context.Background(), 1, "img"), page, true)
	if _, _, _, _, err := f.FetchRequest(seed, http.MethodGet, target, nil, ""); err != nil {
		t.Fatalf("seed request: %v", err)
	}
	if _, err := f.JSFetch(context.Background(), protocol.FetchRequest{
		URL: target.String(), Method: "GET", Origin: page.String(),
		Purpose: protocol.FetchFetch, Kind: "fetch",
	}); !errors.Is(err, ErrLocalNetworkBlocked) {
		t.Fatalf("guarded fetch over a reused connection = %v, want blocked", err)
	}
	if got := dials.Load(); got != 1 {
		t.Fatalf("guarded fetch dialed %d times, want the pooled connection reused", got)
	}
}

type fixedRemoteConn struct {
	net.Conn
	remote net.Addr
}

func (c *fixedRemoteConn) RemoteAddr() net.Addr { return c.remote }

// A local intermediate hop in a redirect chain must not make the public final
// document inherit local-network access.
func TestPageLocalTracksFinalRedirectHop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "http://public.example/final", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	localAddr := srv.Listener.Addr().String()
	tr := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(addr)
		c, err := (&net.Dialer{}).DialContext(ctx, network, localAddr)
		if err != nil {
			return nil, err
		}
		if host == "public.example" {
			return &fixedRemoteConn{Conn: c, remote: &net.TCPAddr{IP: net.ParseIP("8.8.8.8"), Port: 443}}, nil
		}
		return c, nil
	}}
	s := NewSession()
	s.SetHTTPClient(&http.Client{Transport: tr})
	res, err := s.Fetch(context.Background(), http.MethodGet, mustURL(t, "http://intranet.invalid/start"), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.PageLocal {
		t.Fatal("public final redirect hop inherited PageLocal from a local intermediate hop")
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
