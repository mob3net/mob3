package engine

import (
	"testing"

	"browser/internal/textwidth"
)

func TestWideRunesPaint(t *testing.T) {
	ly := build(`<p>日本語テキスト</p>`, 10)
	g := NewGrid(10, 10, RGB(0, 0, 0), RGB(255, 255, 255))
	PaintLayout(g, ly, 0, 0, 10, nil, RGB(0, 0, 0))
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			c := g.Cells[y*g.W+x]
			if c.Cont && (x == 0 || g.Cells[y*g.W+x-1].R == ' ') {
				t.Fatalf("orphan continuation cell at %d,%d", x, y)
			}
		}
	}
}

func TestRuneWidths(t *testing.T) {
	cases := []struct {
		s string
		w int
	}{
		{"abc", 3}, {"日本", 4}, {"é", 1}, {"a\u0301", 1}, {"★", 1},
	}
	for _, c := range cases {
		if got := textwidth.StringWidth(c.s); got != c.w {
			t.Errorf("width(%q) = %d, want %d", c.s, got, c.w)
		}
	}
	if got := textwidth.Truncate("abcdef", 3); got != "abc" {
		t.Errorf("truncate = %q", got)
	}
	if got := textwidth.Pad("ab", 5); got != "ab   " {
		t.Errorf("pad = %q", got)
	}
	if got := textwidth.Fit("abc", 5); got != "abc  " {
		t.Errorf("fit short = %q", got)
	}
	if got := textwidth.Fit("abcdef", 5); got != "abcd…" {
		t.Errorf("fit long = %q", got)
	}
}

func TestTextTransform(t *testing.T) {
	if got := ApplyTextTransform("hello world", "uppercase"); got != "HELLO WORLD" {
		t.Errorf("uppercase = %q", got)
	}
	if got := ApplyTextTransform("hello world", "capitalize"); got != "Hello World" {
		t.Errorf("capitalize = %q", got)
	}
	if got := ExpandTabs("a\tb"); got != "a   b" {
		t.Errorf("tabs = %q", got)
	}
}
