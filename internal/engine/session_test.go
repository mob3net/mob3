package engine

import (
	"net/url"
	"testing"

	"browser/internal/protocol"
)

func TestSessionPageState(t *testing.T) {
	u, _ := url.Parse("https://example.com/a")
	link := &Node{Type: ElementNode, Tag: "a"}
	doc := ParseHTML(`<html><head><meta http-equiv="refresh" content="0;url=/next"></head><body></body></html>`)
	s := Session{
		Frame: &Frame{
			Page:   &Page{URL: u, Base: u, Root: doc},
			URL:    u,
			Layout: &Layout{Links: []*LinkInfo{{Node: link}}},
		},
		displayState: displayState{Focus: 0, Hover: -1},
	}
	if s.PageKind() != protocol.PageHTML {
		t.Fatalf("kind = %v", s.PageKind())
	}
	if s.FocusedLink() == nil || s.HoveredLink() != nil {
		t.Fatalf("links focus=%v hover=%v", s.FocusedLink(), s.HoveredLink())
	}
	if got := MetaRefreshTarget(s.Page); got != "https://example.com/next" {
		t.Fatalf("refresh = %q", got)
	}
	if !SameDocument(u, &url.URL{Scheme: "https", Host: "example.com", Path: "/a", Fragment: "x"}) {
		t.Fatal("same-document fragment check failed")
	}
	if s.PageFG().Set == false || s.PageBG().Set == false {
		t.Fatal("page colors not set")
	}
}

func TestSessionFindHintsAndSelection(t *testing.T) {
	link := &Node{Type: ElementNode, Tag: "a"}
	s := Session{Frame: &Frame{Layout: &Layout{
		Frags: []Frag{{Kind: FragText, Text: "x", Link: &LinkInfo{Index: 3}}},
		Links: []*LinkInfo{{Node: link}, {}},
	}}}
	s.OpenFind(FindScopePage)
	s.FindText = "x"
	s.FindMatches = []FindMatch{{Frag: 0}}
	if got := s.FindBarCount(); got != " 1/1 " {
		t.Fatalf("count = %q", got)
	}
	if s.CurrentFindLink() == nil {
		t.Fatal("current find link missing")
	}
	s.ClearFind()
	if s.FindActive || s.FindText != "" || s.FindMatches != nil {
		t.Fatalf("clear find active=%v text=%q matches=%v", s.FindActive, s.FindText, s.FindMatches)
	}

	s.HintTargets = []HintTarget{{Link: 0}}
	if !s.LinkHints {
		// ClearHints reports whether it was active.
		if s.ClearHints() {
			t.Fatal("clear inactive hints reported active")
		}
	}
	s.LinkHints = true
	if !s.ClearHints() || s.LinkHints || s.HintTargets != nil {
		t.Fatal("hints not cleared")
	}
	if s.HintTargetAt(0).Link != 0 || s.HintTargetAt(1).Link != 1 {
		t.Fatal("hint target fallback failed")
	}

	s.Selection.Anchor = CellPos{X: 0}
	s.Selection.Cursor = CellPos{X: 1}
	if !s.HasSelection() {
		t.Fatal("selection missing")
	}
	s.ClearSelection()
	if s.HasSelection() || s.Selecting {
		t.Fatal("selection not cleared")
	}
}

func TestSessionPDFPageTracking(t *testing.T) {
	page := &PDFDocument{Pages: []PDFPageText{{}, {}}}
	nodes := []*Node{{Type: ElementNode, Tag: "div"}, {Type: ElementNode, Tag: "div"}}
	page.PageNodes = nodes
	s := Session{Frame: &Frame{
		Page:   &Page{PDF: page},
		Layout: &Layout{Frags: []Frag{{Node: nodes[0], Y: 0}, {Node: nodes[1], Y: 40}}},
		Scroll: 45,
	}}
	s.UpdatePDFPageStarts()
	if len(s.PDFStarts) != 2 || s.PDFStarts[1] != 40 {
		t.Fatalf("starts = %v", s.PDFStarts)
	}
	if got := s.PDFPageNumber(); got != 2 {
		t.Fatalf("page = %d, want 2", got)
	}
}

func TestSessionScrollMathAndAnchor(t *testing.T) {
	node := &Node{Type: ElementNode, Tag: "h2", Attrs: []Attr{{Key: "id", Value: "target"}}}
	s := Session{Frame: &Frame{
		Layout: &Layout{Height: 100, Frags: []Frag{{Node: node, Y: 40}}},
		Scroll: 200,
	}}
	if got := s.MaxScroll(20); got != 80 {
		t.Fatalf("max = %d", got)
	}
	s.ClampScroll(20)
	if s.Scroll != 80 {
		t.Fatalf("clamp = %d", s.Scroll)
	}
	s.Scroll = 40
	if got := s.ScrollPercent(20); got != 50 {
		t.Fatalf("percent = %d", got)
	}
	if !s.ScrollToAnchor("target", 20) || s.Scroll != 39 {
		t.Fatalf("anchor scroll = %d", s.Scroll)
	}
}
