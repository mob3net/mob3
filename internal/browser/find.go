package browser

import (
	"browser/internal/engine"
	"fmt"

	"browser/internal/protocol"
)

const (
	findScopePage = engine.FindScopePage
	findScopeHTML = engine.FindScopeHTML
)

// findRect is one highlight rectangle inside a match. A multi-word match can
// span several text fragments or wrapped lines.
// Find matching lives in the engine; these aliases keep the session-side
// navigation, painting and result messages readable during the migration.
type findRect = engine.FindRect
type findMatch = engine.FindMatch

func (b *Browser) openFind(scope int) { b.Session.OpenFind(scope) }

// clearFind removes the query, matches and highlights.
func (b *Browser) clearFind() { b.Session.ClearFind() }

// findStatusText describes the current match for the footer.
func (b *Browser) findStatusText() string {
	if b.FindText == "" {
		return ""
	}
	if len(b.FindMatches) == 0 {
		return "Find: no matches"
	}
	idx := b.FindIdx
	if idx < 0 || idx >= len(b.FindMatches) {
		idx = 0
	}
	status := fmt.Sprintf("Find %d/%d: %s", idx+1, len(b.FindMatches), b.FindText)
	if li := b.currentFindLink(); li != nil {
		if target := b.resolveHref(li.Href); target != "" && target != "#" {
			status += " (f → " + target + ")"
		} else {
			status += " (f follows this link)"
		}
	}
	return status
}

// currentFindLink returns the link containing the current page find match.
func (b *Browser) currentFindLink() *LinkInfo { return b.Session.CurrentFindLink() }

func (b *Browser) findBarCount() string { return b.Session.FindBarCount() }

// handleFindKey edits the find query and navigates matches.
func (b *Browser) handleFindKey(k string) bool {
	text := b.FindText
	cur := b.FindCursor
	switch k {
	case "esc":
		b.FindActive = false
		b.status = b.findStatusText()
		return false
	case "enter":
		// Confirm the query and return to normal keys; n/N now navigate.
		b.FindActive = false
		b.status = b.findStatusText()
		return false
	case "down":
		b.findNext(1)
		return false
	case "up":
		b.findNext(-1)
		return false
	case "tab", "backtab":
		b.FindActive = false
		b.status = b.findStatusText()
		return false
	}
	if k == "ctrl-a" {
		// The query has no selection model, so Ctrl-A means line start here.
		k = "home"
	}
	text, cur, _ = editLineKey(text, cur, k, editConfig{maxRunes: 512})
	changed := text != b.FindText
	b.FindText = text
	if cur > len(text) {
		cur = len(text)
	}
	b.FindCursor = cur
	if changed {
		b.refreshFind()
	}
	return false
}

// refreshFind recomputes matches and jumps to the first one at or after the
// current match/viewport.
func (b *Browser) refreshFind() {
	if b.FindText == "" {
		b.FindMatches = nil
		b.FindIdx = -1
		return
	}
	anchor := b.findAnchor()
	matches := b.computeFindMatches()
	b.FindMatches = matches
	if len(matches) == 0 {
		b.FindIdx = -1
		return
	}
	idx := 0
	for i, m := range matches {
		if m.Y >= anchor {
			idx = i
			break
		}
	}
	b.focusFindMatch(idx)
}

func (b *Browser) findAnchor() int {
	if b.FindIdx >= 0 && b.FindIdx < len(b.FindMatches) {
		return b.FindMatches[b.FindIdx].Y
	}
	if b.FindScope == findScopeHTML {
		l := newConsoleLayout(b.Height, consoleHTML)
		return paneStart(len(b.htmlLines(b.Width-2)), b.htmlScroll, l.bodyRows)
	}
	return b.Scroll
}

// reindexFind keeps highlights valid after a layout rebuild.
func (b *Browser) reindexFind() {
	if b.FindText == "" || b.FindScope != findScopePage {
		return
	}
	b.FindMatches = b.computeFindMatches()
	if len(b.FindMatches) == 0 {
		b.FindIdx = -1
		return
	}
	if b.FindIdx < 0 || b.FindIdx >= len(b.FindMatches) {
		b.FindIdx = 0
	}
}

func (b *Browser) computeFindMatches() []findMatch {
	if b.FindScope == findScopeHTML {
		return engine.FindHTMLMatches(b.htmlLines(b.Width-2), b.FindText)
	}
	return engine.FindPageMatches(b.Layout, b.FindText, b.BigText)
}

func (b *Browser) findNext(dir int) {
	if b.FindText == "" {
		return
	}
	if len(b.FindMatches) == 0 {
		b.status = "Find: no matches"
		return
	}
	idx := b.FindIdx
	if idx < 0 {
		if dir < 0 {
			idx = len(b.FindMatches) - 1
		} else {
			idx = 0
		}
	} else {
		idx = (idx + dir + len(b.FindMatches)) % len(b.FindMatches)
	}
	b.focusFindMatch(idx)
	b.status = b.findStatusText()
}

func (b *Browser) focusFindMatch(idx int) {
	if idx < 0 || idx >= len(b.FindMatches) {
		return
	}
	b.FindIdx = idx
	m := b.FindMatches[idx]
	if b.FindScope == findScopeHTML {
		rows := newConsoleLayout(b.Height, consoleHTML).bodyRows
		if rows < 1 {
			return
		}
		total := len(b.htmlLines(b.Width - 2))
		start := paneStart(total, b.htmlScroll, rows)
		if m.Line < start || m.Line >= start+rows {
			b.htmlScroll = total - rows - m.Line
			if b.htmlScroll < 0 {
				b.htmlScroll = 0
			}
		}
		return
	}
	top, bottom := b.viewport()
	rows := bottom - top
	if rows < 1 {
		return
	}
	if m.Y < b.Scroll {
		b.Scroll = m.Y
	} else if m.Y >= b.Scroll+rows {
		b.Scroll = m.Y - rows + 1
	}
	b.clampScroll()
}

// drawFindBar paints the find prompt in a footer row.
func (b *Browser) drawFindBar(g *Grid, y, w int) {
	if y < 0 || y >= g.H || w < 1 {
		return
	}
	bg := RGB(0x27, 0x44, 0x63)
	fg := RGB(0xff, 0xff, 0xff)
	g.Fill(0, y, w, 1, bg)
	st := &Style{Bold: true, Color: fg, FontSize: 16, LineHeight: 1}
	prefix := " Find: "
	g.Text(0, y, prefix, st, fg)
	x := stringWidth(prefix)
	count := b.findBarCount()
	countW := stringWidth(count)
	contentW := w - x - countW
	if contentW < 1 {
		count = ""
		countW = 0
		contentW = w - x
	}
	if contentW < 1 {
		return
	}
	runes, start, end, cur := promptWindow(b.FindText, b.FindCursor, contentW, false)
	xx := x
	for i := start; i < end; i++ {
		if i == cur {
			g.Put(xx, y, ' ', &Style{Bold: true}, RGB(0x0f, 0x14, 0x1a), RGB(0xf8, 0xfa, 0xfc))
			xx++
			continue
		}
		g.Put(xx, y, runes[i], st, fg, Color{})
		xx += runeWidth(runes[i])
	}
	if cur == len(runes) && cur >= start {
		g.Put(xx, y, ' ', &Style{Bold: true}, RGB(0x0f, 0x14, 0x1a), RGB(0xf8, 0xfa, 0xfc))
	}
	if countW > 0 {
		g.Text(w-countW, y, count, st, fg)
	}
}

func (b *Browser) findMatchBG(current bool) Color {
	if current {
		return RGB(0xff, 0x9d, 0x2a)
	}
	return RGB(0xff, 0xe0, 0x66)
}

func (b *Browser) fillFindCells(g *Grid, x, y, w int, current bool) {
	if y < 0 || y >= g.H || w < 1 {
		return
	}
	if x < 0 {
		w += x
		x = 0
	}
	if x >= g.W || w < 1 {
		return
	}
	if x+w > g.W {
		w = g.W - x
	}
	bg := b.findMatchBG(current)
	fg := RGB(0x1f, 0x23, 0x28)
	for xx := x; xx < x+w; xx++ {
		c := &g.Cells[y*g.W+xx]
		c.BG = bg
		c.FG = fg
	}
}

// paintFindPage highlights page matches in the current frame.
func (b *Browser) paintFindPage(g *Grid, top, bottom int) {
	if b.FindText == "" || b.FindScope != findScopePage {
		return
	}
	for i, m := range b.FindMatches {
		rects := m.Rects
		if len(rects) == 0 {
			rects = []findRect{{X: m.X, Y: m.Y, W: m.W, H: m.H}}
		}
		for _, r := range rects {
			y := r.Y - b.Scroll + top
			if y < top || y >= bottom || y < 0 || y >= g.H {
				continue
			}
			for dy := 0; dy < maxInt(1, r.H); dy++ {
				if y+dy >= bottom || y+dy >= g.H {
					break
				}
				b.fillFindCells(g, r.X, y+dy, r.W, i == b.FindIdx)
			}
		}
	}
}

// paintFindHTML highlights matches in the visible HTML console body.
func (b *Browser) paintFindHTML(g *Grid, start, bodyY, rows int) {
	if b.FindText == "" || b.FindScope != findScopeHTML {
		return
	}
	for i, m := range b.FindMatches {
		rects := m.Rects
		if len(rects) == 0 {
			rects = []findRect{{X: m.X, Line: m.Line, W: m.W}}
		}
		for _, r := range rects {
			rel := r.Line - start
			if rel < 0 || rel >= rows {
				continue
			}
			b.fillFindCells(g, 1+r.X, bodyY+rel, r.W, i == b.FindIdx)
		}
	}
}

// applyFindCommand updates the page find from a front-end query command. The
// front end owns the bar and caret; the engine owns match computation,
// scrolling and highlights.
func (b *Browser) applyFindCommand(v protocol.Find) {
	if v.Close {
		b.clearFind()
		b.sendFindResults()
		return
	}
	scope := findScopePage
	if v.Scope == protocol.FindHTML {
		scope = findScopeHTML
	}
	changed := scope != b.FindScope
	b.FindScope = scope
	if changed || v.Query != b.FindText {
		b.FindText = v.Query
		b.FindCursor = len(v.Query)
		b.FindMatches = nil
		b.FindIdx = -1
		b.refreshFind()
	}
	if v.Next {
		b.findNext(1)
	} else if v.Prev {
		b.findNext(-1)
	}
	b.sendFindResults()
}

func (b *Browser) sendFindResults() {
	if b.sessionOut == nil {
		return
	}
	cur := 0
	if len(b.FindMatches) > 0 {
		cur = b.FindIdx + 1
		if cur < 1 {
			cur = 1
		}
	}
	_ = b.sessionOut.Send(protocol.Event{Body: protocol.FindResults{
		Query: b.FindText, Current: cur, Total: len(b.FindMatches),
	}})
}
