package host_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"browser/internal/host"
	"browser/internal/protocol"
)

func hostRPCPair(t *testing.T) (*protocol.RemoteHost, *host.Session) {
	t.Helper()
	server, client := net.Pipe()
	session := host.NewSession()
	serverSide := protocol.NewHostServer(server, session, protocol.DefaultLimits)
	done := make(chan error, 1)
	go func() { done <- serverSide.Serve(context.Background()) }()
	clientSide := protocol.NewHostClient(client, protocol.DefaultLimits)
	t.Cleanup(func() {
		_ = clientSide.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("host server: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("host server did not stop")
		}
	})
	return protocol.NewRemoteHost(clientSide), session
}

func TestPageAndHostRPCOnOnePeer(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	parentPeer := protocol.NewMuxPeerReadWriter(clientSide, protocol.DefaultLimits)
	childPeer := protocol.NewMuxPeerReadWriter(serverSide, protocol.DefaultLimits)

	hostSession := host.NewSession()
	_ = protocol.NewHostServerPeer(parentPeer, hostSession)
	hostClient := protocol.NewHostClientPeer(childPeer)
	remote := protocol.NewRemoteHost(hostClient)
	session := protocol.DialPeer(context.Background(), parentPeer, 1, protocol.DefaultLimits, 8)
	defer session.Close()

	handler := protocol.HandlerFunc(func(ctx context.Context, _ <-chan struct{}, in <-chan protocol.Command, out protocol.EventSink) error {
		for {
			select {
			case cmd, ok := <-in:
				if !ok {
					return nil
				}
				if _, ok := cmd.Body.(protocol.Navigate); ok {
					if err := out.Send(protocol.Event{Body: protocol.State{Title: "shared peer"}}); err != nil {
						return err
					}
				}
			case <-ctx.Done():
				return nil
			}
		}
	})
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- protocol.ServePeer(context.Background(), childPeer, 1, handler, protocol.DefaultLimits, 8)
	}()

	remote.SetUserAgent("shared-agent")
	if got := remote.UserAgent(); got != "shared-agent" {
		t.Fatalf("host RPC over shared peer = %q", got)
	}
	if err := session.Send(protocol.Command{Body: protocol.Navigate{URL: "https://example.test/"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case ev, ok := <-session.Events():
		if !ok {
			t.Fatal("event stream closed")
		}
		st, ok := ev.Body.(protocol.State)
		if !ok || st.Title != "shared peer" {
			t.Fatalf("event = %#v", ev.Body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("page event did not arrive over the shared peer")
	}
	_ = session.Close()
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("page server did not stop")
	}
}

func TestRemoteHostSettings(t *testing.T) {
	remote, _ := hostRPCPair(t)
	remote.SetUserAgent("remote-agent")
	if got := remote.UserAgent(); got != "remote-agent" {
		t.Fatalf("UserAgent = %q", got)
	}
	remote.SetSearchURL("https://search.test/?q=%s")
	if got := remote.SearchURL(); got != "https://search.test/?q=%s" {
		t.Fatalf("SearchURL = %q", got)
	}
	remote.SetImageCaps(640, 480)
	if w, h := remote.ImageCaps(); w != 640 || h != 480 {
		t.Fatalf("ImageCaps = %d,%d", w, h)
	}
}

func TestRemoteHostFetchAndRequestLog(t *testing.T) {
	remote, _ := hostRPCPair(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>rpc</body></html>"))
	}))
	defer srv.Close()
	u, err := url.Parse(srv.URL + "/page")
	if err != nil {
		t.Fatal(err)
	}
	gen := remote.BeginPage()
	ctx := protocol.WithRequestMeta(context.Background(), gen, "doc")
	got, err := remote.Fetch(ctx, protocol.MethodGet, u, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != 200 || got.ContentType != "text/html; charset=utf-8" {
		t.Fatalf("fetch = status %d type %q", got.Status, got.ContentType)
	}
	if got.FinalURL == nil || got.FinalURL.String() != u.String() {
		t.Fatalf("final URL = %v", got.FinalURL)
	}
	records := remote.RequestSnapshot()
	if len(records) != 1 || !records[0].Done || records[0].Status != 200 {
		t.Fatalf("request log = %+v", records)
	}
}

func TestRemoteHostBlocksLocalMetadata(t *testing.T) {
	remote, _ := hostRPCPair(t)
	gen := remote.BeginPage()
	ctx := protocol.WithRequestMeta(context.Background(), gen, "img")
	public, _ := url.Parse("https://public.example/")
	local, _ := url.Parse("http://127.0.0.1:1/")
	ctx = protocol.WithRequestPage(ctx, public, false)
	_, err := remote.FetchCtx(ctx, local)
	if !errors.Is(err, protocol.ErrLocalNetworkBlocked) {
		t.Fatalf("fetch error = %v, want ErrLocalNetworkBlocked", err)
	}
	if got := remote.RequestSnapshot(); len(got) != 1 || got[0].Status != 0 || !got[0].Done {
		t.Fatalf("blocked request log = %+v", got)
	}
}

func TestRemoteHostJSFetch(t *testing.T) {
	remote, _ := hostRPCPair(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	got, err := remote.JSFetch(context.Background(), protocol.FetchRequest{
		URL: srv.URL, Method: protocol.MethodGet, Origin: srv.URL,
		Purpose: protocol.FetchFetch, Kind: "fetch", PageLocal: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != 200 || string(got.Body) != `{"ok":true}` {
		t.Fatalf("JS fetch = status %d body %q", got.Status, got.Body)
	}
}

func TestRemoteHostConcurrentCalls(t *testing.T) {
	remote, _ := hostRPCPair(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.URL.Path))
	}))
	defer srv.Close()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := "/" + strconv.Itoa(i)
			u, _ := url.Parse(srv.URL + path)
			got, err := remote.FetchCtx(context.Background(), u)
			if err != nil {
				errs <- err
				return
			}
			if string(got.Data) != path {
				errs <- errors.New("response mismatch: " + string(got.Data))
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestRemoteHostCancel(t *testing.T) {
	remote, _ := hostRPCPair(t)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	if _, err := remote.FetchCtx(ctx, u); !errors.Is(err, context.Canceled) {
		t.Fatalf("fetch error = %v, want context.Canceled", err)
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("host fetch was not cancelled")
	}
}

func TestRemoteHostMixedContentAndRequestLog(t *testing.T) {
	remote, _ := hostRPCPair(t)
	gen := remote.BeginPage()
	ctx := protocol.WithRequestMeta(context.Background(), gen, "img")
	page, _ := url.Parse("https://example.test/")
	ctx = protocol.WithRequestPage(ctx, page, false)
	httpURL, _ := url.Parse("http://cdn.example.test/image.png")
	got, err := remote.ApplyMixedContent(ctx, httpURL)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Scheme != "https" {
		t.Fatalf("mixed-content URL = %v", got)
	}
	ctx = protocol.WithRequestMeta(ctx, gen, "js")
	if _, err := remote.ApplyMixedContent(ctx, httpURL); !errors.Is(err, protocol.ErrMixedContentBlocked) {
		t.Fatalf("script mixed content error = %v", err)
	}

	id, ok := remote.StartRequest(gen, "GET", "https://example.test/a", "doc")
	if !ok {
		t.Fatal("StartRequest = false")
	}
	remote.FinishRequest(id, gen, 201, 5, time.Millisecond, nil)
	if got := remote.RequestCount(); got != 1 {
		t.Fatalf("RequestCount = %d", got)
	}
	records := remote.RequestSnapshot()
	if len(records) != 1 || records[0].Status != 201 || records[0].Size != 5 || !records[0].Done {
		t.Fatalf("records = %+v", records)
	}
}

func TestRemoteHostCookies(t *testing.T) {
	remote, _ := hostRPCPair(t)
	u, _ := url.Parse("https://example.test/path")
	if !remote.SetDocumentCookieRaw(u, "session=abc; Path=/") {
		t.Fatal("SetDocumentCookieRaw = false")
	}
	values := remote.DocumentCookieValues(u)
	if len(values) != 1 || values[0].Name != "session" || values[0].Value != "abc" {
		t.Fatalf("DocumentCookieValues = %+v", values)
	}
	views := remote.CookieViews(u)
	if len(views) != 1 || views[0].Name != "session" {
		t.Fatalf("CookieViews = %+v", views)
	}
}

func TestRemoteHostConfigToken(t *testing.T) {
	remote, _ := hostRPCPair(t)
	token := remote.ConfigToken()
	current := token.Current()
	if current == "" {
		t.Fatal("empty config token")
	}
	if !token.Consume(current) {
		t.Fatal("config token was not consumed")
	}
	if token.Consume(current) {
		t.Fatal("config token replayed")
	}
}

func TestRemoteHostCacheAndCert(t *testing.T) {
	remote, _ := hostRPCPair(t)
	remote.SetCacheLimitBytes(1 << 20)
	if got := remote.CacheLimitBytes(); got != 1<<20 {
		t.Fatalf("CacheLimitBytes = %d", got)
	}
	if used, limit, entries := remote.CacheStats(); used != 0 || limit != 1<<20 || entries != 0 {
		t.Fatalf("CacheStats = %d/%d/%d", used, limit, entries)
	}
	if err := remote.SetCertSource("system"); err != nil {
		t.Fatal(err)
	}
	if got := remote.CertSource(); got != "system" {
		t.Fatalf("CertSource = %q", got)
	}
}
