package engine

import (
	"browser/internal/protocol"
	"browser/internal/textwidth"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"math"
	"strings"

	"github.com/go-gfx/oksvg"
	"github.com/srwiley/rasterx"
)

// Cell is one terminal cell in the framebuffer.
type Cell struct {
	R      rune
	FG, BG Color
	// Run is a 1-based index into Grid.Runs for the scaled text starting at
	// this cell; Skip marks the cells covered by the rest of that run.
	Run       uint16
	Bold      bool
	Italic    bool
	Underline bool
	Strike    bool
	Reverse   bool
	Cont      bool
	Img       bool
	Skip      bool
}

// ScaledRun is the structured form of a kitty scaled-text run. The terminal
// escape sequence is built only when the grid is encoded; page state never
// carries a raw escape.
type ScaledRun struct {
	X, Y      int
	Cells     int
	Scale     int
	Text      string
	FG, BG    Color
	Bold      bool
	Italic    bool
	Underline bool
	Strike    bool
	Reverse   bool
}

// Grid is a terminal framebuffer.
type Grid struct {
	W, H  int
	Cells []Cell
	// Runs holds the scaled-text runs referenced by Cell.Run, in index order.
	Runs []ScaledRun
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
	g.Reset(fg, bg)
	return g
}

// Reset blanks every cell and drops all scaled runs, keeping the buffers for
// reuse between frames.
func (g *Grid) Reset(fg, bg Color) {
	c := Cell{R: ' ', FG: fg, BG: bg}
	for i := range g.Cells {
		g.Cells[i] = c
	}
	g.Runs = g.Runs[:0]
}

// ReuseGrid returns g ready for a frame of the given size, allocating only
// when the size changed.
func ReuseGrid(g *Grid, w, h int, fg, bg Color) *Grid {
	if g == nil || g.W != w || g.H != h {
		return NewGrid(w, h, fg, bg)
	}
	g.Reset(fg, bg)
	return g
}

// BlitGrid copies src into dst starting at row y. Cell run indexes refer to
// src.Runs, so the caller must copy those runs in the same order.
func BlitGrid(dst, src *Grid, y int) {
	if dst == nil || src == nil || y < 0 || y >= dst.H {
		return
	}
	rows := MinInt(src.H, dst.H-y)
	w := MinInt(src.W, dst.W)
	for r := 0; r < rows; r++ {
		copy(dst.Cells[(y+r)*dst.W:(y+r)*dst.W+w], src.Cells[r*src.W:r*src.W+w])
	}
}

// Fill paints a rectangle of background cells. A translucent fill is composited
// over whatever background each cell already has, so a 20%-black scrim reads as
// light grey on a white page instead of solid black.
func (g *Grid) Fill(x, y, w, h int, c Color) {
	if !c.Set || c.A == 0 {
		return
	}
	x0, y0 := MaxInt(x, 0), MaxInt(y, 0)
	x1, y1 := MinInt(x+w, g.W), MinInt(y+h, g.H)
	for yy := y0; yy < y1; yy++ {
		row := yy * g.W
		for xx := x0; xx < x1; xx++ {
			cell := &g.Cells[row+xx]
			if c.A < 255 {
				cell.BG = blendOver(cell.BG, c)
			} else {
				cell.BG = c
			}
			cell.Cont = false
			cell.Img = false
			cell.Run = 0
			cell.Skip = false
		}
	}
}

// blendOver composites src over dst using straight-alpha source-over.
func blendOver(dst, src Color) Color {
	if src.A >= 255 {
		return src
	}
	if !dst.Set || dst.A == 0 {
		src.A = 255
		return src
	}
	sa := uint32(src.A)
	da := uint32(dst.A) * uint32(255-src.A) / 255
	outA := sa + da
	if outA == 0 {
		return Color{}
	}
	mix := func(s, d uint8) uint8 {
		return uint8((uint32(s)*sa + uint32(d)*da) / outA)
	}
	return Color{R: mix(src.R, dst.R), G: mix(src.G, dst.G), B: mix(src.B, dst.B), A: uint8(outA), Set: true}
}

// Highlight recolors the background and foreground of existing cells without
// changing their glyphs, used for focused form controls.
func (g *Grid) Highlight(x, y, w, h int, bg, fg Color) {
	x0, y0 := MaxInt(x, 0), MaxInt(y, 0)
	x1, y1 := MinInt(x+w, g.W), MinInt(y+h, g.H)
	for yy := y0; yy < y1; yy++ {
		row := yy * g.W
		for xx := x0; xx < x1; xx++ {
			cell := &g.Cells[row+xx]
			cell.BG = bg
			cell.FG = fg
		}
	}
}

// ClearGlyphs blanks the characters in a rectangle while keeping each cell's
// background, used to reserve the scrollbar column without erasing page fills.
func (g *Grid) ClearGlyphs(x, y, w, h int) {
	x0, y0 := MaxInt(x, 0), MaxInt(y, 0)
	x1, y1 := MinInt(x+w, g.W), MinInt(y+h, g.H)
	for yy := y0; yy < y1; yy++ {
		row := yy * g.W
		for xx := x0; xx < x1; xx++ {
			cell := &g.Cells[row+xx]
			cell.R = ' '
			cell.FG = cell.BG
			cell.Bold = false
			cell.Italic = false
			cell.Underline = false
			cell.Strike = false
			cell.Cont = false
			cell.Img = false
			cell.Run = 0
			cell.Skip = false
		}
	}
}

// FillHGradient fills a rectangle with a left-to-right color ramp.
func (g *Grid) FillHGradient(x, y, w, h int, from, to Color) {
	if !from.Set || !to.Set || w <= 0 || h <= 0 {
		return
	}
	x0, x1 := MaxInt(x, 0), MinInt(x+w, g.W)
	y0, y1 := MaxInt(y, 0), MinInt(y+h, g.H)
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
	rw := textwidth.RuneWidth(r)
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
	c.Reverse = false
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
	c.Run = 0
	c.Skip = false
	if rw == 2 && x+1 < g.W {
		c2 := &g.Cells[y*g.W+x+1]
		c2.R = 0
		c2.Cont = true
		c2.BG = c.BG
		c2.FG = c.FG
		c2.Img = false
		c2.Run = 0
		c2.Skip = false
	}
}

func (g *Grid) Text(x, y int, s string, st *Style, fg Color) {
	if st == nil {
		st = defaultStyle()
	}
	for _, r := range s {
		rw := textwidth.RuneWidth(r)
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
// text consumes scale*textwidth.StringWidth(s) columns and scale rows; covered cells are
// marked so Lines does not paint over the multicell.
func (g *Grid) TextBig(x, y int, s string, st *Style, fg, bg Color, scale int) {
	s = StripTerminalControls(s)
	if s == "" || x < 0 || y < 0 || y >= g.H || x >= g.W {
		return
	}
	if scale < 2 {
		g.Text(x, y, s, st, fg)
		return
	}
	base := textwidth.StringWidth(s)
	if base <= 0 {
		return
	}
	if room := (g.W - x) / scale; base > room {
		s = textwidth.Truncate(s, room)
		base = textwidth.StringWidth(s)
	}
	total := base * scale
	if base <= 0 || total <= 0 {
		return
	}
	if strings.TrimSpace(s) == "" {
		// Scaling a whitespace-only run draws its underline at the unscaled
		// baseline, which shows up as stray lines inside or below large text.
		// Leave plain blank cells instead; the surrounding words keep their
		// own scaled underline.
		var blank Style
		if st != nil {
			blank = *st
		}
		blank.Underline = false
		blank.Strike = false
		for ry := 0; ry < scale; ry++ {
			for i := 0; i < total; i++ {
				g.Put(x+i, y+ry, ' ', &blank, fg, bg)
			}
		}
		return
	}
	if len(g.Runs) >= 0xffff {
		// Cell.Run is 16-bit; fall back to normal text rather than corrupting
		// the run index on a pathologically run-heavy frame.
		g.Text(x, y, s, st, fg)
		return
	}
	c := &g.Cells[y*g.W+x]
	c.Run = uint16(len(g.Runs) + 1)
	c.FG = fg
	if bg.Set {
		c.BG = bg
	}
	c.R = ' '
	c.Cont = false
	c.Img = false
	c.Skip = false
	c.Reverse = false
	run := ScaledRun{
		X: x, Y: y, Cells: total, Scale: scale, Text: s,
		FG: fg, BG: c.BG,
	}
	if st != nil {
		c.Bold = st.Bold
		c.Italic = st.Italic
		c.Underline = st.Underline
		c.Strike = st.Strike
		run.Bold, run.Italic, run.Underline, run.Strike = st.Bold, st.Italic, st.Underline, st.Strike
	} else {
		c.Bold, c.Italic, c.Underline, c.Strike = false, false, false, false
	}
	g.Runs = append(g.Runs, run)
	mark := func(cx, cy int) {
		if cx < 0 || cx >= g.W || cy < 0 || cy >= g.H {
			return
		}
		cc := &g.Cells[cy*g.W+cx]
		cc.Run = 0
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
	// Fixed chrome backgrounds are painted after the page. Native terminal
	// images are composited above the text grid and cannot be erased by those
	// fills, so images that scroll under fixed chrome fall back to half-block
	// art instead of a placement.
	var chrome []Rect
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if (!f.Fixed && f.Sticky == 0) || f.Kind != FragFill || !Opaque(f.Color) {
			continue
		}
		y := f.Y - scroll + topRow
		y0 := MaxInt(y, topRow)
		y1 := MinInt(y+f.H, bottomRow)
		if y1 <= y0 {
			continue
		}
		chrome = append(chrome, Rect{X: f.X, Y: y0, W: f.W, H: y1 - y0})
	}
	hitsChrome := func(x, y, w, h int) bool {
		for _, r := range chrome {
			if x < r.X+r.W && x+w > r.X && y < r.Y+r.H && y+h > r.Y {
				return true
			}
		}
		return false
	}
	paintBlocks := func(f *Frag, y0, y1 int) bool {
		return hitsChrome(f.X, y0, f.W, y1-y0)
	}
	// Native placements are composited above the cell grid, so an image with
	// page text painted over it (a caption, a tab strip on a hero photo) must
	// fall back to half-block art as well; a placement would hide the text.
	var overlayRows map[int][]Rect
	if placements != nil {
		overlayRows = make(map[int][]Rect)
		for i := range ly.Frags {
			f := &ly.Frags[i]
			if f.Kind != FragText || f.Text == "" || f.W <= 0 {
				continue
			}
			y := f.Y - scroll + topRow
			h := MaxInt(1, f.H)
			if bigText && f.Style != nil {
				if s := bigTextScale(f.Style, layoutZoom); s > 1 {
					h = MaxInt(h, s)
				}
			}
			for r := MaxInt(y, topRow); r < MinInt(y+h, bottomRow); r++ {
				overlayRows[r] = append(overlayRows[r], Rect{X: f.X, Y: r, W: f.W, H: 1})
			}
		}
	}
	hasTextOverlay := func(x, y, w, h int) bool {
		for r := y; r < y+h; r++ {
			for _, tr := range overlayRows[r] {
				if x < tr.X+tr.W && x+w > tr.X {
					return true
				}
			}
		}
		return false
	}
	paintOne := func(f *Frag) {
		y := f.Y - scroll + topRow
		if y >= bottomRow {
			return
		}
		if y+f.H <= topRow || y >= g.H {
			return
		}
		switch f.Kind {
		case FragFill, FragFrame:
			clippedTop := MaxInt(y, topRow)
			clippedBot := MinInt(y+f.H, bottomRow)
			if f.Fixed || f.Sticky != 0 {
				// Viewport-anchored chrome (fixed or sticky) paints after the
				// page, so its fills must also erase glyphs from content that
				// scrolled underneath.
				g.ClearGlyphs(f.X, clippedTop, f.W, clippedBot-clippedTop)
			}
			g.Fill(f.X, clippedTop, f.W, clippedBot-clippedTop, f.Color)
		case FragText:
			if y < topRow || y >= bottomRow {
				return
			}
			st := f.Style
			fg := st.Color
			if !fg.Set {
				fg = pageFG
			}
			scale := 1
			if bigText {
				scale = bigTextScale(st, layoutZoom)
			}
			if scale > 1 && (y+scale > bottomRow || hitsChrome(f.X, y, f.W*scale, scale)) {
				// A scaled run from the last content row would be drawn over
				// the footer or another chrome row, and a run touching fixed
				// chrome (such as a cookie bar) would spill its large glyphs
				// over that fill because the terminal draws scaled text above
				// the cell grid. Keep it normal size in both cases.
				scale = 1
			}
			bg := textBackground(f)
			if Opaque(bg) {
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
			fg = ReadableFG(fg, effBG)
			if scale > 1 {
				g.TextBig(f.X, y, f.Text, st, fg, bg, scale)
			} else {
				g.Text(f.X, y, f.Text, st, fg)
			}
		case FragLink:
			if f.Link != nil && f.Link == focus {
				g.Fill(f.X, f.Y, f.W, 1, focusBG)
				g.Fill(f.X, f.Y+f.H-1, f.W, 1, focusBG)
				g.Fill(f.X, f.Y, 1, f.H, focusBG)
				g.Fill(f.X+f.W-1, f.Y, 1, f.H, focusBG)
			}
		case FragImage:
			if f.Img == nil {
				return
			}
			y0 := MaxInt(y, topRow)
			y1 := MinInt(y+f.H, bottomRow)
			if y0 >= y1 {
				return
			}
			imgTop := y0 - y
			if f.Background {
				// CSS backgrounds are always half-block art: native terminal
				// placements composite above the cell grid and would cover the
				// text and links drawn on top of the image.
				if ly.BackgroundsOff {
					// Keep the box as a neutral placeholder rather than blanking
					// it. Site text is often white (designed for a dark photo) or
					// black; mid grey keeps either readable through the contrast
					// fallback while making the hidden image obvious.
					g.Fill(f.X, y0, f.W, y1-y0, backgroundPlaceholderBG)
					return
				}
				PaintImage(g, f.Img, f.Crop, f.X, y0, f.W, y1-y0, imgTop, f.H)
				return
			}
			if placements != nil && !hasTextOverlay(f.X, y0, f.W, y1-y0) && (f.Fixed || !paintBlocks(f, y0, y1)) {
				src := ""
				if f.Node != nil {
					src = f.Node.ImgSrc
				}
				*placements = append(*placements, ImagePlacement{
					Img: f.Img, Src: src, X: f.X, Y: y0, W: f.W, H: y1 - y0,
					RowStart: imgTop, TotalRows: f.H,
				})
				return
			}
			PaintImage(g, f.Img, f.Crop, f.X, y0, f.W, y1-y0, imgTop, f.H)
			if f.Link != nil && f.Link == focus {
				g.Fill(f.X, y, f.W, 1, focusBG)
				g.Fill(f.X, y+f.H-1, f.W, 1, focusBG)
				g.Fill(f.X, y, 1, f.H, focusBG)
				g.Fill(f.X+f.W-1, y, 1, f.H, focusBG)
			}
		}
	}
	// Paint the scrolling page first, sticky boxes above it (they overlap
	// content while scrolled), and viewport-anchored (fixed) chrome last.
	// A page with z-index uses the precomputed order, which sorts within each
	// phase.
	if len(ly.PaintOrder()) > 0 {
		for _, i := range ly.PaintOrder() {
			paintOne(&ly.Frags[i])
		}
		return
	}
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if !f.Fixed && f.Sticky == 0 {
			paintOne(f)
		}
	}
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if !f.Fixed && f.Sticky != 0 {
			paintOne(f)
		}
	}
	for i := range ly.Frags {
		if ly.Frags[i].Fixed {
			paintOne(&ly.Frags[i])
		}
	}
}

// paintImage draws an image as half-block art. rowStart and totalRows describe
// which part of the (possibly clipped) image we are drawing.
func PaintImage(g *Grid, img image.Image, crop image.Rectangle, x, y, w, h, rowStart, totalRows int) {
	b := img.Bounds()
	if crop.Dx() > 0 && crop.Dy() > 0 {
		b = crop
	}
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
	y0 := b.Min.Y + srcRow*b.Dy()/MaxInt(1, totalRows)
	y1 := b.Min.Y + (srcRow+1)*b.Dy()/MaxInt(1, totalRows)
	if y1 <= y0 {
		y1 = y0 + 1
	}
	return rowRegion{y0, y1}
}

func sampleColumn(img image.Image, b image.Rectangle, col, totalCols int, reg rowRegion) Color {
	x0 := b.Min.X + col*b.Dx()/MaxInt(1, totalCols)
	x1 := b.Min.X + (col+1)*b.Dx()/MaxInt(1, totalCols)
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
	if f.Style != nil && Opaque(f.Style.Background) {
		return f.Style.Background
	}
	for n := f.Node; n != nil; n = n.Parent {
		st := n.Style
		if st == nil {
			continue
		}
		if Opaque(st.Background) {
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

// StripTerminalControls drops C0/C1 control characters before text is embedded
// in an OSC 66 escape. Ordinary Grid.Text drops them via zero rune width, but
// scaled text is written raw inside the escape sequence.
func StripTerminalControls(s string) string {
	return protocol.StripControls(s)
}

// ReadableFG keeps text legible when author CSS leaves a near-invisible
// foreground/background pair (for example white text whose background came
// from an unsupported gradient or CSS variable).
// backgroundPlaceholderBG is the mid-grey fill used when CSS background images
// are switched off. #767676 is the tone where both black and white text keep
// roughly 4.6:1 contrast, so the page's own text colour stays legible without
// forcing the contrast fallback to flip it.
var backgroundPlaceholderBG = RGB(0x76, 0x76, 0x76)

// BackgroundPlaceholderBG is the neutral fill used when backgrounds are hidden.
var BackgroundPlaceholderBG = backgroundPlaceholderBG

const maxSVGDim = 1024

// MaxSVGDim caps one SVG raster dimension.
const MaxSVGDim = maxSVGDim

var errBadSVG = errors.New("invalid SVG")

// ErrBadSVG marks malformed SVG input.
var ErrBadSVG = errBadSVG

// IsSVG reports whether fetched data is an SVG image.
func IsSVG(ctype string, data []byte) bool {
	ct := strings.ToLower(ctype)
	if strings.Contains(ct, "image/svg+xml") {
		return true
	}
	head := strings.TrimSpace(string(data[:MinInt(len(data), 1024)]))
	if strings.HasPrefix(strings.ToLower(head), "<svg") {
		return true
	}
	if strings.HasPrefix(head, "<?xml") && strings.Contains(strings.ToLower(head), "<svg") {
		return true
	}
	return false
}

// DecodeSVG rasterizes an SVG into an RGBA image, bounded in size.
func DecodeSVG(data []byte) (image.Image, error) {
	return DecodeSVGWithin(data, 0)
}

// decodeSVGWithin is DecodeSVG with a cap on the rasterized pixel count. It
// scales the raster down rather than failing when the budget is smaller.
func recoverImage(fn func() (image.Image, error)) (img image.Image, err error) {
	defer func() {
		if r := recover(); r != nil {
			img, err = nil, fmt.Errorf("image decoder panic: %v", r)
		}
	}()
	return fn()
}

func DecodeSVGWithin(data []byte, maxPixels int64) (image.Image, error) {
	return recoverImage(func() (image.Image, error) { return renderSVGWithin(data, maxPixels) })
}

func renderSVG(data []byte) (image.Image, error) {
	return renderSVGWithin(data, 0)
}

func renderSVGWithin(data []byte, maxPixels int64) (image.Image, error) {
	if len(data) == 0 {
		return nil, ErrBadSVG
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
	if maxPixels > 0 && w*h*scale*scale > float64(maxPixels) {
		scale *= math.Sqrt(float64(maxPixels) / (w * h * scale * scale))
	}
	w = float64(MaxInt(1, int(w*scale)))
	h = float64(MaxInt(1, int(h*scale)))
	rgba := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	draw.Draw(rgba, rgba.Bounds(), image.Transparent, image.Point{}, draw.Src)
	scanner := rasterx.NewScannerGV(int(w), int(h), rgba, rgba.Bounds())
	raster := rasterx.NewDasher(int(w), int(h), scanner)
	icon.SetTarget(0, 0, w, h)
	icon.Draw(raster, 1.0)
	return rgba, nil
}
