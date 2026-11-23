package engine

import (
	"strings"
	"testing"
)

func TestLinkRects(t *testing.T) {
	ly := build(`<p>Click <a href="/next">this link</a> now.</p>`, 40)
	if len(ly.Links) != 1 {
		t.Fatalf("links = %d, want 1", len(ly.Links))
	}
	if len(ly.Links[0].Rects) == 0 {
		t.Fatal("link has no rects")
	}
	found := false
	for _, f := range ly.Frags {
		if f.Kind == fragText && f.Link == ly.Links[0] {
			found = true
		}
	}
	if !found {
		t.Fatal("no text fragment is attached to the link")
	}
}

func TestLinkSpaceStaysOutsideLink(t *testing.T) {
	ly := build(`<p>Dark theme for <a href="/gnome">GNOME</a></p>`, 60)
	if len(ly.Links) != 1 {
		t.Fatalf("links = %d, want 1", len(ly.Links))
	}
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind != fragText || f.Text != "GNOME" {
			continue
		}
		if f.Link != ly.Links[0] {
			t.Fatal("GNOME fragment is not part of the link")
		}
		if got := ly.Links[0].Rects[0].W; got != stringWidth("GNOME") {
			t.Fatalf("link rect width = %d, want %d", got, stringWidth("GNOME"))
		}
		if i == 0 {
			t.Fatal("no space before the link fragment")
		}
		prev := &ly.Frags[i-1]
		if prev.Kind != fragText || strings.TrimSpace(prev.Text) != "" {
			t.Fatalf("previous fragment = %q", prev.Text)
		}
		if prev.Link != nil {
			t.Fatal("leading space was attached to the link")
		}
		if prev.Style != nil && prev.Style.Underline {
			t.Fatal("leading space is underlined")
		}
		return
	}
	t.Fatal("GNOME text fragment not found")
}

func TestFixedLinkRectsFollowScroll(t *testing.T) {
	ly := buildViewport(`<a href="#x" style="position:fixed;bottom:0;left:0">go</a>`, 30, 10)
	if len(ly.Links) != 1 || len(ly.Links[0].Rects) == 0 {
		t.Fatalf("links = %+v", ly.Links)
	}
	if r := ly.Links[0].Rects[0]; r.Y != 9 {
		t.Fatalf("fixed link Y = %d, want 9", r.Y)
	}
	ly.SetFixedScroll(3)
	if r := ly.Links[0].Rects[0]; r.Y != 12 {
		t.Fatalf("scrolled fixed link Y = %d, want 12", r.Y)
	}
}

func TestEmptyOverlayAnchorIsLinkable(t *testing.T) {
	doc := ParseHTML(`<html><body>
<div style="position:relative;width:80px;height:40px">
  <a href="/article" style="position:absolute;top:0;right:0;bottom:0;left:0"></a>
</div></body></html>`)
	ly := BuildLayout(doc, nil, 40)
	for _, li := range ly.Links {
		if li.Href == "/article" {
			if len(li.Rects) == 0 || li.Rects[0].W < 2 || li.Rects[0].H < 1 {
				t.Fatalf("overlay link rects = %+v", li.Rects)
			}
			return
		}
	}
	t.Fatal("empty overlay anchor did not become a link")
}
