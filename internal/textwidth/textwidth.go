// Package textwidth contains the terminal-cell text arithmetic shared by the
// engine and the front end: rune and string widths, truncation, padding, tail
// windows and wrapping. It is standard-library only so both sides of the
// engine/front-end boundary use the same wide-rune table.
package textwidth

import (
	"strings"
	"unicode"
)

// RuneWidth returns the number of terminal cells a rune occupies.
func RuneWidth(r rune) int {
	if r == 0 || r < 32 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	if IsWideRune(r) {
		return 2
	}
	return 1
}

// IsWideRune reports whether a rune occupies two terminal cells.
func IsWideRune(r rune) bool {
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

// StringWidth returns the number of terminal cells s occupies.
func StringWidth(s string) int {
	w := 0
	for _, r := range s {
		w += RuneWidth(r)
	}
	return w
}

// Truncate cuts s to at most max cells without splitting a wide rune.
func Truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	w := 0
	for i, r := range s {
		rw := RuneWidth(r)
		if w+rw > max {
			return s[:i]
		}
		w += rw
	}
	return s
}

// Pad pads s with spaces to exactly w cells, truncating when it is longer.
func Pad(s string, w int) string {
	sw := StringWidth(s)
	if sw >= w {
		return Truncate(s, w)
	}
	return s + strings.Repeat(" ", w-sw)
}

// Fit truncates s with an ellipsis and pads it to exactly w cells.
func Fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if StringWidth(s) <= w {
		return Pad(s, w)
	}
	return Pad(Truncate(s, w-1)+"…", w)
}

// Tail returns the rightmost max cells of s without splitting a wide rune.
func Tail(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	used := 0
	start := len(r)
	for start > 0 {
		rw := RuneWidth(r[start-1])
		if used+rw > max {
			break
		}
		used += rw
		start--
	}
	return string(r[start:])
}

// Wrap splits s into lines of at most width cells, keeping zero-width runes
// attached to the preceding cell. It always returns at least one line.
func Wrap(s string, width int) []string {
	var out []string
	var line strings.Builder
	col := 0
	for _, r := range s {
		rw := RuneWidth(r)
		if rw == 0 {
			line.WriteRune(r)
			continue
		}
		if col+rw > width {
			out = append(out, line.String())
			line.Reset()
			col = 0
		}
		line.WriteRune(r)
		col += rw
	}
	if line.Len() > 0 {
		out = append(out, line.String())
	}
	if len(out) == 0 {
		out = append(out, "")
	}
	return out
}

// WrapHanging is Wrap with indent spaces on continuation lines, so wrapped
// console entries line up under their first line.
func WrapHanging(s string, width, indent int) []string {
	if width <= 0 {
		return []string{s}
	}
	if indent < 0 || indent+2 > width {
		indent = 0
	}
	var out []string
	var line strings.Builder
	col := 0
	for _, r := range s {
		rw := RuneWidth(r)
		if rw == 0 {
			line.WriteRune(r)
			continue
		}
		if col+rw > width {
			out = append(out, line.String())
			line.Reset()
			for i := 0; i < indent; i++ {
				line.WriteByte(' ')
			}
			col = indent
		}
		line.WriteRune(r)
		col += rw
	}
	if line.Len() > 0 {
		out = append(out, line.String())
	}
	if len(out) == 0 {
		out = append(out, "")
	}
	return out
}
