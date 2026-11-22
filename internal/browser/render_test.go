package browser

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"
	"unsafe"
)

// TestFrameCellLayout guards the frame memory budget: adding a string or a
// pointer to Cell silently doubles the cost of every viewport and screen grid.
func TestFrameCellLayout(t *testing.T) {
	if got := unsafe.Sizeof(Cell{}); got > 24 {
		t.Fatalf("Cell is %d bytes; the frame budget requires at most 24", got)
	}
}

func TestGridResetClearsRuns(t *testing.T) {
	g := NewGrid(8, 4, RGB(255, 255, 255), RGB(0, 0, 0))
	g.TextBig(0, 0, "Big", &Style{FontSize: 32, LineHeight: 1}, RGB(1, 2, 3), Color{}, 2)
	if len(g.Runs) == 0 || g.Cells[0].Run == 0 {
		t.Fatal("scaled run was not recorded")
	}
	g.Reset(RGB(9, 9, 9), RGB(0, 0, 0))
	if len(g.Runs) != 0 || g.Cells[0].Run != 0 || g.Cells[0].R != ' ' {
		t.Fatalf("reset left state: runs=%d cell=%+v", len(g.Runs), g.Cells[0])
	}
}

func TestClearGlyphsKeepsBackground(t *testing.T) {
	g := NewGrid(20, 3, RGB(0, 0, 0), RGB(255, 255, 255))
	bg := RGB(0x10, 0x20, 0x30)
	g.Fill(19, 1, 1, 1, bg)
	g.Put(19, 1, 'X', &Style{Bold: true, Underline: true}, RGB(0xff, 0, 0), bg)
	g.ClearGlyphs(19, 1, 1, 1)
	c := g.Cells[1*g.W+19]
	if c.R != ' ' || c.Bold || c.Underline {
		t.Fatalf("glyph not cleared: %+v", c)
	}
	if c.BG != bg {
		t.Fatalf("background changed: %+v", c.BG)
	}
}

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

func TestUniformHalfBlockUsesSpace(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{10, 20, 30, 255})
		}
	}
	g := NewGrid(2, 2, RGB(0, 0, 0), RGB(255, 255, 255))
	paintImage(g, img, image.Rectangle{}, 0, 0, 2, 2, 0, 2)
	want := Color{R: 10, G: 20, B: 30, A: 255, Set: true}
	for _, c := range g.Cells[:4] {
		if c.R != ' ' {
			t.Fatalf("uniform image cell glyph = %q, want space", c.R)
		}
		if c.BG != want || c.FG != want {
			t.Fatalf("uniform image cell = %+v", c)
		}
		if !c.Img {
			t.Fatal("uniform image cell lost its image marker")
		}
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

// gridRun returns the structured scaled run starting at a cell.
func gridRun(g *Grid, x, y int) (scaledRun, bool) {
	c := g.Cells[y*g.W+x]
	if c.Run == 0 || int(c.Run) > len(g.Runs) {
		return scaledRun{}, false
	}
	return g.Runs[int(c.Run)-1], true
}

func runEscape(r scaledRun) string {
	return fmt.Sprintf("\x1b]66;s=%d;%s\x07", r.Scale, r.Text)
}

func TestBigTextStripsControls(t *testing.T) {
	g := NewGrid(20, 4, RGB(0, 0, 0), RGB(255, 255, 255))
	g.TextBig(0, 0, "A\x1b]52;c;evil\x07B", &Style{FontSize: 32, LineHeight: 1}, RGB(0, 0, 0), Color{}, 2)
	r, ok := gridRun(g, 0, 0)
	if !ok {
		t.Fatal("scaled run was not recorded")
	}
	raw := runEscape(r)
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
	joined := strings.Join(gridLines(g, true), "\n")
	for _, bad := range []string{"\x1b]52", "\x1b[2J", "\x07", "\u009b"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("terminal control %q leaked into output: %q", bad, joined)
		}
	}
	if !strings.Contains(joined, "safe") || !strings.Contains(joined, "after") {
		t.Fatalf("ordinary text was lost: %q", joined)
	}
	if plain := strings.Join(gridLines(g, false), "\n"); strings.Contains(plain, "\x1b") {
		t.Fatalf("no-color output contains an escape: %q", plain)
	}
}

func TestBigTextPaint(t *testing.T) {
	ly := &Layout{Height: 4, Frags: []Frag{
		{Kind: fragText, X: 0, Y: 0, W: 4, H: 2, Text: "AB", Style: &Style{FontSize: 32, Bold: true, LineHeight: 1}},
	}}
	g := NewGrid(10, 4, RGB(0, 0, 0), RGB(255, 255, 255))
	PaintLayoutBig(g, ly, 0, 0, 4, nil, RGB(0, 0, 0), nil, true)
	r, ok := gridRun(g, 0, 0)
	if !ok {
		t.Fatal("scaled run was not recorded")
	}
	raw := runEscape(r)
	if !strings.Contains(raw, "\x1b]66;s=2;AB\x07") {
		t.Fatalf("raw scaled text = %q", raw)
	}
	if strings.Contains(raw, "w=") {
		t.Fatalf("raw scaled text must not send an explicit w key: %q", raw)
	}
	if r.Cells != 4 {
		t.Fatalf("run width = %d, want 4", r.Cells)
	}
	if !g.Cells[1].Skip || !g.Cells[1*g.W].Skip {
		t.Fatal("covered cells were not marked")
	}
	lines := gridLines(g, true)
	if !strings.Contains(lines[0], "\x1b]66;") || !strings.Contains(lines[1], "\x1b[4C") {
		t.Fatalf("lines = %q | %q", lines[0], lines[1])
	}
	// Painting over a scaled cell (for example the footer) clears its skip flag.
	g.Fill(0, 1, 10, 1, RGB(1, 2, 3))
	if g.Cells[1*g.W].Skip {
		t.Fatal("Fill did not clear covered cells")
	}
}

func TestContentFrameBlitMatchesFullPaint(t *testing.T) {
	root := ParseHTML(`<html><head><style>
h1{font-size:32px;line-height:1} p{margin:0}
.bar{position:fixed;top:0;left:0;width:100%;background:#eee}
</style></head><body><h1>Big</h1><div class="bar">status</div>` +
		strings.Repeat("<p>content line</p>", 30) + `</body></html>`)
	ly := BuildLayoutViewportDeadline(root, nil, 40, 12, true, time.Time{})
	ly.SetFixedScroll(0)
	fg, bg := RGB(0x1f, 0x23, 0x28), RGB(255, 255, 255)
	full := NewGrid(40, 12, fg, bg)
	PaintLayoutBig(full, ly, 0, 2, 10, nil, fg, nil, true)
	content := NewGrid(40, 8, fg, bg)
	PaintLayoutBig(content, ly, 0, 0, 8, nil, fg, nil, true)
	screen := NewGrid(40, 12, fg, bg)
	blitGrid(screen, content, 2)
	for _, r := range content.Runs {
		r.Y += 2
		screen.Runs = append(screen.Runs, r)
	}
	for y := 2; y < 10; y++ {
		for x := 0; x < 40; x++ {
			got := screen.Cells[y*40+x]
			want := full.Cells[y*40+x]
			if got != want {
				t.Fatalf("cell %d,%d: content+blit %+v, full paint %+v", x, y, got, want)
			}
		}
	}
	if len(screen.Runs) != len(full.Runs) {
		t.Fatalf("runs = %d, full paint = %d", len(screen.Runs), len(full.Runs))
	}
	for i := range screen.Runs {
		if screen.Runs[i] != full.Runs[i] {
			t.Fatalf("run %d: content+blit %+v, full paint %+v", i, screen.Runs[i], full.Runs[i])
		}
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
	if !strings.Contains(text.String(), "▣") {
		t.Fatalf("inline SVG icon marker missing: %q", text.String())
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

func TestBigTextWhitespaceHasNoScaledUnderline(t *testing.T) {
	g := NewGrid(10, 4, RGB(0, 0, 0), RGB(255, 255, 255))
	st := &Style{FontSize: 32, LineHeight: 1, Underline: true}
	g.TextBig(0, 0, " ", st, RGB(0, 0, 0), Color{}, 2)
	for _, idx := range []int{0, 1, 10, 11} {
		c := g.Cells[idx]
		if c.Run != 0 || c.Skip || c.Underline {
			t.Fatalf("cell %d still renders a scaled underlined space: run=%d skip=%v underline=%v", idx, c.Run, c.Skip, c.Underline)
		}
		if c.R != ' ' {
			t.Fatalf("cell %d is not blank: %q", idx, c.R)
		}
	}
}

func TestBigTextStaysInsideViewport(t *testing.T) {
	ly := &Layout{Frags: []Frag{
		{Kind: fragText, X: 0, Y: 2, W: 3, H: 2, Text: "Big", Style: &Style{FontSize: 32, LineHeight: 1}},
		{Kind: fragText, X: 0, Y: 4, W: 6, H: 2, Text: "Banana", Style: &Style{FontSize: 32, LineHeight: 1}},
	}}
	g := NewGrid(20, 6, RGB(0, 0, 0), RGB(255, 255, 255))
	// Rows 0-4 are the page viewport; row 5 is the footer.
	PaintLayoutBig(g, ly, 0, 0, 5, nil, RGB(0, 0, 0), nil, true)
	if g.Cells[2*20].Run == 0 {
		t.Fatal("a scaled run that fits the viewport was not scaled")
	}
	if g.Cells[5*20].Run != 0 || g.Cells[5*20].Skip {
		t.Fatal("scaled text from the last content row painted into the footer row")
	}
	if g.Cells[4*20].Run != 0 {
		t.Fatal("last-row text was still sent as scaled text")
	}
	if g.Cells[4*20].R != 'B' {
		t.Fatalf("last-row text is missing: %q", g.Cells[4*20].R)
	}
}

func TestBigTextDisabledUnderFixedChrome(t *testing.T) {
	fill := RGB(0x20, 0x20, 0x20)
	// A large heading whose scaled box (rows 4-5) touches a fixed bar on row
	// 5 must be painted at normal size: the terminal composites OSC 66 scaled
	// text above the cell grid, so the big glyphs would cover the bar.
	ly := &Layout{Frags: []Frag{
		{Kind: fragText, X: 0, Y: 4, W: 4, H: 2, Text: "AB", Style: &Style{FontSize: 32, LineHeight: 1}},
		{Kind: fragFill, X: 0, Y: 5, W: 20, H: 1, Color: fill, Fixed: true},
	}}
	g := NewGrid(20, 8, RGB(255, 255, 255), RGB(255, 255, 255))
	PaintLayoutBig(g, ly, 0, 0, 8, nil, RGB(0, 0, 0), nil, true)
	if g.Cells[4*20].Run != 0 {
		t.Fatalf("scaled text touching fixed chrome was sent as OSC 66: run=%d", g.Cells[4*20].Run)
	}
	if g.Cells[4*20].Skip || g.Cells[4*20].R != 'A' {
		t.Fatalf("normal-size fallback missing: cell=%+v", g.Cells[4*20])
	}

	// Clear of the bar, the same run keeps its scaled output.
	ly = &Layout{Frags: []Frag{
		{Kind: fragText, X: 0, Y: 4, W: 4, H: 2, Text: "AB", Style: &Style{FontSize: 32, LineHeight: 1}},
		{Kind: fragFill, X: 0, Y: 7, W: 20, H: 1, Color: fill, Fixed: true},
	}}
	g = NewGrid(20, 10, RGB(255, 255, 255), RGB(255, 255, 255))
	PaintLayoutBig(g, ly, 0, 0, 10, nil, RGB(0, 0, 0), nil, true)
	r, ok := gridRun(g, 0, 4)
	if !ok {
		t.Fatal("unobstructed scaled text was not painted")
	}
	if raw := runEscape(r); !strings.Contains(raw, "\x1b]66;s=2;AB\x07") {
		t.Fatalf("unobstructed scaled text was not painted: %q", raw)
	}
}

func TestColor256Palette(t *testing.T) {
	cases := []struct {
		c    Color
		want int
	}{
		{RGB(0, 0, 0), 16},
		{RGB(255, 255, 255), 231},
		{RGB(255, 0, 0), 196},
		{RGB(0, 255, 0), 46},
		{RGB(0, 0, 255), 21},
	}
	for _, tc := range cases {
		if got := color256(tc.c); got != tc.want {
			t.Errorf("color256(%v) = %d, want %d", tc.c, got, tc.want)
		}
	}
}

func TestColorModeCodes(t *testing.T) {
	old := terminalColorMode
	defer SetColorMode(old)
	SetColorMode(Color256)
	if got := fgCode(RGB(255, 0, 0)); got != "38;5;196" {
		t.Fatalf("256 fg = %q", got)
	}
	if got := bgCode(RGB(0, 0, 255)); got != "48;5;21" {
		t.Fatalf("256 bg = %q", got)
	}
	SetColorMode(ColorTrueColor)
	if got := fgCode(RGB(255, 0, 0)); got != "38;2;255;0;0" {
		t.Fatalf("truecolor fg = %q", got)
	}
}

func TestDetectColorMode(t *testing.T) {
	t.Setenv("COLORTERM", "")
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	if got := DetectColorMode(); got != Color256 {
		t.Fatalf("Apple_Terminal mode = %v, want 256", got)
	}
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	if got := DetectColorMode(); got != ColorTrueColor {
		t.Fatalf("iTerm mode = %v, want truecolor", got)
	}
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	t.Setenv("COLORTERM", "truecolor")
	if got := DetectColorMode(); got != ColorTrueColor {
		t.Fatalf("truecolor COLORTERM mode = %v, want truecolor", got)
	}
}

func TestLinesMonoKeepsAttributes(t *testing.T) {
	g := NewGrid(4, 1, RGB(0, 0, 0), RGB(255, 255, 255))
	g.Put(0, 0, 'a', &Style{Bold: true, Underline: true}, RGB(255, 0, 0), RGB(0, 0, 255))
	g.Cells[0].Reverse = true
	mono := gridLinesMono(g)[0]
	if strings.Contains(mono, "38;2") || strings.Contains(mono, "48;2") {
		t.Fatalf("mono line kept colors: %q", mono)
	}
	if !strings.Contains(mono, "0;1;4;7") {
		t.Fatalf("mono line lost attributes: %q", mono)
	}
	plain := gridLines(g, false)[0]
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("plain dump kept escapes: %q", plain)
	}
}

func TestFixedChromePaintsAboveContent(t *testing.T) {
	doc := ParseHTML(`<html><body style="margin:0">` +
		`<div style="position:fixed;top:0;left:0;width:200px;background:#000">TOP</div>` +
		`<div style="height:16px">LINE1</div><div style="height:16px">LINE2</div>` +
		`<div style="height:16px">LINE3</div><div style="height:16px">LINE4</div>` +
		`</body></html>`)
	ly := BuildLayoutBudgetDeadline(doc, nil, 40, 10, false, false, time.Time{}, DefaultLayoutBudget)
	rowText := func(g *Grid, y int) string {
		var b strings.Builder
		for x := 0; x < g.W; x++ {
			r := g.Cells[y*g.W+x].R
			if r == 0 {
				r = ' '
			}
			b.WriteRune(r)
		}
		return b.String()
	}
	for _, scroll := range []int{0, 3} {
		ly.SetFixedScroll(scroll)
		g := NewGrid(40, 10, RGB(0, 0, 0), RGB(255, 255, 255))
		PaintLayout(g, ly, scroll, 0, 10, nil, RGB(255, 255, 255))
		row := rowText(g, 0)
		if strings.TrimSpace(row) != "TOP" {
			t.Fatalf("scroll %d: fixed header row = %q, want only TOP", scroll, row)
		}
	}
}

func TestImagesUnderFixedChromeUseBlocks(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	fill := RGB(0x20, 0x20, 0x20)
	g := NewGrid(20, 10, RGB(255, 255, 255), RGB(255, 255, 255))

	// An image scrolling under a fixed chrome fill must not become a native
	// placement, because the terminal would composite it over the chrome.
	ly := &Layout{Frags: []Frag{
		{Kind: fragImage, X: 0, Y: 5, W: 2, H: 2, Img: img},
		{Kind: fragFill, X: 0, Y: 5, W: 20, H: 1, Color: fill, Fixed: true},
	}}
	var placements []ImagePlacement
	PaintLayoutGraphics(g, ly, 5, 0, 10, nil, RGB(255, 255, 255), &placements)
	if len(placements) != 0 {
		t.Fatalf("image under fixed chrome produced %d placements, want half-block art", len(placements))
	}

	// An image outside the chrome band keeps its native placement.
	ly = &Layout{Frags: []Frag{{Kind: fragImage, X: 0, Y: 8, W: 2, H: 1, Img: img}}}
	placements = nil
	PaintLayoutGraphics(g, ly, 0, 0, 10, nil, RGB(255, 255, 255), &placements)
	if len(placements) != 1 {
		t.Fatalf("image below chrome produced %d placements, want 1", len(placements))
	}

	// A fixed image (for example a logo in the fixed header) is part of the
	// chrome and keeps its native placement.
	ly = &Layout{Frags: []Frag{
		{Kind: fragImage, X: 0, Y: 5, W: 2, H: 1, Img: img, Fixed: true},
		{Kind: fragFill, X: 0, Y: 5, W: 20, H: 1, Color: fill, Fixed: true},
	}}
	placements = nil
	PaintLayoutGraphics(g, ly, 5, 0, 10, nil, RGB(255, 255, 255), &placements)
	if len(placements) != 1 {
		t.Fatalf("fixed image produced %d placements, want 1", len(placements))
	}
}

func TestImagesUnderTextUseBlocks(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	g := NewGrid(20, 10, RGB(255, 255, 255), RGB(255, 255, 255))

	// Text painted over an image (a caption or tab strip on a hero photo)
	// must keep the image in half-block art: a native placement would be
	// composited above the text grid and hide it.
	ly := &Layout{Frags: []Frag{
		{Kind: fragImage, X: 0, Y: 0, W: 8, H: 2, Img: img},
		{Kind: fragText, X: 1, Y: 0, W: 4, H: 1, Text: "tabs", Style: &Style{}},
	}}
	var placements []ImagePlacement
	PaintLayoutGraphics(g, ly, 0, 0, 10, nil, RGB(255, 255, 255), &placements)
	if len(placements) != 0 {
		t.Fatalf("image under text produced %d placements, want half-block art", len(placements))
	}

	// Text beside the image, not on it, leaves the native placement alone.
	ly = &Layout{Frags: []Frag{
		{Kind: fragImage, X: 0, Y: 0, W: 8, H: 2, Img: img},
		{Kind: fragText, X: 10, Y: 0, W: 4, H: 1, Text: "side", Style: &Style{}},
	}}
	placements = nil
	PaintLayoutGraphics(g, ly, 0, 0, 10, nil, RGB(255, 255, 255), &placements)
	if len(placements) != 1 {
		t.Fatalf("image beside text produced %d placements, want 1", len(placements))
	}
}

// svgAlpha decodes an SVG document and returns the alpha of one pixel.
func svgAlpha(t *testing.T, doc string, x, y int) uint32 {
	t.Helper()
	img, err := decodeSVG([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, a := img.At(x, y).RGBA()
	return a
}

func TestDecodeSVGFillRule(t *testing.T) {
	// Nested squares: evenodd leaves the centre hollow, nonzero fills it.
	nested := `<path fill-rule="%s" fill="#00ff00" d="M2 2h16v16H2z M6 6h8v8H6z"/>`
	doc := func(rule string) string {
		return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20">` +
			fmt.Sprintf(nested, rule) + `</svg>`
	}
	if a := svgAlpha(t, doc("evenodd"), 10, 10); a != 0 {
		t.Fatalf("evenodd centre alpha = %d, want a hole", a)
	}
	if a := svgAlpha(t, doc("evenodd"), 3, 10); a == 0 {
		t.Fatal("evenodd ring was not painted")
	}
	if a := svgAlpha(t, doc("nonzero"), 10, 10); a == 0 {
		t.Fatal("nonzero centre should be filled")
	}
}

func TestDecodeSVGClipPath(t *testing.T) {
	doc := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20">
		<defs><clipPath id="c"><rect width="10" height="20"/></clipPath></defs>
		<rect width="20" height="20" fill="#0000ff" clip-path="url(#c)"/>
	</svg>`
	if a := svgAlpha(t, doc, 2, 10); a == 0 {
		t.Fatal("pixel inside the clip was not painted")
	}
	if a := svgAlpha(t, doc, 17, 10); a != 0 {
		t.Fatal("clip-path did not trim the box")
	}
}

func TestDecodeSVGCompactArcFlags(t *testing.T) {
	// "0110" packs both arc flags and the x coordinate; reading it as one
	// number drops the rest of the path.
	doc := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
		<path d="M2 12a9.564 9.564 0 0110 4.844" fill="none" stroke="#000000" stroke-width="2"/>
	</svg>`
	if a := svgAlpha(t, doc, 8, 14); a == 0 {
		t.Fatal("arc with separator-less flags was not drawn")
	}
}

func TestDecodeSVGBoundsDashStroke(t *testing.T) {
	// A tiny dash array over a long path must not make the stroker take one
	// step per dash; the renderer draws it solid instead.
	long := strings.Repeat("L100 1", 500)
	doc := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 2">
		<path d="M0 1` + long + `" stroke="#000000" stroke-width="1" stroke-dasharray="0.001 0.001"/>
	</svg>`
	done := make(chan error, 1)
	go func() {
		_, err := decodeSVG([]byte(doc))
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("hostile dash array failed to render: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("hostile dash array stalled the renderer")
	}
}

func TestTranslucentFillBlendsWithBackdrop(t *testing.T) {
	g := NewGrid(2, 1, RGB(0, 0, 0), RGB(255, 255, 255))
	g.Fill(0, 0, 1, 1, Color{R: 0, G: 0, B: 0, A: 51, Set: true})
	if c := g.Cells[0].BG; c.R != 204 || c.G != 204 || c.B != 204 || c.A != 255 {
		t.Fatalf("20%% black over white = %+v, want 204,204,204,255", c)
	}
	// Stacking a second scrim darkens the result further.
	g.Fill(0, 0, 1, 1, Color{R: 0, G: 0, B: 0, A: 51, Set: true})
	if c := g.Cells[0].BG; c.R >= 204 {
		t.Fatalf("second scrim did not darken: %+v", c)
	}
	// An opaque fill still replaces the backdrop.
	g.Fill(1, 0, 1, 1, RGB(10, 20, 30))
	if got := g.Cells[1].BG; got != RGB(10, 20, 30) {
		t.Fatalf("opaque fill = %+v", got)
	}
}
