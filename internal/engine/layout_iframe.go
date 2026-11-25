package engine

import "math"

const (
	// iframeDefaultWidthPx and iframeDefaultHeightPx are the replaced-element
	// defaults a browser uses when an iframe has no declared size.
	iframeDefaultWidthPx  = 300
	iframeDefaultHeightPx = 150
	// iframeLabelCells is the width of the full "iframe" label. A narrower box
	// gets the single-cell "i" marker instead.
	iframeLabelCells = 6
)

func iframeDefaultWidthCells() int {
	w := int(math.Round(iframeDefaultWidthPx / cellW))
	if w < 1 {
		w = 1
	}
	return w
}

func iframeDefaultHeightCells() int {
	h := int(math.Round(iframeDefaultHeightPx / cellH))
	if h < 1 {
		h = 1
	}
	return h
}

// iframeSize returns the replaced box an inline iframe reserves, in terminal
// cells. st.Width and st.Height already include the legacy width/height
// attributes, which are applied before author CSS, so a CSS declaration wins.
// maxW is the line width used for percentage resolution and for capping a box
// wider than the line. The document is never loaded.
func iframeSize(st *Style, maxW int) (w, h int) {
	if st == nil {
		st = defaultStyle()
	}
	basis := MaxInt(1, maxW)
	widthSet := st.Width.Set && !st.Width.IsAuto()
	// A percentage height needs a definite containing-block height; inline
	// content has none, so it behaves like auto and the replaced default wins.
	heightSet := st.Height.Set && !st.Height.IsAuto() && st.Height.U != UnitPercent
	if widthSet {
		w = st.Width.ResolveH(basis, st.FontSize)
	}
	if heightSet {
		h = st.Height.ResolveV(basis, st.FontSize)
	}
	switch {
	case widthSet && heightSet:
	case widthSet && st.AspectRatio > 0:
		h = int(math.Round(float64(w) * cellW / (st.AspectRatio * cellH)))
	case heightSet && st.AspectRatio > 0:
		w = int(math.Round(float64(h) * cellH * st.AspectRatio / cellW))
	case widthSet:
		h = iframeDefaultHeightCells()
	case heightSet:
		w = iframeDefaultWidthCells()
	default:
		w = iframeDefaultWidthCells()
		h = iframeDefaultHeightCells()
	}
	if w > 0 && maxW > 0 && w > maxW {
		w = maxW
	}
	return w, h
}

// addIFrame reserves an inline iframe's replaced box and paints the placeholder
// that stands in for the never-loaded browsing context. A percentage width
// resolves against the nearest sized inline ancestor, as it does for images,
// and the box is capped to the line band.
func (c *inlineCtx) addIFrame(n *Node, st *Style) {
	w, h := iframeSize(st, c.l.inlineImageWidth(n, c.lineMaxW()))
	if maxW := c.lineMaxW(); maxW > 0 && w > maxW {
		w = maxW
	}
	if w < 1 || h < 1 {
		return
	}
	w2, h2 := w, h
	c.addAtomicBox(inlineItem{
		kind: itemAtomic, w: w, h: h, style: st, node: n,
		paint: func(x, y int) {
			c.l.paintIFrame(n, x, y, w2, h2)
		},
	})
}

// blockIFrame resolves and paints the replaced box for a block-level iframe.
// usedW is the block's content width after CSS width/min/max, usedH its content
// height after CSS height/aspect-ratio/min/max. It returns the height the box
// reserves, which can be the replaced default.
func (l *Layouter) blockIFrame(n *Node, st *Style, x, y, usedW, usedH int) int {
	if st == nil {
		return usedH
	}
	if usedW <= 0 {
		// A zero-width block has no box to paint, but a declared height still
		// reserves vertical space.
		return usedH
	}
	widthSet := st.Width.Set && !st.Width.IsAuto()
	heightSet := st.Height.Set && !st.Height.IsAuto()
	// A percentage height the block layout could not resolve against a
	// definite parent behaves like auto, so the replaced default applies.
	definiteH := heightSet && !(st.Height.U == UnitPercent && usedH <= 0)
	w := usedW
	if !widthSet {
		switch {
		case st.AspectRatio > 0 && usedH > 0:
			w = int(math.Round(float64(usedH) * cellH * st.AspectRatio / cellW))
		default:
			w = iframeDefaultWidthCells()
		}
		if usedW > 0 && w > usedW {
			w = usedW
		}
	}
	h := usedH
	if h <= 0 && !definiteH {
		h = iframeDefaultHeightCells()
		usedH = h
	}
	if w > 0 && h > 0 {
		l.paintIFrame(n, x, y, w, h)
	}
	return usedH
}

// paintIFrame paints the reserved iframe box black with a gray label so the
// user can see that the space belongs to an iframe. Narrow boxes use "i".
func (l *Layouter) paintIFrame(n *Node, x, y, w, h int) {
	if w < 1 || h < 1 || l.outOfTime() {
		return
	}
	if l.overFragLimit() || l.overRowLimit(y) {
		l.truncated = true
		return
	}
	l.frags = append(l.frags, Frag{
		Kind: FragFrame, X: x, Y: y, W: w, H: h,
		Color: RGB(0, 0, 0), Node: n,
	})
	label := "i"
	if w >= iframeLabelCells {
		label = "iframe"
	}
	st := defaultStyle()
	st.Color = RGB(0x9a, 0xa0, 0xa6)
	st.Background = RGB(0, 0, 0)
	l.addText(x, y, label, st, n)
}
