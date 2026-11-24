package engine

import (
	"net/url"
	"testing"
)

func TestSessionTopLevelFrameHoldsDocumentState(t *testing.T) {
	b := NewSession(80, 24)
	if b.Frame == nil {
		t.Fatal("session has no top-level frame")
	}
	page := &Page{}
	layout := &Layout{Height: 40}
	u, _ := url.Parse("https://example.test/a")

	b.Page = page
	b.Layout = layout
	b.URL = u
	b.Scroll = 7

	if b.Frame.Page != page || b.Frame.Layout != layout || b.Frame.URL != u || b.Frame.Scroll != 7 {
		t.Fatalf("top-level frame does not hold the session document state")
	}
}

func TestNewEmptySessionHasTopLevelFrame(t *testing.T) {
	if s := NewEmptySession(); s.Frame == nil {
		t.Fatal("empty session has no top-level frame")
	}
}

func TestFrameOwnsJSRuntime(t *testing.T) {
	b := NewSession(80, 24)
	page := &Page{Root: ParseHTML(`<html><body></body></html>`)}
	b.Page = page
	b.startJS(page)
	if b.Frame.JS == nil || b.JS != b.Frame.JS {
		t.Fatal("session JavaScript runtime is not owned by the top-level frame")
	}
	b.StopJS()
	if b.Frame.JS != nil {
		t.Fatal("stopping JavaScript left the frame runtime in place")
	}
}

func TestFrameJSHostUsesFrameState(t *testing.T) {
	b := NewSession(80, 24)
	f := b.Frame
	f.Layout = &Layout{Height: 100}
	f.Scroll = 4
	h := newFrameJSHost(b, f)
	if h.Layout() != f.Layout || h.Scroll() != 4 {
		t.Fatalf("host layout/scroll not frame-owned: %v %d", h.Layout(), h.Scroll())
	}
	h.SetScroll(9)
	if f.Scroll != 9 {
		t.Fatalf("host did not write frame scroll: %d", f.Scroll)
	}
}

func TestFrameOwnsJSTimers(t *testing.T) {
	b := NewSession(80, 24)
	page := &Page{Root: ParseHTML(`<html><body></body></html>`)}
	b.Page = page
	b.startJS(page)
	if _, err := b.JS.RunCode("test", `setTimeout(function(){}, 500);`); err != nil {
		t.Fatalf("setTimeout: %v", err)
	}
	if _, ok := b.Frame.nextJSTimerDelay(b); !ok {
		t.Fatal("frame did not report its pending timer")
	}
}
