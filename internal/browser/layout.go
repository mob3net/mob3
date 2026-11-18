package browser

import (
	"image"
	"math"
	"strings"
	"time"
)

type fragKind int

const (
	fragFill fragKind = iota
	fragText
	fragImage
	// fragLink is an invisible box for an empty anchor, so overlay links that
	// cover a whole card remain clickable and focusable.
	fragLink
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
	Crop  image.Rectangle
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
	// Boxes holds border-box rectangles for laid-out block elements. Hover and
	// click hit testing falls back to them when the pointer is over padding or
	// background rather than a text fragment.
	Boxes map[*Node]Rect

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

// LayoutBudget bundles the style/layout safety limits. A zero Time, Frags or
// Rows value disables that individual limit.
type LayoutBudget struct {
	Name  string
	Time  time.Duration
	Frags int
	Rows  int
}

var DefaultLayoutBudget = LayoutBudget{Name: "standard", Time: 2 * time.Second, Frags: 250000, Rows: 200000}

var layoutBudgetPresets = map[string]LayoutBudget{
	"low":      {Name: "low", Time: time.Second, Frags: 100000, Rows: 80000},
	"standard": DefaultLayoutBudget,
	"high":     {Name: "high", Time: 5 * time.Second, Frags: 1000000, Rows: 1000000},
	"none":     {Name: "none"},
}

// ParseLayoutBudget resolves a preset name. Several spellings of "none" are
// accepted so the flag is forgiving at the command line.
func ParseLayoutBudget(name string) (LayoutBudget, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "low":
		return layoutBudgetPresets["low"], true
	case "standard", "", "default":
		return layoutBudgetPresets["standard"], true
	case "high":
		return layoutBudgetPresets["high"], true
	case "none", "no-limit", "nolimit", "off", "unlimited":
		return layoutBudgetPresets["none"], true
	}
	return LayoutBudget{}, false
}

type layouter struct {
	styles    map[*Node]*Style
	cbs       map[*Node]*cbRect
	width     int
	frags     []Frag
	boxes     map[*Node]Rect
	bigText   bool
	truncated bool
	deadline  time.Time
	expired   bool
	maxFrags  int
	maxRows   int
	hideFixed bool
	// imagesOff lays decoded images out as alt labels without clearing them,
	// so graphics mode changes can hide images without reloading the page.
	imagesOff bool
	// forcedImageH is a resolved percentage height for an absolutely
	// positioned image whose containing block height is known.
	forcedImageH int

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
	// floats holds active floated boxes for the current flow. Inline lines
	// shorten around them and the containing flow reserves their height.
	floats []floatBox

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

// floatBox is one floated box in document coordinates. Floats shorten the
// line boxes that overlap their vertical range but stay out of normal flow.
type floatBox struct {
	side        string // "left" or "right"
	top, bottom int
	left, right int
}

// outOfTime reports whether the layout pass has exceeded its deadline. The
// flag is sticky so recursive callers unwind instead of starting new work.
func (l *layouter) overFragLimit() bool {
	return l.maxFrags > 0 && len(l.frags) >= l.maxFrags
}

func (l *layouter) overRowLimit(y int) bool {
	return l.maxRows > 0 && y > l.maxRows
}

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
	return buildLayout(root, author, width, 0, bigText, false, false, false, deadline, DefaultLayoutBudget)
}

// BuildLayoutViewportDeadline is BuildLayoutDeadline with the terminal
// viewport height, so position:fixed boxes can anchor to the visible area.
func BuildLayoutViewportDeadline(root *Node, author []Rule, width, viewportH int, bigText bool, deadline time.Time) *Layout {
	return buildLayout(root, author, width, viewportH, bigText, false, false, false, deadline, DefaultLayoutBudget)
}

// BuildLayoutDarkDeadline is BuildLayoutDeadline with the dark palette applied
// to page, text, border and background colors after layout.
func BuildLayoutDarkDeadline(root *Node, author []Rule, width int, bigText bool, deadline time.Time) *Layout {
	return buildLayout(root, author, width, 0, bigText, true, false, false, deadline, DefaultLayoutBudget)
}

// BuildLayoutDarkViewportDeadline is BuildLayoutViewportDeadline with the dark
// palette applied.
func BuildLayoutDarkViewportDeadline(root *Node, author []Rule, width, viewportH int, bigText bool, deadline time.Time) *Layout {
	return buildLayout(root, author, width, viewportH, bigText, true, false, false, deadline, DefaultLayoutBudget)
}

// BuildLayoutBudgetDeadline is BuildLayoutViewportDeadline with explicit
// layout limits.
func BuildLayoutBudgetDeadline(root *Node, author []Rule, width, viewportH int, bigText, dark bool, deadline time.Time, budget LayoutBudget) *Layout {
	return BuildLayoutBudgetDeadlineOptions(root, author, width, viewportH, bigText, dark, false, deadline, budget)
}

// BuildLayoutBudgetDeadlineOptions is BuildLayoutBudgetDeadline with additional
// display switches. hideFixed removes position:fixed boxes from the result.
func BuildLayoutBudgetDeadlineOptions(root *Node, author []Rule, width, viewportH int, bigText, dark, hideFixed bool, deadline time.Time, budget LayoutBudget) *Layout {
	return buildLayout(root, author, width, viewportH, bigText, dark, hideFixed, false, deadline, budget)
}

// BuildLayoutBudgetDeadlineImages is BuildLayoutBudgetDeadlineOptions with an
// imagesOff switch. When set, decoded images are laid out as their alt labels
// without reloading the page, matching a load with images disabled.
func BuildLayoutBudgetDeadlineImages(root *Node, author []Rule, width, viewportH int, bigText, dark, hideFixed, imagesOff bool, deadline time.Time, budget LayoutBudget) *Layout {
	return buildLayout(root, author, width, viewportH, bigText, dark, hideFixed, imagesOff, deadline, budget)
}

func buildLayout(root *Node, author []Rule, width, viewportH int, bigText, dark, hideFixed, imagesOff bool, deadline time.Time, budget LayoutBudget) *Layout {
	if width < 1 {
		width = 1
	}
	if budget.Time <= 0 {
		deadline = time.Time{}
	}
	engine := newStyleEngineDeadline(author, width*cellW, deadline)
	if viewportH > 0 {
		engine.viewportH = float64(viewportH) * cellH
	}
	engine.dark = dark
	styles := engine.computeAll(root)
	l := &layouter{styles: styles, width: width, bigText: bigText, hideFixed: hideFixed, imagesOff: imagesOff, deadline: deadline, maxFrags: budget.Frags, maxRows: budget.Rows, boxes: make(map[*Node]Rect), cbs: make(map[*Node]*cbRect)}
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
	explicitBG := false
	if body != nil && body.Style != nil && body.Style.Background.opaque() {
		pageBG = body.Style.Background
		explicitBG = true
	} else if html != nil && html.Style != nil && html.Style.Background.opaque() {
		pageBG = html.Style.Background
		explicitBG = true
	}
	if !explicitBG {
		// Sites sometimes paint their background on a full-page wrapper or
		// use a gradient the engine cannot parse. Use the largest opaque fill
		// covering most of the first viewport as the page background.
		if c, ok := fallbackPageBackground(l.frags, width, viewportH, height); ok {
			pageBG = c
		}
	}
	out := &Layout{
		Width: width, Height: height, Frags: l.frags, Styles: styles,
		PageBG: pageBG, Body: body, HTML: html, Truncated: l.truncated,
		Cursor: l.cursor, HasCursor: l.hasCursor, Boxes: l.boxes,
	}
	out.buildLinks()
	// A page that already renders a dark background is using its own dark
	// theme (or responded to prefers-color-scheme); keep its palette. The
	// post-layout remap remains the fallback for light pages.
	if dark && !isDarkColor(out.PageBG) {
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
	if l.overFragLimit() || l.overRowLimit(y) {
		l.truncated = true
		return
	}
	l.frags = append(l.frags, Frag{Kind: fragFill, X: x, Y: y, W: w, H: h, Color: c})
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

// usedBorderH is the horizontal border width in cells. A border whose style
// is none or hidden takes no space, even when border-width is declared.
func usedBorderH(st *Style, i, basis int) int {
	if !st.BorderStyle[i] {
		return 0
	}
	return resolveBorderH(st.Border[i], basis, st.FontSize)
}

func usedBorderV(st *Style, i, basis int) int {
	if !st.BorderStyle[i] {
		return 0
	}
	return resolveBorderV(st.Border[i], basis, st.FontSize)
}

// boxWidth returns the horizontal border and padding of a box in cells.
func (l *layouter) boxWidth(st *Style, basis int) int {
	return usedBorderH(st, 3, basis) +
		usedBorderH(st, 1, basis) +
		nonNeg(st.Padding[3].resolveH(basis, st.FontSize)) +
		nonNeg(st.Padding[1].resolveH(basis, st.FontSize))
}

// boxHeight returns the vertical border and padding of a box in rows.
func (l *layouter) boxHeight(st *Style, basis int) int {
	return usedBorderV(st, 0, basis) +
		usedBorderV(st, 2, basis) +
		nonNeg(st.Padding[0].resolveV(basis, st.FontSize)) +
		nonNeg(st.Padding[2].resolveV(basis, st.FontSize))
}

// widthValue resolves a horizontal CSS length to a content width, applying
// border-box sizing when requested.
func (l *layouter) widthValue(st *Style, v Length, basis int) int {
	w := v.resolveH(basis, st.FontSize)
	if st.BoxSizing == "border-box" {
		w -= l.boxWidth(st, basis)
		if w < 0 {
			w = 0
		}
	}
	return w
}

// heightValue resolves a vertical CSS length to a content height, applying
// border-box sizing when requested.
func (l *layouter) heightValue(st *Style, v Length, basis int) int {
	h := v.resolveV(basis, st.FontSize)
	if st.BoxSizing == "border-box" {
		h -= l.boxHeight(st, basis)
		if h < 0 {
			h = 0
		}
	}
	return h
}

func (l *layouter) boxMetrics(st *Style, availW int) (ml, mr, mt, mb, bl, br, bt, bb, pl, pr, pt, pb int) {
	f := st.FontSize
	// CSS resolves percentage margins and padding vertically against the
	// containing block's width. Convert that width to a row basis so
	// `padding-bottom: 56.25%` keeps the usual 16:9 box.
	vBasis := int(math.Round(float64(availW) * cellW / cellH))
	ml = st.Margin[3].resolveH(availW, f)
	mr = st.Margin[1].resolveH(availW, f)
	mt = st.Margin[0].resolveV(vBasis, f)
	mb = st.Margin[2].resolveV(vBasis, f)
	bl = usedBorderH(st, 3, availW)
	br = usedBorderH(st, 1, availW)
	bt = usedBorderV(st, 0, availW)
	bb = usedBorderV(st, 2, availW)
	pl = nonNeg(st.Padding[3].resolveH(availW, f))
	pr = nonNeg(st.Padding[1].resolveH(availW, f))
	pt = nonNeg(st.Padding[0].resolveV(vBasis, f))
	pb = nonNeg(st.Padding[2].resolveV(vBasis, f))
	return
}

func (l *layouter) isBlock(n *Node) bool {
	if n == nil || n.Type != ElementNode || n.Style == nil {
		return false
	}
	if n.Style.Display == "none" {
		return false
	}
	if n.Style.Float != "" {
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

// hasBlockDescendant reports whether an inline box contains a block-level
// descendant. Browsers split such an inline box around the block; treating the
// inline wrapper as a block keeps the descendant's grid/flex children out of
// the surrounding inline flow.
func (l *layouter) hasBlockDescendant(n *Node) bool {
	if n == nil {
		return false
	}
	for _, c := range n.Children {
		if c.Type != ElementNode || c.Style == nil || c.Style.Display == "none" {
			continue
		}
		if l.isBlock(c) {
			return true
		}
		// An inline-block is an atomic inline box; its internal blocks do not
		// break the surrounding line.
		if c.Style.Display == "inline-block" {
			continue
		}
		if c.Style.Display == "inline" && l.hasBlockDescendant(c) {
			return true
		}
	}
	return false
}

// inlineWrapperIsBlock reports whether an inline-level wrapper must be laid
// out as a block so its block/grid/flex descendants keep their own layout.
// An inline-block with an explicit width behaves like a block container; an
// inline-block without one stays an atomic inline box (menu items, buttons).
func (l *layouter) inlineWrapperIsBlock(n *Node) bool {
	if n == nil || n.Style == nil {
		return false
	}
	switch n.Style.Display {
	case "inline":
		return l.hasBlockDescendant(n)
	case "inline-block":
		return n.Style.Width.Set && !n.Style.Width.IsAuto() && l.hasBlockDescendant(n)
	}
	return false
}

// block lays out node n as a block at (x,y) and returns its outer height,
// including vertical margins.
func (l *layouter) block(n *Node, x, y, availW int) int {
	h, _ := l.blockLayout(n, x, y, availW, 0, false)
	return h
}

// blockLayout is block with the border-box rectangle reported. forceContentH,
// when positive, stretches the content box to at least that height (used by
// absolutely positioned boxes with both top and bottom offsets).
func (l *layouter) blockLayout(n *Node, x, y, availW, forceContentH int, widthResolved bool) (int, Rect) {
	if l.outOfTime() {
		return 0, Rect{}
	}
	if l.truncated || l.overRowLimit(y) {
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
	if !widthResolved {
		if st.Width.Set && !st.Width.IsAuto() {
			if w := l.widthValue(st, st.Width, availW); w < contentW {
				contentW = w
			}
		}
		if st.MaxWidth.Set && !st.MaxWidth.IsAuto() {
			if w := l.widthValue(st, st.MaxWidth, availW); w < contentW {
				contentW = w
			}
		}
		if st.MinWidth.Set && !st.MinWidth.IsAuto() {
			if w := l.widthValue(st, st.MinWidth, availW); w > contentW {
				contentW = w
			}
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
		if l.cbs != nil {
			l.cbs[n] = newCB
		}
		defer func() { l.cb = parentCB }()
	}

	bgIndex := -1
	if st.Background.opaque() {
		l.addFill(boxX, top, boxW, 1, st.Background)
		bgIndex = len(l.frags) - 1
	}
	clipStart := len(l.frags)

	var contentH int
	switch {
	case n.Tag == "img":
		contentH = l.blockImage(n, contentX, contentY, contentW)
	case st.Display == "table":
		contentH = l.table(n, contentX, contentY, contentW)
	case st.Display == "grid":
		contentH = l.grid(n, contentX, contentY, contentW)
	case st.Display == "flex" && st.FlexDir != "column":
		contentH = l.flexRow(n, contentX, contentY, contentW)
	default:
		contentH = l.flow(n, contentX, contentY, contentW)
	}
	if st.LineClamp > 0 && st.Overflow == "hidden" {
		// Approximate -webkit-line-clamp: keep at most N text lines inside the
		// block and let following content move up accordingly.
		maxH := st.LineClamp * l.lineH(st)
		if maxH < 1 {
			maxH = 1
		}
		if contentH > maxH {
			l.trimFrags(contentY+maxH, fragStart)
			contentH = maxH
		}
	}
	if n.Tag != "img" && st.AspectRatio > 0 && (!st.Height.Set || st.Height.IsAuto()) {
		h := int(math.Round(float64(contentW) * float64(cellW) / (st.AspectRatio * float64(cellH))))
		if h > contentH {
			contentH = h
		}
	}
	if st.Height.Set && !st.Height.IsAuto() && st.Height.U != UnitPercent {
		// An explicit height is the used height, not a minimum: overflowing
		// children may paint outside the box unless overflow clips them.
		contentH = l.heightValue(st, st.Height, availW)
	}
	if st.MaxHeight.Set && !st.MaxHeight.IsAuto() && st.MaxHeight.U != UnitPercent {
		if h := l.heightValue(st, st.MaxHeight, availW); h < contentH {
			contentH = h
		}
	}
	if st.MinHeight.Set && !st.MinHeight.IsAuto() && st.MinHeight.U != UnitPercent {
		if h := l.heightValue(st, st.MinHeight, availW); h > contentH {
			contentH = h
		}
	}
	if forceContentH > contentH {
		contentH = forceContentH
	}
	// An overflow container clips its descendants to its content box. This
	// matters for scrollable panels: a child's max-height must not let its
	// overflowing text paint over following content.
	if st.Overflow != "" {
		l.clipFragments(clipStart, Rect{
			X: boxX + bl,
			Y: top + bt,
			W: pl + contentW + pr,
			H: pt + contentH + pb,
		})
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
	rx, ry := 0, 0
	if st.Position == "relative" {
		dx, dy := l.relativeOffset(st, parentCB)
		if dx != 0 || dy != 0 {
			rx, ry = dx, dy
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
	if st.Position != "fixed" {
		l.boxes[n] = Rect{X: boxX + rx, Y: top + ry, W: boxW, H: totalH}
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

const (
	itemWord inlineItemKind = iota
	itemSpace
	itemAtomic
)
