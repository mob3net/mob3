package browser

import (
	"bytes"
	"image/png"
	"testing"
)

func TestGridPNG(t *testing.T) {
	g := NewGrid(4, 2, RGB(31, 35, 40), RGB(255, 255, 255))
	g.Put(0, 0, 'A', &Style{Bold: true}, RGB(200, 20, 30), Color{})
	data, err := g.PNG(1)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := img.Bounds().Dx(), 4*pngCellW; got != want {
		t.Fatalf("width = %d, want %d", got, want)
	}
	if got, want := img.Bounds().Dy(), 2*pngCellH; got != want {
		t.Fatalf("height = %d, want %d", got, want)
	}
	found := false
	for y := 0; y < pngCellH; y++ {
		for x := 0; x < pngCellW; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r > 120<<8 && g < 100<<8 && b < 100<<8 {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("glyph did not render in the foreground color")
	}

	scaled, err := g.PNG(2)
	if err != nil {
		t.Fatal(err)
	}
	img2, err := png.Decode(bytes.NewReader(scaled))
	if err != nil {
		t.Fatal(err)
	}
	if img2.Bounds().Dx() != 2*img.Bounds().Dx() || img2.Bounds().Dy() != 2*img.Bounds().Dy() {
		t.Fatalf("scaled bounds = %v, want double %v", img2.Bounds(), img.Bounds())
	}
}

func TestGridPNGHalfBlockImage(t *testing.T) {
	g := NewGrid(1, 1, RGB(31, 35, 40), RGB(255, 255, 255))
	c := &g.Cells[0]
	c.Img = true
	c.R = '▀'
	c.FG = RGB(255, 0, 0)
	c.BG = RGB(0, 0, 255)
	data, err := g.PNG(1)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r, gg, b, _ := img.At(1, 1).RGBA()
	if r>>8 < 200 || gg>>8 > 50 || b>>8 > 50 {
		t.Fatalf("top half = %d,%d,%d, want red", r>>8, gg>>8, b>>8)
	}
	r, gg, b, _ = img.At(1, pngCellH-1).RGBA()
	if b>>8 < 200 || r>>8 > 50 || gg>>8 > 50 {
		t.Fatalf("bottom half = %d,%d,%d, want blue", r>>8, gg>>8, b>>8)
	}
}

func TestGridPNGEmpty(t *testing.T) {
	if _, err := (*Grid)(nil).PNG(1); err == nil {
		t.Fatal("nil grid produced an image")
	}
	if _, err := (&Grid{}).PNG(1); err == nil {
		t.Fatal("empty grid produced an image")
	}
}
