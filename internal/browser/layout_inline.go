package browser

import (
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

func (l *layouter) addText(x, y int, s string, st *Style, n *Node) {
	if l.outOfTime() {
		return
	}
	if s == "" || st == nil {
		return
	}
	if l.overFragLimit() || l.overRowLimit(y) {
		l.truncated = true
		return
	}
	l.frags = append(l.frags, Frag{Kind: fragText, X: x, Y: y, W: l.textWidth(s, st), H: l.lineH(st), Text: s, Style: st, Node: n})
}

// layoutTextOverlaps reports whether a scaled (kitty large-type) run shares
// cells with any other text run. Normal-size overlays are ignored; only scaled
// text spilling into neighbouring columns indicates a garbled layout.
func layoutTextOverlaps(ly *Layout) bool {
	if ly == nil {
		return false
	}
	type span struct {
		x, end int
		scaled bool
	}
	rows := make(map[int][]span)
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind != fragText || f.W <= 0 || f.H <= 0 || strings.TrimSpace(f.Text) == "" {
			continue
		}
		scaled := f.Style != nil && bigTextScale(f.Style, layoutZoom) > 1
		for y := f.Y; y < f.Y+f.H; y++ {
			rows[y] = append(rows[y], span{x: f.X, end: f.X + f.W, scaled: scaled})
		}
	}
	for _, spans := range rows {
		sort.Slice(spans, func(i, j int) bool {
			if spans[i].x != spans[j].x {
				return spans[i].x < spans[j].x
			}
			return spans[i].end < spans[j].end
		})
		for i := 1; i < len(spans); i++ {
			if spans[i].x < spans[i-1].end && (spans[i].scaled || spans[i-1].scaled) {
				return true
			}
		}
	}
	return false
}

// trimTextLeft removes the first cells from s, preserving rune boundaries.
func trimTextLeft(s string, cells int) string {
	for cells > 0 && s != "" {
		r, size := utf8.DecodeRuneInString(s)
		cells -= runeWidth(r)
		s = s[size:]
	}
	return s
}

type inlineItemKind int

type inlineItem struct {
	kind  inlineItemKind
	text  string
	style *Style
	node  *Node
	w, h  int
	paint func(x, y int)
}

type inlineCtx struct {
	l          *layouter
	x, y       int
	baseX      int
	baseW      int
	maxW       int
	imageCapW  int
	base       *Style
	items      []inlineItem
	curW       int
	hasContent bool
	space      bool
	spaceStyle *Style
	spaceNode  *Node
	// lastSpace records that the most recent emitted item was a collapsed
	// inter-word space, so whitespace crossing an inline-box boundary does
	// not add a second space.
	lastSpace   bool
	breakBefore bool
}

// lineBounds returns the left edge and width available for the line that
// starts at the current row, shortened around any active floats.
func (c *inlineCtx) lineBounds() (int, int) {
	left, right := c.l.floatBand(c.baseX, c.y, c.baseW)
	w := right - left
	if w < 1 {
		w = 1
	}
	return left, w
}

func (c *inlineCtx) lineMaxW() int {
	_, w := c.lineBounds()
	return w
}

func (l *layouter) inlineFlow(nodes []*Node, x, y, width int, base *Style) int {
	if l.outOfTime() {
		return 0
	}
	if l.truncated || l.overRowLimit(y) {
		l.truncated = true
		return 0
	}
	if base == nil {
		base = defaultStyle()
	}
	c := &inlineCtx{l: l, x: x, y: y, baseX: x, baseW: width, maxW: width, imageCapW: l.imageCapWidth(), base: base}
	if width < 1 {
		c.maxW = 1
	}
	if indent := base.TextIndent.ResolveH(width, base.FontSize); indent > 0 {
		c.items = append(c.items, inlineItem{kind: itemSpace, w: indent, style: base})
		c.curW = indent
	}
	for _, n := range nodes {
		c.collect(n)
	}
	if len(c.items) > 0 {
		c.flush(true)
	}
	if c.y < y {
		return 0
	}
	return c.y - y
}

func (c *inlineCtx) collect(n *Node) {
	if n == nil {
		return
	}
	if n.Type == TextNode {
		if n.Style != nil && !n.Style.Hidden {
			c.addText(expandTabs(n.Text), n.Style, n)
		}
		return
	}
	if n.Type != ElementNode {
		return
	}
	st := n.Style
	if st == nil || st.Display == "none" || st.Hidden {
		return
	}
	if isOutOfFlow(st) {
		c.l.deferAbsolute(n, c.x, c.y)
		return
	}
	switch n.Tag {
	case "br":
		if len(c.items) > 0 {
			c.flush(true)
		} else {
			c.y += c.l.lineH(st)
		}
		return
	case "wbr":
		return
	case "img":
		c.addImage(n, st)
		return
	case "input", "select", "textarea", "button":
		c.addForm(n, st)
		return
	case "svg":
		if n.Img != nil {
			c.addImage(n, st)
			return
		}
		label, visible := svgLabel(n)
		if !visible || label == "" {
			return
		}
		c.addLabel(label, st, n)
		return
	case "canvas", "video", "audio", "iframe", "object", "embed":
		c.addLabel("["+n.Tag+"]", st, n)
		return
	case "hr":
		c.flushIfAny()
		return
	}
	if st.Display == "inline" || st.Display == "inline-block" {
		// The inter-word space that precedes the box must stay outside its
		// margin/padding, or the box's edge cells appear before the space.
		c.flushPendingSpace(st)
		left, right := c.inlineEdges(st)
		if left > 0 {
			c.items = append(c.items, inlineItem{kind: itemSpace, w: left, style: st, node: n})
			c.curW += left
		}
		savedBreak := c.breakBefore
		c.breakBefore = true
		if n.Before != nil && n.Before.Content != "" {
			c.collect(pseudoElement(n, n.Before))
		}
		for _, ch := range n.Children {
			c.collect(ch)
		}
		if n.After != nil && n.After.Content != "" {
			c.collect(pseudoElement(n, n.After))
		}
		c.breakBefore = savedBreak
		if right > 0 {
			c.items = append(c.items, inlineItem{kind: itemSpace, w: right, style: st, node: n})
			c.curW += right
		}
		return
	}
	for _, ch := range n.Children {
		c.collect(ch)
	}
}

// inlineEdges returns the horizontal margin, border and padding reserved
// around an inline box. The three edges on each side are summed in CSS px and
// rounded to cells once, so several sub-cell values (such as a 1px border and
// 4px padding and margin) take one cell rather than one cell each. A positive
// total still reserves at least one cell so menu links do not glue together.
func (c *inlineCtx) inlineEdges(st *Style) (int, int) {
	if st == nil {
		return 0, 0
	}
	avail := c.maxW
	if avail < 1 {
		avail = 1
	}
	return inlineEdgeCells(st, 3, avail), inlineEdgeCells(st, 1, avail)
}

// inlineEdgeCells sums one side's margin, border and padding and converts the
// total to terminal cells in a single rounding step.
func inlineEdgeCells(st *Style, side, basis int) int {
	total := 0.0
	add := func(l Length) {
		if !l.Set || l.Auto {
			return
		}
		switch {
		case l.Calc:
			total += l.CalcPx/cellW + float64(basis)*l.CalcPct/100
		case l.U == UnitPercent:
			total += float64(basis) * l.V / 100
		default:
			total += l.CSSPx(st.FontSize) / cellW
		}
	}
	add(st.Margin[side])
	if st.BorderStyle[side] {
		add(st.Border[side])
	}
	add(st.Padding[side])
	if total <= 0 {
		return 0
	}
	n := int(math.Round(total))
	if n < 1 {
		n = 1
	}
	return n
}

// flushPendingSpace emits the inter-word space collected before an inline box
// so the box's left edge comes after it. It mirrors addWord's space handling.
func (c *inlineCtx) flushPendingSpace(next *Style) {
	if !c.space {
		return
	}
	c.space = false
	if !c.hasContent {
		return
	}
	spaceStyle := c.spaceStyle
	if spaceStyle == nil {
		spaceStyle = next
	}
	if spaceStyle == nil {
		return
	}
	spaceW := c.charW(' ', spaceStyle)
	if (c.base == nil || c.base.WhiteSpace != "nowrap") && c.curW+spaceW > c.lineMaxW() {
		c.flush(false)
		return
	}
	if c.lastSpace {
		return
	}
	c.items = append(c.items, inlineItem{kind: itemSpace, w: spaceW, style: spaceStyle, node: c.spaceNode})
	c.curW += spaceW
	c.lastSpace = true
}

func lengthPositive(l Length) bool {
	if !l.Set || l.Auto {
		return false
	}
	if l.Calc {
		return l.CalcPx > 0 || l.CalcPct > 0
	}
	return l.V > 0
}

// lineH returns the line box height for st, including large-glyph scaling.
func (l *layouter) lineH(st *Style) int {
	if st == nil {
		return 1
	}
	h := st.LineHeightCells()
	if l.bigText {
		if s := bigTextScale(st, l.zoom); s > h {
			h = s
		}
	}
	return h
}

// scale is how many cells each glyph in st occupies.
func (l *layouter) scale(st *Style) int {
	if !l.bigText || st == nil {
		return 1
	}
	return bigTextScale(st, l.zoom)
}

// textWidth measures s in layout cells, honouring glyph scaling.
func (l *layouter) textWidth(s string, st *Style) int {
	return stringWidth(s) * l.scale(st)
}

func (c *inlineCtx) addText(s string, st *Style, node *Node) {
	if s == "" {
		return
	}
	s = applyTextTransform(s, st.TextTransform)
	if st.WhiteSpace == "pre" {
		c.addPre(s, st, node)
		return
	}
	start := -1
	for i, r := range s {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' || r == '\f' {
			if start >= 0 {
				c.addWord(s[start:i], st, node)
				start = -1
			}
			if !c.space {
				c.spaceStyle, c.spaceNode = st, node
			}
			c.space = true
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		c.addWord(s[start:], st, node)
	}
}

func (c *inlineCtx) charW(r rune, st *Style) int {
	return runeWidth(r) * c.l.scale(st)
}

func (c *inlineCtx) addPre(s string, st *Style, node *Node) {
	for _, r := range s {
		if r == '\n' {
			c.flush(true)
			continue
		}
		if r == '\r' {
			continue
		}
		rw := c.charW(r, st)
		if rw == 0 {
			continue
		}
		if c.curW+rw > c.lineMaxW() && c.curW > 0 {
			c.flush(false)
		}
		c.appendRune(r, st, node)
		c.curW += rw
		c.hasContent = true
	}
}

func (c *inlineCtx) appendRune(r rune, st *Style, node *Node) {
	rw := c.charW(r, st)
	if n := len(c.items); n > 0 {
		it := &c.items[n-1]
		if it.kind == itemWord && it.style == st && it.node == node {
			it.text += string(r)
			it.w += rw
			return
		}
	}
	c.items = append(c.items, inlineItem{kind: itemWord, text: string(r), style: st, node: node, w: rw})
	c.lastSpace = false
}

func (c *inlineCtx) addWord(w string, st *Style, node *Node) {
	if w == "" {
		return
	}
	if c.breakBefore {
		c.breakBefore = false
		if (c.base == nil || c.base.WhiteSpace != "nowrap") && c.curW > 0 && c.curW+c.l.textWidth(w, st) > c.lineMaxW() {
			c.flush(false)
		}
	}
	width := c.l.textWidth(w, st)
	if width <= 0 {
		return
	}
	nowrap := st.WhiteSpace == "nowrap"
	spaceW := c.charW(' ', st)
	if c.space {
		spaceStyle, spaceNode := c.spaceStyle, c.spaceNode
		if spaceStyle == nil {
			spaceStyle = st
		}
		if c.hasContent {
			if !nowrap && c.curW+spaceW+width > c.lineMaxW() {
				c.flush(false)
			} else if !c.lastSpace {
				c.items = append(c.items, inlineItem{kind: itemSpace, w: spaceW, style: spaceStyle, node: spaceNode})
				c.curW += spaceW
				c.lastSpace = true
			}
		}
		c.space = false
		c.spaceStyle, c.spaceNode = nil, nil
	}
	if width > c.lineMaxW() && (c.base == nil || c.base.WhiteSpace != "nowrap") {
		for _, r := range w {
			rw := c.charW(r, st)
			if rw == 0 {
				continue
			}
			if c.curW+rw > c.lineMaxW() && c.curW > 0 {
				c.flush(false)
			}
			c.appendRune(r, st, node)
			c.curW += rw
			c.hasContent = true
		}
		return
	}
	if !nowrap && c.curW > 0 && c.curW+width > c.lineMaxW() {
		c.flush(false)
	}
	c.items = append(c.items, inlineItem{kind: itemWord, text: w, style: st, node: node, w: width})
	c.curW += width
	c.hasContent = true
	c.lastSpace = false
}

func (c *inlineCtx) addAtomicBox(it inlineItem) {
	if it.w < 1 {
		it.w = 1
	}
	if it.h < 1 {
		it.h = 1
	}
	nowrap := it.style != nil && it.style.WhiteSpace == "nowrap"
	spaceW := c.charW(' ', it.style)
	if c.space && c.hasContent {
		spaceStyle, spaceNode := c.spaceStyle, c.spaceNode
		if spaceStyle == nil {
			spaceStyle = it.style
		}
		if !nowrap && c.curW+spaceW+it.w > c.lineMaxW() {
			c.flush(false)
		} else if !c.lastSpace {
			c.items = append(c.items, inlineItem{kind: itemSpace, w: spaceW, style: spaceStyle, node: spaceNode})
			c.curW += spaceW
			c.lastSpace = true
		}
	}
	c.space = false
	c.spaceStyle, c.spaceNode = nil, nil
	if !nowrap && c.curW > 0 && c.curW+it.w > c.lineMaxW() {
		c.flush(false)
	}
	c.items = append(c.items, it)
	c.curW += it.w
	c.hasContent = true
	c.lastSpace = false
}

func (c *inlineCtx) flushIfAny() {
	if len(c.items) > 0 {
		c.flush(true)
	}
}

// inlineY places one inline box inside its line box. Terminal rows have no
// usable baseline, so baseline/super/sub stay centered like middle.
func inlineY(st *Style, lineY, lineH, itemH int) int {
	align := ""
	if st != nil {
		align = st.VerticalAlign
	}
	switch align {
	case "top", "text-top":
		return lineY
	case "bottom", "text-bottom":
		return lineY + lineH - itemH
	default:
		return lineY + (lineH-itemH)/2
	}
}

func (c *inlineCtx) flush(last bool) {
	lineH := c.l.lineH(c.base)
	for i := range c.items {
		it := &c.items[i]
		h := 1
		if it.style != nil {
			h = c.l.lineH(it.style)
		}
		if it.kind == itemAtomic {
			h = it.h
		}
		if h > lineH {
			lineH = h
		}
	}
	bandX, bandW := c.lineBounds()
	shift := 0
	switch c.base.TextAlign {
	case "center":
		shift = (bandW - c.curW) / 2
	case "right":
		shift = bandW - c.curW
	}
	if shift < 0 {
		shift = 0
	}
	perSpace, remainder := 0, 0
	if c.base.TextAlign == "justify" && !last && c.curW < bandW {
		spaces := 0
		for i := range c.items {
			if c.items[i].kind == itemSpace && c.items[i].w > 0 {
				spaces++
			}
		}
		if spaces > 0 {
			extra := bandW - c.curW
			perSpace = extra / spaces
			remainder = extra % spaces
		}
	}
	pos := 0
	for i := range c.items {
		it := &c.items[i]
		switch it.kind {
		case itemSpace:
			if it.style != nil && it.w > 0 {
				add := perSpace
				if remainder > 0 {
					add++
					remainder--
				}
				cellW := it.w + add
				runH := c.l.lineH(it.style)
				yy := inlineY(it.style, c.y, lineH, runH)
				spaces := strings.Repeat(" ", cellW)
				if s := c.l.scale(it.style); s > 1 {
					spaces = strings.Repeat(" ", maxInt(1, cellW/s))
				}
				c.l.frags = append(c.l.frags, Frag{
					Kind: fragText, X: bandX + shift + pos, Y: yy, W: cellW, H: runH,
					Text: spaces, Style: it.style, Node: it.node,
				})
				pos += cellW
				continue
			}
			pos += it.w
		case itemWord:
			runH := c.l.lineH(it.style)
			yy := inlineY(it.style, c.y, lineH, runH)
			c.l.frags = append(c.l.frags, Frag{
				Kind: fragText, X: bandX + shift + pos, Y: yy, W: it.w, H: runH,
				Text: it.text, Style: it.style, Node: it.node,
			})
			pos += it.w
		case itemAtomic:
			yy := inlineY(it.style, c.y, lineH, it.h)
			it.paint(bandX+shift+pos, yy)
			pos += it.w
		}
	}
	c.y += lineH
	c.items = c.items[:0]
	c.curW = 0
	c.hasContent = false
	c.space = false
	c.spaceStyle, c.spaceNode = nil, nil
	c.lastSpace = false
}
