package browser

import (
	"strings"
	"testing"
)

func TestGridAutoTrackSizedToContent(t *testing.T) {
	// `auto 1fr` is the common thumbnail + text pattern. The auto track must
	// take the thumbnail's content width instead of collapsing to one cell for
	// the fr track to fill; otherwise images are squeezed into a vertical strip.
	ly := build(`<div style="display:grid;grid-template-columns:auto 1fr;gap:8px">`+
		`<span style="display:inline-block;width:100%">ABC</span>`+
		`<div>Story starts here</div></div>`, 40)
	find := func(text string) *Frag {
		for i := range ly.Frags {
			if ly.Frags[i].Kind == fragText && ly.Frags[i].Text == text {
				return &ly.Frags[i]
			}
		}
		return nil
	}
	thumb, story := find("ABC"), find("Story")
	if thumb == nil || story == nil {
		t.Fatalf("missing fragments: thumb=%v story=%v", thumb, story)
	}
	if thumb.W != 3 {
		t.Fatalf("thumbnail width = %d, want 3", thumb.W)
	}
	// Auto track (3) + gap (1) puts the text column at 4.
	if story.X != thumb.X+thumb.W+1 {
		t.Fatalf("story text x = %d, want %d (auto track must be content-sized)", story.X, thumb.X+thumb.W+1)
	}
}

func TestResolveGridTracksAutoContent(t *testing.T) {
	tracks := parseGridTracks("auto 1fr", 40, 16)
	widths := resolveGridTracks(tracks, 40, 1, []int{12, 0}, []int{4, 0})
	if widths[0] != 12 || widths[1] != 27 {
		t.Fatalf("widths = %v, want auto=12 fr=27", widths)
	}

	// A very wide auto item yields space instead of starving the fr track, and
	// the final scale keeps the row inside the container.
	widths = resolveGridTracks(tracks, 10, 1, []int{30, 0}, []int{30, 0})
	if widths[0]+widths[1] > 10 || widths[1] < 1 {
		t.Fatalf("narrow widths = %v, want the row to fit with a non-empty fr track", widths)
	}

	// Without a fr track, leftover space is still shared between auto tracks so
	// plain `auto auto` grids keep filling the row.
	tracks = parseGridTracks("auto auto", 20, 16)
	widths = resolveGridTracks(tracks, 20, 0, []int{2, 2}, []int{2, 2})
	if widths[0] != 10 || widths[1] != 10 {
		t.Fatalf("auto auto widths = %v, want an even split", widths)
	}
}

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

func TestGridDenseTracksJudgedByItemWidth(t *testing.T) {
	// A page grid uses many narrow tracks (24 of 3-4 cells) but its cards span
	// 6-12 tracks, so their text is wide. The dense-grid fallback must judge
	// the placed items, not the average track, or the whole grid collapses
	// into a single left-aligned column.
	doc := ParseHTML(`<html><body style="margin:0"><div style="display:grid;grid-template-columns:repeat(24,1fr);column-gap:16px">` +
		`<div id="center" style="grid-column:7/span 12">centre lead story</div>` +
		`<div id="left" style="grid-column:1/span 6;grid-row:1">left rail</div>` +
		`<div id="right" style="grid-column:19/span 6;grid-row:1">right rail</div>` +
		`</div></body></html>`)
	ly := BuildLayout(doc, nil, 120)
	box := map[string]Rect{}
	doc.FindAll(func(n *Node) bool {
		if id := n.Attr("id"); id != "" {
			box[id] = ly.Boxes[n]
		}
		return false
	})
	center, left, right := box["center"], box["left"], box["right"]
	if center.W < 20 {
		t.Fatalf("lead card width = %d cells, want its 12-track span", center.W)
	}
	// The left and right rails are declared on row 1 after the lead card in
	// the DOM, so they must stay level with it instead of wrapping below.
	if left.Y != center.Y || right.Y != center.Y {
		t.Fatalf("rails not on the lead row: center y=%d left y=%d right y=%d", center.Y, left.Y, right.Y)
	}
	if !(left.X < center.X && center.X < right.X) {
		t.Fatalf("columns out of order: left x=%d center x=%d right x=%d", left.X, center.X, right.X)
	}
}

func TestGridExplicitRowBeatsDOMOrder(t *testing.T) {
	// A centre card written first and side cards with grid-row:1 later in the
	// DOM form one row, as they would in a browser.
	doc := ParseHTML(`<html><body style="margin:0"><div style="display:grid;grid-template-columns:1fr 2fr 1fr;gap:8px">` +
		`<div id="mid" style="grid-column:2;grid-row:1">middle</div>` +
		`<div id="left" style="grid-column:1;grid-row:1">left side</div>` +
		`<div id="right" style="grid-column:3;grid-row:1">right side</div>` +
		`</div></body></html>`)
	ly := BuildLayout(doc, nil, 80)
	box := map[string]Rect{}
	doc.FindAll(func(n *Node) bool {
		if id := n.Attr("id"); id != "" {
			box[id] = ly.Boxes[n]
		}
		return false
	})
	mid, left, right := box["mid"], box["left"], box["right"]
	if left.Y != mid.Y || right.Y != mid.Y {
		t.Fatalf("explicit row split: mid y=%d left y=%d right y=%d", mid.Y, left.Y, right.Y)
	}
	if !(left.X < mid.X && mid.X < right.X) {
		t.Fatalf("columns out of order: left x=%d mid x=%d right x=%d", left.X, mid.X, right.X)
	}
}
