package browser

import (
	"strings"
	"testing"
)

func typeFindQuery(t *testing.T, b *Browser, open func(), query string) {
	t.Helper()
	open()
	if !b.findActive {
		t.Fatal("find bar did not open")
	}
	for _, r := range query {
		b.handleFindKey(string(r))
	}
}

func TestFindPageIncrementalAndNavigate(t *testing.T) {
	root := ParseHTML(`<html><body><p>Alpha beta</p><p>alpha gamma</p></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 20)
	b.page = &Page{URL: u, Base: u, Root: root}
	b.layout = BuildLayout(root, nil, 60)

	typeFindQuery(t, b, func() { b.HandleKey("/") }, "alpha")
	if got := len(b.findMatches); got != 2 {
		t.Fatalf("matches = %d, want 2 (case-insensitive)", got)
	}
	first := b.findIdx
	b.handleFindKey("down")
	if b.findIdx == first {
		t.Fatal("Down did not move to the next match")
	}
	b.handleFindKey("up")
	if b.findIdx != first {
		t.Fatalf("Up = %d, want %d", b.findIdx, first)
	}
	// Enter confirms the query, closes the bar and leaves n/N in charge.
	b.handleFindKey("enter")
	if b.findActive {
		t.Fatal("Enter left the find bar open")
	}
	if b.findIdx != first {
		t.Fatalf("Enter changed the current match to %d, want %d", b.findIdx, first)
	}

	// Highlights use the match cell rectangle.
	g := NewGrid(60, 20, RGB(0, 0, 0), RGB(255, 255, 255))
	b.paintFindPage(g, 0, 20)
	m := b.findMatches[b.findIdx]
	if got := g.Cells[m.y*g.W+m.x].BG; got != b.findMatchBG(true) {
		t.Fatalf("current match background = %+v, want %+v", got, b.findMatchBG(true))
	}

	// Closing keeps matches; n/N still navigate.
	if len(b.findMatches) != 2 {
		t.Fatal("closing the find bar cleared the matches")
	}
	prev := b.findIdx
	b.HandleKey("n")
	if b.findIdx == prev {
		t.Fatal("n did not navigate after closing the find bar")
	}
	// Esc clears the retained highlights.
	b.HandleKey("esc")
	if b.findText != "" || len(b.findMatches) != 0 {
		t.Fatalf("second Esc did not clear find state: %q %d", b.findText, len(b.findMatches))
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
			b := NewBrowser(tc.width, 20)
			b.page = &Page{URL: u, Base: u, Root: root}
			b.layout = BuildLayout(root, nil, tc.width)
			typeFindQuery(t, b, func() { b.HandleKey("/") }, "reader mode")
			if got := len(b.findMatches); got != 2 {
				t.Fatalf("matches = %d, want 2", got)
			}
			if got := len(b.findMatches[0].rects); got < tc.minRects {
				t.Fatalf("highlight rects = %d, want at least %d", got, tc.minRects)
			}
		})
	}
}

func TestFindMultiWordNormalisesWhitespace(t *testing.T) {
	root := ParseHTML(`<html><body><p>reader   mode</p></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 20)
	b.page = &Page{URL: u, Base: u, Root: root}
	b.layout = BuildLayout(root, nil, 60)
	typeFindQuery(t, b, func() { b.HandleKey("/") }, "reader  mode")
	if got := len(b.findMatches); got != 1 {
		t.Fatalf("matches = %d, want 1", got)
	}
}

func TestFindWorksOnAboutPages(t *testing.T) {
	loader := NewLoader()
	help, err := loader.Load("about:help", nil)
	if err != nil {
		t.Fatal(err)
	}
	b := NewBrowser(80, 20)
	b.page = help
	b.layout = BuildLayout(help.Root, help.Author, 80)
	typeFindQuery(t, b, func() { b.HandleKey("/") }, "reader mode")
	if len(b.findMatches) == 0 {
		t.Fatal("help page multi-word search found no matches")
	}

	loader.bookmarks = []Bookmark{{Title: "Example Site", URL: "https://example.com/"}}
	marks, err := loader.Load("about:bookmarks", nil)
	if err != nil {
		t.Fatal(err)
	}
	b2 := NewBrowser(80, 20)
	b2.page = marks
	b2.layout = BuildLayout(marks.Root, marks.Author, 80)
	typeFindQuery(t, b2, func() { b2.HandleKey("/") }, "Example")
	if len(b2.findMatches) == 0 {
		t.Fatal("bookmarks page search found no matches")
	}
}

func TestFindHTMLConsole(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	b.consolePane = consoleHTML
	b.page = &Page{RawHTML: "<html>\n<p>Needle</p>\n<p>needle</p>\n</html>"}
	typeFindQuery(t, b, func() { b.handleConsoleKey("/") }, "needle")
	if b.findScope != findScopeHTML {
		t.Fatalf("find scope = %d, want HTML", b.findScope)
	}
	if got := len(b.findMatches); got != 2 {
		t.Fatalf("HTML matches = %d, want 2", got)
	}
	joined := strings.Join(b.htmlLines(b.width-2), "\n")
	if !strings.Contains(joined, "Needle") {
		t.Fatalf("HTML lines = %q", joined)
	}
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	if !strings.Contains(b.prev[len(b.prev)-1], "Find:") {
		t.Fatalf("find bar not drawn in console footer: %q", b.prev[len(b.prev)-1])
	}
	// The current match is highlighted in the HTML body.
	g := NewGrid(60, 20, RGB(0, 0, 0), RGB(255, 255, 255))
	l := newConsoleLayout(20, consoleHTML)
	start := paneStart(len(b.htmlLines(b.width-2)), b.htmlScroll, l.bodyRows)
	b.paintFindHTML(g, start, l.bodyY, l.bodyRows)
	m := b.findMatches[b.findIdx]
	y := l.bodyY + m.line - start
	if got := g.Cells[y*g.W+1+m.x].BG; got != b.findMatchBG(true) {
		t.Fatalf("HTML match background = %+v, want %+v", got, b.findMatchBG(true))
	}
}
