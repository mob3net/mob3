package browser

import (
	"strings"
	"testing"
)

func TestGridNamedAreasAndAutoTrack(t *testing.T) {
	ly := build(`<div style="display:grid;grid-template-columns:minmax(0,1fr) auto;grid-template-areas:'heading cta' 'promos promos'">`+
		`<div style="grid-area:heading">head</div>`+
		`<div style="grid-area:promos">alpha beta gamma delta</div>`+
		`<a style="grid-area:cta">View more</a></div>`, 40)
	find := func(text string) *Frag {
		for i := range ly.Frags {
			if ly.Frags[i].Kind == fragText && ly.Frags[i].Text == text {
				return &ly.Frags[i]
			}
		}
		return nil
	}
	head, alpha, view, more := find("head"), find("alpha"), find("View"), find("more")
	if head == nil || alpha == nil || view == nil || more == nil {
		t.Fatalf("missing fragments: head=%v alpha=%v view=%v more=%v", head, alpha, view, more)
	}
	if alpha.Y <= head.Y {
		t.Fatalf("full-width area did not start a new row: head y=%d alpha y=%d", head.Y, alpha.Y)
	}
	if view.Y != more.Y {
		t.Fatalf("auto track collapsed: View y=%d more y=%d", view.Y, more.Y)
	}
}

func TestNamedGridLines(t *testing.T) {
	rules := ParseStylesheet(`.g{display:grid;grid-template-columns:[a] repeat(4,1fr) [b]}.item{grid-column:a/b}`)
	doc := ParseHTML(`<html><body><div class="g"><div class="item">` + strings.Repeat("word ", 30) + `</div></div></body></html>`)
	ly := BuildLayout(doc, rules, 40)
	lines := map[int]bool{}
	for _, f := range ly.Frags {
		if f.Kind == fragText {
			lines[f.Y] = true
		}
	}
	if len(lines) > 8 {
		t.Fatalf("named-line grid item wrapped to %d lines, want the four-track span", len(lines))
	}
}

func TestGridAutoItemStaysInItsTrack(t *testing.T) {
	doc := ParseHTML(`<div style="display:grid;grid-template-columns:120px 120px">` +
		`<div id="a">alpha alpha alpha alpha alpha</div>` +
		`<div id="b">beta beta beta beta beta</div></div>`)
	ly := BuildLayout(doc, nil, 80)
	var a, b *Node
	doc.FindAll(func(n *Node) bool {
		switch n.Attr("id") {
		case "a":
			a = n
		case "b":
			b = n
		}
		return false
	})
	if a == nil || b == nil {
		t.Fatal("grid items missing")
	}
	ra, rb := ly.Boxes[a], ly.Boxes[b]
	if ra.W != 15 || rb.W != 15 {
		t.Fatalf("auto grid item widths = %d and %d, want 15 each", ra.W, rb.W)
	}
	if rb.X-ra.X != 15 {
		t.Fatalf("second column x offset = %d, want 15", rb.X-ra.X)
	}
}
