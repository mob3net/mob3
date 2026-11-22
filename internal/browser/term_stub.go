//go:build !linux && !darwin

package browser

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"sync/atomic"
	"unicode/utf8"
)

type termState struct{}

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

func makeRaw(f *os.File) (*termState, error) {
	return nil, errors.New("interactive mode is only supported on Linux and macOS; use -dump")
}

func restoreTerm(f *os.File, st *termState) {}

func terminalSize(f *os.File) (w, h int, ok bool) {
	w, h = 80, 24
	if s := os.Getenv("COLUMNS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			w = n
		}
	}
	if s := os.Getenv("LINES"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			h = n
		}
	}
	return w, h, false
}

func terminalCellPixels(f *os.File) (w, h int, ok bool) {
	return 0, 0, false
}

func readKeyEvents(f *os.File, ch chan<- string, abort *atomic.Uint64) {
	r := bufio.NewReader(f)
	for {
		runes, _, err := r.ReadRune()
		if err != nil {
			ch <- "eof"
			return
		}
		k := ""
		if runes == 3 {
			k = "ctrl-c"
		} else if runes == 27 {
			k = "esc"
		} else if runes == '\n' || runes == '\r' {
			k = "enter"
		} else if runes == 127 {
			k = "backspace"
		} else if runes == 9 {
			k = "tab"
		} else if utf8.ValidRune(runes) {
			k = string(runes)
		}
		if k == "" {
			continue
		}
		if abort != nil && (k == "esc" || k == "ctrl-c") {
			abort.Add(1)
		}
		ch <- k
	}
}
