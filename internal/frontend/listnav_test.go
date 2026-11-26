package frontend

import "testing"

func TestMoveListKeys(t *testing.T) {
	cases := []struct {
		key     string
		index   int
		count   int
		want    int
		handled bool
	}{
		{"up", 1, 3, 0, true},
		{"k", 1, 3, 0, true},
		{"ctrl-p", 1, 3, 0, true},
		{"down", 1, 3, 2, true},
		{"j", 1, 3, 2, true},
		{"ctrl-n", 1, 3, 2, true},
		{"home", 2, 3, 0, true},
		{"g", 2, 3, 0, true},
		{"end", 0, 3, 2, true},
		{"G", 0, 3, 2, true},
		{"up", 0, 3, 0, true},
		{"down", 2, 3, 2, true},
		{"down", 5, 3, 2, true},
		{"up", -1, 3, 0, true},
		{"end", 1, 0, 0, true},
		{"up", 1, 0, 0, true},
		{"d", 1, 3, 1, false},
		{"u", 1, 3, 1, false},
		{" ", 1, 3, 1, false},
		{"space", 1, 3, 1, false},
		{"pgdn", 1, 3, 1, false},
		{"b", 1, 3, 1, false},
		{"x", 1, 3, 1, false},
	}
	for _, c := range cases {
		got, handled := moveList(c.key, c.index, c.count)
		if got != c.want || handled != c.handled {
			t.Errorf("moveList(%q, %d, %d) = %d, %v; want %d, %v",
				c.key, c.index, c.count, got, handled, c.want, c.handled)
		}
	}
}

func TestMoveChoiceClamps(t *testing.T) {
	if got := moveChoice(0, -1, 3); got != 0 {
		t.Fatalf("moveChoice below zero = %d", got)
	}
	if got := moveChoice(2, 1, 3); got != 2 {
		t.Fatalf("moveChoice above end = %d", got)
	}
	if got := moveChoice(0, 1, 0); got != 0 {
		t.Fatalf("moveChoice empty list = %d", got)
	}
}
