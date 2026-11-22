package engine

import (
	"math"
	"strconv"
)

// table lays out a display:table subtree with collapsed one-cell grid borders.
// growColumns adds extra cells to columns in proportion to their remaining
// max-content slack, so a wide data column grows more than a short label one.
func growColumns(cols, caps []int, extra int) {
	if extra <= 0 || len(cols) == 0 {
		return
	}
	weights := make([]int, len(cols))
	sum := 0
	for i := range cols {
		slack := caps[i] - cols[i]
		if slack < 0 {
			slack = 0
		}
		weights[i] = slack
		sum += slack
	}
	assigned := 0
	for i, w := range weights {
		add := extra / len(cols)
		if sum > 0 {
			add = extra * w / sum
		}
		if i == len(weights)-1 {
			add = extra - assigned
		}
		cols[i] += add
		assigned += add
	}
}

// distributeSpan raises the columns of a spanning cell so their total reaches
// target, splitting the deficit in proportion to their current widths.
func distributeSpan(cols []int, col, span, target int) {
	if span < 1 || col < 0 || col >= len(cols) {
		return
	}
	end := col + span
	if end > len(cols) {
		end = len(cols)
	}
	sum := 0
	for i := col; i < end; i++ {
		sum += cols[i]
	}
	if sum >= target {
		return
	}
	deficit := target - sum
	weights := append([]int(nil), cols[col:end]...)
	assigned := 0
	for i, w := range weights {
		add := 0
		if sum > 0 {
			add = deficit * w / sum
		} else {
			add = deficit / len(weights)
		}
		if i == len(weights)-1 {
			add = deficit - assigned
		}
		cols[col+i] += add
		assigned += add
	}
}

func (l *Layouter) table(n *Node, x, y, availW int) int {
	if l.outOfTime() {
		return 0
	}
	for _, c := range layoutChildren(n) {
		if c.Type == ElementNode && c.Style != nil && c.Style.Float != "" {
			return l.flow(n, x, y, availW)
		}
	}
	if l.truncated || l.overRowLimit(y) {
		l.truncated = true
		return 0
	}
	st := n.Style
	if st == nil {
		return 0
	}
	startY := y
	if cap := childByTag(n, "caption"); cap != nil {
		y += l.block(cap, x, y, availW)
	}
	type cell struct {
		node *Node
		col  int
		span int
	}
	type row struct {
		cells []cell
	}
	type placedCell struct {
		node       *Node
		start, end int
		h          int
	}
	var rows []row
	var collect func(*Node)
	collect = func(parent *Node) {
		for _, c := range parent.Children {
			if c.Type != ElementNode || c.Style == nil || c.Style.Display == "none" {
				continue
			}
			if isOutOfFlow(c.Style) {
				l.deferAbsolute(c, x, y)
				continue
			}
			if c.Tag == "tr" {
				r := row{}
				col := 0
				for _, cc := range c.Children {
					if cc.Type != ElementNode || cc.Style == nil || cc.Style.Display == "none" {
						continue
					}
					if cc.Tag == "td" || cc.Tag == "th" || cc.Style.Display == "table-cell" {
						span := 1
						if v := cc.Attr("colspan"); v != "" {
							if num, err := strconv.Atoi(v); err == nil && num > 0 {
								span = num
								if span > 20 {
									span = 20
								}
							}
						}
						r.cells = append(r.cells, cell{node: cc, col: col, span: span})
						col += span
					}
				}
				rows = append(rows, r)
				continue
			}
			if c.Tag == "table" {
				continue
			}
			collect(c)
		}
	}
	collect(n)
	ncols := 0
	for _, r := range rows {
		for _, c := range r.cells {
			if c.col+c.span > ncols {
				ncols = c.col + c.span
			}
		}
	}
	if ncols == 0 || len(rows) == 0 {
		// Fall back to normal block flow for empty tables.
		return l.flow(n, x, y, availW)
	}
	colMin := make([]int, ncols)
	colW := make([]int, ncols)
	colPct := make([]float64, ncols)
	hasPct := false
	type spanCell struct {
		col, span int
		pref, min int
	}
	var spans []spanCell
	for _, r := range rows {
		for _, c := range r.cells {
			if c.col >= ncols {
				continue
			}
			pref, min := l.measure(c.node)
			if c.span == 1 {
				if pref > colW[c.col] {
					colW[c.col] = pref
				}
				if min > colMin[c.col] {
					colMin[c.col] = min
				}
			} else {
				spans = append(spans, spanCell{col: c.col, span: c.span, pref: pref, min: min})
			}
			// Legacy table cells carry width hints. They size the column, not
			// the cell's content box, so percentage/fixed widths must be folded
			// into the column targets below.
			if st := c.node.Style; c.span == 1 && st != nil && st.Width.Set && !st.Width.IsAuto() {
				if st.Width.U == UnitPercent {
					hasPct = true
					for k := 0; k < c.span && c.col+k < ncols; k++ {
						if st.Width.V > colPct[c.col+k] {
							colPct[c.col+k] = st.Width.V
						}
					}
				} else {
					w := st.Width.ResolveH(availW, st.FontSize)
					per := (w + c.span - 1) / c.span
					for k := 0; k < c.span && c.col+k < ncols; k++ {
						if per > colW[c.col+k] {
							colW[c.col+k] = per
						}
						if per > colMin[c.col+k] {
							colMin[c.col+k] = per
						}
					}
				}
			}
		}
	}
	// A spanning cell constrains the sum of its columns, not each column
	// individually. Distribute any deficit in proportion to the existing
	// non-spanning widths so one wide colspan row cannot force every column to
	// an equal, oversized share.
	for _, sc := range spans {
		distributeSpan(colMin, sc.col, sc.span, sc.min)
		distributeSpan(colW, sc.col, sc.span, sc.pref)
	}
	for i := range colW {
		if colMin[i] < 1 {
			colMin[i] = 1
		}
		if colW[i] < colMin[i] {
			colW[i] = colMin[i]
		}
		if colW[i] < 3 {
			colW[i] = 3
		}
	}
	// A table width declaration sets the used table width. Percentages resolve
	// against the containing block; fixed lengths are in CSS pixels. Without
	// one, auto tables keep their shrink-to-fit width.
	tableTarget := availW
	hasWidth := false
	if st.Width.Set && !st.Width.IsAuto() {
		w := 0
		if st.Width.U == UnitPercent {
			w = int(math.Round(float64(availW) * st.Width.V / 100))
		} else {
			w = st.Width.ResolveH(availW, st.FontSize)
		}
		if w > 0 {
			if w > availW {
				w = availW
			}
			tableTarget = w
			hasWidth = true
		}
	}
	// Percentage columns resolve against the table width, so a table using
	// them fills the available width and lets the auto columns take the rest.
	if hasPct {
		caps := append([]int(nil), colW...)
		inner := tableTarget - (ncols + 1)
		if inner < 1 {
			inner = 1
		}
		// Auto columns keep their min-content width first; percentage columns
		// then share what remains, normalised when their declared percentages
		// add up to more than 100%.
		autoSum, pctSum := 0, 0
		for i := 0; i < ncols; i++ {
			if colPct[i] > 0 {
				pctSum += int(math.Round(colPct[i]))
			} else {
				autoSum += colMin[i]
			}
		}
		remaining := inner - autoSum
		if remaining < 1 {
			remaining = 1
		}
		for i := 0; i < ncols; i++ {
			if colPct[i] > 0 {
				w := 1
				if pctSum > 0 {
					w = int(math.Round(float64(remaining) * colPct[i] / float64(pctSum)))
				}
				if w < colMin[i] {
					w = colMin[i]
				}
				if w < 1 {
					w = 1
				}
				colW[i] = w
			} else {
				colW[i] = colMin[i]
				if colW[i] < 1 {
					colW[i] = 1
				}
			}
		}
		// Absorb integer rounding so the table uses exactly the target width.
		assigned := 0
		for _, w := range colW {
			assigned += w
		}
		for assigned > inner {
			widest := 0
			for i := range colW {
				if colW[i] > colW[widest] {
					widest = i
				}
			}
			if colW[widest] <= 1 {
				break
			}
			colW[widest]--
			assigned--
		}
		if assigned < inner {
			growColumns(colW, caps, inner-assigned)
		}
		// The percentage layout now becomes the minimum, so the shrink pass
		// below cannot replace it with unnormalised min-content widths.
		copy(colMin, colW)
	}
	total := ncols + 1
	for _, w := range colW {
		total += w
	}
	if total > tableTarget {
		avail := tableTarget - (ncols + 1)
		if avail < 1 {
			avail = 1
		}
		sum := 0
		minSum := 0
		for i := range colW {
			sum += colW[i]
			minSum += colMin[i]
		}
		switch {
		case minSum >= avail:
			// Even the min-content widths do not fit (common in nested navbox
			// tables). Scale columns down proportionally instead of letting the
			// table overflow the page.
			assigned := 0
			for i := range colW {
				w := 0
				if minSum > 0 {
					w = avail * colMin[i] / minSum
				}
				if w < 1 {
					w = 1
				}
				colW[i] = w
				assigned += w
			}
			for assigned > avail {
				widest := 0
				for i := range colW {
					if colW[i] > colW[widest] {
						widest = i
					}
				}
				if colW[widest] <= 1 {
					break
				}
				colW[widest]--
				assigned--
			}
			for i := 0; assigned < avail; i = (i + 1) % len(colW) {
				colW[i]++
				assigned++
			}
		case sum > avail:
			budget := avail - minSum
			slackSum := sum - minSum
			caps := append([]int(nil), colW...)
			assigned := 0
			for i := range colW {
				slack := caps[i] - colMin[i]
				add := 0
				if slackSum > 0 {
					add = budget * slack / slackSum
				}
				if add > slack {
					add = slack
				}
				colW[i] = colMin[i] + add
				assigned += colW[i]
			}
			for leftover := avail - assigned; leftover > 0; leftover-- {
				grew := false
				for i := range colW {
					if colW[i] < caps[i] {
						colW[i]++
						grew = true
						break
					}
				}
				if !grew {
					break
				}
			}
		}
		total = ncols + 1
		for _, w := range colW {
			total += w
		}
	}
	// A declared width (notably width:100%) also expands a narrower table.
	if hasWidth || hasPct {
		avail := tableTarget - (ncols + 1)
		if avail < 1 {
			avail = 1
		}
		sum := 0
		for _, w := range colW {
			sum += w
		}
		if sum < avail {
			extra := avail - sum
			weights := append([]int(nil), colW...)
			assigned := 0
			for i, w := range weights {
				add := 0
				if sum > 0 {
					add = extra * w / sum
				} else {
					add = extra / len(weights)
				}
				if i == len(weights)-1 {
					add = extra - assigned
				}
				colW[i] += add
				assigned += add
			}
		}
	}
	tableW := ncols + 1
	for _, w := range colW {
		tableW += w
	}
	// Legacy <center> maps to an inherited text-align; a narrow table is
	// centered (or right-aligned) inside its containing block.
	if tableW < availW {
		switch st.TextAlign {
		case "center":
			x += (availW - tableW) / 2
		case "right":
			x += availW - tableW
		}
	}
	colX := make([]int, ncols)
	cx := x + 1
	for i := 0; i < ncols; i++ {
		colX[i] = cx
		cx += colW[i] + 1
	}
	bc := st.BorderColor
	if !bc.Set {
		bc = RGB(0xd0, 0xd7, 0xde)
	}
	curY := y
	for _, r := range rows {
		if len(r.cells) == 0 {
			continue
		}
		topY := curY
		rowY := topY + 1
		rowH := 1
		var placed []placedCell
		for _, c := range r.cells {
			cw := c.span - 1
			for k := 0; k < c.span && c.col+k < ncols; k++ {
				cw += colW[c.col+k]
			}
			if c.col >= ncols {
				continue
			}
			orig := c.node.Style
			cs := *orig
			cs.Border = [4]Length{}
			// The column width already carries any cell width hint; applying
			// it again to the content box would shrink the content to a
			// fraction of its own column.
			cs.Width = Length{}
			// Table content must not paint outside its cell; nowrap link lists
			// and oversized inline content would otherwise spill across the
			// grid lines.
			cs.Overflow = "hidden"
			c.node.Style = &cs
			start := len(l.frags)
			h := l.block(c.node, colX[c.col], rowY, cw)
			// A cell's own background fill belongs to the cell box, not to
			// its vertically aligned content. Cell borders are suppressed
			// above because the table draws the collapsed grid.
			contentStart := start
			if start < len(l.frags) && Opaque(orig.Background) {
				if f := &l.frags[start]; f.Kind == FragFill && f.Color == orig.Background {
					contentStart++
				}
			}
			placed = append(placed, placedCell{node: c.node, start: contentStart, end: len(l.frags), h: h})
			c.node.Style = orig
			if h > rowH {
				rowH = h
			}
		}
		// Vertical alignment is only known once every cell in the row has
		// been laid out and the row height is final.
		for _, pc := range placed {
			dy := tableCellOffset(pc.node.Style.VerticalAlign, rowH-pc.h)
			if dy == 0 {
				continue
			}
			for i := pc.start; i < pc.end; i++ {
				if !l.frags[i].Fixed {
					l.frags[i].Y += dy
				}
			}
			if l.hasCursor && l.cursor.Y >= rowY && l.cursor.Y < rowY+pc.h {
				l.cursor.Y += dy
			}
		}
		l.addFill(x, topY, tableW, 1, bc)
		l.addFill(x, rowY+rowH, tableW, 1, bc)
		l.addFill(x, topY, 1, rowH+2, bc)
		for i := 0; i < ncols; i++ {
			internal := false
			for _, c := range r.cells {
				if i >= c.col && i < c.col+c.span-1 {
					internal = true
					break
				}
			}
			if internal {
				continue
			}
			l.addFill(colX[i]+colW[i], topY, 1, rowH+2, bc)
		}
		curY = rowY + rowH
	}
	tableH := curY + 1 - y
	return (y - startY) + tableH
}

// tableCellOffset returns the vertical shift for a table cell whose content is
// shorter than its row. Browsers center td/th content by default, so an empty
// vertical-align behaves like middle.
func tableCellOffset(align string, extra int) int {
	if extra <= 0 {
		return 0
	}
	switch align {
	case "bottom", "text-bottom":
		return extra
	case "top", "baseline", "text-top", "sub", "super":
		return 0
	default:
		return extra / 2
	}
}

func childByTag(n *Node, tag string) *Node {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.Type == ElementNode && c.Tag == tag {
			return c
		}
	}
	return nil
}

func (l *Layouter) paddingH(n *Node) (int, int) {
	if n == nil || n.Style == nil {
		return 0, 0
	}
	st := n.Style
	pl := nonNeg(st.Padding[3].ResolveH(l.Width, st.FontSize))
	pr := nonNeg(st.Padding[1].ResolveH(l.Width, st.FontSize))
	bl := usedBorderH(st, 3, l.Width)
	br := usedBorderH(st, 1, l.Width)
	return pl + pr + bl + br, 0
}
