package engine

import (
	"strings"
	"testing"
)

func TestClosedDetailsHidesContent(t *testing.T) {
	closed := build(`<details><summary>summary</summary><p>secret</p></details>`, 40)
	for _, f := range closed.Frags {
		if f.Kind == FragText && strings.Contains(f.Text, "secret") {
			t.Fatal("closed details rendered its content")
		}
	}
	open := build(`<details open><summary>summary</summary><p>visible</p></details>`, 40)
	found := false
	for _, f := range open.Frags {
		if f.Kind == FragText && strings.Contains(f.Text, "visible") {
			found = true
		}
	}
	if !found {
		t.Fatal("open details did not render its content")
	}
}

func TestListMarkerBeforeText(t *testing.T) {
	ly := build(`<ul><li>First item</li></ul>`, 30)
	markerX, textX := -1, -1
	for _, f := range ly.Frags {
		if f.Kind != FragText {
			continue
		}
		if strings.TrimSpace(f.Text) == "•" {
			markerX = f.X
		}
		if f.Text == "First" {
			textX = f.X
		}
	}
	if markerX < 0 || textX < 0 {
		t.Fatalf("marker=%d text=%d", markerX, textX)
	}
	if markerX >= textX {
		t.Fatalf("marker at %d should be left of text at %d", markerX, textX)
	}
}

func TestPseudoAfterAspectBox(t *testing.T) {
	rules := ParseStylesheet(`.box:after { content:""; display:block; padding-top:50%; }`)
	doc := ParseHTML(`<html><body><div class="box" style="width:100%"></div><p id="after">after</p></body></html>`)
	ly := BuildLayout(doc, rules, 40)
	afterY := 0
	for _, f := range ly.Frags {
		if f.Kind == FragText && f.Text == "after" {
			afterY = f.Y
		}
	}
	if afterY < 8 {
		t.Fatalf("paragraph after aspect box is at row %d, want the generated box to add height", afterY)
	}
}

func TestFloatLeftLaysItemsHorizontally(t *testing.T) {
	doc := ParseHTML(`<html><body>
<ul style="display:table">
  <li style="float:left">A</li>
  <li style="float:left">B</li>
  <li style="float:left">C</li>
</ul></body></html>`)
	ly := BuildLayout(doc, nil, 40)
	pos := map[string][2]int{}
	for _, f := range ly.Frags {
		if f.Kind == FragText && (f.Text == "A" || f.Text == "B" || f.Text == "C") {
			pos[f.Text] = [2]int{f.X, f.Y}
		}
	}
	if pos["A"][1] != pos["B"][1] || pos["B"][1] != pos["C"][1] || pos["B"][0] <= pos["A"][0] || pos["C"][0] <= pos["B"][0] {
		t.Fatalf("floated items are not horizontal: %+v", pos)
	}
}

func TestClipHiddenContent(t *testing.T) {
	doc := ParseHTML(`<html><body>
<div style="clip:rect(1px,1px,1px,1px)">clipped</div>
<p id="shown">shown</p></body></html>`)
	ly := BuildLayout(doc, nil, 40)
	seen := map[string]bool{}
	for _, f := range ly.Frags {
		if f.Kind == FragText {
			seen[f.Text] = true
		}
	}
	if seen["clipped"] {
		t.Fatalf("clipped content rendered: %+v", seen)
	}
	if !seen["shown"] {
		t.Fatal("visible content was dropped")
	}
}

func TestOverflowClippingStopsSpill(t *testing.T) {
	ly := build(`<html><body>
<div id="box" style="max-height:2em;overflow:hidden">
  <div>one</div><div>two</div><div>three</div><div>four</div>
</div>
<p id="after">after</p></body></html>`, 40)
	seen := map[string]Frag{}
	for _, f := range ly.Frags {
		if f.Kind == FragText {
			if s := strings.TrimSpace(f.Text); s != "" {
				seen[s] = f
			}
		}
	}
	if _, ok := seen["three"]; ok {
		t.Fatal("overflowing line three was not clipped")
	}
	if _, ok := seen["four"]; ok {
		t.Fatal("overflowing line four was not clipped")
	}
	two, ok := seen["two"]
	if !ok {
		t.Fatal("visible line two missing")
	}
	after, ok := seen["after"]
	if !ok {
		t.Fatal("content after the clipped box missing")
	}
	if after.Y <= two.Y {
		t.Fatalf("content after the clipped box overlaps it: two y=%d after y=%d", two.Y, after.Y)
	}
}

func TestOverflowClipsHorizontalRun(t *testing.T) {
	ly := build(`<html><body><div style="width:2em;overflow:hidden;white-space:nowrap">abcdefgh</div></body></html>`, 40)
	for _, f := range ly.Frags {
		if f.Kind == FragText && strings.Contains(f.Text, "abcdefgh") {
			t.Fatalf("nowrap run was not clipped: %+v", f)
		}
	}
}

func TestFloatLeftWrapsText(t *testing.T) {
	ly := build(`<html><body>
<div id="f" style="float:left;width:10em;height:3em;background:#eee">F</div>
<p id="p">one two three four five six seven eight nine ten eleven twelve thirteen fourteen</p>
</body></html>`, 40)
	var narrowed, full bool
	for _, f := range ly.Frags {
		if f.Kind != FragText || fragOwnerID(f) != "p" || strings.TrimSpace(f.Text) == "" {
			continue
		}
		if f.Y < 4 {
			if f.X < 21 {
				t.Fatalf("line beside the float starts inside it: x=%d %q", f.X, f.Text)
			}
			narrowed = true
		} else if f.X <= 1 {
			full = true
		}
	}
	if !narrowed || !full {
		t.Fatalf("text did not wrap beside and below the float: narrowed=%v full=%v", narrowed, full)
	}
}

func TestFloatRightNarrowsLines(t *testing.T) {
	ly := build(`<html><body>
<div id="f" style="float:right;width:10em;height:3em">F</div>
<p id="p">one two three four five six seven eight nine ten eleven twelve thirteen fourteen</p>
</body></html>`, 40)
	found := false
	for _, f := range ly.Frags {
		if f.Kind != FragText || fragOwnerID(f) != "p" || strings.TrimSpace(f.Text) == "" {
			continue
		}
		if f.Y < 4 {
			if f.X+f.W > 19 {
				t.Fatalf("line beside the right float overlaps it: x=%d w=%d %q", f.X, f.W, f.Text)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no text lines were laid out beside the right float")
	}
}

func TestClearMovesBelowFloat(t *testing.T) {
	ly := build(`<html><body>
<div style="float:left;width:5em;height:3em">F</div>
<p id="clear" style="clear:left">after</p>
</body></html>`, 40)
	for _, f := range ly.Frags {
		if f.Kind == FragText && fragOwnerID(f) == "clear" {
			if f.X > 1 || f.Y < 4 {
				t.Fatalf("cleared block did not move below the float: x=%d y=%d %q", f.X, f.Y, f.Text)
			}
			return
		}
	}
	t.Fatal("cleared block text missing")
}

func TestFloatContainedByOverflow(t *testing.T) {
	ly := build(`<html><body>
<div style="overflow:hidden"><div style="float:left;width:5em;height:2em">F</div></div>
<p id="after">after</p></body></html>`, 40)
	for _, f := range ly.Frags {
		if f.Kind == FragText && fragOwnerID(f) == "after" {
			if f.X > 1 {
				t.Fatalf("float leaked out of its formatting context: x=%d", f.X)
			}
			return
		}
	}
	t.Fatal("text after the float container missing")
}

func TestFloatTableCaptionDoesNotInflateWidth(t *testing.T) {
	ly := build(`<html><body>
<figure style="display:table;float:left"><div style="width:5em;height:2em">F</div>
<figcaption>a very long figcaption that must wrap to the table width instead of widening it</figcaption></figure>
<p id="p">one two three four five six seven eight nine ten eleven twelve</p></body></html>`, 60)
	for _, f := range ly.Frags {
		if f.Kind == FragText && fragOwnerID(f) == "p" && strings.TrimSpace(f.Text) != "" && f.Y < 4 {
			if f.X > 15 {
				t.Fatalf("figcaption inflated the float width: text x=%d %q", f.X, f.Text)
			}
			return
		}
	}
	t.Fatal("no text beside the floated figure")
}

func TestFloatWidthResolvedOnce(t *testing.T) {
	doc := ParseHTML(`<div style="width:400px"><div id="f" style="float:left;width:50%;height:16px"></div></div>`)
	ly := BuildLayout(doc, nil, 100)
	var f *Node
	doc.FindAll(func(n *Node) bool {
		if n.Attr("id") == "f" {
			f = n
		}
		return false
	})
	if f == nil {
		t.Fatal("float missing")
	}
	if box := ly.Boxes[f]; box.W != 25 {
		t.Fatalf("float width = %d, want 25 (50%% of 50, resolved once)", box.W)
	}
}

func TestFloatDeclaredWidthCapsMinContent(t *testing.T) {
	doc := ParseHTML(`<div style="width:160px"><div id="f" style="float:left;width:16px">abcdefghijklmnopqrstuvwxyz</div></div>`)
	ly := BuildLayout(doc, nil, 100)
	var f *Node
	doc.FindAll(func(n *Node) bool {
		if n.Attr("id") == "f" {
			f = n
		}
		return false
	})
	if f == nil {
		t.Fatal("float missing")
	}
	if box := ly.Boxes[f]; box.W != 2 {
		t.Fatalf("float width = %d, want 2 despite a longer word", box.W)
	}
}

func TestFloatRowMaxContentIsSum(t *testing.T) {
	doc := ParseHTML(`<div style="width:400px"><div id="nav" style="float:left"><span style="float:left">aaaa</span> <span style="float:left">bbbb</span></div></div>`)
	ly := BuildLayout(doc, nil, 50)
	var nav *Node
	doc.FindAll(func(n *Node) bool {
		if n.Attr("id") == "nav" {
			nav = n
		}
		return false
	})
	if nav == nil {
		t.Fatal("nav missing")
	}
	if box := ly.Boxes[nav]; box.W != 8 {
		t.Fatalf("float container width = %d, want 8 (sum of both floats)", box.W)
	}
	var rows []int
	for _, f := range ly.Frags {
		if f.Kind == FragText && (strings.TrimSpace(f.Text) == "aaaa" || strings.TrimSpace(f.Text) == "bbbb") {
			rows = append(rows, f.Y)
		}
	}
	if len(rows) != 2 || rows[0] != rows[1] {
		t.Fatalf("float rows = %v, want both on one row", rows)
	}
}
