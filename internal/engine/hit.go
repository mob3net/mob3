package engine

// LinkAt returns the index of the link covering document cell (x, y), or -1.
func (s *Session) LinkAt(x, y int) int {
	if s.Layout == nil {
		return -1
	}
	best, bestLayer := -1, -1
	for i, li := range s.Layout.Links {
		for _, r := range li.Rects {
			if x < r.X || x >= r.X+r.W || y < r.Y || y >= r.Y+r.H {
				continue
			}
			// Positioned overlays and sticky bars paint above in-flow links,
			// so a click on their cells must follow the visible link.
			if layer := OverlayLayer(li.Node); layer > bestLayer {
				best, bestLayer = i, layer
			}
			break
		}
	}
	return best
}

// NodeAt returns the nearest element under a document cell, or nil.
func (s *Session) NodeAt(x, y int) *Node {
	if s.Layout == nil {
		return nil
	}
	var best *Node
	bestLayer, bestArea, bestDepth := -1, 0, 0
	consider := func(n *Node, layer, area, depth int) {
		if n == nil {
			return
		}
		if layer > bestLayer || (layer == bestLayer && (area < bestArea || (area == bestArea && depth > bestDepth))) {
			best, bestLayer, bestArea, bestDepth = n, layer, area, depth
		}
	}
	for i := len(s.Layout.Frags) - 1; i >= 0; i-- {
		f := &s.Layout.Frags[i]
		if f.Node == nil || (f.Kind != FragText && f.Kind != FragImage) {
			continue
		}
		if x < f.X || x >= f.X+f.W || y < f.Y || y >= f.Y+f.H {
			continue
		}
		for p := f.Node; p != nil; p = p.Parent {
			if p.Type == ElementNode {
				consider(p, OverlayLayer(p), -1, nodeDepth(p))
				break
			}
		}
	}
	// Padding and background gaps have no text fragment. Use laid-out element
	// boxes so hover menus remain reachable while moving between their items.
	for n, r := range s.Layout.Boxes {
		if r.W <= 0 || r.H <= 0 || x < r.X || x >= r.X+r.W || y < r.Y || y >= r.Y+r.H {
			continue
		}
		consider(n, OverlayLayer(n), r.W*r.H, nodeDepth(n))
	}
	return best
}

// ScrollBy moves the viewport by n rows and clamps it to the page.
func (s *Session) ScrollBy(n, viewH int) {
	s.Scroll += n
	s.ClampScroll(viewH)
}

// ResolveHref resolves a link target against the page base.
func (s *Session) ResolveHref(href string) string {
	if s.Page == nil || s.Page.Base == nil {
		return href
	}
	if u, err := s.Page.Base.Parse(href); err == nil {
		return u.String()
	}
	return href
}

func OverlayLayer(n *Node) int {
	layer := 0
	for p := n; p != nil; p = p.Parent {
		if p.Style == nil || !IsPositionedStyle(p.Style) {
			continue
		}
		if l := 2 + p.Style.ZIndex; l > layer {
			layer = l
		}
	}
	return layer
}

func nodeDepth(n *Node) int {
	d := 0
	for p := n; p != nil; p = p.Parent {
		if p.Type == ElementNode {
			d++
		}
	}
	return d
}
