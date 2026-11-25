package engine

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"browser/internal/textwidth"
)

// CellPos is a cell in document coordinates: row 0 is the top of the page.
type CellPos struct{ X, Y int }

// Selection is the page's drag-selection range. The browser keeps the drag
// state and clipboard policy; the engine owns ordering, text extraction and
// painting.
type Selection struct {
	Anchor CellPos
	Cursor CellPos
}

// Order returns the endpoints in reading order.
func (s Selection) Order() (CellPos, CellPos) {
	a, c := s.Anchor, s.Cursor
	if a.Y > c.Y || (a.Y == c.Y && a.X > c.X) {
		a, c = c, a
	}
	return a, c
}

// Has reports whether the range covers at least one cell.
func (s Selection) Has() bool { return s.Anchor != s.Cursor }

// Clear resets the range.
func (s *Selection) Clear() { *s = Selection{} }

// SetCursor moves the range end.
func (s *Selection) SetCursor(p CellPos) { s.Cursor = p }

// Text reconstructs the text covered by the selection.
func (s Selection) Text(layout *Layout) string {
	if layout == nil || !s.Has() {
		return ""
	}
	a, c := s.Order()
	var sb strings.Builder
	lastY := -1
	for i := range layout.Frags {
		f := &layout.Frags[i]
		if f.Kind != FragText || f.Text == "" {
			continue
		}
		if f.Y < a.Y || f.Y > c.Y {
			continue
		}
		x0, x1 := f.X, f.X+f.W-1
		if f.Y == a.Y && a.X > x0 {
			x0 = a.X
		}
		if f.Y == c.Y && c.X < x1 {
			x1 = c.X
		}
		if x1 < x0 {
			continue
		}
		if lastY >= 0 && f.Y != lastY {
			sb.WriteByte('\n')
		}
		lastY = f.Y
		cx := f.X
		for _, r := range f.Text {
			rw := textwidth.RuneWidth(r)
			if rw == 0 {
				continue
			}
			if cx >= x0 && cx <= x1 {
				sb.WriteRune(r)
			}
			cx += rw
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// Summary describes the selection length for the footer.
func (s Selection) Summary(layout *Layout) string {
	n := utf8.RuneCountInString(s.Text(layout))
	if n == 0 {
		return "Selection"
	}
	return "Selected " + strconv.Itoa(n) + " chars (y copies)"
}

// Paint highlights selected cells in the content viewport. Only cells covered
// by rendered text fragments are tinted, so selecting across several lines does
// not paint empty margins or unrelated columns.
func (s Selection) Paint(g *Grid, layout *Layout, scroll, top, bottom int) {
	if g == nil || layout == nil || !s.Has() {
		return
	}
	a, c := s.Order()
	selBG := RGB(0x2f, 0x6f, 0xbf)
	selFG := RGB(0xf8, 0xfa, 0xfc)
	for i := range layout.Frags {
		f := &layout.Frags[i]
		if f.Kind != FragText || f.Text == "" || f.W < 1 {
			continue
		}
		y := f.Y - scroll + top
		if y < top || y >= bottom || y >= g.H {
			continue
		}
		if f.Y < a.Y || f.Y > c.Y {
			continue
		}
		x0, x1 := f.X, f.X+f.W-1
		if f.Y == a.Y && a.X > x0 {
			x0 = a.X
		}
		if f.Y == c.Y && c.X < x1 {
			x1 = c.X
		}
		if x0 < f.X {
			x0 = f.X
		}
		if x1 >= f.X+f.W {
			x1 = f.X + f.W - 1
		}
		if x0 < 0 {
			x0 = 0
		}
		if x1 >= g.W {
			x1 = g.W - 1
		}
		for x := x0; x <= x1; x++ {
			cell := &g.Cells[y*g.W+x]
			if cell.Img {
				continue
			}
			cell.BG = selBG
			if cell.R != ' ' && cell.R != 0 {
				cell.FG = selFG
			}
		}
	}
}
