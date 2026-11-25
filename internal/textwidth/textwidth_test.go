package textwidth

import (
	"reflect"
	"testing"
)

func TestRuneWidth(t *testing.T) {
	cases := []struct {
		r    rune
		want int
	}{
		{'a', 1},
		{0, 0},
		{0x1f, 0},
		{0x7f, 0},
		{0x0301, 0}, // combining acute
		{0x4e00, 2}, // CJK
		{0x1f600, 2},
	}
	for _, c := range cases {
		if got := RuneWidth(c.r); got != c.want {
			t.Errorf("RuneWidth(%U) = %d, want %d", c.r, got, c.want)
		}
	}
}

func TestStringWidth(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"a界", 3},
		{"a\u0301", 1},
		{"\x00a", 1},
	}
	for _, c := range cases {
		if got := StringWidth(c.s); got != c.want {
			t.Errorf("StringWidth(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		s    string
		max  int
		want string
	}{
		{"abc", 0, ""},
		{"abc", 2, "ab"},
		{"abc", 3, "abc"},
		{"a界", 2, "a"},
		{"a界", 3, "a界"},
	}
	for _, c := range cases {
		if got := Truncate(c.s, c.max); got != c.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", c.s, c.max, got, c.want)
		}
	}
}

func TestPadAndFit(t *testing.T) {
	if got := Pad("a", 3); got != "a  " {
		t.Errorf("Pad short = %q", got)
	}
	if got := Pad("abc", 2); got != "ab" {
		t.Errorf("Pad long = %q", got)
	}
	if got := Fit("abc", 5); got != "abc  " {
		t.Errorf("Fit short = %q", got)
	}
	got := Fit("abcdef", 5)
	if got != "abcd…" {
		t.Errorf("Fit long = %q", got)
	}
	if w := StringWidth(got); w != 5 {
		t.Errorf("Fit width = %d, want 5", w)
	}
}

func TestTail(t *testing.T) {
	cases := []struct {
		s    string
		max  int
		want string
	}{
		{"abcde", 0, ""},
		{"abcde", 3, "cde"},
		{"a界", 2, "界"},
		{"a界", 3, "a界"},
	}
	for _, c := range cases {
		if got := Tail(c.s, c.max); got != c.want {
			t.Errorf("Tail(%q, %d) = %q, want %q", c.s, c.max, got, c.want)
		}
	}
}

func TestWrap(t *testing.T) {
	cases := []struct {
		s     string
		width int
		want  []string
	}{
		{"", 10, []string{""}},
		{"abc", 2, []string{"ab", "c"}},
		{"a界", 2, []string{"a", "界"}},
		{"a\u0301b", 1, []string{"a\u0301", "b"}},
	}
	for _, c := range cases {
		if got := Wrap(c.s, c.width); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Wrap(%q, %d) = %#v, want %#v", c.s, c.width, got, c.want)
		}
	}
}

func TestWrapHanging(t *testing.T) {
	if got := WrapHanging("abcdef", 0, 2); !reflect.DeepEqual(got, []string{"abcdef"}) {
		t.Errorf("zero width = %#v", got)
	}
	if got := WrapHanging("abcdef", 4, 2); !reflect.DeepEqual(got, []string{"abcd", "  ef"}) {
		t.Errorf("indent = %#v", got)
	}
	if got := WrapHanging("abcdef", 4, 10); !reflect.DeepEqual(got, []string{"abcd", "ef"}) {
		t.Errorf("invalid indent = %#v", got)
	}
}
