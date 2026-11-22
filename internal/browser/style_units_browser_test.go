package browser

import "testing"

func TestRemUsesRootFontSize(t *testing.T) {
	doc := ParseHTML(`<html><head><style>
		html { font-size: 14px; }
		body { margin: 0; }
		.box { width: 10rem; margin-left: 1.5rem; padding-left: 1rem; }
	</style></head><body><div class="box">text</div></body></html>`)
	rules := ParseStylesheet(`html { font-size: 14px } body { margin: 0 }
		.box { width: 10rem; margin-left: 1.5rem; padding-left: 1rem }`)
	ly := BuildLayout(doc, rules, 80)
	box, _ := findSelectorNode(doc, ".box")
	st := ly.Styles[box]
	// 1rem is the root's 14px: 10rem = 140px = 18 cells, 1.5rem = 21px = 3 cells.
	if got := st.Width.ResolveH(80, st.FontSize); got != 18 {
		t.Fatalf("width:10rem = %d cells, want 18 (root font 14px)", got)
	}
	if got := st.Margin[3].ResolveH(80, st.FontSize); got != 3 {
		t.Fatalf("margin-left:1.5rem = %d cells, want 3", got)
	}
	if got := st.Padding[3].ResolveH(80, st.FontSize); got != 2 {
		t.Fatalf("padding-left:1rem = %d cells, want 2", got)
	}
}

func TestZoomCellRatio(t *testing.T) {
	defer setLayoutZoom(1)

	setLayoutZoom(1)
	if got := px(80).ResolveH(0, 16); got != 10 {
		t.Fatalf("zoom 1: 80px = %d cells, want 10", got)
	}
	if got := px(16).ResolveV(0, 16); got != 1 {
		t.Fatalf("zoom 1: 16px = %d rows, want 1", got)
	}
	if got := cssWidthPx(100); got != 800 {
		t.Fatalf("zoom 1: 100 columns = %d CSS px, want 800", got)
	}

	setLayoutZoom(2)
	if got := px(80).ResolveH(0, 16); got != 20 {
		t.Fatalf("zoom 2: 80px = %d cells, want 20", got)
	}
	if got := px(16).ResolveV(0, 16); got != 2 {
		t.Fatalf("zoom 2: 16px = %d rows, want 2", got)
	}
	// The CSS viewport shrinks, so responsive rules see a narrower screen.
	if got := cssWidthPx(100); got != 400 {
		t.Fatalf("zoom 2: 100 columns = %d CSS px, want 400", got)
	}
	// Percentages stay relative to their container, so they do not zoom.
	if got := pct(50).ResolveH(100, 16); got != 50 {
		t.Fatalf("zoom 2: 50%% of 100 = %d cells, want 50", got)
	}

	// Text metrics keep their normal cell size.
	if got := (&Style{FontSize: 16, LineHeight: 1}).LineHeightCells(); got != 1 {
		t.Fatalf("body line height at zoom 2 = %d rows, want 1", got)
	}
	if got := (&Style{FontSize: 32, LineHeight: 1}).LineHeightCells(); got != 2 {
		t.Fatalf("32px line height at zoom 2 = %d rows, want 2", got)
	}
	// Scaled headings grow with the zoom.
	big := &Style{FontSize: 32}
	if got := bigTextScale(big, 1); got != 2 {
		t.Fatalf("32px scale at zoom 1 = %d, want 2", got)
	}
	if got := bigTextScale(big, 2); got != 4 {
		t.Fatalf("32px scale at zoom 2 = %d, want 4", got)
	}
	if got := bigTextScale(&Style{FontSize: 96}, 2); got != maxTextScale {
		t.Fatalf("large heading scale = %d, want cap %d", got, maxTextScale)
	}
	if got := bigTextScale(&Style{FontSize: 16}, 2); got != 1 {
		t.Fatalf("body text scale = %d, want 1", got)
	}
}
