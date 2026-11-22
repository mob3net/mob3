package browser

import (
	"image"

	"browser/internal/engine"
)

// Temporary paint aliases. Paint lives in internal/engine; the browser keeps
// the gfx/terminal placement layer and the tests that exercise both.

type Grid = engine.Grid
type Cell = engine.Cell
type ScaledRun = engine.ScaledRun
type scaledRun = engine.ScaledRun

const maxSVGDim = engine.MaxSVGDim

var errBadSVG = engine.ErrBadSVG

type ImagePlacement = engine.ImagePlacement

func NewGrid(w, h int, fg, bg Color) *Grid { return engine.NewGrid(w, h, fg, bg) }

func PaintLayout(g *Grid, ly *Layout, scroll, topRow, bottomRow int, focus *LinkInfo, pageFG Color) {
	engine.PaintLayout(g, ly, scroll, topRow, bottomRow, focus, pageFG)
}
func PaintLayoutGraphics(g *Grid, ly *Layout, scroll, topRow, bottomRow int, focus *LinkInfo, pageFG Color, placements *[]ImagePlacement) {
	engine.PaintLayoutGraphics(g, ly, scroll, topRow, bottomRow, focus, pageFG, placements)
}
func PaintLayoutBig(g *Grid, ly *Layout, scroll, topRow, bottomRow int, focus *LinkInfo, pageFG Color, placements *[]ImagePlacement, bigText bool) {
	engine.PaintLayoutBig(g, ly, scroll, topRow, bottomRow, focus, pageFG, placements, bigText)
}

func reuseGrid(g *Grid, w, h int, fg, bg Color) *Grid { return engine.ReuseGrid(g, w, h, fg, bg) }
func blitGrid(dst, src *Grid, y int)                  { engine.BlitGrid(dst, src, y) }

func stripTerminalControls(s string) string { return engine.StripTerminalControls(s) }
func isSVG(ctype string, data []byte) bool  { return engine.IsSVG(ctype, data) }
func decodeSVG(data []byte) (image.Image, error) {
	return engine.DecodeSVG(data)
}
func decodeSVGWithin(data []byte, maxPixels int64) (image.Image, error) {
	return engine.DecodeSVGWithin(data, maxPixels)
}

var backgroundPlaceholderBG = engine.BackgroundPlaceholderBG

func paintImage(g *Grid, img image.Image, crop image.Rectangle, x, y, w, h, rowStart, totalRows int) {
	engine.PaintImage(g, img, crop, x, y, w, h, rowStart, totalRows)
}
