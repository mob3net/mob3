package frontend

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"

	"browser/internal/protocol"
)

func testPlacement(key string, src image.Image, x, y, w, h, rowStart, totalRows int) protocol.Placement {
	data, err := encodePNG(src)
	if err != nil {
		panic(err)
	}
	return protocol.Placement{
		Key: key, X: x, Y: y, W: w, H: h, RowStart: rowStart, TotalRows: totalRows,
		Format: protocol.PlacementPNG, Data: data,
	}
}

func TestResetForTabClearsImageCaches(t *testing.T) {
	r := NewGFXRenderer(GraphicsKitty, 8, 16)
	key := gfxKey{key: "1", cols: 1, rows: 1, pxW: 8, pxH: 16}
	r.kitty[key] = &kittyImage{id: 1, w: 8, h: 16}
	r.scaled[key] = image.NewNRGBA(image.Rect(0, 0, 8, 16))
	r.encoded[key] = []byte{1}
	r.decoded["1"] = image.NewNRGBA(image.Rect(0, 0, 8, 16))
	r.iterm[itermKey{gfxKey: key}] = "payload"
	var sb strings.Builder
	r.ResetForTab(&sb)
	if len(r.kitty) != 0 || len(r.iterm) != 0 || len(r.scaled) != 0 ||
		len(r.encoded) != 0 || len(r.decoded) != 0 {
		t.Fatal("image caches survived a tab reset")
	}
	if r.nextID != 1 {
		t.Fatalf("next image id = %d, want reset", r.nextID)
	}
	if !strings.Contains(sb.String(), "a=d,d=A") {
		t.Fatalf("reset did not remove terminal images: %q", sb.String())
	}
}

func TestTargetPixels(t *testing.T) {
	r := NewGFXRenderer(GraphicsKitty, 8, 16)
	if r == nil {
		t.Fatal("kitty renderer is nil")
	}
	if w, h := r.targetPixels(10, 5); w != 80 || h != 80 {
		t.Errorf("targetPixels(10,5) = %d,%d want 80,80", w, h)
	}
	w, h := r.targetPixels(1000, 1000)
	if w > 2048 || h > 2048 {
		t.Errorf("targetPixels not bounded: %d,%d", w, h)
	}
	if w*2 != h {
		t.Errorf("bounded target lost aspect ratio: %d,%d", w, h)
	}
}

func TestScaleImage(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 200, G: 10, B: 20, A: 255})
		}
	}
	dst := scaleImage(src, 2, 2)
	if dst.Bounds().Dx() != 2 || dst.Bounds().Dy() != 2 {
		t.Fatalf("size = %v", dst.Bounds())
	}
	c := dst.NRGBAAt(1, 1)
	if c.R != 200 || c.G != 10 || c.B != 20 || c.A != 255 {
		t.Errorf("pixel = %+v", c)
	}
}

func TestKittyTransmitChunks(t *testing.T) {
	r := NewGFXRenderer(GraphicsKitty, 8, 16)
	if r == nil {
		t.Fatal("kitty renderer is nil")
	}
	var sb strings.Builder
	r.kittyTransmit(&sb, 7, make([]byte, 8000))
	out := sb.String()
	if !strings.Contains(out, "a=t,f=100,q=2,i=7,m=1;") {
		t.Errorf("missing first chunk header: %.80q", out)
	}
	if strings.Count(out, "\x1b_G") < 2 {
		t.Errorf("payload was not chunked: %d", strings.Count(out, "\x1b_G"))
	}
	if !strings.Contains(out, "m=0;") {
		t.Error("missing final chunk")
	}
}

func TestGraphicsResetForgetsTransmissions(t *testing.T) {
	r := NewGFXRenderer(GraphicsKitty, 8, 16)
	src := image.NewNRGBA(image.Rect(0, 0, 32, 64))
	p := testPlacement("x.png", src, 0, 0, 4, 4, 0, 4)
	var sb strings.Builder
	r.PlaceAll(&sb, []protocol.Placement{p})
	if len(r.kitty) != 1 {
		t.Fatalf("kitty cache = %d", len(r.kitty))
	}
	var reset strings.Builder
	r.Reset(&reset)
	if len(r.kitty) != 0 {
		t.Fatal("transmission cache was not reset")
	}
	if len(r.scaled) != 1 {
		t.Fatal("scaled image cache should survive a transmission reset")
	}
	if !strings.Contains(reset.String(), "a=d,d=A") {
		t.Errorf("reset output = %q", reset.String())
	}
	sb.Reset()
	r.PlaceAll(&sb, []protocol.Placement{p})
	if !strings.Contains(sb.String(), "a=t,f=100") {
		t.Error("image was not retransmitted after reset")
	}
}

func TestPlaceAllKeepsVisibleImagesOverCacheCap(t *testing.T) {
	r := NewGFXRenderer(GraphicsKitty, 8, 16)
	placements := make([]protocol.Placement, 0, maxCachedImages+16)
	for i := 0; i < maxCachedImages+16; i++ {
		src := image.NewNRGBA(image.Rect(0, 0, 16, 16))
		placements = append(placements, testPlacement(
			fmt.Sprintf("img%d.png", i), src, i%10, i/10, 1, 1, 0, 1))
	}
	var first strings.Builder
	r.PlaceAll(&first, placements)
	if len(r.scaled) != len(placements) {
		t.Fatalf("scaled cache = %d, want %d", len(r.scaled), len(placements))
	}
	var second strings.Builder
	r.PlaceAll(&second, placements)
	if strings.Contains(second.String(), "a=d,d=A") {
		t.Error("visible images were reset on the next frame")
	}
	if strings.Contains(second.String(), "a=t,f=100") {
		t.Error("visible images were retransmitted on the next frame")
	}
	if !strings.Contains(second.String(), "a=p,i=") {
		t.Error("cached images were not re-placed")
	}
}

func TestPlaceAllPrunesOnlyUnseenImages(t *testing.T) {
	r := NewGFXRenderer(GraphicsKitty, 8, 16)
	mk := func(name string) protocol.Placement {
		return testPlacement(name, image.NewNRGBA(image.Rect(0, 0, 16, 16)), 0, 0, 1, 1, 0, 1)
	}
	var all []protocol.Placement
	for i := 0; i < maxCachedImages+8; i++ {
		all = append(all, mk(fmt.Sprintf("old%d.png", i)))
	}
	var sb strings.Builder
	r.PlaceAll(&sb, all)
	if len(r.scaled) <= maxCachedImages {
		t.Fatalf("scaled cache = %d, want over cap", len(r.scaled))
	}
	fresh := mk("new.png")
	var sb2 strings.Builder
	r.PlaceAll(&sb2, []protocol.Placement{all[0], fresh})
	if len(r.scaled) != 2 || len(r.kitty) != 2 {
		t.Fatalf("cache after prune = %d scaled, %d kitty; want 2 each", len(r.scaled), len(r.kitty))
	}
	if !strings.Contains(sb2.String(), "a=d,d=I,i=") {
		t.Error("evicted kitty image data was not freed")
	}
}

func TestClearScreenKeepsKittyTransmissions(t *testing.T) {
	r := NewGFXRenderer(GraphicsKitty, 8, 16)
	src := image.NewNRGBA(image.Rect(0, 0, 32, 64))
	p := testPlacement("x.png", src, 0, 0, 4, 4, 0, 4)
	var first strings.Builder
	r.PlaceAll(&first, []protocol.Placement{p})
	var clear strings.Builder
	r.ClearScreen(&clear)
	out := clear.String()
	if !strings.Contains(out, "\x1b[1J") || !strings.Contains(out, "\x1b[0J") {
		t.Errorf("clear output = %q", out)
	}
	if strings.Contains(out, "\x1b[2J") || strings.Contains(out, "a=d,d=A") {
		t.Errorf("clear freed kitty image data: %q", out)
	}
	if len(r.kitty) != 1 || len(r.scaled) != 1 {
		t.Fatal("kitty caches were dropped by the screen clear")
	}
	var second strings.Builder
	r.PlaceAll(&second, []protocol.Placement{p})
	if strings.Contains(second.String(), "a=t,f=100") {
		t.Error("image was retransmitted after a text-only clear")
	}
	if !strings.Contains(second.String(), "a=p,i=1") {
		t.Error("cached image was not re-placed after the clear")
	}
}

func TestPlaceKittyUsesSourceCrop(t *testing.T) {
	r := NewGFXRenderer(GraphicsKitty, 8, 16)
	src := image.NewNRGBA(image.Rect(0, 0, 32, 64))
	p := testPlacement("x.png", src, 2, 3, 4, 2, 1, 4)
	var sb strings.Builder
	r.PlaceAll(&sb, []protocol.Placement{p})
	out := sb.String()
	if !strings.Contains(out, "a=t,f=100") {
		t.Error("image was not transmitted")
	}
	if !strings.Contains(out, "a=p,i=1,q=2,c=4,r=2") {
		t.Errorf("missing placement: %.200q", out)
	}
	if !strings.Contains(out, "y=16,w=32,h=32") {
		t.Errorf("wrong source crop: %.300q", out)
	}
}

func TestGFXRejectsCorruptPlacement(t *testing.T) {
	r := NewGFXRenderer(GraphicsKitty, 8, 16)
	p := protocol.Placement{Key: "bad", W: 1, H: 1, TotalRows: 1, Format: protocol.PlacementPNG, Data: []byte("not a png")}
	var sb strings.Builder
	r.PlaceAll(&sb, []protocol.Placement{p})
	if len(r.kitty) != 0 || len(r.decoded) != 0 {
		t.Fatalf("corrupt placement was cached: kitty=%d decoded=%d", len(r.kitty), len(r.decoded))
	}
	if strings.Contains(sb.String(), "a=t,f=100") {
		t.Fatal("corrupt placement was transmitted")
	}
}

func TestPresenterReplacesImagesAfterNativeDowngrade(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	cells := make([]protocol.Cell, 8*3)
	for i := range cells {
		cells[i] = protocol.Cell{R: 'x', FG: protocol.Color{Set: true}, BG: protocol.Color{Set: true}}
	}
	f := protocol.Frame{W: 8, H: 3, Cells: cells, Placements: []protocol.Placement{
		testPlacement("p", img, 0, 0, 2, 1, 0, 1),
	}}
	var out strings.Builder
	p := NewPresenter(&out, true)
	p.SetGraphics(GraphicsKitty, 8, 16)
	if err := p.Present(f, 0, true); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := p.Present(f, 0, false); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := p.Present(f, 0, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "a=t") || !strings.Contains(out.String(), "a=p") {
		t.Fatalf("image data was not re-transmitted after re-enabling native mode: %q", out.String())
	}
}
