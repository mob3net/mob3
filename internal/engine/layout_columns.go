package engine

import (
	"math"
	"strings"
)

// hasColumns reports whether a block container has CSS multi-column layout.
func hasColumns(st *Style) bool {
	return st != nil && (st.ColumnCount > 0 || (st.ColumnWidth.Set && !st.ColumnWidth.IsAuto()))
}

// columnGap returns the column gap in cells, defaulting to the CSS `normal`
// value of one em.
func (l *Layouter) columnGap(st *Style, availW int) int {
	if st == nil {
		return 1
	}
	if st.Gap.Set {
		return nonNeg(st.Gap.ResolveH(availW, st.FontSize))
	}
	gap := int(math.Round(st.FontSize / cellW))
	if gap < 1 {
		gap = 1
	}
	return gap
}

// columnCount resolves `column-count` and `column-width` into a column count.
// A minimum column width of five cells keeps a narrow terminal readable.
func (l *Layouter) columnCount(st *Style, availW int) int {
	if st == nil {
		return 0
	}
	gap := l.columnGap(st, availW)
	count := st.ColumnCount
	if st.ColumnWidth.Set && !st.ColumnWidth.IsAuto() {
		if cw := st.ColumnWidth.ResolveH(availW, st.FontSize); cw > 0 {
			fit := (availW + gap) / (cw + gap)
			if fit < 1 {
				fit = 1
			}
			if count <= 0 || fit < count {
				count = fit
			}
		}
	}
	if count < 1 {
		return 0
	}
	for count > 1 {
		if (availW-gap*(count-1))/count >= 5 {
			break
		}
		count--
	}
	return count
}

// multiColumn lays block children into balanced CSS columns. It is a cheap
// approximation: children are distributed by count in column-major order, so
// a list of links fills each column top to bottom. Inline content, column
// spans, column rules and inline fragmentation fall back to normal flow.
func (l *Layouter) multiColumn(n *Node, x, y, availW int) int {
	if l.outOfTime() {
		return 0
	}
	st := n.Style
	cols := l.columnCount(st, availW)
	if cols < 2 {
		return l.flow(n, x, y, availW)
	}
	var items []*Node
	for _, c := range layoutChildren(n) {
		if c.Type == TextNode {
			if strings.TrimSpace(c.Text) != "" {
				return l.flow(n, x, y, availW)
			}
			continue
		}
		if c.Type != ElementNode || c.Style == nil || c.Style.Display == "none" {
			continue
		}
		if isOutOfFlow(c.Style) || c.Style.Float != "" || !l.isBlock(c) {
			return l.flow(n, x, y, availW)
		}
		items = append(items, c)
	}
	if len(items) == 0 {
		return l.flow(n, x, y, availW)
	}
	gap := l.columnGap(st, availW)
	totalGap := gap * (cols - 1)
	if totalGap >= availW {
		return l.flow(n, x, y, availW)
	}
	base := (availW - totalGap) / cols
	rem := (availW - totalGap) % cols
	per := len(items) / cols
	extra := len(items) % cols
	idx := 0
	cx := x
	maxH := 0
	for c := 0; c < cols; c++ {
		width := base
		if c < rem {
			width++
		}
		count := per
		if c < extra {
			count++
		}
		cur := y
		for k := 0; k < count; k++ {
			cur += l.block(items[idx], cx, cur, width)
			idx++
		}
		if cur-y > maxH {
			maxH = cur - y
		}
		cx += width + gap
	}
	return maxH
}
