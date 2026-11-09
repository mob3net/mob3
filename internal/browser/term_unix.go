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
	want := 1
	switch {
	case b&0xe0 == 0xc0:
		want = 2
	case b&0xf0 == 0xe0:
		want = 3
	case b&0xf8 == 0xf0:
		want = 4
	}
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
		_ = r.UnreadByte()
		return "esc"
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
	switch final {
	case 'A':
		return "up"
	case 'B':
		return "down"
	case 'C':
		return "right"
	case 'D':
		return "left"
	case 'H':
		return "home"
	case 'F':
		return "end"
	case 'Z':
		return "backtab"
	case '~':
		switch params {
		case "1":
			return "home"
		case "2":
			return "insert"
		case "3":
			return "delete"
		case "4":
			return "end"
		case "5":
			return "pgup"
		case "6":
			return "pgdn"
		case "7":
			return "home"
		case "8":
			return "end"
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
