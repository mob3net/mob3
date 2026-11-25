package engine

import (
	"strings"
	"unicode"

	"browser/internal/textwidth"
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
			col += textwidth.RuneWidth(r)
		}
	}
	return b.String()
}
