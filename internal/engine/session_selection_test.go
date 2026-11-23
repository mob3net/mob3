package engine

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func TestMouseDragSelection(t *testing.T) {
	b := newTestSession(40, 10)
	b.Layout = BuildLayout(ParseHTML(`<p>hello world</p>`), nil, 40)
	b.Page = &Page{URL: mustParse(t, "https://example.com"), Root: ParseHTML(`<p>hello world</p>`)}
	top, _ := b.viewport()

	var frag *Frag
	for i := range b.Layout.Frags {
		if b.Layout.Frags[i].Kind == FragText {
			frag = &b.Layout.Frags[i]
			break
		}
	}
	if frag == nil {
		t.Fatal("no text fragment")
	}
	// Convert the fragment start into 1-based terminal coordinates.
	row := top + (frag.Y - b.Scroll) + 1
	col := frag.X + 1
	b.HandleMouse(MouseEvent{Button: 0, X: col, Y: row, Press: true})
	b.HandleMouse(MouseEvent{Button: 0, X: col + 4, Y: row, Press: true, Motion: true})
	b.HandleMouse(MouseEvent{Button: 0, X: col + 4, Y: row, Press: false})

	if !b.hasSelection() {
		t.Fatal("drag did not create a selection")
	}
	if got := b.selectedText(); !strings.Contains(got, "hello") {
		t.Fatalf("selected text = %q", got)
	}
	if !strings.Contains(b.status, "Selected") {
		t.Fatalf("status = %q", b.status)
	}

	g := NewGrid(40, 10, RGB(0, 0, 0), RGB(255, 255, 255))
	b.paintSelection(g, top, 9)
	selected := false
	for _, c := range g.Cells {
		if c.BG == RGB(0x2f, 0x6f, 0xbf) {
			selected = true
			break
		}
	}
	if !selected {
		t.Fatal("selection was not highlighted")
	}

	// A release report carrying the motion bit still ends the drag.
	b.clearSelection()
	b.HandleMouse(MouseEvent{Button: 0, X: col, Y: row, Press: true})
	b.HandleMouse(MouseEvent{Button: 0, X: col + 4, Y: row, Press: true, Motion: true})
	b.HandleMouse(MouseEvent{Button: 0, X: col + 4, Y: row, Press: false, Motion: true})
	if b.Selecting {
		t.Fatal("release with motion bit kept the selection drag active")
	}
	if !b.hasSelection() {
		t.Fatal("selection was lost after a release with the motion bit")
	}

	// Releasing outside the page also ends the drag.
	b.clearSelection()
	b.HandleMouse(MouseEvent{Button: 0, X: col, Y: row, Press: true})
	b.HandleMouse(MouseEvent{Button: 0, X: col + 4, Y: row, Press: true, Motion: true})
	b.HandleMouse(MouseEvent{Button: 0, X: col, Y: 1, Press: false})
	if b.Selecting {
		t.Fatal("release over the toolbar kept the selection drag active")
	}

	// Escape clears the selection.
	b.HandleKey("esc")
	if b.hasSelection() {
		t.Fatal("escape did not clear the selection")
	}
}

func TestSelectionHighlightsOnlyContent(t *testing.T) {
	doc := ParseHTML(`<html><body><p>one</p><p>two</p><p>three</p></body></html>`)
	b := newTestSession(40, 16)
	b.Page = &Page{URL: mustParse(t, "https://example.com"), Root: doc}
	b.Layout = BuildLayout(doc, nil, 40)
	b.Scroll = 0
	var first, middle, last *Frag
	for i := range b.Layout.Frags {
		f := &b.Layout.Frags[i]
		if f.Kind != FragText {
			continue
		}
		switch f.Text {
		case "one":
			first = f
		case "two":
			middle = f
		case "three":
			last = f
		}
	}
	if first == nil || middle == nil || last == nil {
		t.Fatalf("missing paragraph fragments: %v %v %v", first, middle, last)
	}
	b.Selection.Anchor = CellPos{X: first.X, Y: first.Y}
	b.Selection.Cursor = CellPos{X: last.X + last.W - 1, Y: last.Y}
	top, bottom := b.viewport()
	g := NewGrid(40, 16, RGB(0, 0, 0), RGB(255, 255, 255))
	b.paintSelection(g, top, bottom)
	selBG := RGB(0x2f, 0x6f, 0xbf)
	y := middle.Y - b.Scroll + top
	if g.Cells[y*g.W+middle.X].BG != selBG {
		t.Fatal("selected text was not highlighted")
	}
	if g.Cells[y*g.W+30].BG == selBG {
		t.Fatal("empty space to the right of the content was highlighted")
	}
}

func TestCopySelectionUsesOSC52(t *testing.T) {
	b := newTestSession(40, 10)
	b.Layout = &Layout{Frags: []Frag{{Kind: FragText, Text: "hello", X: 0, Y: 0, W: 5}}}
	b.Selection.Anchor = CellPos{X: 0, Y: 0}
	b.Selection.Cursor = CellPos{X: 4, Y: 0}
	var buf bytes.Buffer
	b.clipboardWriter = &buf
	b.HandleKey("y")
	out := buf.String()
	if !strings.HasPrefix(out, "\x1b]52;c;") || !strings.HasSuffix(out, "\x07") {
		t.Fatalf("clipboard sequence = %q", out)
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(out, "\x1b]52;c;"), "\x07")
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || string(decoded) != "hello" {
		t.Fatalf("clipboard text = %q, %v", decoded, err)
	}
	if b.status != "Copied 5 chars" {
		t.Fatalf("status = %q", b.status)
	}
	if b.hasSelection() {
		t.Fatal("copy did not clear the selection")
	}
}

func TestCopySelectionWithoutSelectionCopiesURL(t *testing.T) {
	b := newTestSession(40, 10)
	b.URL = mustParse(t, "https://example.com/page")
	var buf bytes.Buffer
	b.clipboardWriter = &buf
	b.HandleKey("y")
	raw := strings.TrimSuffix(strings.TrimPrefix(buf.String(), "\x1b]52;c;"), "\x07")
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || string(decoded) != "https://example.com/page" {
		t.Fatalf("clipboard text = %q, %v", decoded, err)
	}
	if b.status != "Copied URL" {
		t.Fatalf("status = %q", b.status)
	}
}

// TestYankWholePageAsText checks that Y copies the rendered page as plain text
// with no chrome, colors or terminal escapes.
func TestYankWholePageAsText(t *testing.T) {
	root := ParseHTML(`<html><head><style>body{margin:0}p{margin:0}</style></head>` +
		`<body><p>First line</p><p>Second line</p></body></html>`)
	u := mustParse(t, "https://example.com/debug")
	b := newTestSession(40, 10)
	b.Page = &Page{URL: u, Base: u, Root: root}
	b.URL = u
	b.rebuild()
	var buf bytes.Buffer
	b.clipboardWriter = &buf

	b.HandleKey("Y")
	out := buf.String()
	if !strings.HasPrefix(out, "\x1b]52;c;") || !strings.HasSuffix(out, "\x07") {
		t.Fatalf("clipboard sequence = %q", out)
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(out, "\x1b]52;c;"), "\x07")
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	text := string(decoded)
	for _, want := range []string{"First line", "Second line"} {
		if !strings.Contains(text, want) {
			t.Errorf("yanked page is missing %q:\n%s", want, text)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasSuffix(line, " ") {
			t.Errorf("yanked line keeps trailing spaces: %q", line)
		}
	}
	if !strings.HasPrefix(text, "# url=https://example.com/debug ") {
		t.Errorf("yanked page has no metadata header:\n%s", text)
	}
	if strings.Count(text, "https://example.com/debug") != 1 {
		t.Errorf("yanked page repeats the URL outside the header:\n%s", text)
	}
	for _, want := range []string{
		"build=", "term=40x10", "page=39x7", "scroll=0", "zoom=100%", "dark=off",
		"reader=off", "js=on", "graphics=none", "cell=8x16", "lines=",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("metadata header is missing %q:\n%s", want, text)
		}
	}
	if strings.ContainsAny(text, "\x1b") {
		t.Errorf("yanked page includes terminal escapes")
	}
	if !strings.HasPrefix(b.status, "Copied page (") {
		t.Fatalf("status after Y = %q", b.status)
	}
}

func TestYankWholePageWithoutPage(t *testing.T) {
	b := newTestSession(40, 10)
	var buf bytes.Buffer
	b.clipboardWriter = &buf
	b.HandleKey("Y")
	if b.status != "Nothing to copy" {
		t.Fatalf("status = %q, want Nothing to copy", b.status)
	}
	if buf.Len() != 0 {
		t.Fatalf("clipboard written without a page: %q", buf.String())
	}
}

// TestCopyViewportText checks that Ctrl-Y copies only the page rows visible at
// the current scroll, with the same metadata header as Y.
func TestCopyViewportText(t *testing.T) {
	var body strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&body, "<p>line%02d</p>", i)
	}
	root := ParseHTML(`<html><head><style>body{margin:0}p{margin:0}</style></head>` +
		`<body>` + body.String() + `</body></html>`)
	u := mustParse(t, "https://example.com/scroll")
	b := newTestSession(40, 10)
	b.Page = &Page{URL: u, Base: u, Root: root, Author: fixtureAuthorRules(root)}
	b.URL = u
	b.rebuild()
	b.Scroll = 20
	var buf bytes.Buffer
	b.clipboardWriter = &buf

	b.HandleKey("ctrl-y")
	out := buf.String()
	if !strings.HasPrefix(out, "\x1b]52;c;") || !strings.HasSuffix(out, "\x07") {
		t.Fatalf("clipboard sequence = %q", out)
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(out, "\x1b]52;c;"), "\x07")
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	text := string(decoded)
	if !strings.HasPrefix(text, "# url=https://example.com/scroll ") || !strings.Contains(text, "scroll=20") {
		t.Fatalf("viewport copy has no scroll header:\n%s", text)
	}
	if !strings.Contains(text, "line20") || !strings.Contains(text, "line26") {
		t.Errorf("viewport copy misses visible rows:\n%s", text)
	}
	if strings.Contains(text, "line00") || strings.Contains(text, "line27") {
		t.Errorf("viewport copy includes rows outside the viewport:\n%s", text)
	}
	if !strings.HasPrefix(b.status, "Copied viewport (") {
		t.Fatalf("status after Ctrl-Y = %q", b.status)
	}
}
