package browser

import (
	"strings"

	"browser/internal/engine"
	"browser/internal/protocol"
)

// Graphics modes are shared plain values; the renderer is injected by the app.
type GraphicsMode = protocol.GraphicsMode

const (
	GraphicsAuto   = protocol.GraphicsAuto
	GraphicsNone   = protocol.GraphicsNone
	GraphicsBlocks = protocol.GraphicsBlocks
	GraphicsKitty  = protocol.GraphicsKitty
	GraphicsITerm  = protocol.GraphicsITerm
)

// Graphics is the terminal image renderer the app injects.
type Graphics interface {
	Mode() GraphicsMode
	TextSizing() bool
	BeginFrame(*strings.Builder)
	ClearScreen(*strings.Builder)
	Clear(*strings.Builder)
	PlaceAll(*strings.Builder, []protocol.Placement)
}

func (l *Loader) resetSVGBudget() {
	l.svgMu.Lock()
	l.svgPixels = 0
	l.svgCount = 0
	l.svgMu.Unlock()
}

// loadInlineSVGs rasterizes inline <svg> elements into their Node.Img field so
// layout can treat them as images. The per-page pixel and count budgets keep a
// document with many icons from retaining gigabytes of rasters.
func (l *Loader) loadInlineSVGs(root *Node) {
	if l.noImages {
		return
	}
	l.svgMu.Lock()
	defer l.svgMu.Unlock()
	nodes := root.FindAll(func(n *Node) bool { return n.Tag == "svg" })
	for _, n := range nodes {
		if n.Img != nil || n.ImgFailed {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(n.Attr("aria-hidden")), "true") {
			continue
		}
		remaining := l.svgBudgetPixels - l.svgPixels
		if remaining < 1 || l.svgCount >= maxInlineSVGCount {
			n.ImgFailed = true
			continue
		}
		img, err := decodeSVGWithin([]byte(serializeSVG(n)), remaining)
		if err != nil {
			n.ImgFailed = true
			continue
		}
		n.Img = img
		n.ImgSrc = "inline-svg"
		l.svgPixels += int64(img.Bounds().Dx()) * int64(img.Bounds().Dy())
		l.svgCount++
	}
}

// serializeSVG keeps the historical browser test surface while the
// implementation lives in the engine.
func serializeSVG(n *Node) string { return engine.SerializeSVG(n) }
