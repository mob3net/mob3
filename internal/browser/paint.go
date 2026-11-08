package browser

import (
	"image"
	"strconv"
	"strings"
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
