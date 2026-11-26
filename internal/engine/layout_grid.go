package engine

import (
	"sort"
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

// maxGridRows bounds explicit and implicit row state so a hostile grid value
// cannot allocate unbounded rows.
const maxGridRows = 256

// parseGridRows parses explicit row tracks and named row lines. Row tracks
// store their minimum height in cells; auto and fr rows grow with content
// because the grid container itself has indefinite height.
func parseGridRows(raw string, font float64) ([]gridTrack, map[string][]int) {
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
			tracks, nestedLines := parseGridRows(rest, font)
			for i := 0; i < count && i < 64; i++ {
				base := len(out)
				out = append(out, tracks...)
				for name, indexes := range nestedLines {
					for _, index := range indexes {
						lines[name] = append(lines[name], base+index)
					}
				}
				if len(out) >= maxGridRows {
					break
				}
			}
			continue
		}
		if (strings.HasPrefix(lower, "max(") || strings.HasPrefix(lower, "min(")) && strings.HasSuffix(part, ")") {
			if t, ok := parseMinMaxRowTrack(part, font); ok {
				out = append(out, t)
			}
			continue
		}
		if strings.HasPrefix(lower, "minmax(") && strings.HasSuffix(part, ")") {
			inner := part[len("minmax(") : len(part)-1]
			fields := SplitTopLevel(inner, ',')
			if len(fields) == 2 {
				minRaw := strings.TrimSpace(fields[0])
				if l := ParseLength(minRaw, font); l.Set && !l.Auto && l.U != UnitPercent {
					if h := l.ResolveV(0, font); h > 0 {
						out = append(out, gridTrack{Fixed: h})
						continue
					}
				}
				out = append(out, gridTrack{Auto: true})
				continue
			}
		}
		switch {
		case strings.HasSuffix(lower, "fr"):
			out = append(out, gridTrack{Auto: true})
		case lower == "auto" || strings.Contains(lower, "content") || strings.HasPrefix(lower, "fit-content"):
			out = append(out, gridTrack{Auto: true})
		default:
			if l := ParseLength(part, font); l.Set && !l.Auto && l.U != UnitPercent {
				if h := l.ResolveV(0, font); h >= 0 {
					out = append(out, gridTrack{Fixed: h})
					continue
				}
			}
			out = append(out, gridTrack{Auto: true})
		}
	}
	if len(out) > maxGridRows {
		out = out[:maxGridRows]
	}
	return out, lines
}

// parseMinMaxRowTrack resolves a min()/max() row expression to its smallest or
// largest fixed height in cells.
func parseMinMaxRowTrack(part string, font float64) (gridTrack, bool) {
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
		if !l.Set || l.Auto || l.U == UnitPercent {
			continue
		}
		h := l.ResolveV(0, font)
		if !found || (isMax && h > best) || (!isMax && h < best) {
			best = h
			found = true
		}
	}
	if !found {
		return gridTrack{Auto: true}, true
	}
	return gridTrack{Fixed: best}, true
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
	start    int
	span     int
	rowStart int
	rowSpan  int
}

// parseGridAreas extracts named areas from grid-template-areas. The name's
// column and row bounds both describe its placement, so a hero spanning two
// area rows reserves both rows.
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
		start, end       int
		rowStart, rowEnd int
	}
	seen := map[string]bounds{}
	for r, row := range rows {
		for c, name := range row {
			if name == "." || name == "" {
				continue
			}
			b, ok := seen[name]
			if !ok {
				b.start, b.end = c, c
				b.rowStart, b.rowEnd = r, r
			} else {
				if c < b.start {
					b.start = c
				}
				if c > b.end {
					b.end = c
				}
				if r < b.rowStart {
					b.rowStart = r
				}
				if r > b.rowEnd {
					b.rowEnd = r
				}
			}
			seen[name] = b
		}
	}
	for name, b := range seen {
		out[name] = gridAreaSpan{
			start: b.start, span: b.end - b.start + 1,
			rowStart: b.rowStart, rowSpan: b.rowEnd - b.rowStart + 1,
		}
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
	rowTracks, rowLines := parseGridRows(st.GridRows, st.FontSize)
	areas := parseGridAreas(st.GridAreas)
	areaCols := 0
	for _, a := range areas {
		if end := a.start + a.span; end > areaCols {
			areaCols = end
		}
	}
	if len(tracks) == 0 {
		// A grid with no explicit template has one implicit column. Keep it as
		// a grid when the container centers or right-aligns its items, when
		// explicit rows need the 2D placement path, or when grid areas define
		// the implicit columns.
		switch {
		case areaCols > 0:
			for i := 0; i < areaCols; i++ {
				tracks = append(tracks, gridTrack{Flex: 1})
			}
		case len(rowTracks) > 0 || st.JustifyItems == "center" || st.JustifyItems == "end":
			tracks = []gridTrack{{Flex: 1}}
		default:
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
	flow := strings.ToLower(st.GridAutoFlow)
	cells := gridCells(n, len(tracks), gridLines, rowLines, len(rowTracks), areas, strings.Contains(flow, "column"), strings.Contains(flow, "dense"))
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
	maxRow := len(rowTracks) - 1
	for _, c := range cells {
		if c.out {
			l.deferAbsolute(c.node, x, y)
			continue
		}
		if end := c.row + c.rowSpan - 1; end > maxRow {
			maxRow = end
		}
	}
	rowHeights := make([]int, maxRow+1)
	for i := range rowHeights {
		if i < len(rowTracks) && rowTracks[i].Flex == 0 && !rowTracks[i].Auto {
			if h := rowTracks[i].Fixed; h > 0 {
				rowHeights[i] = h
			}
		}
	}
	// Lay out one row at a time: items on the same row share its height, and
	// an explicit grid-row keeps a side rail level with the lead card even
	// when the lead card comes first in the DOM. Content of a row-spanning item
	// is distributed over its spanned rows so later tracks stay aligned.
	type spanNeed struct {
		end, height int
	}
	var placed []gridPlaced
	for r := 0; r <= maxRow; r++ {
		rowH := rowHeights[r]
		var spans []spanNeed
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
			if c.rowSpan > 1 {
				spans = append(spans, spanNeed{end: c.row + c.rowSpan - 1, height: h})
			} else if h > rowH {
				rowH = h
			}
			l.alignGridItemX(n, c.node, cellX, cellW, fragStart, absStart)
			placed = append(placed, gridPlaced{
				node: c.node, row: c.row, rowSpan: c.rowSpan,
				fragStart: fragStart, absStart: absStart, height: h,
			})
		}
		rowHeights[r] = rowH
		for _, s := range spans {
			sum := rowGap * (s.end - r)
			for i := r; i <= s.end; i++ {
				sum += rowHeights[i]
			}
			if s.height > sum {
				rowHeights[s.end] += s.height - sum
			}
		}
		y += rowHeights[r]
		if r < maxRow && rowHeights[r] > 0 {
			y += rowGap
		}
	}
	for _, p := range placed {
		avail := 0
		end := p.row + p.rowSpan - 1
		for i := p.row; i <= end; i++ {
			avail += rowHeights[i]
			if i > p.row {
				avail += rowGap
			}
		}
		l.alignGridItemY(n, p.node, avail, p.height, p.fragStart, p.absStart)
	}
	return y - startY
}

// gridPlaced records one laid-out grid item for vertical alignment after its
// final row heights are known.
type gridPlaced struct {
	node      *Node
	row       int
	rowSpan   int
	fragStart int
	absStart  int
	height    int
}

// gridCell is one grid item's resolved placement.
type gridCell struct {
	node    *Node
	start   int
	span    int
	row     int
	rowSpan int
	colSet  bool
	rowSet  bool
	out     bool // out-of-flow item, deferred to the absolute pass
}

// gridOccupancy marks the cells reserved by placed items so sparse auto
// placement can find a free slot.
type gridOccupancy struct {
	cols int
	rows [][]bool
}

func (o *gridOccupancy) ensure(row int) {
	for len(o.rows) <= row {
		o.rows = append(o.rows, make([]bool, o.cols))
	}
}

func (o *gridOccupancy) free(row, col, rowSpan, colSpan int) bool {
	if col < 0 || colSpan < 1 || col+colSpan > o.cols || row < 0 || rowSpan < 1 || row+rowSpan > maxGridRows {
		return false
	}
	o.ensure(row + rowSpan - 1)
	for r := row; r < row+rowSpan; r++ {
		for c := col; c < col+colSpan; c++ {
			if o.rows[r][c] {
				return false
			}
		}
	}
	return true
}

func (o *gridOccupancy) mark(row, col, rowSpan, colSpan int) {
	if col < 0 || colSpan < 1 || col+colSpan > o.cols || row < 0 || rowSpan < 1 {
		return
	}
	o.ensure(row + rowSpan - 1)
	for r := row; r < row+rowSpan; r++ {
		for c := col; c < col+colSpan; c++ {
			o.rows[r][c] = true
		}
	}
}

func (o *gridOccupancy) firstFreeRowMajor(rowStart, colStart, rowSpan, colSpan, limit int) (int, int, bool) {
	if rowStart < 0 {
		rowStart = 0
	}
	if colStart < 0 {
		colStart = 0
	}
	for r := rowStart; r <= limit; r++ {
		start := 0
		if r == rowStart {
			start = colStart
		}
		for c := start; c+colSpan <= o.cols; c++ {
			if o.free(r, c, rowSpan, colSpan) {
				return r, c, true
			}
		}
	}
	return 0, 0, false
}

func (o *gridOccupancy) firstFreeColumnMajor(rowStart, colStart, rowSpan, colSpan, limit int) (int, int, bool) {
	if rowStart < 0 {
		rowStart = 0
	}
	if colStart < 0 {
		colStart = 0
	}
	for c := colStart; c+colSpan <= o.cols; c++ {
		start := 0
		if c == colStart {
			start = rowStart
		}
		for r := start; r <= limit; r++ {
			if o.free(r, c, rowSpan, colSpan) {
				return r, c, true
			}
		}
	}
	return 0, 0, false
}

// gridCells resolves every item's row and column placement before sizing and
// painting. Definite placements are reserved first, then auto items fill the
// remaining cells in sparse row-major order. Row spans reserve every covered
// cell so an auto item cannot land inside them.
func gridCells(n *Node, cols int, colLines map[string][]int, rowLines map[string][]int, rowCount int, areas map[string]gridAreaSpan, columnFlow, dense bool) []gridCell {
	if cols < 1 {
		return nil
	}
	children := layoutChildrenWithPseudo(n)
	sort.SliceStable(children, func(i, j int) bool {
		oi, oj := 0, 0
		if children[i].Style != nil {
			oi = children[i].Style.Order
		}
		if children[j].Style != nil {
			oj = children[j].Style.Order
		}
		return oi < oj
	})
	var cells []gridCell
	for _, child := range children {
		if child.Type != ElementNode || child.Style == nil || child.Style.Display == "none" {
			continue
		}
		if isOutOfFlow(child.Style) {
			cells = append(cells, gridCell{node: child, out: true})
			continue
		}
		cell := gridCell{node: child, start: -1, span: 1, row: -1, rowSpan: 1}
		colStart, colSpan, colExplicit := gridPlacementNamed(child.Style.GridColumn, cols, colLines)
		rowStart, rowSpan, rowExplicit := gridPlacementNamed(child.Style.GridRow, rowCount, rowLines)
		if span := colSpan; span > 1 {
			cell.span = span
		}
		if span := rowSpan; span > 1 {
			cell.rowSpan = span
		}
		if colExplicit && colStart >= 0 {
			cell.start = colStart
			cell.colSet = true
		}
		if rowExplicit && rowStart >= 0 {
			cell.row = rowStart
			cell.rowSet = true
		}
		if name := child.Style.GridArea; name != "" {
			if a, ok := areas[name]; ok {
				cell.start, cell.span, cell.colSet = a.start, a.span, true
				if !rowExplicit {
					cell.row, cell.rowSpan, cell.rowSet = a.rowStart, a.rowSpan, true
				}
			}
		}
		if cell.span < 1 {
			cell.span = 1
		}
		if cell.span > cols {
			cell.span = cols
		}
		if cell.rowSpan < 1 {
			cell.rowSpan = 1
		}
		if cell.rowSpan > maxGridRows {
			cell.rowSpan = maxGridRows
		}
		if cell.rowSet && cell.row >= maxGridRows {
			cell.row = maxGridRows - 1
		}
		if cell.rowSet && cell.row+cell.rowSpan > maxGridRows {
			cell.rowSpan = maxGridRows - cell.row
		}
		if cell.rowSpan < 1 {
			cell.rowSpan = 1
		}
		if cell.colSet && (cell.start < 0 || cell.start+cell.span > cols) {
			cell.start = 0
			if cell.span > cols {
				cell.span = cols
			}
		}
		cells = append(cells, cell)
	}
	occ := &gridOccupancy{cols: cols}
	maxReserved := 0
	for i := range cells {
		c := &cells[i]
		if !c.out && c.rowSet {
			if end := c.row + c.rowSpan - 1; end > maxReserved {
				maxReserved = end
			}
		}
		if !c.out && c.rowSet && c.colSet {
			occ.mark(c.row, c.start, c.rowSpan, c.span)
		}
	}
	autoRow, autoCol := 0, 0
	limit := len(cells) + rowCount + 8
	if maxReserved+8 > limit {
		limit = maxReserved + 8
	}
	if limit >= maxGridRows {
		limit = maxGridRows - 1
	}
	colLimit := limit
	if columnFlow {
		colLimit = rowCount
		if colLimit < 1 {
			colLimit = 1
		}
		if maxReserved+1 > colLimit {
			colLimit = maxReserved + 1
		}
		if colLimit > maxGridRows {
			colLimit = maxGridRows
		}
		colLimit--
	}
	for i := range cells {
		c := &cells[i]
		if c.out || c.rowSet && c.colSet {
			continue
		}
		switch {
		case c.rowSet:
			// Definite row: take the first free column in that row.
			col := -1
			for start := autoCol; start < cols; start++ {
				if occ.free(c.row, start, c.rowSpan, c.span) {
					col = start
					break
				}
			}
			if col < 0 {
				for start := 0; start < autoCol; start++ {
					if occ.free(c.row, start, c.rowSpan, c.span) {
						col = start
						break
					}
				}
			}
			if col < 0 {
				col = 0
			}
			c.start, c.colSet = col, true
		case c.colSet:
			// Definite column: take the first free row at that column.
			row := -1
			for r := autoRow; r <= limit; r++ {
				if occ.free(r, c.start, c.rowSpan, c.span) {
					row = r
					break
				}
			}
			if row < 0 {
				row = autoRow
			}
			c.row, c.rowSet = row, true
		default:
			// Fully auto: sparse placement from the cursor, or from the origin
			// for dense flow.
			startRow, startCol := autoRow, autoCol
			if dense {
				startRow, startCol = 0, 0
			}
			var row, col int
			var ok bool
			if columnFlow {
				row, col, ok = occ.firstFreeColumnMajor(startRow, startCol, c.rowSpan, c.span, colLimit)
			} else {
				row, col, ok = occ.firstFreeRowMajor(startRow, startCol, c.rowSpan, c.span, limit)
			}
			if !ok {
				row, col = autoRow, 0
			}
			c.row, c.start = row, col
			c.rowSet, c.colSet = true, true
			if !dense {
				if columnFlow {
					autoRow, autoCol = row+c.rowSpan, col
					if autoRow > colLimit {
						autoRow, autoCol = 0, autoCol+1
					}
					if autoCol >= cols {
						autoRow, autoCol = 0, 0
					}
				} else {
					autoRow, autoCol = row, col+c.span
					if autoCol >= cols {
						autoRow++
						autoCol = 0
					}
				}
			}
		}
		if c.row >= maxGridRows {
			c.row = maxGridRows - 1
		}
		if c.row+c.rowSpan > maxGridRows {
			c.rowSpan = maxGridRows - c.row
		}
		if c.rowSpan < 1 {
			c.rowSpan = 1
		}
		occ.mark(c.row, c.start, c.rowSpan, c.span)
	}
	return cells
}

// alignGridItemX applies justify-items/justify-self to one placed grid item.
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
	l.shiftGridItem(item, fragStart, absStart, dx, 0)
}

// alignGridItemY applies align-items/align-self inside one row or row span.
func (l *Layouter) alignGridItemY(container, item *Node, available, height, fragStart, absStart int) {
	align := ""
	if item != nil && item.Style != nil {
		align = item.Style.AlignSelf
	}
	if align == "" && container != nil && container.Style != nil {
		align = container.Style.AlignItems
	}
	if align == "" {
		align = "stretch"
	}
	leftover := available - height
	if leftover <= 0 || item == nil {
		return
	}
	switch align {
	case "stretch":
		if item.Style != nil && item.Style.Height.Set && !item.Style.Height.IsAuto() {
			return
		}
		box, ok := l.boxes[item]
		if !ok {
			return
		}
		oldH := box.H
		box.H += leftover
		l.boxes[item] = box
		fillDone, imageDone := false, false
		for i := fragStart; i < len(l.frags); i++ {
			f := &l.frags[i]
			if f.X != box.X || f.Y != box.Y || f.W != box.W {
				continue
			}
			if !fillDone && f.Kind == FragFill && f.H == oldH {
				f.H += leftover
				fillDone = true
			}
			if !imageDone && f.Kind == FragImage && f.Background && f.H == oldH {
				f.H += leftover
				imageDone = true
			}
		}
	case "center":
		l.shiftGridItem(item, fragStart, absStart, 0, leftover/2)
	case "end":
		l.shiftGridItem(item, fragStart, absStart, 0, leftover)
	}
}

// shiftGridItem moves the laid-out fragments and boxes for one grid item.
func (l *Layouter) shiftGridItem(item *Node, fragStart, absStart, dx, dy int) {
	if (dx == 0 && dy == 0) || item == nil {
		return
	}
	for i := fragStart; i < len(l.frags); i++ {
		l.frags[i].X += dx
		l.frags[i].Y += dy
	}
	for i := absStart; i < len(l.abs); i++ {
		l.abs[i].staticX += dx
		l.abs[i].staticY += dy
	}
	var walk func(*Node)
	walk = func(n *Node) {
		if r, ok := l.boxes[n]; ok {
			r.X += dx
			r.Y += dy
			l.boxes[n] = r
		}
		if cb := l.cbs[n]; cb != nil {
			cb.x += dx
			cb.y += dy
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
