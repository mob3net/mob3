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
	maxTotals := make([]int, len(items))
	fixed := 0
	growTotal := 0.0
	for i, it := range items {
		st := it.Style
		ml, mr, _, _, bl, br, _, _, pl, pr, _, _ := l.boxMetrics(st, availW)
		extra := ml + mr + bl + br + pl + pr
		maxTotals[i] = l.flexMaxTotal(st, extra, availW)
		switch {
		case st.FlexBasisSet:
			basis := l.widthValue(st, st.FlexBasis, availW)
			if basis < 0 {
				basis = 0
			}
			widths[i] = l.clampFlexWidth(st, basis+extra, extra, availW)
			if widths[i] < 1 {
				widths[i] = 1
			}
			fixed += widths[i]
			if st.FlexGrow > 0 {
				grow[i] = st.FlexGrow
				growTotal += st.FlexGrow
			}
		case st.Width.Set && !st.Width.IsAuto():
			w := l.clampFlexWidth(st, l.widthValue(st, st.Width, availW)+extra, extra, availW)
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
			w := l.clampFlexWidth(st, mw+extra, extra, availW)
			if w < 1 {
				w = 1
			}
			widths[i] = w
			fixed += w
		}
	}
	if growTotal > 0 {
		distributeGrow(widths, grow, maxTotals, totalAvail-fixed)
	}
	mins := make([]int, len(items))
	for i, it := range items {
		st := it.Style
		ml, mr, _, _, bl, br, _, _, pl, pr, _, _ := l.boxMetrics(st, availW)
		extra := ml + mr + bl + br + pl + pr
		_, mi := l.measure(it)
		mins[i] = maxInt(1, mi+extra)
		// A flex item is still bounded by its min/max widths. Bootstrap-style
		// columns use max-width:75%, and a wide descendant (a carousel stage
		// with an inline pixel width) would otherwise inflate the item's
		// min-content and force the whole column far past the row.
		maxTotal, minTotal := 0, 0
		if st != nil {
			if st.MaxWidth.Set && !st.MaxWidth.IsAuto() {
				if w := l.widthValue(st, st.MaxWidth, availW); w >= 0 {
					maxTotal = w + extra
				}
			}
			if st.MinWidth.Set && !st.MinWidth.IsAuto() {
				if w := l.widthValue(st, st.MinWidth, availW); w >= 0 {
					minTotal = w + extra
				}
			}
		}
		if minTotal > 0 && widths[i] < minTotal {
			widths[i] = minTotal
		}
		if maxTotal > 0 {
			// min-width wins when the two conflict, as in CSS.
			if minTotal > maxTotal {
				maxTotal = minTotal
			}
			if widths[i] > maxTotal {
				widths[i] = maxTotal
			}
			if mins[i] > maxTotal {
				mins[i] = maxTotal
			}
		}
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
		// Shrink in proportion to each item's flex-shrink factor and its slack
		// above the min-content width. A `flex-shrink:0` item (carousel slides,
		// fixed toolbars) keeps its size and overflows instead of collapsing.
		deficit := total - totalAvail
		for pass := 0; pass < 8 && deficit > 0; pass++ {
			weight := 0.0
			for i, it := range items {
				if shrink := flexShrinkOf(it); shrink > 0 {
					if slack := widths[i] - mins[i]; slack > 0 {
						weight += float64(slack) * shrink
					}
				}
			}
			if weight == 0 {
				break
			}
			removed := 0
			for i, it := range items {
				if deficit <= 0 {
					break
				}
				shrink := flexShrinkOf(it)
				slack := widths[i] - mins[i]
				if shrink <= 0 || slack <= 0 {
					continue
				}
				take := int(float64(deficit) * float64(slack) * shrink / weight)
				if take < 1 {
					take = 1
				}
				if take > slack {
					take = slack
				}
				if take > deficit {
					take = deficit
				}
				widths[i] -= take
				deficit -= take
				removed += take
			}
			if removed == 0 {
				break
			}
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
		if l.isBlock(it) || (it.Style != nil && it.Style.Display == "inline") {
			// Flex items are blockified: an inline element becomes a block
			// container, so its width and block descendants apply. Keeping it
			// inline collapsed cards whose markup is an inline anchor around
			// block content.
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

// flexShrinkOf returns an item's flex-shrink factor. The computed style
// defaults to 1, so only an explicit `flex-shrink:0` (or `flex: … 0 …`)
// prevents an item from shrinking.
func flexShrinkOf(n *Node) float64 {
	if n == nil || n.Style == nil {
		return 1
	}
	return n.Style.FlexShrink
}

// clampFlexWidth applies an item's min-width/max-width to a border-box width.
// This must happen before free space is distributed: a `flex-basis:100%` item
// capped by `max-width` otherwise consumes the whole row and leaves the
// growing sibling nothing to fill.
func (l *layouter) clampFlexWidth(st *Style, w, extra, availW int) int {
	if st == nil {
		return w
	}
	minTotal, maxTotal := 0, 0
	if st.MinWidth.Set && !st.MinWidth.IsAuto() {
		if v := l.widthValue(st, st.MinWidth, availW); v >= 0 {
			minTotal = v + extra
		}
	}
	if st.MaxWidth.Set && !st.MaxWidth.IsAuto() {
		if v := l.widthValue(st, st.MaxWidth, availW); v >= 0 {
			maxTotal = v + extra
		}
	}
	if maxTotal > 0 {
		if minTotal > maxTotal {
			maxTotal = minTotal
		}
		if w > maxTotal {
			w = maxTotal
		}
	}
	if minTotal > 0 && w < minTotal {
		w = minTotal
	}
	return w
}

// flexMaxTotal returns an item's max-width as a border-box total, or 0 when it
// is not set.
func (l *layouter) flexMaxTotal(st *Style, extra, availW int) int {
	if st == nil || !st.MaxWidth.Set || st.MaxWidth.IsAuto() {
		return 0
	}
	w := l.widthValue(st, st.MaxWidth, availW)
	if w < 0 {
		return 0
	}
	return w + extra
}

// distributeGrow hands free space to growing items without pushing any of them
// past its max-width. Space an item cannot take is redistributed to the others,
// so a `max-width` column does not silently swallow the row's leftover space.
// Every item's share is computed from the same free-space total; decrementing
// it per item made the first items absorb more and left later ones narrow.
func distributeGrow(widths []int, grow []float64, maxTotals []int, remaining int) {
	for pass := 0; pass < 8 && remaining > 0; pass++ {
		totalGrow := 0.0
		for i := range widths {
			if grow[i] <= 0 {
				continue
			}
			if maxTotals[i] > 0 && widths[i] >= maxTotals[i] {
				continue
			}
			totalGrow += grow[i]
		}
		if totalGrow == 0 {
			return
		}
		budget := remaining
		added := 0
		for i := range widths {
			if budget <= 0 {
				break
			}
			if grow[i] <= 0 {
				continue
			}
			if maxTotals[i] > 0 && widths[i] >= maxTotals[i] {
				continue
			}
			share := int(float64(remaining) * grow[i] / totalGrow)
			if share > budget {
				share = budget
			}
			if maxTotals[i] > 0 && widths[i]+share > maxTotals[i] {
				share = maxTotals[i] - widths[i]
			}
			if share <= 0 {
				continue
			}
			widths[i] += share
			budget -= share
			added += share
		}
		if added == 0 {
			// Rounding can leave fewer free cells than there are growing items,
			// so every proportional share truncates to zero. Hand the remainder
			// out one cell per item in turn instead of letting the first item
			// absorb it, then stop when no item can take another cell.
			for budget > 0 {
				progressed := false
				for i := range widths {
					if budget <= 0 {
						break
					}
					if grow[i] <= 0 {
						continue
					}
					if maxTotals[i] > 0 && widths[i] >= maxTotals[i] {
						continue
					}
					widths[i]++
					budget--
					added++
					progressed = true
				}
				if !progressed {
					break
				}
			}
			if added == 0 {
				return
			}
		}
		remaining = budget
	}
}
