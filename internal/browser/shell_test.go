package browser

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
		b := NewBrowser(100, 24)
		b.height = tc.h
		cr := b.contentRect()
		if cr.Top != tc.top || cr.Bottom != tc.bottom || cr.Height != tc.bottom-tc.top || cr.Width != 100 {
			t.Fatalf("height %d: rect = %+v", tc.h, cr)
		}
	}
}

func TestShellKeyRouting(t *testing.T) {
	b := NewBrowser(60, 20)
	b.layout = &Layout{Height: 100}
	b.prompt = true
	b.promptText = ""

	if quit := b.HandleKey("j"); quit {
		t.Fatal("prompt key quit the browser")
	}
	if b.scroll != 0 {
		t.Fatalf("prompt key scrolled the page: %d", b.scroll)
	}
	if b.promptText != "j" {
		t.Fatalf("prompt text = %q, want j", b.promptText)
	}

	b.prompt = false
	b.HandleKey("j")
	if b.scroll != 1 {
		t.Fatalf("page key did not scroll: %d", b.scroll)
	}

	b.quitPending = true
	if !b.HandleKey("y") {
		t.Fatal("quit confirmation was not consumed")
	}
	if b.quitPending {
		t.Fatal("quit confirmation did not clear")
	}
}

func TestStateMessage(t *testing.T) {
	b := NewBrowser(100, 30)
	b.url = mustParse(t, "https://example.test/page")
	b.page = &Page{URL: b.url, Title: "Title", Root: ParseHTML("<p>x</p>")}
	b.layout = &Layout{Height: 100}
	b.scroll = 5
	b.loading = true
	b.progress = 0.42
	b.status = "Ready"
	b.dark = true
	b.reader = true
	b.jsErr = true
	b.moduleErr = true
	b.pageHasHover = true
	b.bigText = true
	b.loopStopped = true
	b.loader.noImages = false

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

	b.loader.noImages = true
	if b.stateMessage().Flags&protocol.StateImages != 0 {
		t.Fatal("images flag set with images disabled")
	}
	b.page.PDF = &PDFDocument{}
	if b.stateMessage().Kind != protocol.PagePDF {
		t.Fatal("PDF page kind not reported")
	}
}

func TestFrontEndContentGeometry(t *testing.T) {
	b := NewBrowser(80, 24)
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
