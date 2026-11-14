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

func TestReadKeyKittyProtocol(t *testing.T) {
	cases := []struct{ in, want string }{
		{"\x1b[27u", "esc"},
		{"\x1b[13u", "enter"},
		{"\x1b[9u", "tab"},
		{"\x1b[9;2u", "backtab"},
		{"\x1b[127;3u", "alt-backspace"},
		{"\x1b[127;5u", "ctrl-backspace"},
		{"\x1b[97;5u", "ctrl-a"},
		{"\x1b[98;3u", "alt-b"},
		{"\x1b[32;5u", "ctrl-space"},
		{"\x1b[32u", " "},
		{"\x1b[97;1;97u", "a"},
		{"\x1b[97;2;65u", "A"},
		{"\x1b[49;2;33u", "!"},
		{"\x1b[113;7;64u", "@"},
		{"\x1b[65u", "A"},
		{"\x1b[97;1:2u", "a"},
		{"\x1b[97;1:3u", ""},
		{"\x1b[57441;1u", ""},
		{"\x1b[?19u", ""},
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

func TestKittyKeyboardDetection(t *testing.T) {
	clear := func() {
		for _, k := range []string{
			"TERM", "TERM_PROGRAM", "KITTY_WINDOW_ID", "WEZTERM_PANE",
			"GHOSTTY_RESOURCES_DIR", "TMUX", "ZELLIJ", "STY",
		} {
			t.Setenv(k, "")
		}
	}
	cases := []struct {
		name    string
		term    string
		program string
		extra   string
		want    bool
	}{
		{"kitty term", "xterm-kitty", "", "", true},
		{"kitty id", "", "", "KITTY_WINDOW_ID=1", true},
		{"wezterm", "", "WezTerm", "", true},
		{"ghostty", "", "ghostty", "", true},
		{"foot", "foot-extra", "", "", true},
		{"plain xterm", "xterm-256color", "Apple_Terminal", "", false},
		{"tmux", "screen-256color", "", "TMUX=/tmp/tmux", false},
		{"screen", "screen.xterm-kitty", "", "STY=1", false},
	}
	for _, tc := range cases {
		clear()
		if tc.term != "" {
			t.Setenv("TERM", tc.term)
		}
		if tc.program != "" {
			t.Setenv("TERM_PROGRAM", tc.program)
		}
		if tc.extra != "" {
			k, v, _ := strings.Cut(tc.extra, "=")
			t.Setenv(k, v)
		}
		if got := kittyKeyboardEnabled(); got != tc.want {
			t.Errorf("%s: enabled = %v, want %v", tc.name, got, tc.want)
		}
	}
	clear()
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
