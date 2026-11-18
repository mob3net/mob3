package browser

import (
	"sort"
)

func (l *layouter) flexRow(n *Node, x, y, availW int) int {
	if l.outOfTime() {
		return 0
	}
	var items []*Node
	for _, c := range layoutChildren(n) {
		if c.Type == ElementNode && c.Style != nil && c.Style.Display != "none" {
			if isOutOfFlow(c.Style) {
				l.deferAbsolute(c, x, y)
				continue
			}
			items = append(items, c)
		}
	}
	if len(items) == 0 {
		// Text-only flex items: regular inline flow already places them in a
		// row and keeps the text visible.
		return l.flow(n, x, y, availW)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Style.Order < items[j].Style.Order })
	gap := 0
	if n.Style != nil {
		gap = nonNeg(n.Style.Gap.resolveH(availW, n.Style.FontSize))
	}
	if n.Style != nil && n.Style.FlexWrap == "wrap" {
		var lines [][]*Node
		var line []*Node
		used := 0
		for _, it := range items {
			w := l.flexItemBasis(it, availW)
			if len(line) > 0 && used+gap+w > availW {
				lines = append(lines, line)
				line = nil
				used = 0
			}
			if len(line) > 0 {
				used += gap
			}
			line = append(line, it)
			used += w
		}
		if len(line) > 0 {
			lines = append(lines, line)
		}
		totalH := 0
		for _, ln := range lines {
			totalH += l.layFlexLine(ln, x, y+totalH, availW, gap, n.Style)
		}
		return totalH
	}
	return l.layFlexLine(items, x, y, availW, gap, n.Style)
}

// flexItemBasis returns the base width used to decide where wrapped flex rows
// break.
func (l *layouter) flexItemBasis(it *Node, availW int) int {
	st := it.Style
	if st == nil {
		return 1
	}
	ml, mr, _, _, bl, br, _, _, pl, pr, _, _ := l.boxMetrics(st, availW)
	extra := ml + mr + bl + br + pl + pr
	base := 0
	switch {
	case st.FlexBasisSet:
		base = l.widthValue(st, st.FlexBasis, availW)
	case st.Width.Set && !st.Width.IsAuto():
		base = l.widthValue(st, st.Width, availW)
	case st.FlexGrow > 0:
		base = 0
	default:
		base, _ = l.measure(it)
	}
	if base < 0 {
		base = 0
	}
	return maxInt(1, base+extra)
}

// layFlexLine sizes and lays out one flex row.
func (l *layouter) layFlexLine(items []*Node, x, y, availW, gap int, base *Style) int {
	if len(items) == 0 {
		return 0
	}
	totalAvail := availW - gap*(len(items)-1)
	if totalAvail < len(items) {
		totalAvail = len(items)
	}
	widths := make([]int, len(items))
	grow := make([]float64, len(items))
	fixed := 0
	growTotal := 0.0
	for i, it := range items {
		st := it.Style
		ml, mr, _, _, bl, br, _, _, pl, pr, _, _ := l.boxMetrics(st, availW)
		extra := ml + mr + bl + br + pl + pr
		switch {
		case st.FlexBasisSet:
			basis := l.widthValue(st, st.FlexBasis, availW)
			if basis < 0 {
				basis = 0
			}
			widths[i] = basis + extra
			if widths[i] < 1 {
				widths[i] = 1
			}
			fixed += widths[i]
			if st.FlexGrow > 0 {
				grow[i] = st.FlexGrow
				growTotal += st.FlexGrow
			}
		case st.Width.Set && !st.Width.IsAuto():
			w := l.widthValue(st, st.Width, availW) + extra
			if w < 1 {
				w = 1
			}
			widths[i] = w
			fixed += w
			if st.FlexGrow > 0 {
				grow[i] = st.FlexGrow
				growTotal += st.FlexGrow
			}
		case st.FlexGrow > 0:
			grow[i] = st.FlexGrow
			growTotal += st.FlexGrow
		default:
			mw, _ := l.measure(it)
			w := mw + extra
			if w < 1 {
				w = 1
			}
			widths[i] = w
			fixed += w
		}
	}
	if remaining := totalAvail - fixed; remaining > 0 && growTotal > 0 {
		for i := range items {
			if grow[i] > 0 {
				widths[i] += int(float64(remaining) * grow[i] / growTotal)
			}
		}
	}
	mins := make([]int, len(items))
	for i, it := range items {
		ml, mr, _, _, bl, br, _, _, pl, pr, _, _ := l.boxMetrics(it.Style, availW)
		_, mi := l.measure(it)
		mins[i] = maxInt(1, mi+ml+mr+bl+br+pl+pr)
		if widths[i] < mins[i] {
			widths[i] = mins[i]
		}
	}
	total := 0
	for i := range widths {
		if widths[i] < 1 {
			widths[i] = 1
		}
		total += widths[i]
	}
	if total > totalAvail && total > 0 {
		for i := range widths {
			widths[i] = maxInt(mins[i], widths[i]*totalAvail/total)
		}
	}
	used := gap * (len(items) - 1)
	for _, w := range widths {
		used += w
	}
	free := availW - used
	if free < 0 {
		free = 0
	}
	// Auto margins absorb the free space before justify-content does.
	autoL := make([]int, len(items))
	autoR := make([]int, len(items))
	autoCount := 0
	for _, it := range items {
		if it.Style == nil {
			continue
		}
		if it.Style.Margin[3].Auto {
			autoCount++
		}
		if it.Style.Margin[1].Auto {
			autoCount++
		}
	}
	if free > 0 && autoCount > 0 {
		per := free / autoCount
		for i, it := range items {
			if it.Style == nil {
				continue
			}
			if it.Style.Margin[3].Auto {
				autoL[i] = per
			}
			if it.Style.Margin[1].Auto {
				autoR[i] = per
			}
		}
		free = 0
	}
	startX := x
	extraGap := 0
	justify := ""
	if base != nil {
		justify = base.JustifyContent
	}
	n := len(items)
	switch justify {
	case "end":
		startX += free
	case "center":
		startX += free / 2
	case "between":
		if n > 1 {
			extraGap = free / (n - 1)
		}
	case "around":
		unit := free / maxInt(1, n)
		startX += unit / 2
		extraGap = unit
	case "evenly":
		unit := free / (n + 1)
		startX += unit
		extraGap = unit
	}

	starts := make([]int, n)
	heights := make([]int, n)
	maxH := 0
	xx := startX
	for i, it := range items {
		xx += autoL[i]
		basis := maxInt(1, widths[i])
		starts[i] = len(l.frags)
		var h int
		if l.isBlock(it) {
			// The flex line already resolved this item's width, including
			// percentages and grow/shrink; do not apply Width again inside
			// block layout or the box drifts from its assigned slot.
			h, _ = l.blockLayout(it, xx, y, basis, 0, true)
		} else {
			h = l.inlineFlow([]*Node{it}, xx, y, basis, base)
		}
		heights[i] = h
		if h > maxH {
			maxH = h
		}
		xx += widths[i] + gap + extraGap + autoR[i]
	}
	align := ""
	if base != nil {
		align = base.AlignItems
	}
	if align == "center" || align == "end" {
		for i, start := range starts {
			end := len(l.frags)
			if i+1 < len(starts) {
				end = starts[i+1]
			}
			dy := 0
			if align == "center" {
				dy = (maxH - heights[i]) / 2
			} else {
				dy = maxH - heights[i]
			}
			if dy > 0 {
				for j := start; j < end; j++ {
					l.frags[j].Y += dy
				}
			}
		}
	}
	return maxH
}
