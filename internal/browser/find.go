package browser

import (
	"fmt"
	"unicode"
	"unicode/utf8"
)

const (
	findScopePage = iota
	findScopeHTML
)

// findRect is one highlight rectangle inside a match. A multi-word match can
// span several text fragments or wrapped lines.
type findRect struct {
	x, y int // cells; y is the document row for page matches
	line int // HTML display line, -1 for page matches
	w, h int
}

// findMatch is one search hit. Page matches point at layout fragments; HTML
// matches point at wrapped lines in the console HTML tab.
type findMatch struct {
	frag  int // -1 for HTML matches
	line  int // first HTML display line
	x, y  int // first rectangle
	w, h  int
	rects []findRect
}

// findUnit maps one normalised search rune to its screen position.
type findUnit struct {
	frag int
	line int
	x, y int
	w, h int
	real bool
}

// openFind starts an incremental find for the page or the console HTML tab.
func (b *Browser) openFind(scope int) {
	b.findActive = true
	b.findScope = scope
	b.findText = ""
	b.findCursor = 0
	b.findMatches = nil
	b.findIdx = -1
}

// clearFind removes the query, matches and highlights.
func (b *Browser) clearFind() {
	b.findActive = false
	b.findText = ""
	b.findCursor = 0
	b.findMatches = nil
	b.findIdx = -1
}

// findStatusText describes the current match for the footer.
func (b *Browser) findStatusText() string {
	if b.findText == "" {
		return ""
	}
	if len(b.findMatches) == 0 {
		return "Find: no matches"
	}
	idx := b.findIdx
	if idx < 0 || idx >= len(b.findMatches) {
		idx = 0
	}
	return fmt.Sprintf("Find %d/%d: %s", idx+1, len(b.findMatches), b.findText)
}

func (b *Browser) findBarCount() string {
	if b.findText == "" || len(b.findMatches) == 0 {
		return " 0/0 "
	}
	idx := b.findIdx
	if idx < 0 || idx >= len(b.findMatches) {
		idx = 0
	}
	return fmt.Sprintf(" %d/%d ", idx+1, len(b.findMatches))
}

// handleFindKey edits the find query and navigates matches.
func (b *Browser) handleFindKey(k string) bool {
	text := b.findText
	cur := b.findCursor
	if cur < 0 {
		cur = 0
	}
	if cur > len(text) {
		cur = len(text)
	}
	switch k {
	case "esc":
		b.findActive = false
		b.status = b.findStatusText()
		return false
	case "enter":
		// Confirm the query and return to normal keys; n/N now navigate.
		b.findActive = false
		b.status = b.findStatusText()
		return false
	case "down":
		b.findNext(1)
		return false
	case "up":
		b.findNext(-1)
		return false
	case "tab", "backtab":
		b.findActive = false
		b.status = b.findStatusText()
		return false
	case "left", "ctrl-b":
		cur = prevRuneBoundary(text, cur)
	case "right", "ctrl-f":
		cur = nextRuneBoundary(text, cur)
	case "alt-b", "ctrl-left", "alt-left":
		cur = prevWordBoundary(text, cur)
	case "alt-f", "ctrl-right", "alt-right":
		cur = nextWordBoundary(text, cur)
	case "home", "ctrl-a":
		cur = 0
	case "end", "ctrl-e":
		cur = len(text)
	case "backspace":
		if cur > 0 {
			prev := prevRuneBoundary(text, cur)
			text = text[:prev] + text[cur:]
			cur = prev
		}
	case "alt-backspace", "ctrl-backspace", "ctrl-w":
		cut := prevWordBoundary(text, cur)
		if cut < cur {
			text = text[:cut] + text[cur:]
			cur = cut
		}
	case "delete":
		if cur < len(text) {
			next := nextRuneBoundary(text, cur)
			text = text[:cur] + text[next:]
		}
	case "alt-delete", "ctrl-delete", "alt-d":
		end := forwardWordBoundary(text, cur)
		if end > cur {
			text = text[:cur] + text[end:]
		}
	case "ctrl-u":
		text = ""
		cur = 0
	case "ctrl-k":
		text = text[:cur]
	default:
		if utf8.RuneCountInString(k) == 1 {
			r, _ := utf8.DecodeRuneInString(k)
			if r >= 32 && r != 127 && len([]rune(text)) < 512 {
				text = text[:cur] + k + text[cur:]
				cur += len(k)
			}
		}
	}
	changed := text != b.findText
	b.findText = text
	if cur > len(text) {
		cur = len(text)
	}
	b.findCursor = cur
	if changed {
		b.refreshFind()
	}
	return false
}

// refreshFind recomputes matches and jumps to the first one at or after the
// current match/viewport.
func (b *Browser) refreshFind() {
	if b.findText == "" {
		b.findMatches = nil
		b.findIdx = -1
		return
	}
	anchor := b.findAnchor()
	matches := b.computeFindMatches()
	b.findMatches = matches
	if len(matches) == 0 {
		b.findIdx = -1
		return
	}
	idx := 0
	for i, m := range matches {
		if m.y >= anchor {
			idx = i
			break
		}
	}
	b.focusFindMatch(idx)
}

func (b *Browser) findAnchor() int {
	if b.findIdx >= 0 && b.findIdx < len(b.findMatches) {
		return b.findMatches[b.findIdx].y
	}
	if b.findScope == findScopeHTML {
		l := newConsoleLayout(b.height, consoleHTML)
		return paneStart(len(b.htmlLines(b.width-2)), b.htmlScroll, l.bodyRows)
	}
	return b.scroll
}

// reindexFind keeps highlights valid after a layout rebuild.
func (b *Browser) reindexFind() {
	if b.findText == "" || b.findScope != findScopePage {
		return
	}
	b.findMatches = b.computeFindMatches()
	if len(b.findMatches) == 0 {
		b.findIdx = -1
		return
	}
	if b.findIdx < 0 || b.findIdx >= len(b.findMatches) {
		b.findIdx = 0
	}
}

func (b *Browser) computeFindMatches() []findMatch {
	if b.findScope == findScopeHTML {
		return b.computeHTMLMatches()
	}
	return b.computePageMatches()
}

func (b *Browser) computePageMatches() []findMatch {
	if b.layout == nil {
		return nil
	}
	query := normalizeFindRunes(b.findText)
	if len(query) == 0 {
		return nil
	}
	if !containsFindSpace(query) {
		return b.computePageSingle(query)
	}
	text, units := b.pageFindStream()
	return findMatchesInStream(text, units, query, false)
}

func (b *Browser) computeHTMLMatches() []findMatch {
	query := normalizeFindRunes(b.findText)
	if len(query) == 0 {
		return nil
	}
	lines := b.htmlLines(b.width - 2)
	if !containsFindSpace(query) {
		return computeHTMLSingle(lines, query)
	}
	text, units := b.htmlFindStream(lines)
	return findMatchesInStream(text, units, query, true)
}

// computePageSingle keeps the common one-word case allocation-free beyond the
// result slice; multi-word queries use the full visual text stream.
func (b *Browser) computePageSingle(query []rune) []findMatch {
	var out []findMatch
	for fi := range b.layout.Frags {
		f := &b.layout.Frags[fi]
		if f.Kind != fragText || f.Text == "" {
			continue
		}
		scale := 1
		if b.bigText && f.Style != nil {
			scale = f.Style.TextScale()
		}
		runes := []rune(f.Text)
		for _, span := range findRuneSpans(runes, query) {
			x := f.X + cellsForRunes(runes[:span[0]], scale)
			w := cellsForRunes(runes[span[0]:span[1]], scale)
			if w < 1 {
				continue
			}
			out = append(out, findMatch{frag: fi, x: x, y: f.Y, w: w, h: scale})
			if len(out) >= 5000 {
				return out
			}
		}
	}
	return out
}

func computeHTMLSingle(lines []string, query []rune) []findMatch {
	var out []findMatch
	for li, line := range lines {
		runes := []rune(line)
		for _, span := range findRuneSpans(runes, query) {
			x := cellsForRunes(runes[:span[0]], 1)
			w := cellsForRunes(runes[span[0]:span[1]], 1)
			if w < 1 {
				continue
			}
			out = append(out, findMatch{frag: -1, line: li, x: x, w: w, h: 1})
			if len(out) >= 5000 {
				return out
			}
		}
	}
	return out
}

func containsFindSpace(runes []rune) bool {
	for _, r := range runes {
		if isFindSpace(r) {
			return true
		}
	}
	return false
}

// pageFindStream flattens visible text in visual order. Spaces and line breaks
// are normalised so a query such as "reader mode" can span word fragments and
// wrapped lines.
func (b *Browser) pageFindStream() ([]rune, []findUnit) {
	var text []rune
	var units []findUnit
	prevY := -1
	prevRune := rune(0)
	for fi := range b.layout.Frags {
		f := &b.layout.Frags[fi]
		if f.Kind != fragText || f.Text == "" {
			continue
		}
		runes := []rune(f.Text)
		if prevY >= 0 && f.Y != prevY && !isFindSpace(prevRune) && !isFindSpace(runes[0]) {
			appendFindRune(&text, &units, ' ', findUnit{})
		}
		scale := 1
		if b.bigText && f.Style != nil {
			scale = f.Style.TextScale()
		}
		x := f.X
		for _, r := range runes {
			w := runeWidth(r) * scale
			u := findUnit{frag: fi, line: -1, x: x, y: f.Y, w: w, h: scale, real: true}
			appendFindRune(&text, &units, r, u)
			x += w
		}
		prevY = f.Y
		prevRune = runes[len(runes)-1]
	}
	return text, units
}

// htmlFindStream flattens console HTML lines with the same whitespace rules as
// page finding, so a phrase can wrap across display lines.
func (b *Browser) htmlFindStream(lines []string) ([]rune, []findUnit) {
	var text []rune
	var units []findUnit
	prevRune := rune(0)
	for li, line := range lines {
		runes := []rune(line)
		if len(runes) == 0 {
			continue
		}
		if len(text) > 0 && !isFindSpace(prevRune) && !isFindSpace(runes[0]) {
			appendFindRune(&text, &units, ' ', findUnit{})
		}
		x := 0
		for _, r := range runes {
			w := runeWidth(r)
			u := findUnit{frag: -1, line: li, x: x, w: w, h: 1, real: true}
			appendFindRune(&text, &units, r, u)
			x += w
		}
		prevRune = runes[len(runes)-1]
	}
	return text, units
}

func normalizeFindRunes(s string) []rune {
	var out []rune
	for _, r := range s {
		appendFindRune(&out, nil, r, findUnit{})
	}
	return out
}

// appendFindRune adds a rune and collapses runs of whitespace to one space.
// units may be nil when only normalising a query.
func appendFindRune(text *[]rune, units *[]findUnit, r rune, u findUnit) {
	if isFindSpace(r) {
		if len(*text) == 0 || isFindSpace((*text)[len(*text)-1]) {
			return
		}
		r = ' '
	}
	*text = append(*text, r)
	if units != nil {
		*units = append(*units, u)
	}
}

func isFindSpace(r rune) bool {
	return unicode.IsSpace(r) || r == ' '
}

// findMatchesInStream matches a normalised query and groups each occurrence
// into one or more highlight rectangles.
func findMatchesInStream(text []rune, units []findUnit, query []rune, html bool) []findMatch {
	var out []findMatch
	for _, span := range findRuneSpans(text, query) {
		var rects []findRect
		var cur *findRect
		for i := span[0]; i < span[1]; i++ {
			u := units[i]
			if !u.real {
				continue
			}
			if cur != nil && u.y == cur.y && (html || u.x <= cur.x+cur.w) &&
				(!html || u.line == cur.line) {
				if end := u.x + u.w; end > cur.x+cur.w {
					cur.w = end - cur.x
				}
				continue
			}
			if cur != nil {
				rects = append(rects, *cur)
			}
			cur = &findRect{x: u.x, y: u.y, line: u.line, w: u.w, h: u.h}
		}
		if cur != nil {
			rects = append(rects, *cur)
		}
		if len(rects) == 0 {
			continue
		}
		first := rects[0]
		m := findMatch{
			frag: firstFrag(units[span[0]:span[1]], html),
			line: first.line, x: first.x, y: first.y, w: first.w, h: first.h,
			rects: rects,
		}
		out = append(out, m)
		if len(out) >= 5000 {
			return out
		}
	}
	return out
}

func firstFrag(units []findUnit, html bool) int {
	if html {
		return -1
	}
	for _, u := range units {
		if u.real {
			return u.frag
		}
	}
	return -1
}

// findRuneSpans returns case-insensitive, possibly overlapping match spans.
func findRuneSpans(text, query []rune) [][2]int {
	if len(query) == 0 || len(query) > len(text) {
		return nil
	}
	var out [][2]int
	for i := 0; i+len(query) <= len(text); i++ {
		ok := true
		for j := range query {
			if unicode.ToLower(text[i+j]) != unicode.ToLower(query[j]) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, [2]int{i, i + len(query)})
		}
	}
	return out
}

func cellsForRunes(runes []rune, scale int) int {
	if scale < 1 {
		scale = 1
	}
	w := 0
	for _, r := range runes {
		w += runeWidth(r) * scale
	}
	return w
}

func (b *Browser) findNext(dir int) {
	if b.findText == "" {
		return
	}
	if len(b.findMatches) == 0 {
		b.status = "Find: no matches"
		return
	}
	idx := b.findIdx
	if idx < 0 {
		if dir < 0 {
			idx = len(b.findMatches) - 1
		} else {
			idx = 0
		}
	} else {
		idx = (idx + dir + len(b.findMatches)) % len(b.findMatches)
	}
	b.focusFindMatch(idx)
	b.status = b.findStatusText()
}

func (b *Browser) focusFindMatch(idx int) {
	if idx < 0 || idx >= len(b.findMatches) {
		return
	}
	b.findIdx = idx
	m := b.findMatches[idx]
	if b.findScope == findScopeHTML {
		rows := newConsoleLayout(b.height, consoleHTML).bodyRows
		if rows < 1 {
			return
		}
		total := len(b.htmlLines(b.width - 2))
		start := paneStart(total, b.htmlScroll, rows)
		if m.line < start || m.line >= start+rows {
			b.htmlScroll = total - rows - m.line
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
	if m.y < b.scroll {
		b.scroll = m.y
	} else if m.y >= b.scroll+rows {
		b.scroll = m.y - rows + 1
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
	runes, start, end, cur := promptWindow(b.findText, b.findCursor, contentW, false)
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
	if b.findText == "" || b.findScope != findScopePage {
		return
	}
	for i, m := range b.findMatches {
		rects := m.rects
		if len(rects) == 0 {
			rects = []findRect{{x: m.x, y: m.y, w: m.w, h: m.h}}
		}
		for _, r := range rects {
			y := r.y - b.scroll + top
			if y < top || y >= bottom || y < 0 || y >= g.H {
				continue
			}
			for dy := 0; dy < maxInt(1, r.h); dy++ {
				if y+dy >= bottom || y+dy >= g.H {
					break
				}
				b.fillFindCells(g, r.x, y+dy, r.w, i == b.findIdx)
			}
		}
	}
}

// paintFindHTML highlights matches in the visible HTML console body.
func (b *Browser) paintFindHTML(g *Grid, start, bodyY, rows int) {
	if b.findText == "" || b.findScope != findScopeHTML {
		return
	}
	for i, m := range b.findMatches {
		rects := m.rects
		if len(rects) == 0 {
			rects = []findRect{{x: m.x, line: m.line, w: m.w}}
		}
		for _, r := range rects {
			rel := r.line - start
			if rel < 0 || rel >= rows {
				continue
			}
			b.fillFindCells(g, 1+r.x, bodyY+rel, r.w, i == b.findIdx)
		}
	}
}
