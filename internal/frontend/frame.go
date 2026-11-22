// Package frontend turns protocol frames into terminal output and images. It
// depends only on the wire protocol, never on engine types or page state.
package frontend

import (
	"math"
	"os"
	"strconv"
	"strings"

	"browser/internal/protocol"
)

// ColorMode selects the SGR color depth used for cell output.
type ColorMode int

const (
	// ColorTrueColor uses 24-bit 38;2/48;2 sequences.
	ColorTrueColor ColorMode = iota
	// Color256 uses the xterm 256-color palette with 38;5/48;5 sequences.
	Color256
)

var terminalColorMode = ColorTrueColor

// SetColorMode sets the SGR color depth used by the encoder.
func SetColorMode(mode ColorMode) {
	terminalColorMode = mode
}

// DetectColorMode reports the color depth to use. macOS Terminal.app does not
// implement 24-bit SGR sequences and garbles them, so it gets 256-color output;
// terminals that advertise truecolor through COLORTERM keep 24-bit output.
func DetectColorMode() ColorMode {
	ct := strings.ToLower(os.Getenv("COLORTERM"))
	if strings.Contains(ct, "truecolor") || strings.Contains(ct, "24bit") {
		return ColorTrueColor
	}
	if strings.EqualFold(os.Getenv("TERM_PROGRAM"), "Apple_Terminal") {
		return Color256
	}
	return ColorTrueColor
}

func attrSet(c protocol.Cell, bit uint8) bool {
	return c.Attrs&bit != 0
}

// ANSIFor returns the SGR sequence for a cell's attributes and colors.
func ANSIFor(c protocol.Cell) string {
	var b strings.Builder
	b.WriteString("\x1b[0")
	if attrSet(c, protocol.AttrBold) {
		b.WriteString(";1")
	}
	if attrSet(c, protocol.AttrItalic) {
		b.WriteString(";3")
	}
	if attrSet(c, protocol.AttrUnderline) {
		b.WriteString(";4")
	}
	if attrSet(c, protocol.AttrStrike) {
		b.WriteString(";9")
	}
	if attrSet(c, protocol.AttrReverse) {
		b.WriteString(";7")
	}
	b.WriteString(";")
	b.WriteString(FGCode(c.FG))
	b.WriteString(";")
	b.WriteString(BGCode(c.BG))
	b.WriteString("m")
	return b.String()
}

// FGCode returns the foreground SGR fragment for a color.
func FGCode(c protocol.Color) string {
	if !c.Set {
		return "39"
	}
	if terminalColorMode == Color256 {
		return "38;5;" + strconv.Itoa(Color256Index(c))
	}
	return "38;2;" + strconv.Itoa(int(c.R)) + ";" + strconv.Itoa(int(c.G)) + ";" + strconv.Itoa(int(c.B))
}

// BGCode returns the background SGR fragment for a color.
func BGCode(c protocol.Color) string {
	if !c.Set {
		return "49"
	}
	if terminalColorMode == Color256 {
		return "48;5;" + strconv.Itoa(Color256Index(c))
	}
	return "48;2;" + strconv.Itoa(int(c.R)) + ";" + strconv.Itoa(int(c.G)) + ";" + strconv.Itoa(int(c.B))
}

// Color256Index maps an RGB color to the closest index in the xterm 256-color
// palette: the 6x6x6 color cube or the 24-step grayscale ramp, whichever is
// nearer.
func Color256Index(c protocol.Color) int {
	r, g, b := int(c.R), int(c.G), int(c.B)
	ri := int(math.Round(float64(r) / 255 * 5))
	gi := int(math.Round(float64(g) / 255 * 5))
	bi := int(math.Round(float64(b) / 255 * 5))
	levels := [6]int{0, 95, 135, 175, 215, 255}
	cr, cg, cb := levels[ri], levels[gi], levels[bi]
	cubeDist := sq(cr-r) + sq(cg-g) + sq(cb-b)

	avg := (r + g + b) / 3
	grayIdx := int(math.Round((float64(avg) - 8) / 10))
	if grayIdx < 0 {
		grayIdx = 0
	}
	if grayIdx > 23 {
		grayIdx = 23
	}
	gray := 8 + 10*grayIdx
	grayDist := sq(gray-r) + sq(gray-g) + sq(gray-b)
	if grayDist < cubeDist {
		return 232 + grayIdx
	}
	return 16 + 36*ri + 6*gi + bi
}

func sq(v int) int { return v * v }

// AttrCode returns the SGR sequence for a cell's non-color attributes.
func AttrCode(c protocol.Cell) string {
	if c.Attrs&(protocol.AttrBold|protocol.AttrItalic|protocol.AttrUnderline|protocol.AttrStrike|protocol.AttrReverse) == 0 {
		return "\x1b[0m"
	}
	var b strings.Builder
	b.WriteString("\x1b[0")
	if attrSet(c, protocol.AttrBold) {
		b.WriteString(";1")
	}
	if attrSet(c, protocol.AttrItalic) {
		b.WriteString(";3")
	}
	if attrSet(c, protocol.AttrUnderline) {
		b.WriteString(";4")
	}
	if attrSet(c, protocol.AttrStrike) {
		b.WriteString(";9")
	}
	if attrSet(c, protocol.AttrReverse) {
		b.WriteString(";7")
	}
	b.WriteString("m")
	return b.String()
}

// EncodeFrame renders a frame to ANSI strings, one per row. color selects SGR
// colors; attrs keeps bold/italic/underline/strike when colors are off, which
// is what interactive monochrome output needs.
func EncodeFrame(f protocol.Frame, color, attrs bool) []string {
	if f.W < 0 || f.H < 0 || len(f.Cells) != f.W*f.H {
		return nil
	}
	out := make([]string, f.H)
	for y := 0; y < f.H; y++ {
		var b strings.Builder
		var last string
		for x := 0; x < f.W; x++ {
			c := f.Cells[y*f.W+x]
			if c.Flags&protocol.CellSkip != 0 {
				n := 0
				for x < f.W && f.Cells[y*f.W+x].Flags&protocol.CellSkip != 0 {
					n++
					x++
				}
				x--
				if color {
					b.WriteString("\x1b[")
					b.WriteString(strconv.Itoa(n))
					b.WriteString("C")
				} else {
					b.WriteString(strings.Repeat(" ", n))
				}
				continue
			}
			if c.Flags&protocol.CellCont != 0 {
				continue
			}
			if c.Run != 0 && int(c.Run) <= len(f.Runs) {
				r := f.Runs[int(c.Run)-1]
				if color {
					b.WriteString(ANSIFor(protocol.Cell{R: ' ', FG: r.FG, BG: r.BG, Attrs: r.Attrs}))
					last = ""
					b.WriteString("\x1b]66;s=")
					b.WriteString(strconv.Itoa(r.Scale))
					b.WriteString(";")
					b.WriteString(r.Text)
					b.WriteString("\x07")
				} else {
					b.WriteString(strings.Repeat(" ", maxInt(1, r.Cells)))
				}
				x += r.Cells - 1
				continue
			}
			r := c.R
			if r == 0 {
				r = ' '
			}
			if !color {
				if attrs {
					s := AttrCode(c)
					if s != last {
						b.WriteString(s)
						last = s
					}
				}
				if c.Flags&protocol.CellImg != 0 {
					b.WriteRune('▒')
				} else {
					b.WriteRune(r)
				}
				continue
			}
			s := ANSIFor(c)
			if s != last {
				b.WriteString(s)
				last = s
			}
			b.WriteRune(r)
		}
		if color {
			b.WriteString("\x1b[0m")
		} else {
			line := strings.TrimRight(b.String(), " ")
			b.Reset()
			b.WriteString(line)
			if attrs && line != "" {
				b.WriteString("\x1b[0m")
			}
		}
		out[y] = b.String()
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
