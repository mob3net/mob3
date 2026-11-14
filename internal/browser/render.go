package browser

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

// Cell is one terminal cell in the framebuffer.
type Cell struct {
	R         rune
	FG, BG    Color
	Bold      bool
	Italic    bool
	Underline bool
	Strike    bool
	Cont      bool
	Img       bool
	// Raw is an escape sequence that renders scaled text occupying RawW
	// cells; Skip marks cells covered by the rest of that multicell.
	Raw  string
	RawW int
	Skip bool
}

// Grid is a terminal framebuffer.
type Grid struct {
	W, H  int
	Cells []Cell
}

// NewGrid creates a grid filled with spaces and the given background.
func NewGrid(w, h int, fg, bg Color) *Grid {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	g := &Grid{W: w, H: h, Cells: make([]Cell, w*h)}
	for i := range g.Cells {
		g.Cells[i] = Cell{R: ' ', FG: fg, BG: bg}
	}
	return g
}

func (g *Grid) Fill(x, y, w, h int, c Color) {
	if !c.Set || c.A == 0 {
		return
	}
	x0, y0 := maxInt(x, 0), maxInt(y, 0)
	x1, y1 := minInt(x+w, g.W), minInt(y+h, g.H)
	for yy := y0; yy < y1; yy++ {
		row := yy * g.W
		for xx := x0; xx < x1; xx++ {
			cell := &g.Cells[row+xx]
			cell.BG = c
			cell.Cont = false
			cell.Img = false
			cell.Raw = ""
			cell.RawW = 0
			cell.Skip = false
		}
	}
}

// FillHGradient fills a rectangle with a left-to-right color ramp.
func (g *Grid) FillHGradient(x, y, w, h int, from, to Color) {
	if !from.Set || !to.Set || w <= 0 || h <= 0 {
		return
	}
	x0, x1 := maxInt(x, 0), minInt(x+w, g.W)
	y0, y1 := maxInt(y, 0), minInt(y+h, g.H)
	mix := func(a, b uint8, t int) uint8 {
		return uint8((int(a)*(255-t) + int(b)*t) / 255)
	}
	for xx := x0; xx < x1; xx++ {
		t := 0
		if w > 1 {
			t = (xx - x) * 255 / (w - 1)
		}
		c := Color{
			R: mix(from.R, to.R, t), G: mix(from.G, to.G, t),
			B: mix(from.B, to.B, t), A: 255, Set: true,
		}
		for yy := y0; yy < y1; yy++ {
			cell := &g.Cells[yy*g.W+xx]
			cell.BG = c
			cell.Cont = false
			cell.Img = false
		}
	}
}

func (g *Grid) Put(x, y int, r rune, st *Style, fg, bg Color) {
	if y < 0 || y >= g.H {
		return
	}
	rw := runeWidth(r)
	if rw == 0 {
		return
	}
	if x >= g.W || x+rw <= 0 {
		return
	}
	if x < 0 {
		return
	}
	if rw == 2 && x+1 >= g.W {
		// A double-width glyph that does not fully fit would overflow the row
		// and can make the terminal wrap or scroll. Clip it away.
		return
	}
	c := &g.Cells[y*g.W+x]
	c.R = r
	c.FG = fg
	if bg.Set {
		c.BG = bg
	}
	if st != nil {
		c.Bold = st.Bold
		c.Italic = st.Italic
		c.Underline = st.Underline
		c.Strike = st.Strike
	} else {
		c.Bold, c.Italic, c.Underline, c.Strike = false, false, false, false
	}
	c.Cont = false
	c.Img = false
	c.Raw = ""
	c.RawW = 0
	c.Skip = false
	if rw == 2 && x+1 < g.W {
		c2 := &g.Cells[y*g.W+x+1]
		c2.R = 0
		c2.Cont = true
		c2.BG = c.BG
		c2.FG = c.FG
		c2.Img = false
		c2.Raw = ""
		c2.RawW = 0
		c2.Skip = false
	}
}

func (g *Grid) Text(x, y int, s string, st *Style, fg Color) {
	if st == nil {
		st = defaultStyle()
	}
	for _, r := range s {
		rw := runeWidth(r)
		if rw == 0 {
			continue
		}
		if x >= g.W {
			break
		}
		if x+rw > 0 {
			g.Put(x, y, r, st, fg, Color{})
		}
		x += rw
	}
}

// TextBig writes s with kitty's text-sizing protocol, scaled by scale. The
// text consumes scale*stringWidth(s) columns and scale rows; covered cells are
// marked so Lines does not paint over the multicell.
func (g *Grid) TextBig(x, y int, s string, st *Style, fg, bg Color, scale int) {
	s = stripTerminalControls(s)
	if s == "" || x < 0 || y < 0 || y >= g.H || x >= g.W {
		return
	}
	if scale < 2 {
		g.Text(x, y, s, st, fg)
		return
	}
	base := stringWidth(s)
	if base <= 0 {
		return
	}
	if room := (g.W - x) / scale; base > room {
		s = truncateWidth(s, room)
		base = stringWidth(s)
	}
	total := base * scale
	if base <= 0 || total <= 0 {
		return
	}
	var b strings.Builder
	// Use the minimal documented form: scale only. With w omitted (w=0), the
	// terminal derives the cell width itself, which matches the layout width
	// of scale*stringWidth(s). Some kitty-compatible terminals accept `s` but
	// render a literal `w=...` prefix as text, so never send the w key.
	b.WriteString("\x1b]66;s=")
	b.WriteString(strconv.Itoa(scale))
	b.WriteString(";")
	b.WriteString(s)
	b.WriteString("\x07")

	c := &g.Cells[y*g.W+x]
	c.Raw = b.String()
	c.RawW = total
	c.FG = fg
	if bg.Set {
		c.BG = bg
	}
	c.R = ' '
	c.Cont = false
	c.Img = false
	c.Skip = false
	if st != nil {
		c.Bold = st.Bold
		c.Italic = st.Italic
		c.Underline = st.Underline
		c.Strike = st.Strike
	} else {
		c.Bold, c.Italic, c.Underline, c.Strike = false, false, false, false
	}
	mark := func(cx, cy int) {
		if cx < 0 || cx >= g.W || cy < 0 || cy >= g.H {
			return
		}
		cc := &g.Cells[cy*g.W+cx]
		cc.Raw = ""
		cc.Skip = true
		cc.R = ' '
		cc.Cont = false
		cc.Img = false
		if bg.Set {
			cc.BG = bg
		}
	}
	for i := 1; i < total; i++ {
		mark(x+i, y)
	}
	for ry := 1; ry < scale; ry++ {
		for i := 0; i < total; i++ {
			mark(x+i, y+ry)
		}
	}
}

// ImagePlacement describes one image rectangle that should be drawn by a
// terminal graphics protocol instead of half-block art. Coordinates are in
// screen cells (already offset by the browser chrome and scroll).
type ImagePlacement struct {
	Img       image.Image
	Src       string // raw image source, used only as a cache discriminator
	X, Y      int
	W, H      int
	RowStart  int // first source image row (in layout image rows) to show
	TotalRows int // total layout image rows
}

// PaintLayout paints a laid-out page into the grid using half-block art.
// topRow/bottomRow bound the viewport (used for browser chrome), scroll is the
// vertical scroll offset.
func PaintLayout(g *Grid, ly *Layout, scroll, topRow, bottomRow int, focus *LinkInfo, pageFG Color) {
	PaintLayoutGraphics(g, ly, scroll, topRow, bottomRow, focus, pageFG, nil)
}

// PaintLayoutGraphics is PaintLayout with optional native image placement.
// When placements is non-nil, image fragments are appended to it instead of
// being drawn as half blocks; the caller draws them with the graphics protocol.
func PaintLayoutGraphics(g *Grid, ly *Layout, scroll, topRow, bottomRow int, focus *LinkInfo, pageFG Color, placements *[]ImagePlacement) {
	paintLayout(g, ly, scroll, topRow, bottomRow, focus, pageFG, placements, false)
}

// PaintLayoutBig is PaintLayoutGraphics with kitty-style scaled text runs.
func PaintLayoutBig(g *Grid, ly *Layout, scroll, topRow, bottomRow int, focus *LinkInfo, pageFG Color, placements *[]ImagePlacement, bigText bool) {
	paintLayout(g, ly, scroll, topRow, bottomRow, focus, pageFG, placements, bigText)
}

func paintLayout(g *Grid, ly *Layout, scroll, topRow, bottomRow int, focus *LinkInfo, pageFG Color, placements *[]ImagePlacement, bigText bool) {
	if ly == nil {
		return
	}
	focusBG := RGB(0xff, 0xe0, 0x66)
	focusFG := RGB(0x1f, 0x23, 0x28)
	for i := range ly.Frags {
		f := &ly.Frags[i]
		y := f.Y - scroll + topRow
		if y >= bottomRow {
			continue
		}
		if y+f.H <= topRow || y >= g.H {
			continue
		}
		switch f.Kind {
		case fragFill:
			clippedTop := maxInt(y, topRow)
			clippedBot := minInt(y+f.H, bottomRow)
			g.Fill(f.X, clippedTop, f.W, clippedBot-clippedTop, f.Color)
		case fragText:
			if y < topRow || y >= bottomRow {
				continue
			}
			st := f.Style
			fg := st.Color
			if !fg.Set {
				fg = pageFG
			}
			scale := 1
			if bigText {
				scale = st.TextScale()
			}
			bg := textBackground(f)
			if bg.opaque() {
				g.Fill(f.X, y, f.W, scale, bg)
			}
			if f.Link != nil && f.Link == focus {
				fg = focusFG
				bg = focusBG
				g.Fill(f.X, y, f.W, scale, bg)
			}
			effBG := bg
			if !effBG.Set && y >= 0 && y < g.H && g.W > 0 {
				ex := f.X
				if ex < 0 {
					ex = 0
				}
				if ex >= g.W {
					ex = g.W - 1
				}
				effBG = g.Cells[y*g.W+ex].BG
			}
			fg = readableFG(fg, effBG)
			if scale > 1 {
				g.TextBig(f.X, y, f.Text, st, fg, bg, scale)
			} else {
				g.Text(f.X, y, f.Text, st, fg)
			}
		case fragImage:
			if f.Img == nil {
				continue
			}
			y0 := maxInt(y, topRow)
			y1 := minInt(y+f.H, bottomRow)
			if y0 >= y1 {
				continue
			}
			imgTop := y0 - y
			if placements != nil {
				src := ""
				if f.Node != nil {
					src = f.Node.ImgSrc
				}
				*placements = append(*placements, ImagePlacement{
					Img: f.Img, Src: src, X: f.X, Y: y0, W: f.W, H: y1 - y0,
					RowStart: imgTop, TotalRows: f.H,
				})
				continue
			}
			paintImage(g, f.Img, f.X, y0, f.W, y1-y0, imgTop, f.H)
			if f.Link != nil && f.Link == focus {
				g.Fill(f.X, y, f.W, 1, focusBG)
				g.Fill(f.X, y+f.H-1, f.W, 1, focusBG)
				g.Fill(f.X, y, 1, f.H, focusBG)
				g.Fill(f.X+f.W-1, y, 1, f.H, focusBG)
			}
		}
	}
}

// paintImage draws an image as half-block art. rowStart and totalRows describe
// which part of the (possibly clipped) image we are drawing.
func paintImage(g *Grid, img image.Image, x, y, w, h, rowStart, totalRows int) {
	b := img.Bounds()
	iw, ih := b.Dx(), b.Dy()
	if iw <= 0 || ih <= 0 || w <= 0 || h <= 0 || totalRows <= 0 {
		return
	}
	for row := 0; row < h; row++ {
		gy := y + row
		if gy < 0 || gy >= g.H {
			continue
		}
		srcRow := rowStart + row
		top := sampleImage(img, b, srcRow*2, totalRows*2)
		bot := sampleImage(img, b, srcRow*2+1, totalRows*2)
		for col := 0; col < w; col++ {
			gx := x + col
			if gx < 0 || gx >= g.W {
				continue
			}
			cell := &g.Cells[gy*g.W+gx]
			under := cell.BG
			if !under.Set {
				under = RGB(255, 255, 255)
			}
			t := sampleColumn(img, b, col, w, top)
			bo := sampleColumn(img, b, col, w, bot)
			topC := over(t, under)
			botC := over(bo, under)
			topA := t.Set && t.A >= 128
			botA := bo.Set && bo.A >= 128
			switch {
			case !topA && !botA:
				continue
			case topA && botA && topC == botC:
				// Uniform cells need no glyph; the background color alone
				// renders the pixel and keeps the text layer clean.
				cell.R = ' '
				cell.FG = topC
				cell.BG = topC
			case topA && botA:
				cell.R = '▀'
				cell.FG = topC
				cell.BG = botC
			case topA:
				cell.R = '▀'
				cell.FG = topC
				cell.BG = under
			default:
				cell.R = '▄'
				cell.FG = botC
				cell.BG = under
			}
			cell.Cont = false
			cell.Img = true
			cell.Bold, cell.Italic, cell.Underline, cell.Strike = false, false, false, false
		}
	}
}

// rowRegion holds pre-computed y ranges for one image row.
type rowRegion struct{ y0, y1 int }

func sampleImage(img image.Image, b image.Rectangle, srcRow, totalRows int) rowRegion {
	y0 := b.Min.Y + srcRow*b.Dy()/maxInt(1, totalRows)
	y1 := b.Min.Y + (srcRow+1)*b.Dy()/maxInt(1, totalRows)
	if y1 <= y0 {
		y1 = y0 + 1
	}
	return rowRegion{y0, y1}
}

func sampleColumn(img image.Image, b image.Rectangle, col, totalCols int, reg rowRegion) Color {
	x0 := b.Min.X + col*b.Dx()/maxInt(1, totalCols)
	x1 := b.Min.X + (col+1)*b.Dx()/maxInt(1, totalCols)
	if x1 <= x0 {
		x1 = x0 + 1
	}
	stepX := (x1 - x0) / 6
	if stepX < 1 {
		stepX = 1
	}
	stepY := (reg.y1 - reg.y0) / 6
	if stepY < 1 {
		stepY = 1
	}
	var sr, sg, sb, sa uint64
	var n uint64
	for yy := reg.y0; yy < reg.y1; yy += stepY {
		for xx := x0; xx < x1; xx += stepX {
			r, g, bl, a := img.At(xx, yy).RGBA()
			sr += uint64(r)
			sg += uint64(g)
			sb += uint64(bl)
			sa += uint64(a)
			n++
		}
	}
	if n == 0 {
		return Color{}
	}
	a := uint8(sa / n / 257)
	if a == 0 {
		return Color{}
	}
	// RGBA() returns alpha-premultiplied values; convert back to straight alpha.
	unpremul := func(v uint64) uint8 {
		x := v / n / 257
		y := x * 255 / uint64(a)
		if y > 255 {
			y = 255
		}
		return uint8(y)
	}
	return Color{R: unpremul(sr), G: unpremul(sg), B: unpremul(sb), A: a, Set: true}
}

func over(c, bg Color) Color {
	if !c.Set {
		return bg
	}
	if c.A >= 255 {
		return Color{R: c.R, G: c.G, B: c.B, A: 255, Set: true}
	}
	a := float64(c.A) / 255
	mix := func(fg, b uint8) uint8 {
		return uint8(float64(fg)*a + float64(b)*(1-a) + 0.5)
	}
	return Color{R: mix(c.R, bg.R), G: mix(c.G, bg.G), B: mix(c.B, bg.B), A: 255, Set: true}
}

// textBackground returns the nearest inline background that should be painted
// behind a text fragment. Block-level backgrounds are painted with their box,
// so the walk stops there; inline/inline-block backgrounds otherwise have no
// box of their own and would be lost (common on buttons and nav links).
func textBackground(f *Frag) Color {
	if f.Style != nil && f.Style.Background.opaque() {
		return f.Style.Background
	}
	for n := f.Node; n != nil; n = n.Parent {
		st := n.Style
		if st == nil {
			continue
		}
		if st.Background.opaque() {
			switch st.Display {
			case "block", "flex", "list-item", "table", "table-row", "table-cell":
				return Color{}
			default:
				return st.Background
			}
		}
		switch st.Display {
		case "block", "flex", "list-item", "table":
			return Color{}
		}
	}
	return Color{}
}

// stripTerminalControls drops C0/C1 control characters before text is embedded
// in an OSC 66 escape. Ordinary Grid.Text drops them via zero rune width, but
// scaled text is written raw inside the escape sequence.
func stripTerminalControls(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || (r >= 0x7f && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

// readableFG keeps text legible when author CSS leaves a near-invisible
// foreground/background pair (for example white text whose background came
// from an unsupported gradient or CSS variable).
func readableFG(fg, bg Color) Color {
	if !fg.Set || !bg.Set {
		return fg
	}
	lum := func(c Color) int {
		return (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
	}
	lf, lb := lum(fg), lum(bg)
	diff := lf - lb
	if diff < 0 {
		diff = -diff
	}
	if diff >= 72 {
		return fg
	}
	if lb >= 128 {
		return RGB(0x1f, 0x23, 0x28)
	}
	return RGB(0xff, 0xff, 0xff)
}

func ansiFor(c Cell) string {
	var b strings.Builder
	b.WriteString("\x1b[0")
	if c.Bold {
		b.WriteString(";1")
	}
	if c.Italic {
		b.WriteString(";3")
	}
	if c.Underline {
		b.WriteString(";4")
	}
	if c.Strike {
		b.WriteString(";9")
	}
	b.WriteString(";")
	b.WriteString(fgCode(c.FG))
	b.WriteString(";")
	b.WriteString(bgCode(c.BG))
	b.WriteString("m")
	return b.String()
}

func fgCode(c Color) string {
	if !c.Set {
		return "39"
	}
	return "38;2;" + strconv.Itoa(int(c.R)) + ";" + strconv.Itoa(int(c.G)) + ";" + strconv.Itoa(int(c.B))
}

func bgCode(c Color) string {
	if !c.Set {
		return "49"
	}
	return "48;2;" + strconv.Itoa(int(c.R)) + ";" + strconv.Itoa(int(c.G)) + ";" + strconv.Itoa(int(c.B))
}

// Lines renders the grid to ANSI strings, one per row.
func (g *Grid) Lines(color bool) []string {
	out := make([]string, g.H)
	for y := 0; y < g.H; y++ {
		var b strings.Builder
		var last string
		for x := 0; x < g.W; x++ {
			c := g.Cells[y*g.W+x]
			if c.Skip {
				n := 0
				for x < g.W && g.Cells[y*g.W+x].Skip {
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
			if c.Cont {
				continue
			}
			if c.Raw != "" {
				if color {
					b.WriteString(ansiFor(c))
					last = ""
					b.WriteString(c.Raw)
				} else {
					b.WriteString(strings.Repeat(" ", maxInt(1, c.RawW)))
				}
				x += c.RawW - 1
				continue
			}
			r := c.R
			if r == 0 {
				r = ' '
			}
			if !color {
				if c.Img {
					b.WriteRune('▒')
				} else {
					b.WriteRune(r)
				}
				continue
			}
			s := ansiFor(c)
			if s != last {
				b.WriteString(s)
				last = s
			}
			b.WriteRune(r)
		}
		if color {
			b.WriteString("\x1b[0m")
		} else {
			line := b.String()
			line = strings.TrimRight(line, " ")
			b.Reset()
			b.WriteString(line)
		}
		out[y] = b.String()
	}
	return out
}

// GraphicsMode selects how images are drawn in interactive mode.
type GraphicsMode int

const (
	// GraphicsAuto detects the best protocol from the environment.
	GraphicsAuto GraphicsMode = iota
	// GraphicsNone loads and draws no images, which is the fastest default.
	GraphicsNone
	// GraphicsBlocks always uses Unicode half-block art.
	GraphicsBlocks
	// GraphicsKitty uses the kitty graphics protocol.
	GraphicsKitty
	// GraphicsITerm uses the iTerm2 OSC 1337 inline-image protocol.
	GraphicsITerm
)

// ParseGraphicsMode turns a -graphics flag value into a mode.
func ParseGraphicsMode(s string) GraphicsMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return GraphicsAuto
	case "none", "off", "no", "disabled":
		return GraphicsNone
	case "blocks", "block", "terminal", "text":
		return GraphicsBlocks
	case "kitty":
		return GraphicsKitty
	case "iterm", "iterm2", "iterm-2":
		return GraphicsITerm
	}
	return GraphicsAuto
}

// DetectGraphics resolves an auto mode to a concrete protocol.
func DetectGraphics(mode GraphicsMode) GraphicsMode {
	if mode != GraphicsAuto {
		return mode
	}
	if inMultiplexer() {
		// Escapes would be consumed by the multiplexer unless explicit
		// passthrough is configured, so prefer the always-safe fallback.
		return GraphicsBlocks
	}
	if kittyTerminal() {
		return GraphicsKitty
	}
	if itermTerminal() {
		return GraphicsITerm
	}
	return GraphicsBlocks
}

func inMultiplexer() bool {
	term := strings.ToLower(os.Getenv("TERM"))
	return os.Getenv("TMUX") != "" || os.Getenv("ZELLIJ") != "" ||
		strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux")
}

func kittyTerminal() bool {
	term := strings.ToLower(os.Getenv("TERM"))
	program := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	if strings.Contains(term, "kitty") || os.Getenv("KITTY_WINDOW_ID") != "" {
		return true
	}
	if strings.Contains(program, "wezterm") || os.Getenv("WEZTERM_PANE") != "" {
		return true
	}
	if strings.Contains(program, "ghostty") || os.Getenv("GHOSTTY_RESOURCES_DIR") != "" {
		return true
	}
	return false
}

// kittyKeyboardEnabled reports whether the terminal is expected to implement
// the kitty keyboard protocol. Multiplexers are excluded because their
// forwarding of the push/pop sequences is inconsistent.
func kittyKeyboardEnabled() bool {
	if inMultiplexer() || os.Getenv("STY") != "" {
		return false
	}
	if kittyTerminal() {
		return true
	}
	return strings.HasPrefix(strings.ToLower(os.Getenv("TERM")), "foot")
}

func itermTerminal() bool {
	program := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	return strings.Contains(program, "iterm") || strings.EqualFold(os.Getenv("LC_TERMINAL"), "iTerm2")
}

// gfxKey identifies a scaled image. ptr is a best-effort object identity while
// src keeps images from different pages apart if the address is reused.
type gfxKey struct {
	ptr  uintptr
	src  string
	cols int
	rows int
	pxW  int
	pxH  int
}

type itermKey struct {
	gfxKey
	rowStart int
	cropRows int
}

type kittyImage struct {
	id   int
	w, h int
}

// gfxRenderer writes images with a terminal graphics protocol.
type gfxRenderer struct {
	mode         GraphicsMode
	cellW, cellH int
	nextID       int
	kitty        map[gfxKey]*kittyImage
	scaled       map[gfxKey]*image.NRGBA
	encoded      map[gfxKey][]byte
	iterm        map[itermKey]string
}

// NewGFXRenderer creates a renderer for the given mode. It returns nil when
// half-block art should be used.
func NewGFXRenderer(mode GraphicsMode, cellW, cellH int) *gfxRenderer {
	mode = DetectGraphics(mode)
	if mode != GraphicsKitty && mode != GraphicsITerm {
		return nil
	}
	if cellW <= 0 {
		cellW = 8
	}
	if cellH <= 0 {
		cellH = 16
	}
	return &gfxRenderer{
		mode:    mode,
		cellW:   cellW,
		cellH:   cellH,
		nextID:  1,
		kitty:   make(map[gfxKey]*kittyImage),
		scaled:  make(map[gfxKey]*image.NRGBA),
		encoded: make(map[gfxKey][]byte),
		iterm:   make(map[itermKey]string),
	}
}

// Mode reports the active protocol (GraphicsBlocks when inactive).
func (r *gfxRenderer) Mode() GraphicsMode {
	if r == nil {
		return GraphicsBlocks
	}
	return r.mode
}

// TextSizing reports whether kitty's scaled-text protocol can be used. It is
// limited to real kitty because other kitty-graphics terminals do not
// implement OSC 66.
func (r *gfxRenderer) TextSizing() bool {
	if r == nil || r.mode != GraphicsKitty {
		return false
	}
	term := strings.ToLower(os.Getenv("TERM"))
	return strings.Contains(term, "kitty") || os.Getenv("KITTY_WINDOW_ID") != ""
}

// BeginFrame emits protocol bookkeeping before the text frame is written.
func (r *gfxRenderer) BeginFrame(sb *strings.Builder) {
	if r == nil || r.mode != GraphicsKitty {
		return
	}
	// Delete every visible placement but keep the transmitted image data so
	// the next frame can re-place images without re-uploading them.
	sb.WriteString("\x1b_Ga=d,q=2\x1b\\")
}

// ClearScreen clears screen text for a full repaint while preserving what can
// be reused. Kitty keeps transmitted image data across ED 1/0 (only ED 2/3
// frees it), so using those instead of ED 2 lets the next frame re-place cached
// images by id instead of re-scaling, re-encoding and re-uploading them after
// every scroll-triggered clear.
func (r *gfxRenderer) ClearScreen(sb *strings.Builder) {
	if sb == nil {
		return
	}
	if r == nil {
		sb.WriteString("\x1b[2J")
		return
	}
	if r.mode == GraphicsKitty {
		sb.WriteString("\x1b[1J\x1b[0J")
		return
	}
	r.Reset(sb)
	sb.WriteString("\x1b[2J")
}

// Reset frees terminal image storage and forgets cached transmissions. Use it
// when image data really becomes invalid (for example an explicit 2J clear);
// scroll-triggered repaints use ClearScreen, which keeps kitty data.
func (r *gfxRenderer) Reset(sb *strings.Builder) {
	if r == nil {
		return
	}
	if r.mode == GraphicsKitty && sb != nil {
		sb.WriteString("\x1b_Ga=d,d=A,q=2\x1b\\")
	}
	r.kitty = make(map[gfxKey]*kittyImage)
	r.iterm = make(map[itermKey]string)
}

// Clear removes any images this renderer placed.
func (r *gfxRenderer) Clear(sb *strings.Builder) {
	if r == nil {
		return
	}
	switch r.mode {
	case GraphicsKitty:
		sb.WriteString("\x1b_Ga=d,d=A,q=2\x1b\\")
	case GraphicsITerm:
		// Inline images are ordinary screen content and are cleared by the
		// caller's screen reset / alternate-screen switch.
	}
}

// maxCachedImages bounds the client-side image caches. Eviction never drops an
// image used by the frame being drawn, so a viewport that shows more images
// than this keeps them all instead of resetting and rebuilding the cache on
// every frame.
const maxCachedImages = 64

// PlaceAll emits every image placement for the current frame.
func (r *gfxRenderer) PlaceAll(sb *strings.Builder, placements []ImagePlacement) {
	if r == nil || len(placements) == 0 {
		return
	}
	keep := make(map[gfxKey]struct{}, len(placements))
	for _, p := range placements {
		if p.Img == nil || p.W <= 0 || p.H <= 0 || p.TotalRows <= 0 {
			continue
		}
		switch r.mode {
		case GraphicsKitty:
			r.placeKitty(sb, p, keep)
		case GraphicsITerm:
			r.placeITerm(sb, p, keep)
		}
	}
	r.prune(sb, keep)
}

// prune drops cached images that are not in the current frame once the cache
// exceeds its soft cap. Visible images are always retained.
func (r *gfxRenderer) prune(sb *strings.Builder, keep map[gfxKey]struct{}) {
	if len(r.scaled) <= maxCachedImages && len(r.kitty) <= maxCachedImages && len(r.encoded) <= maxCachedImages {
		return
	}
	for key, entry := range r.kitty {
		if _, ok := keep[key]; ok {
			continue
		}
		if r.mode == GraphicsKitty && sb != nil {
			fmt.Fprintf(sb, "\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", entry.id)
		}
		delete(r.kitty, key)
	}
	for key := range r.scaled {
		if _, ok := keep[key]; !ok {
			delete(r.scaled, key)
		}
	}
	for key := range r.encoded {
		if _, ok := keep[key]; !ok {
			delete(r.encoded, key)
		}
	}
}

func (r *gfxRenderer) placeKitty(sb *strings.Builder, p ImagePlacement, keep map[gfxKey]struct{}) {
	key, img := r.scaledImage(p)
	keep[key] = struct{}{}
	entry := r.kitty[key]
	if entry == nil {
		entry = &kittyImage{id: r.nextID, w: img.Bounds().Dx(), h: img.Bounds().Dy()}
		r.nextID++
		data := r.encoded[key]
		if data == nil {
			var err error
			data, err = encodePNG(img)
			if err != nil {
				return
			}
			r.encoded[key] = data
		}
		r.kittyTransmit(sb, entry.id, data)
		r.kitty[key] = entry
	}
	if entry.h <= 0 || p.TotalRows <= 0 {
		return
	}
	y0 := p.RowStart * entry.h / p.TotalRows
	y1 := (p.RowStart + p.H) * entry.h / p.TotalRows
	if y1 <= y0 {
		y1 = y0 + 1
	}
	if y1 > entry.h {
		y1 = entry.h
	}
	fmt.Fprintf(sb, "\x1b[%d;%dH", p.Y+1, p.X+1)
	fmt.Fprintf(sb, "\x1b_Ga=p,i=%d,q=2,c=%d,r=%d,x=0,y=%d,w=%d,h=%d,C=1\x1b\\",
		entry.id, p.W, p.H, y0, entry.w, y1-y0)
}

func (r *gfxRenderer) placeITerm(sb *strings.Builder, p ImagePlacement, keep map[gfxKey]struct{}) {
	key, img := r.scaledImage(p)
	keep[key] = struct{}{}
	ik := itermKey{gfxKey: key, rowStart: p.RowStart, cropRows: p.H}
	payload, ok := r.iterm[ik]
	if !ok {
		b := img.Bounds()
		y0 := p.RowStart * b.Dy() / p.TotalRows
		y1 := (p.RowStart + p.H) * b.Dy() / p.TotalRows
		if y1 <= y0 {
			y1 = y0 + 1
		}
		if y1 > b.Dy() {
			y1 = b.Dy()
		}
		crop := img.SubImage(image.Rect(0, y0, b.Dx(), y1)).(*image.NRGBA)
		data, err := encodePNG(crop)
		if err != nil {
			return
		}
		payload = base64.StdEncoding.EncodeToString(data)
		r.iterm[ik] = payload
	}
	fmt.Fprintf(sb, "\x1b7\x1b[%d;%dH\x1b]1337;File=inline=1;width=%d;height=%d;preserveAspectRatio=0;doNotMoveCursor=1:%s\x07\x1b8",
		p.Y+1, p.X+1, p.W, p.H, payload)
}

// scaledImage returns the cache key and a downscaled NRGBA image sized for the
// placement's full cell box.
func (r *gfxRenderer) scaledImage(p ImagePlacement) (gfxKey, *image.NRGBA) {
	pxW, pxH := r.targetPixels(p.W, p.TotalRows)
	key := gfxKey{
		ptr:  imagePointer(p.Img),
		src:  p.Src,
		cols: p.W,
		rows: p.TotalRows,
		pxW:  pxW,
		pxH:  pxH,
	}
	if img, ok := r.scaled[key]; ok {
		return key, img
	}
	img := scaleImage(p.Img, pxW, pxH)
	r.scaled[key] = img
	return key, img
}

// targetPixels chooses a bounded pixel size for a cell box while preserving its
// aspect ratio, so cropping stays proportional.
func (r *gfxRenderer) targetPixels(cols, rows int) (int, int) {
	const maxDim = 2048
	w := cols * r.cellW
	h := rows * r.cellH
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	scale := 1.0
	if w > maxDim {
		scale = float64(maxDim) / float64(w)
	}
	if h > maxDim {
		if s := float64(maxDim) / float64(h); s < scale {
			scale = s
		}
	}
	if scale < 1 {
		w = maxInt(1, int(float64(w)*scale))
		h = maxInt(1, int(float64(h)*scale))
	}
	return w, h
}

func (r *gfxRenderer) kittyTransmit(sb *strings.Builder, id int, data []byte) {
	const chunk = 4096
	encoded := base64.StdEncoding.EncodeToString(data)
	first := true
	for len(encoded) > 0 {
		n := chunk
		if n > len(encoded) {
			n = len(encoded)
		}
		part := encoded[:n]
		encoded = encoded[n:]
		more := 0
		if len(encoded) > 0 {
			more = 1
		}
		if first {
			fmt.Fprintf(sb, "\x1b_Ga=t,f=100,q=2,i=%d,m=%d;%s\x1b\\", id, more, part)
			first = false
		} else {
			fmt.Fprintf(sb, "\x1b_Gm=%d;%s\x1b\\", more, part)
		}
	}
	if first {
		fmt.Fprintf(sb, "\x1b_Ga=t,f=100,q=2,i=%d,m=0;\x1b\\", id)
	}
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func imagePointer(img image.Image) uintptr {
	if img == nil {
		return 0
	}
	v := reflect.ValueOf(img)
	switch v.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return v.Pointer()
	}
	return 0
}

// scaleImage resizes src to w x h with area sampling. The result is always
// straight-alpha NRGBA so it can be encoded as PNG.
func scaleImage(src image.Image, w, h int) *image.NRGBA {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 {
		return dst
	}
	for dy := 0; dy < h; dy++ {
		y0 := b.Min.Y + dy*sh/h
		y1 := b.Min.Y + (dy+1)*sh/h
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for dx := 0; dx < w; dx++ {
			x0 := b.Min.X + dx*sw/w
			x1 := b.Min.X + (dx+1)*sw/w
			if x1 <= x0 {
				x1 = x0 + 1
			}
			sx := maxInt(1, (x1-x0)/4)
			sy := maxInt(1, (y1-y0)/4)
			var sr, sg, sb, sa uint64
			var n uint64
			for yy := y0; yy < y1; yy += sy {
				for xx := x0; xx < x1; xx += sx {
					r, g, bl, a := src.At(xx, yy).RGBA()
					sr += uint64(r)
					sg += uint64(g)
					sb += uint64(bl)
					sa += uint64(a)
					n++
				}
			}
			if n == 0 {
				continue
			}
			alpha := uint8(sa / n / 257)
			if alpha == 0 {
				continue
			}
			unpremul := func(v uint64) uint8 {
				x := v / n / 257
				y := x * 255 / uint64(alpha)
				if y > 255 {
					y = 255
				}
				return uint8(y)
			}
			dst.SetNRGBA(dx, dy, color.NRGBA{
				R: unpremul(sr),
				G: unpremul(sg),
				B: unpremul(sb),
				A: alpha,
			})
		}
	}
	return dst
}

const maxSVGDim = 1024

var errBadSVG = errors.New("invalid SVG")

// isSVG reports whether fetched data is an SVG image.
func isSVG(ctype string, data []byte) bool {
	ct := strings.ToLower(ctype)
	if strings.Contains(ct, "image/svg+xml") {
		return true
	}
	head := strings.TrimSpace(string(data[:minInt(len(data), 1024)]))
	if strings.HasPrefix(strings.ToLower(head), "<svg") {
		return true
	}
	if strings.HasPrefix(head, "<?xml") && strings.Contains(strings.ToLower(head), "<svg") {
		return true
	}
	return false
}

// decodeSVG rasterizes an SVG into an RGBA image, bounded in size.
func decodeSVG(data []byte) (image.Image, error) {
	if len(data) == 0 {
		return nil, errBadSVG
	}
	icon, err := oksvg.ReadIconStream(bytes.NewReader(data), oksvg.IgnoreErrorMode)
	if err != nil {
		return nil, err
	}
	w, h := icon.ViewBox.W, icon.ViewBox.H
	if w <= 0 || h <= 0 {
		w, h = 300, 150
	}
	scale := 1.0
	if w > maxSVGDim {
		scale = float64(maxSVGDim) / w
	}
	if h > maxSVGDim {
		if s := float64(maxSVGDim) / h; s < scale {
			scale = s
		}
	}
	w = float64(maxInt(1, int(w*scale)))
	h = float64(maxInt(1, int(h*scale)))
	rgba := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	draw.Draw(rgba, rgba.Bounds(), image.Transparent, image.Point{}, draw.Src)
	scanner := rasterx.NewScannerGV(int(w), int(h), rgba, rgba.Bounds())
	raster := rasterx.NewDasher(int(w), int(h), scanner)
	icon.SetTarget(0, 0, w, h)
	icon.Draw(raster, 1.0)
	return rgba, nil
}

// loadInlineSVGs rasterizes inline <svg> elements into their Node.Img field so
// layout can treat them as images.
func (l *Loader) loadInlineSVGs(root *Node) {
	if l.noImages {
		return
	}
	nodes := root.FindAll(func(n *Node) bool { return n.Tag == "svg" })
	for _, n := range nodes {
		if n.Img != nil || n.ImgFailed {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(n.Attr("aria-hidden")), "true") {
			continue
		}
		img, err := decodeSVG([]byte(serializeSVG(n)))
		if err != nil {
			n.ImgFailed = true
			continue
		}
		n.Img = img
		n.ImgSrc = "inline-svg"
	}
}

var svgTagCase = map[string]string{
	"lineargradient":      "linearGradient",
	"radialgradient":      "radialGradient",
	"clippath":            "clipPath",
	"textpath":            "textPath",
	"foreignobject":       "foreignObject",
	"feblend":             "feBlend",
	"fecolormatrix":       "feColorMatrix",
	"fecomponenttransfer": "feComponentTransfer",
	"fecomposite":         "feComposite",
	"feconvolvematrix":    "feConvolveMatrix",
	"fediffuselighting":   "feDiffuseLighting",
	"fedisplacementmap":   "feDisplacementMap",
	"fedistantlight":      "feDistantLight",
	"fedropshadow":        "feDropShadow",
	"feflood":             "feFlood",
	"fefunca":             "feFuncA",
	"fefuncb":             "feFuncB",
	"fefuncg":             "feFuncG",
	"fefuncr":             "feFuncR",
	"fegaussianblur":      "feGaussianBlur",
	"feimage":             "feImage",
	"femerge":             "feMerge",
	"femergenode":         "feMergeNode",
	"femorphology":        "feMorphology",
	"feoffset":            "feOffset",
	"fepointlight":        "fePointLight",
	"fespecularlighting":  "feSpecularLighting",
	"fespotlight":         "feSpotLight",
	"fetile":              "feTile",
	"feturbulence":        "feTurbulence",
}

var svgAttrCase = map[string]string{
	"viewbox":                   "viewBox",
	"preserveaspectratio":       "preserveAspectRatio",
	"gradientunits":             "gradientUnits",
	"gradienttransform":         "gradientTransform",
	"patternunits":              "patternUnits",
	"patterncontentunits":       "patternContentUnits",
	"patterntransform":          "patternTransform",
	"clippathunits":             "clipPathUnits",
	"maskunits":                 "maskUnits",
	"maskcontentunits":          "maskContentUnits",
	"markerunits":               "markerUnits",
	"markerwidth":               "markerWidth",
	"markerheight":              "markerHeight",
	"refx":                      "refX",
	"refy":                      "refY",
	"spreadmethod":              "spreadMethod",
	"startoffset":               "startOffset",
	"textlength":                "textLength",
	"lengthadjust":              "lengthAdjust",
	"baseprofile":               "baseProfile",
	"zoomandpan":                "zoomAndPan",
	"attributename":             "attributeName",
	"attributetype":             "attributeType",
	"repeatcount":               "repeatCount",
	"repeatdur":                 "repeatDur",
	"keytimes":                  "keyTimes",
	"keysplines":                "keySplines",
	"keypoints":                 "keyPoints",
	"calcmode":                  "calcMode",
	"pathlength":                "pathLength",
	"requiredfeatures":          "requiredFeatures",
	"requiredextensions":        "requiredExtensions",
	"systemlanguage":            "systemLanguage",
	"externalresourcesrequired": "externalResourcesRequired",
	"xml:space":                 "xml:space",
}

func canonicalSVGTag(tag string) string {
	if v, ok := svgTagCase[tag]; ok {
		return v
	}
	return tag
}

func canonicalSVGAttr(name string) string {
	lower := strings.ToLower(name)
	if v, ok := svgAttrCase[lower]; ok {
		return v
	}
	return lower
}

// serializeSVG turns the parsed inline SVG subtree back into XML. HTML parsing
// lowercases names, so known SVG camelCase names are restored for oksvg.
func serializeSVG(n *Node) string {
	var b strings.Builder
	var walk func(*Node)
	walk = func(x *Node) {
		if x == nil {
			return
		}
		if x.Type == TextNode {
			b.WriteString(textEscape(x.Text))
			return
		}
		if x.Type != ElementNode {
			return
		}
		tag := canonicalSVGTag(x.Tag)
		b.WriteString("<")
		b.WriteString(tag)
		if x.Tag == "svg" && x.Attr("xmlns") == "" {
			b.WriteString(` xmlns="http://www.w3.org/2000/svg"`)
		}
		for _, a := range x.Attrs {
			b.WriteString(" ")
			b.WriteString(canonicalSVGAttr(a.Key))
			b.WriteString(`="`)
			b.WriteString(EscapeHTML(a.Value))
			b.WriteString(`"`)
		}
		if len(x.Children) == 0 {
			b.WriteString("/>")
			return
		}
		b.WriteString(">")
		for _, c := range x.Children {
			walk(c)
		}
		b.WriteString("</")
		b.WriteString(tag)
		b.WriteString(">")
	}
	walk(n)
	return b.String()
}
