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

func TestFindWordEditing(t *testing.T) {
	root := ParseHTML(`<html><body><p>alpha beta gamma</p></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 20)
	b.page = &Page{URL: u, Base: u, Root: root}
	b.layout = BuildLayout(root, nil, 60)
	typeFindQuery(t, b, func() { b.HandleKey("/") }, "alpha beta gamma")

	b.handleFindKey("alt-backspace")
	if b.findText != "alpha beta " {
		t.Fatalf("alt-backspace = %q", b.findText)
	}
	b.handleFindKey("ctrl-w")
	if b.findText != "alpha " {
		t.Fatalf("ctrl-w = %q", b.findText)
	}
	b.handleFindKey("alt-left")
	b.handleFindKey("Z")
	if b.findText != "Zalpha " {
		t.Fatalf("word motion = %q", b.findText)
	}
	b.handleFindKey("alt-d")
	if b.findText != "Z" {
		t.Fatalf("alt-d = %q", b.findText)
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

func TestFindStatusHintsLinkMatch(t *testing.T) {
	root := ParseHTML(`<html><body><p><a href="#one">evidence link</a></p><p>plain evidence text</p><div id="one">target</div></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(80, 20)
	b.page = &Page{URL: u, Base: u, Root: root}
	b.url = u
	b.layout = BuildLayout(root, nil, 80)

	// The first match is the link text.
	typeFindQuery(t, b, func() { b.HandleKey("/") }, "evidence")
	b.handleFindKey("enter")
	if !strings.Contains(b.status, "(f → https://example.com/#one)") {
		t.Fatalf("link match status = %q", b.status)
	}

	// f follows the matched link rather than opening hint mode.
	b.HandleKey("f")
	if b.findActive || b.url == nil || b.url.Fragment != "one" {
		t.Fatalf("f did not follow the matched link: active=%v url=%v", b.findActive, b.url)
	}

	// The second match is plain text, so the hint disappears.
	b.HandleKey("/")
	b.findCursor = len(b.findText)
	b.handleFindKey("ctrl-u")
	for _, r := range "plain" {
		b.handleFindKey(string(r))
	}
	b.handleFindKey("enter")
	if strings.Contains(b.status, "(f ") {
		t.Fatalf("plain match status = %q", b.status)
	}
	b.HandleKey("f")
	if !b.linkHints {
		t.Fatal("f on a plain match did not open link hints")
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
	b := NewBrowser(60, 20)
	b.page = &Page{URL: u, Base: u, Root: root}
	b.layout = BuildLayout(root, nil, 60)

	typeFindQuery(t, b, func() { b.HandleKey("/") }, "alpha")
	b.handleFindKey("enter")
	want := b.findStatusText()
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
	if b.findText == "" || b.status != b.findStatusText() {
		t.Fatalf("after n status = %q, want %q", b.status, b.findStatusText())
	}

	// Escape is the only key that resets the search.
	b.HandleKey("esc")
	if b.findText != "" || len(b.findMatches) != 0 {
		t.Fatalf("Esc did not clear find state: %q %d", b.findText, len(b.findMatches))
	}
	if b.status == want {
		t.Fatalf("Esc kept the find status %q", b.status)
	}
}
