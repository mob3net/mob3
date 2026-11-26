package engine

import (
	"strings"
	"testing"
)

func TestFlexWrapStartsNewLine(t *testing.T) {
	ly := build(`<div style="display:flex;flex-wrap:wrap"><div style="width:100%">alpha</div><div style="width:100%">beta</div></div>`, 40)
	var alpha, beta *Frag
	for i := range ly.Frags {
		if ly.Frags[i].Kind != FragText {
			continue
		}
		switch ly.Frags[i].Text {
		case "alpha":
			alpha = &ly.Frags[i]
		case "beta":
			beta = &ly.Frags[i]
		}
	}
	if alpha == nil || beta == nil {
		t.Fatal("missing wrapped flex items")
	}
	if beta.Y <= alpha.Y {
		t.Fatalf("wrapped flex item stayed on the first row: alpha y=%d beta y=%d", alpha.Y, beta.Y)
	}
}

func TestFlexItemMinSizeClampedBySpecifiedWidth(t *testing.T) {
	doc := ParseHTML(`<html><body style="display:flex">
	<div id="row" style="display:flex;flex-flow:row wrap;width:100%">
		<section id="ticker" style="width:100%"><div style="white-space:nowrap">` +
		strings.Repeat("LONGLABEL ", 80) +
		`</div></section>
		<section id="article" style="width:100%"><p>Article body</p></section>
	</div>
</body></html>`)
	ly := BuildLayout(doc, nil, 40)
	byID := func(id string) *Node {
		var out *Node
		doc.FindAll(func(n *Node) bool {
			if n.Attr("id") == id {
				out = n
				return true
			}
			return false
		})
		return out
	}
	ticker, article := byID("ticker"), byID("article")
	if ticker == nil || article == nil {
		t.Fatal("missing fixture nodes")
	}
	if box := ly.Boxes[ticker]; box.W > 40 {
		t.Fatalf("full-width ticker expanded to %d cells, want <= 40", box.W)
	}
	if box := ly.Boxes[article]; box.X < 0 || box.X+box.W > 40 {
		t.Fatalf("article section lies outside the viewport: %+v", box)
	}
}

func TestFlexInlineBlockItemKeepsBlockChildren(t *testing.T) {
	doc := ParseHTML(`<html><body>
	<div style="display:flex;flex-flow:row">
		<div id="a" style="display:inline-block;width:50%"><div id="ab" style="width:100%">alpha</div><div id="ac">beta</div></div>
		<div id="b" style="display:inline-block;width:50%"><div id="bb">gamma</div></div>
	</div>
</body></html>`)
	ly := BuildLayout(doc, nil, 40)
	byID := func(id string) *Node {
		var out *Node
		doc.FindAll(func(n *Node) bool {
			if n.Attr("id") == id {
				out = n
				return true
			}
			return false
		})
		return out
	}
	for _, id := range []string{"ab", "ac", "bb"} {
		n := byID(id)
		if n == nil {
			t.Fatalf("missing #%s", id)
		}
		if box := ly.Boxes[n]; box.W <= 0 || box.H <= 0 {
			t.Fatalf("#%s has no block box: %+v", id, box)
		}
	}
	ab, ac, bb := ly.Boxes[byID("ab")], ly.Boxes[byID("ac")], ly.Boxes[byID("bb")]
	if ac.Y <= ab.Y {
		t.Fatalf("block children of an inline-block flex item were flattened inline: ab=%+v ac=%+v", ab, ac)
	}
	if bb.X <= ab.X {
		t.Fatalf("second flex item is not to the right of the first: ab=%+v bb=%+v", ab, bb)
	}
}

func TestSearchInputFlexFullWidth(t *testing.T) {
	ly := build(`<form style="display:flex;gap:8px"><input type="search" name="q" placeholder="Search or enter address" style="flex:1"><button type="submit">Go</button></form>`, 40)
	inputY, inputW, buttonY := -1, 0, -1
	for _, f := range ly.Frags {
		if f.Kind != FragText {
			continue
		}
		if strings.Contains(f.Text, "Search or enter address") {
			inputY, inputW = f.Y, f.W
		}
		if strings.Contains(f.Text, "Go") {
			buttonY = f.Y
		}
	}
	if inputY < 0 || buttonY < 0 {
		t.Fatal("search form controls were not laid out")
	}
	if inputY != buttonY {
		t.Fatalf("input Y=%d, button Y=%d; want same line", inputY, buttonY)
	}
	if inputW < 30 {
		t.Fatalf("search input width = %d, want it to fill the flex row", inputW)
	}
}

func TestFlexPercentageWidthNotDoubleResolved(t *testing.T) {
	doc := ParseHTML(`<html><body><div style="display:flex">
		<div style="width:50%"><h1 style="font-size:48px">LinkPreview API</h1></div>
		<div style="width:50%">side</div>
	</div></body></html>`)
	ly := BuildLayoutBig(doc, nil, 100, true)
	ys := map[string]int{}
	for _, f := range ly.Frags {
		if f.Kind == FragText && (f.Text == "LinkPreview" || f.Text == "API") {
			ys[f.Text] = f.Y
		}
	}
	if ys["LinkPreview"] == 0 || ys["API"] == 0 {
		t.Fatalf("heading fragments = %v", ys)
	}
	if ys["LinkPreview"] != ys["API"] {
		t.Fatalf("heading wrapped at width 100: %v", ys)
	}
}

func TestFlexTextareaRendersWidget(t *testing.T) {
	ly := build(`<div style="display:flex"><textarea name="q"></textarea></div>`, 40)
	found := false
	for _, f := range ly.Frags {
		if f.Kind == FragText && strings.Contains(f.Text, "[") && f.W > 2 {
			found = true
		}
	}
	if !found {
		t.Fatal("textarea widget was not rendered inside flex layout")
	}
}

func TestFlexWrapToleratesCellRounding(t *testing.T) {
	// 470px + 470px + a 20px gap is exactly 960px, but each length rounds to
	// whole cells on its own (59 + 59 + 3 = 121 in a 120-cell row). The row
	// must keep both columns side by side instead of wrapping the second one.
	ly := build(`<html><body style="margin:0">
<div style="display:flex;flex-wrap:wrap;width:960px;gap:20px">
<div style="width:470px">alpha</div>
<div style="width:470px">beta</div>
</div></body></html>`, 120)
	pos := map[string]Frag{}
	for _, f := range ly.Frags {
		if f.Kind == FragText && (f.Text == "alpha" || f.Text == "beta") {
			pos[f.Text] = f
		}
	}
	a, okA := pos["alpha"]
	b, okB := pos["beta"]
	if !okA || !okB {
		t.Fatal("missing flex items")
	}
	if a.Y != b.Y {
		t.Fatalf("rounded row wrapped: alpha y=%d beta y=%d", a.Y, b.Y)
	}
	if b.X <= a.X+a.W {
		t.Fatalf("columns overlap: alpha x=%d w=%d beta x=%d", a.X, a.W, b.X)
	}
}

func TestFlexPercentageMarginUsesContainerWidth(t *testing.T) {
	// A flex item's percentage margin resolves against the flex container,
	// not against the item's own assigned width. Bootstrap/Bulma offset
	// columns rely on this: a quarter-width offset must move the item a
	// quarter of the row, leaving a visible gutter after the previous column.
	ly := build(`<html><body style="margin:0">
<div style="display:flex;width:640px">
<div style="width:50%">left</div>
<div style="width:25%;margin-left:25%">right</div>
</div></body></html>`, 80)
	x := -1
	for _, f := range ly.Frags {
		if f.Kind == FragText && f.Text == "right" {
			x = f.X
		}
	}
	if x < 0 {
		t.Fatal("offset item missing")
	}
	if x < 60 {
		t.Fatalf("offset item x=%d, want it past the 60-cell offset", x)
	}
}

func TestFlexRow(t *testing.T) {
	ly := build(`<div style="display:flex"><div style="flex:1">a</div><div style="flex:1">b</div></div>`, 40)
	pos := map[string]int{}
	for _, f := range ly.Frags {
		if f.Kind == FragText && (f.Text == "a" || f.Text == "b") {
			pos[f.Text] = f.X
		}
	}
	if _, ok := pos["a"]; !ok {
		t.Fatal("first flex item missing")
	}
	if _, ok := pos["b"]; !ok {
		t.Fatal("second flex item missing")
	}
	if pos["a"] >= pos["b"] {
		t.Fatalf("flex items not side by side: a=%d b=%d", pos["a"], pos["b"])
	}
}

func TestFlexBasisCalcConstrainsText(t *testing.T) {
	text := strings.Repeat("alpha beta gamma delta ", 30)
	ly := build(`<html><body>
<div style="display:flex;width:720px">
  <div id="main" style="flex:0 0 calc(100% - 312px)"><p>`+text+`</p></div>
  <div style="flex:0 0 300px">side</div>
</div></body></html>`, 100)
	maxRight := 0
	lines := 0
	for _, f := range ly.Frags {
		if f.Kind != FragText {
			continue
		}
		inside := false
		for p := f.Node; p != nil; p = p.Parent {
			if p.Attr("id") == "main" {
				inside = true
				break
			}
		}
		if !inside {
			continue
		}
		lines++
		if f.X+f.W > maxRight {
			maxRight = f.X + f.W
		}
	}
	if lines < 5 {
		t.Fatalf("main text produced %d lines, want wrapping", lines)
	}
	if maxRight > 70 {
		t.Fatalf("main text reaches column %d, want it constrained to the calc width", maxRight)
	}
}

func TestFlexItemCalcWidthUsesContainer(t *testing.T) {
	text := strings.Repeat("word ", 30)
	ly := build(`<html><body>
<div style="display:flex">
  <div id="left" style="width:calc(50% - 8px)"><p>`+text+`</p></div>
  <div style="width:calc(50% - 8px)">right</div>
</div></body></html>`, 100)
	lines := map[int]bool{}
	for _, f := range ly.Frags {
		if f.Kind != FragText {
			continue
		}
		for p := f.Node; p != nil; p = p.Parent {
			if p.Attr("id") == "left" {
				lines[f.Y] = true
				break
			}
		}
	}
	if len(lines) > 6 {
		t.Fatalf("calc flex item wrapped to %d lines, want about half the container width", len(lines))
	}
}

func TestDisplayContentsExpandsInFlex(t *testing.T) {
	doc := ParseHTML(`<html><body>
<div style="display:flex">
  <div style="display:contents">
    <span id="a" style="flex:0 0 50%">A</span>
    <span id="b" style="flex:0 0 50%">B</span>
  </div>
</div></body></html>`)
	ly := BuildLayout(doc, nil, 40)
	ax, bx := -1, -1
	for _, f := range ly.Frags {
		if f.Kind == FragText && f.Text == "A" {
			ax = f.X
		}
		if f.Kind == FragText && f.Text == "B" {
			bx = f.X
		}
	}
	if ax < 0 || bx < 0 || bx <= ax {
		t.Fatalf("contents children did not become flex items: A=%d B=%d", ax, bx)
	}
}

func TestFlexAlignShiftsOnlyOwnItem(t *testing.T) {
	ly := build(`<div style="display:flex;align-items:center"><div id="a" style="height:3em">A</div><span id="b">B</span></div><div id="c">C</div>`, 40)
	pos := map[string]Frag{}
	for _, f := range ly.Frags {
		if f.Kind == FragText {
			if id := fragOwnerID(f); id == "a" || id == "b" || id == "c" {
				pos[id] = f
			}
		}
	}
	if _, ok := pos["a"]; !ok {
		t.Fatalf("missing flex fragments: %+v", pos)
	}
	if _, ok := pos["b"]; !ok {
		t.Fatalf("missing flex fragments: %+v", pos)
	}
	if _, ok := pos["c"]; !ok {
		t.Fatalf("missing flex fragments: %+v", pos)
	}
	if pos["b"].Y != pos["a"].Y+1 {
		t.Fatalf("centered item y=%d, want %d", pos["b"].Y, pos["a"].Y+1)
	}
	if pos["c"].Y != pos["a"].Y+3 {
		t.Fatalf("content after flex row shifted: y=%d, want %d", pos["c"].Y, pos["a"].Y+3)
	}
}

func TestFlexOrderAndJustify(t *testing.T) {
	doc := ParseHTML(`<html><body>
<div style="display:flex">
  <span style="order:2">A</span>
  <span style="order:0">B</span>
  <span style="order:1">C</span>
</div>
<div style="display:flex;justify-content:flex-end">
  <div style="width:10px">D</div>
  <div style="width:10px">E</div>
</div></body></html>`)
	ly := BuildLayout(doc, nil, 40)
	pos := map[string][2]int{}
	for _, f := range ly.Frags {
		if f.Kind == FragText && len(f.Text) == 1 {
			pos[f.Text] = [2]int{f.X, f.Y}
		}
	}
	if !(pos["B"][0] < pos["C"][0] && pos["C"][0] < pos["A"][0]) {
		t.Fatalf("order not applied: %+v", pos)
	}
	if pos["D"][0] < 20 {
		t.Fatalf("justify-content:flex-end did not move items right: %+v", pos)
	}
}

func TestInlineListInFlexHeaderStaysOnOneLine(t *testing.T) {
	ly := build(`<div style="display:flex;justify-content:space-between;width:400px">`+
		`<p style="margin:0">Logo</p>`+
		`<ul style="padding:0;margin:0;list-style:none"><li style="display:inline"><a>Products</a></li> `+
		`<li style="display:inline"><a>Contact</a></li></ul></div>`, 50)
	rows := map[string]int{}
	for _, f := range ly.Frags {
		if f.Kind != FragText {
			continue
		}
		switch strings.TrimSpace(f.Text) {
		case "Products":
			rows["Products"] = f.Y
		case "Contact":
			rows["Contact"] = f.Y
		}
	}
	if len(rows) != 2 {
		t.Fatalf("menu items not rendered: %v", rows)
	}
	if rows["Products"] != rows["Contact"] {
		t.Fatalf("menu wrapped: Products y=%d Contact y=%d", rows["Products"], rows["Contact"])
	}
}

func TestFlexPercentageItemsKeepAssignedSlots(t *testing.T) {
	doc := ParseHTML(`<div style="display:flex;width:400px">` +
		`<div id="s" style="width:25%;padding:0 24px;box-sizing:border-box;border-right:1px solid #ccc">Side</div>` +
		`<div id="m" style="width:75%;padding:0 24px;box-sizing:border-box">Main</div></div>`)
	ly := BuildLayout(doc, nil, 100)
	var side, main *Node
	doc.FindAll(func(n *Node) bool {
		switch n.Attr("id") {
		case "s":
			side = n
		case "m":
			main = n
		}
		return false
	})
	if side == nil || main == nil {
		t.Fatal("flex items missing")
	}
	rs, rm := ly.Boxes[side], ly.Boxes[main]
	if rm.X < rs.X+rs.W {
		t.Fatalf("main starts at %d before the sidebar ends at %d: %+v %+v", rm.X, rs.X+rs.W, rs, rm)
	}
	// Percentage rounding can add a cell or two; the important property is
	// that the columns keep their own slots.
	if rs.W+rm.W > 52 {
		t.Fatalf("flex items overflow their 50-cell row: %+v %+v", rs, rm)
	}
}

func TestFlexAutoMarginPushesItemRight(t *testing.T) {
	ly := build(`<div style="display:flex;width:400px"><span>Brand</span>`+
		`<div style="margin-left:auto">Right</div></div>`, 100)
	for _, f := range ly.Frags {
		if f.Kind == FragText && strings.Contains(f.Text, "Right") {
			if f.X < 40 {
				t.Fatalf("auto-margin item x = %d, want near the right edge", f.X)
			}
			return
		}
	}
	t.Fatal("auto-margin item text missing")
}

func TestDisplayContentsChildrenSumInFlexItemWidth(t *testing.T) {
	doc := ParseHTML(`<div style="display:flex;justify-content:space-between;width:600px">
		<div id="left" style="display:flex;flex:none"><div style="display:contents">
			<span>US</span><span>World</span><span>Politics</span></div></div>
		<div id="right">Sign</div></div>`)
	ly := BuildLayout(doc, nil, 75)
	left := ly.Boxes[findElementByID(doc, "left")]
	right := ly.Boxes[findElementByID(doc, "right")]
	// The contents wrapper's children join the flex row, so their widths add
	// up (US + World + Politics = 15 cells plus spacing) instead of the item
	// collapsing to the widest child.
	if left.W < 15 {
		t.Fatalf("contents flex item width = %d, want at least the 15 cells of its links", left.W)
	}
	if right.X < left.X+left.W {
		t.Fatalf("right item at x=%d overlaps the %d-wide contents item at x=%d", right.X, left.W, left.X)
	}
}

func TestFlexBlockifiesInlineItems(t *testing.T) {
	// Flex items are blockified: an inline anchor becomes a block container,
	// so its width and block children apply. Without this a card whose markup
	// is an inline anchor collapsed to zero width.
	doc := ParseHTML(`<div style="display:flex"><a id="c" style="display:inline;width:25%"><div id="in">Card text</div></a></div>`)
	ly := BuildLayout(doc, nil, 80)
	card := ly.Boxes[findElementByID(doc, "c")]
	inner := ly.Boxes[findElementByID(doc, "in")]
	if card.W < 15 {
		t.Fatalf("inline flex item width = %d, want the 20-cell 25%% slot", card.W)
	}
	if inner.W < 15 {
		t.Fatalf("block child width = %d, want it to fill the item", inner.W)
	}
}

func TestFlexItemRespectsMaxWidth(t *testing.T) {
	// A descendant with a fixed pixel width (a carousel stage, a wide table)
	// must not inflate a flex item past its max-width. Bootstrap columns rely
	// on `flex:0 0 75%;max-width:75%`; without the clamp one wide stage
	// stretched the content column to 710 cells and pushed the sidebar off.
	doc := ParseHTML(`<div style="display:flex;flex-wrap:wrap">` +
		`<div id="main" style="flex:0 0 75%;max-width:75%"><div style="width:640px">wide stage</div></div>` +
		`<div id="side">sidebar</div></div>`)
	ly := BuildLayout(doc, nil, 80)
	main := ly.Boxes[findElementByID(doc, "main")]
	side := ly.Boxes[findElementByID(doc, "side")]
	if main.W > 60 {
		t.Fatalf("main column width = %d, want it clamped near 75%% of the row", main.W)
	}
	if side.X < main.X+main.W-1 {
		t.Fatalf("sidebar at x=%d overlaps the main column ending at %d", side.X, main.X+main.W)
	}
}

func TestFlexGrowFillsSpaceLeftByMaxWidth(t *testing.T) {
	// A `flex:0 0 100%` column capped by max-width must not consume the row's
	// free space: the flex:1 sibling grows into it. Site headers rely on this
	// (a fixed 312px rail beside a flex:1 content column); leaving the space
	// unclaimed narrowed the content column and made every card taller.
	doc := ParseHTML(`<div style="display:flex;width:160px">` +
		`<div id="main" style="flex:1">content</div>` +
		`<div id="side" style="flex:0 0 100%;max-width:40px">side</div>` +
		`</div>`)
	ly := BuildLayout(doc, nil, 177)
	main := ly.Boxes[findElementByID(doc, "main")]
	side := ly.Boxes[findElementByID(doc, "side")]
	if side.W != 5 {
		t.Fatalf("side width = %d, want the 40px max-width", side.W)
	}
	if main.W != 15 {
		t.Fatalf("main width = %d, want it to fill the remaining 15 cells", main.W)
	}
	if side.X != main.X+main.W {
		t.Fatalf("side at x=%d, want it right after the main column ending at %d", side.X, main.X+main.W)
	}
}

func TestFlexGrowSharesFreeSpaceEvenly(t *testing.T) {
	// Four `flex:1 1 auto` items must each get the same share. Subtracting
	// every item's share from the free-space total inside the loop gave the
	// first item the most and the last the least (e.g. 53/40/29/22 cells).
	widths := []int{0, 0, 0, 0}
	distributeGrow(widths, []float64{1, 1, 1, 1}, []int{0, 0, 0, 0}, 144)
	for i, w := range widths {
		if w != 36 {
			t.Fatalf("item %d width = %d, want 36 (all shares: %v)", i, w, widths)
		}
	}
}

func TestFlexGrowRedistributesPastMaxWidth(t *testing.T) {
	widths := []int{0, 0}
	// The first item may only take 10 cells; the rest goes to its sibling.
	distributeGrow(widths, []float64{1, 1}, []int{10, 0}, 40)
	if widths[0] != 10 || widths[1] != 30 {
		t.Fatalf("widths = %v, want [10 30]", widths)
	}
}

func TestFlexEqualGrowColumnsSameWidth(t *testing.T) {
	doc := ParseHTML(`<div style="display:flex;flex-flow:row wrap;justify-content:space-between;width:640px">` +
		`<div id="a" style="flex:1 1 auto;margin:4px">Alpha</div>` +
		`<div id="b" style="flex:1 1 auto;margin:4px">Bravo</div>` +
		`<div id="c" style="flex:1 1 auto;margin:4px">Chair</div>` +
		`<div id="d" style="flex:1 1 auto;margin:4px">Delta</div>` +
		`</div>`)
	ly := BuildLayout(doc, nil, 100)
	a := ly.Boxes[findElementByID(doc, "a")]
	b := ly.Boxes[findElementByID(doc, "b")]
	c := ly.Boxes[findElementByID(doc, "c")]
	d := ly.Boxes[findElementByID(doc, "d")]
	if a.W != b.W || b.W != c.W || c.W != d.W {
		t.Fatalf("plan widths = %d/%d/%d/%d, want equal columns", a.W, b.W, c.W, d.W)
	}
	if !(a.X < b.X && b.X < c.X && c.X < d.X) {
		t.Fatalf("columns not placed left to right: %d %d %d %d", a.X, b.X, c.X, d.X)
	}
}

func TestFlexGrowRemainderSpreadAcrossItems(t *testing.T) {
	// Fewer free cells than growing items used to give the whole remainder to
	// the first item. Spread it one cell per item instead.
	widths := make([]int, 20)
	grow := make([]float64, 20)
	maxTotals := make([]int, 20)
	for i := range grow {
		grow[i] = 1
	}
	distributeGrow(widths, grow, maxTotals, 19)
	for i, w := range widths {
		want := 1
		if i == len(widths)-1 {
			want = 0
		}
		if w != want {
			t.Fatalf("widths = %v, want 19 items with one cell each", widths)
		}
	}
}

func TestFlexPseudoAspectBoxGivesHeight(t *testing.T) {
	// The responsive-media pattern puts an empty ::after with a percentage
	// padding-bottom inside a flex box to give an absolutely positioned image
	// a height. As a flex item it must produce a box, or the container is
	// zero-height and the image covers the following text.
	doc := ParseHTML(`<html><body style="margin:0">
<div id="box" style="display:flex;flex-direction:column"><div id="inner" style="width:160px"></div></div>
<p id="after">following text</p>
</body></html>`)
	rules := ParseStylesheet(`#inner::after{content:"";display:block;padding-bottom:56.25%}`)
	ly := BuildLayout(doc, rules, 40)
	var after *Frag
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind == FragText && f.Text == "following" {
			after = f
		}
	}
	if after == nil {
		t.Fatal("following text missing")
	}
	// The aspect box is 160px wide (20 cells) and 56.25% tall (90px, about 6
	// rows at 16px per row); the following text must start below it.
	if after.Y < 5 {
		t.Fatalf("following text y=%d, want it below the aspect box", after.Y)
	}
}

func TestFlexColumnBlockifiesInlineChildren(t *testing.T) {
	ly := build(`<html><body style="margin:0">
<div style="display:flex;flex-direction:column">
<span id="pic" style="width:160px;aspect-ratio:16/9"></span>
</div>
<p id="after">caption</p>
</body></html>`, 40)
	var pic *Node
	for n := range ly.Styles {
		if n.Attr("id") == "pic" {
			pic = n
		}
	}
	if pic == nil {
		t.Fatal("picture-like child missing")
	}
	r := ly.Boxes[pic]
	if r.W < 10 || r.H < 2 {
		t.Fatalf("column flex child box = %+v, want a blockified aspect-ratio box", r)
	}
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind == FragText && f.Text == "caption" {
			if f.Y < r.Y+r.H {
				t.Fatalf("caption y=%d overlaps column child bottom %d", f.Y, r.Y+r.H)
			}
			return
		}
	}
	t.Fatal("caption missing")
}
