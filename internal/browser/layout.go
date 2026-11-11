package browser

import (
	"image"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type fragKind int

const (
	fragFill fragKind = iota
	fragText
	fragImage
)

// Rect is a terminal-cell rectangle.
type Rect struct {
	X, Y, W, H int
}

// LinkInfo describes one navigable link and the cells it occupies.
type LinkInfo struct {
	Node  *Node
	Href  string
	Rects []Rect
	Index int
}

// Frag is a paint primitive produced by layout, in document coordinates
// (row 0 is the top of the page).
type Frag struct {
	Kind  fragKind
	X, Y  int
	W, H  int
	Color Color
	Text  string
	Style *Style
	Node  *Node
	Img   image.Image
	Link  *LinkInfo
	// Fixed marks a viewport-anchored fragment. Y is recalculated on scroll
	// from FixedY (the viewport row) plus the browser scroll offset.
	Fixed  bool
	FixedY int
}

// Layout is the result of styling and layout.
type Layout struct {
	Width  int
	Height int
	Frags  []Frag
	Links  []*LinkInfo
	Styles map[*Node]*Style
	PageBG Color
	Body   *Node
	HTML   *Node

	// Truncated reports that a resource or time limit stopped layout early.
	Truncated bool

	// Cursor is the cell of the focused input/textarea caret, in document
	// coordinates. It is drawn as an overlay, not part of the text.
	Cursor    Rect
	HasCursor bool

	// fixedScroll tracks the scroll offset already applied to fixed fragments.
	fixedScroll  int
	fixedApplied bool
}

const (
	maxLayoutFrags = 250000
	maxLayoutRows  = 200000
)

type layouter struct {
	styles    map[*Node]*Style
	width     int
	frags     []Frag
	bigText   bool
	truncated bool
	deadline  time.Time
	expired   bool

	// cb is the padding box of the nearest positioned ancestor, used to
	// resolve absolute offsets. nil at the root, where rootCB stands in.
	cb     *cbRect
	rootCB *cbRect
	// fixedCB is the viewport rectangle used by position:fixed elements. It
	// falls back to rootCB when the viewport height is unknown (dump mode).
	fixedCB *cbRect
	// curFixed marks that the absolute box currently being laid out is inside
	// a fixed subtree, so nested absolutes stay viewport-anchored.
	curFixed bool
	// abs collects absolutely positioned boxes. They are kept out of normal
	// flow and laid out after it, so they paint above in-flow content.
	abs []*absBox

	// cursor is the focused form caret cell, in document coordinates.
	cursor    Rect
	hasCursor bool
}

// cbRect is a containing block in document coordinates. h stays unknown until
// the ancestor's layout finishes, or the in-flow page height is known for the
// initial containing block.
type cbRect struct {
	x, y, w, h int
	knownH     bool
}

// absBox records an absolutely positioned element found during normal flow.
// The subtree is laid out in a second pass, once containing-block heights are
// known, then its fragments are appended above the in-flow content.
type absBox struct {
	node    *Node
	cb      *cbRect
	staticX int
	staticY int
	fixed   bool
}

// outOfTime reports whether the layout pass has exceeded its deadline. The
// flag is sticky so recursive callers unwind instead of starting new work.
func (l *layouter) outOfTime() bool {
	if l.expired {
		return true
	}
	if !l.deadline.IsZero() && time.Now().After(l.deadline) {
		l.expired = true
		l.truncated = true
	}
	return l.expired
}

// BuildLayout computes styles, runs block/inline layout, and gathers links.
func BuildLayout(root *Node, author []Rule, width int) *Layout {
	return BuildLayoutBig(root, author, width, false)
}

// BuildLayoutBig is BuildLayout with optional large-glyph layout. When
// bigText is true, runs larger than the base font get extra cell width and
// line height, matching the kitty text-sizing protocol.
func BuildLayoutBig(root *Node, author []Rule, width int, bigText bool) *Layout {
	return BuildLayoutDeadline(root, author, width, bigText, time.Time{})
}

// BuildLayoutDeadline is BuildLayoutBig with a cooperative time budget shared
// by style computation and layout. A zero deadline means no limit. When the
// budget runs out the returned layout is partial and Truncated is set.
func BuildLayoutDeadline(root *Node, author []Rule, width int, bigText bool, deadline time.Time) *Layout {
	return buildLayout(root, author, width, 0, bigText, false, deadline)
}

// BuildLayoutViewportDeadline is BuildLayoutDeadline with the terminal
// viewport height, so position:fixed boxes can anchor to the visible area.
func BuildLayoutViewportDeadline(root *Node, author []Rule, width, viewportH int, bigText bool, deadline time.Time) *Layout {
	return buildLayout(root, author, width, viewportH, bigText, false, deadline)
}

// BuildLayoutDarkDeadline is BuildLayoutDeadline with the dark palette applied
// to page, text, border and background colors after layout.
func BuildLayoutDarkDeadline(root *Node, author []Rule, width int, bigText bool, deadline time.Time) *Layout {
	return buildLayout(root, author, width, 0, bigText, true, deadline)
}

// BuildLayoutDarkViewportDeadline is BuildLayoutViewportDeadline with the dark
// palette applied.
func BuildLayoutDarkViewportDeadline(root *Node, author []Rule, width, viewportH int, bigText bool, deadline time.Time) *Layout {
	return buildLayout(root, author, width, viewportH, bigText, true, deadline)
}

func buildLayout(root *Node, author []Rule, width, viewportH int, bigText, dark bool, deadline time.Time) *Layout {
	if width < 1 {
		width = 1
	}
	engine := newStyleEngineDeadline(author, width*cellW, deadline)
	styles := engine.computeAll(root)
	l := &layouter{styles: styles, width: width, bigText: bigText, deadline: deadline}
	l.rootCB = &cbRect{w: width}
	if viewportH > 0 {
		l.fixedCB = &cbRect{w: width, h: viewportH, knownH: true}
	} else {
		l.fixedCB = l.rootCB
	}
	if engine.expired {
		l.truncated = true
	}
	html := root.FindFirst("html")
	if html != nil {
		l.block(html, 0, 0, width)
	} else {
		l.flow(root, 0, 0, width)
	}
	// Absolute boxes resolve against the in-flow page height when they have no
	// positioned ancestor, so record it before laying them out.
	inFlowHeight := 0
	for i := range l.frags {
		f := &l.frags[i]
		if f.Y+f.H > inFlowHeight {
			inFlowHeight = f.Y + f.H
		}
		if f.Kind == fragText && f.Y+1 > inFlowHeight {
			inFlowHeight = f.Y + 1
		}
	}
	l.rootCB.h = inFlowHeight
	l.rootCB.knownH = true
	l.finalizeAbsolute()
	height := 0
	for i := range l.frags {
		f := &l.frags[i]
		if f.Fixed {
			continue
		}
		if f.Y+f.H > height {
			height = f.Y + f.H
		}
		if f.Kind == fragText && f.Y+1 > height {
			height = f.Y + 1
		}
	}
	body := root.FindFirst("body")
	if body == nil && html != nil {
		body = html.FindFirst("body")
	}
	pageBG := RGB(255, 255, 255)
	if body != nil && body.Style != nil && body.Style.Background.opaque() {
		pageBG = body.Style.Background
	} else if html != nil && html.Style != nil && html.Style.Background.opaque() {
		pageBG = html.Style.Background
	}
	out := &Layout{
		Width: width, Height: height, Frags: l.frags, Styles: styles,
		PageBG: pageBG, Body: body, HTML: html, Truncated: l.truncated,
		Cursor: l.cursor, HasCursor: l.hasCursor,
	}
	out.buildLinks()
	if dark {
		applyDarkLayout(out)
	}
	return out
}

func (l *layouter) addFill(x, y, w, h int, c Color) {
	if l.outOfTime() {
		return
	}
	if w <= 0 || h <= 0 || !c.Set || c.A == 0 {
		return
	}
	if len(l.frags) >= maxLayoutFrags || y > maxLayoutRows {
		l.truncated = true
		return
	}
	l.frags = append(l.frags, Frag{Kind: fragFill, X: x, Y: y, W: w, H: h, Color: c})
}

func (l *layouter) addText(x, y int, s string, st *Style, n *Node) {
	if l.outOfTime() {
		return
	}
	if s == "" || st == nil {
		return
	}
	if len(l.frags) >= maxLayoutFrags || y > maxLayoutRows {
		l.truncated = true
		return
	}
	l.frags = append(l.frags, Frag{Kind: fragText, X: x, Y: y, W: l.textWidth(s, st), H: l.lineH(st), Text: s, Style: st, Node: n})
}

// setInputCursor records the focused form caret cell in document coordinates.
func (l *layouter) setInputCursor(x, y int) {
	l.cursor = Rect{X: x, Y: y, W: 1, H: 1}
	l.hasCursor = true
}

func resolveBorderH(l Length, basis int, fontPx float64) int {
	if !l.Set {
		return 0
	}
	w := l.resolveH(basis, fontPx)
	if l.V > 0 && w < 1 {
		w = 1
	}
	if w < 0 {
		w = 0
	}
	return w
}

func resolveBorderV(l Length, basis int, fontPx float64) int {
	if !l.Set {
		return 0
	}
	w := l.resolveV(basis, fontPx)
	if l.V > 0 && w < 1 {
		w = 1
	}
	if w < 0 {
		w = 0
	}
	return w
}

func nonNeg(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func (l *layouter) boxMetrics(st *Style, availW int) (ml, mr, mt, mb, bl, br, bt, bb, pl, pr, pt, pb int) {
	f := st.FontSize
	ml = st.Margin[3].resolveH(availW, f)
	mr = st.Margin[1].resolveH(availW, f)
	mt = st.Margin[0].resolveV(availW, f)
	mb = st.Margin[2].resolveV(availW, f)
	bl = resolveBorderH(st.Border[3], availW, f)
	br = resolveBorderH(st.Border[1], availW, f)
	bt = resolveBorderV(st.Border[0], availW, f)
	bb = resolveBorderV(st.Border[2], availW, f)
	pl = nonNeg(st.Padding[3].resolveH(availW, f))
	pr = nonNeg(st.Padding[1].resolveH(availW, f))
	pt = nonNeg(st.Padding[0].resolveV(availW, f))
	pb = nonNeg(st.Padding[2].resolveV(availW, f))
	return
}

func isFormControl(n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	switch n.Tag {
	case "input", "select", "textarea", "button":
		return true
	}
	return false
}

// isDisabledControl reports a control that is disabled itself or belongs to a
// disabled fieldset.
func isDisabledControl(n *Node) bool {
	if n == nil {
		return false
	}
	for p := n; p != nil; p = p.Parent {
		if p.Type != ElementNode {
			continue
		}
		if (p.Tag == "input" || p.Tag == "select" || p.Tag == "textarea" || p.Tag == "button" || p.Tag == "option") && p.HasAttr("disabled") {
			return true
		}
		if p.Tag == "fieldset" && p.HasAttr("disabled") {
			return true
		}
	}
	return false
}

// isReadOnlyControl reports a text-like control that is not editable.
func isReadOnlyControl(n *Node) bool {
	if n == nil {
		return false
	}
	if n.Tag == "textarea" {
		return n.HasAttr("readonly")
	}
	if n.Tag != "input" {
		return false
	}
	switch strings.ToLower(n.Attr("type")) {
	case "", "text", "search", "email", "url", "tel", "number", "password",
		"date", "datetime-local", "month", "week", "time", "color":
		return n.HasAttr("readonly")
	}
	return false
}

// maxLength returns the maxlength limit in runes, or 0 when unrestricted.
func maxLength(n *Node) int {
	if n == nil {
		return 0
	}
	limit, err := strconv.Atoi(strings.TrimSpace(n.Attr("maxlength")))
	if err != nil || limit < 0 {
		return 0
	}
	return limit
}

func (l *layouter) isBlock(n *Node) bool {
	if n == nil || n.Type != ElementNode || n.Style == nil {
		return false
	}
	if n.Style.Display == "none" {
		return false
	}
	// Controls are rendered as inline form widgets even when a stylesheet
	// gives them display:block/flex; the block path does not paint their
	// value/widget.
	if isFormControl(n) {
		return false
	}
	switch n.Style.Display {
	case "block", "flex", "grid", "list-item", "table", "table-row", "table-cell":
		return true
	}
	return false
}

// block lays out node n as a block at (x,y) and returns its outer height,
// including vertical margins.
func (l *layouter) block(n *Node, x, y, availW int) int {
	h, _ := l.blockLayout(n, x, y, availW, 0)
	return h
}

// blockLayout is block with the border-box rectangle reported. forceContentH,
// when positive, stretches the content box to at least that height (used by
// absolutely positioned boxes with both top and bottom offsets).
func (l *layouter) blockLayout(n *Node, x, y, availW, forceContentH int) (int, Rect) {
	if l.outOfTime() {
		return 0, Rect{}
	}
	if l.truncated || y > maxLayoutRows {
		l.truncated = true
		return 0, Rect{}
	}
	st := n.Style
	if st == nil || st.Display == "none" {
		return 0, Rect{}
	}
	if availW < 1 {
		availW = 1
	}
	parentCB := l.cb
	fragStart := len(l.frags)
	absStart := len(l.abs)
	ml, mr, mt, mb, bl, br, bt, bb, pl, pr, pt, pb := l.boxMetrics(st, availW)
	contentW := availW - ml - mr - bl - br - pl - pr
	if st.Width.Set && !st.Width.IsAuto() {
		if w := st.Width.resolveH(availW, st.FontSize); w < contentW {
			contentW = w
		}
	}
	if st.MaxWidth.Set {
		if w := st.MaxWidth.resolveH(availW, st.FontSize); w >= 0 && w < contentW {
			contentW = w
		}
	}
	if contentW < 0 {
		contentW = 0
	}
	// Distribute space left over by width/max-width constraints into auto
	// margins, so `margin:0 auto` centers the box like a normal browser.
	if leftover := availW - ml - mr - bl - br - pl - pr - contentW; leftover > 0 {
		lAuto := st.Margin[3].Auto
		rAuto := st.Margin[1].Auto
		switch {
		case lAuto && rAuto:
			ml += leftover / 2
			mr += leftover - leftover/2
		case lAuto:
			ml += leftover
		case rAuto:
			mr += leftover
		}
	}
	boxX := x + ml
	top := y + mt
	contentX := boxX + bl + pl
	contentY := top + bt + pt
	boxW := bl + pl + contentW + pr + br

	// A positioned box is a containing block for its absolutely positioned
	// descendants. Its height is filled in once the content has been laid out.
	positioned := st.Position == "relative" || st.Position == "absolute" || st.Position == "fixed"
	var newCB *cbRect
	if positioned {
		newCB = &cbRect{x: boxX + bl, y: top + bt, w: pl + contentW + pr}
		l.cb = newCB
		defer func() { l.cb = parentCB }()
	}

	bgIndex := -1
	if st.Background.opaque() {
		l.addFill(boxX, top, boxW, 1, st.Background)
		bgIndex = len(l.frags) - 1
	}

	var contentH int
	switch {
	case st.Display == "table":
		contentH = l.table(n, contentX, contentY, contentW)
	case st.Display == "grid":
		contentH = l.grid(n, contentX, contentY, contentW)
	case st.Display == "flex" && st.FlexDir != "column":
		contentH = l.flexRow(n, contentX, contentY, contentW)
	default:
		contentH = l.flow(n, contentX, contentY, contentW)
	}
	if st.Height.Set && st.Height.U != UnitPercent {
		if h := st.Height.resolveV(availW, st.FontSize); h > contentH {
			contentH = h
		}
	}
	if forceContentH > contentH {
		contentH = forceContentH
	}
	if newCB != nil {
		newCB.h = pt + contentH + pb
		newCB.knownH = true
	}
	if st.Display == "list-item" {
		if marker := listMarker(st, n); marker != "" {
			ms := *st
			ms.Underline = false
			ms.Color = st.Color
			mw := l.textWidth(marker, &ms)
			mx := contentX - mw - 1
			if mx < 0 {
				mx = 0
			}
			l.addText(mx, contentY, marker, &ms, n)
		}
	}
	totalH := bt + pt + contentH + pb + bb
	if bgIndex >= 0 {
		l.frags[bgIndex].H = totalH
	}
	if bl+br+bt+bb > 0 {
		bc := st.BorderColor
		if !bc.Set {
			bc = RGB(0xd0, 0xd7, 0xde)
		}
		if bt > 0 {
			l.addFill(boxX, top, boxW, bt, bc)
		}
		if bb > 0 {
			l.addFill(boxX, top+bt+pt+contentH+pb, boxW, bb, bc)
		}
		if bl > 0 {
			l.addFill(boxX, top, bl, totalH, bc)
		}
		if br > 0 {
			l.addFill(boxX+boxW-br, top, br, totalH, bc)
		}
	}
	// Relative offsets move the box and its painted subtree without affecting
	// siblings. Shift the containing block it establishes as well, so nested
	// absolute descendants follow.
	if st.Position == "relative" {
		dx, dy := l.relativeOffset(st, parentCB)
		if dx != 0 || dy != 0 {
			for i := fragStart; i < len(l.frags); i++ {
				l.frags[i].X += dx
				l.frags[i].Y += dy
			}
			if newCB != nil {
				newCB.x += dx
				newCB.y += dy
			}
			for i := absStart; i < len(l.abs); i++ {
				l.abs[i].staticX += dx
				l.abs[i].staticY += dy
			}
		}
	}
	return mt + totalH + mb, Rect{X: boxX, Y: top, W: boxW, H: totalH}
}

// offsetSet reports whether a positioned offset is a real length rather than
// auto or unset.
func offsetSet(l Length) bool {
	return l.Set && !l.Auto
}

// relativeOffset resolves top/left/right/bottom for a relatively positioned
// box into a visual translation. Left wins over right and top over bottom,
// matching CSS for left-to-right text.
func (l *layouter) relativeOffset(st *Style, cb *cbRect) (int, int) {
	w, h := l.width, 0
	if cb != nil {
		w = cb.w
		if cb.knownH {
			h = cb.h
		}
	}
	dx, dy := 0, 0
	if offsetSet(st.Left) {
		dx = st.Left.resolveH(w, st.FontSize)
	} else if offsetSet(st.Right) {
		dx = -st.Right.resolveH(w, st.FontSize)
	}
	if offsetSet(st.Top) {
		dy = st.Top.resolveV(h, st.FontSize)
	} else if offsetSet(st.Bottom) {
		dy = -st.Bottom.resolveV(h, st.FontSize)
	}
	return dx, dy
}

// isOutOfFlow reports whether a box is absolutely or fixed positioned.
func isOutOfFlow(st *Style) bool {
	return st != nil && (st.Position == "absolute" || st.Position == "fixed")
}

// deferAbsolute records an out-of-flow element. Its subtree is laid out after
// normal flow, when containing-block heights are known.
func (l *layouter) deferAbsolute(n *Node, staticX, staticY int) {
	if n == nil || n.Style == nil || n.Style.Display == "none" || n.Style.Hidden {
		return
	}
	fixed := l.curFixed
	cb := l.cb
	if n.Style.Position == "fixed" {
		if l.fixedCB != l.rootCB {
			fixed = true
			cb = l.fixedCB
		} else {
			// No viewport height (dump mode): treat fixed as page-anchored so
			// the content still appears in the rendered document.
			fixed = false
			cb = l.rootCB
		}
	}
	if cb == nil {
		cb = l.rootCB
	}
	l.abs = append(l.abs, &absBox{node: n, cb: cb, staticX: staticX, staticY: staticY, fixed: fixed})
}

// finalizeAbsolute lays out the recorded absolute boxes and appends their
// fragments above the in-flow content. Boxes discovered while laying out an
// absolute subtree are appended to the queue and processed in tree order.
func (l *layouter) finalizeAbsolute() {
	for i := 0; i < len(l.abs); i++ {
		l.layoutAbsolute(l.abs[i])
	}
}

// layoutAbsolute places one absolute box against its containing block,
// resolving left/right/top/bottom, percentage offsets, shrink-to-fit widths
// and top+bottom height stretching.
func (l *layouter) layoutAbsolute(a *absBox) {
	if l.outOfTime() {
		return
	}
	n := a.node
	st := n.Style
	if st == nil || st.Display == "none" || st.Hidden {
		return
	}
	cb := a.cb
	if cb == nil {
		cb = l.rootCB
	}
	cbW := cb.w
	if cbW < 1 {
		cbW = l.width
	}
	cbH := 0
	if cb.knownH {
		cbH = cb.h
	}
	ml, mr, mt, mb, bl, br, bt, bb, pl, pr, pt, pb := l.boxMetrics(st, cbW)

	hasL, hasR := offsetSet(st.Left), offsetSet(st.Right)
	hasT, hasB := offsetSet(st.Top), offsetSet(st.Bottom)
	left, right, topOff, bottomOff := 0, 0, 0, 0
	if hasL {
		left = st.Left.resolveH(cbW, st.FontSize)
	}
	if hasR {
		right = st.Right.resolveH(cbW, st.FontSize)
	}
	if hasT {
		topOff = st.Top.resolveV(cbH, st.FontSize)
	}
	if hasB {
		bottomOff = st.Bottom.resolveV(cbH, st.FontSize)
	}

	borderPad := bl + br + pl + pr
	availContent := cbW - ml - mr - borderPad - left - right
	contentW := availContent
	switch {
	case st.Width.Set && !st.Width.IsAuto():
		contentW = st.Width.resolveH(cbW, st.FontSize)
	case hasL && hasR:
		// Stretch to fill the space between the offsets.
	default:
		// Shrink-to-fit: the used width is the content's preferred width,
		// clamped to the space available between the offsets.
		maxW, _ := l.measure(n)
		if l.isBlock(n) {
			maxW -= bl + br + pl + pr
		}
		if maxW < 0 {
			maxW = 0
		}
		if maxW < contentW {
			contentW = maxW
		}
	}
	if contentW < 0 {
		contentW = 0
	}
	availW := contentW + ml + mr + borderPad
	if availW < 1 {
		availW = 1
	}

	// With both vertical offsets and an auto height, stretch between them.
	forceH := 0
	if hasT && hasB && (!st.Height.Set || st.Height.IsAuto()) {
		forceH = cbH - topOff - bottomOff - mt - mb - bt - bb - pt - pb
		if forceH < 0 {
			forceH = 0
		}
	}

	x, y := a.staticX, a.staticY
	if hasL {
		x = cb.x + left
	}
	if hasT {
		y = cb.y + topOff
	}

	prevCB := l.cb
	prevFixed := l.curFixed
	l.cb = cb
	l.curFixed = a.fixed
	start := len(l.frags)
	_, rect := l.blockLayout(n, x, y, availW, forceH)
	frags := make([]Frag, len(l.frags)-start)
	copy(frags, l.frags[start:])
	l.frags = l.frags[:start]
	l.cb = prevCB
	l.curFixed = prevFixed

	desiredX, desiredY := rect.X, rect.Y
	switch {
	case hasL:
		desiredX = cb.x + left + ml
	case hasR:
		desiredX = cb.x + cb.w - right - mr - rect.W
	}
	switch {
	case hasT:
		desiredY = cb.y + topOff + mt
	case hasB:
		desiredY = cb.y + cbH - bottomOff - mb - rect.H
	}
	dx, dy := desiredX-rect.X, desiredY-rect.Y
	if a.fixed {
		for i := range frags {
			frags[i].Fixed = true
			frags[i].FixedY = frags[i].Y + dy
		}
	}
	if dx != 0 || dy != 0 {
		for i := range frags {
			frags[i].X += dx
			frags[i].Y += dy
		}
	}
	l.frags = append(l.frags, frags...)
}

// flexRow lays out a row-direction flex container. It supports equal or
// explicit item widths, flex-grow, shrink-to-fit items and column gaps; it does
// not implement wrapping, alignment or order.
type gridTrack struct {
	fixed int
	flex  float64
	auto  bool
}

func splitGridParts(s string) []string {
	var out []string
	start := 0
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ' ', '\t', '\n', '\r', ',':
			if depth == 0 {
				if part := strings.TrimSpace(s[start:i]); part != "" {
					out = append(out, part)
				}
				start = i + 1
			}
		}
	}
	if part := strings.TrimSpace(s[start:]); part != "" {
		out = append(out, part)
	}
	return out
}

func parseGridTracks(raw string, avail int, font float64) []gridTrack {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "none") || strings.HasPrefix(strings.ToLower(raw), "subgrid") {
		return nil
	}
	var out []gridTrack
	for _, part := range splitGridParts(raw) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lower := strings.ToLower(part)
		if strings.HasPrefix(lower, "repeat(") && strings.HasSuffix(part, ")") {
			inner := part[len("repeat(") : len(part)-1]
			fields := splitTopLevel(inner, ',')
			if len(fields) < 2 {
				continue
			}
			count := 1
			first := strings.TrimSpace(fields[0])
			if n, err := strconv.Atoi(first); err == nil && n > 0 {
				count = n
			}
			rest := strings.Join(fields[1:], ",")
			tracks := parseGridTracks(rest, avail, font)
			for i := 0; i < count && i < 64; i++ {
				out = append(out, tracks...)
			}
			continue
		}
		if strings.HasPrefix(lower, "minmax(") && strings.HasSuffix(part, ")") {
			inner := part[len("minmax(") : len(part)-1]
			fields := splitTopLevel(inner, ',')
			if len(fields) == 2 {
				part = strings.TrimSpace(fields[1])
				lower = strings.ToLower(part)
			}
		}
		switch {
		case strings.HasSuffix(lower, "fr"):
			if f, err := strconv.ParseFloat(strings.TrimSuffix(lower, "fr"), 64); err == nil && f > 0 {
				out = append(out, gridTrack{flex: f})
			} else {
				out = append(out, gridTrack{flex: 1})
			}
		case lower == "auto" || strings.Contains(lower, "content") || strings.HasPrefix(lower, "fit-content"):
			out = append(out, gridTrack{auto: true})
		default:
			if l := parseLength(part, font); l.Set {
				out = append(out, gridTrack{fixed: l.resolveH(avail, font)})
			} else {
				out = append(out, gridTrack{auto: true})
			}
		}
	}
	return out
}

func resolveGridTracks(tracks []gridTrack, avail, gap int) []int {
	if len(tracks) == 0 {
		return nil
	}
	widths := make([]int, len(tracks))
	fixedTotal := 0
	flexTotal := 0.0
	autoCount := 0
	for i, t := range tracks {
		if t.flex > 0 {
			flexTotal += t.flex
		} else if t.auto {
			autoCount++
		} else {
			widths[i] = t.fixed
			fixedTotal += t.fixed
		}
	}
	remaining := avail - gap*(len(tracks)-1) - fixedTotal
	if remaining < 0 {
		remaining = 0
	}
	if flexTotal > 0 {
		for i, t := range tracks {
			if t.flex > 0 {
				widths[i] = int(float64(remaining) * t.flex / flexTotal)
			}
		}
		remaining = 0
	}
	if autoCount > 0 {
		each := remaining / autoCount
		for i, t := range tracks {
			if t.auto {
				widths[i] = each
			}
		}
	}
	total := 0
	for i := range widths {
		if widths[i] < 1 {
			widths[i] = 1
		}
		total += widths[i]
	}
	if max := avail - gap*(len(tracks)-1); total > max && max > 0 {
		scaled := 0
		for i := range widths {
			widths[i] = widths[i] * max / total
			if widths[i] < 1 {
				widths[i] = 1
			}
			scaled += widths[i]
		}
		if diff := scaled - max; diff > 0 {
			for i := len(widths) - 1; i >= 0 && diff > 0; i-- {
				if widths[i] > 1 {
					widths[i]--
					diff--
				}
			}
		}
	}
	return widths
}

func gridPlacement(raw string, cols int) (start, span int, explicit bool) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return -1, 1, false
	}
	parts := splitTopLevel(raw, '/')
	first := strings.TrimSpace(parts[0])
	if strings.HasPrefix(first, "span ") {
		if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(first, "span "))); err == nil && n > 0 {
			return -1, n, true
		}
		return -1, 1, true
	}
	line, err := strconv.Atoi(first)
	if err != nil {
		return -1, 1, false
	}
	start = line - 1
	if start < 0 {
		start = cols + line
	}
	if start < 0 {
		start = 0
	}
	span = 1
	if len(parts) > 1 {
		endRaw := strings.TrimSpace(parts[1])
		if strings.HasPrefix(endRaw, "span ") {
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(endRaw, "span "))); err == nil && n > 0 {
				span = n
			}
		} else if endLine, err := strconv.Atoi(endRaw); err == nil {
			if endLine < 0 {
				endLine = cols + endLine + 2
			}
			if endLine > line {
				span = endLine - line
			}
		}
	}
	if span < 1 {
		span = 1
	}
	return start, span, true
}

func (l *layouter) grid(n *Node, x, y, availW int) int {
	if l.outOfTime() {
		return 0
	}
	st := n.Style
	if st == nil {
		return l.flow(n, x, y, availW)
	}
	tracks := parseGridTracks(st.GridCols, availW, st.FontSize)
	if len(tracks) == 0 {
		return l.flow(n, x, y, availW)
	}
	gap := st.Gap.resolveH(availW, st.FontSize)
	if !st.Gap.Set {
		gap = 0
	}
	rowGap := gap
	if st.RowGap.Set {
		rowGap = st.RowGap.resolveH(availW, st.FontSize)
	}
	widths := resolveGridTracks(tracks, availW, gap)
	if len(widths) > 4 {
		total := 0
		for _, w := range widths {
			total += w
		}
		if total/len(widths) < 6 {
			return l.flow(n, x, y, availW)
		}
	}
	colX := make([]int, len(widths))
	cx := x
	for i, w := range widths {
		colX[i] = cx
		cx += w + gap
	}
	col := 0
	rowH := 0
	startY := y
	for _, child := range n.Children {
		if child.Type != ElementNode || child.Style == nil || child.Style.Display == "none" {
			continue
		}
		if isOutOfFlow(child.Style) {
			l.deferAbsolute(child, x, y)
			continue
		}
		start, span, explicit := gridPlacement(child.Style.GridColumn, len(widths))
		if explicit && start >= 0 && start < len(widths) {
			col = start
		}
		if span < 1 {
			span = 1
		}
		if span > len(widths) {
			span = len(widths)
		}
		if col+span > len(widths) {
			y += rowH + rowGap
			rowH = 0
			col = 0
		}
		cellX := colX[col]
		cellW := 0
		for i := 0; i < span && col+i < len(widths); i++ {
			cellW += widths[col+i]
			if i > 0 {
				cellW += gap
			}
		}
		if !explicit {
			if mw, _ := l.measure(child); mw > cellW {
				for span < len(widths)-col {
					cellW += gap + widths[col+span]
					span++
					if cellW >= mw {
						break
					}
				}
			}
		}
		h := l.block(child, cellX, y, cellW)
		if h > rowH {
			rowH = h
		}
		col += span
		if col >= len(widths) {
			y += rowH + rowGap
			rowH = 0
			col = 0
		}
	}
	if col > 0 {
		y += rowH
	}
	return y - startY
}

func (l *layouter) flexRow(n *Node, x, y, availW int) int {
	if l.outOfTime() {
		return 0
	}
	var items []*Node
	for _, c := range n.Children {
		if c.Type == ElementNode && c.Style != nil && c.Style.Display != "none" {
			if isOutOfFlow(c.Style) {
				l.deferAbsolute(c, x, y)
				continue
			}
			items = append(items, c)
		}
	}
	if len(items) == 0 {
		// Text-only flex items: regular inline flow already places them in a
		// row and keeps the text visible.
		return l.flow(n, x, y, availW)
	}
	gap := 0
	if n.Style != nil {
		gap = nonNeg(n.Style.Gap.resolveH(availW, n.Style.FontSize))
	}
	totalAvail := availW - gap*(len(items)-1)
	if totalAvail < len(items) {
		totalAvail = len(items)
	}
	widths := make([]int, len(items))
	grow := make([]float64, len(items))
	fixed := 0
	growTotal := 0.0
	for i, it := range items {
		st := it.Style
		ml, mr, _, _, bl, br, _, _, pl, pr, _, _ := l.boxMetrics(st, availW)
		extra := ml + mr + bl + br + pl + pr
		switch {
		case st.FlexBasisSet:
			basis := st.FlexBasis.resolveH(availW, st.FontSize)
			if basis < 0 {
				basis = 0
			}
			widths[i] = basis + extra
			if widths[i] < 1 {
				widths[i] = 1
			}
			fixed += widths[i]
			if st.FlexGrow > 0 {
				grow[i] = st.FlexGrow
				growTotal += st.FlexGrow
			}
		case st.Width.Set && !st.Width.IsAuto():
			w := st.Width.resolveH(availW, st.FontSize) + extra
			if w < 1 {
				w = 1
			}
			widths[i] = w
			fixed += w
			if st.FlexGrow > 0 {
				grow[i] = st.FlexGrow
				growTotal += st.FlexGrow
			}
		case st.FlexGrow > 0:
			grow[i] = st.FlexGrow
			growTotal += st.FlexGrow
		default:
			mw, _ := l.measure(it)
			w := mw + extra
			if w < 1 {
				w = 1
			}
			widths[i] = w
			fixed += w
		}
	}
	if remaining := totalAvail - fixed; remaining > 0 && growTotal > 0 {
		for i := range items {
			if grow[i] > 0 {
				widths[i] += int(float64(remaining) * grow[i] / growTotal)
			}
		}
	}
	mins := make([]int, len(items))
	for i, it := range items {
		ml, mr, _, _, bl, br, _, _, pl, pr, _, _ := l.boxMetrics(it.Style, availW)
		_, mi := l.measure(it)
		mins[i] = maxInt(1, mi+ml+mr+bl+br+pl+pr)
		if widths[i] < mins[i] {
			widths[i] = mins[i]
		}
	}
	total := 0
	for i := range widths {
		if widths[i] < 1 {
			widths[i] = 1
		}
		total += widths[i]
	}
	if total > totalAvail && total > 0 {
		for i := range widths {
			widths[i] = maxInt(mins[i], widths[i]*totalAvail/total)
		}
	}
	maxH := 0
	xx := x
	for i, it := range items {
		basis := widths[i]
		// Percentage widths must be resolved against the container, not
		// against the already-resolved item width (which would halve them a
		// second time inside block()).
		if it.Style != nil && it.Style.Width.Set && it.Style.Width.U == UnitPercent {
			basis = availW
		}
		var h int
		if l.isBlock(it) {
			h = l.block(it, xx, y, maxInt(1, basis))
		} else {
			h = l.inlineFlow([]*Node{it}, xx, y, maxInt(1, basis), n.Style)
		}
		if h > maxH {
			maxH = h
		}
		xx += widths[i] + gap
	}
	return maxH
}

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
func (l *layouter) flow(parent *Node, x, y, width int) int {
	if l.outOfTime() {
		return 0
	}
	cur := y
	var inline []*Node
	flush := func() {
		if len(inline) > 0 {
			cur += l.inlineFlow(inline, x, cur, width, parent.Style)
			inline = nil
		}
	}
	for _, c := range parent.Children {
		if l.truncated || cur > maxLayoutRows {
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
			if l.isBlock(c) {
				flush()
				cur += l.block(c, x, cur, width)
				continue
			}
		}
		inline = append(inline, c)
	}
	flush()
	if cur < y {
		return 0
	}
	return cur - y
}

type inlineItemKind int

const (
	itemWord inlineItemKind = iota
	itemSpace
	itemAtomic
)

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
	maxW       int
	imageCapW  int
	base       *Style
	items      []inlineItem
	curW       int
	space      bool
	spaceStyle *Style
	spaceNode  *Node
}

func (l *layouter) inlineFlow(nodes []*Node, x, y, width int, base *Style) int {
	if l.outOfTime() {
		return 0
	}
	if l.truncated || y > maxLayoutRows {
		l.truncated = true
		return 0
	}
	if base == nil {
		base = defaultStyle()
	}
	c := &inlineCtx{l: l, x: x, y: y, maxW: width, imageCapW: l.imageCapWidth(), base: base}
	if width < 1 {
		c.maxW = 1
	}
	if indent := base.TextIndent.resolveH(width, base.FontSize); indent > 0 {
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
		label := strings.TrimSpace(n.Attr("aria-label"))
		if label == "" {
			if t := n.FindFirst("title"); t != nil {
				label = strings.TrimSpace(t.TextContent())
			}
		}
		if label == "" {
			label = strings.TrimSpace(visibleText(n))
		}
		if label == "" || strings.EqualFold(n.Attr("aria-hidden"), "true") {
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
	for _, ch := range n.Children {
		c.collect(ch)
	}
}

// lineH returns the line box height for st, including large-glyph scaling.
func (l *layouter) lineH(st *Style) int {
	if st == nil {
		return 1
	}
	h := st.LineHeightCells()
	if l.bigText {
		if s := st.TextScale(); s > h {
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
	return st.TextScale()
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
		if c.curW+rw > c.maxW && c.curW > 0 {
			c.flush(false)
		}
		c.appendRune(r, st, node)
		c.curW += rw
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
}

func (c *inlineCtx) addWord(w string, st *Style, node *Node) {
	if w == "" {
		return
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
		if c.curW > 0 {
			if !nowrap && c.curW+spaceW+width > c.maxW {
				c.flush(false)
			} else {
				c.items = append(c.items, inlineItem{kind: itemSpace, w: spaceW, style: spaceStyle, node: spaceNode})
				c.curW += spaceW
			}
		}
		c.space = false
		c.spaceStyle, c.spaceNode = nil, nil
	}
	if width > c.maxW && !nowrap {
		for _, r := range w {
			rw := c.charW(r, st)
			if rw == 0 {
				continue
			}
			if c.curW+rw > c.maxW && c.curW > 0 {
				c.flush(false)
			}
			c.appendRune(r, st, node)
			c.curW += rw
		}
		return
	}
	if !nowrap && c.curW > 0 && c.curW+width > c.maxW {
		c.flush(false)
	}
	c.items = append(c.items, inlineItem{kind: itemWord, text: w, style: st, node: node, w: width})
	c.curW += width
}

func (c *inlineCtx) addLabel(label string, st *Style, node *Node) {
	if label == "" {
		return
	}
	if w := stringWidth(label); w > c.maxW && c.maxW > 0 {
		label = truncateWidth(label, c.maxW)
	}
	w := stringWidth(label)
	if w < 1 {
		w = 1
	}
	c.addAtomicBox(inlineItem{
		kind: itemAtomic, w: w, h: 1, style: st, node: node,
		paint: func(x, y int) {
			c.l.addText(x, y, label, st, node)
		},
	})
}

func (c *inlineCtx) addForm(n *Node, st *Style) {
	if n.Tag == "textarea" {
		c.addTextarea(n, st)
		return
	}
	if n.Tag == "select" && n.HasAttr("multiple") {
		c.addMultiSelect(n, st)
		return
	}
	label := formLabel(n)
	if label == "" {
		return
	}
	if w := stringWidth(label); w > c.maxW && c.maxW > 0 {
		label = truncateWidth(label, c.maxW)
	}
	w := stringWidth(label)
	if w < 1 {
		w = 1
	}
	editable := isTextInput(n)
	// A text-like control stretches its widget to an explicit CSS width, or to
	// the width allocated by a flex row (used by the home search box).
	if editable {
		target := 0
		switch {
		case st.FlexGrow > 0:
			target = c.maxW
		case st.Width.Set && !st.Width.IsAuto():
			target = st.Width.resolveH(c.maxW, st.FontSize)
		}
		if target > w {
			w = minInt(target, c.maxW)
			if strings.HasSuffix(label, "]") {
				pad := w - stringWidth(label)
				label = label[:len(label)-1] + strings.Repeat(" ", pad) + "]"
			}
		}
	}
	disabled := isDisabledControl(n)
	readOnly := isReadOnlyControl(n)
	c.addAtomicBox(inlineItem{
		kind: itemAtomic, w: w, h: 1, style: st, node: n,
		paint: func(x, y int) {
			bg := RGB(0xea, 0xee, 0xf2)
			fg := RGB(0x1f, 0x23, 0x28)
			paintStyle := st
			cl := *st
			cl.Bold = true
			switch {
			case disabled:
				bg = RGB(0xdc, 0xe0, 0xe4)
				fg = RGB(0x8a, 0x93, 0x9d)
			case readOnly:
				bg = RGB(0xf1, 0xf3, 0xf5)
				fg = RGB(0x57, 0x60, 0x6a)
			case n.InputFocused && isRangeInput(n):
				bg = RGB(0x2f, 0x6f, 0xbf)
				fg = RGB(0xff, 0xff, 0xff)
			case editable:
				if n.InputFocused {
					bg = RGB(0x2f, 0x6f, 0xbf)
					fg = RGB(0xff, 0xff, 0xff)
				} else {
					bg = RGB(0xd8, 0xe7, 0xf8)
					fg = RGB(0x1f, 0x23, 0x28)
				}
			}
			if !disabled && !readOnly && strings.EqualFold(n.Attr("type"), "color") {
				if swatch, ok := parseColor(inputValue(n), Color{}); ok && swatch.Set && swatch.A > 0 {
					bg = swatch
					fg = readableFG(RGB(0x1f, 0x23, 0x28), bg)
					if n.InputFocused {
						cl.Bold = true
					}
				}
			}
			cl.Color = fg
			cl.Background = bg
			paintStyle = &cl
			c.l.addFill(x, y, w, 1, bg)
			c.l.addText(x, y, label, paintStyle, n)
			if col, ok := inputCursorOffset(n, label); ok {
				c.l.setInputCursor(x+2+col, y)
			}
		},
	})
}

// addMultiSelect renders a <select multiple> as an always-visible list box.
// Every option occupies its own row and keeps the option node, so a click can
// toggle precisely that option rather than cycling the whole control.
func (c *inlineCtx) addMultiSelect(n *Node, st *Style) {
	opts := selectOptions(n)
	if len(opts) == 0 {
		c.addLabel("[ multi-select ]", st, n)
		return
	}
	labels := make([]string, len(opts))
	boxW := 0
	for i, opt := range opts {
		mark := "[ ] "
		if opt.HasAttr("selected") {
			mark = "[x] "
		}
		labels[i] = mark + optionLabel(opt)
		if w := stringWidth(labels[i]); w > boxW {
			boxW = w
		}
	}
	if c.maxW > 0 && boxW > c.maxW {
		boxW = c.maxW
	}
	if boxW < 10 {
		boxW = 10
		if c.maxW > 0 && boxW > c.maxW {
			boxW = c.maxW
		}
	}
	if boxW < 1 {
		boxW = 1
	}
	for i := range labels {
		labels[i] = truncateWidth(labels[i], boxW)
	}
	c.addAtomicBox(inlineItem{
		kind: itemAtomic, w: boxW, h: len(opts), style: st, node: n,
		paint: func(x, y int) {
			for i, opt := range opts {
				bg := RGB(0xea, 0xee, 0xf2)
				fg := RGB(0x1f, 0x23, 0x28)
				rowStyle := st
				if opt.HasAttr("selected") {
					bg = RGB(0xd8, 0xe7, 0xf8)
					cl := *st
					cl.Color = fg
					rowStyle = &cl
				}
				c.l.addFill(x, y+i, boxW, 1, bg)
				c.l.addText(x, y+i, labels[i], rowStyle, opt)
			}
		},
	})
}

// addTextarea renders a textarea as a multi-row editable widget. Its first row
// uses the same bracketed style as a text input; the extra rows make the
// control visibly taller than a single-line field.
func (c *inlineCtx) addTextarea(n *Node, st *Style) {
	label := formLabel(n)
	if label == "" {
		return
	}
	boxW := stringWidth(label)
	if c.maxW > 0 && boxW > c.maxW {
		boxW = c.maxW
	}
	if boxW < 8 {
		boxW = 8
	}
	rows, cursorRow, cursorCol, hasCursor := textareaLayoutFor(n, boxW)
	editable := isTextInput(n)
	c.addAtomicBox(inlineItem{
		kind: itemAtomic, w: boxW, h: len(rows), style: st, node: n,
		paint: func(x, y int) {
			bg := RGB(0xea, 0xee, 0xf2)
			paintStyle := st
			if editable {
				cl := *st
				cl.Bold = true
				if n.InputFocused {
					bg = RGB(0x2f, 0x6f, 0xbf)
					cl.Color = RGB(0xff, 0xff, 0xff)
				} else {
					bg = RGB(0xd8, 0xe7, 0xf8)
					cl.Color = RGB(0x1f, 0x23, 0x28)
				}
				cl.Background = bg
				paintStyle = &cl
			}
			for i, row := range rows {
				c.l.addFill(x, y+i, boxW, 1, bg)
				c.l.addText(x, y+i, row, paintStyle, n)
			}
			if hasCursor && n.InputFocused {
				c.l.setInputCursor(x+2+cursorCol, y+cursorRow)
			}
		},
	})
}

// textareaLayoutFor formats the visible rows of a textarea and the focused
// caret cell within the box.
func textareaLayoutFor(n *Node, boxW int) (rows []string, cursorRow, cursorCol int, hasCursor bool) {
	inner := boxW - 3
	if inner < 1 {
		inner = 1
	}
	lines, cursorRow, cursorCol, hasCursor := textareaLinesCursor(n, inner)
	h := len(lines)
	if rows := textareaRows(n); rows > h {
		h = rows
	}
	if h < 2 {
		h = 2
	}
	if h > 12 {
		h = 12
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	if cursorRow >= h {
		hasCursor = false
	}
	out := make([]string, h)
	for i, line := range lines {
		out[i] = "[ " + padWidth(truncateWidth(line, inner), inner) + "]"
	}
	if cursorCol >= inner {
		cursorCol = inner - 1
	}
	if cursorCol < 0 {
		cursorCol = 0
	}
	return out, cursorRow, cursorCol, hasCursor
}

// textareaLines splits a textarea value into display rows, wrapping long lines
// to width. Only the rows that can be displayed are produced, so typing into a
// large textarea stays cheap.
func textareaLines(n *Node, width int) []string {
	lines, _, _, _ := textareaLinesCursor(n, width)
	return lines
}

// textareaLinesCursor is textareaLines plus the caret's row and cell column.
func textareaLinesCursor(n *Node, width int) (out []string, cursorRow, cursorCol int, hasCursor bool) {
	txt := inputValue(n)
	cur := n.InputCursor
	hasCursor = n.InputFocused
	if txt == "" && !n.InputFocused {
		txt = n.Attr("placeholder")
		hasCursor = false
	}
	txt, cur = normalizeTextCursor(txt, cur)
	if width < 1 {
		width = 1
	}
	// The widget never shows more than 12 rows; stop scanning beyond that.
	txt = truncateWidth(txt, width*12+12)
	if hasCursor {
		if cur > len(txt) {
			hasCursor = false
		} else {
			cur = len(expandTabs(txt[:cur]))
		}
	}
	txt = expandTabs(txt)
	var b strings.Builder
	w := 0
	row := 0
	found := false
	flush := func() {
		out = append(out, b.String())
		b.Reset()
		w = 0
		row = len(out)
	}
	for i, r := range txt {
		if len(out) >= 12 {
			break
		}
		atCursor := hasCursor && !found && i == cur
		if atCursor {
			cursorRow, cursorCol = row, w
			found = true
		}
		if r == '\n' {
			flush()
			continue
		}
		rw := runeWidth(r)
		if rw == 0 {
			b.WriteRune(r)
			continue
		}
		if w+rw > width && w > 0 {
			flush()
			if atCursor {
				cursorRow, cursorCol = row, w
			}
		}
		b.WriteRune(r)
		w += rw
	}
	if len(out) < 12 {
		out = append(out, b.String())
	}
	if hasCursor && !found {
		if cur <= len(txt) && row < len(out) {
			cursorRow, cursorCol = row, w
			found = true
		}
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out, cursorRow, cursorCol, hasCursor && found
}

// normalizeTextCursor normalises CRLF/CR newlines while keeping the caret at
// the same logical position.
func normalizeTextCursor(s string, cur int) (string, int) {
	if cur < 0 {
		cur = 0
	}
	if cur > len(s) {
		cur = len(s)
	}
	before := strings.ReplaceAll(strings.ReplaceAll(s[:cur], "\r\n", "\n"), "\r", "\n")
	full := strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	return full, len(before)
}

// textareaRows reads the HTML rows attribute, clamped to the supported range.
func textareaRows(n *Node) int {
	rows, err := strconv.Atoi(strings.TrimSpace(n.Attr("rows")))
	if err != nil || rows < 2 {
		return 0
	}
	if rows > 12 {
		rows = 12
	}
	return rows
}

// selectOptions returns every option below a select, including those nested in
// optgroup elements.
func selectOptions(n *Node) []*Node {
	if n == nil {
		return nil
	}
	return n.FindAll(func(x *Node) bool { return x.Tag == "option" })
}

// optionLabel is the visible text of an option, falling back to its value.
func optionLabel(opt *Node) string {
	if opt == nil {
		return ""
	}
	label := strings.TrimSpace(opt.TextContent())
	if label == "" {
		label = strings.TrimSpace(opt.Attr("value"))
	}
	return label
}

// optionValue is the value submitted for an option. Options without a value
// use their visible text, matching HTML.
func optionValue(opt *Node) string {
	if opt == nil {
		return ""
	}
	if v := opt.Attr("value"); v != "" {
		return v
	}
	return strings.TrimSpace(opt.TextContent())
}

// selectedOptionValues returns the submitted values of a multi-select in
// document order.
func selectedOptionValues(n *Node) []string {
	var values []string
	for _, opt := range selectOptions(n) {
		if opt.HasAttr("selected") && !isDisabledControl(opt) {
			values = append(values, optionValue(opt))
		}
	}
	return values
}

// rangeParams resolves the min/max/step/value of a range input using the HTML
// defaults.
func rangeParams(n *Node) (min, max, step, value float64) {
	min, max, step, value = 0, 100, 1, 0
	if v, err := strconv.ParseFloat(strings.TrimSpace(n.Attr("min")), 64); err == nil {
		min = v
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(n.Attr("max")), 64); err == nil {
		max = v
	}
	if max < min {
		max = min
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(n.Attr("step")), 64); err == nil && v > 0 {
		step = v
	}
	value = min + (max-min)/2
	if n.ValueSet {
		if v, err := strconv.ParseFloat(strings.TrimSpace(n.Value), 64); err == nil {
			value = v
		}
	} else if v, err := strconv.ParseFloat(strings.TrimSpace(n.Attr("value")), 64); err == nil {
		value = v
	}
	if value < min {
		value = min
	}
	if value > max {
		value = max
	}
	return min, max, step, value
}

func formatRangeNumber(v float64) string {
	if math.Abs(v-math.Round(v)) < 1e-9 {
		return strconv.FormatInt(int64(math.Round(v)), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func rangeValueString(n *Node) string {
	_, _, _, value := rangeParams(n)
	return formatRangeNumber(value)
}

// rangeLabel draws a compact slider with the current value.
func rangeLabel(n *Node) string {
	min, max, _, value := rangeParams(n)
	const barW = 16
	pos := 0
	if max > min {
		pos = int(math.Round((value - min) / (max - min) * float64(barW-1)))
	}
	if pos < 0 {
		pos = 0
	}
	if pos >= barW {
		pos = barW - 1
	}
	var bar strings.Builder
	for i := 0; i < barW; i++ {
		if i <= pos {
			bar.WriteRune('█')
		} else {
			bar.WriteRune('░')
		}
	}
	return "[ - " + bar.String() + " " + rangeValueString(n) + " + ]"
}

// inputValue returns the current editable value of a form control.
func inputValue(n *Node) string {
	if n == nil {
		return ""
	}
	if n.ValueSet {
		return n.Value
	}
	if n.Tag == "select" {
		var first string
		for _, opt := range selectOptions(n) {
			if isDisabledControl(opt) {
				continue
			}
			val := optionValue(opt)
			if first == "" {
				first = val
			}
			if opt.HasAttr("selected") {
				return val
			}
		}
		return first
	}
	if n.Tag == "textarea" {
		return strings.TrimSpace(n.TextContent())
	}
	if n.Tag == "input" {
		switch strings.ToLower(n.Attr("type")) {
		case "range":
			return rangeValueString(n)
		case "color":
			if v := n.Attr("value"); v != "" {
				return v
			}
			return "#000000"
		}
	}
	return n.Attr("value")
}

// inputDisplayCells is the widest text window shown inside a focused input.
const inputDisplayCells = 40

// inputTextDisplay returns the text shown inside a text-like input and the
// cell column of the caret within that text. A focused value scrolls so the
// caret stays visible; unfocused values simply truncate at the right edge.
func inputTextDisplay(n *Node, typ string) (string, int, bool) {
	val := inputValue(n)
	if !n.InputFocused {
		if val == "" {
			val = n.Attr("placeholder")
		}
		if typ == "password" {
			val = strings.Repeat("•", utf8.RuneCountInString(val))
		}
		if val == "" {
			val = strings.Repeat(" ", 10)
		}
		if w := stringWidth(val); w > inputDisplayCells {
			val = truncateWidth(val, inputDisplayCells)
		}
		return val, 0, false
	}
	cur := n.InputCursor
	if cur < 0 {
		cur = 0
	}
	if cur > len(val) {
		cur = len(val)
	}
	if typ == "password" {
		before := utf8.RuneCountInString(val[:cur])
		val = strings.Repeat("•", utf8.RuneCountInString(val))
		cur = len(strings.Repeat("•", before))
	}
	runes := []rune(val)
	curRune := utf8.RuneCountInString(val[:cur])
	if curRune > len(runes) {
		curRune = len(runes)
	}
	// Leave one cell for the caret itself and one for a leading ellipsis.
	limit := inputDisplayCells - 1
	start := curRune
	used := 0
	for start > 0 {
		rw := runeWidth(runes[start-1])
		if used+rw > limit {
			break
		}
		used += rw
		start--
	}
	used = 0
	for i := start; i < curRune; i++ {
		used += runeWidth(runes[i])
	}
	end := curRune
	for end < len(runes) {
		rw := runeWidth(runes[end])
		if used+rw > limit {
			break
		}
		used += rw
		end++
	}
	display := string(runes[start:end])
	cursorCol := stringWidth(string(runes[start:curRune]))
	if start > 0 {
		display = "…" + display
		cursorCol++
	}
	return display, cursorCol, true
}

// inputCursorOffset returns the caret column within a formatted input label.
func inputCursorOffset(n *Node, label string) (int, bool) {
	if n == nil || n.Tag != "input" || !n.InputFocused {
		return 0, false
	}
	typ := strings.ToLower(n.Attr("type"))
	switch typ {
	case "hidden", "checkbox", "radio", "submit", "button", "reset", "file", "image", "range":
		return 0, false
	}
	_, col, ok := inputTextDisplay(n, typ)
	if !ok {
		return 0, false
	}
	if max := stringWidth(label) - 4; col > max {
		col = max
	}
	if col < 0 {
		col = 0
	}
	return col, true
}

func formLabel(n *Node) string {
	switch n.Tag {
	case "input":
		typ := strings.ToLower(n.Attr("type"))
		val := inputValue(n)
		switch typ {
		case "hidden":
			return ""
		case "checkbox":
			if n.HasAttr("checked") {
				return "[x]"
			}
			return "[ ]"
		case "radio":
			if n.HasAttr("checked") {
				return "(x)"
			}
			return "( )"
		case "submit", "button", "reset":
			if val == "" {
				if typ == "reset" {
					val = "Reset"
				} else {
					val = "Submit"
				}
			}
			return "[ " + val + " ]"
		case "file":
			return "[ Choose File ]"
		case "image":
			return "[ image button ]"
		case "range":
			return rangeLabel(n)
		case "password":
			display, _, _ := inputTextDisplay(n, typ)
			minW := 16
			if w := stringWidth(display); w < minW {
				display += strings.Repeat(" ", minW-w)
			}
			return "[ " + display + " ]"
		default:
			display, _, _ := inputTextDisplay(n, typ)
			minW := 16
			if typ == "search" {
				minW = 30
			}
			if w := stringWidth(display); w < minW {
				display += strings.Repeat(" ", minW-w)
			}
			return "[ " + display + " ]"
		}
	case "select":
		var chosen string
		for _, opt := range selectOptions(n) {
			if isDisabledControl(opt) {
				continue
			}
			if chosen == "" {
				chosen = strings.TrimSpace(opt.TextContent())
			}
			if opt.HasAttr("selected") {
				chosen = strings.TrimSpace(opt.TextContent())
				break
			}
		}
		if chosen == "" {
			chosen = "select"
		}
		return "[ " + chosen + " ▾ ]"
	case "textarea":
		txt := truncateWidth(inputValue(n), 80)
		if txt == "" && !n.InputFocused {
			txt = n.Attr("placeholder")
		}
		if i := strings.IndexByte(txt, '\n'); i >= 0 {
			txt = txt[:i]
		}
		if txt == "" {
			txt = strings.Repeat(" ", 10)
		}
		if w := stringWidth(txt); w < 30 {
			txt += strings.Repeat(" ", 30-w)
		}
		if w := stringWidth(txt); w > 40 {
			txt = truncateWidth(txt, 40)
		}
		return "[ " + txt + " ]"
	case "button":
		txt := strings.TrimSpace(visibleText(n))
		if txt == "" {
			txt = "Button"
		}
		return "[ " + txt + " ]"
	}
	return ""
}

// visibleText concatenates rendered text below n, skipping hidden elements.
// Form controls (notably buttons) use this instead of TextContent so CSS or
// script text cannot leak into their labels.
func visibleText(n *Node) string {
	var b strings.Builder
	var walk func(*Node)
	walk = func(x *Node) {
		for _, c := range x.Children {
			if c.Type == TextNode {
				b.WriteString(c.Text)
				continue
			}
			if c.Type != ElementNode {
				continue
			}
			if c.Style != nil && c.Style.Display == "none" {
				continue
			}
			switch c.Tag {
			case "script", "style", "noscript", "template", "head", "title", "meta", "link":
				continue
			}
			walk(c)
		}
	}
	if n != nil {
		walk(n)
	}
	return b.String()
}

func (c *inlineCtx) addImage(n *Node, st *Style) {
	if n.Img == nil {
		alt := n.Attr("alt")
		if alt == "" {
			// Decorative broken image: render nothing instead of
			// "[image: image]" noise.
			return
		}
		c.addLabel("[image: "+alt+"]", st, n)
		return
	}
	w, h := imageSize(n, st, c.maxW, c.imageCapW)
	w2, h2 := w, h
	c.addAtomicBox(inlineItem{
		kind: itemAtomic, w: w, h: h, style: st, node: n,
		paint: func(x, y int) {
			c.l.frags = append(c.l.frags, Frag{Kind: fragImage, X: x, Y: y, W: w2, H: h2, Img: n.Img, Node: n})
		},
	})
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
	if c.space && c.curW > 0 {
		spaceStyle, spaceNode := c.spaceStyle, c.spaceNode
		if spaceStyle == nil {
			spaceStyle = it.style
		}
		if !nowrap && c.curW+spaceW+it.w > c.maxW {
			c.flush(false)
		} else {
			c.items = append(c.items, inlineItem{kind: itemSpace, w: spaceW, style: spaceStyle, node: spaceNode})
			c.curW += spaceW
		}
	}
	c.space = false
	c.spaceStyle, c.spaceNode = nil, nil
	if !nowrap && c.curW > 0 && c.curW+it.w > c.maxW {
		c.flush(false)
	}
	c.items = append(c.items, it)
	c.curW += it.w
}

func (c *inlineCtx) flushIfAny() {
	if len(c.items) > 0 {
		c.flush(true)
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
	shift := 0
	switch c.base.TextAlign {
	case "center":
		shift = (c.maxW - c.curW) / 2
	case "right":
		shift = c.maxW - c.curW
	}
	if shift < 0 {
		shift = 0
	}
	perSpace, remainder := 0, 0
	if c.base.TextAlign == "justify" && !last && c.curW < c.maxW {
		spaces := 0
		for i := range c.items {
			if c.items[i].kind == itemSpace && c.items[i].w > 0 {
				spaces++
			}
		}
		if spaces > 0 {
			extra := c.maxW - c.curW
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
				yy := c.y + (lineH-runH)/2
				spaces := strings.Repeat(" ", cellW)
				if s := c.l.scale(it.style); s > 1 {
					spaces = strings.Repeat(" ", maxInt(1, cellW/s))
				}
				c.l.frags = append(c.l.frags, Frag{
					Kind: fragText, X: c.x + shift + pos, Y: yy, W: cellW, H: runH,
					Text: spaces, Style: it.style, Node: it.node,
				})
				pos += cellW
				continue
			}
			pos += it.w
		case itemWord:
			runH := c.l.lineH(it.style)
			yy := c.y + (lineH-runH)/2
			c.l.frags = append(c.l.frags, Frag{
				Kind: fragText, X: c.x + shift + pos, Y: yy, W: it.w, H: runH,
				Text: it.text, Style: it.style, Node: it.node,
			})
			pos += it.w
		case itemAtomic:
			yy := c.y + (lineH-it.h)/2
			it.paint(c.x+shift+pos, yy)
			pos += it.w
		}
	}
	c.y += lineH
	c.items = c.items[:0]
	c.curW = 0
	c.space = false
	c.spaceStyle, c.spaceNode = nil, nil
}

const maxImageRows = 24

func (l *layouter) imageCapWidth() int {
	w := l.width * 3 / 5
	if w < 1 {
		w = 1
	}
	return w
}

// imageSize returns the terminal cell size for an image, preserving its aspect
// ratio and applying the terminal-friendly display cap.
func imageSize(n *Node, st *Style, maxW, capW int) (w, h int) {
	iw, ih := 0, 0
	if n.Img != nil {
		b := n.Img.Bounds()
		iw, ih = b.Dx(), b.Dy()
	}
	cw, ch := 0, 0
	if st.Width.Set && !st.Width.IsAuto() {
		cw = st.Width.resolveH(maxW, st.FontSize)
	}
	if st.Height.Set && st.Height.U != UnitPercent {
		ch = st.Height.resolveV(maxW, st.FontSize)
	}
	if iw > 0 && ih > 0 {
		switch {
		case cw <= 0 && ch <= 0:
			cw = int(math.Round(float64(iw) / cellW))
			ch = int(math.Round(float64(ih) / cellH))
		case cw <= 0:
			cw = int(math.Round(float64(ch) * cellH * float64(iw) / float64(ih) / cellW))
		case ch <= 0:
			ch = int(math.Round(float64(cw) * cellW * float64(ih) / float64(iw) / cellH))
		}
	} else {
		if cw <= 0 {
			cw = 16
		}
		if ch <= 0 {
			ch = 1
		}
		ch = 1
	}
	if cw < 1 {
		cw = 1
	}
	if ch < 1 {
		ch = 1
	}
	if maxW > 0 && cw > maxW {
		ch = int(math.Round(float64(ch) * float64(maxW) / float64(cw)))
		cw = maxW
		if ch < 1 {
			ch = 1
		}
	}
	if capW > 0 && cw > capW {
		ch = int(math.Round(float64(ch) * float64(capW) / float64(cw)))
		cw = capW
		if ch < 1 {
			ch = 1
		}
	}
	if ch > maxImageRows {
		cw = int(math.Round(float64(cw) * float64(maxImageRows) / float64(ch)))
		ch = maxImageRows
		if cw < 1 {
			cw = 1
		}
	}
	return cw, ch
}

// table lays out a display:table subtree with collapsed one-cell grid borders.
func (l *layouter) table(n *Node, x, y, availW int) int {
	if l.outOfTime() {
		return 0
	}
	if l.truncated || y > maxLayoutRows {
		l.truncated = true
		return 0
	}
	st := n.Style
	if st == nil {
		return 0
	}
	startY := y
	if cap := childByTag(n, "caption"); cap != nil {
		y += l.block(cap, x, y, availW)
	}
	type cell struct {
		node *Node
		col  int
		span int
	}
	type row struct {
		cells []cell
	}
	var rows []row
	var collect func(*Node)
	collect = func(parent *Node) {
		for _, c := range parent.Children {
			if c.Type != ElementNode || c.Style == nil || c.Style.Display == "none" {
				continue
			}
			if isOutOfFlow(c.Style) {
				l.deferAbsolute(c, x, y)
				continue
			}
			if c.Tag == "tr" {
				r := row{}
				col := 0
				for _, cc := range c.Children {
					if cc.Type != ElementNode || cc.Style == nil || cc.Style.Display == "none" {
						continue
					}
					if cc.Tag == "td" || cc.Tag == "th" || cc.Style.Display == "table-cell" {
						span := 1
						if v := cc.Attr("colspan"); v != "" {
							if num, err := strconv.Atoi(v); err == nil && num > 0 {
								span = num
								if span > 20 {
									span = 20
								}
							}
						}
						r.cells = append(r.cells, cell{node: cc, col: col, span: span})
						col += span
					}
				}
				rows = append(rows, r)
				continue
			}
			if c.Tag == "table" {
				continue
			}
			collect(c)
		}
	}
	collect(n)
	ncols := 0
	for _, r := range rows {
		for _, c := range r.cells {
			if c.col+c.span > ncols {
				ncols = c.col + c.span
			}
		}
	}
	if ncols == 0 || len(rows) == 0 {
		// Fall back to normal block flow for empty tables.
		return l.flow(n, x, y, availW)
	}
	colW := make([]int, ncols)
	for _, r := range rows {
		for _, c := range r.cells {
			pref, _ := l.measure(c.node)
			need := pref
			per := (need + c.span - 1) / c.span
			for k := 0; k < c.span && c.col+k < ncols; k++ {
				if per > colW[c.col+k] {
					colW[c.col+k] = per
				}
			}
		}
	}
	for i := range colW {
		if colW[i] < 3 {
			colW[i] = 3
		}
	}
	total := ncols + 1
	for _, w := range colW {
		total += w
	}
	if total > availW {
		avail := availW - (ncols + 1)
		if avail < ncols {
			avail = ncols
		}
		sum := 0
		for _, w := range colW {
			sum += w
		}
		if sum > 0 {
			for i := range colW {
				colW[i] = int(math.Floor(float64(colW[i]) * float64(avail) / float64(sum)))
				if colW[i] < 1 {
					colW[i] = 1
				}
			}
		}
	}
	tableW := ncols + 1
	for _, w := range colW {
		tableW += w
	}
	colX := make([]int, ncols)
	cx := x + 1
	for i := 0; i < ncols; i++ {
		colX[i] = cx
		cx += colW[i] + 1
	}
	bc := st.BorderColor
	if !bc.Set {
		bc = RGB(0xd0, 0xd7, 0xde)
	}
	curY := y
	for _, r := range rows {
		if len(r.cells) == 0 {
			continue
		}
		topY := curY
		rowY := topY + 1
		rowH := 1
		for _, c := range r.cells {
			cw := c.span - 1
			for k := 0; k < c.span && c.col+k < ncols; k++ {
				cw += colW[c.col+k]
			}
			if c.col >= ncols {
				continue
			}
			orig := c.node.Style
			cs := *orig
			cs.Border = [4]Length{}
			c.node.Style = &cs
			h := l.block(c.node, colX[c.col], rowY, cw)
			c.node.Style = orig
			if h > rowH {
				rowH = h
			}
		}
		l.addFill(x, topY, tableW, 1, bc)
		l.addFill(x, rowY+rowH, tableW, 1, bc)
		l.addFill(x, topY, 1, rowH+2, bc)
		for i := 0; i < ncols; i++ {
			l.addFill(colX[i]+colW[i], topY, 1, rowH+2, bc)
		}
		curY = rowY + rowH
	}
	tableH := curY + 1 - y
	return (y - startY) + tableH
}

func childByTag(n *Node, tag string) *Node {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.Type == ElementNode && c.Tag == tag {
			return c
		}
	}
	return nil
}

func (l *layouter) paddingH(n *Node) (int, int) {
	if n == nil || n.Style == nil {
		return 0, 0
	}
	st := n.Style
	pl := nonNeg(st.Padding[3].resolveH(l.width, st.FontSize))
	pr := nonNeg(st.Padding[1].resolveH(l.width, st.FontSize))
	bl := resolveBorderH(st.Border[3], l.width, st.FontSize)
	br := resolveBorderH(st.Border[1], l.width, st.FontSize)
	return pl + pr + bl + br, 0
}

// measure estimates the max-content and min-content widths (in cells) of a
// subtree. It is used to size table columns.
func (l *layouter) measure(n *Node) (maxW, minW int) {
	if n == nil {
		return 0, 0
	}
	if n.Type == TextNode {
		if n.Style == nil {
			return 0, 0
		}
		s := expandTabs(n.Text)
		s = applyTextTransform(s, n.Style.TextTransform)
		sc := l.scale(n.Style)
		for _, line := range strings.Split(s, "\n") {
			lineMax := 0
			word := 0
			wordMax := 0
			for _, r := range line {
				if r == ' ' || r == '\t' || r == '\r' || r == '\f' {
					if word > wordMax {
						wordMax = word
					}
					word = 0
					lineMax += sc
					continue
				}
				rw := runeWidth(r) * sc
				lineMax += rw
				word += rw
			}
			if word > wordMax {
				wordMax = word
			}
			if lineMax > maxW {
				maxW = lineMax
			}
			if wordMax > minW {
				minW = wordMax
			}
		}
		return maxW, minW
	}
	if n.Type != ElementNode || n.Style == nil || n.Style.Display == "none" {
		return 0, 0
	}
	st := n.Style
	if n.Tag == "img" {
		cw, _ := imageSize(n, st, 1000, l.imageCapWidth())
		return cw, cw
	}
	if n.Tag == "select" && n.HasAttr("multiple") {
		opts := selectOptions(n)
		w := 10
		for _, opt := range opts {
			mark := "[ ] "
			if opt.HasAttr("selected") {
				mark = "[x] "
			}
			if ow := stringWidth(mark + optionLabel(opt)); ow > w {
				w = ow
			}
		}
		return w, w
	}
	if n.Tag == "input" || n.Tag == "select" || n.Tag == "textarea" || n.Tag == "button" {
		w := stringWidth(formLabel(n))
		if w < 1 {
			w = 1
		}
		return w, w
	}
	pad, _ := l.paddingH(n)
	isBlock := false
	switch st.Display {
	case "block", "grid", "list-item", "table":
		isBlock = true
	}
	if isBlock {
		for _, c := range n.Children {
			mw, mi := l.measure(c)
			if mw > maxW {
				maxW = mw
			}
			if mi > minW {
				minW = mi
			}
		}
		return maxW + pad, minW + pad
	}
	// inline: content is laid out left to right, so max widths add up while
	// the minimum width is the widest unbreakable piece.
	total := 0
	space := false
	for _, c := range n.Children {
		if c.Type == TextNode && c.Style != nil {
			s := expandTabs(c.Text)
			s = applyTextTransform(s, c.Style.TextTransform)
			sc := l.scale(c.Style)
			partMax, partMin := 0, 0
			for _, line := range strings.Split(s, "\n") {
				lineMax := 0
				word := 0
				for _, r := range line {
					if r == ' ' || r == '\t' || r == '\r' || r == '\f' {
						if word > partMin {
							partMin = word
						}
						word = 0
						lineMax += sc
						continue
					}
					rw := runeWidth(r) * sc
					lineMax += rw
					word += rw
				}
				if word > partMin {
					partMin = word
				}
				if lineMax > partMax {
					partMax = lineMax
				}
			}
			total += partMax
			if space {
				total += sc
			}
			if partMin > minW {
				minW = partMin
			}
			space = true
			continue
		}
		mw, mi := l.measure(c)
		if space && (mw > 0 || mi > 0) {
			total++
		}
		total += mw
		if mi > minW {
			minW = mi
		}
		space = true
	}
	if total > maxW {
		maxW = total
	}
	return maxW + pad, minW + pad
}

// SetFixedScroll repositions viewport-anchored fragments for the current
// scroll offset. Fixed fragments are stored relative to the viewport and then
// shifted into document coordinates so painting, hit testing, find and
// selection can keep using one coordinate space.
func (ly *Layout) SetFixedScroll(scroll int) {
	if ly.fixedApplied && ly.fixedScroll == scroll {
		return
	}
	changed := false
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Fixed {
			f.Y = f.FixedY + scroll
			changed = true
		}
	}
	ly.fixedScroll = scroll
	ly.fixedApplied = true
	if changed {
		ly.rebuildLinks()
	}
}

// rebuildLinks recreates link rectangles after fixed fragments move.
func (ly *Layout) rebuildLinks() {
	ly.Links = ly.Links[:0]
	for i := range ly.Frags {
		ly.Frags[i].Link = nil
	}
	ly.buildLinks()
}

// buildLinks assigns fragments to links and records link rectangles in
// document order.
func (ly *Layout) buildLinks() {
	byNode := map[*Node]*LinkInfo{}
	for i := range ly.Frags {
		f := &ly.Frags[i]
		a := nearestAnchor(f.Node)
		if a == nil {
			continue
		}
		href := a.Attr("href")
		if href == "" {
			continue
		}
		li := byNode[a]
		if li == nil {
			li = &LinkInfo{Node: a, Href: href, Index: len(ly.Links)}
			byNode[a] = li
			ly.Links = append(ly.Links, li)
		}
		f.Link = li
		if f.Kind == fragFill {
			continue
		}
		li.Rects = append(li.Rects, Rect{X: f.X, Y: f.Y, W: f.W, H: maxInt(1, f.H)})
	}
}

func nearestAnchor(n *Node) *Node {
	for p := n; p != nil; p = p.Parent {
		if p.Type == ElementNode && p.Tag == "a" && p.HasAttr("href") {
			return p
		}
	}
	return nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// runeWidth returns the number of terminal cells a rune occupies.
func runeWidth(r rune) int {
	if r == 0 || r < 32 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	if isWideRune(r) {
		return 2
	}
	return 1
}

func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115f,
		r >= 0x2329 && r <= 0x232a,
		r >= 0x2e80 && r <= 0x303e,
		r >= 0x3041 && r <= 0x33ff,
		r >= 0x3400 && r <= 0x4dbf,
		r >= 0x4e00 && r <= 0x9fff,
		r >= 0xa000 && r <= 0xa4cf,
		r >= 0xa960 && r <= 0xa97f,
		r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff,
		r >= 0xfe10 && r <= 0xfe19,
		r >= 0xfe30 && r <= 0xfe6f,
		r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6,
		r >= 0x1f000 && r <= 0x1faff,
		r >= 0x20000 && r <= 0x3fffd:
		return true
	}
	return false
}

func stringWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// truncateWidth cuts s to at most max cells.
func truncateWidth(s string, max int) string {
	if max <= 0 {
		return ""
	}
	w := 0
	for i, r := range s {
		rw := runeWidth(r)
		if w+rw > max {
			return s[:i]
		}
		w += rw
	}
	return s
}

// padWidth pads s with spaces to exactly w cells.
func padWidth(s string, w int) string {
	sw := stringWidth(s)
	if sw >= w {
		return truncateWidth(s, w)
	}
	return s + strings.Repeat(" ", w-sw)
}

// fitWidth truncates s with an ellipsis and pads it to exactly w cells.
func fitWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if stringWidth(s) <= w {
		return padWidth(s, w)
	}
	return padWidth(truncateWidth(s, w-1)+"…", w)
}

func applyTextTransform(s, mode string) string {
	switch mode {
	case "uppercase":
		return strings.ToUpper(s)
	case "lowercase":
		return strings.ToLower(s)
	case "capitalize":
		var b strings.Builder
		start := true
		for _, r := range s {
			if start && unicode.IsLetter(r) {
				r = unicode.ToUpper(r)
				start = false
			} else if unicode.IsSpace(r) {
				start = true
			}
			b.WriteRune(r)
		}
		return b.String()
	}
	return s
}

// expandTabs converts tabs to spaces using a 4-cell tab width.
func expandTabs(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		switch r {
		case '\t':
			n := 4 - col%4
			b.WriteString(strings.Repeat(" ", n))
			col += n
		case '\n':
			b.WriteRune(r)
			col = 0
		default:
			b.WriteRune(r)
			col += runeWidth(r)
		}
	}
	return b.String()
}
