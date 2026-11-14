//go:build linux || darwin

package browser

import (
	"bufio"
	"os"
	"strings"
	"unicode/utf8"
)

// readKeyEvents decodes keystrokes into symbolic names and streams them on ch.
func readKeyEvents(f *os.File, ch chan<- string) {
	r := bufio.NewReaderSize(f, 256)
	for {
		k, err := readKey(r)
		if err != nil {
			ch <- "eof"
			return
		}
		if k == "" {
			continue
		}
		ch <- k
	}
}

func readKey(r *bufio.Reader) (string, error) {
	b, err := r.ReadByte()
	if err != nil {
		return "", err
	}
	switch b {
	case 3:
		return "ctrl-c", nil
	case 9:
		return "tab", nil
	case 13, 10:
		return "enter", nil
	case 127, 8:
		return "backspace", nil
	case 27:
		return readEscape(r), nil
	case 0:
		return "ctrl-space", nil
	}
	if b < 32 {
		return "ctrl-" + string(rune('a'+b-1)), nil
	}
	if b < utf8.RuneSelf {
		return string(rune(b)), nil
	}
	// UTF-8 multibyte.
	buf := []byte{b}
	want := utf8Len(b)
	for len(buf) < want {
		nb, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		buf = append(buf, nb)
	}
	if !utf8.Valid(buf) {
		return "", nil
	}
	return string(buf), nil
}

func utf8Len(b byte) int {
	switch {
	case b&0xf8 == 0xf0:
		return 4
	case b&0xf0 == 0xe0:
		return 3
	case b&0xe0 == 0xc0:
		return 2
	}
	return 1
}

// readAltKey decodes ESC followed by a key as an alt-<key> token. Terminals
// send Alt as an ESC prefix; when ESC arrives alone Buffered is empty and the
// caller reports a plain escape instead.
func readAltKey(r *bufio.Reader, b byte) string {
	switch b {
	case 127, 8:
		return "alt-backspace"
	case 13, 10:
		return "alt-enter"
	case 9:
		return "alt-tab"
	}
	if b < 32 {
		// Alt+control has no binding; leave it for the normal key decoder.
		_ = r.UnreadByte()
		return "esc"
	}
	buf := []byte{b}
	want := utf8Len(b)
	for len(buf) < want {
		nb, err := r.ReadByte()
		if err != nil {
			break
		}
		buf = append(buf, nb)
	}
	if !utf8.Valid(buf) {
		return "esc"
	}
	return "alt-" + string(buf)
}

// modifierPrefix maps xterm/kitty modifier parameters (1 + shift|alt|ctrl|meta
// bit flags) to the key-name prefixes the rest of the browser understands.
func modifierPrefix(params string) string {
	parts := strings.Split(params, ";")
	if len(parts) < 2 {
		return ""
	}
	switch parts[len(parts)-1] {
	case "2":
		return "shift-"
	case "3":
		return "alt-"
	case "4":
		return "alt-shift-"
	case "5":
		return "ctrl-"
	case "6":
		return "ctrl-shift-"
	case "7":
		return "ctrl-alt-"
	case "8":
		return "ctrl-alt-shift-"
	}
	return ""
}

func readEscape(r *bufio.Reader) string {
	if r.Buffered() == 0 {
		return "esc"
	}
	b2, err := r.ReadByte()
	if err != nil {
		return "esc"
	}
	if b2 == '_' || b2 == '^' || b2 == 'P' || b2 == ']' {
		// Terminal reply (APC/DCS/OSC), for example a kitty graphics
		// acknowledgement. Drain it so it never reaches the key handler.
		drainTerminalReply(r)
		return ""
	}
	if b2 != '[' && b2 != 'O' {
		return readAltKey(r, b2)
	}
	var seq []byte
	for len(seq) < 32 {
		c, err := r.ReadByte()
		if err != nil {
			break
		}
		seq = append(seq, c)
		if c >= 0x40 && c <= 0x7e {
			break
		}
	}
	if len(seq) == 0 {
		return "esc"
	}
	final := seq[len(seq)-1]
	params := string(seq[:len(seq)-1])
	mod := modifierPrefix(params)
	switch final {
	case 'A':
		return mod + "up"
	case 'B':
		return mod + "down"
	case 'C':
		return mod + "right"
	case 'D':
		return mod + "left"
	case 'H':
		return mod + "home"
	case 'F':
		return mod + "end"
	case 'Z':
		return "backtab"
	case '~':
		base := params
		if i := strings.IndexByte(base, ';'); i >= 0 {
			base = base[:i]
		}
		switch base {
		case "1":
			return mod + "home"
		case "2":
			return mod + "insert"
		case "3":
			return mod + "delete"
		case "4":
			return mod + "end"
		case "5":
			return mod + "pgup"
		case "6":
			return mod + "pgdn"
		case "7":
			return mod + "home"
		case "8":
			return mod + "end"
		}
	case 'M', 'm':
		if strings.HasPrefix(params, "<") {
			if ev, ok := parseSGRMouse(params, final == 'm'); ok {
				return mouseToken(ev)
			}
			return ""
		}
		if final == 'M' && params == "" {
			// Legacy X10 encoding: three raw bytes follow the final M.
			var buf [3]byte
			for i := range buf {
				c, err := r.ReadByte()
				if err != nil {
					return ""
				}
				buf[i] = c
			}
			if ev, ok := parseX10Mouse(buf[0], buf[1], buf[2]); ok {
				return mouseToken(ev)
			}
		}
	}
	return ""
}

// drainTerminalReply consumes an already-started APC/DCS/OSC sequence up to
// BEL or ST (ESC \).
func drainTerminalReply(r *bufio.Reader) {
	for {
		c, err := r.ReadByte()
		if err != nil {
			return
		}
		if c == 7 {
			return
		}
		if c == 27 {
			if next, err := r.Peek(1); err == nil && next[0] == '\\' {
				_, _ = r.ReadByte()
				return
			}
		}
	}
}
