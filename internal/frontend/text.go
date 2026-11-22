package frontend

import (
	"strings"
	"unicode"
)

// These helpers mirror the engine's terminal width rules for the chrome layer.
// The front end cannot import the engine, and the chrome only needs the same
// cell widths that paint uses, so keep the two implementations in step.

func runeWidth(r rune) int {
	if r == 0 || r < 32 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	if isWideRune(r) {
		return 2
	}
	return 1
}

func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115f,
		r >= 0x2329 && r <= 0x232a,
		r >= 0x2e80 && r <= 0x303e,
		r >= 0x3041 && r <= 0x33ff,
		r >= 0x3400 && r <= 0x4dbf,
		r >= 0x4e00 && r <= 0x9fff,
		r >= 0xa000 && r <= 0xa4cf,
		r >= 0xa960 && r <= 0xa97f,
		r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff,
		r >= 0xfe10 && r <= 0xfe19,
		r >= 0xfe30 && r <= 0xfe6f,
		r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6,
		r >= 0x1f000 && r <= 0x1faff,
		r >= 0x20000 && r <= 0x3fffd:
		return true
	}
	return false
}

func stringWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

func truncateWidth(s string, max int) string {
	if max <= 0 {
		return ""
	}
	w := 0
	for i, r := range s {
		rw := runeWidth(r)
		if w+rw > max {
			return s[:i]
		}
		w += rw
	}
	return s
}

func padWidth(s string, w int) string {
	sw := stringWidth(s)
	if sw >= w {
		return truncateWidth(s, w)
	}
	return s + strings.Repeat(" ", w-sw)
}

func fitWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if stringWidth(s) <= w {
		return padWidth(s, w)
	}
	return padWidth(truncateWidth(s, w-1)+"…", w)
}

// tailWidth returns the rightmost max cells of s.
func tailWidth(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	used := 0
	start := len(r)
	for start > 0 {
		rw := runeWidth(r[start-1])
		if used+rw > max {
			break
		}
		used += rw
		start--
	}
	return string(r[start:])
}
