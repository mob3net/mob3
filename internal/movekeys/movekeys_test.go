package movekeys

import "testing"

func TestLookup(t *testing.T) {
	cases := []struct {
		key  string
		want Move
	}{
		{"j", LineDown}, {"down", LineDown}, {"ctrl-n", LineDown},
		{"k", LineUp}, {"up", LineUp}, {"ctrl-p", LineUp},
		{"d", HalfDown}, {"ctrl-d", HalfDown},
		{"u", HalfUp}, {"ctrl-u", HalfUp},
		{" ", PageDown}, {"space", PageDown}, {"pgdn", PageDown}, {"ctrl-v", PageDown},
		{"b", PageUp}, {"pgup", PageUp},
		{"g", Top}, {"home", Top},
		{"G", Bottom}, {"end", Bottom},
	}
	for _, c := range cases {
		got, ok := Lookup(c.key)
		if !ok || got != c.want {
			t.Errorf("Lookup(%q) = %v, %v; want %v, true", c.key, got, ok, c.want)
		}
	}
	for _, key := range []string{"", "x", "enter", "tab", "f"} {
		if got, ok := Lookup(key); ok {
			t.Errorf("Lookup(%q) = %v, true; want false", key, got)
		}
	}
}

func TestWheelDelta(t *testing.T) {
	cases := []struct{ wheel, want int }{
		{-1, -WheelLines},
		{1, WheelLines},
		{0, 0},
		{2, WheelLines},
		{-2, -WheelLines},
	}
	for _, c := range cases {
		if got := WheelDelta(c.wheel); got != c.want {
			t.Errorf("WheelDelta(%d) = %d, want %d", c.wheel, got, c.want)
		}
	}
}

func TestDelta(t *testing.T) {
	cases := []struct {
		move Move
		rows int
		want int
		ok   bool
	}{
		{LineUp, 20, -1, true},
		{LineDown, 20, 1, true},
		{HalfUp, 20, -10, true},
		{HalfDown, 20, 10, true},
		{HalfDown, 1, 0, true},
		{HalfDown, 0, 0, true},
		{PageUp, 20, -19, true},
		{PageDown, 20, 19, true},
		{PageUp, 1, 0, true},
		{PageDown, 0, 0, true},
		{Top, 20, 0, false},
		{Bottom, 20, 0, false},
	}
	for _, c := range cases {
		got, ok := c.move.Delta(c.rows)
		if got != c.want || ok != c.ok {
			t.Errorf("(%v).Delta(%d) = %d, %v; want %d, %v", c.move, c.rows, got, ok, c.want, c.ok)
		}
	}
}
