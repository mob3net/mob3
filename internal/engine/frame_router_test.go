package engine

import (
	"errors"
	"net/url"
	"testing"
)

func routerURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func TestSessionRegistersTopLevelFrame(t *testing.T) {
	b := NewSession(80, 24)
	b.URL = routerURL(t, "https://example.com/a")
	if b.Frame.ID() != 1 {
		t.Fatalf("top-level frame ID = %d, want 1", b.Frame.ID())
	}
	o, ok := b.router.Origin(b.Frame.ID())
	if !ok || o.String() != "https://example.com" {
		t.Fatalf("top-level origin = %v, %v", o, ok)
	}
	if got := NewEmptySession().Frame.ID(); got != 1 {
		t.Fatalf("empty session top-level frame ID = %d, want 1", got)
	}
}

func TestFrameOriginIsCommittedURL(t *testing.T) {
	f := &Frame{URL: routerURL(t, "https://EXAMPLE.com:443/a")}
	if got := f.Origin().String(); got != "https://example.com" {
		t.Fatalf("frame origin = %q", got)
	}
	f.URL = routerURL(t, "about:home")
	if got := f.Origin().String(); got != "null" {
		t.Fatalf("opaque frame origin = %q", got)
	}
}

func TestFrameRouterRegistersFrames(t *testing.T) {
	r := newFrameRouter()
	a := &Frame{URL: routerURL(t, "https://a.example/x")}
	b := &Frame{URL: routerURL(t, "https://b.example/y")}
	ida, idb := r.Register(a), r.Register(b)
	if ida == 0 || idb == 0 || ida == idb {
		t.Fatalf("frame IDs = %d, %d", ida, idb)
	}
	if got := r.Register(a); got != ida {
		t.Fatalf("re-register returned %d, want %d", got, ida)
	}
	if !r.SameOrigin(ida, ida) {
		t.Fatal("frame is not same-origin with itself")
	}
	if r.SameOrigin(ida, idb) {
		t.Fatal("different origins matched")
	}
	r.Unregister(a)
	if a.ID() != 0 {
		t.Fatalf("unregistered frame keeps ID %d", a.ID())
	}
	if _, ok := r.Origin(ida); ok {
		t.Fatal("unregistered frame still resolves")
	}
}

func TestFrameRouterRoute(t *testing.T) {
	r := newFrameRouter()
	a := &Frame{URL: routerURL(t, "https://a.example/x")}
	b := &Frame{URL: routerURL(t, "https://b.example/y")}
	ida, idb := r.Register(a), r.Register(b)

	var got RoutedMessage
	r.SetDeliver(func(m RoutedMessage) error {
		got = m
		return nil
	})
	payload := []byte("hello")
	if err := r.Route(ida, idb, payload, "*"); err != nil {
		t.Fatalf("route *: %v", err)
	}
	if got.From != ida || got.To != idb || string(got.Data) != "hello" || got.Origin.String() != "https://a.example" {
		t.Fatalf("routed message = %+v", got)
	}
	got.Data[0] = 'X'
	if string(payload) != "hello" {
		t.Fatal("router did not copy the payload")
	}
	if err := r.Route(ida, idb, payload, "https://b.example"); err != nil {
		t.Fatalf("route exact target: %v", err)
	}
	if err := r.Route(ida, idb, payload, "https://a.example"); !errors.Is(err, ErrFrameTargetMismatch) {
		t.Fatalf("route wrong target = %v", err)
	}
	if err := r.Route(ida, idb, payload, ""); !errors.Is(err, ErrFrameTargetMismatch) {
		t.Fatalf("cross-origin empty target = %v", err)
	}
	if err := r.Route(ida, 0, payload, "*"); !errors.Is(err, ErrUnknownFrame) {
		t.Fatalf("unknown target = %v", err)
	}
	if err := r.Route(ida, idb, make([]byte, maxRoutedMessageBytes+1), "*"); !errors.Is(err, ErrFrameMessageTooLarge) {
		t.Fatalf("oversized message = %v", err)
	}
}

func TestFrameRouterSameOriginAndOpaque(t *testing.T) {
	r := newFrameRouter()
	a := &Frame{URL: routerURL(t, "https://example.com/a")}
	b := &Frame{URL: routerURL(t, "https://example.com:443/b")}
	c := &Frame{URL: routerURL(t, "https://other.example/c")}
	ida, idb, idc := r.Register(a), r.Register(b), r.Register(c)
	if !r.SameOrigin(ida, idb) {
		t.Fatal("default port should not split the origin")
	}
	if r.SameOrigin(ida, idc) {
		t.Fatal("different hosts matched")
	}
	if err := r.Route(ida, idb, []byte("x"), ""); err != nil {
		t.Fatalf("same-origin empty target: %v", err)
	}

	about := &Frame{URL: routerURL(t, "about:home")}
	idAbout := r.Register(about)
	if err := r.Route(idAbout, idAbout, []byte("x"), ""); err != nil {
		t.Fatalf("opaque self message: %v", err)
	}
	if err := r.Route(idAbout, ida, []byte("x"), ""); !errors.Is(err, ErrFrameTargetMismatch) {
		t.Fatalf("opaque-to-tuple message = %v", err)
	}
	if err := r.Route(idAbout, ida, []byte("x"), "*"); err != nil {
		t.Fatalf("opaque star message: %v", err)
	}
}
