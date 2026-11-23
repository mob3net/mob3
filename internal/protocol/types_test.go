package protocol

import "testing"

func TestRunsEqual(t *testing.T) {
	a := []ScaledRun{
		{X: 1, Y: 2, Cells: 4, Scale: 2, Text: "Hi", FG: Color{R: 1, Set: true}},
		{X: 6, Y: 2, Cells: 2, Scale: 3, Text: "x", Attrs: AttrBold},
	}
	b := append([]ScaledRun(nil), a...)
	if !RunsEqual(a, b) {
		t.Fatal("identical runs reported different")
	}
	if !RunsEqual(nil, nil) {
		t.Fatal("nil runs reported different")
	}

	b[0].Y++
	if RunsEqual(a, b) {
		t.Fatal("moved run reported equal")
	}
	b = append([]ScaledRun(nil), a...)
	b[1].Text = "y"
	if RunsEqual(a, b) {
		t.Fatal("changed run text reported equal")
	}
	b = append([]ScaledRun(nil), a...)
	b = append(b, ScaledRun{X: 0, Y: 0, Cells: 1, Scale: 2, Text: "z"})
	if RunsEqual(a, b) {
		t.Fatal("added run reported equal")
	}

	// A style-only change is repainted by rewriting the run's top row, so it
	// must not force a whole-screen repaint.
	b = append([]ScaledRun(nil), a...)
	b[0].FG = Color{R: 200, G: 10, Set: true}
	b[0].Attrs = AttrUnderline
	if !RunsEqual(a, b) {
		t.Fatal("style-only change reported as a run move")
	}
}
