package engine

import (
	"testing"

	"browser/internal/protocol"
)

func TestContentRect(t *testing.T) {
	cases := []struct {
		h, top, bottom int
	}{
		{2, 0, 2},
		{3, 1, 2},
		{5, 2, 4},
		{20, 2, 19},
	}
	for _, tc := range cases {
		b := newTestSession(100, 24)
		b.Height = tc.h
		cr := b.contentRect()
		if cr.Top != tc.top || cr.Bottom != tc.bottom || cr.Height != tc.bottom-tc.top || cr.Width != 100 {
			t.Fatalf("height %d: rect = %+v", tc.h, cr)
		}
	}
}

func TestShellKeyRouting(t *testing.T) {
	b := newTestSession(60, 20)
	b.Layout = &Layout{Height: 100}
	if quit := b.HandleKey("j"); quit {
		t.Fatal("content key quit the browser")
	}
	if b.Scroll != 1 {
		t.Fatalf("page key did not scroll: %d", b.Scroll)
	}
	// Shell keys are front-end-owned; the engine must not act on them.
	if quit := b.HandleKey("q"); quit {
		t.Fatal("shell quit key quit the engine directly")
	}
	if b.quitPending {
		t.Fatal("shell quit key set engine quit state")
	}
}

func TestStateMessage(t *testing.T) {
	b := newTestSession(100, 30)
	b.URL = mustParse(t, "https://example.test/page")
	b.Page = &Page{URL: b.URL, Title: "Title", Root: ParseHTML("<p>x</p>")}
	b.Layout = &Layout{Height: 100}
	b.Scroll = 5
	b.Loading = true
	b.Progress = 0.42
	b.status = "Ready"
	b.Dark = true
	b.Reader = true
	b.jsErr = true
	b.moduleErr = true
	b.PageHasHover = true
	b.BigText = true
	b.LoopStopped = true
	b.loader.NoImages = false

	st := b.stateMessage()
	if st.URL != "https://example.test/page" || st.Title != "Title" || st.Status != "Ready" {
		t.Fatalf("state text = %+v", st)
	}
	if !st.Loading || st.Progress != 42 || st.Scroll != 5 || st.MaxScroll != b.maxScroll() {
		t.Fatalf("state numbers = %+v", st)
	}
	if st.Kind != protocol.PageHTML {
		t.Fatalf("kind = %v", st.Kind)
	}
	want := protocol.StateDark | protocol.StateReader | protocol.StateJSError |
		protocol.StateModuleError | protocol.StateHover | protocol.StateImages |
		protocol.StateBigText | protocol.StateLoopStopped
	if st.Flags != want {
		t.Fatalf("flags = %#x, want %#x", st.Flags, want)
	}

	b.loader.NoImages = true
	if b.stateMessage().Flags&protocol.StateImages != 0 {
		t.Fatal("images flag set with images disabled")
	}
	b.Page.PDF = &PDFDocument{}
	if b.stateMessage().Kind != protocol.PagePDF {
		t.Fatal("PDF page kind not reported")
	}
}

func TestFrontEndContentGeometry(t *testing.T) {
	b := newTestSession(80, 24)
	b.resizeWithContent(80, 24, 3, 10)
	if got := b.headerRows(); got != 3 {
		t.Fatalf("header rows = %d, want 3", got)
	}
	top, bottom := b.viewport()
	if top != 3 || bottom != 13 {
		t.Fatalf("viewport = %d,%d, want 3,13", top, bottom)
	}
	cr := b.contentRect()
	if cr.Top != 3 || cr.Bottom != 13 || cr.Height != 10 || cr.Width != 80 {
		t.Fatalf("content rect = %+v", cr)
	}
	if st := b.stateMessage(); st.HeaderRows != 3 || st.Width != 80 || st.Height != 24 {
		t.Fatalf("state geometry = %+v", st)
	}

	// A direct resize without content fields keeps the legacy derivation.
	b.resize(80, 24)
	if top, bottom = b.viewport(); top != 2 || bottom != 23 {
		t.Fatalf("legacy viewport = %d,%d, want 2,23", top, bottom)
	}
}
