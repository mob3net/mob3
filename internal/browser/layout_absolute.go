package browser

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
	if a.fixed && l.hideFixed {
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
		contentW = l.widthValue(st, st.Width, cbW)
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
	if st.MaxWidth.Set && !st.MaxWidth.IsAuto() {
		if w := l.widthValue(st, st.MaxWidth, cbW); w < contentW {
			contentW = w
		}
	}
	if st.MinWidth.Set && !st.MinWidth.IsAuto() {
		if w := l.widthValue(st, st.MinWidth, cbW); w > contentW {
			contentW = w
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
	prevImageH := l.forcedImageH
	l.cb = cb
	l.curFixed = a.fixed
	l.forcedImageH = 0
	if n.Tag == "img" && st.Height.Set && st.Height.U == UnitPercent && cbH > 0 {
		l.forcedImageH = st.Height.resolveV(cbH, st.FontSize)
	}
	start := len(l.frags)
	absStart := len(l.abs)
	_, rect := l.blockLayout(n, x, y, availW, forceH, true)
	l.forcedImageH = prevImageH
	frags := make([]Frag, len(l.frags)-start)
	copy(frags, l.frags[start:])
	l.frags = l.frags[:start]
	if n.Tag == "a" && n.HasAttr("href") && rect.W > 0 && rect.H > 0 && noContentFrags(frags) {
		frags = append(frags, Frag{Kind: fragLink, X: rect.X, Y: rect.Y, W: rect.W, H: rect.H, Node: n})
	}
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
		// The box shifted after layout, so its containing block and any
		// auto-placed absolute descendants must move with it.
		if c := l.cbs[n]; c != nil {
			c.x += dx
			c.y += dy
		}
		for i := absStart; i < len(l.abs); i++ {
			l.abs[i].staticX += dx
			l.abs[i].staticY += dy
		}
	}
	l.frags = append(l.frags, frags...)
}
