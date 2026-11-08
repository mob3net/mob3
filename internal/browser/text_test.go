package browser

import "testing"

func TestRuneWidths(t *testing.T) {
	cases := []struct {
		s string
		w int
	}{
		{"abc", 3}, {"日本", 4}, {"é", 1}, {"a\u0301", 1}, {"★", 1},
	}
	for _, c := range cases {
		if got := stringWidth(c.s); got != c.w {
			t.Errorf("width(%q) = %d, want %d", c.s, got, c.w)
		}
	}
	if got := truncateWidth("abcdef", 3); got != "abc" {
		t.Errorf("truncate = %q", got)
	}
	if got := padWidth("ab", 5); got != "ab   " {
		t.Errorf("pad = %q", got)
	}
	if got := fitWidth("abc", 5); got != "abc  " {
		t.Errorf("fit short = %q", got)
	}
	if got := fitWidth("abcdef", 5); got != "abcd…" {
		t.Errorf("fit long = %q", got)
	}
}

func TestTextTransform(t *testing.T) {
	if got := applyTextTransform("hello world", "uppercase"); got != "HELLO WORLD" {
		t.Errorf("uppercase = %q", got)
	}
	if got := applyTextTransform("hello world", "capitalize"); got != "Hello World" {
		t.Errorf("capitalize = %q", got)
	}
	if got := expandTabs("a\tb"); got != "a   b" {
		t.Errorf("tabs = %q", got)
	}
}
