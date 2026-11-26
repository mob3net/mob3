package engine

import (
	"strings"

	"browser/internal/protocol"
	"testing"
)

func typeFindQuery(t *testing.T, b *Session, _ func(), query string) {
	t.Helper()
	b.applyFindCommand(protocol.Find{Query: query})
}

func TestFindPageIncrementalAndNavigate(t *testing.T) {
	root := ParseHTML(`<html><body><p>Alpha beta</p><p>alpha gamma</p></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := newTestSession(60, 20)
	b.Page = &Page{URL: u, Base: u, Root: root}
	b.Layout = BuildLayout(root, nil, 60)

	typeFindQuery(t, b, func() { b.HandleKey("/") }, "alpha")
	if got := len(b.FindMatches); got != 2 {
		t.Fatalf("matches = %d, want 2 (case-insensitive)", got)
	}
	first := b.FindIdx
	b.applyFindCommand(protocol.Find{Query: b.FindText, Next: true})
	if b.FindIdx == first {
		t.Fatal("Next did not move to the next match")
	}
	b.applyFindCommand(protocol.Find{Query: b.FindText, Prev: true})
	if b.FindIdx != first {
		t.Fatalf("Prev = %d, want %d", b.FindIdx, first)
	}

	// Highlights use the match cell rectangle.
	g := NewGrid(60, 20, RGB(0, 0, 0), RGB(255, 255, 255))
	b.paintFindPage(g, 0, 20)
	m := b.FindMatches[b.FindIdx]
	if got := g.Cells[m.Y*g.W+m.X].BG; got != b.findMatchBG(true) {
		t.Fatalf("current match background = %+v, want %+v", got, b.findMatchBG(true))
	}

	// Closing keeps matches; n/N still navigate.
	if len(b.FindMatches) != 2 {
		t.Fatal("closing the find bar cleared the matches")
	}
	prev := b.FindIdx
	b.HandleKey("n")
	if b.FindIdx == prev {
		t.Fatal("n did not navigate after closing the find bar")
	}
	// Esc clears the retained highlights.
	b.HandleKey("esc")
	if b.FindText != "" || len(b.FindMatches) != 0 {
		t.Fatalf("second Esc did not clear find state: %q %d", b.FindText, len(b.FindMatches))
	}
}

func TestFindMultiWordQuery(t *testing.T) {
	for _, tc := range []struct {
		name     string
		width    int
		minRects int
	}{
		{name: "same line", width: 60, minRects: 1},
		{name: "wrapped", width: 10, minRects: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := ParseHTML(`<html><body><p>reader mode</p><p>reader mode again</p></body></html>`)
			u := mustParse(t, "https://example.com/")
			b := newTestSession(tc.width, 20)
			b.Page = &Page{URL: u, Base: u, Root: root}
			b.Layout = BuildLayout(root, nil, tc.width)
			typeFindQuery(t, b, func() { b.HandleKey("/") }, "reader mode")
			if got := len(b.FindMatches); got != 2 {
				t.Fatalf("matches = %d, want 2", got)
			}
			if got := len(b.FindMatches[0].Rects); got < tc.minRects {
				t.Fatalf("highlight rects = %d, want at least %d", got, tc.minRects)
			}
		})
	}
}

func TestFindMultiWordNormalisesWhitespace(t *testing.T) {
	root := ParseHTML(`<html><body><p>reader   mode</p></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := newTestSession(60, 20)
	b.Page = &Page{URL: u, Base: u, Root: root}
	b.Layout = BuildLayout(root, nil, 60)
	typeFindQuery(t, b, func() { b.HandleKey("/") }, "reader  mode")
	if got := len(b.FindMatches); got != 1 {
		t.Fatalf("matches = %d, want 1", got)
	}
}

func TestFindWorksOnAboutPages(t *testing.T) {
	loader := newTestLoader()
	help, err := loader.Load("about:help", nil)
	if err != nil {
		t.Fatal(err)
	}
	b := newTestSession(80, 20)
	b.Page = help
	b.Layout = BuildLayout(help.Root, help.Author, 80)
	typeFindQuery(t, b, func() { b.HandleKey("/") }, "reader mode")
	if len(b.FindMatches) == 0 {
		t.Fatal("help page multi-word search found no matches")
	}
}

func TestFindStatusDoesNotFollowLinks(t *testing.T) {
	root := ParseHTML(`<html><body><p><a href="#one">evidence link</a></p><p>plain evidence text</p><div id="one">target</div></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := newTestSession(80, 20)
	b.Page = &Page{URL: u, Base: u, Root: root}
	b.URL = u
	b.Layout = BuildLayout(root, nil, 80)

	// The first match is the link text, but the find status must not advertise
	// link following; f always opens link hints.
	typeFindQuery(t, b, func() { b.HandleKey("/") }, "evidence")
	b.status = b.findStatusText()
	if strings.Contains(b.status, "(f ") {
		t.Fatalf("find status advertises link following: %q", b.status)
	}
	if !strings.Contains(b.status, "n/N next/prev") {
		t.Fatalf("status is missing the result key hint: %q", b.status)
	}
	b.HandleKey("f")
	if b.FindActive || !b.LinkHints {
		t.Fatalf("f opened hints=%v instead of link hints from a find match", b.LinkHints)
	}
}

func TestFindStatusPersistsWhileScrolling(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`<html><body><p>alpha first</p>`)
	for i := 0; i < 80; i++ {
		sb.WriteString("<p>filler line</p>")
	}
	sb.WriteString(`</body></html>`)
	root := ParseHTML(sb.String())
	u := mustParse(t, "https://example.com/")
	b := newTestSession(60, 20)
	b.Page = &Page{URL: u, Base: u, Root: root}
	b.Layout = BuildLayout(root, nil, 60)

	typeFindQuery(t, b, func() { b.HandleKey("/") }, "alpha")
	want := b.findStatusText()
	b.status = want
	if want == "" || b.status != want {
		t.Fatalf("confirmed find status = %q, want %q", b.status, want)
	}

	// Scrolling with keys and the wheel leaves the find status in place.
	for _, key := range []string{"j", "j", "k", " ", "b", "ctrl-d", "ctrl-u"} {
		b.HandleKey(key)
		if b.status != want {
			t.Fatalf("after %q status = %q, want %q", key, b.status, want)
		}
	}
	b.scrollBy(3)
	if b.status != want {
		t.Fatalf("after wheel scroll status = %q, want %q", b.status, want)
	}

	// n/N still navigate and describe the new match.
	b.HandleKey("n")
	if b.FindText == "" || b.status != b.findStatusText() {
		t.Fatalf("after n status = %q, want %q", b.status, b.findStatusText())
	}

	// Escape is the only key that resets the search.
	b.HandleKey("esc")
	if b.FindText != "" || len(b.FindMatches) != 0 {
		t.Fatalf("Esc did not clear find state: %q %d", b.FindText, len(b.FindMatches))
	}
	if b.status == want {
		t.Fatalf("Esc kept the find status %q", b.status)
	}
}

func TestApplyFindCommand(t *testing.T) {
	root := ParseHTML(`<html><body><p>Alpha beta</p><p>alpha gamma</p></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := newTestSession(60, 20)
	b.Page = &Page{URL: u, Base: u, Root: root}
	b.Layout = BuildLayout(root, nil, 60)
	b.emitFrames = true
	b.frontendChrome = true

	b.applyFindCommand(protocol.Find{Query: "alpha"})
	if b.FindText != "alpha" || len(b.FindMatches) != 2 {
		t.Fatalf("query = %q matches = %d", b.FindText, len(b.FindMatches))
	}
	first := b.FindIdx
	b.applyFindCommand(protocol.Find{Query: "alpha", Next: true})
	if b.FindIdx == first {
		t.Fatal("next did not move the match")
	}
	b.applyFindCommand(protocol.Find{Query: "alpha", Prev: true})
	if b.FindIdx != first {
		t.Fatalf("prev = %d, want %d", b.FindIdx, first)
	}
	b.applyFindCommand(protocol.Find{Close: true})
	if b.FindText != "" || len(b.FindMatches) != 0 {
		t.Fatalf("close left find state: %q %d", b.FindText, len(b.FindMatches))
	}
}
