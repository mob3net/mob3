package browser

import (
	"strings"
	"testing"
)

func TestFlexWrapStartsNewLine(t *testing.T) {
	ly := build(`<div style="display:flex;flex-wrap:wrap"><div style="width:100%">alpha</div><div style="width:100%">beta</div></div>`, 40)
	var alpha, beta *Frag
	for i := range ly.Frags {
		if ly.Frags[i].Kind != fragText {
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

func TestSearchInputFlexFullWidth(t *testing.T) {
	ly := build(`<form style="display:flex;gap:8px"><input type="search" name="q" placeholder="Search or enter address" style="flex:1"><button type="submit">Go</button></form>`, 40)
	inputY, inputW, buttonY := -1, 0, -1
	for _, f := range ly.Frags {
		if f.Kind != fragText {
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
		if f.Kind == fragText && (f.Text == "LinkPreview" || f.Text == "API") {
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
		if f.Kind == fragText && strings.Contains(f.Text, "[") && f.W > 2 {
			found = true
		}
	}
	if !found {
		t.Fatal("textarea widget was not rendered inside flex layout")
	}
}

func TestFlexRow(t *testing.T) {
	ly := build(`<div style="display:flex"><div style="flex:1">a</div><div style="flex:1">b</div></div>`, 40)
	pos := map[string]int{}
	for _, f := range ly.Frags {
		if f.Kind == fragText && (f.Text == "a" || f.Text == "b") {
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
		if f.Kind != fragText {
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
		if f.Kind != fragText {
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
		if f.Kind == fragText && f.Text == "A" {
			ax = f.X
		}
		if f.Kind == fragText && f.Text == "B" {
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
		if f.Kind == fragText {
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
		if f.Kind == fragText && len(f.Text) == 1 {
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
		if f.Kind != fragText {
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
		if f.Kind == fragText && strings.Contains(f.Text, "Right") {
			if f.X < 40 {
				t.Fatalf("auto-margin item x = %d, want near the right edge", f.X)
			}
			return
		}
	}
	t.Fatal("auto-margin item text missing")
}
