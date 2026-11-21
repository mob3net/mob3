package browser

import (
	"sort"
	"strconv"
	"strings"
)

func listMarker(st *Style, n *Node) string {
	switch st.ListStyle {
	case "none":
		return ""
	case "circle":
		return "◦"
	case "square":
		return "▪"
	case "decimal", "decimal-leading-zero":
		return strconv.Itoa(listIndex(n)) + "."
	case "lower-alpha", "lower-latin":
		return string(rune('a'+(listIndex(n)-1)%26)) + "."
	case "upper-alpha", "upper-latin":
		return string(rune('A'+(listIndex(n)-1)%26)) + "."
	case "lower-roman":
		return roman(listIndex(n)) + "."
	case "upper-roman":
		return strings.ToUpper(roman(listIndex(n))) + "."
	default:
		return "•"
	}
}

func listIndex(n *Node) int {
	idx := 0
	if n == nil || n.Parent == nil {
		return 1
	}
	for _, c := range n.Parent.Children {
		if c.Type != ElementNode || c.Tag != "li" {
			continue
		}
		idx++
		if c == n {
			return idx
		}
	}
	return 1
}

func roman(n int) string {
	if n <= 0 || n > 3999 {
		return strconv.Itoa(n)
	}
	vals := []struct {
		v int
		s string
	}{{1000, "m"}, {900, "cm"}, {500, "d"}, {400, "cd"}, {100, "c"}, {90, "xc"},
		{50, "l"}, {40, "xl"}, {10, "x"}, {9, "ix"}, {5, "v"}, {4, "iv"}, {1, "i"}}
	var b strings.Builder
	for _, p := range vals {
		for n >= p.v {
			b.WriteString(p.s)
			n -= p.v
		}
	}
	return b.String()
}

// flow lays out a block's children: consecutive inline content is grouped into
// anonymous inline blocks, block-level children stack vertically.
// layoutChildren expands display:contents wrappers, whose children take part
// in the parent's layout as if the wrapper did not generate a box.
func layoutChildren(parent *Node) []*Node {
	var out []*Node
	var add func(*Node)
	add = func(n *Node) {
		if n.Type == ElementNode && n.Style != nil && n.Style.Display == "contents" {
			for _, c := range n.Children {
				add(c)
			}
			return
		}
		out = append(out, n)
	}
	for _, c := range parent.Children {
		add(c)
	}
	return out
}

// layoutChildrenWithPseudo returns the in-flow children plus generated
// ::before/::after boxes. A flex or grid container makes those boxes into
// items, and the common aspect-ratio pattern puts an empty ::after with a
// percentage padding-bottom in a flex media box to give the absolutely
// positioned image a height; dropping it collapses the box to zero and lets
// the image paint over the following text.
func layoutChildrenWithPseudo(parent *Node) []*Node {
	kids := layoutChildren(parent)
	before := pseudoElement(parent, parent.Before)
	after := pseudoElement(parent, parent.After)
	if before == nil && after == nil {
		return kids
	}
	out := make([]*Node, 0, len(kids)+2)
	if before != nil {
		out = append(out, before)
	}
	out = append(out, kids...)
	if after != nil {
		out = append(out, after)
	}
	return out
}

func (l *layouter) flow(parent *Node, x, y, width int) int {
	if l.outOfTime() {
		return 0
	}
	startFloats := len(l.floats)
	cur := y
	var inline []*Node
	flush := func() {
		if len(inline) > 0 {
			cur += l.inlineFlow(inline, x, cur, width, parent.Style)
			inline = nil
		}
	}
	if p := pseudoElement(parent, parent.Before); p != nil && l.isBlock(p) {
		cur += l.block(p, x, cur, width)
	}
	for _, c := range layoutChildren(parent) {
		if l.truncated || l.overRowLimit(cur) {
			l.truncated = true
			break
		}
		if c.Type == ElementNode {
			if c.Style == nil || c.Style.Display == "none" {
				continue
			}
			if isOutOfFlow(c.Style) {
				l.deferAbsolute(c, x, cur)
				continue
			}
			if c.Style.Float != "" {
				flush()
				cur = l.placeFloat(c, x, cur, width)
				continue
			}
			if c.Style.Clear != "" {
				flush()
				cur = l.clearY(c.Style.Clear, cur)
			}
			if l.isBlock(c) || l.inlineWrapperIsBlock(c) {
				flush()
				if c.Style.Display == "inline-block" && len(l.floats) > startFloats {
					// A blockified inline-block is still an atomic inline box,
					// so it sits in the line's band beside an active float
					// instead of starting at the container's left edge (where
					// it would overlap the float).
					bx, bw := l.floatBand(x, cur, width)
					if need := l.declaredFloatWidth(c, width); need > 0 && need > bw+1 {
						// It cannot fit beside the float: start below it, the
						// way a line box that does not fit wraps.
						bx = x
						cur = l.clearY("both", cur)
					}
					cur += l.block(c, bx, cur, width)
					continue
				}
				cur += l.block(c, x, cur, width)
				continue
			}
		}
		inline = append(inline, c)
	}
	flush()
	if p := pseudoElement(parent, parent.After); p != nil && l.isBlock(p) {
		cur += l.block(p, x, cur, width)
	}
	// The flow contains its floats: their height extends it and they stop
	// affecting content after the flow. This keeps unrelated sections from
	// inheriting each other's float bands.
	for _, f := range l.floats[startFloats:] {
		if f.bottom > cur {
			cur = f.bottom
		}
	}
	l.floats = l.floats[:startFloats]
	if cur < y {
		return 0
	}
	return cur - y
}

// floatBand returns the horizontal range available for a line at y, after
// shortening it around any active floated boxes.
func (l *layouter) floatBand(x, y, width int) (int, int) {
	left, right := x, x+width
	for _, f := range l.floats {
		if y >= f.bottom || y+1 <= f.top {
			continue
		}
		if f.side == "right" {
			if f.left < right {
				right = f.left
			}
		} else if f.right > left {
			left = f.right
		}
	}
	if left > right {
		left = right
	}
	return left, right
}

// clearY moves y below the floats selected by a CSS clear value.
func (l *layouter) clearY(clear string, y int) int {
	for _, f := range l.floats {
		if f.bottom <= y {
			continue
		}
		switch clear {
		case "both":
			y = f.bottom
		case "left", "inline-start":
			if f.side == "left" {
				y = f.bottom
			}
		case "right", "inline-end":
			if f.side == "right" {
				y = f.bottom
			}
		}
	}
	return y
}

// placeFloat lays out a floated child at the left or right edge of the current
// line band. It stays out of normal flow; only its height counts.
func (l *layouter) placeFloat(n *Node, x, y, width int) int {
	st := n.Style
	if st == nil {
		return y
	}
	if st.Clear != "" {
		y = l.clearY(st.Clear, y)
	}
	// A float with a declared width drops to the next band when it does not fit
	// at the current row, the way CSS places floats as high as possible. A
	// shrink-to-fit float keeps taking whatever band space is left.
	if need := l.declaredFloatWidth(n, width); need > 0 {
		for attempt := 0; attempt < 8; attempt++ {
			left, right := l.floatBand(x, y, width)
			// Percentage column pairs (75% beside 25%) are exact in a
			// browser but round up to one extra cell here. A one-cell
			// shortfall is rounding, not a reason to drop the float below
			// its row; the width is clamped to the band below.
			if right-left+1 >= need {
				break
			}
			next := l.floatDropY(x, y, width, need)
			if next <= y {
				break
			}
			y = next
		}
	}
	left, right := l.floatBand(x, y, width)
	avail := right - left
	if avail < 1 {
		avail = 1
	}
	fw := l.shrinkFitWidth(n, width, avail)
	fx := left
	side := "left"
	if st.Float == "right" || st.Float == "inline-end" {
		fx = right - fw
		if fx < left {
			fx = left
		}
		side = "right"
	}
	fragStart := len(l.frags)
	absStart := len(l.abs)
	h, rect := l.blockLayout(n, fx, y, fw, 0, true)
	boxLeft, boxRight := rect.X, rect.X+rect.W
	if side == "right" && boxRight < right && rect.W > 0 {
		// A right float with a narrower declared width is aligned to the
		// right edge of the band rather than the left edge it was measured at.
		dx := right - boxRight
		for i := fragStart; i < len(l.frags); i++ {
			l.frags[i].X += dx
		}
		for i := absStart; i < len(l.abs); i++ {
			l.abs[i].staticX += dx
		}
		boxLeft += dx
		boxRight += dx
	}
	if boxRight <= boxLeft || h <= 0 {
		return y
	}
	l.floats = append(l.floats, floatBox{
		side: side, top: y, bottom: y + h, left: boxLeft, right: boxRight,
	})
	return y
}

// shrinkFitWidth returns the shrink-to-fit border-box width of a floated box
// within avail cells.
func (l *layouter) shrinkFitWidth(n *Node, cbW, avail int) int {
	maxW, minW := l.measure(n)
	ml, mr, _, _, bl, br, _, _, pl, pr, _, _ := l.boxMetrics(n.Style, cbW)
	extra := ml + mr + bl + br + pl + pr
	st := n.Style
	if st != nil && st.Width.Set && !st.Width.IsAuto() {
		// A specified width replaces the max-content shrink-to-fit width; the
		// content wraps inside it. Min-content still wins when it is larger.
		if w := l.widthValue(st, st.Width, cbW); w > 0 {
			maxW = w
		}
	}
	maxW += extra
	minW += extra
	if maxW < 1 {
		maxW = 1
	}
	if minW < 1 {
		minW = 1
	}
	w := maxW
	if w > avail {
		w = avail
	}
	if w < minW {
		w = minW
	}
	if w > maxW {
		// The preferred width caps the result even when nested content has a
		// larger min-content width, so a specified float width is not widened.
		w = maxW
	}
	if st := n.Style; st != nil {
		if st.MaxWidth.Set && !st.MaxWidth.IsAuto() {
			if m := l.widthValue(st, st.MaxWidth, cbW) + extra; m < w {
				w = m
			}
		}
		if st.MinWidth.Set && !st.MinWidth.IsAuto() {
			if m := l.widthValue(st, st.MinWidth, cbW) + extra; m > w {
				w = m
			}
		}
	}
	if w < 1 {
		w = 1
	}
	return w
}

func noContentFrags(frags []Frag) bool {
	for _, f := range frags {
		if f.Kind == fragText || f.Kind == fragImage {
			return false
		}
	}
	return true
}

// trimFrags removes fragments from start onward that begin at or below limit.
func (l *layouter) trimFrags(limit, start int) {
	if start < 0 || start > len(l.frags) {
		return
	}
	out := l.frags[:start]
	for _, f := range l.frags[start:] {
		if f.Y >= limit {
			continue
		}
		out = append(out, f)
	}
	l.frags = out
}

// clipFragments drops or trims fragments from start onward to the clip rect.
// Text and images that only partially fit vertically are dropped; fills and
// link boxes are trimmed so panels do not paint over their own boundaries.
func (l *layouter) clipFragments(start int, r Rect) {
	if start < 0 || start > len(l.frags) || r.W <= 0 || r.H <= 0 {
		return
	}
	right, bottom := r.X+r.W, r.Y+r.H
	out := l.frags[:start]
	for _, f := range l.frags[start:] {
		if f.Fixed {
			out = append(out, f)
			continue
		}
		if f.Y+f.H <= r.Y || f.Y >= bottom || f.X+f.W <= r.X || f.X >= right {
			continue
		}
		if f.Kind == fragText || f.Kind == fragImage {
			// Glyphs and bitmaps cannot be cut cleanly; keep only the parts
			// that fit entirely inside the clip box.
			if f.Y < r.Y || f.Y+f.H > bottom {
				continue
			}
			if f.X < r.X {
				if f.Kind == fragImage {
					continue
				}
				cut := r.X - f.X
				f.Text = trimTextLeft(f.Text, cut)
				f.X = r.X
				f.W -= cut
			}
			if f.X+f.W > right {
				if f.Kind == fragImage {
					continue
				}
				f.W = right - f.X
				f.Text = truncateWidth(f.Text, f.W)
			}
		} else {
			if f.Y < r.Y {
				cut := r.Y - f.Y
				f.Y += cut
				f.H -= cut
			}
			if f.Y+f.H > bottom {
				f.H = bottom - f.Y
			}
			if f.X < r.X {
				cut := r.X - f.X
				f.X = r.X
				f.W -= cut
			}
			if f.X+f.W > right {
				f.W = right - f.X
			}
		}
		if f.W <= 0 || f.H <= 0 {
			continue
		}
		out = append(out, f)
	}
	l.frags = out
}

// pseudoElement wraps a generated pseudo style in a childless synthetic node
// so the normal block layout can size it.
func pseudoElement(parent *Node, st *Style) *Node {
	if st == nil {
		return nil
	}
	return &Node{Type: ElementNode, Tag: "#pseudo", Style: st, Parent: parent}
}

// declaredFloatWidth returns a float's declared border-box width, or 0 when it
// is auto (shrink-to-fit).
func (l *layouter) declaredFloatWidth(n *Node, cbW int) int {
	st := n.Style
	if st == nil || !st.Width.Set || st.Width.IsAuto() {
		return 0
	}
	ml, mr, _, _, bl, br, _, _, pl, pr, _, _ := l.boxMetrics(st, cbW)
	w := l.widthValue(st, st.Width, cbW) + ml + mr + bl + br + pl + pr
	if w < 1 {
		w = 1
	}
	return w
}

// floatDropY finds the nearest lower row where a float of the given width fits
// between the active floats, or 0 when none does.
func (l *layouter) floatDropY(x, y, width, need int) int {
	var cands []int
	for _, f := range l.floats {
		if f.bottom > y {
			cands = append(cands, f.bottom)
		}
	}
	if len(cands) == 0 {
		return 0
	}
	sort.Ints(cands)
	for _, c := range cands {
		left, right := l.floatBand(x, c, width)
		if right-left >= need {
			return c
		}
	}
	return 0
}
