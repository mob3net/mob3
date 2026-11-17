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

// linkHint is one Vimium-style hint: the target it activates, the letters
// that follow it, and the text cells to highlight. target indexes hintTargets.
type linkHint struct {
	target int
	label  string
	chars  []hintChar
}

// hintTarget is a link or editable field reachable through link hints.
type hintTarget struct {
	link int   // Layout.Links index for a link target
	node *Node // editable control for a field target
}

// openLinkHints starts hint mode and labels every link and editable field that
// intersects the current viewport. Each label is picked from the target's own
// text: the first unused letter for up to 26 targets, then two letters for
// target-heavy pages. With ask set (the F key), choosing a link shows its
// destination and waits for confirmation; fields are focused directly.
func (b *Browser) openLinkHints(ask bool) {
	if b.layout == nil {
		b.status = "No links or fields on this page"
		return
	}
	top, bottom := b.viewport()
	var targets []hintTarget
	for i, li := range b.layout.Links {
		if _, ok := b.visibleLinkRect(li, top, bottom); ok {
			targets = append(targets, hintTarget{link: i})
		}
	}
	if b.page != nil && b.page.Root != nil {
		for _, n := range b.page.Root.FindAll(func(n *Node) bool {
			if !isHintControl(n) || n.Style == nil || n.Style.Display == "none" || n.Style.Hidden {
				return false
			}
			return true
		}) {
			if n.Tag == "select" && n.HasAttr("multiple") {
				for _, opt := range selectOptions(n) {
					if isDisabledControl(opt) || opt.Style == nil || opt.Style.Display == "none" {
						continue
					}
					if _, ok := b.visibleInputRect(opt, top, bottom); ok {
						targets = append(targets, hintTarget{link: -1, node: opt})
					}
				}
				continue
			}
			if _, ok := b.visibleInputRect(n, top, bottom); ok {
				targets = append(targets, hintTarget{link: -1, node: n})
			}
		}
	}
	if len(targets) == 0 {
		b.status = "No links or controls in view"
		return
	}
	ids := make([]int, len(targets))
	for i := range targets {
		ids[i] = i
	}
	b.hintTargets = targets
	b.hintItems = b.hintItems[:0]
	b.hintItems = append(b.hintItems, buildLinkHints(ids, b.collectHintGlyphs(targets, top, bottom))...)
	b.linkHints = true
	b.hintPrefix = ""
	b.hintAsk = ask
	b.hintConfirm = false
	b.hintConfirmLink = -1
	b.status = "Choose link or control (Esc cancels)"
}

// visibleInputRect returns the first visible fragment rectangle of an editable
// control.
func (b *Browser) visibleInputRect(n *Node, top, bottom int) (Rect, bool) {
	if b.layout == nil {
		return Rect{}, false
	}
	viewTop := b.scroll
	viewBottom := b.scroll + (bottom - top)
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Kind != fragText || f.Node != n || f.Fixed {
			continue
		}
		if f.Y >= viewBottom || f.Y+f.H <= viewTop {
			continue
		}
		return Rect{X: f.X, Y: f.Y, W: f.W, H: f.H}, true
	}
	return Rect{}, false
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
	b.status = fmt.Sprintf("Choose hint: %s (Esc cancels)", prefix)
}

// hintTargetRect returns the visible box of a hint target.
func (b *Browser) hintTargetRect(target int, top, bottom int) (Rect, bool) {
	t := b.hintTargetAt(target)
	if t.node != nil {
		return b.visibleInputRect(t.node, top, bottom)
	}
	if b.layout == nil || t.link < 0 || t.link >= len(b.layout.Links) {
		return Rect{}, false
	}
	return b.visibleLinkRect(b.layout.Links[t.link], top, bottom)
}

// collectHintGlyphs returns the visible letters of each requested target in
// document order, with their terminal cells.
func (b *Browser) collectHintGlyphs(targets []hintTarget, top, bottom int) map[int][]hintGlyph {
	linkIDs := make(map[int]int, len(targets))
	inputIDs := make(map[*Node]int, len(targets))
	for id, t := range targets {
		if t.node != nil {
			inputIDs[t.node] = id
		} else {
			linkIDs[t.link] = id
		}
	}
	out := make(map[int][]hintGlyph, len(targets))
	viewTop := b.scroll
	viewBottom := b.scroll + (bottom - top)
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Kind != fragText || f.Text == "" {
			continue
		}
		id := -1
		if f.Link != nil {
			if tid, ok := linkIDs[f.Link.Index]; ok {
				id = tid
			}
		} else if f.Node != nil {
			if tid, ok := inputIDs[f.Node]; ok {
				id = tid
			}
		}
		if id < 0 || f.Y < viewTop || f.Y >= viewBottom {
			continue
		}
		x := f.X
		for _, r := range f.Text {
			if ch, ok := hintLetter(r); ok {
				out[id] = append(out[id], hintGlyph{ch: ch, r: r, x: x, y: f.Y})
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
				target: link,
				label:  string(g.ch),
				chars:  []hintChar{{r: g.r, x: g.x, y: g.y}},
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
		hints = append(hints, linkHint{target: link, label: string(byte('a' + free))})
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
		hints = append(hints, linkHint{target: link, label: label, chars: chars})
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
	b.hintTargets = nil
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

// typeLinkHint narrows the visible hints by one letter and activates the
// target when only one hint still matches.
func (b *Browser) typeLinkHint(letter string) {
	next := b.hintPrefix + letter
	matches := 0
	match := -1
	for _, h := range b.hintItems {
		if strings.HasPrefix(h.label, next) {
			matches++
			match = h.target
		}
	}
	if matches == 0 {
		return
	}
	b.hintPrefix = next
	if matches == 1 {
		b.chooseHint(match)
		return
	}
	b.status = fmt.Sprintf("Choose hint: %s (Esc cancels)", next)
}

// chooseHint follows a chosen link or activates a chosen form control. With F,
// links wait for destination confirmation; controls act immediately.
func (b *Browser) chooseHint(target int) {
	t := b.hintTargetAt(target)
	ask := b.hintAsk
	top, bottom := b.viewport()
	r, _ := b.hintTargetRect(target, top, bottom)
	b.clearLinkHints()
	if t.node != nil {
		b.activateHintControl(t.node, r)
		return
	}
	if t.link < 0 {
		return
	}
	if !ask {
		b.focus = t.link
		b.followFocused()
		return
	}
	b.beginHintConfirm(t.link)
}

// isHintControl reports whether a control is reachable through f hints.
func isHintControl(n *Node) bool {
	if n == nil || n.Type != ElementNode || isDisabledControl(n) {
		return false
	}
	switch n.Tag {
	case "option":
		return false
	case "button":
		return true
	case "textarea":
		return isTextInput(n)
	case "select":
		return true
	case "input":
		if isTextInput(n) || isRangeInput(n) || isFileInput(n) {
			return true
		}
		switch strings.ToLower(strings.TrimSpace(n.Attr("type"))) {
		case "checkbox", "radio", "submit", "button", "reset", "image":
			return true
		}
	}
	return false
}

// activateHintControl applies the same action as clicking a form control and
// leaves that control focused for follow-up keyboard use.
func (b *Browser) activateHintControl(n *Node, r Rect) {
	if n == nil {
		return
	}
	switch {
	case isFileInput(n):
		b.focusControlTarget(n, nil)
		b.showNotice("Upload not supported", "File uploads and multipart forms are not supported.")
	case n.Tag == "option":
		sel := nearestSelect(n)
		if sel == nil {
			return
		}
		b.focusControlTarget(sel, n)
		b.toggleControl(n)
	case isToggleControl(n):
		b.focusControlTarget(n, nil)
		b.toggleControl(n)
	case isTextInput(n), isRangeInput(n):
		b.setInputFocus(n)
		b.ensureInputVisible(n)
	case isButtonControl(n):
		b.focusControlTarget(n, nil)
		x, y := r.X, r.Y
		if r.W <= 0 || r.H <= 0 {
			x, y = r.X+1, r.Y
		}
		b.clickContent(x, y)
		if b.liveNode(n) {
			b.focusControlTarget(n, nil)
		}
	}
}

func (b *Browser) focusControlTarget(n *Node, option *Node) {
	if n == nil {
		return
	}
	b.focus = -1
	b.controlFocus = n
	b.controlOption = option
	if n.Tag == "select" && n.HasAttr("multiple") && option == nil {
		b.controlOption = firstSelectOption(n)
	}
	b.status = tabControlStatus(n)
}

func isButtonControl(n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	if n.Tag == "button" {
		return true
	}
	if n.Tag != "input" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(n.Attr("type"))) {
	case "submit", "button", "reset", "image":
		return true
	}
	return false
}

// hintTargetAt resolves a hint target index. Tests and older hint-only pages
// without hintTargets fall back to treating the index as a link index.
func (b *Browser) hintTargetAt(i int) hintTarget {
	if i >= 0 && i < len(b.hintTargets) {
		return b.hintTargets[i]
	}
	if b.layout != nil && i >= 0 && i < len(b.layout.Links) {
		return hintTarget{link: i}
	}
	return hintTarget{link: -1}
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
// terminal image protocols. Hint mode and any open modal fall back to
// half-block art so text painted above the images cannot be covered by them.
func (b *Browser) inlineImagesEnabled() bool {
	if b.gfx == nil || b.linkHints {
		return false
	}
	return b.dialog == nil && b.download == nil && b.notice == nil && b.selectMenu == nil
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
			if !b.color {
				// Without colors, reverse video marks the hint letter.
				g.Cells[y*g.W+c.x].Reverse = true
			}
		}
	}
}

// paintLinkHintFallback draws free label letters inside a target box that has
// no usable text, such as an image link or an empty field.
func (b *Browser) paintLinkHintFallback(g *Grid, h linkHint, top, bottom int) {
	r, ok := b.hintTargetRect(h.target, top, bottom)
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
		if !b.color {
			g.Cells[y*g.W+x+i].Reverse = true
		}
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
