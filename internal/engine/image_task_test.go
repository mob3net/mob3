package engine

import (
	"image"
	"net/url"
	"testing"
)

func TestImageSourceSrcsetAndLazyAttrs(t *testing.T) {
	pic := &Node{Type: ElementNode, Tag: "picture"}
	source := &Node{Type: ElementNode, Tag: "source", Parent: pic,
		Attrs: []Attr{{Key: "srcset", Value: "mobile.jpg 480w"}, {Key: "media", Value: "(max-width: 500px)"}}}
	img := &Node{Type: ElementNode, Tag: "img", Parent: pic,
		Attrs: []Attr{{Key: "src", Value: "fallback.jpg"}}}
	pic.Children = []*Node{source, img}
	if got := ImageSource(img, 400); got != "mobile.jpg" {
		t.Fatalf("mobile picture = %q", got)
	}
	if got := ImageSource(img, 1600); got != "fallback.jpg" {
		t.Fatalf("desktop picture = %q", got)
	}
	lazy := &Node{Type: ElementNode, Tag: "img",
		Attrs: []Attr{{Key: "data-src", Value: "lazy.jpg"}}}
	if got := ImageSource(lazy, 800); got != "lazy.jpg" {
		t.Fatalf("data-src = %q", got)
	}
}

func TestImageTasksAndViewportOrder(t *testing.T) {
	doc := ParseHTML(`<html><body>` +
		`<img id="d" src="d.png"><img id="c" src="c.png">` +
		`<img id="b" src="b.png"><img id="a" src="a.png"></body></html>`)
	byID := map[string]*Node{}
	doc.FindAll(func(n *Node) bool {
		if n.Tag == "img" {
			byID[n.Attr("id")] = n
		}
		return false
	})
	base := mustParseURL(t, "https://example.com/")
	tasks := ImageTasks(doc, base, 800)
	if len(tasks) != 4 {
		t.Fatalf("tasks = %d", len(tasks))
	}
	s := NewSession()
	s.Scroll = 0
	s.Layout = &Layout{Frags: []Frag{
		{Kind: FragText, Node: byID["d"], Y: 100},
		{Kind: FragText, Node: byID["c"], Y: 25},
		{Kind: FragText, Node: byID["b"], Y: 5},
		{Kind: FragText, Node: byID["a"], Y: 0},
	}}
	s.SortImageTasks(tasks, 20)
	want := []string{"b", "a", "c", "d"}
	for i := range want {
		if got := tasks[i].Node.Attr("id"); got != want[i] {
			t.Fatalf("priority[%d] = %q, want %q", i, got, want[i])
		}
	}
}

func TestProtocolPlacementsClipAndCache(t *testing.T) {
	s := NewSession()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	p := ImagePlacement{Img: img, Src: "x", X: -1, Y: -1, W: 4, H: 4, RowStart: 0, TotalRows: 4}
	out := s.ProtocolPlacements(nil, []ImagePlacement{p}, 2, 2)
	if len(out) != 1 {
		t.Fatalf("placements = %d", len(out))
	}
	if out[0].X != 0 || out[0].Y != 0 || out[0].W != 2 || out[0].H != 2 {
		t.Fatalf("clipped placement = %+v", out[0])
	}
	if out[0].RowStart != 1 {
		t.Fatalf("row start = %d, want 1", out[0].RowStart)
	}
	again := s.ProtocolPlacements(nil, []ImagePlacement{p}, 2, 2)
	if len(again) != 1 || again[0].Key != out[0].Key {
		t.Fatalf("placement cache did not reuse the image: %+v vs %+v", again, out)
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
