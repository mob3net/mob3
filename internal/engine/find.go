package engine

import (
	"unicode"

	"browser/internal/textwidth"
)

// FindRect is one rectangle covered by a find match.
type FindRect struct {
	X, Y, Line, W, H int
}

// FindMatch is one occurrence of a find query. Frag indexes Layout.Frags for a
// page match; HTML matches use Line instead.
type FindMatch struct {
	Frag  int
	X, Y  int
	Line  int
	W, H  int
	Rects []FindRect
}

// findUnit is one visible rune in the flattened find stream.
type findUnit struct {
	frag int
	line int
	x, y int
	w, h int
	real bool
}

func FindPageMatches(layout *Layout, text string, bigText bool) []FindMatch {
	if layout == nil {
		return nil
	}
	query := normalizeFindRunes(text)
	if len(query) == 0 {
		return nil
	}
	if !containsFindSpace(query) {
		return findPageSingle(layout, query, bigText)
	}
	runes, units := findPageStream(layout, bigText)
	return findMatchesInStream(runes, units, query, false)
}

func FindHTMLMatches(lines []string, text string) []FindMatch {
	query := normalizeFindRunes(text)
	if len(query) == 0 {
		return nil
	}
	if !containsFindSpace(query) {
		return computeHTMLSingle(lines, query)
	}
	runes, units := htmlFindStream(lines)
	return findMatchesInStream(runes, units, query, true)
}

// computePageSingle keeps the common one-word case allocation-free beyond the
// result slice; multi-word queries use the full visual text stream.
func findPageSingle(layout *Layout, query []rune, bigText bool) []FindMatch {
	var out []FindMatch
	for fi := range layout.Frags {
		f := &layout.Frags[fi]
		if f.Kind != FragText || f.Text == "" {
			continue
		}
		scale := 1
		if bigText && f.Style != nil {
			scale = bigTextScale(f.Style, layoutZoom)
		}
		runes := []rune(f.Text)
		for _, span := range findRuneSpans(runes, query) {
			x := f.X + cellsForRunes(runes[:span[0]], scale)
			w := cellsForRunes(runes[span[0]:span[1]], scale)
			if w < 1 {
				continue
			}
			out = append(out, FindMatch{Frag: fi, X: x, Y: f.Y, W: w, H: scale})
			if len(out) >= 5000 {
				return out
			}
		}
	}
	return out
}

func computeHTMLSingle(lines []string, query []rune) []FindMatch {
	var out []FindMatch
	for li, line := range lines {
		runes := []rune(line)
		for _, span := range findRuneSpans(runes, query) {
			x := cellsForRunes(runes[:span[0]], 1)
			w := cellsForRunes(runes[span[0]:span[1]], 1)
			if w < 1 {
				continue
			}
			out = append(out, FindMatch{Frag: -1, Line: li, X: x, W: w, H: 1})
			if len(out) >= 5000 {
				return out
			}
		}
	}
	return out
}

func containsFindSpace(runes []rune) bool {
	for _, r := range runes {
		if isFindSpace(r) {
			return true
		}
	}
	return false
}

// pageFindStream flattens visible text in visual order. Spaces and line breaks
// are normalised so a query such as "reader mode" can span word fragments and
// wrapped lines.
func findPageStream(layout *Layout, bigText bool) ([]rune, []findUnit) {
	var text []rune
	var units []findUnit
	prevY := -1
	prevRune := rune(0)
	for fi := range layout.Frags {
		f := &layout.Frags[fi]
		if f.Kind != FragText || f.Text == "" {
			continue
		}
		runes := []rune(f.Text)
		if prevY >= 0 && f.Y != prevY && !isFindSpace(prevRune) && !isFindSpace(runes[0]) {
			appendFindRune(&text, &units, ' ', findUnit{})
		}
		scale := 1
		if bigText && f.Style != nil {
			scale = bigTextScale(f.Style, layoutZoom)
		}
		x := f.X
		for _, r := range runes {
			w := textwidth.RuneWidth(r) * scale
			u := findUnit{frag: fi, line: -1, x: x, y: f.Y, w: w, h: scale, real: true}
			appendFindRune(&text, &units, r, u)
			x += w
		}
		prevY = f.Y
		prevRune = runes[len(runes)-1]
	}
	return text, units
}

// htmlFindStream flattens console HTML lines with the same whitespace rules as
// page finding, so a phrase can wrap across display lines.
func htmlFindStream(lines []string) ([]rune, []findUnit) {
	var text []rune
	var units []findUnit
	prevRune := rune(0)
	for li, line := range lines {
		runes := []rune(line)
		if len(runes) == 0 {
			continue
		}
		if len(text) > 0 && !isFindSpace(prevRune) && !isFindSpace(runes[0]) {
			appendFindRune(&text, &units, ' ', findUnit{})
		}
		x := 0
		for _, r := range runes {
			w := textwidth.RuneWidth(r)
			u := findUnit{frag: -1, line: li, x: x, w: w, h: 1, real: true}
			appendFindRune(&text, &units, r, u)
			x += w
		}
		prevRune = runes[len(runes)-1]
	}
	return text, units
}

func normalizeFindRunes(s string) []rune {
	var out []rune
	for _, r := range s {
		appendFindRune(&out, nil, r, findUnit{})
	}
	return out
}

// appendFindRune adds a rune and collapses runs of whitespace to one space.
// units may be nil when only normalising a query.
func appendFindRune(text *[]rune, units *[]findUnit, r rune, u findUnit) {
	if isFindSpace(r) {
		if len(*text) == 0 || isFindSpace((*text)[len(*text)-1]) {
			return
		}
		r = ' '
	}
	*text = append(*text, r)
	if units != nil {
		*units = append(*units, u)
	}
}

func isFindSpace(r rune) bool {
	return unicode.IsSpace(r) || r == ' '
}

// findMatchesInStream matches a normalised query and groups each occurrence
// into one or more highlight rectangles.
func findMatchesInStream(text []rune, units []findUnit, query []rune, html bool) []FindMatch {
	var out []FindMatch
	for _, span := range findRuneSpans(text, query) {
		var rects []FindRect
		var cur *FindRect
		for i := span[0]; i < span[1]; i++ {
			u := units[i]
			if !u.real {
				continue
			}
			if cur != nil && u.y == cur.Y && (html || u.x <= cur.X+cur.W) &&
				(!html || u.line == cur.Line) {
				if end := u.x + u.w; end > cur.X+cur.W {
					cur.W = end - cur.X
				}
				continue
			}
			if cur != nil {
				rects = append(rects, *cur)
			}
			cur = &FindRect{X: u.x, Y: u.y, Line: u.line, W: u.w, H: u.h}
		}
		if cur != nil {
			rects = append(rects, *cur)
		}
		if len(rects) == 0 {
			continue
		}
		first := rects[0]
		m := FindMatch{
			Frag: firstFrag(units[span[0]:span[1]], html),
			Line: first.Line, X: first.X, Y: first.Y, W: first.W, H: first.H,
			Rects: rects,
		}
		out = append(out, m)
		if len(out) >= 5000 {
			return out
		}
	}
	return out
}

func firstFrag(units []findUnit, html bool) int {
	if html {
		return -1
	}
	for _, u := range units {
		if u.real {
			return u.frag
		}
	}
	return -1
}

// findRuneSpans returns case-insensitive, possibly overlapping match spans.
func findRuneSpans(text, query []rune) [][2]int {
	if len(query) == 0 || len(query) > len(text) {
		return nil
	}
	var out [][2]int
	for i := 0; i+len(query) <= len(text); i++ {
		ok := true
		for j := range query {
			if unicode.ToLower(text[i+j]) != unicode.ToLower(query[j]) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, [2]int{i, i + len(query)})
		}
	}
	return out
}

func cellsForRunes(runes []rune, scale int) int {
	if scale < 1 {
		scale = 1
	}
	w := 0
	for _, r := range runes {
		w += textwidth.RuneWidth(r) * scale
	}
	return w
}
