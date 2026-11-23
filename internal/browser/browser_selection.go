package browser

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	"browser/internal/engine"
	"browser/internal/protocol"
)

// Selection ordering, text extraction and painting live in the engine. The
// browser keeps the drag state and clipboard policy.

func (b *Browser) hasSelection() bool { return b.Session.HasSelection() }

func (b *Browser) clearSelection() { b.Session.ClearSelection() }

func (b *Browser) startSelection(p engine.CellPos) {
	b.Selection.Anchor = p
	b.Selection.Cursor = p
	b.Selecting = true
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
	if x >= b.Width {
		x = b.Width - 1
	}
	docY := b.Scroll + (y - top)
	if docY < 0 {
		docY = 0
	}
	b.Selection.SetCursor(engine.CellPos{X: x, Y: docY})
}

func (b *Browser) selectedText() string { return b.Selection.Text(b.Layout) }

// copySelection sends the selected page text to the terminal clipboard with
// OSC 52. Terminals and multiplexers must support it for the copy to arrive.
func (b *Browser) copySelection() {
	text := b.selectedText()
	if text == "" {
		if b.URL == nil || b.URL.String() == "" {
			b.status = "No URL"
			return
		}
		if b.copyText(b.URL.String()) {
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
			w = b.writer()
		}
		if _, err := fmt.Fprint(w, osc52(text)); err != nil {
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

func (b *Browser) selectionSummary() string { return b.Selection.Summary(b.Layout) }

// paintSelection highlights selected cells in the content viewport.
func (b *Browser) paintSelection(g *Grid, top, bottom int) {
	b.Selection.Paint(g, b.Layout, b.Scroll, top, bottom)
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
	ly := b.Layout
	if ly == nil || b.Page == nil {
		b.status = "Nothing to copy"
		return
	}
	width := ly.Width
	if width < 1 {
		width = b.Width
	}
	start := 0
	docRows := ly.Height
	rows := docRows
	label := "Copied page"
	if visibleOnly {
		label = "Copied viewport"
		// Anchor fixed and sticky fragments for the frame the user sees.
		ly.SetFixedScroll(b.Scroll)
		start = b.Scroll
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
		defer ly.SetFixedScroll(b.Scroll)
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
		for _, line := range engine.GridLines(g) {
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
	if b.URL != nil {
		url = stripTerminalControls(b.URL.String())
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
		url, buildVersion, b.Width, b.Height, width, b.viewportHeight(), docRows,
		b.Scroll, zoomPercentLabel(b.Zoom), onOff(b.Dark), onOff(b.Reader), onOff(!b.NoJS),
		strings.TrimPrefix(b.graphicsStatusText(), "Graphics:"), b.CellW, b.CellH,
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

func osc52(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
}
