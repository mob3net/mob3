package engine

import (
	"math"
	"testing"
)

func TestParseBackgroundGradient(t *testing.T) {
	g, idx := parseBackgroundGradient("linear-gradient(to right, #ff0000, #0000ff)", RGB(0, 0, 0))
	if g == nil || idx != 0 {
		t.Fatalf("gradient = %+v, idx=%d", g, idx)
	}
	if g.Kind != GradientLinear || math.Abs(g.Angle-90) > 0.01 {
		t.Fatalf("linear gradient = %+v, want 90deg", g)
	}
	if len(g.Stops) != 2 || g.Stops[0].Pos != 0 || g.Stops[1].Pos != 1 {
		t.Fatalf("stops = %+v", g.Stops)
	}
	if g.Stops[0].Color.R != 255 || g.Stops[1].Color.B != 255 {
		t.Fatalf("stop colors = %+v", g.Stops)
	}

	g, _ = parseBackgroundGradient("repeating-linear-gradient(45deg, rgba(255,0,0,0.5) 10%, #0000ff 20%)", RGB(0, 0, 0))
	if g == nil || !g.Repeat || math.Abs(g.Angle-45) > 0.01 {
		t.Fatalf("repeating gradient = %+v", g)
	}
	if len(g.Stops) != 2 || g.Stops[0].Pos != 0.1 || g.Stops[1].Pos != 0.2 {
		t.Fatalf("repeating stops = %+v", g.Stops)
	}

	g, _ = parseBackgroundGradient("radial-gradient(circle at 20% 30%, #ff0000, #0000ff)", RGB(0, 0, 0))
	if g == nil || g.Kind != GradientRadial || math.Abs(g.CenterX-0.2) > 0.01 || math.Abs(g.CenterY-0.3) > 0.01 {
		t.Fatalf("radial gradient = %+v", g)
	}

	if g, _ := parseBackgroundGradient("linear-gradient(red)", RGB(0, 0, 0)); g != nil {
		t.Fatalf("one-stop gradient parsed: %+v", g)
	}
}

func TestBackgroundGradientPaints(t *testing.T) {
	doc := ParseHTML(`<html><body style="margin:0"><div id="hero" style="width:40px;height:32px;background-image:linear-gradient(to right,#ff0000,#0000ff)">hero</div></body></html>`)
	ly := BuildLayout(doc, nil, 40)
	hero := findByID(doc, "hero")
	box, ok := ly.Boxes[hero]
	if !ok || box.W < 2 || box.H < 1 {
		t.Fatalf("hero box = %+v", box)
	}
	pageFG := RGB(0x1f, 0x23, 0x28)
	g := NewGrid(40, ly.Height, pageFG, ly.PageBG)
	PaintLayout(g, ly, 0, 0, ly.Height, nil, pageFG)
	y := box.Y
	if y < 0 || y >= g.H {
		t.Fatalf("hero y = %d, grid height = %d", y, g.H)
	}
	left := g.Cells[y*g.W+box.X].BG
	right := g.Cells[y*g.W+box.X+box.W-1].BG
	if !left.Set || left.R < 200 || left.B > 80 {
		t.Fatalf("left gradient cell = %+v, want red", left)
	}
	if !right.Set || right.B < 200 || right.R > 80 {
		t.Fatalf("right gradient cell = %+v, want blue", right)
	}
}

func TestBackgroundVarColor(t *testing.T) {
	doc := ParseHTML(`<html><body style="margin:0"><div id="card" style="--brand:#00ff00;background:var(--brand);width:20px">card</div></body></html>`)
	ly := BuildLayout(doc, nil, 40)
	card := findByID(doc, "card")
	st := ly.Styles[card]
	if st == nil || st.Background.R != 0 || st.Background.G != 255 || st.Background.B != 0 {
		t.Fatalf("var background = %+v", st)
	}
	box := ly.Boxes[card]
	if box.W < 1 || box.H < 1 {
		t.Fatalf("card box = %+v", box)
	}
	pageFG := RGB(0x1f, 0x23, 0x28)
	g := NewGrid(40, ly.Height, pageFG, ly.PageBG)
	PaintLayout(g, ly, 0, 0, ly.Height, nil, pageFG)
	cell := g.Cells[box.Y*g.W+box.X].BG
	if cell.G != 255 || cell.R != 0 || cell.B != 0 {
		t.Fatalf("var background cell = %+v", cell)
	}
}

func TestBackgroundVarGradient(t *testing.T) {
	doc := ParseHTML(`<html><body style="margin:0"><div id="hero" style="--paint:linear-gradient(to bottom,#ff0000,#0000ff);background-image:var(--paint);width:20px;height:32px">hero</div></body></html>`)
	ly := BuildLayout(doc, nil, 40)
	hero := findByID(doc, "hero")
	st := ly.Styles[hero]
	if st == nil || st.BackgroundGradient == nil {
		t.Fatalf("var gradient = %+v", st)
	}
	if st.BackgroundGradient.Angle != 180 || len(st.BackgroundGradient.Stops) != 2 {
		t.Fatalf("var gradient = %+v", st.BackgroundGradient)
	}
}
