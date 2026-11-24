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
