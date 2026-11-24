package engine

import "browser/internal/frontend"

func init() {
	defaultPNGEncoder = frontend.RenderPNG
}

const (
	pngCellW = 8
	pngCellH = 16
)

type ColorMode = frontend.ColorMode

const (
	ColorTrueColor = frontend.ColorTrueColor
	Color256       = frontend.Color256
)

var terminalColorMode = ColorTrueColor

func SetColorMode(mode ColorMode) {
	terminalColorMode = mode
	frontend.SetColorMode(mode)
}

func DetectColorMode() ColorMode { return frontend.DetectColorMode() }

func fgCode(c Color) string { return frontend.FGCode(protoColor(c)) }
func bgCode(c Color) string { return frontend.BGCode(protoColor(c)) }
func color256(c Color) int  { return frontend.Color256Index(protoColor(c)) }

func gridLines(g *Grid, color bool) []string {
	return frontend.EncodeFrame(toProtocolFrame(g, 0), color, false)
}

func gridLinesMono(g *Grid) []string {
	return frontend.EncodeFrame(toProtocolFrame(g, 0), false, true)
}

func gridPNG(g *Grid, scale int) ([]byte, error) {
	return frontend.RenderPNG(toProtocolFrame(g, 0), scale)
}
