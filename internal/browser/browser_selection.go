package browser

import (
	"fmt"
	"os"
	"strings"

	"browser/internal/frontend"
	"browser/internal/protocol"
	"unicode/utf8"
)

// cellPos is a cell in document coordinates (row 0 is the top of the page).
type cellPos struct{ X, Y int }

// selectionOrder returns the selection endpoints in reading order.
func (b *Browser) selectionOrder() (cellPos, cellPos) {
	a, c := b.selAnchor, b.selCursor
	if a.Y > c.Y || (a.Y == c.Y && a.X > c.X) {
		a, c = c, a
	}
	return a, c
}

func (b *Browser) hasSelection() bool {
	return b.selAnchor != b.selCursor
}

func (b *Browser) clearSelection() {
	b.selAnchor = cellPos{}
	b.selCursor = cellPos{}
	b.selecting = false
}

// updateSelection moves the drag endpoint, auto-scrolling when it leaves the
// content viewport.
func (b *Browser) updateSelection(x, y, top, bottom int) {
	if y < top {
		b.scrollBy(-1)
		y = top
	} else if y >= bottom {
		b.scrollBy(1)
		y = bottom - 1
	}
	if x < 0 {
		x = 0
	}
	if x >= b.width {
		x = b.width - 1
	}
	docY := b.scroll + (y - top)
	if docY < 0 {
		docY = 0
	}
	b.selCursor = cellPos{X: x, Y: docY}
}

// selectedText reconstructs the text covered by the current selection.
func (b *Browser) selectedText() string {
	if b.layout == nil || !b.hasSelection() {
		return ""
	}
	a, c := b.selectionOrder()
	var sb strings.Builder
	lastY := -1
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Kind != fragText || f.Text == "" {
			continue
		}
		if f.Y < a.Y || f.Y > c.Y {
			continue
		}
		x0, x1 := f.X, f.X+f.W-1
		if f.Y == a.Y && a.X > x0 {
			x0 = a.X
		}
		if f.Y == c.Y && c.X < x1 {
			x1 = c.X
		}
		if x1 < x0 {
			continue
		}
		if lastY >= 0 && f.Y != lastY {
			sb.WriteByte('\n')
		}
		lastY = f.Y
		cx := f.X
		for _, r := range f.Text {
			rw := runeWidth(r)
			if rw == 0 {
				continue
			}
			if cx >= x0 && cx <= x1 {
				sb.WriteRune(r)
			}
			cx += rw
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// copySelection sends the selected page text to the terminal clipboard with
// OSC 52. Terminals and multiplexers must support it for the copy to arrive.
func (b *Browser) copySelection() {
	text := b.selectedText()
	if text == "" {
		if b.url == nil || b.url.String() == "" {
			b.status = "No URL"
			return
		}
		if b.copyText(b.url.String()) {
			b.status = "Copied URL"
		}
		return
	}
	if b.copyText(text) {
		b.clearSelection()
	}
}

// copyText sends text to the terminal clipboard with OSC 52 and reports
// whether it was written.
func (b *Browser) copyText(text string) bool {
	if text == "" {
		b.status = "Nothing to copy"
		return false
	}
	truncated := false
	if len(text) > maxClipboardBytes {
		cut := maxClipboardBytes
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut]
		truncated = true
	}
	if b.sessionOut != nil {
		// The front end owns terminal escapes and the clipboard.
		if err := b.sessionOut.Send(protocol.Event{Body: protocol.Clipboard{Text: text}}); err != nil {
			b.status = "Copy failed"
			return false
		}
	} else {
		w := b.clipboardWriter
		if w == nil {
			w = os.Stdout
		}
		if _, err := fmt.Fprint(w, frontend.OSC52(text)); err != nil {
			b.status = "Copy failed"
			return false
		}
	}
	n := utf8.RuneCountInString(text)
	if truncated {
		b.status = fmt.Sprintf("Copied %d chars (truncated)", n)
	} else {
		b.status = fmt.Sprintf("Copied %d chars", n)
	}
	return true
}

func (b *Browser) selectionSummary() string {
	n := utf8.RuneCountInString(b.selectedText())
	if n == 0 {
		return "Selection"
	}
	return fmt.Sprintf("Selected %d chars (y copies)", n)
}

// paintSelection highlights selected cells in the content viewport. Only cells
// covered by rendered text fragments are tinted, so selecting across several
// lines does not paint empty margins or unrelated columns.
func (b *Browser) paintSelection(g *Grid, top, bottom int) {
	if !b.hasSelection() || b.layout == nil {
		return
	}
	a, c := b.selectionOrder()
	selBG := RGB(0x2f, 0x6f, 0xbf)
	selFG := RGB(0xf8, 0xfa, 0xfc)
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Kind != fragText || f.Text == "" || f.W < 1 {
			continue
		}
		y := f.Y - b.scroll + top
		if y < top || y >= bottom || y >= g.H {
			continue
		}
		if f.Y < a.Y || f.Y > c.Y {
			continue
		}
		x0, x1 := f.X, f.X+f.W-1
		if f.Y == a.Y && a.X > x0 {
			x0 = a.X
		}
		if f.Y == c.Y && c.X < x1 {
			x1 = c.X
		}
		if x0 < f.X {
			x0 = f.X
		}
		if x1 >= f.X+f.W {
			x1 = f.X + f.W - 1
		}
		if x0 < 0 {
			x0 = 0
		}
		if x1 >= g.W {
			x1 = g.W - 1
		}
		for x := x0; x <= x1; x++ {
			cell := &g.Cells[y*g.W+x]
			if cell.Img {
				continue
			}
			cell.BG = selBG
			if cell.R != ' ' && cell.R != 0 {
				cell.FG = selFG
			}
		}
	}
}

// copyPageText copies the whole rendered page as plain text: the rows -dump
// would print, with browser chrome, colors and focus highlights left out, so a
// layout can be pasted into a bug report. The page is painted in chunks and
// stops once the clipboard budget is reached, keeping a huge document from
// allocating a full-page grid; the status reports the truncation.
func (b *Browser) copyPageText() {
	b.copyRenderedPage(false)
}

// copyViewportText copies only the page rows visible at the current scroll
// position, with the same metadata header, so a bug report can show the frame
// the user is actually looking at.
func (b *Browser) copyViewportText() {
	b.copyRenderedPage(true)
}

func (b *Browser) copyRenderedPage(visibleOnly bool) {
	ly := b.layout
	if ly == nil || b.page == nil {
		b.status = "Nothing to copy"
		return
	}
	width := ly.Width
	if width < 1 {
		width = b.width
	}
	start := 0
	docRows := ly.Height
	rows := docRows
	label := "Copied page"
	if visibleOnly {
		label = "Copied viewport"
		// Anchor fixed and sticky fragments for the frame the user sees.
		ly.SetFixedScroll(b.scroll)
		start = b.scroll
		if start < 0 {
			start = 0
		}
		rows = b.viewportHeight()
		if rows < 1 {
			rows = 1
		}
		if start >= ly.Height {
			rows = 0
		} else if start+rows > ly.Height {
			rows = ly.Height - start
		}
	} else if ly.ScrollAnchored() && ly.FixedScrollOffset() != 0 {
		// A whole-page copy shows fixed and sticky boxes at their flow
		// positions, not shifted by the current scroll.
		ly.SetFixedScroll(0)
		defer ly.SetFixedScroll(b.scroll)
	}
	if rows < 1 {
		b.status = "Nothing to copy"
		return
	}
	pageFG := RGB(0x1f, 0x23, 0x28)
	if ly.Body != nil && ly.Body.Style != nil && ly.Body.Style.Color.Set {
		pageFG = ly.Body.Style.Color
	}
	pageBG := ly.PageBG
	if !pageBG.Set {
		pageBG = RGB(255, 255, 255)
	}
	const chunkRows = 256
	var out strings.Builder
	truncated := false
	for off := 0; off < rows; off += chunkRows {
		if out.Len() >= maxClipboardBytes {
			truncated = true
			break
		}
		h := chunkRows
		if rows-off < h {
			h = rows - off
		}
		g := NewGrid(width, h, pageFG, pageBG)
		PaintLayout(g, ly, start+off, 0, h, nil, pageFG)
		for _, line := range gridLines(g, false) {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	text := strings.TrimRight(out.String(), "\n")
	if text == "" {
		b.status = "Nothing to copy"
		return
	}
	pageLines := strings.Count(text, "\n") + 1
	copyTruncated := truncated || len(text) > maxClipboardBytes

	// A single metadata line makes a pasted page self-describing: which URL,
	// which terminal and page size, and which modes produced the text.
	url := ""
	if b.url != nil {
		url = stripTerminalControls(b.url.String())
	}
	if len(url) > 500 {
		cut := 500
		for cut > 0 && !utf8.RuneStart(url[cut]) {
			cut--
		}
		url = url[:cut] + "…"
	}
	header := fmt.Sprintf(
		"# url=%s build=%s term=%dx%d page=%dx%d rows=%d scroll=%d zoom=%s dark=%s reader=%s js=%s graphics=%s cell=%dx%d layout_truncated=%t copy_truncated=%t lines=%d",
		url, Version, b.width, b.height, width, b.viewportHeight(), docRows,
		b.scroll, zoomPercentLabel(b.zoom), onOff(b.dark), onOff(b.reader), onOff(!b.noJS),
		strings.TrimPrefix(b.graphicsStatusText(), "Graphics:"), b.cellW, b.cellH,
		ly.Truncated, copyTruncated, pageLines)
	text = header + "\n" + text
	if !b.copyText(text) {
		return
	}
	if copyTruncated || len(text) > maxClipboardBytes {
		b.status = fmt.Sprintf("%s (%d lines, truncated)", label, pageLines+1)
	} else {
		b.status = fmt.Sprintf("%s (%d lines)", label, pageLines+1)
	}
}
