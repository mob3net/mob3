package browser

import (
	"strings"
	"testing"
)

func hintTestBrowser(t *testing.T, html string, width, height int) *Browser {
	t.Helper()
	root := ParseHTML(html)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(width, height)
	b.page = &Page{URL: u, Base: u, Root: root}
	b.layout = BuildLayout(root, nil, width)
	return b
}

func TestLinkHintsPickLetterFromText(t *testing.T) {
	b := hintTestBrowser(t, `<html><body>
<p><a href="#one">Documentation</a></p>
<div id="one">target</div>
</body></html>`, 50, 20)

	b.HandleKey("f")
	if !b.linkHints || len(b.hintItems) != 1 {
		t.Fatalf("hints = %v, want one active hint", b.linkHints)
	}
	if got := b.hintItems[0].label; got != "d" {
		t.Fatalf("label = %q, want d from Documentation", got)
	}
	// The label is case-insensitive, even when the page uses an uppercase D.
	b.HandleKey("D")
	if b.linkHints {
		t.Fatal("following a hint left hint mode on")
	}
	if b.focus != 0 {
		t.Fatalf("focus = %d, want 0", b.focus)
	}

	// Reopening and typing the lowercase form works too.
	b.HandleKey("f")
	b.HandleKey("d")
	if b.linkHints || b.focus != 0 {
		t.Fatalf("lowercase hint: active=%v focus=%d", b.linkHints, b.focus)
	}
}

func TestLinkHintsSkipUsedLetters(t *testing.T) {
	b := hintTestBrowser(t, `<html><body>
<p><a href="#one">Documentation</a> <a href="#two">Dog</a></p>
</body></html>`, 50, 20)

	b.HandleKey("f")
	if len(b.hintItems) != 2 {
		t.Fatalf("hints = %d, want 2", len(b.hintItems))
	}
	if got := b.hintItems[0].label; got != "d" {
		t.Fatalf("first label = %q, want d", got)
	}
	if got := b.hintItems[1].label; got != "o" {
		t.Fatalf("second label = %q, want o because d is already used", got)
	}
	b.HandleKey("o")
	if b.linkHints || b.focus != 1 {
		t.Fatalf("following o: active=%v focus=%d", b.linkHints, b.focus)
	}
}

func TestLinkHintsTwoLettersAndFiltering(t *testing.T) {
	var doc strings.Builder
	doc.WriteString("<html><body>")
	for i := 0; i < 28; i++ {
		doc.WriteString(`<div><a href="#x">`)
		doc.WriteString(string(rune('a' + i%26)))
		doc.WriteString("link</a></div>")
	}
	doc.WriteString("</body></html>")
	b := hintTestBrowser(t, doc.String(), 40, 60)
	b.HandleKey("f")
	if len(b.hintItems) != 28 {
		t.Fatalf("hints = %d, want 28", len(b.hintItems))
	}
	seen := map[string]bool{}
	prefixes := map[byte]int{}
	for _, h := range b.hintItems {
		if len(h.label) < 1 || len(h.label) > 2 {
			t.Fatalf("label %q is not one or two letters", h.label)
		}
		if seen[h.label] {
			t.Fatalf("duplicate label %q", h.label)
		}
		seen[h.label] = true
		prefixes[h.label[0]]++
		if len(h.chars) != len(h.label) {
			t.Fatalf("label %q highlights %d chars, want %d", h.label, len(h.chars), len(h.label))
		}
	}
	for _, h := range b.hintItems {
		if prefixes[h.label[0]] == 1 && len(h.label) != 1 {
			t.Fatalf("label %q has a unique first letter but is not shortened", h.label)
		}
	}
	h := b.hintItems[27]
	for _, r := range h.label {
		b.HandleKey(string(r))
	}
	if b.linkHints {
		t.Fatal("typing the hint did not follow the link")
	}
	if b.focus != 27 {
		t.Fatalf("focus = %d, want link 27 for %q", b.focus, h.label)
	}
}

func TestLinkHintsIgnoreUnknownLetter(t *testing.T) {
	b := hintTestBrowser(t, `<html><body>
<p><a href="#one">one</a> <a href="#two">two</a></p>
</body></html>`, 50, 20)
	b.HandleKey("f")
	b.HandleKey("z")
	if !b.linkHints || b.hintPrefix != "" {
		t.Fatalf("unknown letter changed hint state: active=%v prefix=%q", b.linkHints, b.hintPrefix)
	}
}

func TestLinkHintsClearOnEscapeAndScroll(t *testing.T) {
	var doc strings.Builder
	doc.WriteString(`<html><body><p><a href="#one">one</a></p>`)
	for i := 0; i < 40; i++ {
		doc.WriteString("<p>filler line</p>")
	}
	doc.WriteString("</body></html>")
	b := hintTestBrowser(t, doc.String(), 50, 10)
	b.HandleKey("f")
	b.HandleKey("esc")
	if b.linkHints || len(b.hintItems) != 0 {
		t.Fatal("Esc did not clear link hints")
	}
	if b.status != exitHint {
		t.Fatalf("status = %q, want the exit hint", b.status)
	}

	b.HandleKey("f")
	b.HandleMouse(MouseEvent{X: 5, Y: 5, Wheel: 1})
	if b.linkHints {
		t.Fatal("mouse wheel scrolling did not clear link hints")
	}
	if b.scroll != 3 {
		t.Fatalf("scroll = %d, want the wheel scroll to still happen", b.scroll)
	}
}

func TestLinkHintsOnlyVisibleLinks(t *testing.T) {
	var doc strings.Builder
	doc.WriteString(`<html><body><p><a href="#top">top</a></p>`)
	for i := 0; i < 40; i++ {
		doc.WriteString("<p>filler line</p>")
	}
	doc.WriteString(`<p><a href="#bottom">bottom</a></p></body></html>`)
	b := hintTestBrowser(t, doc.String(), 40, 8)
	if len(b.layout.Links) != 2 {
		t.Fatalf("page links = %d, want 2", len(b.layout.Links))
	}
	b.HandleKey("f")
	if len(b.hintItems) != 1 || b.hintItems[0].target != 0 {
		t.Fatalf("visible hints = %+v, want only the top link", b.hintItems)
	}
}

func TestPickPairUsesAdjacentCells(t *testing.T) {
	glyphs := []hintGlyph{
		{ch: 'u', r: 'U', x: 1, y: 1},
		{ch: 'p', r: 'p', x: 2, y: 1},
		{ch: 'g', r: 'g', x: 3, y: 1},
		{ch: 'r', r: 'r', x: 4, y: 1},
	}
	label, chars := pickPair(glyphs, map[string]bool{"up": true})
	if label != "pg" {
		t.Fatalf("label = %q, want pg", label)
	}
	if len(chars) != 2 || chars[0].r != 'p' || chars[1].r != 'g' {
		t.Fatalf("chars = %+v, want p and g", chars)
	}
	if chars[0].y != chars[1].y || chars[1].x != chars[0].x+1 {
		t.Fatalf("hint letters are not adjacent: %+v", chars)
	}
}

func TestShortenUniquePairHints(t *testing.T) {
	hints := []linkHint{
		{label: "fi", chars: []hintChar{{r: 'F'}, {r: 'i'}}},
		{label: "pg", chars: []hintChar{{r: 'p'}, {r: 'g'}}},
		{label: "pa", chars: []hintChar{{r: 'p'}, {r: 'a'}}},
	}
	shortenUniquePairs(hints)
	if hints[0].label != "f" || len(hints[0].chars) != 1 {
		t.Fatalf("unique hint = %+v, want shortened to f", hints[0])
	}
	if hints[1].label != "pg" || hints[2].label != "pa" {
		t.Fatalf("shared-prefix hints should stay two letters: %+v", hints)
	}
}

func TestLinkHintsDisableInlineImages(t *testing.T) {
	b := NewBrowser(40, 10)
	b.gfx = NewGFXRenderer(GraphicsKitty, 8, 16)
	if !b.inlineImagesEnabled() {
		t.Fatal("inline images should be enabled outside hint mode")
	}
	b.linkHints = true
	if b.inlineImagesEnabled() {
		t.Fatal("inline images should be disabled while hints are shown")
	}
}

func TestModalsDisableInlineImages(t *testing.T) {
	b := NewBrowser(40, 10)
	b.gfx = NewGFXRenderer(GraphicsKitty, 8, 16)
	modals := []struct {
		name  string
		set   func()
		clear func()
	}{
		{"dialog", func() { b.dialog = &jsDialog{} }, func() { b.dialog = nil }},
		{"download", func() { b.download = &downloadDialog{} }, func() { b.download = nil }},
		{"notice", func() { b.notice = &noticeDialog{} }, func() { b.notice = nil }},
		{"select", func() { b.selectMenu = &selectMenuDialog{} }, func() { b.selectMenu = nil }},
	}
	for _, m := range modals {
		m.set()
		if b.inlineImagesEnabled() {
			t.Fatalf("%s: inline images should be disabled while a modal is open", m.name)
		}
		m.clear()
		if !b.inlineImagesEnabled() {
			t.Fatalf("%s: inline images should resume after the modal closes", m.name)
		}
	}
}

func TestLinkHintsNoTextFreeLetters(t *testing.T) {
	hints := buildLinkHints([]int{0, 1}, map[int][]hintGlyph{
		0: {{ch: 'd', r: 'D', x: 1, y: 1}},
	})
	if len(hints) != 2 {
		t.Fatalf("hints = %d, want 2", len(hints))
	}
	if hints[0].label != "d" {
		t.Fatalf("text link label = %q, want d", hints[0].label)
	}
	if hints[1].label != "a" || len(hints[1].chars) != 0 {
		t.Fatalf("no-text link = %+v, want free letter a", hints[1])
	}
}

func TestLinkHintsOperateFormControls(t *testing.T) {
	b := hintTestBrowser(t, `<html><body>
<form action="/go">
<input type="checkbox" name="check">
<input type="radio" name="pick" value="one" checked>
<input type="radio" name="pick" value="two">
<select multiple name="multi"><option selected>Alpha</option><option>Beta</option></select>
<select name="one"><option>Gamma</option><option>Delta</option></select>
<button type="button">Press</button>
</form></body></html>`, 60, 30)
	doc := b.page.Root
	check := doc.FindFirst("input")
	radios := doc.FindAll(func(n *Node) bool { return n.Tag == "input" && n.Attr("type") == "radio" })
	multi := doc.FindFirst("select")
	single := doc.FindAll(func(n *Node) bool { return n.Tag == "select" && !n.HasAttr("multiple") })
	if len(radios) != 2 || multi == nil || len(single) != 1 || check == nil {
		t.Fatal("test form is incomplete")
	}
	beta := selectOptions(multi)[1]
	choose := func(match func(*Node) bool) {
		t.Helper()
		b.HandleKey("f")
		if !b.linkHints {
			t.Fatal("f did not open hints")
		}
		label := ""
		for _, h := range b.hintItems {
			if target := b.hintTargetAt(h.target); target.node != nil && match(target.node) {
				label = h.label
				break
			}
		}
		if label == "" {
			t.Fatalf("no hint for control")
		}
		for _, ch := range label {
			b.HandleKey(string(ch))
		}
	}

	choose(func(n *Node) bool { return n == check })
	if !check.HasAttr("checked") {
		t.Fatal("checkbox hint did not toggle it")
	}
	choose(func(n *Node) bool { return n == radios[1] })
	if !radios[1].HasAttr("checked") || radios[0].HasAttr("checked") {
		t.Fatal("radio hint did not select it")
	}
	choose(func(n *Node) bool { return n == beta })
	if !beta.HasAttr("selected") {
		t.Fatal("multi-select hint did not toggle the option")
	}
	choose(func(n *Node) bool { return n == single[0] })
	if b.selectMenu == nil {
		t.Fatal("select hint did not open the option popup")
	}
}

func TestLinkHintsFocusInputFields(t *testing.T) {
	b := hintTestBrowser(t, `<html><body>
<p><a href="#one">Documentation</a></p>
<form action="/go"><input name="q" value="Search here"></form>
</body></html>`, 50, 20)

	b.HandleKey("f")
	if !b.linkHints {
		t.Fatal("f did not open hints")
	}
	var input *Node
	label := ""
	for _, h := range b.hintItems {
		if target := b.hintTargetAt(h.target); target.node != nil {
			input = target.node
			label = h.label
			break
		}
	}
	if input == nil || label == "" {
		t.Fatalf("input field has no hint: %+v", b.hintItems)
	}
	for _, ch := range label {
		b.HandleKey(string(ch))
	}
	if b.inputFocus != input {
		t.Fatalf("hint did not focus the input: %v", b.inputFocus)
	}
	if b.linkHints {
		t.Fatal("hint mode stayed active after focusing a field")
	}
}

func TestPaintLinkHintsHidesTypedPrefix(t *testing.T) {
	b := NewBrowser(40, 10)
	b.layout = &Layout{Links: make([]*LinkInfo, 3)}
	b.linkHints = true
	b.hintPrefix = "g"
	b.hintItems = []linkHint{
		{target: 0, label: "ab", chars: []hintChar{{r: 'a', x: 1}, {r: 'b', x: 2}}},
		{target: 1, label: "gx", chars: []hintChar{{r: 'g', x: 5}, {r: 'x', x: 6}}},
		{target: 2, label: "gy", chars: []hintChar{{r: 'g', x: 8}, {r: 'y', x: 9}}},
	}
	g := NewGrid(40, 10, RGB(0, 0, 0), RGB(255, 255, 255))
	b.paintLinkHints(g, 0, 10)
	yellow := RGB(0xff, 0xd7, 0x00)
	for _, x := range []int{1, 2, 5, 8} {
		if g.Cells[x].BG == yellow {
			t.Fatalf("cell %d should not be highlighted after the prefix", x)
		}
	}
	for _, x := range []int{6, 9} {
		if g.Cells[x].BG != yellow {
			t.Fatalf("cell %d should show the remaining hint letter", x)
		}
	}
}

func TestPaintLinkHintFallbackInsideBox(t *testing.T) {
	b := NewBrowser(40, 10)
	b.layout = &Layout{Links: []*LinkInfo{{Rects: []Rect{{X: 3, Y: 0, W: 10, H: 1}}}}}
	b.linkHints = true
	b.hintItems = []linkHint{{target: 0, label: "bx"}}
	g := NewGrid(40, 10, RGB(0, 0, 0), RGB(255, 255, 255))
	b.paintLinkHints(g, 0, 10)
	for i, want := range "bx" {
		c := g.Cells[3+i]
		if c.R != want || c.BG != RGB(0xff, 0xd7, 0x00) {
			t.Fatalf("fallback cell %d = %+v, want %q on yellow", i, c, want)
		}
	}

	// After the first letter, only the remaining letter stays visible.
	b.hintPrefix = "b"
	g = NewGrid(40, 10, RGB(0, 0, 0), RGB(255, 255, 255))
	b.paintLinkHints(g, 0, 10)
	if c := g.Cells[3]; c.R != 'x' || c.BG != RGB(0xff, 0xd7, 0x00) {
		t.Fatalf("fallback remainder = %+v, want x on yellow", c)
	}
	if c := g.Cells[4]; c.BG == RGB(0xff, 0xd7, 0x00) {
		t.Fatal("the typed fallback letter should not stay highlighted")
	}
}

func TestPaintLinkHintsHighlightsText(t *testing.T) {
	b := hintTestBrowser(t, `<html><body><p><a href="#one">Documentation</a></p></body></html>`, 40, 10)
	b.HandleKey("f")
	top, bottom := b.viewport()
	g := NewGrid(b.width, b.height, RGB(0, 0, 0), RGB(255, 255, 255))
	PaintLayoutBig(g, b.layout, b.scroll, top, bottom, nil, RGB(0, 0, 0), nil, false)
	b.paintLinkHints(g, top, bottom)
	r := b.layout.Links[0].Rects[0]
	y := r.Y - b.scroll + top

	var row strings.Builder
	for x := r.X; x < r.X+r.W; x++ {
		row.WriteRune(g.Cells[y*g.W+x].R)
	}
	if got := row.String(); got != "Documentation" {
		t.Fatalf("link text = %q, want it unchanged", got)
	}
	d := g.Cells[y*g.W+r.X]
	if d.R != 'D' || d.BG != RGB(0xff, 0xd7, 0x00) || !d.Underline {
		t.Fatalf("first glyph = %+v, want background-highlighted underlined D", d)
	}
	o := g.Cells[y*g.W+r.X+1]
	if o.BG == RGB(0xff, 0xd7, 0x00) {
		t.Fatal("the unselected o should keep its normal style")
	}
}

func TestCapitalFConfirmsBeforeFollowing(t *testing.T) {
	b := hintTestBrowser(t, `<html><body>
<p><a href="#one">Documentation</a></p>
<div id="one">target</div>
</body></html>`, 50, 20)
	b.HandleKey("F")
	if !b.linkHints || !b.hintAsk {
		t.Fatal("F did not open confirmation hint mode")
	}
	b.HandleKey("d")
	if b.linkHints || !b.hintConfirm {
		t.Fatalf("choosing a hint: active=%v confirm=%v", b.linkHints, b.hintConfirm)
	}
	if b.focus != -1 {
		t.Fatalf("focus = %d, want no navigation before confirmation", b.focus)
	}
	if strings.HasPrefix(b.status, "Follow ") {
		t.Fatalf("status still starts with Follow: %q", b.status)
	}
	if !strings.Contains(b.status, "https://example.com/#one") {
		t.Fatalf("status = %q, want the resolved target", b.status)
	}

	// Timer-driven rebuilds must not discard the pending confirmation.
	b.rebuild()
	if !b.hintConfirm {
		t.Fatal("rebuild cleared the confirmation")
	}
	if !strings.Contains(b.status, "https://example.com/#one") {
		t.Fatalf("status after rebuild = %q, want the resolved target", b.status)
	}

	b.HandleKey("enter")
	if b.hintConfirm {
		t.Fatal("Enter did not confirm the link")
	}
	if b.focus != 0 {
		t.Fatalf("focus = %d, want 0 after confirmation", b.focus)
	}
}

func TestCapitalFConfirmationCancel(t *testing.T) {
	b := hintTestBrowser(t, `<html><body>
<p><a href="#one">Documentation</a></p>
<div id="one">target</div>
</body></html>`, 50, 20)
	b.HandleKey("F")
	b.HandleKey("d")
	b.HandleKey("esc")
	if b.hintConfirm {
		t.Fatal("Esc left the confirmation open")
	}
	if b.focus != -1 {
		t.Fatalf("focus = %d, want no navigation after cancel", b.focus)
	}
	if b.status != exitHint {
		t.Fatalf("status = %q, want the exit hint", b.status)
	}
}

func TestLinkHintsSurviveRebuild(t *testing.T) {
	var doc strings.Builder
	doc.WriteString("<html><body>")
	for i := 0; i < 27; i++ {
		doc.WriteString(`<div><a href="#x">Upgrading</a></div>`)
	}
	doc.WriteString("</body></html>")
	b := hintTestBrowser(t, doc.String(), 40, 60)
	b.HandleKey("f")
	if !b.linkHints {
		t.Fatal("f did not open hint mode")
	}
	b.rebuild()
	if !b.linkHints || len(b.hintItems) != 27 {
		t.Fatalf("rebuild dismissed hints: active=%v hints=%d", b.linkHints, len(b.hintItems))
	}

	b.HandleKey("a")
	if !b.linkHints || b.hintPrefix != "a" {
		t.Fatalf("typing a: active=%v prefix=%q", b.linkHints, b.hintPrefix)
	}
	b.rebuild()
	if !b.linkHints || b.hintPrefix != "a" {
		t.Fatalf("rebuild lost the typed prefix: active=%v prefix=%q", b.linkHints, b.hintPrefix)
	}

	// Scrolling is still an explicit dismissal.
	b.scrollBy(1)
	if b.linkHints {
		t.Fatal("scrolling did not dismiss the hints")
	}
}
