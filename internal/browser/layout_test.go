package browser

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"time"
)

func build(src string, width int) *Layout {
	doc := ParseHTML(src)
	return BuildLayout(doc, nil, width)
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

func TestMediaQueryViewport(t *testing.T) {
	doc := ParseHTML(`<html><body><p class="m">x</p></body></html>`)
	rules := ParseStylesheet(`@media (max-width: 500px) { .m { display:none } }`)
	narrow := BuildLayout(doc, rules, 40) // 320 CSS px
	if st := narrow.Styles[doc.FindFirst("p")]; st == nil || st.Display != "none" {
		t.Fatalf("narrow display = %+v", st)
	}
	wide := BuildLayout(doc, rules, 100) // 800 CSS px
	if st := wide.Styles[doc.FindFirst("p")]; st == nil || st.Display == "none" {
		t.Fatalf("wide display = %+v", st)
	}
}

func TestOffscreenTransformHidden(t *testing.T) {
	ly := build(`<div style="transform:translateY(-100%)"><p>x</p></div>`, 40)
	for _, f := range ly.Frags {
		if f.Kind == fragText && strings.TrimSpace(f.Text) == "x" {
			t.Fatal("off-screen transformed content was rendered")
		}
	}
}

func TestInlineSVGFallbackLabel(t *testing.T) {
	ly := build(`<div><svg aria-label="Google"><path d="M0 0"/></svg><svg aria-hidden="true"></svg></div>`, 40)
	text := ""
	for _, f := range ly.Frags {
		if f.Kind == fragText {
			text += f.Text
		}
	}
	if !strings.Contains(text, "Google") {
		t.Fatalf("inline SVG label missing: %q", text)
	}
	if strings.Contains(text, "[svg]") || strings.Contains(text, "aria-hidden") {
		t.Fatalf("unexpected SVG placeholder: %q", text)
	}
}

func TestBrokenDecorativeImageSkipped(t *testing.T) {
	ly := build(`<p>before <img src="bad.svg"> after</p>`, 40)
	for _, f := range ly.Frags {
		if f.Kind == fragText && strings.Contains(f.Text, "[image:") {
			t.Fatalf("decorative broken image rendered: %q", f.Text)
		}
	}
}

func TestAutoMarginCenters(t *testing.T) {
	ly := build(`<div style="width:80px;margin:0 auto">x</div>`, 40)
	x := -1
	for _, f := range ly.Frags {
		if f.Kind == fragText && strings.TrimSpace(f.Text) == "x" {
			x = f.X
		}
	}
	// Body inset is 1 cell (8px); the 10-cell box leaves 28 cells, split 14/14.
	if x != 15 {
		t.Fatalf("centered text x = %d, want 15", x)
	}
}

func TestSearchFieldMinimumWidth(t *testing.T) {
	input := &Node{Type: ElementNode, Tag: "input", Attrs: []Attr{
		{Key: "type", Value: "search"}, {Key: "placeholder", Value: "Search"},
	}}
	input.Style = &Style{Display: "inline", FontSize: 16, LineHeight: 1}
	if w := stringWidth(formLabel(input)); w < 34 {
		t.Fatalf("search field width = %d, want at least 34", w)
	}
	area := &Node{Type: ElementNode, Tag: "textarea", Attrs: []Attr{{Key: "placeholder", Value: "Search"}}}
	area.Style = &Style{Display: "inline", FontSize: 16, LineHeight: 1}
	if w := stringWidth(formLabel(area)); w < 34 {
		t.Fatalf("textarea width = %d, want at least 34", w)
	}
}

func TestInputFocusClearsPlaceholder(t *testing.T) {
	input := &Node{Type: ElementNode, Tag: "input", Attrs: []Attr{{Key: "placeholder", Value: "Search query"}}}
	focusStyle := func(n *Node) {
		n.Style = &Style{Display: "inline", FontSize: 16, LineHeight: 1, Color: RGB(0, 0, 0)}
	}
	focusStyle(input)
	if label := formLabel(input); !strings.Contains(label, "Search query") {
		t.Fatalf("unfocused label = %q", label)
	}
	input.InputFocused = true
	if label := formLabel(input); strings.Contains(label, "Search query") {
		t.Fatalf("focused field still shows placeholder: %q", label)
	}
}

func TestFocusedInputIsProminent(t *testing.T) {
	ly := build(`<input value="abc">`, 40)
	input := ly.Frags
	var node *Node
	for i := range input {
		if input[i].Kind == fragText && input[i].Node != nil && input[i].Node.Tag == "input" {
			node = input[i].Node
			break
		}
	}
	if node == nil {
		t.Fatal("input fragment not found")
	}
	node.InputFocused = true
	ly = build(`<input value="abc">`, 40)
	g := NewGrid(40, 4, RGB(0, 0, 0), RGB(255, 255, 255))
	PaintLayout(g, ly, 0, 0, 4, nil, RGB(0, 0, 0))
	// Rebuild with the focused node so the painting path sees the flag.
	doc := ParseHTML(`<input value="abc">`)
	doc.FindFirst("input").InputFocused = true
	ly = BuildLayout(doc, nil, 40)
	PaintLayout(g, ly, 0, 0, 4, nil, RGB(0, 0, 0))
	blue := RGB(0x2f, 0x6f, 0xbf)
	found := false
	for _, c := range g.Cells {
		if c.BG == blue {
			found = true
			if c.FG.R != 255 || c.FG.G != 255 || c.FG.B != 255 {
				t.Fatalf("focused field text color = %+v", c.FG)
			}
		}
	}
	if !found {
		t.Fatal("focused input background was not painted")
	}
}

func TestButtonSkipsStyleText(t *testing.T) {
	ly := build(`<button>Go<style>.x{display:none}</style></button>`, 40)
	found := false
	for _, f := range ly.Frags {
		if f.Kind == fragText && strings.Contains(f.Text, ".x{") {
			t.Fatal("style text leaked into button label")
		}
		if f.Kind == fragText && strings.Contains(f.Text, "Go") {
			found = true
		}
	}
	if !found {
		t.Fatal("button label missing")
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

func TestLowContrastTextFallback(t *testing.T) {
	white := RGB(255, 255, 255)
	dark := RGB(0x1f, 0x23, 0x28)
	ly := &Layout{Height: 2, Frags: []Frag{{
		Kind: fragText, X: 0, Y: 0, W: 4, H: 1, Text: "test",
		Style: &Style{Color: white, FontSize: 16, LineHeight: 1},
	}}}
	g := NewGrid(10, 2, white, white)
	PaintLayout(g, ly, 0, 0, 2, nil, white)
	cell := g.Cells[0]
	if cell.FG == white {
		t.Fatal("white text on white background was not adjusted")
	}
	if cell.FG.R != dark.R || cell.FG.G != dark.G || cell.FG.B != dark.B {
		t.Fatalf("fallback color = %+v", cell.FG)
	}

	// White on black stays white.
	ly.Frags[0].Style.Color = white
	g2 := NewGrid(10, 2, white, RGB(0, 0, 0))
	PaintLayout(g2, ly, 0, 0, 2, nil, white)
	if g2.Cells[0].FG != white {
		t.Fatalf("white on black changed to %+v", g2.Cells[0].FG)
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

func TestTableColumnsAndCells(t *testing.T) {
	ly := build(`<table><tr><th>Name</th><th>Value</th></tr><tr><td>alpha</td><td>1</td></tr></table>`, 40)
	var texts []string
	for _, f := range ly.Frags {
		if f.Kind == fragText {
			texts = append(texts, f.Text)
		}
	}
	joined := strings.Join(texts, " ")
	for _, want := range []string{"Name", "Value", "alpha"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %q", want, joined)
		}
	}
}

func TestListMarkerBeforeText(t *testing.T) {
	ly := build(`<ul><li>First item</li></ul>`, 30)
	markerX, textX := -1, -1
	for _, f := range ly.Frags {
		if f.Kind != fragText {
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

func TestImageHalfBlocksSize(t *testing.T) {
	doc := ParseHTML(`<p><img src="x" alt="x"></p>`)
	img := image.NewRGBA(image.Rect(0, 0, 160, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 160; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 100, 255})
		}
	}
	doc.FindFirst("img").Img = img
	ly := BuildLayout(doc, nil, 60)
	var f *Frag
	for i := range ly.Frags {
		if ly.Frags[i].Kind == fragImage {
			f = &ly.Frags[i]
		}
	}
	if f == nil {
		t.Fatal("no image fragment")
	}
	if f.W != 20 || f.H != 5 {
		t.Fatalf("image size = %dx%d, want 20x5", f.W, f.H)
	}
}

func TestDisplayNoneAndHiddenText(t *testing.T) {
	ly := build(`<p style="display:none">gone</p><p style="visibility:hidden">invisible</p><p>shown</p>`, 30)
	var texts []string
	for _, f := range ly.Frags {
		if f.Kind == fragText {
			texts = append(texts, f.Text)
		}
	}
	j := strings.Join(texts, " ")
	if strings.Contains(j, "gone") || strings.Contains(j, "invisible") {
		t.Fatalf("hidden text rendered: %q", j)
	}
	if !strings.Contains(j, "shown") {
		t.Fatal("visible text missing")
	}
}

func TestScreenReaderOnlyBoxHidden(t *testing.T) {
	ly := build(`<a class="sr" href="#x" style="position:absolute;width:1px;height:1px;overflow:hidden">Jump</a><p>body</p>`, 30)
	for _, f := range ly.Frags {
		if f.Kind == fragText && f.Text == "Jump" {
			t.Fatal("screen-reader-only text should be hidden")
		}
	}
}

func TestPercentageWidth(t *testing.T) {
	ly := build(`<div style="width:50%; background:#eee"><p>abcdefghij klmnopqrst uvwxyz</p></div>`, 40)
	for _, f := range ly.Frags {
		if f.Kind == fragFill && f.W == 20 {
			return
		}
	}
	t.Fatal("no 50% wide background")
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

func TestWideRunesPaint(t *testing.T) {
	ly := build(`<p>日本語テキスト</p>`, 10)
	g := NewGrid(10, 10, RGB(0, 0, 0), RGB(255, 255, 255))
	PaintLayout(g, ly, 0, 0, 10, nil, RGB(0, 0, 0))
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			c := g.Cells[y*g.W+x]
			if c.Cont && (x == 0 || g.Cells[y*g.W+x-1].R == ' ') {
				t.Fatalf("orphan continuation cell at %d,%d", x, y)
			}
		}
	}
}

func TestRuneWidths(t *testing.T) {
	cases := []struct {
		s string
		w int
	}{
		{"abc", 3}, {"日本", 4}, {"é", 1}, {"a\u0301", 1}, {"★", 1},
	}
	for _, c := range cases {
		if got := stringWidth(c.s); got != c.w {
			t.Errorf("width(%q) = %d, want %d", c.s, got, c.w)
		}
	}
	if got := truncateWidth("abcdef", 3); got != "abc" {
		t.Errorf("truncate = %q", got)
	}
	if got := padWidth("ab", 5); got != "ab   " {
		t.Errorf("pad = %q", got)
	}
	if got := fitWidth("abc", 5); got != "abc  " {
		t.Errorf("fit short = %q", got)
	}
	if got := fitWidth("abcdef", 5); got != "abcd…" {
		t.Errorf("fit long = %q", got)
	}
}

func TestTextTransform(t *testing.T) {
	if got := applyTextTransform("hello world", "uppercase"); got != "HELLO WORLD" {
		t.Errorf("uppercase = %q", got)
	}
	if got := applyTextTransform("hello world", "capitalize"); got != "Hello World" {
		t.Errorf("capitalize = %q", got)
	}
	if got := expandTabs("a\tb"); got != "a   b" {
		t.Errorf("tabs = %q", got)
	}
}

func TestBuildLayoutDeadlineTruncates(t *testing.T) {
	doc := ParseHTML(`<html><body><p id="p">hello</p><div>world</div></body></html>`)
	rules := ParseStylesheet(`p { color: red }`)
	ly := BuildLayoutDeadline(doc, rules, 40, false, time.Now().Add(-time.Millisecond))
	if !ly.Truncated {
		t.Fatal("expired layout was not marked truncated")
	}
	for _, n := range doc.FindAll(func(*Node) bool { return true }) {
		if n.Style == nil {
			t.Fatalf("element %q has no style after truncated styling", n.Tag)
		}
	}
}

func TestBuildLayoutWithoutDeadlineComplete(t *testing.T) {
	doc := ParseHTML(`<html><body><p>hello</p></body></html>`)
	ly := BuildLayout(doc, nil, 40)
	if ly.Truncated {
		t.Fatal("small layout was unexpectedly truncated")
	}
}

func textFrag(ly *Layout, s string) *Frag {
	for i := range ly.Frags {
		if ly.Frags[i].Kind == fragText && strings.TrimSpace(ly.Frags[i].Text) == s {
			return &ly.Frags[i]
		}
	}
	return nil
}

func TestAbsoluteOutOfFlowAndOffsets(t *testing.T) {
	ly := build(`<div>first</div><div style="position:absolute;top:80px;left:24px">abs</div><div>after</div>`, 40)
	abs := textFrag(ly, "abs")
	after := textFrag(ly, "after")
	if abs == nil || after == nil {
		t.Fatalf("missing fragments: abs=%v after=%v", abs, after)
	}
	if abs.X != 3 || abs.Y != 5 {
		t.Fatalf("absolute offset = (%d,%d), want (3,5)", abs.X, abs.Y)
	}
	if after.Y > abs.Y {
		t.Fatalf("absolute box pushed following flow: after.Y=%d abs.Y=%d", after.Y, abs.Y)
	}
}

func TestAbsoluteContainingBlock(t *testing.T) {
	ly := build(`<div style="position:relative;height:160px"><div style="position:absolute;top:16px;left:16px">child</div></div><div>after</div>`, 40)
	child := textFrag(ly, "child")
	after := textFrag(ly, "after")
	if child == nil || after == nil {
		t.Fatalf("missing fragments: child=%v after=%v", child, after)
	}
	if child.X != 2 || child.Y != 1 {
		t.Fatalf("child at (%d,%d), want (2,1)", child.X, child.Y)
	}
	if after.Y < 10 {
		t.Fatalf("following flow did not clear the 160px containing block: after.Y=%d", after.Y)
	}
}

func TestAbsoluteRightBottom(t *testing.T) {
	ly := build(`<div>body</div><div style="position:absolute;right:0;bottom:0">abc</div>`, 30)
	abs := textFrag(ly, "abc")
	if abs == nil {
		t.Fatal("missing absolute fragment")
	}
	if abs.X != 27 || abs.Y != 0 {
		t.Fatalf("right/bottom position = (%d,%d), want (27,0)", abs.X, abs.Y)
	}
}

func TestAbsolutePercentageOffset(t *testing.T) {
	ly := build(`<div>body</div><div style="position:absolute;left:50%;top:0">abcd</div>`, 40)
	abs := textFrag(ly, "abcd")
	if abs == nil {
		t.Fatal("missing absolute fragment")
	}
	if abs.X != 20 {
		t.Fatalf("50%% left = %d, want 20", abs.X)
	}
}

func TestAbsoluteStretchBetweenOffsets(t *testing.T) {
	ly := build(`<div style="position:relative;height:80px;background:#eee"><div style="position:absolute;top:0;left:0;right:0;bottom:0;background:#ff0000"></div></div>`, 30)
	red := RGB(0xff, 0, 0)
	for _, f := range ly.Frags {
		if f.Kind == fragFill && f.Color == red {
			if f.X != 0 || f.Y != 0 || f.W != 30 || f.H != 5 {
				t.Fatalf("stretched box = %+v, want x=0 y=0 w=30 h=5", f)
			}
			return
		}
	}
	t.Fatal("no stretched absolute background")
}

func TestAbsolutePaintsAboveFlow(t *testing.T) {
	ly := build(`<div style="position:absolute;top:0;left:0">abs</div><p>flow</p>`, 30)
	absIdx, flowIdx := -1, -1
	for i, f := range ly.Frags {
		if f.Kind != fragText {
			continue
		}
		switch strings.TrimSpace(f.Text) {
		case "abs":
			absIdx = i
		case "flow":
			flowIdx = i
		}
	}
	if absIdx < 0 || flowIdx < 0 {
		t.Fatalf("missing fragments: abs=%d flow=%d", absIdx, flowIdx)
	}
	if absIdx < flowIdx {
		t.Fatalf("absolute painted below flow: abs=%d flow=%d", absIdx, flowIdx)
	}
}

func TestRelativeOffsetMovesOnlyItsBox(t *testing.T) {
	ly := build(`<div style="position:relative;top:16px;left:16px">shift</div><div>next</div>`, 30)
	shift := textFrag(ly, "shift")
	next := textFrag(ly, "next")
	if shift == nil || next == nil {
		t.Fatalf("missing fragments: shift=%v next=%v", shift, next)
	}
	if shift.X != 2 || shift.Y != 1 {
		t.Fatalf("relative box at (%d,%d), want (2,1)", shift.X, shift.Y)
	}
	if next.X != 0 || next.Y != 1 {
		t.Fatalf("sibling moved with relative box: (%d,%d), want (0,1)", next.X, next.Y)
	}
}

func TestAbsoluteLinkRectFollowsOffset(t *testing.T) {
	ly := build(`<a href="#x" style="position:absolute;left:24px;top:80px">L</a>`, 30)
	if len(ly.Links) != 1 || len(ly.Links[0].Rects) == 0 {
		t.Fatalf("links = %+v", ly.Links)
	}
	r := ly.Links[0].Rects[0]
	if r.X != 3 || r.Y != 5 {
		t.Fatalf("link rect = %+v, want x=3 y=5", r)
	}
}

func TestAbsoluteNestedInRelative(t *testing.T) {
	ly := build(`<div style="position:relative;left:16px"><div style="position:absolute;top:0;left:0">nested</div></div>`, 30)
	nested := textFrag(ly, "nested")
	if nested == nil {
		t.Fatal("missing nested absolute fragment")
	}
	// The relative parent shifts to x=2 and its containing block follows, so
	// the child starts at the parent's new left edge.
	if nested.X != 2 || nested.Y != 0 {
		t.Fatalf("nested absolute at (%d,%d), want (2,0)", nested.X, nested.Y)
	}
}
