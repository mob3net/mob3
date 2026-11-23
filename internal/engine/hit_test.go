package engine

import "testing"

func TestSessionHitTestingAndScroll(t *testing.T) {
	n := &Node{Type: ElementNode, Tag: "a"}
	box := &Node{Type: ElementNode, Tag: "div"}
	s := NewEmptySession()
	s.Layout = &Layout{
		Height: 20,
		Links:  []*LinkInfo{{Node: n, Href: "/a", Rects: []Rect{{X: 1, Y: 2, W: 3, H: 1}}}},
		Frags:  []Frag{{Kind: FragText, Node: box, X: 0, Y: 4, W: 5, H: 1}},
		Boxes:  map[*Node]Rect{box: {X: 0, Y: 4, W: 5, H: 1}},
	}
	if got := s.LinkAt(2, 2); got != 0 {
		t.Fatalf("LinkAt = %d, want 0", got)
	}
	if got := s.LinkAt(0, 0); got != -1 {
		t.Fatalf("LinkAt outside = %d, want -1", got)
	}
	if got := s.NodeAt(1, 4); got != box {
		t.Fatalf("NodeAt = %v, want %v", got, box)
	}
	if got := s.NodeAt(20, 20); got != nil {
		t.Fatalf("NodeAt outside = %v", got)
	}
	s.Scroll = 5
	s.ScrollBy(100, 10)
	if s.Scroll != 10 {
		t.Fatalf("scroll = %d, want 10", s.Scroll)
	}
	s.Page = &Page{Base: mustParseURL(t, "https://example.com/dir/")}
	if got := s.ResolveHref("x"); got != "https://example.com/dir/x" {
		t.Fatalf("ResolveHref = %q", got)
	}
	if got := s.ResolveHref("%zz"); got != "%zz" {
		t.Fatalf("bad href = %q", got)
	}
}
