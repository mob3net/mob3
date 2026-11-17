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

func TestLayoutBudgetPresets(t *testing.T) {
	if _, ok := ParseLayoutBudget("nonsense"); ok {
		t.Fatal("invalid budget name accepted")
	}
	if b, ok := ParseLayoutBudget("standard"); !ok || b.Frags != 250000 || b.Rows != 200000 {
		t.Fatalf("standard budget = %+v", b)
	}
	if b, ok := ParseLayoutBudget("none"); !ok || b.Time != 0 || b.Frags != 0 || b.Rows != 0 {
		t.Fatalf("none budget = %+v", b)
	}
}

func TestLayoutRowLimitTruncates(t *testing.T) {
	src := `<div><p>alpha beta gamma delta epsilon</p><p>one two three four five</p></div>`
	root := ParseHTML(src)
	limited := BuildLayoutBudgetDeadline(root, nil, 40, 0, false, false, time.Time{}, LayoutBudget{Name: "tiny", Rows: 1})
	if !limited.Truncated {
		t.Fatalf("tiny row budget did not truncate: frags=%d height=%d", len(limited.Frags), limited.Height)
	}
	root = ParseHTML(src)
	unlimited := BuildLayoutBudgetDeadline(root, nil, 40, 0, false, false, time.Time{}, LayoutBudget{Name: "none"})
	if unlimited.Truncated {
		t.Fatal("unlimited budget truncated")
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
	ly := build(`<div><svg aria-label="Google"><path d="M0 0"/></svg><svg aria-hidden="true"></svg><svg><title>Visible title</title></svg></div>`, 40)
	text := ""
	for _, f := range ly.Frags {
		if f.Kind == fragText {
			text += f.Text
		}
	}
	if !strings.Contains(text, "▣") {
		t.Fatalf("icon-only SVG placeholder missing: %q", text)
	}
	if !strings.Contains(text, "Visible title") {
		t.Fatalf("SVG title fallback missing: %q", text)
	}
	if strings.Contains(text, "Google") || strings.Contains(text, "aria-hidden") {
		t.Fatalf("unexpected SVG accessible-name text: %q", text)
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

func TestTableCellPercentageWidthAndNowrap(t *testing.T) {
	ly := build(`<table><tr><td width="25%"></td><td>search</td><td width="25%" nowrap><a href="/x">Advanced search</a></td></tr></table>`, 60)
	linkLines := map[int]bool{}
	var linkText string
	for _, f := range ly.Frags {
		if f.Kind != fragText || f.Link == nil || f.Link.Href != "/x" {
			continue
		}
		linkLines[f.Y] = true
		linkText += f.Text
	}
	if len(linkLines) != 1 {
		t.Fatalf("legacy nowrap cell wrapped over %d lines", len(linkLines))
	}
	if !strings.Contains(linkText, "Advanced") {
		t.Fatalf("advanced link text missing: %q", linkText)
	}
}

func TestTableCellWidthDoesNotClampContent(t *testing.T) {
	ly := build(`<table><tr><td width="10%">abcdefgh</td><td>zzzzzzzzzzzzzzzzzzzz</td></tr></table>`, 60)
	for _, f := range ly.Frags {
		if f.Kind == fragText && f.Text == "abcdefgh" {
			return
		}
	}
	t.Fatal("cell content was wrapped by its own width hint")
}

func TestTableCenteredByCenterTag(t *testing.T) {
	left := build(`<table><tr><td>Name</td><td>Value</td></tr></table>`, 40)
	center := build(`<center><table><tr><td>Name</td><td>Value</td></tr></table></center>`, 40)
	leftX, centerX := -1, -1
	for _, f := range left.Frags {
		if f.Kind == fragText && f.Text == "Name" {
			leftX = f.X
		}
	}
	for _, f := range center.Frags {
		if f.Kind == fragText && f.Text == "Name" {
			centerX = f.X
		}
	}
	if leftX < 0 || centerX < 0 {
		t.Fatalf("left=%d center=%d", leftX, centerX)
	}
	if centerX <= leftX {
		t.Fatalf("centered table at %d should be right of left-aligned %d", centerX, leftX)
	}
}

func TestHiddenInputDoesNotWidenTableCell(t *testing.T) {
	plain := build(`<table><tr><td>ab</td></tr></table>`, 40)
	hidden := build(`<table><tr><td><input type="hidden" name="x" value="long-value">ab</td></tr></table>`, 40)
	tableWidth := func(ly *Layout) int {
		for _, f := range ly.Frags {
			if f.Kind == fragFill && f.W > 1 {
				return f.W
			}
		}
		return 0
	}
	if pw, hw := tableWidth(plain), tableWidth(hidden); pw != hw {
		t.Fatalf("hidden input changed table width: plain=%d hidden=%d", pw, hw)
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

func TestClosedDetailsHidesContent(t *testing.T) {
	closed := build(`<details><summary>summary</summary><p>secret</p></details>`, 40)
	for _, f := range closed.Frags {
		if f.Kind == fragText && strings.Contains(f.Text, "secret") {
			t.Fatal("closed details rendered its content")
		}
	}
	open := build(`<details open><summary>summary</summary><p>visible</p></details>`, 40)
	found := false
	for _, f := range open.Frags {
		if f.Kind == fragText && strings.Contains(f.Text, "visible") {
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

func TestTextareaLargeValueBounded(t *testing.T) {
	doc := ParseHTML(`<textarea name="t">` + strings.Repeat("word ", 50000) + `</textarea>`)
	ly := BuildLayout(doc, nil, 80)
	area := doc.FindFirst("textarea")
	if area == nil {
		t.Fatal("textarea not found")
	}
	ys := map[int]bool{}
	for _, f := range ly.Frags {
		if f.Kind == fragText && f.Node == area {
			ys[f.Y] = true
		}
	}
	if len(ys) == 0 || len(ys) > 12 {
		t.Fatalf("large textarea rendered %d rows, want 1-12", len(ys))
	}
}

func TestTextareaRendersAtLeastTwoRows(t *testing.T) {
	doc := ParseHTML(`<textarea name="t">hello</textarea>`)
	ly := BuildLayout(doc, nil, 60)
	area := doc.FindFirst("textarea")
	if area == nil {
		t.Fatal("textarea not found")
	}
	ys := map[int]bool{}
	for _, f := range ly.Frags {
		if f.Kind == fragText && f.Node == area {
			ys[f.Y] = true
		}
	}
	if len(ys) < 2 {
		t.Fatalf("textarea rendered %d row(s), want at least 2", len(ys))
	}
}

func TestTextareaWrapsAndHonoursRows(t *testing.T) {
	doc := ParseHTML(`<textarea name="t" rows="4">` + strings.Repeat("word ", 40) + `</textarea>`)
	ly := BuildLayout(doc, nil, 60)
	area := doc.FindFirst("textarea")
	ys := map[int]bool{}
	for _, f := range ly.Frags {
		if f.Kind == fragText && f.Node == area {
			ys[f.Y] = true
		}
	}
	if len(ys) < 4 {
		t.Fatalf("textarea rendered %d row(s), want at least 4 rows", len(ys))
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

func buildViewport(src string, width, viewportH int) *Layout {
	doc := ParseHTML(src)
	return BuildLayoutViewportDeadline(doc, nil, width, viewportH, false, time.Time{})
}

func TestFixedDumpFallsBackToPageCoordinates(t *testing.T) {
	ly := build(`<div style="height:80px">top</div><div style="position:fixed;bottom:0">bar</div>`, 30)
	bar := textFrag(ly, "bar")
	if bar == nil {
		t.Fatal("fixed fragment missing in dump layout")
	}
	if bar.Fixed {
		t.Fatal("dump layout should not mark fixed fragments as viewport-anchored")
	}
	if ly.Height < bar.Y+1 {
		t.Fatalf("page height %d does not include fixed content at %d", ly.Height, bar.Y)
	}
}

func TestFixedAnchorsToViewportBottomRight(t *testing.T) {
	ly := buildViewport(`<div>top</div><div style="position:fixed;bottom:0;right:0">bar</div>`, 30, 10)
	bar := textFrag(ly, "bar")
	if bar == nil {
		t.Fatal("fixed fragment not rendered")
	}
	if bar.X != 27 || bar.Y != 9 {
		t.Fatalf("fixed box at (%d,%d), want (27,9)", bar.X, bar.Y)
	}
	if !bar.Fixed || bar.FixedY != 9 {
		t.Fatalf("fixed marker = %v FixedY=%d", bar.Fixed, bar.FixedY)
	}
	if ly.Height != 1 {
		t.Fatalf("fixed box changed page height: %d", ly.Height)
	}
}

func TestFixedIgnoresRelativeAncestor(t *testing.T) {
	ly := buildViewport(`<div style="position:relative;margin-left:40px;padding-top:40px"><div style="position:fixed;bottom:0;right:0">bar</div></div>`, 30, 10)
	bar := textFrag(ly, "bar")
	if bar == nil {
		t.Fatal("fixed fragment not rendered")
	}
	if bar.X != 27 || bar.Y != 9 {
		t.Fatalf("fixed box at (%d,%d), want viewport (27,9)", bar.X, bar.Y)
	}
}

func TestFixedScrollKeepsViewportPosition(t *testing.T) {
	ly := buildViewport(`<div>top</div><div style="position:fixed;bottom:0;right:0">bar</div>`, 30, 10)
	ly.SetFixedScroll(4)
	bar := textFrag(ly, "bar")
	if bar == nil {
		t.Fatal("fixed fragment missing after scroll")
	}
	// Document coordinates shift with scroll so the painted viewport row stays
	// at 9 once PaintLayout subtracts the scroll offset.
	if bar.Y != 13 {
		t.Fatalf("scrolled fixed Y = %d, want 13", bar.Y)
	}
}

func TestBrokenImagePlaceholder(t *testing.T) {
	ly := build(`<p><img alt="" width="40" height="40"></p>`, 40)
	if textFrag(ly, "░") == nil {
		t.Fatal("explicitly sized broken image has no placeholder")
	}
	ly = build(`<p><img alt="" width="8" height="8"></p>`, 40)
	for _, f := range ly.Frags {
		if f.Kind == fragText && strings.Contains(f.Text, "░") {
			t.Fatalf("1-cell broken image showed a placeholder: %q", f.Text)
		}
	}
}

func TestPageBackgroundFallback(t *testing.T) {
	ly := buildViewport(`<body style="margin:0"><div style="background:#123456;height:800px">x</div></body>`, 40, 20)
	if ly.PageBG != RGB(0x12, 0x34, 0x56) {
		t.Fatalf("fallback page background = %+v", ly.PageBG)
	}

	small := buildViewport(`<body style="margin:0"><div style="background:#123456;height:16px">x</div>`+
		`<div>`+strings.Repeat("y<br>", 40)+`</div></body>`, 40, 20)
	if small.PageBG != RGB(255, 255, 255) {
		t.Fatalf("small fill became the page background: %+v", small.PageBG)
	}

	explicit := buildViewport(`<body style="margin:0;background:#ffffff"><div style="background:#123456;height:800px">x</div></body>`, 40, 20)
	if explicit.PageBG != RGB(255, 255, 255) {
		t.Fatalf("explicit body background was overridden: %+v", explicit.PageBG)
	}
}

func TestBoxSizingBorderBox(t *testing.T) {
	const text = `abcdefghijklmnopqrstuvwxyz`
	content := func(css string) int {
		ly := build(`<div style="`+css+`">`+text+`</div>`, 40)
		for _, f := range ly.Frags {
			if f.Kind == fragText && strings.TrimSpace(f.Text) != "" {
				return f.W
			}
		}
		t.Fatalf("no text fragment for %q", css)
		return 0
	}
	if got := content(`width:80px;padding:8px;border:1px solid;box-sizing:content-box`); got != 10 {
		t.Fatalf("content-box width = %d, want 10", got)
	}
	if got := content(`width:80px;padding:8px;border:1px solid;box-sizing:border-box`); got != 6 {
		t.Fatalf("border-box width = %d, want 6", got)
	}

	height := func(css string) int {
		ly := build(`<div style="background:#ff0000;`+css+`"></div>`, 40)
		for _, f := range ly.Frags {
			if f.Kind == fragFill && f.Color == RGB(0xff, 0, 0) {
				return f.H
			}
		}
		t.Fatalf("no background fill for %q", css)
		return 0
	}
	if got := height(`height:64px;padding:8px;border:1px solid;box-sizing:content-box`); got != 8 {
		t.Fatalf("content-box height = %d, want 8", got)
	}
	if got := height(`height:64px;padding:8px;border:1px solid;box-sizing:border-box`); got != 4 {
		t.Fatalf("border-box height = %d, want 4", got)
	}
}

func TestMinMaxBoxConstraints(t *testing.T) {
	ly := build(`<div style="width:8px;min-width:40px">abcdefghij</div>`, 40)
	if f := textFrag(ly, "abcde"); f == nil || f.W != 5 {
		t.Fatalf("min-width did not widen content: %+v", f)
	}

	bg := func(css string) int {
		ly := build(`<div style="background:#00ff00;`+css+`"></div>`, 40)
		for _, f := range ly.Frags {
			if f.Kind == fragFill && f.Color == RGB(0, 0xff, 0) {
				return f.H
			}
		}
		return -1
	}
	if got := bg("height:80px;max-height:32px"); got != 2 {
		t.Fatalf("max-height = %d rows, want 2", got)
	}
	if got := bg("height:80px;max-height:16px;min-height:48px"); got != 3 {
		t.Fatalf("min-height should win over max-height: %d rows, want 3", got)
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

func TestFormControlGlyphs(t *testing.T) {
	control := func(typ string, checked bool) *Node {
		attrs := []Attr{{Key: "type", Value: typ}}
		if checked {
			attrs = append(attrs, Attr{Key: "checked"})
		}
		return &Node{Type: ElementNode, Tag: "input", Attrs: attrs}
	}
	cases := []struct {
		node *Node
		want string
	}{
		{control("checkbox", true), glyphCheckboxOn},
		{control("checkbox", false), glyphCheckboxOff},
		{control("radio", true), glyphRadioOn},
		{control("radio", false), glyphRadioOff},
	}
	for _, c := range cases {
		if got := formLabel(c.node); got != c.want {
			t.Errorf("formLabel(%s) = %q, want %q", c.node.Attr("type"), got, c.want)
		}
		if w := stringWidth(c.want); w != 1 {
			t.Errorf("glyph %q width = %d, want 1 cell", c.want, w)
		}
	}
}

func TestTableCellVerticalAlign(t *testing.T) {
	table := func(align string) *Layout {
		return build(`<table><tr><td style="`+align+`">short</td>`+
			`<td>top<br>middle<br>bottom</td></tr></table>`, 40)
	}
	shortY := func(ly *Layout) int {
		f := textFrag(ly, "short")
		if f == nil {
			t.Fatal("short cell text missing")
		}
		return f.Y
	}
	if got, want := shortY(table("")), textFrag(table(""), "middle").Y; got != want {
		t.Errorf("default vertical-align Y=%d, want middle Y=%d", got, want)
	}
	if got, want := shortY(table("vertical-align:top")), textFrag(table(""), "top").Y; got != want {
		t.Errorf("vertical-align:top Y=%d, want top line Y=%d", got, want)
	}
	if got, want := shortY(table("vertical-align:bottom")), textFrag(table(""), "bottom").Y; got != want {
		t.Errorf("vertical-align:bottom Y=%d, want bottom line Y=%d", got, want)
	}
}

func TestTableCellBackgroundNotShifted(t *testing.T) {
	ly := build(`<table><tr><td style="background:#ff0000;vertical-align:bottom">short</td>`+
		`<td>top<br>middle<br>bottom</td></tr></table>`, 40)
	top := textFrag(ly, "top")
	if top == nil {
		t.Fatal("reference text missing")
	}
	for _, f := range ly.Frags {
		if f.Kind == fragFill && f.Color == RGB(0xff, 0, 0) {
			if f.Y != top.Y {
				t.Fatalf("cell background Y=%d, want cell top %d", f.Y, top.Y)
			}
			return
		}
	}
	t.Fatal("cell background fill missing")
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

func TestBlockImageFragment(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 160, 90))
	doc := ParseHTML(`<html><body><img id="hero" src="hero.jpg" alt="hero" style="display:block;width:32px"></body></html>`)
	node := doc.FindFirst("img")
	node.Img = img
	ly := BuildLayout(doc, nil, 80)
	for _, f := range ly.Frags {
		if f.Kind == fragImage && f.Node == node {
			if f.W != 4 || f.H < 1 {
				t.Fatalf("block image = %dx%d, want 4-wide image fragment", f.W, f.H)
			}
			return
		}
	}
	t.Fatal("display:block image did not produce an image fragment")
}

func TestAbsoluteImageInsideAspectPlaceholder(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 160, 90))
	doc := ParseHTML(`<html><body>
<div style="position:relative;width:100%;padding-bottom:56.25%">
  <img id="hero" src="hero.jpg" alt="hero" style="display:block;position:absolute;top:0;right:0;bottom:0;left:0;width:100%;height:100%">
</div>
<p id="after">after</p>
</body></html>`)
	node := doc.FindFirst("img")
	node.Img = img
	ly := BuildLayout(doc, nil, 40)
	found := false
	for _, f := range ly.Frags {
		if f.Kind == fragImage && f.Node == node {
			found = true
			if f.W <= 0 || f.H <= 0 {
				t.Fatalf("absolute image size = %dx%d", f.W, f.H)
			}
		}
	}
	if !found {
		t.Fatal("absolute image did not produce an image fragment")
	}
	after := 0
	for _, f := range ly.Frags {
		if f.Kind == fragText && f.Text == "after" {
			after = f.Y
		}
	}
	if after == 0 || after > 20 {
		t.Fatalf("paragraph after aspect placeholder is at row %d, want the placeholder height to use its width", after)
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

func TestPseudoAfterAspectBox(t *testing.T) {
	rules := ParseStylesheet(`.box:after { content:""; display:block; padding-top:50%; }`)
	doc := ParseHTML(`<html><body><div class="box" style="width:100%"></div><p id="after">after</p></body></html>`)
	ly := BuildLayout(doc, rules, 40)
	afterY := 0
	for _, f := range ly.Frags {
		if f.Kind == fragText && f.Text == "after" {
			afterY = f.Y
		}
	}
	if afterY < 8 {
		t.Fatalf("paragraph after aspect box is at row %d, want the generated box to add height", afterY)
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

func TestHideFixedElements(t *testing.T) {
	doc := ParseHTML(`<html><body><p>body</p><div id="fx" style="position:fixed;top:0;left:0">fixed</div></body></html>`)
	show := BuildLayoutBudgetDeadlineOptions(doc, nil, 40, 10, false, false, false, time.Time{}, DefaultLayoutBudget)
	hidden := BuildLayoutBudgetDeadlineOptions(doc, nil, 40, 10, false, false, true, time.Time{}, DefaultLayoutBudget)
	fixedFrags := func(ly *Layout) int {
		n := 0
		for _, f := range ly.Frags {
			if f.Fixed {
				n++
			}
		}
		return n
	}
	if fixedFrags(show) == 0 {
		t.Fatal("normal layout has no fixed fragments")
	}
	if got := fixedFrags(hidden); got != 0 {
		t.Fatalf("hidden layout has %d fixed fragments", got)
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

func TestMeasureSkipsOutOfFlowChildren(t *testing.T) {
	doc := ParseHTML(`<html><body><div id="box"><span style="position:absolute;width:100px">very wide absolute label</span></div></body></html>`)
	styles := newStyleEngine(nil, 0).computeAll(doc)
	l := &layouter{styles: styles, width: 40}
	box := doc.FindFirst("div")
	maxW, minW := l.measure(box)
	if maxW > 10 || minW > 10 {
		t.Fatalf("measure included out-of-flow child: max=%d min=%d", maxW, minW)
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
		if f.Kind == fragText && (f.Text == "A" || f.Text == "B" || f.Text == "C") {
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
		if f.Kind == fragText {
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

func TestViewportUnits(t *testing.T) {
	doc := ParseHTML(`<html><body><div id="v" style="width:50vw;height:25vh"></div></body></html>`)
	ly := BuildLayoutBudgetDeadlineOptions(doc, nil, 80, 20, false, false, false, time.Time{}, DefaultLayoutBudget)
	v := doc.FindFirst("div")
	if v.Style.Width.U != UnitPX || v.Style.Width.V != 320 {
		t.Fatalf("50vw width = %+v, want 320px", v.Style.Width)
	}
	if v.Style.Height.U != UnitPX || v.Style.Height.V != 80 {
		t.Fatalf("25vh height = %+v, want 80px", v.Style.Height)
	}
	_ = ly
}

func TestOverflowClippingStopsSpill(t *testing.T) {
	ly := build(`<html><body>
<div id="box" style="max-height:2em;overflow:hidden">
  <div>one</div><div>two</div><div>three</div><div>four</div>
</div>
<p id="after">after</p></body></html>`, 40)
	seen := map[string]Frag{}
	for _, f := range ly.Frags {
		if f.Kind == fragText {
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
		if f.Kind == fragText && strings.Contains(f.Text, "abcdefgh") {
			t.Fatalf("nowrap run was not clipped: %+v", f)
		}
	}
}

func fragOwnerID(f Frag) string {
	for n := f.Node; n != nil; n = n.Parent {
		if id := n.Attr("id"); id != "" {
			return id
		}
	}
	return ""
}

func TestFloatLeftWrapsText(t *testing.T) {
	ly := build(`<html><body>
<div id="f" style="float:left;width:10em;height:3em;background:#eee">F</div>
<p id="p">one two three four five six seven eight nine ten eleven twelve thirteen fourteen</p>
</body></html>`, 40)
	var narrowed, full bool
	for _, f := range ly.Frags {
		if f.Kind != fragText || fragOwnerID(f) != "p" || strings.TrimSpace(f.Text) == "" {
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
		if f.Kind != fragText || fragOwnerID(f) != "p" || strings.TrimSpace(f.Text) == "" {
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
		if f.Kind == fragText && fragOwnerID(f) == "clear" {
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
		if f.Kind == fragText && fragOwnerID(f) == "after" {
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
		if f.Kind == fragText && fragOwnerID(f) == "p" && strings.TrimSpace(f.Text) != "" && f.Y < 4 {
			if f.X > 15 {
				t.Fatalf("figcaption inflated the float width: text x=%d %q", f.X, f.Text)
			}
			return
		}
	}
	t.Fatal("no text beside the floated figure")
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

func TestHiddenAttributeHidesElement(t *testing.T) {
	ly := build(`<div hidden>secret</div><p>visible</p>`, 40)
	seen := map[string]bool{}
	for _, f := range ly.Frags {
		if f.Kind == fragText {
			seen[f.Text] = true
		}
	}
	if seen["secret"] {
		t.Fatal("hidden element is still laid out")
	}
	if !seen["visible"] {
		t.Fatal("visible sibling was lost")
	}
}

func firstTableFillWidth(ly *Layout) int {
	width := 0
	for _, f := range ly.Frags {
		if f.Kind == fragFill && f.W > width {
			width = f.W
		}
	}
	return width
}

func TestTableWidthDeclaration(t *testing.T) {
	ly := build(`<table style="width:100%"><tr><td>a</td><td>b</td></tr></table>`, 40)
	if got := firstTableFillWidth(ly); got != 40 {
		t.Fatalf("width:100%% table = %d, want 40", got)
	}
}

func TestTablePercentageColumnsFit(t *testing.T) {
	ly := build(`<table style="width:100%"><tr><th style="width:70%">A</th><th style="width:70%">B</th></tr></table>`, 40)
	if got := firstTableFillWidth(ly); got != 40 {
		t.Fatalf("overflowing percentage table = %d, want 40", got)
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

func TestObjectFitCoverAndContain(t *testing.T) {
	doc := ParseHTML(`<img id="c" style="display:block;width:160px;height:160px;object-fit:cover">
<img id="t" style="display:block;width:160px;height:160px;object-fit:contain">`)
	src := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for _, img := range doc.FindAll(func(n *Node) bool { return n.Tag == "img" }) {
		img.Img = src
	}
	ly := BuildLayout(doc, nil, 100)
	got := map[string]*Frag{}
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind == fragImage && f.Node != nil {
			got[f.Node.Attr("id")] = f
		}
	}
	cover := got["c"]
	if cover == nil || cover.W != 20 || cover.H != 10 {
		t.Fatalf("cover frag = %+v", cover)
	}
	if cover.Crop.Dx() != 100 || cover.Crop.Dy() != 100 {
		t.Fatalf("cover crop = %v, want 100x100", cover.Crop)
	}
	contain := got["t"]
	if contain == nil || contain.W != 20 || contain.H != 5 {
		t.Fatalf("contain frag = %+v", contain)
	}
	if contain.Crop != (image.Rectangle{}) {
		t.Fatalf("contain crop = %v, want full image", contain.Crop)
	}
}

func TestObjectFitNone(t *testing.T) {
	doc := ParseHTML(`<img id="s" style="display:block;width:160px;height:160px;object-fit:none">
<img id="l" style="display:block;width:80px;height:80px;object-fit:none;object-position:right bottom">`)
	small := image.NewRGBA(image.Rect(0, 0, 80, 80))
	large := image.NewRGBA(image.Rect(0, 0, 400, 400))
	for _, img := range doc.FindAll(func(n *Node) bool { return n.Tag == "img" }) {
		if img.Attr("id") == "s" {
			img.Img = small
		} else {
			img.Img = large
		}
	}
	ly := BuildLayout(doc, nil, 100)
	got := map[string]*Frag{}
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind == fragImage && f.Node != nil {
			got[f.Node.Attr("id")] = f
		}
	}
	// 80x80 px fits inside a 20x10 cell box and stays at its intrinsic size.
	if f := got["s"]; f == nil || f.W != 10 || f.H != 5 || f.Crop != (image.Rectangle{}) {
		t.Fatalf("small none frag = %+v", f)
	}
	// 400x400 px overflows a 10x5 cell box and is cropped from the bottom-right.
	f := got["l"]
	if f == nil || f.W != 10 || f.H != 5 {
		t.Fatalf("large none frag = %+v", f)
	}
	if f.Crop.Dx() != 80 || f.Crop.Dy() != 80 || f.Crop.Max != (image.Point{400, 400}) {
		t.Fatalf("large none crop = %v, want 80x80 at bottom-right", f.Crop)
	}
}

func TestBlockAspectRatio(t *testing.T) {
	ly := build(`<div style="width:160px;aspect-ratio:2/1"></div>after`, 40)
	for _, f := range ly.Frags {
		if f.Kind == fragText && strings.Contains(f.Text, "after") {
			if f.Y != 5 {
				t.Fatalf("text after aspect-ratio box y = %d, want 5", f.Y)
			}
			return
		}
	}
	t.Fatal("text after box missing")
}

func TestAbsoluteWidthResolvedOnce(t *testing.T) {
	doc := ParseHTML(`<div id="abs" style="position:absolute;width:25%;height:20px;padding:0 8px;box-sizing:border-box"></div>`)
	ly := BuildLayout(doc, nil, 100)
	n := doc.FindFirst("div")
	if n == nil {
		t.Fatal("absolute box missing")
	}
	if box := ly.Boxes[n]; box.W != 25 {
		t.Fatalf("absolute width = %d, want 25 (25%% of 100, resolved once)", box.W)
	}
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

func TestMeasureCollapsesWhitespace(t *testing.T) {
	if mw, mi := measureInlineText("\n   alpha   beta \n", "normal", 1); mw != 10 || mi != 5 {
		t.Fatalf("normal text measure = %d,%d, want 10,5", mw, mi)
	}
	if mw, _ := measureInlineText(" a  b ", "pre", 1); mw != 6 {
		t.Fatalf("pre text measure = %d, want 6 preserved cells", mw)
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
		if f.Kind == fragText && (strings.TrimSpace(f.Text) == "aaaa" || strings.TrimSpace(f.Text) == "bbbb") {
			rows = append(rows, f.Y)
		}
	}
	if len(rows) != 2 || rows[0] != rows[1] {
		t.Fatalf("float rows = %v, want both on one row", rows)
	}
}

func TestDefiniteWidthCountsInMeasure(t *testing.T) {
	doc := ParseHTML(`<div style="width:400px"><div id="box" style="float:left"><div style="width:48px;height:16px"></div></div></div>`)
	ly := BuildLayout(doc, nil, 50)
	var box *Node
	doc.FindAll(func(n *Node) bool {
		if n.Attr("id") == "box" {
			box = n
		}
		return false
	})
	if box == nil {
		t.Fatal("box missing")
	}
	if b := ly.Boxes[box]; b.W != 6 {
		t.Fatalf("float around a 48px child = %d cells, want 6", b.W)
	}
}

func TestLayoutImagesOffUsesAltText(t *testing.T) {
	doc := ParseHTML(`<p>a<img alt="pic" src="x.png">b</p>`)
	for _, img := range doc.FindAll(func(n *Node) bool { return n.Tag == "img" }) {
		img.Img = image.NewRGBA(image.Rect(0, 0, 160, 80))
	}
	ly := BuildLayoutBudgetDeadlineImages(doc, nil, 40, 0, false, false, false, true, time.Time{}, DefaultLayoutBudget)
	found := false
	for _, f := range ly.Frags {
		if f.Kind == fragImage {
			t.Fatal("imagesOff layout produced an image fragment")
		}
		if f.Kind == fragText && strings.Contains(f.Text, "pic") {
			found = true
		}
	}
	if !found {
		t.Fatal("imagesOff layout did not show the alt label")
	}
}

func TestBlockInlineChildrenMeasureAsOneLine(t *testing.T) {
	doc := ParseHTML(`<div id="box" style="display:block"><span>aa</span> <span>bb</span></div>`)
	ly := BuildLayout(doc, nil, 40)
	l := &layouter{width: 40, styles: ly.Styles}
	node := doc.FindFirst("div")
	if node == nil {
		t.Fatal("box missing")
	}
	if mw, _ := l.measure(node); mw != 5 {
		t.Fatalf("block with inline children measure = %d, want 5 (aa bb)", mw)
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

func TestImageAltLabelClipsToBox(t *testing.T) {
	long := strings.Repeat("very long alternative text ", 6)
	cases := []struct {
		name string
		src  string
	}{
		{"inline", `<div style="width:160px"><img alt="` + long + `" style="width:100%"></div>`},
		{"absolute", `<div style="position:relative;width:160px;height:90px"><img alt="` + long + `" style="position:absolute;top:0;left:0;width:100%;height:100%"></div>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ly := build(tc.src, 100)
			found := false
			for _, f := range ly.Frags {
				if f.Kind == fragText && strings.HasPrefix(strings.TrimSpace(f.Text), "[image:") {
					found = true
					if f.W > 20 || f.X+f.W > 21 {
						t.Fatalf("alt label = x %d w %d, want within a 20-cell box", f.X, f.W)
					}
				}
			}
			if !found {
				t.Fatal("no alt label fragment")
			}
		})
	}
}

func TestImageAltLabelUsesMarkerInTinyBox(t *testing.T) {
	ly := build(`<div style="width:32px"><img alt="long alternative text" style="width:100%"></div>`, 100)
	for _, f := range ly.Frags {
		if f.Kind == fragText {
			if strings.HasPrefix(f.Text, "[image:") {
				t.Fatalf("tiny image drew a full label: %q", f.Text)
			}
			if strings.Contains(f.Text, "▣") {
				return
			}
		}
	}
	t.Fatal("tiny image did not use the compact marker")
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
