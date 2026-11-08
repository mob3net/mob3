package browser

import (
	"strings"
	"unicode"
)

// runeWidth returns the number of terminal cells a rune occupies.
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

// truncateWidth cuts s to at most max cells.
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

// padWidth pads s with spaces to exactly w cells.
func padWidth(s string, w int) string {
	sw := stringWidth(s)
	if sw >= w {
		return truncateWidth(s, w)
	}
	return s + strings.Repeat(" ", w-sw)
}

// fitWidth truncates s with an ellipsis and pads it to exactly w cells.
func fitWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if stringWidth(s) <= w {
		return padWidth(s, w)
	}
	return padWidth(truncateWidth(s, w-1)+"…", w)
}

func applyTextTransform(s, mode string) string {
	switch mode {
	case "uppercase":
		return strings.ToUpper(s)
	case "lowercase":
		return strings.ToLower(s)
	case "capitalize":
		var b strings.Builder
		start := true
		for _, r := range s {
			if start && unicode.IsLetter(r) {
				r = unicode.ToUpper(r)
				start = false
			} else if unicode.IsSpace(r) {
				start = true
			}
			b.WriteRune(r)
		}
		return b.String()
	}
	return s
}

// expandTabs converts tabs to spaces using a 4-cell tab width.
func expandTabs(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		switch r {
		case '\t':
			n := 4 - col%4
			b.WriteString(strings.Repeat(" ", n))
			col += n
		case '\n':
			b.WriteRune(r)
			col = 0
		default:
			b.WriteRune(r)
			col += runeWidth(r)
		}
	}
	return b.String()
}
