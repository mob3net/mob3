package browser

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestMouseDragSelection(t *testing.T) {
	b := NewBrowser(40, 10)
	b.layout = BuildLayout(ParseHTML(`<p>hello world</p>`), nil, 40)
	b.page = &Page{URL: mustParse(t, "https://example.com"), Root: ParseHTML(`<p>hello world</p>`)}
	top, _ := b.viewport()

	var frag *Frag
	for i := range b.layout.Frags {
		if b.layout.Frags[i].Kind == fragText {
			frag = &b.layout.Frags[i]
			break
		}
	}
	if frag == nil {
		t.Fatal("no text fragment")
	}
	// Convert the fragment start into 1-based terminal coordinates.
	row := top + (frag.Y - b.scroll) + 1
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
	if b.selecting {
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
	if b.selecting {
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
	b := NewBrowser(40, 16)
	b.page = &Page{URL: mustParse(t, "https://example.com"), Root: doc}
	b.layout = BuildLayout(doc, nil, 40)
	b.scroll = 0
	var first, middle, last *Frag
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Kind != fragText {
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
	b.selAnchor = cellPos{X: first.X, Y: first.Y}
	b.selCursor = cellPos{X: last.X + last.W - 1, Y: last.Y}
	top, bottom := b.viewport()
	g := NewGrid(40, 16, RGB(0, 0, 0), RGB(255, 255, 255))
	b.paintSelection(g, top, bottom)
	selBG := RGB(0x2f, 0x6f, 0xbf)
	y := middle.Y - b.scroll + top
	if g.Cells[y*g.W+middle.X].BG != selBG {
		t.Fatal("selected text was not highlighted")
	}
	if g.Cells[y*g.W+30].BG == selBG {
		t.Fatal("empty space to the right of the content was highlighted")
	}
}

func TestCopySelectionUsesOSC52(t *testing.T) {
	b := NewBrowser(40, 10)
	b.layout = &Layout{Frags: []Frag{{Kind: fragText, Text: "hello", X: 0, Y: 0, W: 5}}}
	b.selAnchor = cellPos{X: 0, Y: 0}
	b.selCursor = cellPos{X: 4, Y: 0}
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
	b := NewBrowser(40, 10)
	b.url = mustParse(t, "https://example.com/page")
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
