//go:build linux || darwin

package browser

import (
	"bufio"
	"strings"
	"testing"
)

func TestReadKeyPlain(t *testing.T) {
	cases := []struct{ in, want string }{
		{"j", "j"},
		{"\x03", "ctrl-c"},
		{"\t", "tab"},
		{"\r", "enter"},
		{"\n", "enter"},
		{"\x7f", "backspace"},
		{"\x08", "backspace"},
		{"\x01", "ctrl-a"},
		{"\x00", "ctrl-space"},
		{"é", "é"},
		{"世", "世"},
	}
	for _, tc := range cases {
		r := bufio.NewReader(strings.NewReader(tc.in))
		got, err := readKey(r)
		if err != nil {
			t.Fatalf("readKey(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("readKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestReadKeyEscapeSequences(t *testing.T) {
	cases := []struct{ in, want string }{
		{"\x1b", "esc"},
		{"\x1b[A", "up"},
		{"\x1b[B", "down"},
		{"\x1b[C", "right"},
		{"\x1b[D", "left"},
		{"\x1b[H", "home"},
		{"\x1b[F", "end"},
		{"\x1b[Z", "backtab"},
		{"\x1b[1~", "home"},
		{"\x1b[2~", "insert"},
		{"\x1b[3~", "delete"},
		{"\x1b[4~", "end"},
		{"\x1b[5~", "pgup"},
		{"\x1b[6~", "pgdn"},
		{"\x1b[7~", "home"},
		{"\x1b[8~", "end"},
		{"\x1bOH", "home"},
		{"\x1b[1;5D", "ctrl-left"},
		{"\x1b[1;3C", "alt-right"},
		{"\x1b[1;2A", "shift-up"},
		{"\x1b[3;5~", "ctrl-delete"},
		{"\x1b[3;3~", "alt-delete"},
	}
	for _, tc := range cases {
		r := bufio.NewReader(strings.NewReader(tc.in))
		got, err := readKey(r)
		if err != nil {
			t.Fatalf("readKey(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("readKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestReadKeyAltKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"\x1bx", "alt-x"},
		{"\x1b\x7f", "alt-backspace"},
		{"\x1bé", "alt-é"},
		{"\x1b ", "alt- "},
	}
	for _, tc := range cases {
		r := bufio.NewReader(strings.NewReader(tc.in))
		if got, _ := readKey(r); got != tc.want {
			t.Errorf("readKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestReadKeyDrainsTerminalReplies(t *testing.T) {
	cases := []string{
		"\x1b]11;rgb:0000/0000/0000\x07j",
		"\x1b_Gi=1;OK\x1b\\j",
		"\x1bP1$r0m\x1b\\j",
	}
	for _, in := range cases {
		r := bufio.NewReader(strings.NewReader(in))
		if got, err := readKey(r); err != nil || got != "" {
			t.Fatalf("reply from %q = %q, %v; want empty", in, got, err)
		}
		if got, err := readKey(r); err != nil || got != "j" {
			t.Errorf("key after reply from %q = %q, %v; want j", in, got, err)
		}
	}
}

func TestReadKeyInvalidUTF8(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("\xff"))
	got, err := readKey(r)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("invalid UTF-8 decoded as %q, want empty", got)
	}
}
