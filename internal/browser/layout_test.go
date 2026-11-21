package browser

import (
	"strings"
	"testing"
	"time"
)

func build(src string, width int) *Layout {
	doc := ParseHTML(src)
	return BuildLayout(doc, nil, width)
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

func TestFixedWidthOverflowsNarrowContainer(t *testing.T) {
	// A fixed desktop canvas wider than the terminal keeps its declared
	// width, so its columns stay side by side and the page clips at the
	// right edge instead of reflowing into one left column.
	ly := build(`<html><body style="margin:0">
<div style="width:960px;background:#000;height:8px"></div>
<div style="width:960px;display:flex;gap:16px">
<div style="width:460px">alpha</div>
<div style="width:460px">beta</div>
</div></body></html>`, 80)
	wide := false
	for _, f := range ly.Frags {
		if f.Kind == fragFill && f.W == 120 {
			wide = true
		}
	}
	if !wide {
		t.Fatal("960px canvas was clamped to the 80-column viewport")
	}
	pos := map[string]Frag{}
	for _, f := range ly.Frags {
		if f.Kind == fragText && (f.Text == "alpha" || f.Text == "beta") {
			pos[f.Text] = f
		}
	}
	a, okA := pos["alpha"]
	b, okB := pos["beta"]
	if !okA || !okB {
		t.Fatal("missing columns")
	}
	if a.Y != b.Y || b.X <= a.X {
		t.Fatalf("fixed canvas reflowed: alpha x=%d y=%d beta x=%d y=%d", a.X, a.Y, b.X, b.Y)
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

func buildViewport(src string, width, viewportH int) *Layout {
	doc := ParseHTML(src)
	return BuildLayoutViewportDeadline(doc, nil, width, viewportH, false, time.Time{})
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

func fragOwnerID(f Frag) string {
	for n := f.Node; n != nil; n = n.Parent {
		if id := n.Attr("id"); id != "" {
			return id
		}
	}
	return ""
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

func TestExplicitHeightDoesNotGrow(t *testing.T) {
	ly := build(`<div style="height:16px"><div style="height:80px">tall</div></div>after`, 40)
	for _, f := range ly.Frags {
		if f.Kind == fragText && strings.Contains(f.Text, "after") {
			if f.Y != 1 {
				t.Fatalf("content after fixed-height box y = %d, want 1", f.Y)
			}
			return
		}
	}
	t.Fatal("trailing text missing")
}

func TestPercentageHeightAgainstDefiniteParent(t *testing.T) {
	doc := ParseHTML(`<div style="height:64px">` +
		`<div id="full" style="height:100%">full</div>` +
		`<div id="half" style="height:calc(50% - 8px)">half</div>` +
		`</div>` +
		`<div id="autoParent"><div id="auto" style="height:100%">auto</div></div>`)
	ly := BuildLayout(doc, nil, 40)
	box := func(id string) Rect { return ly.Boxes[findElementByID(doc, id)] }
	if got := box("full").H; got != 4 {
		t.Fatalf("height:100%% in a 64px parent = %d rows, want 4", got)
	}
	if got := box("half").H; got != 2 {
		t.Fatalf("height:calc(50%% - 8px) = %d rows, want 2", got)
	}
	if got := box("auto").H; got != 1 {
		t.Fatalf("height:100%% in an auto-height parent = %d rows, want the 1-row content", got)
	}
}

func TestFloatDropsBelowShorterFloat(t *testing.T) {
	doc := ParseHTML(`<div style="width:160px">` +
		`<div id="a" style="float:left;width:50%;height:48px">A</div>` +
		`<div id="b" style="float:left;width:50%;height:32px">B</div>` +
		`<div id="c" style="float:left;width:25%;height:32px">C</div>` +
		`<div id="d" style="float:left;width:25%;height:32px">D</div>` +
		`</div>`)
	ly := BuildLayout(doc, nil, 40)
	r := func(id string) Rect { return ly.Boxes[findElementByID(doc, id)] }
	a, b, c, d := r("a"), r("b"), r("c"), r("d")
	if b.Y != a.Y || b.X != a.X+a.W {
		t.Fatalf("second float = %+v, want it beside the first at %d,%d", b, a.X+a.W, a.Y)
	}
	if c.Y != b.Y+b.H {
		t.Fatalf("third float y = %d, want it under the shorter float at %d", c.Y, b.Y+b.H)
	}
	if c.X != b.X {
		t.Fatalf("third float x = %d, want the right column at %d", c.X, b.X)
	}
	if d.X != c.X+c.W {
		t.Fatalf("fourth float x = %d, want it beside the third at %d", d.X, c.X+c.W)
	}
}

func TestZoomScalesBoxesNotText(t *testing.T) {
	defer setLayoutZoom(1)
	src := `<div style="width:80px;padding:16px">hi</div>`
	measure := func(bigText bool) (boxW, textW, textH int) {
		root := ParseHTML(src)
		var ly *Layout
		if bigText {
			ly = BuildLayoutBig(root, nil, 100, true)
		} else {
			ly = BuildLayout(root, nil, 100)
		}
		if r, ok := ly.Boxes[root.FindFirst("div")]; ok {
			boxW = r.W
		}
		for i := range ly.Frags {
			f := &ly.Frags[i]
			if f.Kind == fragText && strings.TrimSpace(f.Text) != "" {
				return boxW, f.W, f.H
			}
		}
		return boxW, 0, 0
	}

	setLayoutZoom(1)
	box1, text1, h1 := measure(false)
	// 80px is 10 cells plus 2+2 cells of padding.
	if box1 != 14 {
		t.Fatalf("zoom 1 box = %d cells, want 14", box1)
	}
	if text1 != 2 || h1 != 1 {
		t.Fatalf("zoom 1 text = %d cells x %d rows, want 2x1", text1, h1)
	}

	setLayoutZoom(2)
	box2, text2, h2 := measure(false)
	// Fixed pixels and padding double; the word keeps its two cells and row.
	if box2 != 28 {
		t.Fatalf("zoom 2 box = %d cells, want 28", box2)
	}
	if text2 != text1 || h2 != h1 {
		t.Fatalf("zoom 2 text = %d cells x %d rows, want %dx%d", text2, h2, text1, h1)
	}

	// A scaled heading grows with the zoom while body text does not.
	bigRoot := ParseHTML(`<h1>head</h1><p>body</p>`)
	setLayoutZoom(1)
	base := BuildLayoutBig(bigRoot, nil, 100, true)
	headW1, headH1 := fragSize(base, "head")
	bodyW1, bodyH1 := fragSize(base, "body")
	setLayoutZoom(2)
	zoomed := BuildLayoutBig(bigRoot, nil, 100, true)
	headW2, headH2 := fragSize(zoomed, "head")
	bodyW2, bodyH2 := fragSize(zoomed, "body")
	if headW1 != 8 || headH1 != 2 {
		t.Fatalf("heading at zoom 1 = %dx%d, want 8x2", headW1, headH1)
	}
	if headW2 != 16 || headH2 != 4 {
		t.Fatalf("heading at zoom 2 = %dx%d, want 16x4", headW2, headH2)
	}
	if bodyW1 != bodyW2 || bodyH1 != bodyH2 {
		t.Fatalf("body text changed with zoom: %dx%d -> %dx%d", bodyW1, bodyH1, bodyW2, bodyH2)
	}
}

func fragSize(ly *Layout, text string) (w, h int) {
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind == fragText && strings.TrimSpace(f.Text) == text {
			return f.W, f.H
		}
	}
	return 0, 0
}

func TestZoomShrinksMediaViewport(t *testing.T) {
	defer setLayoutZoom(1)
	rules := ParseStylesheet(`@media (max-width: 500px) { .m { display:none } }`)
	atZoom := func(z float64) string {
		setLayoutZoom(z)
		doc := ParseHTML(`<p class="m">x</p>`)
		st := BuildLayout(doc, rules, 100).Styles[doc.FindFirst("p")]
		if st == nil {
			t.Fatal("no style")
		}
		return st.Display
	}
	if got := atZoom(1); got == "none" {
		t.Fatalf("80 columns at 100%% zoom reported a narrow viewport")
	}
	if got := atZoom(2); got != "none" {
		t.Fatalf("80 columns at 200%% zoom = %q, want the narrow media query to match", got)
	}
}

// TestStickyTopFollowsScroll checks the sticky offset, its containing-block
// clamp, and that a scrolled sticky header paints above the text underneath.
func TestStickyTopFollowsScroll(t *testing.T) {
	root := ParseHTML(`<html><head><style>
	body { margin: 0; }
	.stick { position: sticky; top: 0; background: #dddddd; }
	.tail p { margin: 0; }
	</style></head><body>
	<div class="wrap"><div class="stick">HEAD</div>
	<div class="tail"><p>one</p><p>two</p><p>three</p><p>four</p></div></div>
	</body></html>`)
	ly := BuildLayout(root, fixtureAuthorRules(root), 40)
	stick, _ := findSelectorNode(root, ".stick")
	box, ok := ly.Boxes[stick]
	if !ok {
		t.Fatal("sticky box was not laid out")
	}
	idx := -1
	for i := range ly.Frags {
		if f := &ly.Frags[i]; f.Kind == fragText && f.Text == "HEAD" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("sticky header text not found")
	}
	base := ly.Frags[idx].Y

	ly.SetFixedScroll(2)
	if got := ly.Frags[idx].Y; got != base+2 {
		t.Fatalf("sticky Y at scroll 2 = %d, want %d", got, base+2)
	}

	ly.SetFixedScroll(1000)
	wrap, _ := findSelectorNode(root, ".wrap")
	cb := ly.Boxes[wrap]
	want := cb.Y + cb.H - box.H
	if got := ly.Frags[idx].Y; got != want {
		t.Fatalf("sticky Y at the containing-block end = %d, want %d", got, want)
	}

	ly.SetFixedScroll(0)
	if got := ly.Frags[idx].Y; got != base {
		t.Fatalf("sticky Y after scrolling back = %d, want %d", got, base)
	}

	// Scroll so the header overlaps the first line, then paint the viewport.
	ly.SetFixedScroll(2)
	pageFG := RGB(0x1f, 0x23, 0x28)
	if ly.Body != nil && ly.Body.Style != nil && ly.Body.Style.Color.Set {
		pageFG = ly.Body.Style.Color
	}
	g := NewGrid(40, 12, pageFG, RGB(255, 255, 255))
	PaintLayout(g, ly, 2, 0, 12, nil, pageFG)
	top := g.Lines(false)[0]
	if !strings.Contains(top, "HEAD") {
		t.Fatalf("sticky header did not paint above the scrolled text: %q", top)
	}
	if strings.Contains(top, "two") {
		t.Fatalf("content shows through the sticky fill: %q", top)
	}
}

// TestZIndexPaintOrder checks that positioned boxes paint in z-index order
// even when the higher layer comes first in the DOM, and that hit testing
// follows the visible layer.
func TestZIndexPaintOrder(t *testing.T) {
	root := ParseHTML(`<html><head><style>
	body { margin: 0; }
	.card { position: absolute; top: 0; left: 0; background: #ffffff; }
	.high { z-index: 2; }
	.low { z-index: 1; }
	</style></head><body>
	<div class="card high">HIGH</div>
	<div class="card low">LOW</div>
	</body></html>`)
	ly := BuildLayout(root, fixtureAuthorRules(root), 20)
	if len(ly.paintOrder) == 0 {
		t.Fatal("z-index page has no paint order")
	}
	pageFG := RGB(0x1f, 0x23, 0x28)
	g := NewGrid(20, 3, pageFG, RGB(255, 255, 255))
	PaintLayout(g, ly, 0, 0, 3, nil, pageFG)
	if top := g.Lines(false)[0]; !strings.HasPrefix(top, "HIGH") {
		t.Fatalf("painted top row = %q, want the higher z-index text", top)
	}

	b := NewBrowser(20, 3)
	b.page = &Page{URL: mustParse(t, "https://example.com/"), Root: root}
	b.layout = ly
	n := b.nodeAt(0, 0)
	if n == nil || !strings.Contains(n.Attr("class"), "high") {
		t.Fatalf("hit testing picked %+v, want the higher z-index box", n)
	}
}
