package browser

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestParseGraphicsMode(t *testing.T) {
	cases := map[string]GraphicsMode{
		"auto": GraphicsAuto, "": GraphicsAuto, "kitty": GraphicsKitty,
		"iTerm2": GraphicsITerm, "blocks": GraphicsBlocks, "off": GraphicsNone,
		"none": GraphicsNone, "nonsense": GraphicsAuto,
	}
	for in, want := range cases {
		if got := ParseGraphicsMode(in); got != want {
			t.Errorf("ParseGraphicsMode(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestDetectGraphics(t *testing.T) {
	if got := DetectGraphics(GraphicsNone); got != GraphicsNone {
		t.Errorf("none = %v, want none", got)
	}
	for _, k := range []string{"TMUX", "ZELLIJ", "KITTY_WINDOW_ID", "WEZTERM_PANE", "GHOSTTY_RESOURCES_DIR", "LC_TERMINAL", "TERM_PROGRAM"} {
		t.Setenv(k, "")
	}
	t.Setenv("TMUX", "1")
	t.Setenv("TERM", "xterm-kitty")
	if got := DetectGraphics(GraphicsAuto); got != GraphicsBlocks {
		t.Errorf("inside tmux = %v, want blocks", got)
	}
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "xterm-kitty")
	if got := DetectGraphics(GraphicsAuto); got != GraphicsKitty {
		t.Errorf("kitty term = %v, want kitty", got)
	}
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	if got := DetectGraphics(GraphicsAuto); got != GraphicsITerm {
		t.Errorf("iterm = %v, want iterm", got)
	}
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("TERM", "xterm-256color")
	if got := DetectGraphics(GraphicsAuto); got != GraphicsBlocks {
		t.Errorf("plain term = %v, want blocks", got)
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
	// The cell box is 8:16 pixels, so a square cell count keeps that 1:2 ratio.
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

func TestBigTextStripsControls(t *testing.T) {
	g := NewGrid(20, 4, RGB(0, 0, 0), RGB(255, 255, 255))
	g.TextBig(0, 0, "A\x1b]52;c;evil\x07B", &Style{FontSize: 32, LineHeight: 1}, RGB(0, 0, 0), Color{}, 2)
	raw := g.Cells[0].Raw
	if strings.Contains(raw, "\x1b]52") || strings.Count(raw, "\x07") != 1 {
		t.Fatalf("control characters escaped into OSC 66: %q", raw)
	}
	if !strings.HasPrefix(raw, "\x1b]66;s=2;A]52") || !strings.HasSuffix(raw, "\x07") {
		t.Fatalf("sanitized text was not preserved: %q", raw)
	}
}

func TestGridDropsTerminalControls(t *testing.T) {
	g := NewGrid(40, 2, RGB(0, 0, 0), RGB(255, 255, 255))
	g.Text(0, 0, "safe\x1b]52;c;evil\x07\x1b[2Jafter", &Style{}, RGB(0, 0, 0))
	joined := strings.Join(g.Lines(true), "\n")
	for _, bad := range []string{"\x1b]52", "\x1b[2J", "\x07", "\u009b"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("terminal control %q leaked into output: %q", bad, joined)
		}
	}
	if !strings.Contains(joined, "safe") || !strings.Contains(joined, "after") {
		t.Fatalf("ordinary text was lost: %q", joined)
	}
	if plain := strings.Join(g.Lines(false), "\n"); strings.Contains(plain, "\x1b") {
		t.Fatalf("no-color output contains an escape: %q", plain)
	}
}

func TestBigTextPaint(t *testing.T) {
	ly := &Layout{Height: 4, Frags: []Frag{
		{Kind: fragText, X: 0, Y: 0, W: 4, H: 2, Text: "AB", Style: &Style{FontSize: 32, Bold: true, LineHeight: 1}},
	}}
	g := NewGrid(10, 4, RGB(0, 0, 0), RGB(255, 255, 255))
	PaintLayoutBig(g, ly, 0, 0, 4, nil, RGB(0, 0, 0), nil, true)
	c := g.Cells[0]
	if !strings.Contains(c.Raw, "\x1b]66;s=2;AB\x07") {
		t.Fatalf("raw scaled text = %q", c.Raw)
	}
	if strings.Contains(c.Raw, "w=") {
		t.Fatalf("raw scaled text must not send an explicit w key: %q", c.Raw)
	}
	if c.RawW != 4 {
		t.Fatalf("raw width = %d, want 4", c.RawW)
	}
	if !g.Cells[1].Skip || !g.Cells[1*g.W].Skip {
		t.Fatal("covered cells were not marked")
	}
	lines := g.Lines(true)
	if !strings.Contains(lines[0], "\x1b]66;") || !strings.Contains(lines[1], "\x1b[4C") {
		t.Fatalf("lines = %q | %q", lines[0], lines[1])
	}
	// Painting over a scaled cell (for example the footer) clears its skip flag.
	g.Fill(0, 1, 10, 1, RGB(1, 2, 3))
	if g.Cells[1*g.W].Skip {
		t.Fatal("Fill did not clear covered cells")
	}
}

func TestPaintLayoutCollectsClippedPlacement(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 64))
	ly := &Layout{Height: 10, Frags: []Frag{{Kind: fragImage, X: 1, Y: 5, W: 4, H: 4, Img: img}}}
	g := NewGrid(20, 8, RGB(0, 0, 0), RGB(255, 255, 255))
	var ps []ImagePlacement
	PaintLayoutGraphics(g, ly, 7, 0, 8, nil, RGB(0, 0, 0), &ps)
	if len(ps) != 1 {
		t.Fatalf("placements = %d", len(ps))
	}
	p := ps[0]
	// Image starts at document row 5; scrolling by 7 clips two rows off the top.
	if p.Y != 0 || p.H != 2 || p.RowStart != 2 || p.TotalRows != 4 || p.W != 4 {
		t.Errorf("placement = %+v", p)
	}
	for _, c := range g.Cells {
		if c.Img {
			t.Error("half-block image was painted despite graphics collection")
			break
		}
	}
}

func TestGraphicsResetForgetsTransmissions(t *testing.T) {
	r := NewGFXRenderer(GraphicsKitty, 8, 16)
	src := image.NewNRGBA(image.Rect(0, 0, 32, 64))
	p := ImagePlacement{Img: src, Src: "x.png", X: 0, Y: 0, W: 4, H: 4, TotalRows: 4}
	var sb strings.Builder
	r.PlaceAll(&sb, []ImagePlacement{p})
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
	r.PlaceAll(&sb, []ImagePlacement{p})
	if !strings.Contains(sb.String(), "a=t,f=100") {
		t.Error("image was not retransmitted after reset")
	}
}

func TestPlaceAllKeepsVisibleImagesOverCacheCap(t *testing.T) {
	r := NewGFXRenderer(GraphicsKitty, 8, 16)
	// More distinct images than maxCachedImages, all in one frame. They must
	// stay cached: resetting them every frame re-scales and re-encodes all of
	// them forever.
	placements := make([]ImagePlacement, 0, maxCachedImages+16)
	for i := 0; i < maxCachedImages+16; i++ {
		src := image.NewNRGBA(image.Rect(0, 0, 16, 16))
		placements = append(placements, ImagePlacement{
			Img: src, Src: fmt.Sprintf("img%d.png", i),
			X: i % 10, Y: i / 10, W: 1, H: 1, TotalRows: 1,
		})
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
	mk := func(name string) ImagePlacement {
		return ImagePlacement{Img: image.NewNRGBA(image.Rect(0, 0, 16, 16)), Src: name, W: 1, H: 1, TotalRows: 1}
	}
	var all []ImagePlacement
	for i := 0; i < maxCachedImages+8; i++ {
		all = append(all, mk(fmt.Sprintf("old%d.png", i)))
	}
	var sb strings.Builder
	r.PlaceAll(&sb, all)
	if len(r.scaled) <= maxCachedImages {
		t.Fatalf("scaled cache = %d, want over cap", len(r.scaled))
	}
	// A later frame showing one new image prunes the old, unseen ones but keeps
	// the image that is still visible.
	fresh := mk("new.png")
	var sb2 strings.Builder
	r.PlaceAll(&sb2, []ImagePlacement{all[0], fresh})
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
	p := ImagePlacement{Img: src, Src: "x.png", X: 0, Y: 0, W: 4, H: 4, TotalRows: 4}
	var first strings.Builder
	r.PlaceAll(&first, []ImagePlacement{p})
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
	r.PlaceAll(&second, []ImagePlacement{p})
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
	p := ImagePlacement{Img: src, Src: "x.png", X: 2, Y: 3, W: 4, H: 2, RowStart: 1, TotalRows: 4}
	var sb strings.Builder
	r.PlaceAll(&sb, []ImagePlacement{p})
	out := sb.String()
	if !strings.Contains(out, "a=t,f=100") {
		t.Error("image was not transmitted")
	}
	if !strings.Contains(out, "a=p,i=1,q=2,c=4,r=2") {
		t.Errorf("missing placement: %.200q", out)
	}
	// Row 1 of 4 with a 64 px tall image: source y should be 16.
	if !strings.Contains(out, "y=16,w=32,h=32") {
		t.Errorf("wrong source crop: %.300q", out)
	}
}

func TestDecodeSVG(t *testing.T) {
	img, err := decodeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">
		<rect width="10" height="10" fill="#ff0000"/>
	</svg>`))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 10 || img.Bounds().Dy() != 10 {
		t.Fatalf("bounds = %v", img.Bounds())
	}
	r, g, b, a := img.At(5, 5).RGBA()
	if r < 0x8000 || g > 0x2000 || b > 0x2000 || a < 0x8000 {
		t.Fatalf("center pixel = %04x %04x %04x %04x", r, g, b, a)
	}
}

func TestSerializeSVGRestoresCamelCase(t *testing.T) {
	root := ParseHTML(`<svg viewbox="0 0 10 10"><lineargradient id="g"></lineargradient></svg>`)
	svg := root.FindFirst("svg")
	out := serializeSVG(svg)
	if !strings.Contains(out, `viewBox="0 0 10 10"`) {
		t.Fatalf("viewBox not restored: %s", out)
	}
	if !strings.Contains(out, "linearGradient") {
		t.Fatalf("linearGradient tag not restored: %s", out)
	}
	if _, err := decodeSVG([]byte(out)); err != nil {
		t.Fatalf("serialized SVG did not decode: %v", err)
	}
}

func TestInlineSVGSkippedByDefault(t *testing.T) {
	root := ParseHTML(`<html><body><svg width="20" height="20" aria-label="logo">
		<rect width="10" height="10" fill="#00ff00"/>
	</svg></body></html>`)
	loader := NewLoader()
	loader.loadInlineSVGs(root)
	svg := root.FindFirst("svg")
	if svg == nil || svg.Img != nil {
		t.Fatal("inline SVG was rasterized even though images are disabled")
	}
	ly := BuildLayout(root, nil, 40)
	var text strings.Builder
	for _, f := range ly.Frags {
		if f.Kind == fragText {
			text.WriteString(f.Text)
		}
	}
	if !strings.Contains(text.String(), "logo") {
		t.Fatalf("inline SVG label fallback missing: %q", text.String())
	}
}

func TestInlineSVGDecoded(t *testing.T) {
	root := ParseHTML(`<html><body><svg width="20" height="20" viewBox="0 0 10 10">
		<rect width="10" height="10" fill="#00ff00"/>
	</svg></body></html>`)
	loader := NewLoader()
	loader.noImages = false
	loader.loadInlineSVGs(root)
	svg := root.FindFirst("svg")
	if svg == nil || svg.Img == nil {
		t.Fatal("inline SVG was not rasterized")
	}
	ly := BuildLayout(root, nil, 40)
	found := false
	for _, f := range ly.Frags {
		if f.Kind == fragImage && f.Img == svg.Img {
			found = true
		}
	}
	if !found {
		t.Fatal("inline SVG image was not laid out")
	}
}

func TestIsSVG(t *testing.T) {
	if !isSVG("image/svg+xml", nil) || !isSVG("", []byte(`<svg></svg>`)) {
		t.Fatal("SVG detection failed")
	}
	if isSVG("image/png", []byte("not svg")) {
		t.Fatal("PNG misdetected as SVG")
	}
}
