package engine

import "strings"

// ResetSVGBudget clears the per-page inline-SVG pixel and count counters.
func (l *Loader) ResetSVGBudget() {
	l.SVGMu.Lock()
	l.SVGPixels = 0
	l.SVGCount = 0
	l.SVGMu.Unlock()
}

// LoadInlineSVGs rasterizes inline <svg> elements into their Node.Img field so
// layout can treat them as images. The per-page pixel and count budgets keep a
// document with many icons from retaining gigabytes of rasters.
func (l *Loader) LoadInlineSVGs(root *Node) {
	if l.NoImages {
		return
	}
	l.SVGMu.Lock()
	defer l.SVGMu.Unlock()
	nodes := root.FindAll(func(n *Node) bool { return n.Tag == "svg" })
	for _, n := range nodes {
		if n.Img != nil || n.ImgFailed {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(n.Attr("aria-hidden")), "true") {
			continue
		}
		remaining := l.SVGBudgetPixels - l.SVGPixels
		if remaining < 1 || l.SVGCount >= maxInlineSVGCount {
			n.ImgFailed = true
			continue
		}
		img, err := DecodeSVGWithin([]byte(SerializeSVG(n)), remaining)
		if err != nil {
			n.ImgFailed = true
			continue
		}
		n.Img = img
		n.ImgSrc = "inline-svg"
		l.SVGPixels += int64(img.Bounds().Dx()) * int64(img.Bounds().Dy())
		l.SVGCount++
	}
}
