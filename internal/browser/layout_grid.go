package browser

import (
	"strconv"
	"strings"
)

// flexRow lays out a row-direction flex container. It supports equal or
// explicit item widths, flex-grow, shrink-to-fit items and column gaps; it does
// not implement wrapping, alignment or order.
type gridTrack struct {
	fixed int
	flex  float64
	auto  bool
}

func splitGridParts(s string) []string {
	var out []string
	start := 0
	depth := 0
	brackets := 0
	for i := 0; i < len(s); i++ {
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

func hasNamedGridRows(n *Node) bool {
	for _, child := range layoutChildren(n) {
		if child.Type != ElementNode || child.Style == nil {
			continue
		}
		row := strings.TrimSpace(strings.ToLower(child.Style.GridRow))
		if row == "" {
			continue
		}
		for _, r := range row {
			if (r >= 'a' && r <= 'z') || r == '_' {
				return true
			}
		}
	}
	return false
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
			fields := splitTopLevel(inner, ',')
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
		if strings.HasPrefix(lower, "minmax(") && strings.HasSuffix(part, ")") {
			inner := part[len("minmax(") : len(part)-1]
			fields := splitTopLevel(inner, ',')
			if len(fields) == 2 {
				part = strings.TrimSpace(fields[1])
				lower = strings.ToLower(part)
			}
		}
		switch {
		case strings.HasSuffix(lower, "fr"):
			if f, err := strconv.ParseFloat(strings.TrimSuffix(lower, "fr"), 64); err == nil && f > 0 {
				out = append(out, gridTrack{flex: f})
			} else {
				out = append(out, gridTrack{flex: 1})
			}
		case lower == "auto" || strings.Contains(lower, "content") || strings.HasPrefix(lower, "fit-content"):
			out = append(out, gridTrack{auto: true})
		default:
			if l := parseLength(part, font); l.Set {
				out = append(out, gridTrack{fixed: l.resolveH(avail, font)})
			} else {
				out = append(out, gridTrack{auto: true})
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
		if t.flex > 0 {
			flexTotal += t.flex
		} else if t.auto {
			autoCount++
			w := 1
			if i < len(content) && content[i] > w {
				w = content[i]
			}
			widths[i] = w
		} else {
			widths[i] = t.fixed
			fixedTotal += t.fixed
		}
	}
	remaining := avail - gap*(len(tracks)-1) - fixedTotal
	if remaining < 0 {
		remaining = 0
	}
	autoTotal := 0
	for i, t := range tracks {
		if t.auto {
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
			if t.auto {
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
			if t.flex > 0 {
				widths[i] = int(float64(remaining) * t.flex / flexTotal)
			}
		}
	} else if autoCount > 0 && remaining > 0 {
		each := remaining / autoCount
		for i, t := range tracks {
			if t.auto {
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
			if !t.auto {
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
		take := maxInt(1, minInt(deficit, slack))
		removed := 0
		for i, t := range tracks {
			if !t.auto {
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
func (l *layouter) gridTrackContent(cells []gridCell, cols int) (content, minContent []int) {
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
	parts := splitTopLevel(raw, '/')
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

func (l *layouter) grid(n *Node, x, y, availW int) int {
	if l.outOfTime() {
		return 0
	}
	st := n.Style
	if st == nil {
		return l.flow(n, x, y, availW)
	}
	tracks, gridLines := parseGridTemplate(st.GridCols, availW, st.FontSize)
	if len(tracks) == 0 || hasNamedGridRows(n) {
		// Named row lines need full two-dimensional placement. Without it,
		// stacking the items is more readable than letting them overlap.
		return l.flow(n, x, y, availW)
	}
	gap := st.Gap.resolveH(availW, st.FontSize)
	if !st.Gap.Set {
		gap = 0
	}
	rowGap := gap
	if st.RowGap.Set {
		rowGap = st.RowGap.resolveH(availW, st.FontSize)
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
		if !ok || a.span != 1 || a.start < 0 || a.start >= len(tracks) || !tracks[a.start].auto {
			continue
		}
		pref, _ := l.measure(child)
		need := minInt(maxInt(2, pref), availW)
		if need <= widths[a.start] {
			continue
		}
		extra := need - widths[a.start]
		widths[a.start] = need
		flexLeft := 0.0
		for i := range tracks {
			if tracks[i].flex > 0 {
				flexLeft += tracks[i].flex
			}
		}
		for i := range tracks {
			if extra <= 0 || flexLeft <= 0 || tracks[i].flex <= 0 || widths[i] <= 1 {
				continue
			}
			take := int(float64(extra) * tracks[i].flex / flexLeft)
			if take > widths[i]-1 {
				take = widths[i] - 1
			}
			if take > extra {
				take = extra
			}
			widths[i] -= take
			extra -= take
			flexLeft -= tracks[i].flex
		}
	}
	if len(widths) > 4 {
		total := 0
		for _, w := range widths {
			total += w
		}
		if total/len(widths) < 6 {
			return l.flow(n, x, y, availW)
		}
	}
	colX := make([]int, len(widths))
	cx := x
	for i, w := range widths {
		colX[i] = cx
		cx += w + gap
	}
	col := 0
	rowH := 0
	startY := y
	for _, c := range cells {
		child := c.node
		if c.out {
			l.deferAbsolute(child, x, y)
			continue
		}
		if c.wrap {
			y += rowH + rowGap
			rowH = 0
		}
		col = c.start
		span := c.span
		cellX := colX[col]
		cellW := 0
		for i := 0; i < span && col+i < len(widths); i++ {
			cellW += widths[col+i]
			if i > 0 {
				cellW += gap
			}
		}
		// An auto-placed item stays in its assigned track and wraps its
		// content; max-content width never expands its span across columns.
		h := l.block(child, cellX, y, cellW)
		if h > rowH {
			rowH = h
		}
		col += span
		if col >= len(widths) {
			y += rowH + rowGap
			rowH = 0
			col = 0
		}
	}
	if col > 0 {
		y += rowH
	}
	return y - startY
}

// gridCell is one grid item's resolved column placement.
type gridCell struct {
	node     *Node
	start    int
	span     int
	explicit bool
	out      bool // out-of-flow item, deferred to the absolute pass
	wrap     bool // starts a new row before being laid out
}

// gridCells resolves every item's column start and span in row-major order.
// Sizing the tracks and laying the items out share this placement, so an item
// cannot be measured for one column and painted in another.
func gridCells(n *Node, cols int, lines map[string][]int, areas map[string]gridAreaSpan) []gridCell {
	if cols < 1 {
		return nil
	}
	var cells []gridCell
	col := 0
	for _, child := range layoutChildren(n) {
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
		wrap := false
		if explicit && start >= 0 && start < cols {
			// An explicit placement that would move backwards on the current
			// row starts a new row (named areas commonly span a full row).
			if start < col && col > 0 {
				wrap = true
				col = 0
			}
			col = start
		}
		if span < 1 {
			span = 1
		}
		if span > cols {
			span = cols
		}
		if col+span > cols {
			wrap = true
			col = 0
		}
		cells = append(cells, gridCell{node: child, start: col, span: span, explicit: explicit, wrap: wrap})
		col += span
		if col >= cols {
			col = 0
		}
	}
	return cells
}
