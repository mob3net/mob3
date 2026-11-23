package engine

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"time"
)

func TestInlineSVGFallbackLabel(t *testing.T) {
	ly := build(`<div><svg aria-label="Google"><path d="M0 0"/></svg><svg aria-hidden="true"></svg><svg><title>Visible title</title></svg></div>`, 40)
	text := ""
	for _, f := range ly.Frags {
		if f.Kind == fragText {
			text += f.Text
		}
	}
	if !strings.Contains(text, "▣") {
		t.Fatalf("icon-only SVG placeholder missing: %q", text)
	}
	if !strings.Contains(text, "Visible title") {
		t.Fatalf("SVG title fallback missing: %q", text)
	}
	if strings.Contains(text, "Google") || strings.Contains(text, "aria-hidden") {
		t.Fatalf("unexpected SVG accessible-name text: %q", text)
	}
}

func TestBrokenDecorativeImageSkipped(t *testing.T) {
	ly := build(`<p>before <img src="bad.svg"> after</p>`, 40)
	for _, f := range ly.Frags {
		if f.Kind == fragText && strings.Contains(f.Text, "[image:") {
			t.Fatalf("decorative broken image rendered: %q", f.Text)
		}
	}
}

func TestImageHalfBlocksSize(t *testing.T) {
	doc := ParseHTML(`<p><img src="x" alt="x"></p>`)
	img := image.NewRGBA(image.Rect(0, 0, 160, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 160; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 100, 255})
		}
	}
	doc.FindFirst("img").Img = img
	ly := BuildLayout(doc, nil, 60)
	var f *Frag
	for i := range ly.Frags {
		if ly.Frags[i].Kind == fragImage {
			f = &ly.Frags[i]
		}
	}
	if f == nil {
		t.Fatal("no image fragment")
	}
	if f.W != 20 || f.H != 5 {
		t.Fatalf("image size = %dx%d, want 20x5", f.W, f.H)
	}
}

func TestBrokenImagePlaceholder(t *testing.T) {
	ly := build(`<p><img alt="" width="40" height="40"></p>`, 40)
	if textFrag(ly, "░") == nil {
		t.Fatal("explicitly sized broken image has no placeholder")
	}
	ly = build(`<p><img alt="" width="8" height="8"></p>`, 40)
	for _, f := range ly.Frags {
		if f.Kind == fragText && strings.Contains(f.Text, "░") {
			t.Fatalf("1-cell broken image showed a placeholder: %q", f.Text)
		}
	}
}

func TestBlockImageFragment(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 160, 90))
	doc := ParseHTML(`<html><body><img id="hero" src="hero.jpg" alt="hero" style="display:block;width:32px"></body></html>`)
	node := doc.FindFirst("img")
	node.Img = img
	ly := BuildLayout(doc, nil, 80)
	for _, f := range ly.Frags {
		if f.Kind == fragImage && f.Node == node {
			if f.W != 4 || f.H < 1 {
				t.Fatalf("block image = %dx%d, want 4-wide image fragment", f.W, f.H)
			}
			return
		}
	}
	t.Fatal("display:block image did not produce an image fragment")
}

func TestAbsoluteImageInsideAspectPlaceholder(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 160, 90))
	doc := ParseHTML(`<html><body>
<div style="position:relative;width:100%;padding-bottom:56.25%">
  <img id="hero" src="hero.jpg" alt="hero" style="display:block;position:absolute;top:0;right:0;bottom:0;left:0;width:100%;height:100%">
</div>
<p id="after">after</p>
</body></html>`)
	node := doc.FindFirst("img")
	node.Img = img
	ly := BuildLayout(doc, nil, 40)
	found := false
	for _, f := range ly.Frags {
		if f.Kind == fragImage && f.Node == node {
			found = true
			if f.W <= 0 || f.H <= 0 {
				t.Fatalf("absolute image size = %dx%d", f.W, f.H)
			}
		}
	}
	if !found {
		t.Fatal("absolute image did not produce an image fragment")
	}
	after := 0
	for _, f := range ly.Frags {
		if f.Kind == fragText && f.Text == "after" {
			after = f.Y
		}
	}
	if after == 0 || after > 20 {
		t.Fatalf("paragraph after aspect placeholder is at row %d, want the placeholder height to use its width", after)
	}
}

func TestObjectFitCoverAndContain(t *testing.T) {
	doc := ParseHTML(`<img id="c" style="display:block;width:160px;height:160px;object-fit:cover">
<img id="t" style="display:block;width:160px;height:160px;object-fit:contain">`)
	src := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for _, img := range doc.FindAll(func(n *Node) bool { return n.Tag == "img" }) {
		img.Img = src
	}
	ly := BuildLayout(doc, nil, 100)
	got := map[string]*Frag{}
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind == fragImage && f.Node != nil {
			got[f.Node.Attr("id")] = f
		}
	}
	cover := got["c"]
	if cover == nil || cover.W != 20 || cover.H != 10 {
		t.Fatalf("cover frag = %+v", cover)
	}
	if cover.Crop.Dx() != 100 || cover.Crop.Dy() != 100 {
		t.Fatalf("cover crop = %v, want 100x100", cover.Crop)
	}
	contain := got["t"]
	if contain == nil || contain.W != 20 || contain.H != 5 {
		t.Fatalf("contain frag = %+v", contain)
	}
	if contain.Crop != (image.Rectangle{}) {
		t.Fatalf("contain crop = %v, want full image", contain.Crop)
	}
}

func TestObjectFitNone(t *testing.T) {
	doc := ParseHTML(`<img id="s" style="display:block;width:160px;height:160px;object-fit:none">
<img id="l" style="display:block;width:80px;height:80px;object-fit:none;object-position:right bottom">`)
	small := image.NewRGBA(image.Rect(0, 0, 80, 80))
	large := image.NewRGBA(image.Rect(0, 0, 400, 400))
	for _, img := range doc.FindAll(func(n *Node) bool { return n.Tag == "img" }) {
		if img.Attr("id") == "s" {
			img.Img = small
		} else {
			img.Img = large
		}
	}
	ly := BuildLayout(doc, nil, 100)
	got := map[string]*Frag{}
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind == fragImage && f.Node != nil {
			got[f.Node.Attr("id")] = f
		}
	}
	// 80x80 px fits inside a 20x10 cell box and stays at its intrinsic size.
	if f := got["s"]; f == nil || f.W != 10 || f.H != 5 || f.Crop != (image.Rectangle{}) {
		t.Fatalf("small none frag = %+v", f)
	}
	// 400x400 px overflows a 10x5 cell box and is cropped from the bottom-right.
	f := got["l"]
	if f == nil || f.W != 10 || f.H != 5 {
		t.Fatalf("large none frag = %+v", f)
	}
	if f.Crop.Dx() != 80 || f.Crop.Dy() != 80 || f.Crop.Max != (image.Point{400, 400}) {
		t.Fatalf("large none crop = %v, want 80x80 at bottom-right", f.Crop)
	}
}

func TestBlockAspectRatio(t *testing.T) {
	ly := build(`<div style="width:160px;aspect-ratio:2/1"></div>after`, 40)
	for _, f := range ly.Frags {
		if f.Kind == fragText && strings.Contains(f.Text, "after") {
			if f.Y != 5 {
				t.Fatalf("text after aspect-ratio box y = %d, want 5", f.Y)
			}
			return
		}
	}
	t.Fatal("text after box missing")
}

func TestLayoutImagesOffUsesAltText(t *testing.T) {
	doc := ParseHTML(`<p>a<img alt="pic" src="x.png">b</p>`)
	for _, img := range doc.FindAll(func(n *Node) bool { return n.Tag == "img" }) {
		img.Img = image.NewRGBA(image.Rect(0, 0, 160, 80))
	}
	ly := BuildLayoutBudgetDeadlineImages(doc, nil, 40, 0, false, false, false, false, true, time.Time{}, DefaultLayoutBudget)
	found := false
	for _, f := range ly.Frags {
		if f.Kind == fragImage {
			t.Fatal("imagesOff layout produced an image fragment")
		}
		if f.Kind == fragText && strings.Contains(f.Text, "pic") {
			found = true
		}
	}
	if !found {
		t.Fatal("imagesOff layout did not show the alt label")
	}
}

func TestImageAltLabelClipsToBox(t *testing.T) {
	long := strings.Repeat("very long alternative text ", 6)
	cases := []struct {
		name string
		src  string
	}{
		{"inline", `<div style="width:160px"><img alt="` + long + `" style="width:100%"></div>`},
		{"absolute", `<div style="position:relative;width:160px;height:90px"><img alt="` + long + `" style="position:absolute;top:0;left:0;width:100%;height:100%"></div>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ly := build(tc.src, 100)
			found := false
			for _, f := range ly.Frags {
				if f.Kind == fragText && strings.HasPrefix(strings.TrimSpace(f.Text), "[image:") {
					found = true
					if f.W > 20 || f.X+f.W > 21 {
						t.Fatalf("alt label = x %d w %d, want within a 20-cell box", f.X, f.W)
					}
				}
			}
			if !found {
				t.Fatal("no alt label fragment")
			}
		})
	}
}

func TestImageAltLabelUsesMarkerInTinyBox(t *testing.T) {
	ly := build(`<div style="width:32px"><img alt="long alternative text" style="width:100%"></div>`, 100)
	for _, f := range ly.Frags {
		if f.Kind == fragText {
			if strings.HasPrefix(f.Text, "[image:") {
				t.Fatalf("tiny image drew a full label: %q", f.Text)
			}
			if strings.Contains(f.Text, "▣") {
				return
			}
		}
	}
	t.Fatal("tiny image did not use the compact marker")
}

func TestImageLabelReservesCSSHeight(t *testing.T) {
	ly := build(`<div style="width:160px"><img alt="pic" style="display:block;width:80px;height:64px">after</div>`, 40)
	labelY, afterY := -1, -1
	for _, f := range ly.Frags {
		if f.Kind != fragText {
			continue
		}
		if strings.Contains(f.Text, "[image:") {
			labelY = f.Y
		}
		if strings.Contains(f.Text, "after") {
			afterY = f.Y
		}
	}
	if labelY < 0 || afterY < 0 {
		t.Fatal("label or trailing text missing")
	}
	if afterY-labelY != 4 {
		t.Fatalf("image label reserved %d rows, want 4", afterY-labelY)
	}
}

func TestBackgroundImageFragment(t *testing.T) {
	doc := ParseHTML(`<div id="hero" style="height:64px;background-image:url(hero.jpg);background-size:cover">text</div>`)
	ly := BuildLayout(doc, nil, 80)
	hero := findElementByID(doc, "hero")
	hero.Img = testImage(40, 20)
	// Rebuild with an image attached so a background fragment is produced.
	ly = BuildLayout(doc, nil, 80)
	var bg *Frag
	for i := range ly.Frags {
		if ly.Frags[i].Background && ly.Frags[i].Node == hero {
			bg = &ly.Frags[i]
			break
		}
	}
	if bg == nil {
		t.Fatal("no background fragment")
	}
	if bg.W != ly.Boxes[hero].W || bg.H != ly.Boxes[hero].H {
		t.Fatalf("background %dx%d, want the box %dx%d", bg.W, bg.H, ly.Boxes[hero].W, ly.Boxes[hero].H)
	}
	if bg.Crop.Empty() {
		t.Fatal("cover background has no crop")
	}
	// Backgrounds never become native placements: they must stay half-block
	// art so text painted on top remains visible.
	g := NewGrid(80, 20, RGB(0, 0, 0), RGB(255, 255, 255))
	var placements []ImagePlacement
	PaintLayoutGraphics(g, ly, 0, 0, 20, nil, RGB(0, 0, 0), &placements)
	for _, p := range placements {
		if p.Src == "hero.jpg" {
			t.Fatal("background image was queued as a native placement")
		}
	}
	if got := g.Cells[ly.Boxes[hero].Y*80+ly.Boxes[hero].X]; got.Img {
		t.Fatal("background should be painted as cells, not an image placement")
	}
}

func testImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 6), G: uint8(y * 12), B: 128, A: 255})
		}
	}
	return img
}

func TestBackgroundImagesOff(t *testing.T) {
	doc := ParseHTML(`<div id="hero" style="height:64px;background-image:url(hero.jpg)">text</div>`)
	BuildLayout(doc, nil, 80)
	hero := findElementByID(doc, "hero")
	hero.Img = testImage(40, 20)
	ly := BuildLayout(doc, nil, 80)
	ly.BackgroundsOff = true
	g := NewGrid(80, 20, RGB(0, 0, 0), RGB(255, 255, 255))
	PaintLayout(g, ly, 0, 0, 20, nil, RGB(0, 0, 0))
	cell := g.Cells[ly.Boxes[hero].Y*80+ly.Boxes[hero].X]
	if cell.BG != backgroundPlaceholderBG {
		t.Fatalf("suppressed background fill = %+v, want the neutral placeholder", cell.BG)
	}
	if cell.R == '▀' || cell.R == '▄' || cell.Img {
		t.Fatalf("suppressed background painted image cells: %+v", cell)
	}
}

func TestInlineSVGInFlexRowIsPainted(t *testing.T) {
	doc := ParseHTML(`<div style="display:flex;justify-content:center">` +
		`<svg viewBox="0 0 40 20"><circle cx="20" cy="10" r="8"/></svg></div>`)
	svg := doc.FindFirst("svg")
	svg.Img = image.NewRGBA(image.Rect(0, 0, 80, 40))
	ly := BuildLayout(doc, nil, 40)
	var got *Frag
	for i := range ly.Frags {
		if ly.Frags[i].Kind == fragImage && ly.Frags[i].Node == svg {
			got = &ly.Frags[i]
		}
	}
	if got == nil {
		t.Fatal("inline SVG used as a flex item produced no image fragment")
	}
	if got.W != 10 {
		t.Fatalf("flex SVG width = %d, want 10 cells", got.W)
	}
	if got.H < 2 {
		t.Fatalf("flex SVG height = %d", got.H)
	}
}

func TestInlineSVGInFlexRowFallsBackToLabel(t *testing.T) {
	ly := build(`<div style="display:flex;justify-content:center">`+
		`<svg viewBox="0 0 20 20"><title>Acme</title></svg></div>`, 30)
	if textFrag(ly, "Acme") == nil {
		t.Fatalf("SVG title missing from the flex row: %+v", ly.Frags)
	}
}

// TestInlineImagePercentInsideSizedWrapper covers a width:100% image inside a
// fixed-size inline wrapper (an avatar ring or w-xl icon): the percentage must
// resolve against the wrapper, not the terminal line, or the image grows to the
// display cap.
func TestInlineImagePercentInsideSizedWrapper(t *testing.T) {
	root := ParseHTML(`<html><head><style>
	body { margin: 0; }
	.outer { display: inline-flex; width: 2rem; height: 2rem; }
	.holder { display: inline-block; width: 100%; height: 100%; }
	.icon { width: 100%; height: 100%; }
	</style></head><body>
	<span class="outer"><span class="holder"><img class="icon" width="32" height="32"></span></span>
	</body></html>`)
	img, _ := findSelectorNode(root, ".icon")
	im := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			im.Set(x, y, color.RGBA{200, 0, 0, 255})
		}
	}
	img.Img = im
	ly := BuildLayout(root, fixtureAuthorRules(root), 100)
	for i := range ly.Frags {
		if f := &ly.Frags[i]; f.Kind == fragImage {
			if f.W != 4 || f.H != 2 {
				t.Fatalf("image box = %dx%d cells, want 4x2 inside the 2rem wrapper", f.W, f.H)
			}
			return
		}
	}
	t.Fatal("image fragment not found")
}
