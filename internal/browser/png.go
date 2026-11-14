package browser

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sync"

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
		if pngRegular, pngFontErr = opentype.Parse(gomono.TTF); pngFontErr != nil {
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
	for _, face := range []font.Face{f.regular, f.bold, f.italic, f.boldItalic} {
		if c, ok := face.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}
}

func pngFaces(scale int) (pngFonts, error) {
	parsePNGFonts()
	if pngFontErr != nil {
		return pngFonts{}, pngFontErr
	}
	opts := &opentype.FaceOptions{Size: 12 * float64(scale), DPI: 72, Hinting: font.HintingFull}
	regular, err := opentype.NewFace(pngRegular, opts)
	if err != nil {
		return pngFonts{}, err
	}
	bold, err := opentype.NewFace(pngBold, opts)
	if err != nil {
		return pngFonts{}, err
	}
	italic, err := opentype.NewFace(pngItalic, opts)
	if err != nil {
		return pngFonts{}, err
	}
	boldItalic, err := opentype.NewFace(pngBoldItal, opts)
	if err != nil {
		return pngFonts{}, err
	}
	return pngFonts{regular: regular, bold: bold, italic: italic, boldItalic: boldItalic}, nil
}

// PNG renders the grid as a PNG image. Scale 1 uses an 8x16 pixel cell;
// scale 2 doubles both dimensions.
func (g *Grid) PNG(scale int) ([]byte, error) {
	if g == nil || g.W < 1 || g.H < 1 {
		return nil, errors.New("empty grid")
	}
	if scale < 1 {
		scale = 1
	}
	if scale > 4 {
		scale = 4
	}
	cw, ch := pngCellW*scale, pngCellH*scale
	img := image.NewRGBA(image.Rect(0, 0, g.W*cw, g.H*ch))
	pageBG := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	if g.Cells[0].BG.Set {
		pageBG = colorRGBA(g.Cells[0].BG)
	}
	draw.Draw(img, img.Bounds(), &image.Uniform{C: pageBG}, image.Point{}, draw.Src)

	faces, err := pngFaces(scale)
	if err != nil {
		return nil, err
	}
	defer faces.close()
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			c := g.Cells[y*g.W+x]
			if c.Skip || c.Cont {
				continue
			}
			bg := pageBG
			if c.BG.Set {
				bg = colorRGBA(c.BG)
			}
			rect := image.Rect(x*cw, y*ch, (x+1)*cw, (y+1)*ch)
			draw.Draw(img, rect, &image.Uniform{C: bg}, image.Point{}, draw.Src)
			if c.Img {
				drawImageCell(img, rect, c, bg)
				continue
			}
			if c.Raw != "" {
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
			switch {
			case c.Bold && c.Italic:
				face = faces.boldItalic
			case c.Bold:
				face = faces.bold
			case c.Italic:
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
			if c.Underline {
				fillRect(img, rect.Min.X, baseline+2*scale, rect.Max.X, baseline+3*scale, fg)
			}
			if c.Strike {
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

func drawImageCell(img *image.RGBA, rect image.Rectangle, c Cell, bg color.RGBA) {
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

func colorRGBA(c Color) color.RGBA {
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}
}
