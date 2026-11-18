package browser

// SetFixedScroll repositions viewport-anchored fragments for the current
// scroll offset. Fixed fragments are stored relative to the viewport and then
// shifted into document coordinates so painting, hit testing, find and
// selection can keep using one coordinate space.
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
	ly.fixedScroll = scroll
	ly.fixedApplied = true
	if changed {
		ly.rebuildLinks()
	}
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
