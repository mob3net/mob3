package frontend

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sync"

	"browser/internal/protocol"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

const (
	pngCellW = 8
	pngCellH = 16
)

type pngFonts struct {
	regular, bold, italic, boldItalic font.Face
}

var (
	pngFontOnce sync.Once
	pngRegular  *sfnt.Font
	pngBold     *sfnt.Font
	pngItalic   *sfnt.Font
	pngBoldItal *sfnt.Font
	pngFontErr  error
)

func parsePNGFonts() {
	pngFontOnce.Do(func() {
		pngRegular, pngFontErr = opentype.Parse(gomono.TTF)
		if pngFontErr != nil {
			return
		}
		pngBold, pngFontErr = opentype.Parse(gomonobold.TTF)
		if pngFontErr != nil {
			return
		}
		pngItalic, pngFontErr = opentype.Parse(gomonoitalic.TTF)
		if pngFontErr != nil {
			return
		}
		pngBoldItal, pngFontErr = opentype.Parse(gomonobolditalic.TTF)
	})
}

func (f pngFonts) close() {
	if f.regular != nil {
		_ = f.regular.Close()
	}
	if f.bold != nil && f.bold != f.regular {
		_ = f.bold.Close()
	}
	if f.italic != nil && f.italic != f.regular {
		_ = f.italic.Close()
	}
	if f.boldItalic != nil && f.boldItalic != f.regular {
		_ = f.boldItalic.Close()
	}
}

func pngFaces(scale int) (pngFonts, error) {
	parsePNGFonts()
	if pngFontErr != nil {
		return pngFonts{}, pngFontErr
	}
	opts := &opentype.FaceOptions{Size: 12 * float64(scale), DPI: 72, Hinting: font.HintingFull}
	face := func(f *sfnt.Font) font.Face {
		if f == nil {
			return nil
		}
		ff, err := opentype.NewFace(f, opts)
		if err != nil {
			pngFontErr = err
			return nil
		}
		return ff
	}
	out := pngFonts{
		regular:    face(pngRegular),
		bold:       face(pngBold),
		italic:     face(pngItalic),
		boldItalic: face(pngBoldItal),
	}
	if pngFontErr != nil {
		out.close()
		return pngFonts{}, pngFontErr
	}
	return out, nil
}

// RenderPNG renders a protocol frame as a PNG image. Scale 1 uses an 8x16
// pixel cell; scale 2 to 4 doubles or quadruples both dimensions.
func RenderPNG(f protocol.Frame, scale int) ([]byte, error) {
	if f.W < 1 || f.H < 1 || len(f.Cells) != f.W*f.H {
		return nil, errors.New("empty frame")
	}
	if scale < 1 {
		scale = 1
	}
	if scale > 4 {
		scale = 4
	}
	cw, ch := pngCellW*scale, pngCellH*scale
	img := image.NewRGBA(image.Rect(0, 0, f.W*cw, f.H*ch))
	pageBG := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	if f.Cells[0].BG.Set {
		pageBG = colorRGBA(f.Cells[0].BG)
	}
	draw.Draw(img, img.Bounds(), &image.Uniform{C: pageBG}, image.Point{}, draw.Src)

	faces, err := pngFaces(scale)
	if err != nil {
		return nil, err
	}
	defer faces.close()
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			c := f.Cells[y*f.W+x]
			if c.Flags&(protocol.CellSkip|protocol.CellCont) != 0 {
				continue
			}
			bg := pageBG
			if c.BG.Set {
				bg = colorRGBA(c.BG)
			}
			rect := image.Rect(x*cw, y*ch, (x+1)*cw, (y+1)*ch)
			draw.Draw(img, rect, &image.Uniform{C: bg}, image.Point{}, draw.Src)
			if c.Flags&protocol.CellImg != 0 {
				drawImageCell(img, rect, c, bg)
				continue
			}
			if c.Run != 0 {
				continue
			}
			r := c.R
			if r == 0 {
				continue
			}
			fg := color.RGBA{R: 31, G: 35, B: 40, A: 255}
			if c.FG.Set {
				fg = colorRGBA(c.FG)
			}
			face := faces.regular
			bold := c.Attrs&protocol.AttrBold != 0
			italic := c.Attrs&protocol.AttrItalic != 0
			switch {
			case bold && italic:
				face = faces.boldItalic
			case bold:
				face = faces.bold
			case italic:
				face = faces.italic
			}
			m := face.Metrics()
			baseline := rect.Min.Y + (ch-(m.Ascent+m.Descent).Ceil())/2 + m.Ascent.Ceil()
			if r != ' ' {
				d := font.Drawer{
					Dst:  img,
					Src:  image.NewUniform(fg),
					Face: face,
					Dot:  fixed.P(rect.Min.X, baseline),
				}
				d.DrawString(string(r))
			}
			if c.Attrs&protocol.AttrUnderline != 0 {
				fillRect(img, rect.Min.X, baseline+2*scale, rect.Max.X, baseline+3*scale, fg)
			}
			if c.Attrs&protocol.AttrStrike != 0 {
				fillRect(img, rect.Min.X, baseline-4*scale, rect.Max.X, baseline-3*scale, fg)
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func drawImageCell(img *image.RGBA, rect image.Rectangle, c protocol.Cell, bg color.RGBA) {
	fg := bg
	if c.FG.Set {
		fg = colorRGBA(c.FG)
	}
	switch c.R {
	case '▀':
		fillRect(img, rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+(rect.Dy()+1)/2, fg)
	case '▄':
		fillRect(img, rect.Min.X, rect.Min.Y+rect.Dy()/2, rect.Max.X, rect.Max.Y, fg)
	}
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	draw.Draw(img, image.Rect(x0, y0, x1, y1), &image.Uniform{C: c}, image.Point{}, draw.Src)
}

func colorRGBA(c protocol.Color) color.RGBA {
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}
}
