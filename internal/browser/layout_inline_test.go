package browser

import (
	"strings"
	"testing"
)

func TestTextAlignJustifyExpandsSpaces(t *testing.T) {
	src := `<p style="text-align:justify">one two three four five six seven eight nine ten</p>`
	ly := build(src, 24)
	firstY := -1
	for _, f := range ly.Frags {
		if f.Kind == fragText && f.Text != "" {
			firstY = f.Y
			break
		}
	}
	if firstY < 0 {
		t.Fatal("no text laid out")
	}
	justified := false
	for _, f := range ly.Frags {
		if f.Y == firstY && f.Kind == fragText && strings.TrimSpace(f.Text) == "" && f.W > 1 {
			justified = true
		}
	}
	if !justified {
		t.Fatal("justified line did not expand inter-word spaces")
	}
	plain := build(`<p>one two three four five six seven eight nine ten</p>`, 24)
	firstY = -1
	for _, f := range plain.Frags {
		if f.Kind == fragText && f.Text != "" {
			firstY = f.Y
			break
		}
	}
	for _, f := range plain.Frags {
		if f.Y == firstY && f.Kind == fragText && strings.TrimSpace(f.Text) == "" && f.W > 1 {
			t.Fatal("plain text spaces were expanded")
		}
	}
}

func TestInlineBlockWrapperWithWidthKeepsBlocks(t *testing.T) {
	ly := build(`<div>before <span style="display:inline-block;width:100%"><div>inside</div></span>after</div>`, 40)
	var before, inside, after *Frag
	for i := range ly.Frags {
		if ly.Frags[i].Kind != fragText {
			continue
		}
		switch ly.Frags[i].Text {
		case "before":
			before = &ly.Frags[i]
		case "inside":
			inside = &ly.Frags[i]
		case "after":
			after = &ly.Frags[i]
		}
	}
	if before == nil || inside == nil || after == nil {
		t.Fatal("missing inline-block content")
	}
	if inside.Y <= before.Y || after.Y <= inside.Y {
		t.Fatalf("inline-block wrapper did not lay out its block: before y=%d inside y=%d after y=%d", before.Y, inside.Y, after.Y)
	}
}

func TestWideRuneClippedAtRightEdge(t *testing.T) {
	g := NewGrid(5, 1, RGB(0, 0, 0), RGB(255, 255, 255))
	g.Put(3, 0, '界', nil, RGB(0, 0, 0), Color{})
	if g.Cells[3].R != '界' || !g.Cells[4].Cont {
		t.Fatalf("wide rune did not fit normally: %+v %+v", g.Cells[3], g.Cells[4])
	}
	g2 := NewGrid(5, 1, RGB(0, 0, 0), RGB(255, 255, 255))
	g2.Put(4, 0, '界', nil, RGB(0, 0, 0), Color{})
	if g2.Cells[4].R == '界' || g2.Cells[4].Cont {
		t.Fatalf("wide rune overflowed the last column: %+v", g2.Cells[4])
	}
}

func TestInlineBackgroundPainted(t *testing.T) {
	ly := build(`<p><a style="display:inline-block;background:#1a73e8;color:#ffffff">Sign in</a></p>`, 40)
	g := NewGrid(40, 4, RGB(255, 255, 255), RGB(255, 255, 255))
	PaintLayout(g, ly, 0, 0, 4, nil, RGB(0, 0, 0))
	blue := RGB(0x1a, 0x73, 0xe8)
	found := false
	for _, c := range g.Cells {
		if c.R == 'S' {
			found = true
			if c.BG != blue {
				t.Fatalf("inline background = %+v, want %+v", c.BG, blue)
			}
			if c.FG.R != 255 || c.FG.G != 255 || c.FG.B != 255 {
				t.Fatalf("text color = %+v", c.FG)
			}
		}
	}
	if !found {
		t.Fatal("text was not painted")
	}
}

func TestBigTextLayout(t *testing.T) {
	doc := ParseHTML(`<h1>AB</h1><p>body</p>`)
	ly := BuildLayoutBig(doc, nil, 40, true)
	found := false
	for _, f := range ly.Frags {
		if f.Kind == fragText && f.Text == "AB" {
			found = true
			if f.W != 4 || f.H != 2 {
				t.Fatalf("big heading size = %dx%d, want 4x2", f.W, f.H)
			}
		}
	}
	if !found {
		t.Fatal("heading fragment not laid out")
	}
	plain := BuildLayout(doc, nil, 40)
	for _, f := range plain.Frags {
		if f.Kind == fragText && f.Text == "AB" && (f.W != 2 || f.H != 2) {
			t.Fatalf("plain heading size = %dx%d, want 2x2", f.W, f.H)
		}
	}
}

func TestTextWrapsInsideWidth(t *testing.T) {
	ly := build(`<div style="width:120px"><p>one two three four five six seven eight nine ten eleven twelve</p></div>`, 30)
	saw := false
	for _, f := range ly.Frags {
		if f.Kind != fragText {
			continue
		}
		saw = true
		if f.X < 0 || f.X+f.W > 30 {
			t.Fatalf("frag %q at x=%d w=%d exceeds width", f.Text, f.X, f.W)
		}
	}
	if !saw {
		t.Fatal("no text laid out")
	}
}

func TestInlineWrapperWithBlockDescendantStartsNewLine(t *testing.T) {
	ly := build(`<div><span style="display:inline"><div style="display:grid;grid-template-columns:1fr 1fr;gap:2px"><p>alpha</p><p>beta</p></div></span></div><p>gamma</p>`, 40)
	find := func(text string) *Frag {
		for i := range ly.Frags {
			if ly.Frags[i].Kind == fragText && ly.Frags[i].Text == text {
				return &ly.Frags[i]
			}
		}
		return nil
	}
	alpha, beta, gamma := find("alpha"), find("beta"), find("gamma")
	if alpha == nil || beta == nil || gamma == nil {
		t.Fatal("missing fragments")
	}
	if alpha.Y != beta.Y {
		t.Fatalf("grid columns on different rows: alpha y=%d beta y=%d", alpha.Y, beta.Y)
	}
	if alpha.X >= beta.X {
		t.Fatalf("grid columns not side by side: alpha x=%d beta x=%d", alpha.X, beta.X)
	}
	if gamma.Y <= alpha.Y {
		t.Fatalf("following block did not start below the inline wrapper: gamma y=%d alpha y=%d", gamma.Y, alpha.Y)
	}
}

func TestLineBreaks(t *testing.T) {
	ly := build(`<p>line one<br>line two<br><br>line four</p>`, 30)
	ys := map[string]int{}
	for _, f := range ly.Frags {
		if f.Kind == fragText {
			ys[strings.TrimSpace(f.Text)] = f.Y
		}
	}
	if ys["one"] != 1 || ys["two"] != 2 || ys["four"] != 4 {
		t.Fatalf("line positions = %v", ys)
	}
}

func TestNoWrap(t *testing.T) {
	ly := build(`<p style="white-space:nowrap">alpha beta gamma delta epsilon</p>`, 10)
	ys := map[int]bool{}
	seen := 0
	for _, f := range ly.Frags {
		if f.Kind == fragText && strings.TrimSpace(f.Text) != "" {
			ys[f.Y] = true
			seen++
		}
	}
	if seen < 3 {
		t.Fatalf("expected the words, saw %d fragments", seen)
	}
	if len(ys) != 1 {
		t.Fatalf("nowrap text used %d lines", len(ys))
	}
}

func TestInlineVerticalAlign(t *testing.T) {
	ly := build(`<p><span style="font-size:32px">X</span>`+
		`<span style="vertical-align:top">A</span>`+
		`<span style="vertical-align:bottom">B</span></p>`, 40)
	x := textFrag(ly, "X")
	a := textFrag(ly, "A")
	b := textFrag(ly, "B")
	if x == nil || a == nil || b == nil {
		t.Fatalf("missing fragments: X=%+v A=%+v B=%+v", x, a, b)
	}
	if a.Y != x.Y {
		t.Fatalf("top-aligned Y=%d, want line top %d", a.Y, x.Y)
	}
	if b.Y != x.Y+1 {
		t.Fatalf("bottom-aligned Y=%d, want %d", b.Y, x.Y+1)
	}
}

func TestLineClampLimitsLines(t *testing.T) {
	rules := ParseStylesheet(`.clamp { display:block; overflow:hidden; -webkit-line-clamp:2; line-height:1; }`)
	doc := ParseHTML(`<html><body><div id="clamp" class="clamp">` + strings.Repeat("word ", 40) + `</div><p id="after">after</p></body></html>`)
	ly := BuildLayout(doc, rules, 20)
	lines := map[int]bool{}
	for _, f := range ly.Frags {
		if f.Kind != fragText {
			continue
		}
		for p := f.Node; p != nil; p = p.Parent {
			if p.Attr("id") == "clamp" {
				lines[f.Y] = true
				break
			}
		}
	}
	if len(lines) > 2 {
		t.Fatalf("line clamp produced %d lines, want at most 2", len(lines))
	}
}

func TestInlinePaddingSeparatesLinks(t *testing.T) {
	ly := build(`<p><a href="#a" style="padding-right:3px">Vesti</a><a href="#b">Društvo</a></p>`, 40)
	var first, second *Frag
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind != fragText {
			continue
		}
		switch strings.TrimSpace(f.Text) {
		case "Vesti":
			first = f
		case "Društvo":
			second = f
		}
	}
	if first == nil || second == nil {
		t.Fatal("link text missing")
	}
	if second.X <= first.X+first.W {
		t.Fatalf("links are glued: first ends at %d, second starts at %d", first.X+first.W, second.X)
	}
}

func TestInlineFlexStaysInLine(t *testing.T) {
	ly := build(`<div><span style="display:inline-flex;padding:0 4px">Get Started</span>
<span style="display:inline-flex;padding:0 4px">Documentation</span></div>`, 60)
	var first, second *Frag
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind != fragText {
			continue
		}
		switch strings.TrimSpace(f.Text) {
		case "Get":
			first = f
		case "Documentation":
			second = f
		}
	}
	if first == nil || second == nil {
		t.Fatal("button text missing")
	}
	if first.Y != second.Y {
		t.Fatalf("inline-flex buttons stacked: y=%d and y=%d", first.Y, second.Y)
	}
}

func TestLeadingWhitespaceDoesNotWrapPaddedLinks(t *testing.T) {
	ly := build(`<div style="width:10em"><a href="#a" style="padding-left:1em">Products</a>
<a href="#b" style="padding-left:1em">Contact</a></div>`, 40)
	var first, second *Frag
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind != fragText {
			continue
		}
		switch strings.TrimSpace(f.Text) {
		case "Products":
			first = f
		case "Contact":
			second = f
		}
	}
	if first == nil || second == nil {
		t.Fatal("link text missing")
	}
	if first.Y != second.Y {
		t.Fatalf("padded links wrapped: Products y=%d Contact y=%d", first.Y, second.Y)
	}
}

func TestNowrapInlineElementsWrapBetweenBoxes(t *testing.T) {
	ly := build(`<div style="width:20ch"><a style="white-space:nowrap">aaaaaaaaaaaa</a><a style="white-space:nowrap">bbbbbbbbbbbb</a></div>`, 40)
	var a, b *Frag
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind == fragText && strings.HasPrefix(f.Text, "aaa") {
			a = f
		}
		if f.Kind == fragText && strings.HasPrefix(f.Text, "bbb") {
			b = f
		}
	}
	if a == nil || b == nil {
		t.Fatalf("links missing: a=%v b=%v", a != nil, b != nil)
	}
	if b.Y <= a.Y {
		t.Fatalf("second nowrap link did not wrap: %d -> %d", a.Y, b.Y)
	}
}

func TestLongNowrapInlineContentSplits(t *testing.T) {
	ly := build(`<div style="width:10ch"><a style="white-space:nowrap">abcdefghijklmno</a></div>`, 40)
	lines := map[int]bool{}
	for _, f := range ly.Frags {
		if f.Kind == fragText && f.Text != "" {
			lines[f.Y] = true
		}
	}
	if len(lines) < 2 {
		t.Fatalf("long nowrap inline content did not split: %d line(s)", len(lines))
	}
}

func TestInlineEdgesRoundTotalOnce(t *testing.T) {
	doc := ParseHTML(`<p>a<code style="margin:0 4px;border:1px solid;padding:4px">x</code>b</p>`)
	ly := BuildLayout(doc, nil, 40)
	pos := map[string]int{}
	for _, f := range ly.Frags {
		if f.Kind == fragText {
			switch strings.TrimSpace(f.Text) {
			case "x":
				pos["x"] = f.X
			case "b":
				pos["b"] = f.X
			}
		}
	}
	// 9px of edge per side is about one 8px cell, not one cell per edge.
	if pos["x"] != 2 || pos["b"] != 4 {
		t.Fatalf("inline code positions x=%d b=%d, want x=2 b=4", pos["x"], pos["b"])
	}

	// A single sub-cell edge still reserves one cell.
	tiny := build(`<p>a<span style="padding:1px">x</span>b</p>`, 40)
	pos = map[string]int{}
	for _, f := range tiny.Frags {
		if f.Kind == fragText {
			switch strings.TrimSpace(f.Text) {
			case "x":
				pos["x"] = f.X
			}
		}
	}
	if pos["x"] != 2 {
		t.Fatalf("1px padding inline x=%d, want 2 (1px rounds up to one cell)", pos["x"])
	}
}

func TestInlineSpacePrecedesCodeEdge(t *testing.T) {
	doc := ParseHTML(`<p>a <code style="background:#000;padding:0 4px">x</code> b</p>`)
	ly := BuildLayout(doc, nil, 40)
	type cell struct {
		text string
		bg   bool
	}
	cells := map[int]cell{}
	for _, f := range ly.Frags {
		if f.Kind != fragText {
			continue
		}
		bg := f.Style != nil && f.Style.Background.Set
		cells[f.X] = cell{strings.TrimSpace(f.Text), bg}
	}
	// a(0) space(1, page background) edge(2, code background) x(3) ...
	if c, ok := cells[1]; !ok || c.bg {
		t.Fatalf("space before code = %+v, want unstyled page space", c)
	}
	if c, ok := cells[2]; !ok || !c.bg {
		t.Fatalf("code left edge = %+v, want code background", c)
	}
	if c, ok := cells[3]; !ok || c.text != "x" {
		t.Fatalf("code text = %+v, want x at column 3", c)
	}
	if c, ok := cells[6]; !ok || c.text != "b" {
		t.Fatalf("text after code = %+v, want b at column 6", c)
	}
}
