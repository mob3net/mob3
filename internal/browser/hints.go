package browser

import (
	"fmt"
	"strings"
)

// hintGlyph is one letter of a link's visible text, with the terminal cell it
// occupies. Hints are chosen from these letters so the page text itself never
// changes, only the styling of the chosen characters.
type hintGlyph struct {
	ch   byte // lowercase ASCII letter used for matching
	r    rune // original glyph, so painting preserves the page text
	x, y int  // document cell coordinates
}

// hintChar records the cell of one highlighted hint letter.
type hintChar struct {
	r    rune
	x, y int
}

// linkHint is one Vimium-style link hint: the index into Layout.Links, the
// letters that follow it, and the text cells to highlight.
type linkHint struct {
	link  int
	label string
	chars []hintChar
}

// openLinkHints starts link-hint mode and labels every link that intersects
// the current viewport. Each label is picked from the link's own text: the
// first unused letter for up to 26 links, then two letters for link-heavy
// pages. With ask set (the F key), choosing a hint shows its destination and
// waits for confirmation instead of following immediately.
func (b *Browser) openLinkHints(ask bool) {
	if b.layout == nil || len(b.layout.Links) == 0 {
		b.status = "No links on this page"
		return
	}
	top, bottom := b.viewport()
	visible := make([]int, 0, len(b.layout.Links))
	for i, li := range b.layout.Links {
		if _, ok := b.visibleLinkRect(li, top, bottom); ok {
			visible = append(visible, i)
		}
	}
	if len(visible) == 0 {
		b.status = "No links in view"
		return
	}
	b.hintItems = b.hintItems[:0]
	b.hintItems = append(b.hintItems, buildLinkHints(visible, b.collectHintGlyphs(visible, top, bottom))...)
	b.linkHints = true
	b.hintPrefix = ""
	b.hintAsk = ask
	b.hintConfirm = false
	b.hintConfirmLink = -1
	b.status = "Follow link (Esc cancels)"
}

// reopenLinkHints restores an active hint session after a layout rebuild. The
// typed prefix is kept only when at least two hints still match, which is the
// only state the interactive key handler leaves active.
func (b *Browser) reopenLinkHints(ask bool, prefix string) {
	b.openLinkHints(ask)
	if !b.linkHints || prefix == "" {
		return
	}
	matches := 0
	for _, h := range b.hintItems {
		if strings.HasPrefix(h.label, prefix) {
			matches++
		}
	}
	if matches < 2 {
		return
	}
	b.hintPrefix = prefix
	b.status = fmt.Sprintf("Follow link: %s (Esc cancels)", prefix)
}

// collectHintGlyphs returns the visible letters of each requested link in
// document order, with their terminal cells.
func (b *Browser) collectHintGlyphs(links []int, top, bottom int) map[int][]hintGlyph {
	wanted := make(map[int]bool, len(links))
	for _, link := range links {
		wanted[link] = true
	}
	out := make(map[int][]hintGlyph, len(links))
	viewTop := b.scroll
	viewBottom := b.scroll + (bottom - top)
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Kind != fragText || f.Text == "" || f.Link == nil {
			continue
		}
		link := f.Link.Index
		if !wanted[link] || f.Y < viewTop || f.Y >= viewBottom {
			continue
		}
		x := f.X
		for _, r := range f.Text {
			if ch, ok := hintLetter(r); ok {
				out[link] = append(out[link], hintGlyph{ch: ch, r: r, x: x, y: f.Y})
			}
			x += runeWidth(r)
		}
	}
	return out
}

// hintLetter lowercases an ASCII letter and reports whether r is one.
func hintLetter(r rune) (byte, bool) {
	if r >= 'a' && r <= 'z' {
		return byte(r), true
	}
	if r >= 'A' && r <= 'Z' {
		return byte(r) + 'a' - 'A', true
	}
	return 0, false
}

// buildLinkHints assigns a unique hint to each link. Up to 26 links get a
// single unused letter from their text. Larger pages start from two adjacent
// letters but shorten any hint whose first letter is unique back to one
// letter. If a link has no text letters at all, a generated label is used and
// drawn inside its box.
func buildLinkHints(links []int, glyphs map[int][]hintGlyph) []linkHint {
	var hints []linkHint
	if len(links) <= 26 {
		if single, ok := assignSingleHints(links, glyphs); ok {
			hints = single
		}
	}
	if hints == nil {
		hints = assignPairHints(links, glyphs)
	}
	shortenUniquePairs(hints)
	return hints
}

// shortenUniquePairs turns a two-letter hint into a one-letter hint when no
// other label starts with the same letter. The second letter is only needed to
// distinguish links that share a prefix, so keeping it otherwise makes simple
// links look more complicated than they are.
func shortenUniquePairs(hints []linkHint) {
	counts := make(map[byte]int, len(hints))
	for _, h := range hints {
		if h.label != "" {
			counts[h.label[0]]++
		}
	}
	for i := range hints {
		h := &hints[i]
		if len(h.label) != 2 || counts[h.label[0]] != 1 {
			continue
		}
		h.label = h.label[:1]
		if len(h.chars) > 0 {
			h.chars = h.chars[:1]
		}
	}
}

func assignSingleHints(links []int, glyphs map[int][]hintGlyph) ([]linkHint, bool) {
	used := [26]bool{}
	hints := make([]linkHint, 0, len(links))
	for _, link := range links {
		chosen := false
		for _, g := range glyphs[link] {
			idx := int(g.ch - 'a')
			if used[idx] {
				continue
			}
			used[idx] = true
			hints = append(hints, linkHint{
				link:  link,
				label: string(g.ch),
				chars: []hintChar{{r: g.r, x: g.x, y: g.y}},
			})
			chosen = true
			break
		}
		if chosen {
			continue
		}
		if len(glyphs[link]) > 0 {
			// This link has text but no unused letter left. Let the caller
			// switch the whole page to two-letter hints.
			return nil, false
		}
		// A link with no text letters (for example an image) gets a free
		// letter from the remaining alphabet, drawn inside its box.
		free := -1
		for i := range used {
			if !used[i] {
				free = i
				break
			}
		}
		if free < 0 {
			return nil, false
		}
		used[free] = true
		hints = append(hints, linkHint{link: link, label: string(byte('a' + free))})
	}
	return hints, true
}

func assignPairHints(links []int, glyphs map[int][]hintGlyph) []linkHint {
	used := make(map[string]bool, len(links))
	hints := make([]linkHint, 0, len(links))
	fallback := 0
	for _, link := range links {
		gs := glyphs[link]
		label, chars := pickPair(gs, used)
		if label == "" {
			for {
				label = encodeLinkHint(fallback, 2)
				fallback++
				if !used[label] {
					break
				}
			}
			chars = nil
		}
		used[label] = true
		hints = append(hints, linkHint{link: link, label: label, chars: chars})
	}
	return hints
}

// pickPair finds the first unique two-letter label made of adjacent letters
// in the link text. Only neighbouring cells are considered, so the two
// highlighted letters never have other characters or blanks between them.
func pickPair(gs []hintGlyph, used map[string]bool) (string, []hintChar) {
	for a := 0; a+1 < len(gs); a++ {
		if gs[a].y != gs[a+1].y || gs[a+1].x != gs[a].x+1 {
			continue
		}
		label := string(gs[a].ch) + string(gs[a+1].ch)
		if used[label] {
			continue
		}
		return label, []hintChar{
			{r: gs[a].r, x: gs[a].x, y: gs[a].y},
			{r: gs[a+1].r, x: gs[a+1].x, y: gs[a+1].y},
		}
	}
	return "", nil
}

// clearLinkHints leaves hint mode without navigating. It is safe to call when
// hints are not active.
func (b *Browser) clearLinkHints() {
	if !b.linkHints {
		return
	}
	b.linkHints = false
	b.hintItems = nil
	b.hintPrefix = ""
	b.hintAsk = false
	b.status = ""
}

// handleLinkHintKey consumes keys while hints are shown. Letters extend or
// follow a hint; Escape cancels. Any other key dismisses hint mode and is then
// handled normally, so scrolling always makes the hints disappear.
func (b *Browser) handleLinkHintKey(k string) bool {
	if k == "esc" || k == "ctrl-c" {
		b.clearLinkHints()
		b.status = exitHint
		return false
	}
	if len(k) == 1 {
		c := k[0]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c >= 'a' && c <= 'z' {
			b.typeLinkHint(string(c))
			return false
		}
	}
	b.clearLinkHints()
	if k == "f" {
		return false
	}
	return b.HandleKey(k)
}

// typeLinkHint narrows the visible hints by one letter and follows the link
// when only one hint still matches.
func (b *Browser) typeLinkHint(letter string) {
	next := b.hintPrefix + letter
	matches := 0
	match := -1
	for _, h := range b.hintItems {
		if strings.HasPrefix(h.label, next) {
			matches++
			match = h.link
		}
	}
	if matches == 0 {
		return
	}
	b.hintPrefix = next
	if matches == 1 {
		b.chooseLinkHint(match)
		return
	}
	b.status = fmt.Sprintf("Follow link: %s (Esc cancels)", next)
}

// chooseLinkHint follows a chosen hint immediately, or with F shows the
// resolved destination in the footer and waits for Enter/y confirmation.
func (b *Browser) chooseLinkHint(link int) {
	ask := b.hintAsk
	b.clearLinkHints()
	if !ask {
		b.focus = link
		b.followFocused()
		return
	}
	b.beginHintConfirm(link)
}

// beginHintConfirm shows a chosen F link and waits for confirmation.
func (b *Browser) beginHintConfirm(link int) {
	b.hintConfirm = true
	b.hintConfirmLink = link
	target := ""
	if b.layout != nil && link >= 0 && link < len(b.layout.Links) {
		target = b.resolveHref(b.layout.Links[link].Href)
	}
	b.status = target + "  (Enter/y follows · Esc cancels)"
}

// findHintLink locates an equivalent link after a layout rebuild, preferring
// the same DOM node and falling back to the same href.
func (b *Browser) findHintLink(node *Node, href string) int {
	if b.layout == nil {
		return -1
	}
	if node != nil {
		for i, li := range b.layout.Links {
			if li.Node == node {
				return i
			}
		}
	}
	for i, li := range b.layout.Links {
		if li.Href == href {
			return i
		}
	}
	return -1
}

// clearHintConfirm cancels a pending F confirmation without navigating.
func (b *Browser) clearHintConfirm() {
	if !b.hintConfirm {
		return
	}
	b.hintConfirm = false
	b.hintConfirmLink = -1
	b.status = ""
}

// handleHintConfirmKey accepts Enter/y to follow the chosen link. Escape, n or
// any other key cancels the confirmation; the key is then processed normally.
func (b *Browser) handleHintConfirmKey(k string) bool {
	link := b.hintConfirmLink
	switch k {
	case "enter", "y", "Y":
		b.clearHintConfirm()
		if link >= 0 {
			b.focus = link
			b.followFocused()
		}
		return false
	case "esc", "n", "N", "ctrl-c":
		b.clearHintConfirm()
		b.status = exitHint
		return false
	}
	b.clearHintConfirm()
	if k == "F" {
		return false
	}
	return b.HandleKey(k)
}

// visibleLinkRect returns the first rectangle of li that intersects the
// viewport rows [top, bottom).
func (b *Browser) visibleLinkRect(li *LinkInfo, top, bottom int) (Rect, bool) {
	if li == nil {
		return Rect{}, false
	}
	viewTop := b.scroll
	viewBottom := b.scroll + (bottom - top)
	for _, r := range li.Rects {
		if r.Y >= viewBottom || r.Y+r.H <= viewTop {
			continue
		}
		return r, true
	}
	return Rect{}, false
}

// inlineImagesEnabled reports whether the current frame should use native
// terminal image protocols. Hint mode falls back to half-block art so hints
// painted into the text grid stay above the images.
func (b *Browser) inlineImagesEnabled() bool {
	return b.gfx != nil && !b.linkHints
}

// paintLinkHints draws the chosen letters of each matching link with a yellow
// background and underline. The page text itself is never replaced unless a
// link has no text letters at all.
func (b *Browser) paintLinkHints(g *Grid, top, bottom int) {
	if !b.linkHints || b.layout == nil {
		return
	}
	bg := RGB(0xff, 0xd7, 0x00)
	fg := RGB(0x1f, 0x23, 0x28)
	st := &Style{Bold: true, Underline: true, FontSize: 16, LineHeight: 1}
	for _, h := range b.hintItems {
		if !strings.HasPrefix(h.label, b.hintPrefix) {
			continue
		}
		if len(h.chars) == 0 {
			b.paintLinkHintFallback(g, h, top, bottom)
			continue
		}
		// The already typed prefix has done its job; repaint only the letters
		// that still need to be chosen.
		chars := h.chars
		if len(b.hintPrefix) < len(chars) {
			chars = chars[len(b.hintPrefix):]
		} else {
			chars = nil
		}
		for _, c := range chars {
			y := c.y - b.scroll + top
			if y < top || y >= bottom || y < 0 || y >= g.H {
				continue
			}
			if c.x < 0 || c.x >= g.W {
				continue
			}
			g.Put(c.x, y, c.r, st, fg, bg)
		}
	}
}

// paintLinkHintFallback draws free label letters inside a link box that has no
// usable text, such as an image link.
func (b *Browser) paintLinkHintFallback(g *Grid, h linkHint, top, bottom int) {
	if h.link < 0 || h.link >= len(b.layout.Links) {
		return
	}
	r, ok := b.visibleLinkRect(b.layout.Links[h.link], top, bottom)
	if !ok {
		return
	}
	label := h.label
	if len(b.hintPrefix) < len(label) {
		label = label[len(b.hintPrefix):]
	} else {
		return
	}
	y := r.Y - b.scroll + top
	if y < top || y >= bottom || y < 0 || y >= g.H {
		return
	}
	x := r.X
	if x+len(label) > g.W {
		x = g.W - len(label)
	}
	if x < 0 || x >= g.W {
		return
	}
	st := &Style{Bold: true, Underline: true, FontSize: 16, LineHeight: 1}
	bg := RGB(0xff, 0xd7, 0x00)
	fg := RGB(0x1f, 0x23, 0x28)
	for i, ch := range label {
		g.Put(x+i, y, ch, st, fg, bg)
	}
}

// encodeLinkHint renders i in base 26 using letters, padded to length. Index 0
// is "a" (or "aa" for two-letter hints).
func encodeLinkHint(i, length int) string {
	out := make([]byte, length)
	for p := length - 1; p >= 0; p-- {
		out[p] = byte('a' + i%26)
		i /= 26
	}
	return string(out)
}
