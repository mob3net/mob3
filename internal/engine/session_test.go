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
		Page:   &Page{URL: u, Base: u, Root: doc},
		URL:    u,
		Layout: &Layout{Links: []*LinkInfo{{Node: link}}},
		Focus:  0,
		Hover:  -1,
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
