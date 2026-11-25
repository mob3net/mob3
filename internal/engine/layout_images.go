package engine

import (
	"image"
	"math"
	"strings"

	"browser/internal/textwidth"
)

// svgLabel returns the text that represents an inline SVG that was not
// rasterized: its <title>, its visible text, or an icon marker for an
// aria-label. visible is false for an SVG the page hides from assistive
// technology, which must render nothing.
func svgLabel(n *Node) (label string, visible bool) {
	if strings.EqualFold(n.Attr("aria-hidden"), "true") {
		return "", false
	}
	if t := n.FindFirst("title"); t != nil {
		if label := strings.TrimSpace(t.TextContent()); label != "" {
			return label, true
		}
	}
	if label := strings.TrimSpace(VisibleText(n)); label != "" {
		return label, true
	}
	if strings.TrimSpace(n.Attr("aria-label")) != "" {
		// An aria-label on an icon-only SVG is an accessibility name, not page
		// text. Rendering it into a tiny icon slot truncates it into garbage,
		// so use a compact icon marker instead.
		return "▣", true
	}
	return "", true
}

// imageFallbackLabel returns the text that stands in for an image that is not
// painted: an <img> alt, or an inline SVG's accessibility fallback.
func imageFallbackLabel(n *Node) (label string, visible bool) {
	if n.Tag == "svg" {
		return svgLabel(n)
	}
	return strings.TrimSpace(n.Attr("alt")), true
}

// blockSVG lays out an SVG that became a block box, such as a flex or grid
// item. It paints the rasterized image, or the accessibility fallback when
// rasterization did not happen (images off or a decoder failure).
func (l *Layouter) blockSVG(n *Node, x, y, w int) int {
	if n.Img != nil && !l.imagesOff {
		return l.blockImage(n, x, y, w)
	}
	label, visible := svgLabel(n)
	if !visible || label == "" {
		return 0
	}
	if w > 0 {
		label = textwidth.Truncate(label, w)
	}
	l.addText(x, y, label, n.Style, n)
	return 1
}

func (c *inlineCtx) addImage(n *Node, st *Style) {
	maxW := c.l.inlineImageWidth(n, c.lineMaxW())
	if n.Img == nil || c.l.imagesOff {
		alt := n.Attr("alt")
		w, h := imageSize(n, st, maxW, c.imageCapW)
		if alt == "" {
			// An explicitly sized broken image gets a small placeholder;
			// 1-cell decorative images (tracking pixels) stay invisible.
			if w >= 2 || h >= 2 {
				c.addLabel("░", st, n)
			}
			return
		}
		c.addLabelBox(imageAltLabel(alt, w), st, n, w, h)
		return
	}
	w, h := imageSize(n, st, maxW, c.imageCapW)
	w2, h2 := w, h
	c.addAtomicBox(inlineItem{
		kind: itemAtomic, w: w, h: h, style: st, node: n,
		paint: func(x, y int) {
			c.l.frags = append(c.l.frags, Frag{Kind: FragImage, X: x, Y: y, W: w2, H: h2, Img: n.Img, Node: n})
		},
	})
}

const maxImageRows = 16

// MaxImageRows caps an image box height in cells.
const MaxImageRows = maxImageRows

func (l *Layouter) imageCapWidth() int {
	w := l.Width * 2 / 5
	if w < 1 {
		w = 1
	}
	return w
}

// imageSize returns the terminal cell size for an image, preserving its aspect
// ratio and applying the terminal-friendly display cap.
// blockImage lays out an image that is itself a block or absolutely / fixed
// positioned box. Normal inline images go through inlineCtx.addImage instead.
func (l *Layouter) blockImage(n *Node, x, y, w int) int {
	st := n.Style
	boxH := l.forcedImageH
	if boxH <= 0 && st.Height.Set && st.Height.U != UnitPercent {
		boxH = st.Height.ResolveV(w, st.FontSize)
	}
	if boxH > 0 && st.ObjectFit != "" && n.Img != nil && !l.imagesOff {
		return l.addFittedImage(n, st, x, y, w, boxH)
	}
	if n.Img == nil || l.imagesOff {
		alt, visible := imageFallbackLabel(n)
		if !visible {
			return 0
		}
		iw, ih := imageSize(n, st, w, l.imageCapWidth())
		if alt == "" {
			if iw >= 2 || ih >= 2 {
				l.addText(x, y, "░", st, n)
				return 1
			}
			return 0
		}
		l.addText(x, y, imageAltLabel(alt, iw), st, n)
		altH := ih
		if boxH > altH {
			altH = boxH
		}
		if altH < 1 {
			altH = 1
		}
		return altH
	}
	iw, ih := imageSize(n, st, w, l.imageCapWidth())
	l.frags = append(l.frags, Frag{Kind: FragImage, X: x, Y: y, W: iw, H: ih, Img: n.Img, Node: n})
	return ih
}

// addFittedImage draws an image into a definite CSS box using object-fit.
func (l *Layouter) addFittedImage(n *Node, st *Style, x, y, boxW, boxH int) int {
	b := n.Img.Bounds()
	iw, ih := b.Dx(), b.Dy()
	if iw <= 0 || ih <= 0 || boxW <= 0 || boxH <= 0 {
		return 0
	}
	posX, posY := st.ObjectPosX, st.ObjectPosY
	if posX < 0 {
		posX = 0
	}
	if posX > 1 {
		posX = 1
	}
	if posY < 0 {
		posY = 0
	}
	if posY > 1 {
		posY = 1
	}
	bw, bh := float64(boxW)*cellW, float64(boxH)*cellH
	fit := st.ObjectFit
	if fit == "" {
		fit = "fill"
	}
	add := func(fx, fy, fw, fh int, crop image.Rectangle) {
		if fw < 1 {
			fw = 1
		}
		if fh < 1 {
			fh = 1
		}
		l.frags = append(l.frags, Frag{Kind: FragImage, X: fx, Y: fy, W: fw, H: fh, Img: n.Img, Crop: crop, Node: n})
	}
	switch fit {
	case "contain", "scale-down":
		scale := math.Min(float64(bw)/float64(iw), float64(bh)/float64(ih))
		if fit == "scale-down" && scale > 1 {
			scale = 1
		}
		fw := int(math.Round(float64(iw) * scale / cellW))
		fh := int(math.Round(float64(ih) * scale / cellH))
		if fw > boxW {
			fw = boxW
		}
		if fh > boxH {
			fh = boxH
		}
		fx := x + int(math.Round(float64(boxW-fw)*posX))
		fy := y + int(math.Round(float64(boxH-fh)*posY))
		add(fx, fy, fw, fh, image.Rectangle{})
		return fh
	case "cover":
		scale := math.Max(float64(bw)/float64(iw), float64(bh)/float64(ih))
		cw := int(math.Round(float64(bw) / scale))
		ch := int(math.Round(float64(bh) / scale))
		if cw > iw {
			cw = iw
		}
		if ch > ih {
			ch = ih
		}
		if cw < 1 {
			cw = 1
		}
		if ch < 1 {
			ch = 1
		}
		sx := b.Min.X + int(math.Round(float64(iw-cw)*posX))
		sy := b.Min.Y + int(math.Round(float64(ih-ch)*posY))
		add(x, y, boxW, boxH, image.Rect(sx, sy, sx+cw, sy+ch))
		return boxH
	case "none":
		// No scaling: intrinsic pixels map to CSS px one to one, and an image
		// larger than the box is cropped from its object-position anchor.
		fw := int(math.Round(float64(iw) / cellW))
		fh := int(math.Round(float64(ih) / cellH))
		if fw <= boxW && fh <= boxH {
			fx := x + int(math.Round(float64(boxW-fw)*posX))
			fy := y + int(math.Round(float64(boxH-fh)*posY))
			add(fx, fy, fw, fh, image.Rectangle{})
			return fh
		}
		cw := float64(boxW) * cellW
		ch := float64(boxH) * cellH
		if cw > float64(iw) {
			cw = float64(iw)
		}
		if ch > float64(ih) {
			ch = float64(ih)
		}
		if cw < 1 {
			cw = 1
		}
		if ch < 1 {
			ch = 1
		}
		sx := b.Min.X + int(math.Round((float64(iw)-cw)*posX))
		sy := b.Min.Y + int(math.Round((float64(ih)-ch)*posY))
		add(x, y, boxW, boxH, image.Rect(sx, sy, sx+int(math.Round(cw)), sy+int(math.Round(ch))))
		return boxH
	default: // fill
		add(x, y, boxW, boxH, image.Rectangle{})
		return boxH
	}
}

// imageAltLabel is the text shown for an image that is not rasterized. It is
// clipped to the image box so a long alt cannot spill into neighbouring
// columns; very narrow boxes get a compact marker instead.
func imageAltLabel(alt string, max int) string {
	if max < 8 {
		return "▣"
	}
	return textwidth.Truncate("[image: "+alt+"]", max)
}

// imageSize returns the terminal-cell size of an image, from its intrinsic
// pixels and CSS/HTML dimensions, preserving aspect ratio and applying the
// viewport caps.
func imageSize(n *Node, st *Style, maxW, capW int) (w, h int) {
	iw, ih := 0, 0
	if n.Img != nil {
		b := n.Img.Bounds()
		iw, ih = b.Dx(), b.Dy()
	}
	cw, ch := 0, 0
	if st.Width.Set && !st.Width.IsAuto() {
		cw = st.Width.ResolveH(maxW, st.FontSize)
	}
	if st.Height.Set && st.Height.U != UnitPercent {
		ch = st.Height.ResolveV(maxW, st.FontSize)
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

// pixelsBackground reports whether a box should paint a CSS background image.
// Only block-level boxes are painted: backgrounds are paint-only, and the
// common text-over-photo pattern uses a block container. Inline boxes would
// need per-line-fragment painting.
func pixelsBackground(n *Node, st *Style) bool {
	if n == nil || st == nil || st.BackgroundImage == "" || st.Hidden {
		return false
	}
	if n.Tag == "html" || n.Tag == "body" {
		// Document backgrounds propagate to the canvas in CSS; painting one
		// across the whole document box would stretch it over every page.
		return false
	}
	switch st.Display {
	case "block", "list-item", "flex", "grid", "table", "inline-block":
		return true
	}
	return st.Position == "absolute" || st.Position == "fixed"
}

// backgroundCrop computes the source rectangle that fills a box with a
// background image (cover, or the whole bitmap for an explicit stretch).
func backgroundCrop(img image.Image, w, h int, st *Style) image.Rectangle {
	if img == nil {
		return image.Rectangle{}
	}
	b := img.Bounds()
	iw, ih := b.Dx(), b.Dy()
	if iw < 1 || ih < 1 || w < 1 || h < 1 {
		return b
	}
	if st != nil && st.BackgroundSize == "stretch" {
		return b
	}
	bw, bh := float64(w)*cellW, float64(h)*cellH
	scale := math.Max(bw/float64(iw), bh/float64(ih))
	cw := int(math.Round(bw / scale))
	ch := int(math.Round(bh / scale))
	if cw > iw {
		cw = iw
	}
	if ch > ih {
		ch = ih
	}
	if cw < 1 {
		cw = 1
	}
	if ch < 1 {
		ch = 1
	}
	px, py := 0.5, 0.5
	if st != nil {
		px = clampUnit(st.BackgroundPosX)
		py = clampUnit(st.BackgroundPosY)
	}
	sx := b.Min.X + int(math.Round(float64(iw-cw)*px))
	sy := b.Min.Y + int(math.Round(float64(ih-ch)*py))
	return image.Rect(sx, sy, sx+cw, sy+ch)
}

func clampUnit(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// inlineImageWidth resolves the containing-block width for an inline image's
// percentage size. An image inside a sized inline wrapper (a w-xl avatar ring
// or a w-full thumbnail holder) must use that wrapper, not the whole line: a
// width:100% image would otherwise be measured against the terminal line and
// hit the image display cap. Ancestors are resolved outermost first so a
// chain such as holder{width:100%} inside outer{width:2rem} narrows to 2rem.
func (l *Layouter) inlineImageWidth(n *Node, fallback int) int {
	var chain []*Style
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type != ElementNode || p.Style == nil {
			continue
		}
		st := p.Style
		if st.Width.Set && !st.Width.IsAuto() {
			chain = append(chain, st)
			if !lengthHasPercent(st.Width) {
				break
			}
			continue
		}
		switch st.Display {
		case "block", "flex", "grid", "list-item", "table", "table-row", "table-cell":
			// The inline formatting context's own width is the fallback.
			p = nil
		}
		if p == nil {
			break
		}
	}
	basis := fallback
	for i := len(chain) - 1; i >= 0; i-- {
		if w := chain[i].Width.ResolveH(basis, chain[i].FontSize); w > 0 {
			basis = w
		}
	}
	if basis < 1 {
		basis = 1
	}
	return basis
}

// ImageSize resolves an image box for layout.
func ImageSize(n *Node, st *Style, maxW, capW int) (w, h int) { return imageSize(n, st, maxW, capW) }
