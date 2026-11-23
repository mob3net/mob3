package browser

import (
	"browser/internal/engine"
	"strings"
	"testing"
	"time"
)

func hintTestBrowser(t *testing.T, html string, width, height int) *Browser {
	t.Helper()
	root := ParseHTML(html)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(width, height)
	b.Page = &Page{URL: u, Base: u, Root: root}
	b.Layout = BuildLayout(root, nil, width)
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
	if got := b.hintItems[0].Label; got != "d" {
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
	if got := b.hintItems[0].Label; got != "d" {
		t.Fatalf("first label = %q, want d", got)
	}
	if got := b.hintItems[1].Label; got != "o" {
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
		if len(h.Label) < 1 || len(h.Label) > 2 {
			t.Fatalf("label %q is not one or two letters", h.Label)
		}
		if seen[h.Label] {
			t.Fatalf("duplicate label %q", h.Label)
		}
		seen[h.Label] = true
		prefixes[h.Label[0]]++
		if len(h.Chars) != len(h.Label) {
			t.Fatalf("label %q highlights %d chars, want %d", h.Label, len(h.Chars), len(h.Label))
		}
	}
	for _, h := range b.hintItems {
		if prefixes[h.Label[0]] == 1 && len(h.Label) != 1 {
			t.Fatalf("label %q has a unique first letter but is not shortened", h.Label)
		}
	}
	h := b.hintItems[27]
	for _, r := range h.Label {
		b.HandleKey(string(r))
	}
	if b.linkHints {
		t.Fatal("typing the hint did not follow the link")
	}
	if b.focus != 27 {
		t.Fatalf("focus = %d, want link 27 for %q", b.focus, h.Label)
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
	if b.Scroll != 3 {
		t.Fatalf("scroll = %d, want the wheel scroll to still happen", b.Scroll)
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
	if len(b.Layout.Links) != 2 {
		t.Fatalf("page links = %d, want 2", len(b.Layout.Links))
	}
	b.HandleKey("f")
	if len(b.hintItems) != 1 || b.hintItems[0].Target != 0 {
		t.Fatalf("visible hints = %+v, want only the top link", b.hintItems)
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
		{"view", func() { b.viewMenu = &viewMenuDialog{} }, func() { b.viewMenu = nil }},
		{"zoom", func() { b.zoomMenu = &zoomMenuDialog{} }, func() { b.zoomMenu = nil }},
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
	doc := b.Page.Root
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
			if target := b.hintTargetAt(h.Target); target.Node != nil && match(target.Node) {
				label = h.Label
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
	if b.forms.ControlFocus != check {
		t.Fatalf("checkbox hint did not focus it: %v", b.forms.ControlFocus)
	}
	b.HandleKey(" ")
	if check.HasAttr("checked") {
		t.Fatal("space did not toggle the focused checkbox")
	}
	choose(func(n *Node) bool { return n == radios[1] })
	if !radios[1].HasAttr("checked") || radios[0].HasAttr("checked") {
		t.Fatal("radio hint did not select it")
	}
	if b.forms.ControlFocus != radios[1] {
		t.Fatalf("radio hint did not focus it: %v", b.forms.ControlFocus)
	}
	choose(func(n *Node) bool { return n == beta })
	if !beta.HasAttr("selected") {
		t.Fatal("multi-select hint did not toggle the option")
	}
	if b.forms.ControlFocus != multi || b.forms.ControlOption != beta {
		t.Fatalf("multi-select hint did not focus the option: control=%v option=%v", b.forms.ControlFocus, b.forms.ControlOption)
	}
	choose(func(n *Node) bool { return n == single[0] })
	if b.selectMenu == nil {
		t.Fatal("select hint did not open the option popup")
	}
	if b.forms.ControlFocus != single[0] {
		t.Fatalf("select hint did not focus it: %v", b.forms.ControlFocus)
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
		if target := b.hintTargetAt(h.Target); target.Node != nil {
			input = target.Node
			label = h.Label
			break
		}
	}
	if input == nil || label == "" {
		t.Fatalf("input field has no hint: %+v", b.hintItems)
	}
	for _, ch := range label {
		b.HandleKey(string(ch))
	}
	if b.forms.InputFocus != input {
		t.Fatalf("hint did not focus the input: %v", b.forms.InputFocus)
	}
	if b.linkHints {
		t.Fatal("hint mode stayed active after focusing a field")
	}
}

func TestPaintLinkHintsHidesTypedPrefix(t *testing.T) {
	b := NewBrowser(40, 10)
	b.Layout = &Layout{Links: make([]*LinkInfo, 3)}
	b.linkHints = true
	b.hintPrefix = "g"
	b.hintItems = []linkHint{
		{Target: 0, Label: "ab", Chars: []hintChar{{R: 'a', X: 1}, {R: 'b', X: 2}}},
		{Target: 1, Label: "gx", Chars: []hintChar{{R: 'g', X: 5}, {R: 'x', X: 6}}},
		{Target: 2, Label: "gy", Chars: []hintChar{{R: 'g', X: 8}, {R: 'y', X: 9}}},
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
	b.Layout = &Layout{Links: []*LinkInfo{{Rects: []Rect{{X: 3, Y: 0, W: 10, H: 1}}}}}
	b.linkHints = true
	b.hintItems = []linkHint{{Target: 0, Label: "bx"}}
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
	PaintLayoutBig(g, b.Layout, b.Scroll, top, bottom, nil, RGB(0, 0, 0), nil, false)
	b.paintLinkHints(g, top, bottom)
	r := b.Layout.Links[0].Rects[0]
	y := r.Y - b.Scroll + top

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

func TestHintsIncludeFixedBarControls(t *testing.T) {
	doc := ParseHTML(`<div style="height:400px">tall content</div>
		<div style="position:fixed;bottom:0;right:0"><button id="ok">Got it</button>
		<a href="/docs">Docs</a></div>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 15)
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.Layout = BuildLayoutViewportDeadline(doc, nil, 60, 12, false, time.Time{})

	find := func() (button, link bool) {
		b.openLinkHints(false)
		for _, tgt := range b.hintTargets {
			if tgt.Node != nil && tgt.Node.Attr("id") == "ok" {
				button = true
			}
			if tgt.Node == nil && tgt.Link >= 0 {
				link = true
			}
		}
		return button, link
	}
	if button, link := find(); !button || !link {
		t.Fatalf("fixed bar hints: button=%v link=%v", button, link)
	}
	// The same must hold once the page is scrolled.
	b.Scroll = 40
	if button, link := find(); !button || !link {
		t.Fatalf("scrolled fixed bar hints: button=%v link=%v", button, link)
	}
}

// TestColumnFlexButtonIsHintable covers controls blockified by a column flex
// container: they must keep the inline widget fragment, or link hints have no
// target for the button and the widget's own rendering is lost.
func TestColumnFlexButtonIsHintable(t *testing.T) {
	u := mustParse(t, "https://example.com/")
	root := ParseHTML(`<html><head><style>
	#bar { display: flex; flex-direction: column; position: fixed; right: 0; bottom: 0; }
	#bar .text { width: 100%; }
	</style></head><body>
	<p>page text</p>
	<div id="bar"><div class="text">Cookies are used here.</div>
	<button id="ok" type="button">Got it</button></div>
	</body></html>`)
	b := NewBrowser(60, 24)
	b.Page = &Page{URL: u, Base: u, Root: root}
	b.URL = u
	b.rebuild()

	var btn *Node
	for _, n := range root.FindAll(func(n *Node) bool { return n.Attr("id") == "ok" }) {
		btn = n
		break
	}
	if btn == nil {
		t.Fatal("button not found")
	}
	top, bottom := b.viewport()
	if _, ok := engine.VisibleInputRect(b.Layout, btn, b.Scroll, top, bottom); !ok {
		t.Fatal("fixed column-flex button has no widget fragment")
	}
	b.openLinkHints(false)
	for _, target := range b.hintTargets {
		if target.Node == btn {
			return
		}
	}
	t.Fatal("fixed column-flex button is not a link-hint target")
}
