package engine

import (
	"math"
	"testing"
)

// iframeFrame returns the FragFrame node for an iframe and the text fragment
// that labels it.
func iframeFrame(t *testing.T, ly *Layout, n *Node) (*Frag, *Frag) {
	t.Helper()
	var frame, label *Frag
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Node != n {
			continue
		}
		switch f.Kind {
		case FragFrame:
			frame = f
		case FragText:
			label = f
		}
	}
	return frame, label
}

func TestIFrameReservesAttributeBox(t *testing.T) {
	doc := ParseHTML(`<body style="margin:0"><iframe width="160" height="48"></iframe></body>`)
	ly := BuildLayout(doc, nil, 80)
	n := doc.FindFirst("iframe")
	frame, label := iframeFrame(t, ly, n)
	if frame == nil {
		t.Fatal("iframe did not reserve a frame box")
	}
	if want := int(math.Round(160 / cellW)); frame.W != want {
		t.Fatalf("frame width = %d, want %d", frame.W, want)
	}
	if want := int(math.Round(48 / cellH)); frame.H != want {
		t.Fatalf("frame height = %d, want %d", frame.H, want)
	}
	if frame.Color != RGB(0, 0, 0) || !Opaque(frame.Color) {
		t.Fatalf("frame color = %+v, want opaque black", frame.Color)
	}
	if label == nil || label.Text != "iframe" {
		t.Fatalf("label = %+v, want iframe", label)
	}
}

func TestIFrameCSSOverridesAttributes(t *testing.T) {
	doc := ParseHTML(`<body style="margin:0"><iframe width="160" height="48" style="width:80px;height:32px"></iframe></body>`)
	ly := BuildLayout(doc, nil, 80)
	frame, _ := iframeFrame(t, ly, doc.FindFirst("iframe"))
	if frame == nil {
		t.Fatal("iframe did not reserve a frame box")
	}
	if want := int(math.Round(80 / cellW)); frame.W != want {
		t.Fatalf("frame width = %d, want CSS width %d", frame.W, want)
	}
	if want := int(math.Round(32 / cellH)); frame.H != want {
		t.Fatalf("frame height = %d, want CSS height %d", frame.H, want)
	}
}

func TestIFrameNarrowBoxUsesShortLabel(t *testing.T) {
	doc := ParseHTML(`<body style="margin:0"><iframe style="width:24px;height:16px"></iframe></body>`)
	ly := BuildLayout(doc, nil, 80)
	frame, label := iframeFrame(t, ly, doc.FindFirst("iframe"))
	if frame == nil || frame.W >= iframeLabelCells {
		t.Fatalf("frame = %+v, want a box narrower than the full label", frame)
	}
	if label == nil || label.Text != "i" {
		t.Fatalf("label = %+v, want i", label)
	}
}

func TestIFrameDefaultBox(t *testing.T) {
	doc := ParseHTML(`<body style="margin:0"><p>before</p><iframe></iframe></body>`)
	ly := BuildLayout(doc, nil, 80)
	frame, label := iframeFrame(t, ly, doc.FindFirst("iframe"))
	if frame == nil {
		t.Fatal("unsized iframe did not reserve the replaced default")
	}
	if frame.W != iframeDefaultWidthCells() || frame.H != iframeDefaultHeightCells() {
		t.Fatalf("frame = %dx%d, want %dx%d", frame.W, frame.H, iframeDefaultWidthCells(), iframeDefaultHeightCells())
	}
	if label == nil || label.Text != "iframe" {
		t.Fatalf("label = %+v, want iframe", label)
	}
}

func TestIFrameBlockReservesBox(t *testing.T) {
	doc := ParseHTML(`<body style="margin:0"><iframe style="display:block;width:160px;height:48px">fallback text</iframe><p>after</p></body>`)
	ly := BuildLayout(doc, nil, 80)
	n := doc.FindFirst("iframe")
	frame, label := iframeFrame(t, ly, n)
	if frame == nil {
		t.Fatal("block iframe did not reserve a frame box")
	}
	if want := int(math.Round(160 / cellW)); frame.W != want {
		t.Fatalf("frame width = %d, want %d", frame.W, want)
	}
	if want := int(math.Round(48 / cellH)); frame.H != want {
		t.Fatalf("frame height = %d, want %d", frame.H, want)
	}
	if label == nil || label.Text != "iframe" {
		t.Fatalf("label = %+v, want iframe", label)
	}
	for _, f := range ly.Frags {
		if f.Kind == FragText && f.Node != n && f.Text == "fallback text" {
			t.Fatal("iframe fallback children were laid out")
		}
	}
}

func TestIFrameBlockDefaultHeight(t *testing.T) {
	doc := ParseHTML(`<body style="margin:0"><iframe style="display:block;width:160px"></iframe></body>`)
	ly := BuildLayout(doc, nil, 80)
	frame, _ := iframeFrame(t, ly, doc.FindFirst("iframe"))
	if frame == nil {
		t.Fatal("block iframe did not reserve a frame box")
	}
	if want := int(math.Round(160 / cellW)); frame.W != want {
		t.Fatalf("frame width = %d, want %d", frame.W, want)
	}
	if frame.H != iframeDefaultHeightCells() {
		t.Fatalf("frame height = %d, want replaced default %d", frame.H, iframeDefaultHeightCells())
	}
}

func TestIFramePlaceholderPaintsBlackBox(t *testing.T) {
	doc := ParseHTML(`<body style="margin:0"><iframe style="width:80px;height:32px"></iframe></body>`)
	ly := BuildLayout(doc, nil, 40)
	g := NewGrid(40, 8, RGB(0x1f, 0x23, 0x28), RGB(255, 255, 255))
	PaintLayout(g, ly, 0, 0, 8, nil, RGB(0x1f, 0x23, 0x28))
	frame, _ := iframeFrame(t, ly, doc.FindFirst("iframe"))
	if frame == nil {
		t.Fatal("iframe did not reserve a frame box")
	}
	for y := frame.Y; y < frame.Y+frame.H; y++ {
		for x := frame.X; x < frame.X+frame.W; x++ {
			if got := g.Cells[y*g.W+x].BG; got != RGB(0, 0, 0) {
				t.Fatalf("cell %d,%d background = %+v, want black", x, y, got)
			}
		}
	}
	wantFG := RGB(0x9a, 0xa0, 0xa6)
	if got := g.Cells[frame.Y*g.W+frame.X]; got.R != 'i' || got.FG != wantFG {
		t.Fatalf("label cell = %q/%+v, want gray i", got.R, got.FG)
	}
}

func TestIFramePercentWidthUsesSizedAncestor(t *testing.T) {
	doc := ParseHTML(`<body style="margin:0"><span style="display:inline-block;width:80px"><iframe style="width:100%;height:32px"></iframe></span></body>`)
	ly := BuildLayout(doc, nil, 80)
	frame, _ := iframeFrame(t, ly, doc.FindFirst("iframe"))
	if frame == nil {
		t.Fatal("iframe did not reserve a frame box")
	}
	if want := int(math.Round(80 / cellW)); frame.W != want {
		t.Fatalf("frame width = %d, want ancestor width %d", frame.W, want)
	}
}

func TestIFrameAspectRatioSuppliesHeight(t *testing.T) {
	doc := ParseHTML(`<body style="margin:0"><iframe style="width:160px;aspect-ratio:2"></iframe></body>`)
	ly := BuildLayout(doc, nil, 80)
	frame, _ := iframeFrame(t, ly, doc.FindFirst("iframe"))
	if frame == nil {
		t.Fatal("iframe did not reserve a frame box")
	}
	want := int(math.Round(float64(frame.W) * cellW / (2 * cellH)))
	if frame.H != want {
		t.Fatalf("frame height = %d, want aspect height %d", frame.H, want)
	}
}
