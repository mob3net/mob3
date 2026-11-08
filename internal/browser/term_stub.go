//go:build !linux

package browser

import (
	"bufio"
	"errors"
	"os"
	"strconv"
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
	return nil, errors.New("interactive mode is only supported on Linux; use -dump")
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

func readKeyEvents(f *os.File, ch chan<- string) {
	r := bufio.NewReader(f)
	for {
		runes, _, err := r.ReadRune()
		if err != nil {
			ch <- "eof"
			return
		}
		if runes == 3 {
			ch <- "ctrl-c"
		} else if runes == '\n' || runes == '\r' {
			ch <- "enter"
		} else if runes == 127 {
			ch <- "backspace"
		} else if runes == 9 {
			ch <- "tab"
		} else if utf8.ValidRune(runes) {
			ch <- string(runes)
		}
	}
}
