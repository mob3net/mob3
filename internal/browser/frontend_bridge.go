package browser

import (
	"errors"

	"browser/internal/frontend"
)

// Temporary bridges to the extracted frontend package. They keep the existing
// browser API while the engine side is moved out.

type ColorMode = frontend.ColorMode

const (
	ColorTrueColor = frontend.ColorTrueColor
	Color256       = frontend.Color256
)

// terminalColorMode mirrors the frontend setting for tests and callers that
// inspect the active mode.
var terminalColorMode = ColorTrueColor

func SetColorMode(mode ColorMode) {
	terminalColorMode = mode
	frontend.SetColorMode(mode)
}

func DetectColorMode() ColorMode { return frontend.DetectColorMode() }

const (
	pngCellW = 8
	pngCellH = 16
)

func fgCode(c Color) string { return frontend.FGCode(protoColor(c)) }
func bgCode(c Color) string { return frontend.BGCode(protoColor(c)) }
func color256(c Color) int  { return frontend.Color256Index(protoColor(c)) }

func ansiFor(c Cell) string  { return frontend.ANSIFor(protoCell(c)) }
func attrCode(c Cell) string { return frontend.AttrCode(protoCell(c)) }

// encodeGrid is the draw-path encoder. It reuses the browser's protocol frame
// buffer, so a repaint does not allocate a second cell grid.
func (b *Browser) encodeGrid(g *Grid, color, attrs bool) []string {
	return frontend.EncodeFrame(toProtocolFrameInto(&b.frameBuf, g, 0), color, attrs)
}

// Lines renders the grid to ANSI strings, one per row.
func gridLines(g *Grid, color bool) []string {
	if g == nil {
		return nil
	}
	return frontend.EncodeFrame(toProtocolFrame(g, 0), color, false)
}

// LinesMono renders the grid without colors but keeps bold, italic, underline
// and strike attributes, so interactive output without color still marks links
// and link-hint letters. Plain dumps use Lines(false) instead.
func gridLinesMono(g *Grid) []string {
	if g == nil {
		return nil
	}
	return frontend.EncodeFrame(toProtocolFrame(g, 0), false, true)
}

// PNG renders the grid as a PNG image.
func gridPNG(g *Grid, scale int) ([]byte, error) {
	if g == nil {
		return nil, errors.New("empty grid")
	}
	return frontend.RenderPNG(toProtocolFrame(g, 0), scale)
}
