package engine

import (
	"strings"
	"unicode"
)

// VisibleText concatenates rendered text below n, skipping hidden elements.
// Form controls (notably buttons) use this instead of TextContent so CSS or
// script text cannot leak into their labels.
func VisibleText(n *Node) string {
	var b strings.Builder
	var walk func(*Node)
	walk = func(x *Node) {
		for _, c := range x.Children {
			if c.Type == TextNode {
				b.WriteString(c.Text)
				continue
			}
			if c.Type != ElementNode {
				continue
			}
			if c.Style != nil && c.Style.Display == "none" {
				continue
			}
			switch c.Tag {
			case "script", "style", "noscript", "template", "head", "title", "meta", "link":
				continue
			}
			walk(c)
		}
	}
	if n != nil {
		walk(n)
	}
	return b.String()
}

// fallbackPageBackground finds a full-page wrapper background when html/body
// do not provide an opaque color. A fill must cover at least 60% of the first
// viewport (or the whole short document) to count.
func FallbackPageBackground(frags []Frag, width, viewportH, docH int) (Color, bool) {
	if width < 1 {
		return Color{}, false
	}
	h := docH
	if viewportH > 0 && viewportH < h {
		h = viewportH
	}
	if h < 1 {
		h = 1
	}
	want := width * h * 60 / 100
	best := Color{}
	bestArea := 0
	for _, f := range frags {
		if f.Kind != FragFill || !Opaque(f.Color) {
			continue
		}
		x0, x1 := MaxInt(f.X, 0), MinInt(f.X+f.W, width)
		y0, y1 := MaxInt(f.Y, 0), MinInt(f.Y+f.H, h)
		if x1 <= x0 || y1 <= y0 {
			continue
		}
		if area := (x1 - x0) * (y1 - y0); area > bestArea {
			bestArea = area
			best = f.Color
		}
	}
	if bestArea < want {
		return Color{}, false
	}
	return best, true
}

func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func MinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

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

func StringWidth(s string) int {
	w := 0
	for _, r := range s {
		w += RuneWidth(r)
	}
	return w
}

// TruncateWidth cuts s to at most max cells.
func TruncateWidth(s string, max int) string {
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

// PadWidth pads s with spaces to exactly w cells.
func PadWidth(s string, w int) string {
	sw := StringWidth(s)
	if sw >= w {
		return TruncateWidth(s, w)
	}
	return s + strings.Repeat(" ", w-sw)
}

// FitWidth truncates s with an ellipsis and pads it to exactly w cells.
func FitWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if StringWidth(s) <= w {
		return PadWidth(s, w)
	}
	return PadWidth(TruncateWidth(s, w-1)+"…", w)
}

func ApplyTextTransform(s, mode string) string {
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

// ExpandTabs converts tabs to spaces using a 4-cell tab width.
func ExpandTabs(s string) string {
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
			col += RuneWidth(r)
		}
	}
	return b.String()
}
