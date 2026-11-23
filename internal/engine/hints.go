package engine

import "strings"

// HintGlyph is one letter of a target's visible text, with the terminal cell
// it occupies.
type HintGlyph struct {
	Ch   byte
	R    rune
	X, Y int
}

// HintChar records the cell of one highlighted hint letter.
type HintChar struct {
	R    rune
	X, Y int
}

// LinkHint is one Vimium-style hint: the target it activates, the letters that
// follow it, and the text cells to highlight. Target indexes the hint target
// list.
type LinkHint struct {
	Target int
	Label  string
	Chars  []HintChar
}

// HintLetter lowercases an ASCII letter and reports whether r is one.
func HintLetter(r rune) (byte, bool) {
	if r >= 'a' && r <= 'z' {
		return byte(r), true
	}
	if r >= 'A' && r <= 'Z' {
		return byte(r) + 'a' - 'A', true
	}
	return 0, false
}

// AssignLinkHints assigns a unique hint to each link. Up to 26 links get a
// single unused letter from their text. Larger pages start from two adjacent
// letters but shorten any hint whose first letter is unique back to one
// letter. If a link has no text letters at all, a generated label is used and
// drawn inside its box.
func AssignLinkHints(links []int, glyphs map[int][]HintGlyph) []LinkHint {
	var hints []LinkHint
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
func shortenUniquePairs(hints []LinkHint) {
	counts := make(map[byte]int, len(hints))
	for _, h := range hints {
		if h.Label != "" {
			counts[h.Label[0]]++
		}
	}
	for i := range hints {
		h := &hints[i]
		if len(h.Label) != 2 || counts[h.Label[0]] != 1 {
			continue
		}
		h.Label = h.Label[:1]
		if len(h.Chars) > 0 {
			h.Chars = h.Chars[:1]
		}
	}
}

func assignSingleHints(links []int, glyphs map[int][]HintGlyph) ([]LinkHint, bool) {
	used := [26]bool{}
	hints := make([]LinkHint, 0, len(links))
	for _, link := range links {
		chosen := false
		for _, g := range glyphs[link] {
			idx := int(g.Ch - 'a')
			if used[idx] {
				continue
			}
			used[idx] = true
			hints = append(hints, LinkHint{
				Target: link,
				Label:  string(g.Ch),
				Chars:  []HintChar{{R: g.R, X: g.X, Y: g.Y}},
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
		hints = append(hints, LinkHint{Target: link, Label: string(byte('a' + free))})
	}
	return hints, true
}

func assignPairHints(links []int, glyphs map[int][]HintGlyph) []LinkHint {
	used := make(map[string]bool, len(links))
	hints := make([]LinkHint, 0, len(links))
	fallback := 0
	for _, link := range links {
		gs := glyphs[link]
		label, chars := pickPair(gs, used)
		if label == "" {
			for {
				label = EncodeLinkHint(fallback, 2)
				fallback++
				if !used[label] {
					break
				}
			}
			chars = nil
		}
		used[label] = true
		hints = append(hints, LinkHint{Target: link, Label: label, Chars: chars})
	}
	return hints
}

// pickPair finds the first unique two-letter label made of adjacent letters
// in the link text. Only neighbouring cells are considered, so the two
// highlighted letters never have other characters or blanks between them.
func pickPair(gs []HintGlyph, used map[string]bool) (string, []HintChar) {
	for a := 0; a+1 < len(gs); a++ {
		if gs[a].Y != gs[a+1].Y || gs[a+1].X != gs[a].X+1 {
			continue
		}
		label := string(gs[a].Ch) + string(gs[a+1].Ch)
		if used[label] {
			continue
		}
		return label, []HintChar{
			{R: gs[a].R, X: gs[a].X, Y: gs[a].Y},
			{R: gs[a+1].R, X: gs[a+1].X, Y: gs[a+1].Y},
		}
	}
	return "", nil
}

// EncodeLinkHint renders i in base 26 using letters, padded to length. Index 0
// is "a" (or "aa" for two-letter hints).
func EncodeLinkHint(i, length int) string {
	out := make([]byte, length)
	for p := length - 1; p >= 0; p-- {
		out[p] = byte('a' + i%26)
		i /= 26
	}
	return string(out)
}

// HintTarget is a link or editable field reachable through link hints.
type HintTarget struct {
	Link int   // Layout.Links index for a link target
	Node *Node // editable control for a field target
}

// VisibleLinkRect returns the first visible rectangle of a link.
func VisibleLinkRect(li *LinkInfo, scroll, top, bottom int) (Rect, bool) {
	if li == nil {
		return Rect{}, false
	}
	viewTop := scroll
	viewBottom := scroll + (bottom - top)
	for _, r := range li.Rects {
		if r.Y >= viewBottom || r.Y+r.H <= viewTop {
			continue
		}
		return r, true
	}
	return Rect{}, false
}

// VisibleInputRect returns the visible text rectangle of a form control.
func VisibleInputRect(layout *Layout, n *Node, scroll, top, bottom int) (Rect, bool) {
	if layout == nil {
		return Rect{}, false
	}
	viewTop := scroll
	viewBottom := scroll + (bottom - top)
	for i := range layout.Frags {
		f := &layout.Frags[i]
		if f.Kind != FragText || f.Node != n {
			continue
		}
		if f.Y >= viewBottom || f.Y+f.H <= viewTop {
			continue
		}
		return Rect{X: f.X, Y: f.Y, W: f.W, H: f.H}, true
	}
	return Rect{}, false
}

// IsHintControl reports whether n is a link-hint form target.
func IsHintControl(n *Node) bool {
	if n == nil || n.Type != ElementNode || IsDisabledControl(n) {
		return false
	}
	switch n.Tag {
	case "option":
		return false
	case "button":
		return true
	case "textarea":
		return IsTextInput(n)
	case "select":
		return true
	case "input":
		if IsTextInput(n) || IsRangeInput(n) || IsFileInput(n) {
			return true
		}
		switch strings.ToLower(strings.TrimSpace(n.Attr("type"))) {
		case "checkbox", "radio", "submit", "button", "reset", "image":
			return true
		}
	}
	return false
}

// BuildHintTargets collects visible links and editable fields in document
// order. Fixed bars must already be re-anchored for the current scroll.
func BuildHintTargets(layout *Layout, root *Node, scroll, top, bottom int) []HintTarget {
	if layout == nil {
		return nil
	}
	var targets []HintTarget
	for i, li := range layout.Links {
		if _, ok := VisibleLinkRect(li, scroll, top, bottom); ok {
			targets = append(targets, HintTarget{Link: i})
		}
	}
	if root == nil {
		return targets
	}
	for _, n := range root.FindAll(func(n *Node) bool {
		if !IsHintControl(n) || n.Style == nil || n.Style.Display == "none" || n.Style.Hidden {
			return false
		}
		return true
	}) {
		if n.Tag == "select" && n.HasAttr("multiple") {
			for _, opt := range SelectOptions(n) {
				if IsDisabledControl(opt) || opt.Style == nil || opt.Style.Display == "none" {
					continue
				}
				if _, ok := VisibleInputRect(layout, opt, scroll, top, bottom); ok {
					targets = append(targets, HintTarget{Link: -1, Node: opt})
				}
			}
			continue
		}
		if _, ok := VisibleInputRect(layout, n, scroll, top, bottom); ok {
			targets = append(targets, HintTarget{Link: -1, Node: n})
		}
	}
	return targets
}

// HintTargetRect returns the visible box of one target.
func HintTargetRect(layout *Layout, targets []HintTarget, target, scroll, top, bottom int) (Rect, bool) {
	if target < 0 {
		return Rect{}, false
	}
	if target < len(targets) {
		t := targets[target]
		if t.Node != nil {
			return VisibleInputRect(layout, t.Node, scroll, top, bottom)
		}
		if layout != nil && t.Link >= 0 && t.Link < len(layout.Links) {
			return VisibleLinkRect(layout.Links[t.Link], scroll, top, bottom)
		}
		return Rect{}, false
	}
	// Tests and older hint-only pages may paint a label without a target list.
	if layout != nil && target < len(layout.Links) {
		return VisibleLinkRect(layout.Links[target], scroll, top, bottom)
	}
	return Rect{}, false
}

// CollectHintGlyphs returns the visible letters of each target in document
// order, with their terminal cells.
func CollectHintGlyphs(layout *Layout, targets []HintTarget, scroll, top, bottom int) map[int][]HintGlyph {
	if layout == nil {
		return nil
	}
	linkIDs := make(map[int]int, len(targets))
	inputIDs := make(map[*Node]int, len(targets))
	for id, t := range targets {
		if t.Node != nil {
			inputIDs[t.Node] = id
		} else {
			linkIDs[t.Link] = id
		}
	}
	out := make(map[int][]HintGlyph, len(targets))
	viewTop := scroll
	viewBottom := scroll + (bottom - top)
	for i := range layout.Frags {
		f := &layout.Frags[i]
		if f.Kind != FragText || f.Text == "" {
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
			if ch, ok := HintLetter(r); ok {
				out[id] = append(out[id], HintGlyph{Ch: ch, R: r, X: x, Y: f.Y})
			}
			x += RuneWidth(r)
		}
	}
	return out
}

// PaintHintLabels paints the remaining letters of each matching hint.
func PaintHintLabels(g *Grid, layout *Layout, targets []HintTarget, hints []LinkHint, prefix string, scroll, top, bottom int, color bool) {
	if g == nil || layout == nil {
		return
	}
	bg := RGB(0xff, 0xd7, 0x00)
	fg := RGB(0x1f, 0x23, 0x28)
	st := &Style{Bold: true, Underline: true, FontSize: 16, LineHeight: 1}
	for _, h := range hints {
		if !strings.HasPrefix(h.Label, prefix) {
			continue
		}
		if len(h.Chars) == 0 {
			paintHintFallback(g, layout, targets, h, prefix, scroll, top, bottom, color, st, fg, bg)
			continue
		}
		chars := h.Chars
		if len(prefix) < len(chars) {
			chars = chars[len(prefix):]
		} else {
			chars = nil
		}
		for _, c := range chars {
			y := c.Y - scroll + top
			if y < top || y >= bottom || y < 0 || y >= g.H {
				continue
			}
			if c.X < 0 || c.X >= g.W {
				continue
			}
			g.Put(c.X, y, c.R, st, fg, bg)
			if !color {
				g.Cells[y*g.W+c.X].Reverse = true
			}
		}
	}
}

func paintHintFallback(g *Grid, layout *Layout, targets []HintTarget, h LinkHint, prefix string, scroll, top, bottom int, color bool, st *Style, fg, bg Color) {
	r, ok := HintTargetRect(layout, targets, h.Target, scroll, top, bottom)
	if !ok {
		return
	}
	label := h.Label
	if len(prefix) < len(label) {
		label = label[len(prefix):]
	} else {
		return
	}
	y := r.Y - scroll + top
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
	for i, ch := range label {
		g.Put(x+i, y, ch, st, fg, bg)
		if !color {
			g.Cells[y*g.W+x+i].Reverse = true
		}
	}
}
