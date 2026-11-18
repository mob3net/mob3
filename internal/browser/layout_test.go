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
