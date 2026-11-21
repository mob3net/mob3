package browser

// SetFixedScroll repositions viewport-anchored fragments for the current
// scroll offset: fixed fragments are shifted into document coordinates, and
// sticky fragments follow their top offset until their containing block ends.
// Painting, hit testing, find and selection therefore keep using one
// coordinate space.
func (ly *Layout) SetFixedScroll(scroll int) {
	if ly.fixedApplied && ly.fixedScroll == scroll {
		return
	}
	changed := false
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Fixed {
			f.Y = f.FixedY + scroll
			changed = true
		}
	}
	for i := range ly.stickies {
		s := &ly.stickies[i]
		shift := s.shift(scroll)
		for _, idx := range s.frags {
			f := &ly.Frags[idx]
			y := f.StickyBaseY + shift
			if f.Y != y {
				f.Y = y
				changed = true
			}
		}
	}
	ly.fixedScroll = scroll
	ly.fixedApplied = true
	if changed {
		ly.rebuildLinks()
	}
}

// stickyBox is one resolved position:sticky group: its in-flow box, the
// containing block it may travel within, and the fragments that move with it.
type stickyBox struct {
	boxY, boxH      int
	cbTop, cbBottom int
	topRows         int
	frags           []int
}

// shift returns how far the group moves down for a scroll offset. The group
// never moves above its in-flow position and stops when its bottom reaches the
// containing block's bottom.
func (s *stickyBox) shift(scroll int) int {
	shift := scroll + s.topRows - s.boxY
	if shift < 0 {
		return 0
	}
	if max := s.cbBottom - (s.boxY + s.boxH); shift > max {
		shift = max
	}
	if shift < 0 {
		return 0
	}
	return shift
}

// resolveStickies turns the recorded sticky nodes into groups using the final
// box geometry. Elements without a top offset keep their normal flow position.
func (l *layouter) resolveStickies() []stickyBox {
	if len(l.stickyNodes) == 0 {
		return nil
	}
	out := make([]stickyBox, 0, len(l.stickyNodes))
	for gi, n := range l.stickyNodes {
		if n == nil || n.Style == nil {
			continue
		}
		r, ok := l.boxes[n]
		if !ok || r.H <= 0 {
			continue
		}
		group := gi + 1
		sb := stickyBox{boxY: r.Y, boxH: r.H}
		cb := Rect{Y: 0, W: l.width, H: l.rootCB.h}
		for p := n.Parent; p != nil; p = p.Parent {
			if pr, ok := l.boxes[p]; ok {
				cb = pr
				break
			}
		}
		if cb.H < 1 {
			cb.H = 1
		}
		sb.cbTop, sb.cbBottom = cb.Y, cb.Y+cb.H
		sb.topRows = nonNeg(n.Style.Top.resolveV(cb.H, n.Style.FontSize))
		for i := range l.frags {
			f := &l.frags[i]
			if f.Sticky != group {
				continue
			}
			sb.frags = append(sb.frags, i)
		}
		if len(sb.frags) > 0 {
			out = append(out, sb)
		}
	}
	return out
}

// nestedSticky reports whether an ancestor is already sticky. Nested sticky
// boxes are approximated as normal flow so two groups cannot fight over the
// same fragments.
func (l *layouter) nestedSticky(n *Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Style != nil && p.Style.Position == "sticky" {
			return true
		}
	}
	return false
}

// rebuildLinks recreates link rectangles after fixed fragments move.
func (ly *Layout) rebuildLinks() {
	ly.Links = ly.Links[:0]
	for i := range ly.Frags {
		ly.Frags[i].Link = nil
	}
	ly.buildLinks()
}

// buildLinks assigns fragments to links and records link rectangles in
// document order.
func (ly *Layout) buildLinks() {
	byNode := map[*Node]*LinkInfo{}
	for i := range ly.Frags {
		f := &ly.Frags[i]
		a := nearestAnchor(f.Node)
		if a == nil {
			continue
		}
		href := a.Attr("href")
		if href == "" {
			continue
		}
		li := byNode[a]
		if li == nil {
			li = &LinkInfo{Node: a, Href: href, Index: len(ly.Links)}
			byNode[a] = li
			ly.Links = append(ly.Links, li)
		}
		f.Link = li
		if f.Kind == fragFill {
			continue
		}
		li.Rects = append(li.Rects, Rect{X: f.X, Y: f.Y, W: f.W, H: maxInt(1, f.H)})
	}
}

func nearestAnchor(n *Node) *Node {
	for p := n; p != nil; p = p.Parent {
		if p.Type == ElementNode && p.Tag == "a" && p.HasAttr("href") {
			return p
		}
	}
	return nil
}
