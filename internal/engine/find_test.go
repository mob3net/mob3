package engine

import "testing"

func TestFindPageAndHTMLMatches(t *testing.T) {
	layout := &Layout{Frags: []Frag{
		{Kind: FragText, X: 0, Y: 0, W: 7, Text: "Reader "},
		{Kind: FragText, X: 0, Y: 1, W: 5, Text: "mode"},
	}}
	page := FindPageMatches(layout, "reader mode", false)
	if len(page) != 1 || len(page[0].Rects) != 2 {
		t.Fatalf("page matches = %+v", page)
	}
	if page[0].Y != 0 || page[0].Frag != 0 {
		t.Fatalf("page match = %+v", page[0])
	}

	html := FindHTMLMatches([]string{"Reader", "mode"}, "reader mode")
	if len(html) != 1 || len(html[0].Rects) != 2 || html[0].Line != 0 {
		t.Fatalf("html matches = %+v", html)
	}
	if got := FindHTMLMatches([]string{"abc"}, "zzz"); len(got) != 0 {
		t.Fatalf("unexpected matches: %+v", got)
	}
}
