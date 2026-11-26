package engine

import (
	"fmt"

	"browser/internal/protocol"
)

// findState is the in-page find session.
type findState struct {
	FindActive  bool
	FindText    string
	FindCursor  int
	FindScope   int
	FindMatches []FindMatch
	FindIdx     int
}

const (
	findScopePage = FindScopePage
	findScopeHTML = FindScopeHTML
)

// findRect is one highlight rectangle inside a match. A multi-word match can
// span several text fragments or wrapped lines.
// Find matching lives in the engine; these aliases keep the session-side
// navigation, painting and result messages readable during the migration.
type findRect = FindRect
type findMatch = FindMatch

// clearFind removes the query, matches and highlights.
func (b *Session) clearFind() { b.ClearFind() }

// findStatusText describes the current match for the footer.
func (b *Session) findStatusText() string {
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
	return status + " · n/N next/prev"
}

// currentFindLink returns the link containing the current page find match.
func (b *Session) currentFindLink() *LinkInfo { return b.CurrentFindLink() }

// refreshFind recomputes matches and jumps to the first one at or after the
// current match/viewport.
func (b *Session) refreshFind() {
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

func (b *Session) findAnchor() int {
	if b.FindIdx >= 0 && b.FindIdx < len(b.FindMatches) {
		return b.FindMatches[b.FindIdx].Y
	}
	if b.FindScope == findScopeHTML {
		if b.window.valid {
			if b.window.start >= 0 {
				return b.window.start
			}
			if b.window.rows > 0 {
				return clampConsoleStart(len(b.htmlLines(b.window.width-2)), -1, b.window.rows)
			}
		}
		return 0
	}
	return b.Scroll
}

// reindexFind keeps highlights valid after a layout rebuild.
func (b *Session) reindexFind() {
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

func (b *Session) computeFindMatches() []findMatch {
	if b.FindScope == findScopeHTML {
		width := b.Width - 2
		if b.window.valid {
			width = b.window.width - 2
		}
		return FindHTMLMatches(b.htmlLines(width), b.FindText)
	}
	return FindPageMatches(b.Layout, b.FindText, b.BigText)
}

func (b *Session) findNext(dir int) {
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

func (b *Session) focusFindMatch(idx int) {
	if idx < 0 || idx >= len(b.FindMatches) {
		return
	}
	b.FindIdx = idx
	m := b.FindMatches[idx]
	if b.FindScope == findScopeHTML {
		// The front end scrolls to the reported FindLine; the engine keeps no
		// console scroll state.
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

func (b *Session) findMatchBG(current bool) Color {
	if current {
		return RGB(0xff, 0x9d, 0x2a)
	}
	return RGB(0xff, 0xe0, 0x66)
}

func (b *Session) fillFindCells(g *Grid, x, y, w int, current bool) {
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
func (b *Session) paintFindPage(g *Grid, top, bottom int) {
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
			for dy := 0; dy < MaxInt(1, r.H); dy++ {
				if y+dy >= bottom || y+dy >= g.H {
					break
				}
				b.fillFindCells(g, r.X, y+dy, r.W, i == b.FindIdx)
			}
		}
	}
}

// applyFindCommand updates the page find from a front-end query command. The
// front end owns the bar and caret; the engine owns match computation,
// scrolling and highlights.
func (b *Session) applyFindCommand(v protocol.Find) {
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
	// Next/Prev commands may omit the query to keep the current search. A plain
	// empty query still clears it, so only an explicit query updates the text.
	if v.Query != "" || (!v.Next && !v.Prev) {
		if changed || v.Query != b.FindText {
			b.FindText = v.Query
			b.FindCursor = len(v.Query)
			b.FindMatches = nil
			b.FindIdx = -1
			b.refreshFind()
		}
	} else if changed && b.FindText != "" {
		b.refreshFind()
	}
	if v.Next {
		b.findNext(1)
	} else if v.Prev {
		b.findNext(-1)
	}
	b.sendFindResults()
}

func (b *Session) sendFindResults() {
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
