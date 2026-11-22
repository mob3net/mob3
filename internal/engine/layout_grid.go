package engine

import (
	"strconv"
	"strings"
)

// flexRow lays out a row-direction flex container. It supports equal or
// explicit item widths, flex-grow, shrink-to-fit items and column gaps; it does
// not implement wrapping, alignment or order.
type gridTrack struct {
	Fixed int
	Flex  float64
	Auto  bool
}

func splitGridParts(s string) []string {
	var out []string
	start := 0
	depth := 0
	brackets := 0
	lower := strings.ToLower(s)
	for i := 0; i < len(s); i++ {
		// Custom property substitution can join two track values without a
		// space (`calc(...)minmax(0,1fr)`, `389pxminmax(0,1fr)`). The CSS
		// tokenizer keeps the dimension and the following function token
		// apart, so treat a top-level track function as a new part.
		if depth == 0 && brackets == 0 && start < i && gridTrackFunctionAt(lower, i) {
			prev := s[i-1]
			if prev != ' ' && prev != '\t' && prev != '\n' && prev != '\r' && prev != ',' && prev != '(' && prev != '/' {
				if part := strings.TrimSpace(s[start:i]); part != "" {
					out = append(out, part)
				}
				start = i
			}
		}
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '[':
			brackets++
		case ']':
			if brackets > 0 {
				brackets--
			}
		case ' ', '\t', '\n', '\r', ',':
			if depth == 0 && brackets == 0 {
				if part := strings.TrimSpace(s[start:i]); part != "" {
					out = append(out, part)
				}
				start = i + 1
			}
		}
	}
	if part := strings.TrimSpace(s[start:]); part != "" {
		out = append(out, part)
	}
	return out
}

// gridTrackFunctionAt reports whether a track-sizing function starts at i.
func gridTrackFunctionAt(lower string, i int) bool {
	// The "max(" in "minmax(" is part of the same token, not a new track.
	if i >= 3 && lower[i-3:i] == "min" && strings.HasPrefix(lower[i:], "max(") {
		return false
	}
	for _, f := range []string{"minmax(", "repeat(", "fit-content(", "max(", "min(", "clamp("} {
		if strings.HasPrefix(lower[i:], f) {
			return true
		}
	}
	return false
}

func hasNamedGridRows(n *Node) bool {
	for _, child := range layoutChildren(n) {
		if child.Type != ElementNode || child.Style == nil {
			continue
		}
		// Only a line *name* needs two-dimensional placement. The value also
		// contains keywords and numbers (`1/span 1`, `span 2`, `auto`), which
		// must not be mistaken for names just because "span" has letters.
		row := strings.ToLower(child.Style.GridRow)
		for _, tok := range strings.FieldsFunc(row, func(r rune) bool {
			return r == '/' || r == ',' || r == ' ' || r == '\t'
		}) {
			if tok == "" || tok == "span" || tok == "auto" {
				continue
			}
			for _, r := range tok {
				if (r >= 'a' && r <= 'z') || r == '_' {
					return true
				}
			}
		}
	}
	return false
}

// parseMinMaxTrack resolves a max()/min() track expression to the largest or
// smallest fixed size among its arguments. Grids use `max(340px,calc(...))` for
// a rail that never drops below its content width.
func parseMinMaxTrack(part string, avail int, font float64) (gridTrack, bool) {
	low := strings.ToLower(part)
	isMax := strings.HasPrefix(low, "max(")
	prefix := "min("
	if isMax {
		prefix = "max("
	}
	if !strings.HasPrefix(low, prefix) || !strings.HasSuffix(part, ")") {
		return gridTrack{}, false
	}
	inner := part[len(prefix) : len(part)-1]
	best := 0
	found := false
	for _, f := range SplitTopLevel(inner, ',') {
		f = strings.TrimSpace(f)
		fl := strings.ToLower(f)
		if f == "" || fl == "auto" || strings.HasSuffix(fl, "fr") {
			continue
		}
		l := ParseLength(f, font)
		if !l.Set || l.Auto {
			continue
		}
		w := l.ResolveH(avail, font)
		if !found || (isMax && w > best) || (!isMax && w < best) {
			best = w
			found = true
		}
	}
	if !found {
		return gridTrack{Auto: true}, true
	}
	return gridTrack{Fixed: best}, true
}

func parseGridTracks(raw string, avail int, font float64) []gridTrack {
	tracks, _ := parseGridTemplate(raw, avail, font)
	return tracks
}

// parseGridTemplate returns tracks plus named grid lines. A bracketed line
// name group sits between tracks; its names map to the 0-based line index at
// that position, so `grid-column: content-start/content-end` resolves without
// creating phantom tracks.
func parseGridTemplate(raw string, avail int, font float64) ([]gridTrack, map[string][]int) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "none") || strings.HasPrefix(strings.ToLower(raw), "subgrid") {
		return nil, nil
	}
	var out []gridTrack
	lines := map[string][]int{}
	for _, part := range splitGridParts(raw) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]") {
			for _, name := range strings.Fields(part[1 : len(part)-1]) {
				lines[name] = append(lines[name], len(out))
			}
			continue
		}
		lower := strings.ToLower(part)
		if strings.HasPrefix(lower, "repeat(") && strings.HasSuffix(part, ")") {
			inner := part[len("repeat(") : len(part)-1]
			fields := SplitTopLevel(inner, ',')
			if len(fields) < 2 {
				continue
			}
			count := 1
			first := strings.TrimSpace(fields[0])
			if n, err := strconv.Atoi(first); err == nil && n > 0 {
				count = n
			}
			rest := strings.Join(fields[1:], ",")
			tracks, _ := parseGridTemplate(rest, avail, font)
			for i := 0; i < count && i < 64; i++ {
				out = append(out, tracks...)
			}
			continue
		}
		if (strings.HasPrefix(lower, "max(") || strings.HasPrefix(lower, "min(")) && strings.HasSuffix(part, ")") {
			if t, ok := parseMinMaxTrack(part, avail, font); ok {
				out = append(out, t)
			}
			continue
		}
		if strings.HasPrefix(lower, "minmax(") && strings.HasSuffix(part, ")") {
			inner := part[len("minmax(") : len(part)-1]
			fields := SplitTopLevel(inner, ',')
			if len(fields) == 2 {
				part = strings.TrimSpace(fields[1])
				lower = strings.ToLower(part)
			}
		}
		switch {
		case strings.HasSuffix(lower, "fr"):
			if f, err := strconv.ParseFloat(strings.TrimSuffix(lower, "fr"), 64); err == nil && f > 0 {
				out = append(out, gridTrack{Flex: f})
			} else {
				out = append(out, gridTrack{Flex: 1})
			}
		case lower == "auto" || strings.Contains(lower, "content") || strings.HasPrefix(lower, "fit-content"):
			out = append(out, gridTrack{Auto: true})
		default:
			if l := ParseLength(part, font); l.Set {
				out = append(out, gridTrack{Fixed: l.ResolveH(avail, font)})
			} else {
				out = append(out, gridTrack{Auto: true})
			}
		}
	}
	return out, lines
}

// resolveGridTracks distributes the container width over the parsed tracks.
// Fixed tracks keep their declared width, auto tracks take their content width
// (capped by the space left after fixed tracks), and fr tracks split what
// remains. When no fr track exists, leftover space is shared between auto
// tracks so plain `auto auto` grids still fill the row.
func resolveGridTracks(tracks []gridTrack, avail, gap int, content, minContent []int) []int {
	if len(tracks) == 0 {
		return nil
	}
	widths := make([]int, len(tracks))
	fixedTotal := 0
	flexTotal := 0.0
	autoCount := 0
	for i, t := range tracks {
		if t.Flex > 0 {
			flexTotal += t.Flex
		} else if t.Auto {
			autoCount++
			w := 1
			if i < len(content) && content[i] > w {
				w = content[i]
			}
			widths[i] = w
		} else {
			widths[i] = t.Fixed
			fixedTotal += t.Fixed
		}
	}
	remaining := avail - gap*(len(tracks)-1) - fixedTotal
	if remaining < 0 {
		remaining = 0
	}
	autoTotal := 0
	for i, t := range tracks {
		if t.Auto {
			autoTotal += widths[i]
		}
	}
	if autoTotal > remaining {
		// Shrink auto tracks toward their min-content width so the flex tracks
		// still get room, then leave any remaining overflow to the safety scale
		// below.
		shrinkAutoTracks(tracks, widths, minContent, autoTotal-remaining)
		autoTotal = 0
		for i, t := range tracks {
			if t.Auto {
				autoTotal += widths[i]
			}
		}
	}
	remaining -= autoTotal
	if remaining < 0 {
		remaining = 0
	}
	if flexTotal > 0 {
		for i, t := range tracks {
			if t.Flex > 0 {
				widths[i] = int(float64(remaining) * t.Flex / flexTotal)
			}
		}
	} else if autoCount > 0 && remaining > 0 {
		each := remaining / autoCount
		for i, t := range tracks {
			if t.Auto {
				widths[i] += each
			}
		}
	}
	total := 0
	for i := range widths {
		if widths[i] < 1 {
			widths[i] = 1
		}
		total += widths[i]
	}
	if max := avail - gap*(len(tracks)-1); total > max && max > 0 {
		scaled := 0
		for i := range widths {
			widths[i] = widths[i] * max / total
			if widths[i] < 1 {
				widths[i] = 1
			}
			scaled += widths[i]
		}
		if diff := scaled - max; diff > 0 {
			for i := len(widths) - 1; i >= 0 && diff > 0; i-- {
				if widths[i] > 1 {
					widths[i]--
					diff--
				}
			}
		}
	}
	return widths
}

// shrinkAutoTracks removes cells from auto tracks in proportion to their slack
// above the min-content width, never below one cell.
func shrinkAutoTracks(tracks []gridTrack, widths, mins []int, deficit int) {
	for deficit > 0 {
		slack := 0
		for i, t := range tracks {
			if !t.Auto {
				continue
			}
			floor := 1
			if i < len(mins) && mins[i] > floor {
				floor = mins[i]
			}
			if widths[i] > floor {
				slack += widths[i] - floor
			}
		}
		if slack <= 0 {
			return
		}
		take := MaxInt(1, MinInt(deficit, slack))
		removed := 0
		for i, t := range tracks {
			if !t.Auto {
				continue
			}
			floor := 1
			if i < len(mins) && mins[i] > floor {
				floor = mins[i]
			}
			if widths[i] <= floor {
				continue
			}
			d := (widths[i] - floor) * take / slack
			if d > widths[i]-floor {
				d = widths[i] - floor
			}
			if d < 1 {
				d = 1
			}
			widths[i] -= d
			removed += d
		}
		if removed == 0 {
			return
		}
		deficit -= removed
	}
}

// gridTrackContent measures the max-content and min-content width each column
// needs from the items placed in it. Spanned items spread their deficit over
// the columns they cover, like table column sizing.
func (l *Layouter) gridTrackContent(cells []gridCell, cols int) (content, minContent []int) {
	content = make([]int, cols)
	minContent = make([]int, cols)
	for _, c := range cells {
		if c.out {
			continue
		}
		span := c.span
		if span < 1 {
			span = 1
		}
		if c.start < 0 || c.start >= cols {
			continue
		}
		if span > cols-c.start {
			span = cols - c.start
		}
		mw, mi := l.measure(c.node)
		growTrackSpan(content, c.start, span, mw)
		growTrackSpan(minContent, c.start, span, mi)
	}
	return content, minContent
}

func growTrackSpan(widths []int, start, span, need int) {
	if need <= 0 || span < 1 {
		return
	}
	used := 0
	for i := 0; i < span && start+i < len(widths); i++ {
		used += widths[start+i]
	}
	if need <= used {
		return
	}
	deficit := need - used
	for i := 0; i < span && start+i < len(widths); i++ {
		add := deficit / span
		if i == span-1 {
			add = deficit - add*i
		}
		if widths[start+i]+add > widths[start+i] {
			widths[start+i] += add
		}
	}
}

func gridPlacement(raw string, cols int) (start, span int, explicit bool) {
	return gridPlacementNamed(raw, cols, nil)
}

// gridPlacementNamed resolves numeric lines, spans and named grid lines.
func gridPlacementNamed(raw string, cols int, lines map[string][]int) (start, span int, explicit bool) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return -1, 1, false
	}
	parts := SplitTopLevel(raw, '/')
	first := strings.TrimSpace(parts[0])
	if strings.HasPrefix(first, "span ") {
		if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(first, "span "))); err == nil && n > 0 {
			return -1, n, true
		}
		return -1, 1, true
	}
	line := 0
	lineOK := false
	if name := strings.TrimSpace(first); name != "" {
		if indexes := lines[name]; len(indexes) > 0 {
			start = indexes[0]
			lineOK = true
		} else if n, err := strconv.Atoi(name); err == nil {
			line = n
			lineOK = true
		}
	}
	if !lineOK {
		return -1, 1, false
	}
	if lineOK && line != 0 {
		start = line - 1
		if start < 0 {
			start = cols + line
		}
		if start < 0 {
			start = 0
		}
	}
	span = 1
	if len(parts) > 1 {
		endRaw := strings.TrimSpace(parts[1])
		if strings.HasPrefix(endRaw, "span ") {
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(endRaw, "span "))); err == nil && n > 0 {
				span = n
			}
		} else if indexes := lines[endRaw]; len(indexes) > 0 {
			if end := indexes[len(indexes)-1]; end > start {
				span = end - start
			}
		} else if endLine, err := strconv.Atoi(endRaw); err == nil {
			if endLine < 0 {
				endLine = cols + endLine + 2
			}
			if endLine > line {
				span = endLine - line
			}
		}
	}
	if span < 1 {
		span = 1
	}
	return start, span, true
}

type gridAreaSpan struct {
	start int
	span  int
}

// parseGridAreas extracts named areas from grid-template-areas. Only the
// column placement is needed by the row-major grid implementation; a name
// spanning several rows still starts at its leftmost column.
func parseGridAreas(raw string) map[string]gridAreaSpan {
	out := map[string]gridAreaSpan{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	var rows [][]string
	for i := 0; i < len(raw); {
		for i < len(raw) && raw[i] != '\'' && raw[i] != '"' {
			i++
		}
		if i >= len(raw) {
			break
		}
		quote := raw[i]
		i++
		start := i
		for i < len(raw) && raw[i] != quote {
			i++
		}
		if i >= len(raw) {
			break
		}
		rows = append(rows, strings.Fields(raw[start:i]))
		i++
	}
	type bounds struct {
		start, end int
		rows       int
	}
	seen := map[string]bounds{}
	for _, row := range rows {
		for c, name := range row {
			if name == "." || name == "" {
				continue
			}
			b, ok := seen[name]
			if !ok {
				b.start, b.end = c, c
			} else {
				if c < b.start {
					b.start = c
				}
				if c > b.end {
					b.end = c
				}
			}
			b.rows++
			seen[name] = b
		}
	}
	for name, b := range seen {
		out[name] = gridAreaSpan{start: b.start, span: b.end - b.start + 1}
	}
	return out
}

func (l *Layouter) grid(n *Node, x, y, availW int) int {
	if l.outOfTime() {
		return 0
	}
	st := n.Style
	if st == nil {
		return l.flow(n, x, y, availW)
	}
	tracks, gridLines := parseGridTemplate(st.GridCols, availW, st.FontSize)
	if hasNamedGridRows(n) {
		// Named row lines need full two-dimensional placement. Without it,
		// stacking the items is more readable than letting them overlap.
		return l.flow(n, x, y, availW)
	}
	if len(tracks) == 0 {
		// A grid with no explicit template has one implicit column. Keep it as
		// a grid when the container centers or right-aligns its items, so a
		// max-width child is aligned instead of falling back to block flow and
		// sticking to the left edge.
		if st.JustifyItems == "center" || st.JustifyItems == "end" {
			tracks = []gridTrack{{Flex: 1}}
		} else {
			return l.flow(n, x, y, availW)
		}
	}
	gap := st.Gap.ResolveH(availW, st.FontSize)
	if !st.Gap.Set {
		gap = 0
	}
	rowGap := gap
	if st.RowGap.Set {
		rowGap = st.RowGap.ResolveH(availW, st.FontSize)
	}
	areas := parseGridAreas(st.GridAreas)
	cells := gridCells(n, len(tracks), gridLines, areas)
	content, minContent := l.gridTrackContent(cells, len(tracks))
	widths := resolveGridTracks(tracks, availW, gap, content, minContent)
	// An auto track placed by a named area is at least as wide as that area's
	// content; take the space from fr tracks so a CTA or label does not end up
	// one character wide.
	for _, child := range layoutChildren(n) {
		if child.Type != ElementNode || child.Style == nil {
			continue
		}
		a, ok := areas[child.Style.GridArea]
		if !ok || a.span != 1 || a.start < 0 || a.start >= len(tracks) || !tracks[a.start].Auto {
			continue
		}
		pref, _ := l.measure(child)
		need := MinInt(MaxInt(2, pref), availW)
		if need <= widths[a.start] {
			continue
		}
		extra := need - widths[a.start]
		widths[a.start] = need
		flexLeft := 0.0
		for i := range tracks {
			if tracks[i].Flex > 0 {
				flexLeft += tracks[i].Flex
			}
		}
		for i := range tracks {
			if extra <= 0 || flexLeft <= 0 || tracks[i].Flex <= 0 || widths[i] <= 1 {
				continue
			}
			take := int(float64(extra) * tracks[i].Flex / flexLeft)
			if take > widths[i]-1 {
				take = widths[i] - 1
			}
			if take > extra {
				take = extra
			}
			widths[i] -= take
			extra -= take
			flexLeft -= tracks[i].Flex
		}
	}
	if len(widths) > 4 {
		// A many-track grid whose columns are all narrow is more readable as
		// block flow, but tiny *tracks* alone are not the test: a 24-track
		// page grid places cards across 6-12 tracks, so their text is still
		// wide. Judge the width of the placed items instead.
		totalTracks := 0
		for _, w := range widths {
			totalTracks += w
		}
		if totalTracks/len(widths) < 6 {
			totalCells, cellCount := 0, 0
			for _, c := range cells {
				if c.out {
					continue
				}
				w := 0
				for i := 0; i < c.span && c.start+i < len(widths); i++ {
					w += widths[c.start+i]
					if i > 0 {
						w += gap
					}
				}
				totalCells += w
				cellCount++
			}
			if cellCount == 0 || totalCells/cellCount < 6 {
				return l.flow(n, x, y, availW)
			}
		}
	}
	colX := make([]int, len(widths))
	cx := x
	for i, w := range widths {
		colX[i] = cx
		cx += w + gap
	}
	startY := y
	maxRow := 0
	for _, c := range cells {
		if c.out {
			l.deferAbsolute(c.node, x, y)
			continue
		}
		if c.row > maxRow {
			maxRow = c.row
		}
	}
	// Lay out one row at a time: items on the same row share its height, and
	// an explicit grid-row keeps a side rail level with the lead card even
	// when the lead card comes first in the DOM.
	for r := 0; r <= maxRow; r++ {
		rowH := 0
		for _, c := range cells {
			if c.out || c.row != r {
				continue
			}
			col := c.start
			if col < 0 || col >= len(widths) {
				col = 0
			}
			cellX := colX[col]
			cellW := 0
			for i := 0; i < c.span && col+i < len(widths); i++ {
				cellW += widths[col+i]
				if i > 0 {
					cellW += gap
				}
			}
			// An auto-placed item stays in its assigned track and wraps its
			// content; max-content width never expands its span across columns.
			fragStart := len(l.frags)
			absStart := len(l.abs)
			h := l.block(c.node, cellX, y, cellW)
			if h > rowH {
				rowH = h
			}
			l.alignGridItemX(n, c.node, cellX, cellW, fragStart, absStart)
		}
		y += rowH
		if r < maxRow && rowH > 0 {
			y += rowGap
		}
	}
	return y - startY
}

// gridCell is one grid item's resolved column placement.
type gridCell struct {
	node     *Node
	start    int
	span     int
	row      int
	explicit bool
	out      bool // out-of-flow item, deferred to the absolute pass
}

// gridRowPlacement reads a numeric grid-row start. Named lines are handled by
// hasNamedGridRows, which sends such grids to block flow before this is used.
func gridRowPlacement(raw string) (start, span int, set bool) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" || raw == "auto" {
		return 0, 1, false
	}
	parts := SplitTopLevel(raw, '/')
	first := strings.TrimSpace(parts[0])
	if first == "" || first == "auto" || strings.HasPrefix(first, "span ") {
		return 0, 1, false
	}
	n, err := strconv.Atoi(first)
	if err != nil {
		return 0, 1, false
	}
	start = n - 1
	if start < 0 {
		return 0, 1, false
	}
	span = 1
	if len(parts) > 1 {
		end := strings.TrimSpace(parts[1])
		if strings.HasPrefix(end, "span ") {
			if v, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(end, "span "))); err == nil && v > 0 {
				span = v
			}
		} else if v, err := strconv.Atoi(end); err == nil && v > n {
			span = v - n
		}
	}
	return start, span, true
}

// gridCells resolves every item's column start and span in row-major order.
// Sizing the tracks and laying the items out share this placement, so an item
// cannot be measured for one column and painted in another. An explicit
// numeric grid-row lands in that row even when it appears later in the DOM
// than a card placed to its right, which keeps page grids with a centre lead
// card and side rails on one row instead of stacking the rails below it.
func gridCells(n *Node, cols int, lines map[string][]int, areas map[string]gridAreaSpan) []gridCell {
	if cols < 1 {
		return nil
	}
	var cells []gridCell
	autoRow, autoCol := 0, 0
	for _, child := range layoutChildrenWithPseudo(n) {
		if child.Type != ElementNode || child.Style == nil || child.Style.Display == "none" {
			continue
		}
		if isOutOfFlow(child.Style) {
			cells = append(cells, gridCell{node: child, out: true})
			continue
		}
		start, span, explicit := gridPlacementNamed(child.Style.GridColumn, cols, lines)
		if name := child.Style.GridArea; name != "" {
			if a, ok := areas[name]; ok {
				start, span, explicit = a.start, a.span, true
			}
		}
		if span < 1 {
			span = 1
		}
		if span > cols {
			span = cols
		}
		if row, _, rowSet := gridRowPlacement(child.Style.GridRow); rowSet {
			if start < 0 || start+span > cols {
				start = 0
			}
			cells = append(cells, gridCell{node: child, start: start, span: span, row: row, explicit: true})
			continue
		}
		if explicit && start >= 0 && start < cols {
			// An explicit placement that would move backwards on the current
			// row starts a new row (named areas commonly span a full row).
			if start < autoCol {
				autoRow++
				autoCol = 0
			}
			autoCol = start
		}
		if autoCol+span > cols {
			autoRow++
			autoCol = 0
		}
		cells = append(cells, gridCell{node: child, start: autoCol, span: span, explicit: explicit, row: autoRow})
		autoCol += span
		if autoCol >= cols {
			autoCol = 0
			autoRow++
		}
	}
	return cells
}

// alignGridItemX applies justify-items/justify-self to one placed grid item.
// Vertical alignment and auto-flow remain approximated.
func (l *Layouter) alignGridItemX(container, item *Node, cellX, cellW, fragStart, absStart int) {
	align := ""
	if item != nil && item.Style != nil {
		align = item.Style.JustifySelf
	}
	if align == "" && container != nil && container.Style != nil {
		align = container.Style.JustifyItems
	}
	if align != "center" && align != "end" {
		return
	}
	box, ok := l.boxes[item]
	if !ok || item.Style == nil {
		return
	}
	ml, mr, _, _, _, _, _, _, _, _, _, _ := l.BoxMetrics(item.Style, cellW)
	leftover := cellW - ml - mr - box.W
	if leftover <= 0 {
		return
	}
	dx := leftover
	if align == "center" {
		dx = leftover / 2
	}
	if dx == 0 {
		return
	}
	for i := fragStart; i < len(l.frags); i++ {
		l.frags[i].X += dx
	}
	for i := absStart; i < len(l.abs); i++ {
		l.abs[i].staticX += dx
	}
	var walk func(*Node)
	walk = func(n *Node) {
		if r, ok := l.boxes[n]; ok {
			r.X += dx
			l.boxes[n] = r
		}
		if cb := l.cbs[n]; cb != nil {
			cb.x += dx
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(item)
}

// GridTrack is one parsed grid track.
type GridTrack = gridTrack

// ParseGridTracks parses a grid-template value into tracks.
func ParseGridTracks(raw string, avail int, font float64) []GridTrack {
	return parseGridTracks(raw, avail, font)
}

// ResolveGridTracks distributes an available width over tracks.
func ResolveGridTracks(tracks []GridTrack, avail, gap int, content, minContent []int) []int {
	return resolveGridTracks(tracks, avail, gap, content, minContent)
}

// GridPlacement parses a numeric grid placement.
func GridPlacement(raw string, cols int) (start, span int, explicit bool) {
	return gridPlacement(raw, cols)
}
