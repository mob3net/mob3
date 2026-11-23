package engine

import "testing"

func TestGridLinesPlainText(t *testing.T) {
	g := NewGrid(5, 2, RGB(0, 0, 0), RGB(255, 255, 255))
	g.Put(0, 0, 'h', nil, RGB(0, 0, 0), Color{})
	g.Put(1, 0, 'i', nil, RGB(0, 0, 0), Color{})
	g.Put(0, 1, 'x', nil, RGB(0, 0, 0), Color{})
	lines := GridLines(g)
	if len(lines) != 2 || lines[0] != "hi" || lines[1] != "x" {
		t.Fatalf("lines = %#v", lines)
	}
}

func TestGridLinesMatchesMonoFrame(t *testing.T) {
	g := NewGrid(8, 1, RGB(0, 0, 0), RGB(255, 255, 255))
	g.Put(0, 0, 'A', nil, RGB(0, 0, 0), Color{})
	g.Put(1, 0, 'B', nil, RGB(0, 0, 0), Color{})
	g.Cells[2].Img = true
	g.Cells[2].R = 'C'
	// A scaled run covers three cells, a skipped cell follows, then a tail.
	g.Cells[3].Run = 1
	g.Cells[4].Skip = true
	g.Cells[5].Skip = true
	g.Cells[6].Skip = true
	g.Put(7, 0, 'Z', nil, RGB(0, 0, 0), Color{})
	g.Runs = []ScaledRun{{X: 3, Y: 0, Cells: 3, Scale: 2, Text: "Hi"}}
	got := GridLines(g)
	if len(got) != 1 {
		t.Fatalf("rows = %d", len(got))
	}
	if got[0] != "AB▒    Z" {
		t.Fatalf("row = %q", got[0])
	}
}

func TestGridLinesRejectsBrokenGrid(t *testing.T) {
	if GridLines(nil) != nil {
		t.Fatal("nil grid produced lines")
	}
	if GridLines(&Grid{W: 2, H: 2, Cells: make([]Cell, 3)}) != nil {
		t.Fatal("short grid produced lines")
	}
}
