package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// drawQuiet runs draw while discarding terminal output.
func drawQuiet(b *Browser) {
	old := os.Stdout
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err == nil {
		os.Stdout = devnull
		defer func() { os.Stdout = old; devnull.Close() }()
	}
	b.draw()
}

func TestHeaderHitRegions(t *testing.T) {
	b := NewBrowser(60, 10)
	b.loader.bookmarks = []Bookmark{{Title: "x", URL: "https://example.com/"}}
	u, _ := url.Parse("https://example.com/a")
	b.url = u
	drawQuiet(b)

	if len(b.headerHits) != 9 {
		t.Fatalf("header hits = %+v", b.headerHits)
	}
	bookW := stringWidth(" ★ ")
	histW := stringWidth(" ≡ ")
	helpW := stringWidth(" ? ")
	cfgW := stringWidth(" ⚙ ")
	want := []headerHit{
		{headerBack, 0, 0, 3, 1},
		{headerForward, 3, 0, 3, 1},
		{headerReload, 6, 0, 3, 1},
		{headerHome, 9, 0, 3, 1},
		{headerBookmarks, b.width - cfgW - helpW - histW - bookW, 0, bookW, 1},
		{headerHistory, b.width - cfgW - helpW - histW, 0, histW, 1},
		{headerHelp, b.width - cfgW - helpW, 0, helpW, 1},
		{headerConfig, b.width - cfgW, 0, cfgW, 1},
		{headerURL, 0, 1, 60, 1},
	}
	for i := range want {
		if b.headerHits[i] != want[i] {
			t.Errorf("hit %d = %+v, want %+v", i, b.headerHits[i], want[i])
		}
	}
	if b.headerHits[7].x+b.headerHits[7].w != b.width {
		t.Error("config is not at the far right")
	}
	if top, _ := b.viewport(); top != 2 {
		t.Errorf("viewport top = %d, want 2 for a two-row bar", top)
	}
}

func TestBookmarksButtonHiddenWithoutFile(t *testing.T) {
	b := NewBrowser(100, 10)
	drawQuiet(b)
	for _, hit := range b.headerHits {
		if hit.kind == headerBookmarks {
			t.Fatalf("bookmarks button shown without -bookmarks: %+v", b.headerHits)
		}
	}
	b.loader.bookmarksSet = true
	drawQuiet(b)
	found := false
	for _, hit := range b.headerHits {
		if hit.kind == headerBookmarks {
			found = true
		}
	}
	if !found {
		t.Fatalf("bookmarks button missing after -bookmarks: %+v", b.headerHits)
	}
}

func TestHeaderWideIcons(t *testing.T) {
	b := NewBrowser(100, 10)
	drawQuiet(b)
	widths := map[headerKind]int{}
	for _, hit := range b.headerHits {
		widths[hit.kind] = hit.w
	}
	if widths[headerForward] != stringWidth(" → ") || widths[headerHome] != stringWidth(" ⌂ ") ||
		widths[headerReload] != stringWidth(" ⟳ ") {
		t.Fatalf("forward/reload/home widths = %d/%d/%d",
			widths[headerForward], widths[headerReload], widths[headerHome])
	}
	if widths[headerBack] != stringWidth(" ← Back ") {
		t.Fatalf("back width = %d", widths[headerBack])
	}
}

func TestConfigPageShowsValues(t *testing.T) {
	b := NewBrowser(80, 10)
	b.setLoader(NewLoader())
	b.home = "https://example.com/home"
	b.noJS = true
	b.jsMemLimit = 64 << 20
	b.noBigText = true
	b.loader.userAgent = "TestUA"
	b.loader.searchURL = "https://search.example/?q=%s"
	b.cliConfig = map[string]string{
		"-dump": "off", "-width": "80", "-graphics": "auto",
		"-bookmarks": "(none)", "-color": "off", "-no-color": "off",
		"-cpuprofile": "(none)", "-version": "dev",
	}
	page := b.configPage(mustParse(t, "about:config"), false)
	text := page.Root.TextContent()
	for _, want := range []string{"-dump", "-width", "-graphics", "-no-js", "-reader", "-dark", "-ignore-blocks",
		"-bookmarks", "-home", "-js-memory", "-cache", "-text-scale", "-color", "-no-color", "-ua",
		"-search", "-cpuprofile", "-version", "startup"} {
		if !strings.Contains(text, want) {
			t.Errorf("config page missing %q", want)
		}
	}
	values := map[string]string{}
	checked := map[string]bool{}
	for _, n := range page.Root.FindAll(func(n *Node) bool { return n.Tag == "input" }) {
		values[n.Attr("name")] = n.Attr("value")
		checked[n.Attr("name")] = n.HasAttr("checked")
	}
	selects := map[string]string{}
	for _, n := range page.Root.FindAll(func(n *Node) bool { return n.Tag == "select" }) {
		selects[n.Attr("name")] = inputValue(n)
	}
	want := map[string]string{
		"no-js": "on", "home": "https://example.com/home", "js-memory": "64", "cache": "32",
		"ua": "TestUA", "search": "https://search.example/?q=%s",
	}
	for k, v := range want {
		if values[k] != v {
			t.Errorf("input %s = %q, want %q", k, values[k], v)
		}
	}
	if selects["text-scale"] != "off" {
		t.Errorf("text-scale select = %q, want off", selects["text-scale"])
	}
	if !checked["no-js"] {
		t.Fatal("no-js checkbox is not checked")
	}
	for _, n := range page.Root.FindAll(func(n *Node) bool { return n.Tag == "input" }) {
		if n.Attr("name") == "no-js" && n.Attr("type") != "checkbox" {
			t.Fatalf("no-js input type = %q", n.Attr("type"))
		}
	}
}

func TestApplyConfig(t *testing.T) {
	b := NewBrowser(80, 10)
	b.setLoader(NewLoader())
	q := url.Values{
		"no-js":         {"on"},
		"ignore-blocks": {"on"},
		"home":          {"https://example.com/start"},
		"js-memory":     {"128"},
		"cache":         {"16"},
		"text-scale":    {"off"},
		"ua":            {"TestUA"},
		"search":        {""},
	}
	b.applyConfig(q)
	if !b.noJS || !b.loader.noScripts {
		t.Fatal("-no-js was not applied")
	}
	if !b.loader.ignoreBlocks {
		t.Fatal("-ignore-blocks was not applied")
	}
	if b.home != "https://example.com/start" {
		t.Fatalf("home = %q", b.home)
	}
	if b.jsMemLimit != 128<<20 {
		t.Fatalf("js memory = %d", b.jsMemLimit)
	}
	if got := b.loader.cacheLimitMB(); got != 16 {
		t.Fatalf("cache = %d MB, want 16", got)
	}
	if !b.noBigText {
		t.Fatal("text-scale off was not applied")
	}
	if b.loader.userAgent != "TestUA" {
		t.Fatalf("ua = %q", b.loader.userAgent)
	}
	if b.loader.searchURL != "" {
		t.Fatalf("search = %q", b.loader.searchURL)
	}
}

func TestConfigNavigation(t *testing.T) {
	b := NewBrowser(80, 10)
	b.setLoader(NewLoader())
	b.Navigate("about:config?home=https%3A%2F%2Fexample.com%2Fhome&js-memory=32", true)
	if b.page == nil || b.page.Title != "Config — MOB³" {
		t.Fatalf("config page = %+v", b.page)
	}
	if b.home != "https://example.com/home" {
		t.Fatalf("home = %q", b.home)
	}
	if b.jsMemLimit != 32<<20 {
		t.Fatalf("js memory = %d", b.jsMemLimit)
	}
	values := map[string]string{}
	for _, n := range b.page.Root.FindAll(func(n *Node) bool { return n.Tag == "input" }) {
		values[n.Attr("name")] = n.Attr("value")
	}
	if values["home"] != "https://example.com/home" || values["js-memory"] != "32" {
		t.Fatalf("config inputs = %+v", values)
	}
}

func TestConfigBlockedInReaderMode(t *testing.T) {
	b := NewBrowser(80, 10)
	u := mustParse(t, "https://example.com/article")
	b.page = &Page{URL: u, Base: u, Title: "Article"}
	b.url = u
	b.reader = true
	oldPage := b.page
	withQuietStdout(t, func() { b.Navigate("about:config", true) })
	if b.page != oldPage {
		t.Fatal("config navigation replaced the page in reader mode")
	}
	want := "Config is not available in reader mode — press R to turn it off"
	if b.status != want {
		t.Fatalf("status = %q, want %q", b.status, want)
	}
	if statusLevelFor(b.status) != statusWarning {
		t.Fatal("reader-mode config notice is not a warning")
	}

	cfg := NewBrowser(80, 10)
	cfgURL := mustParse(t, "about:config")
	cfg.page = &Page{URL: cfgURL, Base: cfgURL, Title: "Config — MOB³"}
	cfg.url = cfgURL
	cfg.HandleKey("R")
	if cfg.reader {
		t.Fatal("reader mode was enabled on the config page")
	}
	if cfg.status != "Reader mode is unavailable on the config page" {
		t.Fatalf("status = %q", cfg.status)
	}
}

func TestToggleCheckbox(t *testing.T) {
	b := NewBrowser(40, 10)
	n := &Node{Type: ElementNode, Tag: "input", Attrs: []Attr{{Key: "type", Value: "checkbox"}, {Key: "name", Value: "x"}}}
	b.toggleControl(n)
	if !n.HasAttr("checked") {
		t.Fatal("checkbox was not checked")
	}
	b.toggleControl(n)
	if n.HasAttr("checked") {
		t.Fatal("checkbox was not unchecked")
	}
}

func TestToggleSelect(t *testing.T) {
	sel := &Node{Type: ElementNode, Tag: "select", Attrs: []Attr{{Key: "name", Value: "text-scale"}}}
	auto := &Node{Type: ElementNode, Tag: "option", Parent: sel, Attrs: []Attr{{Key: "selected", Value: ""}}}
	off := &Node{Type: ElementNode, Tag: "option", Parent: sel}
	auto.Children = []*Node{{Type: TextNode, Text: "auto", Parent: auto}}
	off.Children = []*Node{{Type: TextNode, Text: "off", Parent: off}}
	sel.Children = []*Node{auto, off}
	b := NewBrowser(40, 10)
	if got := inputValue(sel); got != "auto" {
		t.Fatalf("select value = %q", got)
	}
	b.toggleControl(sel)
	if b.selectMenu == nil || len(b.selectMenu.options) != 2 || b.selectMenu.idx != 0 {
		t.Fatalf("select popup = %+v", b.selectMenu)
	}
	b.HandleKey("down")
	b.HandleKey("enter")
	if b.selectMenu != nil {
		t.Fatal("select popup did not close after choosing")
	}
	if got := inputValue(sel); got != "off" {
		t.Fatalf("after choosing = %q", got)
	}
	b.toggleControl(sel)
	b.HandleKey("esc")
	if b.selectMenu != nil {
		t.Fatal("escape did not close the select popup")
	}
	if got := inputValue(sel); got != "off" {
		t.Fatalf("escape changed the value to %q", got)
	}
}

func TestSelectClickOpensAllOptions(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/submit">
		<select name="choice"><option value="one">One</option><option value="two" selected>Two</option><option value="three">Three</option></select>
	</form></body></html>`)
	u, _ := url.Parse("https://example.com/")
	b := NewBrowser(60, 20)
	b.page = &Page{URL: u, Base: u, Root: doc, Title: "form"}
	b.url = u
	b.rebuild()
	sel := doc.FindFirst("select")
	var rect Rect
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Kind == fragText && f.Node == sel {
			rect = Rect{X: f.X, Y: f.Y, W: f.W, H: f.H}
			break
		}
	}
	if rect.W == 0 {
		t.Fatal("select widget was not rendered")
	}
	clickDoc := func(x, docY int) {
		top, _ := b.viewport()
		sx, sy := x+1, docY-b.scroll+top+1
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: sx, Y: sy, Press: true})
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: sx, Y: sy})
	}
	clickDoc(rect.X, rect.Y)
	if b.selectMenu == nil || len(b.selectMenu.options) != 3 {
		t.Fatalf("select popup = %+v", b.selectMenu)
	}
	drawQuiet(b)
	if len(b.selectHits) != 3 {
		t.Fatalf("select popup rows = %+v", b.selectHits)
	}
	if b.modalBox.W < 40 {
		t.Fatalf("select popup width = %d, want at least 40", b.modalBox.W)
	}
	hit := b.selectHits[2]
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: hit.rect.X + 1, Y: hit.rect.Y + 1, Press: true})
	if b.selectMenu != nil {
		t.Fatal("select popup did not close after clicking an option")
	}
	if got := inputValue(sel); got != "three" {
		t.Fatalf("select value = %q, want three", got)
	}
}

func TestMultiSelectShowsAndTogglesAllOptions(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/submit">
		<select name="colours" multiple><option value="red">Red</option><option value="green" selected>Green</option><option value="blue">Blue</option></select>
	</form></body></html>`)
	u, _ := url.Parse("https://example.com/")
	b := NewBrowser(60, 20)
	b.page = &Page{URL: u, Base: u, Root: doc, Title: "form"}
	b.url = u
	b.rebuild()
	sel := doc.FindFirst("select")
	rows := map[*Node]int{}
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Kind == fragText {
			for p := f.Node; p != nil; p = p.Parent {
				if p.Tag == "option" {
					rows[p] = f.Y
					break
				}
			}
		}
	}
	opts := selectOptions(sel)
	if len(opts) != 3 {
		t.Fatalf("options = %d", len(opts))
	}
	if rows[opts[0]] == rows[opts[1]] || rows[opts[1]] == rows[opts[2]] {
		t.Fatalf("multi-select options are not on separate rows: %v", rows)
	}
	clickOption := func(opt *Node) {
		for i := range b.layout.Frags {
			f := &b.layout.Frags[i]
			if f.Kind == fragText && f.Node == opt {
				top, _ := b.viewport()
				b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: f.X + 1, Y: f.Y - b.scroll + top + 1, Press: true})
				return
			}
		}
		t.Fatalf("option %q has no clickable row", optionLabel(opt))
	}
	clickOption(opts[0])
	clickOption(opts[2])
	got := strings.Join(selectedOptionValues(sel), ",")
	if got != "red,green,blue" {
		t.Fatalf("selected values = %q, want red,green,blue", got)
	}
	clickOption(opts[1])
	got = strings.Join(selectedOptionValues(sel), ",")
	if got != "red,blue" {
		t.Fatalf("selected values after toggle = %q, want red,blue", got)
	}
}

func TestConfigCheckboxForm(t *testing.T) {
	b := NewBrowser(80, 10)
	b.setLoader(NewLoader())
	b.noJS = true
	b.loader.ignoreBlocks = true
	b.applyConfig(url.Values{"config-form": {"1"}})
	if b.noJS || b.loader.noScripts {
		t.Fatal("unchecked no-js did not turn JS on")
	}
	if b.loader.ignoreBlocks {
		t.Fatal("unchecked ignore-blocks did not turn it off")
	}
}

func TestGridTracksAndPlacement(t *testing.T) {
	tracks := parseGridTracks("1fr 2fr", 90, 16)
	if len(tracks) != 2 || tracks[0].flex != 1 || tracks[1].flex != 2 {
		t.Fatalf("tracks = %+v", tracks)
	}
	widths := resolveGridTracks(tracks, 90, 0)
	if widths[0] != 30 || widths[1] != 60 {
		t.Fatalf("widths = %v", widths)
	}
	tracks = parseGridTracks("repeat(3, 1fr)", 90, 16)
	if len(tracks) != 3 {
		t.Fatalf("repeat tracks = %+v", tracks)
	}
	start, span, ok := gridPlacement("1 / 3", 4)
	if !ok || start != 0 || span != 2 {
		t.Fatalf("placement = %d,%d,%v", start, span, ok)
	}
	start, span, ok = gridPlacement("span 2", 4)
	if !ok || start != -1 || span != 2 {
		t.Fatalf("span placement = %d,%d,%v", start, span, ok)
	}
}

func TestGridLayout(t *testing.T) {
	doc := ParseHTML(`<html><body>
<div id="g" style="display:grid;grid-template-columns:1fr 1fr;gap:8px">
  <div>A</div><div>B</div>
</div></body></html>`)
	ly := BuildLayout(doc, nil, 40)
	var ax, bx int
	found := 0
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind != fragText {
			continue
		}
		switch f.Text {
		case "A":
			ax = f.X
			found++
		case "B":
			bx = f.X
			found++
		}
	}
	if found != 2 {
		t.Fatalf("found %d grid text fragments", found)
	}
	if bx <= ax || bx-ax < 15 {
		t.Fatalf("grid columns not separated: A=%d B=%d", ax, bx)
	}
}

func TestMediaRangeSyntax(t *testing.T) {
	if !mediaFeatureMatches("width>=960px", 1280) {
		t.Fatal("width>=960px did not match 1280")
	}
	if mediaFeatureMatches("width>=960px", 800) {
		t.Fatal("width>=960px matched 800")
	}
	if !mediaFeatureMatches("width<=959px", 800) {
		t.Fatal("width<=959px did not match 800")
	}
}

func TestSelectorIs(t *testing.T) {
	n := &Node{Type: ElementNode, Tag: "div", Attrs: []Attr{{Key: "class", Value: "card lead"}}}
	sel, err := ParseSelector(":is(.lead, .other)")
	if err != nil {
		t.Fatal(err)
	}
	if !sel.Match(n) {
		t.Fatal(":is selector did not match")
	}
}

func TestImageCap(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 3000, 2000))
	n := &Node{Type: ElementNode, Tag: "img", Img: img}
	st := &Style{Display: "inline-block", Width: pct(100), Height: pct(100)}
	w, h := imageSize(n, st, 100, 60)
	if w > 60 || h > maxImageRows {
		t.Fatalf("capped image = %dx%d", w, h)
	}
	if w != 60 {
		t.Fatalf("capped width = %d, want 60", w)
	}
}

func TestFlexItemsKeepMinContentWidth(t *testing.T) {
	doc := ParseHTML(`<html><body><div style="display:flex">` +
		`<span>abcd</span><span>abcd</span><span>abcd</span><span>abcd</span>` +
		`<span>abcd</span><span>abcd</span><span>abcd</span><span>abcd</span>` +
		`</div></body></html>`)
	ly := BuildLayout(doc, nil, 20)
	whole := 0
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind == fragText && f.Text == "abcd" {
			whole++
		}
	}
	if whole < 8 {
		t.Fatalf("flex items broke below min-content: %d whole words", whole)
	}
}

func TestDenseGridFallsBack(t *testing.T) {
	doc := ParseHTML(`<html><body><div style="display:grid;grid-template-columns:repeat(24,1fr)">` +
		`<div>hello world</div></div></body></html>`)
	ly := BuildLayout(doc, nil, 60)
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind == fragText && f.Text == "hello" && f.W == 5 {
			return
		}
	}
	t.Fatal("dense grid did not fall back to readable block flow")
}

func TestCenteredLabel(t *testing.T) {
	x, s := centeredLabel(10, 21, "abc")
	if x != 19 || s != "abc" {
		t.Fatalf("centeredLabel = %d,%q", x, s)
	}
	x, s = centeredLabel(10, 2, "abcdef")
	if x != 10 || s != "ab" {
		t.Fatalf("truncated centeredLabel = %d,%q", x, s)
	}
}

func TestHeaderNarrow(t *testing.T) {
	b := NewBrowser(20, 10)
	b.loader.bookmarks = []Bookmark{{Title: "x", URL: "https://example.com/"}}
	drawQuiet(b)
	if len(b.headerHits) != 6 || b.headerHits[0].kind != headerHome ||
		b.headerHits[1].kind != headerBookmarks || b.headerHits[2].kind != headerHistory ||
		b.headerHits[3].kind != headerHelp || b.headerHits[4].kind != headerConfig ||
		b.headerHits[5].kind != headerURL {
		t.Fatalf("narrow header hits = %+v", b.headerHits)
	}
	if b.headerHits[4].x+b.headerHits[4].w != b.width {
		t.Error("narrow config is not at the far right")
	}
	b2 := NewBrowser(20, 10)
	b2.width = 4
	drawQuiet(b2)
	if len(b2.headerHits) != 1 || b2.headerHits[0].kind != headerURL {
		t.Fatalf("tiny header hits = %+v", b2.headerHits)
	}
	b3 := NewBrowser(60, 10)
	b3.height = 4
	drawQuiet(b3)
	if top, _ := b3.viewport(); top != 1 {
		t.Errorf("short terminal viewport top = %d, want 1", top)
	}
	for _, hit := range b3.headerHits {
		if hit.y != 0 {
			t.Errorf("short terminal used more than one header row: %+v", hit)
		}
	}
}

func TestMouseDragSelection(t *testing.T) {
	b := NewBrowser(40, 10)
	b.layout = BuildLayout(ParseHTML(`<p>hello world</p>`), nil, 40)
	b.page = &Page{URL: mustParse(t, "https://example.com"), Root: ParseHTML(`<p>hello world</p>`)}
	top, _ := b.viewport()

	var frag *Frag
	for i := range b.layout.Frags {
		if b.layout.Frags[i].Kind == fragText {
			frag = &b.layout.Frags[i]
			break
		}
	}
	if frag == nil {
		t.Fatal("no text fragment")
	}
	// Convert the fragment start into 1-based terminal coordinates.
	row := top + (frag.Y - b.scroll) + 1
	col := frag.X + 1
	b.HandleMouse(MouseEvent{Button: 0, X: col, Y: row, Press: true})
	b.HandleMouse(MouseEvent{Button: 0, X: col + 4, Y: row, Press: true, Motion: true})
	b.HandleMouse(MouseEvent{Button: 0, X: col + 4, Y: row, Press: false})

	if !b.hasSelection() {
		t.Fatal("drag did not create a selection")
	}
	if got := b.selectedText(); !strings.Contains(got, "hello") {
		t.Fatalf("selected text = %q", got)
	}
	if !strings.Contains(b.status, "Selected") {
		t.Fatalf("status = %q", b.status)
	}

	g := NewGrid(40, 10, RGB(0, 0, 0), RGB(255, 255, 255))
	b.paintSelection(g, top, 9)
	selected := false
	for _, c := range g.Cells {
		if c.BG == RGB(0x2f, 0x6f, 0xbf) {
			selected = true
			break
		}
	}
	if !selected {
		t.Fatal("selection was not highlighted")
	}

	// Escape clears the selection.
	b.HandleKey("esc")
	if b.hasSelection() {
		t.Fatal("escape did not clear the selection")
	}
}

func TestSelectionHighlightsOnlyContent(t *testing.T) {
	doc := ParseHTML(`<html><body><p>one</p><p>two</p><p>three</p></body></html>`)
	b := NewBrowser(40, 16)
	b.page = &Page{URL: mustParse(t, "https://example.com"), Root: doc}
	b.layout = BuildLayout(doc, nil, 40)
	b.scroll = 0
	var first, middle, last *Frag
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Kind != fragText {
			continue
		}
		switch f.Text {
		case "one":
			first = f
		case "two":
			middle = f
		case "three":
			last = f
		}
	}
	if first == nil || middle == nil || last == nil {
		t.Fatalf("missing paragraph fragments: %v %v %v", first, middle, last)
	}
	b.selAnchor = cellPos{X: first.X, Y: first.Y}
	b.selCursor = cellPos{X: last.X + last.W - 1, Y: last.Y}
	top, bottom := b.viewport()
	g := NewGrid(40, 16, RGB(0, 0, 0), RGB(255, 255, 255))
	b.paintSelection(g, top, bottom)
	selBG := RGB(0x2f, 0x6f, 0xbf)
	y := middle.Y - b.scroll + top
	if g.Cells[y*g.W+middle.X].BG != selBG {
		t.Fatal("selected text was not highlighted")
	}
	if g.Cells[y*g.W+30].BG == selBG {
		t.Fatal("empty space to the right of the content was highlighted")
	}
}

func inputByName(root *Node, name string) *Node {
	var found *Node
	root.FindAll(func(n *Node) bool {
		if found == nil && n.Tag == "input" && n.Attr("name") == name {
			found = n
		}
		return false
	})
	return found
}

func TestInputTypingAndSubmit(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/search" method="get">
		<input type="hidden" name="hl" value="en">
		<input name="q" value="" placeholder="try typing here">
		<input type="submit" value="Go">
	</form></body></html>`)
	u := mustParse(t, "https://www.google.com/")
	b := NewBrowser(60, 10)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.rebuild()

	q := inputByName(doc, "q")
	if q == nil {
		t.Fatal("input not found")
	}
	var field *Frag
	for i := range b.layout.Frags {
		if b.layout.Frags[i].Node == q {
			field = &b.layout.Frags[i]
			break
		}
	}
	if field == nil {
		t.Fatal("input fragment not laid out")
	}
	if !strings.Contains(formLabel(q), "try typing here") {
		t.Fatalf("unfocused label = %q", formLabel(q))
	}
	top, _ := b.viewport()
	b.HandleMouse(MouseEvent{
		Button: 0, X: field.X + 1, Y: top + field.Y - b.scroll + 1, Press: true,
	})
	b.HandleMouse(MouseEvent{
		Button: 0, X: field.X + 1, Y: top + field.Y - b.scroll + 1, Press: false,
	})
	if b.inputFocus != q || !q.InputFocused {
		t.Fatal("clicking the input did not focus it")
	}
	if strings.Contains(formLabel(q), "try typing here") {
		t.Fatalf("focused label still shows the placeholder: %q", formLabel(q))
	}
	if b.layout == nil || !b.layout.HasCursor {
		t.Fatal("focused input has no caret")
	}
	b.HandleKey("h")
	b.HandleKey("i")
	if got := inputValue(q); got != "hi" {
		t.Fatalf("typed value = %q", got)
	}
	b.HandleKey("backspace")
	if got := inputValue(q); got != "h" {
		t.Fatalf("after backspace = %q", got)
	}
	b.HandleKey("i")

	targetCh := make(chan string, 1)
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		select {
		case targetCh <- target:
		default:
		}
		return &Page{URL: u, Base: u, Root: ParseHTML("<html></html>")}, nil
	}
	b.HandleKey("enter")
	select {
	case target := <-targetCh:
		if !strings.Contains(target, "/search?") || !strings.Contains(target, "q=hi") ||
			!strings.Contains(target, "hl=en") {
			t.Fatalf("submitted URL = %q", target)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("form was not submitted")
	}
	if b.inputFocus != nil {
		t.Fatal("input focus was not cleared after submit")
	}
}

func TestInputCursorMovementAndEditing(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/go"><input name="q" value="abcd"></form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 12)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	q := inputByName(doc, "q")
	if q == nil {
		t.Fatal("input not found")
	}
	b.setInputFocus(q)
	if q.InputCursor != 4 {
		t.Fatalf("focus cursor = %d, want 4", q.InputCursor)
	}
	if b.layout == nil || !b.layout.HasCursor {
		t.Fatal("focused input has no caret")
	}
	endX := b.layout.Cursor.X
	b.HandleKey("left")
	b.HandleKey("left")
	if q.InputCursor != 2 {
		t.Fatalf("cursor after two lefts = %d, want 2", q.InputCursor)
	}
	if b.layout.Cursor.X != endX-2 {
		t.Fatalf("caret did not move: x = %d, want %d", b.layout.Cursor.X, endX-2)
	}
	b.HandleKey("X")
	if got := inputValue(q); got != "abXcd" {
		t.Fatalf("value = %q, want abXcd", got)
	}
	if q.InputCursor != 3 {
		t.Fatalf("cursor after typing = %d, want 3", q.InputCursor)
	}
	b.HandleKey("home")
	if q.InputCursor != 0 {
		t.Fatalf("home cursor = %d", q.InputCursor)
	}
	b.HandleKey("delete")
	if got := inputValue(q); got != "bXcd" {
		t.Fatalf("value after delete = %q, want bXcd", got)
	}
	b.HandleKey("end")
	if q.InputCursor != len(inputValue(q)) {
		t.Fatalf("end cursor = %d", q.InputCursor)
	}
}

func TestTextareaCursorMovesBetweenRows(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/go"><textarea name="body">ab
cd</textarea></form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 12)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	area := doc.FindFirst("textarea")
	if area == nil {
		t.Fatal("textarea not found")
	}
	b.setInputFocus(area)
	if area.InputCursor != len(inputValue(area)) {
		t.Fatalf("focus cursor = %d", area.InputCursor)
	}
	row2 := b.layout.Cursor.Y
	b.HandleKey("up")
	if area.InputCursor != 2 {
		t.Fatalf("cursor after up = %d, want 2", area.InputCursor)
	}
	if b.layout.Cursor.Y == row2 {
		t.Fatal("textarea caret did not move to the previous row")
	}
	b.HandleKey("left")
	if area.InputCursor != 1 {
		t.Fatalf("cursor after left = %d, want 1", area.InputCursor)
	}
	b.HandleKey("down")
	if area.InputCursor != len("ab\nc") {
		t.Fatalf("cursor after down = %d", area.InputCursor)
	}
}

func TestPaintInputCursor(t *testing.T) {
	b := NewBrowser(20, 5)
	b.layout = &Layout{HasCursor: true, Cursor: Rect{X: 2, Y: 1, W: 1, H: 1}}
	g := NewGrid(20, 5, RGB(0, 0, 0), RGB(0x11, 0x22, 0x33))
	b.paintInputCursor(g, 2, 5)
	cell := g.Cells[3*20+2]
	if cell.BG != RGB(0xff, 0xff, 0xff) || cell.FG != RGB(0x2f, 0x6f, 0xbf) {
		t.Fatalf("caret cell colors = %+v", cell)
	}
}

func TestFormValidationConstraints(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/go">
		<input name="required-empty" required>
		<input name="pattern" pattern="[0-9]{3}" value="12a">
		<input name="date" type="date" value="2026-13-01">
		<input name="time" type="time" value="25:00">
		<input name="color" type="color" value="#zzzzzz">
		<input name="ok-date" type="date" value="2026-11-10">
		<input name="disabled-empty" required disabled>
	</form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 12)
	b.page = &Page{URL: u, Base: u, Root: doc}
	form := doc.FindFirst("form")

	field, msg := b.validateForm(form)
	if field == nil || field.Attr("name") != "required-empty" || !strings.Contains(msg, "required") {
		t.Fatalf("first validation = %v %q", field, msg)
	}
	setAttr(inputByName(doc, "required-empty"), "value", "ok")
	field, msg = b.validateForm(form)
	if field == nil || field.Attr("name") != "pattern" || !strings.Contains(msg, "pattern") {
		t.Fatalf("pattern validation = %v %q", field, msg)
	}
	setAttr(inputByName(doc, "pattern"), "value", "123")
	field, msg = b.validateForm(form)
	if field == nil || field.Attr("name") != "date" || !strings.Contains(msg, "date") {
		t.Fatalf("date validation = %v %q", field, msg)
	}
	setAttr(inputByName(doc, "date"), "value", "2026-11-10")
	field, msg = b.validateForm(form)
	if field == nil || field.Attr("name") != "time" || !strings.Contains(msg, "time") {
		t.Fatalf("time validation = %v %q", field, msg)
	}
	setAttr(inputByName(doc, "time"), "value", "14:30")
	field, msg = b.validateForm(form)
	if field == nil || field.Attr("name") != "color" || !strings.Contains(msg, "color") {
		t.Fatalf("color validation = %v %q", field, msg)
	}
	setAttr(inputByName(doc, "color"), "value", "#ff0000")
	if field, msg := b.validateForm(form); field != nil {
		t.Fatalf("valid form rejected: %v %q", field, msg)
	}
}

func TestInputTypeFormatValidation(t *testing.T) {
	cases := []struct {
		typ, value string
		valid      bool
	}{
		{"date", "2026-11-10", true},
		{"date", "2026-02-30", false},
		{"time", "23:59", true},
		{"time", "24:00", false},
		{"datetime-local", "2026-11-10T14:30", true},
		{"datetime-local", "2026-11-10 14:30", false},
		{"month", "2026-11", true},
		{"month", "2026-13", false},
		{"week", "2026-W45", true},
		{"week", "2026-W99", false},
		{"color", "#2f6fbf", true},
		{"color", "#2f6fb", false},
	}
	for _, tc := range cases {
		got := invalidControlValue(nil, tc.typ, tc.value) == ""
		if got != tc.valid {
			t.Errorf("%s %q valid=%v, want %v", tc.typ, tc.value, got, tc.valid)
		}
	}
}

func TestMaxlengthReadonlyDisabled(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/go">
		<input name="short" maxlength="3">
		<input name="ro" value="fixed" readonly>
		<input name="off" value="hidden" disabled>
		<textarea name="ro-area" readonly>fixed area</textarea>
		<input name="off-check" type="checkbox" checked disabled>
		<button type="submit" disabled>No</button>
	</form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 12)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	short := inputByName(doc, "short")
	b.setInputFocus(short)
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		b.HandleKey(k)
	}
	if got := inputValue(short); got != "abc" {
		t.Fatalf("maxlength value = %q", got)
	}
	if isTextInput(inputByName(doc, "ro")) || isTextInput(doc.FindFirst("textarea")) {
		t.Fatal("readonly control is editable")
	}
	if isTextInput(inputByName(doc, "off")) || isToggleControl(inputByName(doc, "off-check")) {
		t.Fatal("disabled control is interactive")
	}
	if isSubmitControl(doc.FindFirst("button")) {
		t.Fatal("disabled button is a submit control")
	}
	if field, msg := b.validateForm(doc.FindFirst("form")); field != nil {
		t.Fatalf("disabled controls affected validation: %v %q", field, msg)
	}
}

func TestRangeKeyboardControl(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/go"><input name="r" type="range" min="0" max="10" step="2" value="4"></form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 12)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	r := inputByName(doc, "r")
	b.setInputFocus(r)
	if b.inputFocus != r {
		t.Fatal("range was not focused")
	}
	b.HandleKey("right")
	if got := inputValue(r); got != "6" {
		t.Fatalf("range after right = %q", got)
	}
	b.HandleKey("home")
	if got := inputValue(r); got != "0" {
		t.Fatalf("range home = %q", got)
	}
	b.HandleKey("end")
	if got := inputValue(r); got != "10" {
		t.Fatalf("range end = %q", got)
	}
	if label := formLabel(r); !strings.Contains(label, "█") || !strings.Contains(label, "10") || !strings.Contains(label, "- ") || !strings.Contains(label, " + ") {
		t.Fatalf("range label = %q", label)
	}
	var frag *Frag
	for i := range b.layout.Frags {
		if b.layout.Frags[i].Kind == fragText && b.layout.Frags[i].Node == r {
			frag = &b.layout.Frags[i]
			break
		}
	}
	if frag == nil {
		t.Fatal("range widget fragment not found")
	}
	top, _ := b.viewport()
	clickAt := func(rel int) {
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: frag.X + rel + 1, Y: frag.Y - b.scroll + top + 1, Press: true})
	}
	minus := strings.Index(frag.Text, "-")
	clickAt(stringWidth(frag.Text[:minus]))
	if got := inputValue(r); got != "8" {
		t.Fatalf("range after minus click = %q", got)
	}
	b.rebuild()
	for i := range b.layout.Frags {
		if b.layout.Frags[i].Kind == fragText && b.layout.Frags[i].Node == r {
			frag = &b.layout.Frags[i]
			break
		}
	}
	plus := strings.LastIndex(frag.Text, "+")
	clickAt(stringWidth(frag.Text[:plus]))
	if got := inputValue(r); got != "10" {
		t.Fatalf("range after plus click = %q", got)
	}
}

func TestLabelAssociations(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/go">
		<label for="query">Query label</label>
		<input id="query" name="q">
		<p><label><input id="agree" type="checkbox" name="agree"> Accept terms</label></p>
		<label for="off">Disabled label</label>
		<input id="off" name="off" disabled>
	</form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(70, 20)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	clickText := func(text string) {
		for i := range b.layout.Frags {
			f := &b.layout.Frags[i]
			if f.Kind != fragText || !strings.Contains(f.Text, text) {
				continue
			}
			top, _ := b.viewport()
			x, y := f.X+1, f.Y-b.scroll+top+1
			b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: true})
			b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y})
			return
		}
		t.Fatalf("label text %q not found", text)
	}
	query := inputByName(doc, "q")
	clickText("Query")
	if b.inputFocus != query {
		t.Fatal("label for= did not focus the input")
	}
	agree := inputByName(doc, "agree")
	clickText("Accept")
	if !agree.HasAttr("checked") {
		t.Fatal("wrapping label did not toggle the checkbox")
	}
	b.HandleKey("esc")
	clickText("Disabled")
	if b.inputFocus != nil {
		t.Fatal("label focused a disabled control")
	}
}

func TestQuitConfirmation(t *testing.T) {
	b := NewBrowser(40, 10)
	if b.HandleKey("q") {
		t.Fatal("q quit without confirmation")
	}
	if !b.quitPending || b.status != quitConfirmText {
		t.Fatalf("quit prompt = pending %v status %q", b.quitPending, b.status)
	}
	if b.HandleKey("n") {
		t.Fatal("n confirmed quit")
	}
	if b.quitPending || b.status != "" {
		t.Fatalf("cancel state = pending %v status %q", b.quitPending, b.status)
	}
	if b.HandleKey("ctrl-c") {
		t.Fatal("ctrl-c quit without confirmation")
	}
	if !b.HandleKey("y") {
		t.Fatal("y did not confirm quit")
	}
	if b.quitPending {
		t.Fatal("quit confirmation survived a confirmed quit")
	}
	if b.HandleKey("q") {
		t.Fatal("second q quit without confirmation")
	}
	if !b.HandleKey("enter") {
		t.Fatal("enter did not confirm quit")
	}
}

func TestConsoleQuitConfirmation(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	if b.handleConsoleKey("q") {
		t.Fatal("console q quit without confirmation")
	}
	if !b.quitPending || b.status != quitConfirmText {
		t.Fatalf("console quit prompt = pending %v status %q", b.quitPending, b.status)
	}
	if !b.HandleKey("y") {
		t.Fatal("y did not confirm console quit")
	}
}

func TestNonSubmitButtonClickRunsHandler(t *testing.T) {
	page := newJSPage(`<html><body><p>intro</p><button id="b" type="button">Got it</button><p id="out">visible</p></body></html>`,
		`document.getElementById("b").addEventListener("click", function () {
			document.getElementById("out").textContent = "hidden";
		});`)
	b := NewBrowser(60, 10)
	b.page = page
	withQuietStdout(t, func() { b.startJS(page) })
	b.rebuild()

	btn := page.Root.FindFirst("button")
	if btn == nil {
		t.Fatal("button not found")
	}
	var field *Frag
	for i := range b.layout.Frags {
		if b.layout.Frags[i].Node == btn {
			field = &b.layout.Frags[i]
			break
		}
	}
	if field == nil {
		t.Fatal("button fragment not laid out")
	}
	top, _ := b.viewport()
	x := field.X + 1
	y := top + field.Y - b.scroll + 1
	withQuietStdout(t, func() {
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: true})
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: false})
	})
	if got := findByID(page.Root, "out"); got != nil {
		if got.TextContent() != "hidden" {
			t.Fatalf("click handler did not run, text = %q", got.TextContent())
		}
	} else {
		t.Fatal("output paragraph missing")
	}
}

func TestMouseClickSubmitButton(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/go" method="get">
		<input name="q" value="x"><button type="submit">Go</button>
	</form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 10)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.rebuild()
	btn := doc.FindFirst("button")
	if btn == nil {
		t.Fatal("button not found")
	}
	var field *Frag
	for i := range b.layout.Frags {
		if b.layout.Frags[i].Node == btn {
			field = &b.layout.Frags[i]
			break
		}
	}
	if field == nil {
		t.Fatal("button fragment not laid out")
	}
	targetCh := make(chan string, 1)
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		select {
		case targetCh <- target:
		default:
		}
		return &Page{URL: u, Base: u, Root: ParseHTML("<html></html>")}, nil
	}
	top, _ := b.viewport()
	x := field.X + 1
	y := top + field.Y - b.scroll + 1
	withQuietStdout(t, func() {
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: true})
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: false})
	})
	select {
	case target := <-targetCh:
		if !strings.Contains(target, "/go?") || !strings.Contains(target, "q=x") {
			t.Fatalf("submitted URL = %q", target)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("clicking the submit button did not submit the form")
	}
}

func TestFormPOSTSubmission(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/submit" method="post">
		<input type="hidden" name="hl" value="en">
		<input name="q" value="hi">
	</form></body></html>`)
	u := mustParse(t, "https://example.com/form")
	b := NewBrowser(40, 10)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.rebuild()

	type req struct {
		method, target, ctype, body string
	}
	ch := make(chan req, 1)
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		ch <- req{method: method, target: target, ctype: contentType, body: string(body)}
		return &Page{URL: u, Base: u, Root: ParseHTML("<html></html>")}, nil
	}
	b.submitForm(doc.FindFirst("form"))
	select {
	case got := <-ch:
		if got.method != "POST" {
			t.Errorf("method = %q", got.method)
		}
		if got.target != "https://example.com/submit" {
			t.Errorf("target = %q", got.target)
		}
		if got.body != "hl=en&q=hi" {
			t.Errorf("body = %q", got.body)
		}
		if got.ctype != "application/x-www-form-urlencoded" {
			t.Errorf("content type = %q", got.ctype)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("POST form was not submitted")
	}
}

func TestNavigateSearchesQuery(t *testing.T) {
	b := NewBrowser(40, 10)
	b.loader.searchURL = "https://www.bing.com/search?q=%s"
	ch := make(chan string, 1)
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		ch <- target
		return &Page{}, nil
	}
	b.Navigate("golang tutorial", false)
	select {
	case got := <-ch:
		if got != "https://www.bing.com/search?q=golang+tutorial" {
			t.Errorf("target = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("search navigation did not start")
	}
}

func TestRemotePageCannotNavigateToLocalFile(t *testing.T) {
	b := NewBrowser(40, 10)
	b.url = mustParse(t, "https://example.com/")
	called := false
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		called = true
		return &Page{}, nil
	}
	b.Navigate("file:///etc/passwd", false)
	if called {
		t.Fatal("remote page navigation to file:// was allowed")
	}
	if !strings.Contains(b.status, "Blocked local file") {
		t.Fatalf("status = %q", b.status)
	}
}

func TestNavigateAsyncAndStop(t *testing.T) {
	b := NewBrowser(40, 10)
	started := make(chan struct{})
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	b.Navigate("https://example.com/", false)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("load did not start")
	}
	if !b.loading {
		t.Fatal("browser is not marked as loading")
	}
	b.HandleKey("esc")
	if b.loading {
		t.Fatal("Esc did not stop loading")
	}
	if b.status != "Stopped loading" {
		t.Fatalf("status = %q", b.status)
	}
	// A late result from the canceled load must be ignored.
	select {
	case res := <-b.loadCh:
		b.finishLoad(res)
	case <-time.After(200 * time.Millisecond):
	}
	if b.page != nil {
		t.Fatal("canceled load was applied")
	}
}

func TestReloadButtonBecomesStopWhileLoading(t *testing.T) {
	b := NewBrowser(60, 10)
	started := make(chan struct{})
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	b.Navigate("https://example.com/", false)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("load did not start")
	}
	drawQuiet(b)
	var stop *headerHit
	for i := range b.headerHits {
		if b.headerHits[i].kind == headerStop {
			stop = &b.headerHits[i]
		}
		if b.headerHits[i].kind == headerReload {
			t.Fatal("reload button still shown while loading")
		}
	}
	if stop == nil {
		t.Fatalf("no stop button while loading: %+v", b.headerHits)
	}
	if stop.w != stringWidth(" × ") {
		t.Fatalf("stop button width = %d, want %d", stop.w, stringWidth(" × "))
	}
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: stop.x + 1, Y: stop.y + 1, Press: true})
	if b.loading {
		t.Fatal("clicking stop did not cancel the load")
	}
	if b.status != "Stopped loading" {
		t.Fatalf("status = %q", b.status)
	}
	drawQuiet(b)
	foundReload := false
	for _, hit := range b.headerHits {
		if hit.kind == headerStop {
			t.Fatal("stop button still shown after cancel")
		}
		if hit.kind == headerReload {
			foundReload = true
		}
	}
	if !foundReload {
		t.Fatal("reload button did not return after cancel")
	}
}

func TestNavigateAsyncSuccess(t *testing.T) {
	b := NewBrowser(40, 10)
	b.Navigate("data:text/html,<title>Loaded</title><p>hi</p>", false)
	var res loadResult
	select {
	case res = <-b.loadCh:
	case <-time.After(2 * time.Second):
		t.Fatal("load did not finish")
	}
	b.finishLoad(res)
	if b.page == nil || b.page.Title != "Loaded" {
		t.Fatalf("page = %+v", b.page)
	}
}

func TestURLPromptPrefillsAndSelects(t *testing.T) {
	b := NewBrowser(40, 10)
	b.url = mustParse(t, "https://example.com/page")
	b.HandleKey("o")
	if b.promptText != "https://example.com/page" || !b.promptSelectAll {
		t.Fatalf("prompt = %q selectAll=%v", b.promptText, b.promptSelectAll)
	}
	b.HandleKey("x")
	if b.promptText != "x" || b.promptSelectAll {
		t.Fatalf("typing did not replace selection: %q selectAll=%v", b.promptText, b.promptSelectAll)
	}

	b.prompt = false
	b.promptText = ""
	b.HandleKey("o")
	b.HandleKey("backspace")
	if b.promptText != "" || b.promptSelectAll {
		t.Fatalf("backspace did not clear selection: %q selectAll=%v", b.promptText, b.promptSelectAll)
	}
}

func TestURLPromptCursorEditing(t *testing.T) {
	b := NewBrowser(40, 10)
	b.url = mustParse(t, "https://example.com/abc")
	b.HandleKey("o")
	b.HandleKey("right") // collapse selection to end
	b.HandleKey("left")
	b.HandleKey("left")
	b.HandleKey("left")
	b.HandleKey("X")
	if got := b.promptText; got != "https://example.com/Xabc" {
		t.Fatalf("insert = %q", got)
	}
	b.HandleKey("delete")
	if got := b.promptText; got != "https://example.com/Xbc" {
		t.Fatalf("delete = %q", got)
	}
	b.HandleKey("home")
	b.HandleKey("delete")
	if got := b.promptText; got != "ttps://example.com/Xbc" {
		t.Fatalf("home/delete = %q", got)
	}
	b.HandleKey("end")
	head, at, tail, show := promptParts(" Open: ", b.promptText, b.promptCursor, false, 40)
	if !show || stringWidth(head)+1+stringWidth(tail) > 40 {
		t.Fatalf("bad prompt parts: head=%q at=%q tail=%q show=%v", head, at, tail, show)
	}
	if !strings.HasSuffix(head, "Xbc") || at != ' ' || tail != "" {
		t.Fatalf("cursor parts: head=%q at=%q tail=%q", head, at, tail)
	}
}

func TestVimURLKey(t *testing.T) {
	b := NewBrowser(40, 10)
	b.HandleKey("o")
	if !b.prompt {
		t.Fatal("o did not open the URL prompt")
	}
	b.prompt = false
	b.HandleKey("g")
	if b.prompt {
		t.Fatal("g should not open the URL prompt")
	}
}

func TestTextScaleCanBeDisabled(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("KITTY_WINDOW_ID", "1")
	b := NewBrowser(40, 10)
	b.gfx = NewGFXRenderer(GraphicsKitty, 8, 16)
	b.page = &Page{URL: mustParse(t, "https://example.com"), Root: ParseHTML(`<h1>big</h1>`)}
	b.noBigText = true
	b.rebuild()
	if b.bigText {
		t.Fatal("scaled text was not disabled")
	}
	b.noBigText = false
	b.rebuild()
	if !b.bigText {
		t.Fatal("scaled text should be enabled in kitty by default")
	}
}

func TestFooterPercentFarRight(t *testing.T) {
	b := NewBrowser(80, 10)
	b.layout = &Layout{Height: 107}
	b.scroll = 50
	drawQuiet(b)
	line := b.prev[len(b.prev)-1]
	if !strings.Contains(line, "50%") || !strings.HasSuffix(line, ">_") {
		t.Fatalf("footer = %q", line)
	}
}

func TestFooterShowsUsage(t *testing.T) {
	b := NewBrowser(120, 10)
	b.layout = &Layout{Height: 100}
	b.usage = procUsage{rss: 18 << 20}
	drawQuiet(b)
	line := b.prev[len(b.prev)-1]
	for _, want := range []string{"MEM 18 MB", "0%"} {
		if !strings.Contains(line, want) {
			t.Errorf("footer = %q, want %q", line, want)
		}
	}
	for _, unwanted := range []string{"CPU", "Tab:", "q: quit", "h/l", "q Quit", "? Help"} {
		if strings.Contains(line, unwanted) {
			t.Errorf("footer still contains %q: %q", unwanted, line)
		}
	}
}

func TestEscapeShowsExitHint(t *testing.T) {
	b := NewBrowser(80, 10)
	b.layout = &Layout{Height: 10}
	b.HandleKey("esc")
	if b.status != exitHint {
		t.Fatalf("status = %q, want %q", b.status, exitHint)
	}
	drawQuiet(b)
	line := b.prev[len(b.prev)-1]
	for _, want := range []string{"q Quit", "? Help"} {
		if !strings.Contains(line, want) {
			t.Errorf("footer = %q, want %q", line, want)
		}
	}
}

func TestEscapeFromPromptShowsExitHint(t *testing.T) {
	b := NewBrowser(80, 10)
	b.openURLPrompt()
	b.HandleKey("esc")
	if b.prompt {
		t.Fatal("escape did not close the URL prompt")
	}
	if b.status != exitHint {
		t.Fatalf("status = %q, want %q", b.status, exitHint)
	}
}

func TestEscapeFromInputShowsExitHint(t *testing.T) {
	doc := ParseHTML(`<html><body><input name="q"></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(80, 10)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.rebuild()
	q := inputByName(doc, "q")
	if q == nil {
		t.Fatal("input not found")
	}
	b.setInputFocus(q)
	b.HandleKey("esc")
	if b.inputFocus != nil {
		t.Fatal("escape did not leave the input")
	}
	if b.status != exitHint {
		t.Fatalf("status = %q, want %q", b.status, exitHint)
	}
}

func TestFooterUsageDropsOnNarrow(t *testing.T) {
	b := NewBrowser(20, 10)
	b.layout = &Layout{Height: 100}
	b.usage = procUsage{rss: 18 << 20}
	drawQuiet(b)
	line := b.prev[len(b.prev)-1]
	if strings.Contains(line, "MEM") {
		t.Fatalf("narrow footer still shows usage: %q", line)
	}
	if !strings.Contains(line, "0%") || !strings.HasSuffix(line, ">_") {
		t.Fatalf("narrow footer = %q", line)
	}
}

func TestPageScrollbarMouse(t *testing.T) {
	b := NewBrowser(20, 10)
	b.layout = &Layout{Height: 100}
	drawQuiet(b)
	if b.scrollBar == nil {
		t.Fatal("no scrollbar for a long page")
	}
	info := b.scrollBar
	if info.trackTop != 2 || info.trackH != 7 {
		t.Fatalf("scrollbar track = %+v", info)
	}
	b.HandleMouse(MouseEvent{Button: 0, X: b.width, Y: info.trackTop + info.trackH, Press: true})
	if b.scroll != b.maxScroll() || b.scroll == 0 {
		t.Fatalf("bottom click scroll = %d, max = %d", b.scroll, b.maxScroll())
	}
	b.HandleMouse(MouseEvent{Button: 0, X: b.width, Y: info.trackTop + 1, Press: true})
	if b.scroll != 0 {
		t.Fatalf("top click scroll = %d", b.scroll)
	}

	b.HandleMouse(MouseEvent{Button: 0, X: b.width, Y: info.trackTop + 3, Press: true})
	b.HandleMouse(MouseEvent{Button: 0, X: b.width, Y: info.trackTop + info.trackH, Press: true, Motion: true})
	b.HandleMouse(MouseEvent{Button: 0, X: b.width, Y: info.trackTop + info.trackH, Press: false})
	if b.scroll != b.maxScroll() {
		t.Fatalf("drag scroll = %d, max = %d", b.scroll, b.maxScroll())
	}
	if b.scrollDrag {
		t.Fatal("scrollbar drag did not end")
	}

	short := NewBrowser(20, 10)
	short.layout = &Layout{Height: 5}
	drawQuiet(short)
	if short.scrollBar != nil {
		t.Fatal("scrollbar shown for a page that fits")
	}
}

func TestFooterConsoleIcon(t *testing.T) {
	b := NewBrowser(80, 10)
	b.layout = &Layout{Height: 100}
	drawQuiet(b)
	var hit *headerHit
	for i := range b.footerHits {
		if b.footerHits[i].kind == headerConsole {
			hit = &b.footerHits[i]
			break
		}
	}
	if hit == nil || hit.x+hit.w != b.width {
		t.Fatalf("console hit = %+v", b.footerHits)
	}
	withQuietStdout(t, func() {
		b.HandleMouse(MouseEvent{Button: 0, X: hit.x + 1, Y: hit.y + 1, Press: true})
	})
	if !b.consoleOpen {
		t.Fatal("clicking the console icon did not open the console")
	}
}

func TestScrollRepaintsChrome(t *testing.T) {
	b := NewBrowser(40, 10)
	b.layout = &Layout{Height: 100}
	drawQuiet(b)
	b.prev[0] = "stale header"
	b.scrollBy(1)
	drawQuiet(b)
	if b.prev[0] == "stale header" {
		t.Fatal("scrolling did not force the header to be repainted")
	}
	if !strings.Contains(b.prev[0], "←") {
		t.Fatalf("header line = %q", b.prev[0])
	}

	// The chrome is rewritten even when nothing changed.
	b.prev[0] = "stale header"
	drawQuiet(b)
	if b.prev[0] == "stale header" || !strings.Contains(b.prev[0], "←") {
		t.Fatal("unchanged header was not repainted")
	}
}

func TestHeaderClicks(t *testing.T) {
	b := NewBrowser(60, 10)
	b.loader.bookmarks = []Bookmark{{Title: "x", URL: "https://example.com/"}}
	u, _ := url.Parse("https://example.com/a")
	b.url = u
	drawQuiet(b)

	b.HandleMouse(MouseEvent{Button: 0, X: 7, Y: 2, Press: true})
	if !b.prompt || b.promptText != "https://example.com/a" || !b.promptSelectAll {
		t.Fatalf("URL prompt = %q selectAll=%v", b.promptText, b.promptSelectAll)
	}
	b.prompt = false
	b.promptSelectAll = false

	b.HandleMouse(MouseEvent{Button: 0, X: 2, Y: 1, Press: true})
	if b.status != "No previous page" {
		t.Fatalf("back button status = %q", b.status)
	}
	b.HandleMouse(MouseEvent{Button: 0, X: 4, Y: 1, Press: true})
	if b.status != "No next page" {
		t.Fatalf("forward button status = %q", b.status)
	}

	targetCh := make(chan string, 1)
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		select {
		case targetCh <- target:
		default:
		}
		return &Page{}, nil
	}
	b.HandleMouse(MouseEvent{Button: 0, X: 55, Y: 1, Press: true})
	select {
	case target := <-targetCh:
		if target != "about:help" {
			t.Fatalf("help target = %q", target)
		}
	case <-time.After(time.Second):
		t.Fatal("help button did not navigate")
	}

	b.home = "https://example.com/home"
	b.HandleMouse(MouseEvent{Button: 0, X: 10, Y: 1, Press: true})
	select {
	case target := <-targetCh:
		if target != "https://example.com/home" {
			t.Fatalf("home target = %q", target)
		}
	case <-time.After(time.Second):
		t.Fatal("home button did not navigate")
	}

	b.HandleMouse(MouseEvent{Button: 0, X: 52, Y: 1, Press: true})
	select {
	case target := <-targetCh:
		if target != "about:history" {
			t.Fatalf("history target = %q", target)
		}
	case <-time.After(time.Second):
		t.Fatal("history button did not navigate")
	}

	b.HandleMouse(MouseEvent{Button: 0, X: 49, Y: 1, Press: true})
	select {
	case target := <-targetCh:
		if target != "about:bookmarks" {
			t.Fatalf("bookmarks target = %q", target)
		}
	case <-time.After(time.Second):
		t.Fatal("bookmarks button did not navigate")
	}

	b.HandleMouse(MouseEvent{Button: 0, X: 58, Y: 1, Press: true})
	if b.page == nil || b.page.Title != "Config — MOB³" {
		t.Fatalf("config page = %+v", b.page)
	}
}

func TestHistoryRecordsSession(t *testing.T) {
	b := NewBrowser(60, 10)
	b.setLoader(NewLoader())
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		u := mustParse(t, target)
		return &Page{URL: u, Base: u, Title: u.Path, Root: ParseHTML("<html><body>ok</body></html>")}, nil
	}
	navigate := func(target string) {
		b.Navigate(target, true)
		select {
		case res := <-b.loadCh:
			b.finishLoad(res)
		case <-time.After(2 * time.Second):
			t.Fatalf("navigation to %s did not finish", target)
		}
	}
	navigate("https://example.com/a")
	navigate("https://example.com/b")

	page, err := b.loader.LoadContext(context.Background(), "about:history", nil)
	if err != nil {
		t.Fatal(err)
	}
	text := page.Root.TextContent()
	if !strings.Contains(text, "https://example.com/a") || !strings.Contains(text, "https://example.com/b") {
		t.Fatalf("history text = %q", text)
	}
	if !strings.Contains(text, "(current)") {
		t.Fatal("current entry was not marked")
	}
}

func TestParseBookmarks(t *testing.T) {
	data := `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<DL><p>
<DT><H3>Work</H3>
<DL><p>
<DT><A HREF="https://a.example/">A &amp; B</A>
<DT><A HREF="https://b.example/">B</A>
</DL><p>
<DT><A HREF="https://c.example/">C</A>
</DL>`
	marks := parseBookmarks([]byte(data))
	if len(marks) != 3 {
		t.Fatalf("bookmarks = %+v", marks)
	}
	if marks[0].Title != "A & B" || marks[0].URL != "https://a.example/" || marks[0].Folder != "Work" {
		t.Errorf("first bookmark = %+v", marks[0])
	}
	if marks[1].Folder != "Work" {
		t.Errorf("second folder = %q", marks[1].Folder)
	}
	if marks[2].Folder != "" || marks[2].URL != "https://c.example/" {
		t.Errorf("root bookmark = %+v", marks[2])
	}
}

func TestBookmarksPage(t *testing.T) {
	html := bookmarksPageHTML([]Bookmark{
		{Title: "A & B", URL: "https://a.example/?x=1&y=2", Folder: "Work"},
		{Title: "Root", URL: "https://c.example/"},
	})
	if !strings.Contains(html, "Work") || !strings.Contains(html, "A &amp; B") ||
		!strings.Contains(html, "https://a.example/?x=1&amp;y=2") {
		t.Fatalf("page = %s", html)
	}
	empty := bookmarksPageHTML(nil)
	if !strings.Contains(empty, "No bookmarks imported") {
		t.Fatalf("empty page = %s", empty)
	}
}

func TestHistoryPage(t *testing.T) {
	b := NewBrowser(60, 10)
	b.history = []string{"https://example.com/a", "about:start"}
	b.histIdx = 1
	page, err := b.loader.LoadContext(context.Background(), "about:history", nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "History — MOB³" {
		t.Fatalf("title = %q", page.Title)
	}
	text := page.Root.TextContent()
	if !strings.Contains(text, "https://example.com/a") || !strings.Contains(text, "about:start") {
		t.Fatalf("history text = %q", text)
	}
	if !strings.Contains(text, "(current)") {
		t.Fatal("current entry was not marked")
	}
	links := 0
	for _, n := range page.Root.FindAll(func(n *Node) bool { return n.Tag == "a" }) {
		if n.Attr("href") != "" {
			links++
		}
	}
	if links != 2 {
		t.Fatalf("history links = %d", links)
	}

	empty := NewBrowser(60, 10)
	p2, err := empty.loader.LoadContext(context.Background(), "about:history", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p2.Root.TextContent(), "No pages in this session") {
		t.Fatalf("empty history = %q", p2.Root.TextContent())
	}
}

func TestTryAnywayRetriesBlockedURL(t *testing.T) {
	b := NewBrowser(40, 10)
	b.url = mustParse(t, "https://example.com/challenge")
	var got string
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		got = target
		u := mustParse(t, target)
		return &Page{URL: u, Base: u, Root: ParseHTML("<html><body><p>challenge</p></body></html>"), Title: "challenge"}, nil
	}
	b.Navigate("about:retry", true)
	select {
	case res := <-b.loadCh:
		b.finishLoad(res)
	case <-time.After(2 * time.Second):
		t.Fatal("retry did not start a load")
	}
	if got != "https://example.com/challenge" {
		t.Fatalf("retry target = %q", got)
	}
	if len(b.history) != 0 {
		t.Fatalf("retry pushed history: %v", b.history)
	}
}

func TestTryAnywayWithoutURL(t *testing.T) {
	b := NewBrowser(40, 10)
	b.url = nil
	b.Navigate("about:retry", true)
	if b.status != "Nothing to retry" {
		t.Fatalf("status = %q", b.status)
	}
	select {
	case <-b.loadCh:
		t.Fatal("retry without a URL started a load")
	default:
	}
}

func TestJSErrorHintInFooter(t *testing.T) {
	b := NewBrowser(140, 10)
	b.usage = procUsage{rss: 18 << 20}
	if strings.Contains(b.footerRight(140), "JS: Error") {
		t.Fatal("error hint shown without a JS error")
	}
	b.logJS("error", "TypeError: boom")
	if !b.jsErr {
		t.Fatal("JS error was not recorded")
	}
	right := b.footerRight(140)
	if !strings.Contains(right, jsErrorHint) || !strings.Contains(right, "MEM 18 MB") ||
		!strings.Contains(right, "JavaScript on") || !strings.Contains(right, "Graphics none") ||
		!strings.Contains(right, "Reader off") {
		t.Fatalf("footer right = %q", right)
	}
	if strings.Index(right, "JS: Error") > strings.Index(right, "Reader off") {
		t.Fatalf("error hint is not before reader state: %q", right)
	}
	if strings.Index(right, "Reader off") > strings.Index(right, "MEM") {
		t.Fatalf("reader state is not before memory: %q", right)
	}
	b.reader = true
	if got := b.footerRight(140); !strings.Contains(got, "Reader on") {
		t.Fatalf("reader-on footer = %q", got)
	}
	if narrow := b.footerRight(24); !strings.HasPrefix(narrow, jsErrorShort) {
		t.Fatalf("narrow footer = %q", narrow)
	}
}

func TestFooterShortcutUnderline(t *testing.T) {
	g := NewGrid(40, 1, RGB(0, 0, 0), RGB(255, 255, 255))
	st := &Style{Color: RGB(0x24, 0x29, 0x2f), FontSize: 16, LineHeight: 1}
	drawFooterShortcut(g, 0, 0, "[Dark off]", st, st.Color)
	if g.Cells[0].Underline {
		t.Fatal("opening bracket was underlined")
	}
	if !g.Cells[1].Underline {
		t.Fatal("Dark shortcut is not underlined")
	}
	if g.Cells[2].Underline {
		t.Fatal("non-shortcut letters are underlined")
	}
	g2 := NewGrid(40, 1, RGB(0, 0, 0), RGB(255, 255, 255))
	drawFooterShortcut(g2, 0, 0, "[Reader off]", st, st.Color)
	if g2.Cells[0].Underline || !g2.Cells[1].Underline || g2.Cells[2].Underline {
		t.Fatal("Reader shortcut underline is wrong")
	}
}

func TestFooterShowsJSAndGraphicsState(t *testing.T) {
	b := NewBrowser(120, 10)
	right := b.footerRight(120)
	if !strings.Contains(right, "JavaScript on") || !strings.Contains(right, "Graphics none") {
		t.Fatalf("footer right = %q", right)
	}
	b.loader.noImages = false
	b.gfx = &gfxRenderer{mode: GraphicsKitty}
	if got := b.footerRight(120); !strings.Contains(got, "Graphics kitty") {
		t.Fatalf("resolved graphics footer = %q", got)
	}
	b.noJS = true
	if got := b.footerRight(120); !strings.Contains(got, "JavaScript off") {
		t.Fatalf("JavaScript-off footer = %q", got)
	}
}

func TestJSConsoleDoesNotUseFooterText(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		console.log("noise");
		console.warn("warning");
		console.error("boom");
	`)
	b := NewBrowser(80, 10)
	b.startJS(page)
	if b.status != "" {
		t.Fatalf("console output set footer status: %q", b.status)
	}
	if !b.jsErr {
		t.Fatal("console.error did not set the footer hint")
	}
}

func TestJSReportErrorUsesFooterHint(t *testing.T) {
	b := NewBrowser(80, 10)
	b.status = "keep me"
	j := &jsRuntime{b: b}
	j.reportError(errors.New("boom"))
	if b.status != "keep me" {
		t.Fatalf("status = %q", b.status)
	}
	if !b.jsErr {
		t.Fatal("reportError did not set the footer hint")
	}
	if len(b.jsLog) == 0 || !strings.Contains(b.jsLog[0], "boom") {
		t.Fatalf("log = %v", b.jsLog)
	}
}

func TestMouseHoverShowsLinkInFooter(t *testing.T) {
	root := ParseHTML(`<html><body><p><a href="/target">target</a></p></body></html>`)
	u := mustParse(t, "https://example.com/page")
	b := NewBrowser(40, 10)
	b.page = &Page{URL: u, Base: u, Root: root}
	b.rebuild()
	if len(b.layout.Links) == 0 {
		t.Fatal("no links in layout")
	}
	r := b.layout.Links[0].Rects[0]
	top, _ := b.viewport()
	b.HandleMouse(MouseEvent{X: r.X + 1, Y: top + r.Y + 1, Motion: true})
	if b.hover != 0 {
		t.Fatalf("hover = %d, want 0", b.hover)
	}
	if got, want := b.footerLeft(), " → https://example.com/target"; got != want {
		t.Fatalf("footer = %q, want %q", got, want)
	}
	b.HandleMouse(MouseEvent{X: b.width - 1, Y: top + 1, Motion: true})
	if b.hover != -1 {
		t.Fatalf("hover = %d after leaving link", b.hover)
	}
}

func TestProcUsageFooterRoundsMemory(t *testing.T) {
	u := procUsage{rss: 18<<20 + 256<<10}
	if got, want := u.footer(), "MEM 18 MB"; got != want {
		t.Fatalf("footer = %q, want %q", got, want)
	}
	u.rss = 18<<20 + 600<<10
	if got, want := u.footer(), "MEM 19 MB"; got != want {
		t.Fatalf("footer = %q, want %q", got, want)
	}
}

func TestParseSGRMouse(t *testing.T) {
	cases := []struct {
		params  string
		release bool
		want    MouseEvent
		ok      bool
	}{
		{"<0;10;5", false, MouseEvent{Button: 0, X: 10, Y: 5, Press: true}, true},
		{"<0;10;5", true, MouseEvent{Button: 0, X: 10, Y: 5, Press: false}, true},
		{"<2;1;1", false, MouseEvent{Button: 2, X: 1, Y: 1, Press: true}, true},
		{"<8;1;1", false, MouseEvent{Button: 8, X: 1, Y: 1, Press: true}, true},
		{"<9;1;1", false, MouseEvent{Button: 9, X: 1, Y: 1, Press: true}, true},
		{"<128;1;1", false, MouseEvent{Button: 8, X: 1, Y: 1, Press: true}, true},
		{"<129;1;1", false, MouseEvent{Button: 9, X: 1, Y: 1, Press: true}, true},
		{"<136;1;1", false, MouseEvent{Button: 8, X: 1, Y: 1, Press: true}, true},
		{"<64;3;4", false, MouseEvent{Button: 0, X: 3, Y: 4, Press: true, Wheel: -1}, true},
		{"<65;3;4", false, MouseEvent{Button: 1, X: 3, Y: 4, Press: true, Wheel: 1}, true},
		{"<32;3;4", false, MouseEvent{Button: 0, X: 3, Y: 4, Press: true, Motion: true}, true},
		{"bad", false, MouseEvent{}, false},
	}
	for _, c := range cases {
		got, ok := parseSGRMouse(c.params, c.release)
		if ok != c.ok || got != c.want {
			t.Errorf("parseSGRMouse(%q,%v) = %+v,%v want %+v,%v", c.params, c.release, got, ok, c.want, c.ok)
		}
	}
}

func TestParseX10Mouse(t *testing.T) {
	ev, ok := parseX10Mouse(32, 32+10, 32+5)
	if !ok || ev != (MouseEvent{Button: 0, X: 10, Y: 5, Press: true}) {
		t.Fatalf("left press = %+v,%v", ev, ok)
	}
	ev, ok = parseX10Mouse(35, 32+2, 32+3)
	if !ok || ev != (MouseEvent{Button: 0, X: 2, Y: 3, Press: false}) {
		t.Fatalf("release = %+v,%v", ev, ok)
	}
	ev, ok = parseX10Mouse(32+64, 32+1, 32+1)
	if !ok || ev.Wheel != -1 {
		t.Fatalf("wheel up = %+v,%v", ev, ok)
	}
	ev, ok = parseX10Mouse(32+8, 32+1, 32+1)
	if !ok || ev != (MouseEvent{Button: MouseButtonBack, X: 1, Y: 1, Press: true}) {
		t.Fatalf("back button = %+v,%v", ev, ok)
	}
	ev, ok = parseX10Mouse(32+128, 32+1, 32+1)
	if !ok || ev != (MouseEvent{Button: MouseButtonBack, X: 1, Y: 1, Press: true}) {
		t.Fatalf("xterm back button = %+v,%v", ev, ok)
	}
}

func TestMouseSideButtonsNavigate(t *testing.T) {
	b := NewBrowser(40, 10)
	b.HandleMouse(MouseEvent{Button: MouseButtonBack, X: 5, Y: 5, Press: true})
	if b.status != "No previous page" {
		t.Fatalf("back status = %q", b.status)
	}
	b.HandleMouse(MouseEvent{Button: MouseButtonForward, X: 5, Y: 5, Press: true})
	if b.status != "No next page" {
		t.Fatalf("forward status = %q", b.status)
	}
}

func TestMouseTokenRoundTrip(t *testing.T) {
	cases := []MouseEvent{
		{Button: 0, X: 3, Y: 7, Press: true},
		{Button: 2, X: 1, Y: 2, Press: false},
		{Button: MouseButtonBack, X: 2, Y: 3, Press: true},
		{Button: MouseButtonForward, X: 2, Y: 3, Press: true},
		{Button: 0, X: 4, Y: 8, Press: true, Motion: true},
		{Wheel: -1, X: 9, Y: 4},
		{Wheel: 1, X: 5, Y: 6},
	}
	for _, want := range cases {
		got, ok := ParseMouseToken(mouseToken(want))
		if !ok || got != want {
			t.Errorf("round trip %+v -> %q -> %+v,%v", want, mouseToken(want), got, ok)
		}
	}
	if _, ok := ParseMouseToken("j"); ok {
		t.Error("plain key decoded as mouse")
	}
}

func withQuietStdout(t *testing.T, fn func()) {
	t.Helper()
	old := os.Stdout
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err == nil {
		os.Stdout = f
		defer func() { os.Stdout = old; f.Close() }()
	}
	fn()
}

func TestJSConfirm(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`,
		`document.title = confirm("Continue?") ? "yes" : "no";`)
	b := NewBrowser(60, 15)
	b.page = page
	keys := make(chan string, 4)
	keys <- "y"
	b.keys = keys
	withQuietStdout(t, func() { b.startJS(page) })
	if page.Title != "yes" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSPrompt(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`,
		`document.title = prompt("Name", "bob") || "cancel";`)
	b := NewBrowser(60, 15)
	b.page = page
	keys := make(chan string, 4)
	keys <- "x"
	keys <- "enter"
	b.keys = keys
	withQuietStdout(t, func() { b.startJS(page) })
	if page.Title != "x" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSDialogsNonInteractive(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`,
		`alert("hello"); document.title = String(confirm("c")) + ":" + String(prompt("p", "d"));`)
	b := NewBrowser(60, 15)
	b.page = page
	withQuietStdout(t, func() { b.startJS(page) })
	if page.Title != "false:null" {
		t.Fatalf("title = %q", page.Title)
	}
	if len(b.jsLog) < 3 {
		t.Fatalf("dialogs were not logged: %v", b.jsLog)
	}
}

func TestHelpAlertExample(t *testing.T) {
	page, err := NewLoader().Load("about:help", nil)
	if err != nil {
		t.Fatal(err)
	}
	var link *Node
	page.Root.FindAll(func(n *Node) bool {
		if link == nil && n.Tag == "a" && strings.Contains(n.Attr("onclick"), "alert(") {
			link = n
		}
		return false
	})
	if link == nil {
		t.Fatal("alert example link not found")
	}
	b := NewBrowser(80, 24)
	b.page = page
	keys := make(chan string, 2)
	keys <- "enter"
	b.keys = keys
	withQuietStdout(t, func() {
		b.startJS(page)
		b.js.dispatchClick(link)
	})
	found := false
	for _, line := range b.jsLog {
		if strings.Contains(line, "Hello from MOB³!") {
			found = true
		}
	}
	if !found {
		t.Fatalf("alert example did not run: %v", b.jsLog)
	}
}

func TestConsoleRequestPane(t *testing.T) {
	b := NewBrowser(60, 20)
	gen := b.loader.beginPage()
	id, ok := b.loader.startRequest(gen, "GET", "https://example.com/a.css", "css")
	if !ok {
		t.Fatal("request was not started")
	}
	b.loader.finishRequest(id, gen, 200, 2048, 10*time.Millisecond, nil)
	b.loader.recordCacheHit(gen, "GET", "https://example.com/b.js", "js", 512)
	lines := b.requestLines()
	if len(lines) != 2 || !strings.Contains(lines[0], "200") ||
		!strings.Contains(lines[0], "2.0K") || !strings.Contains(lines[0], "css") ||
		!strings.Contains(lines[0], "https://example.com/a.css") {
		t.Fatalf("request lines = %q", lines)
	}
	if !strings.Contains(lines[1], "js") || !strings.Contains(lines[1], "cached") || strings.Contains(lines[1], "512B") {
		t.Fatalf("cached request line = %q", lines[1])
	}
	if summary := b.requestCacheSummary(); !strings.Contains(summary, "/") || !strings.Contains(summary, "entries") {
		t.Fatalf("cache summary = %q", summary)
	}
	if got := humanSize(512); got != "512B" {
		t.Fatalf("humanSize = %q", got)
	}

	b.consoleOpen = true
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	b.handleConsoleKey("tab")
	if b.consolePane != 1 {
		t.Fatalf("console pane = %d, want requests", b.consolePane)
	}
	b.handleConsoleKey("k")
	if b.reqScroll != 1 {
		t.Fatalf("request scroll = %d", b.reqScroll)
	}
	b.handleConsoleKey("G")
	if b.reqScroll != 0 {
		t.Fatalf("request scroll after G = %d", b.reqScroll)
	}
}

func TestConsoleRequestsHeaderCountsRealRequests(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	b.url = mustParse(t, "about:home")
	b.loader.beginPage()
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	l := newConsoleLayout(20, consoleLogs)
	line := b.prev[l.tabsY]
	if !strings.Contains(line, "Requests (0)") {
		t.Fatalf("empty request tab = %q, want Requests (0)", line)
	}
}

func TestConsoleStorageSection(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`,
		`localStorage.setItem("theme", "dark"); sessionStorage.setItem("tab", "1");`)
	b := NewBrowser(60, 20)
	b.page = page
	b.url = mustParse(t, "https://example.com/")
	withQuietStdout(t, func() { b.startJS(page) })
	b.loader.cookies.SetCookies(b.url, []*http.Cookie{
		{Name: "sid", Value: "abc", Path: "/", HttpOnly: true},
		{Name: "seen", Value: "yes", Path: "/"},
	})
	joined := strings.Join(b.storageLines(), "\n")
	for _, want := range []string{
		"Cookies", "sid=abc", "[HttpOnly]", "seen=yes",
		"localStorage", "theme = dark",
		"sessionStorage", "tab = 1",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("storage lines missing %q:\n%s", want, joined)
		}
	}
}

func TestConsoleOnlyOpenSectionHasBody(t *testing.T) {
	b := NewBrowser(60, 20)
	b.color = true
	b.consoleOpen = true
	b.jsLog = []string{"[log] hidden when requests open"}
	b.consolePane = consoleRequests
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	text := strings.Join(b.prev, "\n")
	if strings.Contains(text, "hidden when requests open") {
		t.Fatal("logs body was rendered while requests was open")
	}
	l := newConsoleLayout(20, consoleRequests)
	if !strings.Contains(b.prev[l.tabsY], "JS logs") {
		t.Fatalf("logs tab missing: %q", b.prev[l.tabsY])
	}
	if !strings.Contains(b.prev[l.tabsY], "Requests") {
		t.Fatalf("requests tab missing: %q", b.prev[l.tabsY])
	}
}

func TestConsoleScrollReachesFirstWrappedLine(t *testing.T) {
	b := NewBrowser(40, 20)
	b.jsLog = []string{strings.Repeat("x", 400)}
	b.consoleOpen = true
	b.handleConsoleKey("g")
	want := len(b.consoleLines(b.width - 2))
	if b.consoleScroll != want {
		t.Fatalf("console scroll = %d, want %d", b.consoleScroll, want)
	}
	if want <= 1 {
		t.Fatalf("test line did not wrap: %d lines", want)
	}
}

func TestConsoleHTMLShowsRawHTML(t *testing.T) {
	b := NewBrowser(40, 20)
	b.page = &Page{RawHTML: "<html>\n<body><p>Hello raw page</p></body>\n</html>"}
	joined := strings.Join(b.htmlLines(20), "\n")
	if !strings.Contains(joined, "<html>") || !strings.Contains(joined, "Hello raw") {
		t.Fatalf("HTML lines = %q", joined)
	}
	// A different page invalidates the cached wrapping.
	b.page = &Page{RawHTML: "<html><body>second</body></html>"}
	if got := strings.Join(b.htmlLines(20), "\n"); !strings.Contains(got, "second") {
		t.Fatalf("HTML cache was not refreshed: %q", got)
	}
	// Long source lines wrap to the console width.
	b.page = &Page{RawHTML: strings.Repeat("x", 45)}
	for _, line := range b.htmlLines(20) {
		if stringWidth(line) > 20 {
			t.Fatalf("HTML line wider than requested: %q", line)
		}
	}
}

func TestConsoleHTMLOpensAtTop(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	b.page = &Page{RawHTML: "<html>\n" + strings.Repeat("<p>line</p>\n", 100) + "</html>"}
	for i := 0; i < 3; i++ {
		b.handleConsoleKey("tab")
	}
	if b.consolePane != consoleHTML {
		t.Fatalf("console pane = %d, want HTML", b.consolePane)
	}
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	text := strings.Join(b.prev, "\n")
	if !strings.Contains(text, "<html>") {
		t.Fatalf("HTML section did not start at the top: %q", text)
	}
	if strings.Contains(text, "</html>") {
		t.Fatalf("HTML section opened at the bottom: %q", text)
	}
}

func TestConsoleClickSwitchesPane(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	l := newConsoleLayout(20, consoleLogs)
	tabX := func(pane int) int {
		for _, hit := range b.consoleTabHits {
			if hit.pane == pane {
				return hit.x + 1
			}
		}
		t.Fatalf("no clickable tab for pane %d", pane)
		return 0
	}
	// Clicking the log body keeps the logs section open.
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: l.bodyY + 1, Press: true})
	if b.consolePane != consoleLogs {
		t.Fatalf("console pane after log click = %d, want logs", b.consolePane)
	}
	// Clicking the Requests tab opens requests.
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: tabX(consoleRequests), Y: l.tabsY + 1, Press: true})
	if b.consolePane != consoleRequests {
		t.Fatalf("console pane after tab click = %d, want requests", b.consolePane)
	}
	// A body row in the requests section keeps it open.
	reqLayout := newConsoleLayout(20, consoleRequests)
	if reqLayout.bodyY < 0 {
		t.Fatal("requests section has no body")
	}
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: reqLayout.bodyY + 1, Press: true})
	if b.consolePane != consoleRequests {
		t.Fatalf("console pane after request click = %d, want requests", b.consolePane)
	}
	// Clicking the Logs tab returns to the logs section.
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: tabX(consoleLogs), Y: l.tabsY + 1, Press: true})
	if b.consolePane != consoleLogs {
		t.Fatalf("console pane after tab click = %d, want logs", b.consolePane)
	}
}

func TestConsoleFooterIconClosesConsole(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	if !strings.HasSuffix(b.prev[19], ">_") {
		t.Fatalf("console footer missing >_ icon: %q", b.prev[19])
	}
	found := false
	for _, hit := range b.footerHits {
		if hit.kind == headerConsole && hit.y == 19 && hit.x+hit.w == 60 {
			found = true
		}
	}
	if !found {
		t.Fatalf("console footer icon has no click region: %+v", b.footerHits)
	}
	withQuietStdout(t, func() {
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 60, Y: 20, Press: true})
	})
	if b.consoleOpen {
		t.Fatal("clicking the footer icon left the console open")
	}
	if b.status != exitHint {
		t.Fatalf("status = %q, want %q", b.status, exitHint)
	}
}

func TestConsoleTabsHighlightActiveSection(t *testing.T) {
	b := NewBrowser(60, 20)
	normal := RGB(0x1f, 0x29, 0x37)
	active := RGB(0x2f, 0x6f, 0xbf)
	style := &Style{Bold: true, Color: RGB(0xdd, 0xe6, 0xee), FontSize: 16, LineHeight: 1}
	check := func() {
		g := NewGrid(60, 1, RGB(0, 0, 0), RGB(0, 0, 0))
		b.drawConsoleTabs(g, 0, 60, normal, active, style, RGB(0xdd, 0xe6, 0xee))
		seen := 0
		for _, hit := range b.consoleTabHits {
			want := normal
			if hit.pane == b.consolePane {
				want = active
			}
			if got := g.Cells[hit.x].BG; got != want {
				t.Fatalf("tab %d background = %+v, want %+v", hit.pane, got, want)
			}
			seen++
		}
		if seen != consoleSectionCount {
			t.Fatalf("tab hits = %d, want %d", seen, consoleSectionCount)
		}
	}
	check()
	b.consolePane = consoleRequests
	check()
}

func TestConsoleMouseWheelScrollsHoveredPane(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	b.consolePane = consoleRequests
	l := newConsoleLayout(20, consoleRequests)
	b.HandleMouse(MouseEvent{X: 5, Y: l.bodyY + 2, Wheel: -1})
	if b.reqScroll != 3 || b.consoleScroll != 0 {
		t.Fatalf("wheel scroll = logs %d, requests %d", b.consoleScroll, b.reqScroll)
	}
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	logsX := 1
	for _, hit := range b.consoleTabHits {
		if hit.pane == consoleLogs {
			logsX = hit.x + 1
		}
	}
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: logsX, Y: l.tabsY + 1, Press: true})
	if b.consolePane != consoleLogs {
		t.Fatalf("console pane after logs tab = %d, want logs", b.consolePane)
	}
	b.HandleMouse(MouseEvent{X: 5, Y: 2, Wheel: -1})
	if b.reqScroll != 3 || b.consoleScroll != 3 {
		t.Fatalf("wheel scroll = logs %d, requests %d", b.consoleScroll, b.reqScroll)
	}
	b.HandleMouse(MouseEvent{X: 5, Y: 2, Wheel: 1})
	if b.consoleScroll != 0 {
		t.Fatalf("wheel down scroll = logs %d", b.consoleScroll)
	}
}

func TestConsolePausesTimers(t *testing.T) {
	page := newJSPage(`<html><body><p id="out">before</p></body></html>`, `
		setTimeout(function () {
			document.getElementById("out").textContent = "timer";
		}, 10);
	`)
	b := NewBrowser(60, 15)
	b.page = page
	withQuietStdout(t, func() { b.startJS(page) })
	b.consoleOpen = true
	if b.runJSTimers() {
		t.Fatal("timers ran while the console was open")
	}
	time.Sleep(20 * time.Millisecond)
	if b.runJSTimers() {
		t.Fatal("timers ran while the console was open")
	}
	if got := page.Root.FindFirst("p").TextContent(); got != "before" {
		t.Fatalf("timer updated the page while console open: %q", got)
	}
}

func TestPageContentCannotInjectTerminalControls(t *testing.T) {
	u := mustParse(t, "https://example.com/")
	root := ParseHTML("<html><head><title>evil\x1b]52;c;x\x07</title></head><body><p>hi\x1b[2Jthere</p></body></html>")
	page := &Page{URL: u, Base: u, Root: root, Title: "evil\x1b]52;c;x\x07", RawHTML: "<html>\x1b]0;pwn\x07</html>"}
	b := NewBrowser(60, 20)
	b.page = page
	b.url = u
	b.layout = BuildLayout(page.Root, nil, 60)
	withQuietStdout(t, func() { b.draw() })
	joined := strings.Join(b.prev, "\n")
	for _, bad := range []string{"\x1b]52", "\x1b[2J", "\x07", "\x1b]0", "\u009b"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("page content injected terminal control %q: %q", bad, joined)
		}
	}
	if !strings.Contains(joined, "there") {
		t.Fatalf("page text was lost: %q", joined)
	}
}

func TestConsoleContentCannotInjectTerminalControls(t *testing.T) {
	b := NewBrowser(60, 20)
	b.color = true
	b.consoleOpen = true
	b.jsLog = []string{"[log] \x1b]52;c;evil\x07\x1b[2J"}
	b.page = &Page{RawHTML: "<html>\x1b]0;pwn\x07</html>"}
	check := func(where string) {
		joined := strings.Join(b.prev, "\n")
		for _, bad := range []string{"\x1b]52", "\x1b[2J", "\x07", "\x1b]0", "\u009b"} {
			if strings.Contains(joined, bad) {
				t.Fatalf("%s injected terminal control %q: %q", where, bad, joined)
			}
		}
	}
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	check("log")

	gen := b.loader.beginPage()
	id, ok := b.loader.startRequest(gen, "GET", "https://example.com/\x1b]52;c;evil\x07", "doc")
	if !ok {
		t.Fatal("request was not started")
	}
	b.loader.finishRequest(id, gen, 200, 1, 0, nil)
	b.selectConsolePane(consoleRequests)
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	check("request")

	b.js = &jsRuntime{
		b:                  b,
		storageLocalData:   map[string]string{"k\x1b[2J": "v\x1b]52;c;x\x07"},
		storageSessionData: map[string]string{},
	}
	b.selectConsolePane(consoleStorage)
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	check("storage")

	b.selectConsolePane(consoleHTML)
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	check("HTML")
}

func TestDialogRendering(t *testing.T) {
	b := NewBrowser(60, 15)
	b.dialog = &jsDialog{kind: "confirm", message: "Continue with this action?"}
	drawQuiet(b)
	text := strings.Join(b.prev, "\n")
	if !strings.Contains(text, "JavaScript confirm") || !strings.Contains(text, "Continue with this action?") {
		t.Fatalf("dialog not rendered: %q", text)
	}
}

func TestTimerRedrawsAreCoalesced(t *testing.T) {
	page := newJSPage(`<html><body><p id="out">before</p></body></html>`, `
		setInterval(function () {
			document.getElementById("out").textContent = String(Date.now());
		}, 10);
	`)
	b := NewBrowser(60, 15)
	b.page = page
	withQuietStdout(t, func() { b.startJS(page) })
	b.rebuild()
	first := b.layout
	b.lastBuild = 100 * time.Millisecond
	b.lastRebuild = time.Now()
	time.Sleep(25 * time.Millisecond)
	if b.runJSTimers() {
		t.Fatal("timer rebuilt inside the coalescing interval")
	}
	if b.layout != first {
		t.Fatal("layout changed inside the coalescing interval")
	}
	if !b.js.dirty {
		t.Fatal("dirty flag was cleared before the deferred rebuild")
	}
	b.lastRebuild = time.Now().Add(-time.Second)
	if !b.runJSTimers() {
		t.Fatal("deferred timer rebuild did not run after the interval")
	}
	if b.js.dirty {
		t.Fatal("dirty flag was not cleared after rebuilding")
	}
	if b.layout == first {
		t.Fatal("layout was not rebuilt after the interval")
	}
}

func TestClickOutsideURLPromptClosesIt(t *testing.T) {
	b := NewBrowser(80, 10)
	b.layout = &Layout{Height: 10}
	b.openURLPrompt()
	drawQuiet(b)
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: 5, Press: true})
	if b.prompt {
		t.Fatal("click outside the URL field left the prompt open")
	}
	if b.status != exitHint {
		t.Fatalf("status = %q, want %q", b.status, exitHint)
	}
}

func TestClickInURLPromptKeepsItOpen(t *testing.T) {
	b := NewBrowser(80, 10)
	b.layout = &Layout{Height: 10}
	b.openURLPrompt()
	drawQuiet(b)
	// Row 2 (1-based) is the URL field on a normal header.
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: 2, Press: true})
	if !b.prompt {
		t.Fatal("click on the URL field closed the prompt")
	}
}

func TestShiftMouseDragLeavesURLPromptAlone(t *testing.T) {
	b := NewBrowser(80, 10)
	b.url = mustParse(t, "https://example.com/page")
	b.layout = &Layout{Height: 10}
	b.openURLPrompt()
	drawQuiet(b)
	before := b.promptText
	prefixW := stringWidth(" Open: ")
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: prefixW + 5 + 1, Y: 2, Press: true, Shift: true})
	if !b.skipDraw {
		t.Fatal("shift+press did not suppress the redraw")
	}
	b.skipDraw = false
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: prefixW + 10 + 1, Y: 2, Motion: true, Shift: true})
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: prefixW + 10 + 1, Y: 2, Press: false, Shift: true})
	if !b.prompt {
		t.Fatal("shift+drag closed the URL prompt")
	}
	if b.promptText != before {
		t.Fatalf("shift+drag changed the URL to %q", b.promptText)
	}
	if !b.promptSelectAll {
		t.Fatal("shift+drag cleared the prompt selection")
	}
	if !b.skipDraw {
		t.Fatal("shift+release did not suppress the redraw")
	}
}

func TestURLPromptMouseClickPlacesCursor(t *testing.T) {
	b := NewBrowser(80, 10)
	b.url = mustParse(t, "https://example.com/page")
	b.layout = &Layout{Height: 10}
	b.openURLPrompt()
	drawQuiet(b)

	prefixW := stringWidth(" Open: ")
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: prefixW + 4 + 1, Y: 2, Press: true})
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: prefixW + 4 + 1, Y: 2, Press: false})
	if b.promptCursor != 4 {
		t.Fatalf("cursor = %d, want 4", b.promptCursor)
	}
	if b.promptSelectAll {
		t.Fatal("click did not collapse the select-all prompt")
	}
}

func TestShiftMouseTokenRoundTrip(t *testing.T) {
	want := MouseEvent{Button: MouseButtonLeft, X: 4, Y: 9, Press: true, Shift: true}
	got, ok := ParseMouseToken(mouseToken(want))
	if !ok || got != want {
		t.Fatalf("round trip = %+v, %v; want %+v", got, ok, want)
	}
}

func TestDownloadLinkOpensCurlPopup(t *testing.T) {
	u := mustParse(t, "https://filesamples.com/formats/html")
	doc := ParseHTML(`<a href="/samples/code/html/sample1.html" download="">Download</a>`)
	b := NewBrowser(60, 15)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.layout = BuildLayout(doc, nil, 60)
	if len(b.layout.Links) != 1 {
		t.Fatalf("links = %d", len(b.layout.Links))
	}
	b.focus = 0
	b.followFocused()
	if b.download == nil {
		t.Fatal("download link did not open the curl popup")
	}
	want := "curl -L -o 'sample1.html' 'https://filesamples.com/samples/code/html/sample1.html'"
	if b.download.command != want {
		t.Fatalf("command = %q, want %q", b.download.command, want)
	}
	// The download link must not navigate.
	if b.url.String() != "https://filesamples.com/formats/html" {
		t.Fatalf("download link navigated to %s", b.url)
	}
}

func TestDownloadAttributeNameWins(t *testing.T) {
	u := mustParse(t, "https://example.com/files/")
	doc := ParseHTML(`<a href="x.bin" download="chosen.zip">Download</a>`)
	b := NewBrowser(60, 15)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.layout = BuildLayout(doc, nil, 60)
	b.focus = 0
	b.followFocused()
	if b.download == nil {
		t.Fatal("download link did not open the popup")
	}
	want := "curl -L -o 'chosen.zip' 'https://example.com/files/x.bin'"
	if b.download.command != want {
		t.Fatalf("command = %q, want %q", b.download.command, want)
	}
}

func TestDownloadPopupCopiesWithY(t *testing.T) {
	u := mustParse(t, "https://example.com/file.zip")
	b := NewBrowser(60, 15)
	b.page = &Page{URL: u, Base: u, Root: ParseHTML(`<p>x</p>`)}
	b.url = u
	b.showDownload(u.String(), "")
	drawQuiet(b)
	if screen := strings.Join(b.prev, "\n"); !strings.Contains(screen, "curl -L -o") ||
		!strings.Contains(screen, "Download") {
		t.Fatalf("download popup not rendered:\n%s", screen)
	}
	var buf bytes.Buffer
	b.clipboardWriter = &buf
	wantCommand := b.download.command
	b.HandleKey("y")
	out := buf.String()
	if !strings.HasPrefix(out, "\x1b]52;c;") || !strings.HasSuffix(out, "\x07") {
		t.Fatalf("clipboard sequence = %q", out)
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(out, "\x1b]52;c;"), "\x07")
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != wantCommand {
		t.Fatalf("copied %q, want %q", decoded, wantCommand)
	}
	if b.download != nil {
		t.Fatal("copying did not close the popup")
	}
	if !strings.Contains(b.status, "Copied") {
		t.Fatalf("status = %q", b.status)
	}
	b.HandleKey("esc")
	if b.download != nil {
		t.Fatal("Esc did not close the popup")
	}
}

func TestNonHTMLNavigationOffersDownload(t *testing.T) {
	u := mustParse(t, "https://example.com/files/archive.zip")
	b := NewBrowser(60, 15)
	page := &Page{
		URL: u, Base: u, Root: ParseHTML(`<p>cannot display</p>`),
		Download: true, DownloadName: "archive.zip",
	}
	b.loadSeq = 1
	b.finishLoad(loadResult{seq: 1, page: page, u: u, push: true, method: http.MethodGet})
	if b.download == nil {
		t.Fatal("non-HTML response did not offer the curl popup")
	}
	if b.page != nil {
		t.Fatal("non-HTML response was applied as a page")
	}
	if !strings.Contains(b.download.command, "archive.zip") || !strings.Contains(b.download.command, u.String()) {
		t.Fatalf("command = %q", b.download.command)
	}
	if len(b.history) != 0 {
		t.Fatalf("download pushed history: %v", b.history)
	}
}

func TestFileInputClickShowsUnsupportedNotice(t *testing.T) {
	u := mustParse(t, "https://example.com/form")
	doc := ParseHTML(`<form action="/upload" method="post"><input type="file" name="f"><button>Send</button></form>`)
	b := NewBrowser(60, 15)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	file := inputByName(doc, "f")
	if file == nil {
		t.Fatal("file input not found")
	}
	var field *Frag
	for i := range b.layout.Frags {
		if b.layout.Frags[i].Node == file {
			field = &b.layout.Frags[i]
			break
		}
	}
	if field == nil {
		t.Fatal("file input fragment not laid out")
	}
	top, _ := b.viewport()
	x := field.X + 1
	y := top + field.Y - b.scroll + 1
	withQuietStdout(t, func() {
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: true})
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: false})
	})
	if b.notice == nil {
		t.Fatal("clicking the file input did not open a notice")
	}
	if !strings.Contains(b.notice.title, "not supported") ||
		!strings.Contains(b.notice.message, "not supported") {
		t.Fatalf("notice = %+v", b.notice)
	}
	drawQuiet(b)
	if screen := strings.Join(b.prev, "\n"); !strings.Contains(screen, "not supported") {
		t.Fatalf("upload notice not rendered:\n%s", screen)
	}
	b.HandleKey("esc")
	if b.notice != nil {
		t.Fatal("Esc did not close the upload notice")
	}
}

func TestDownloadCommandIsNotDrawnSelected(t *testing.T) {
	u := mustParse(t, "https://example.com/file.zip")
	b := NewBrowser(60, 15)
	b.page = &Page{URL: u, Base: u, Root: ParseHTML(`<p>x</p>`)}
	b.url = u
	b.showDownload(u.String(), "")
	g := NewGrid(60, 15, RGB(0, 0, 0), RGB(255, 255, 255))
	b.paintDownload(g)
	found := false
	for i := 0; i+3 < len(g.Cells); i++ {
		if g.Cells[i].R == 'c' && g.Cells[i+1].R == 'u' && g.Cells[i+2].R == 'r' && g.Cells[i+3].R == 'l' {
			found = true
			if g.Cells[i].BG == RGB(0x2f, 0x6f, 0xbf) {
				t.Fatal("curl command is drawn with the blue selection background")
			}
			if g.Cells[i].BG != modalBG {
				t.Fatalf("curl command background = %+v", g.Cells[i].BG)
			}
			break
		}
	}
	if !found {
		t.Fatal("curl command not rendered")
	}
}

func TestModalCloseButtonClosesWithMouse(t *testing.T) {
	u := mustParse(t, "https://example.com/file.zip")
	b := NewBrowser(60, 15)
	b.page = &Page{URL: u, Base: u, Root: ParseHTML(`<p>x</p>`)}
	b.url = u
	b.showDownload(u.String(), "")
	drawQuiet(b)
	r := b.modalClose
	if r.W <= 0 {
		t.Fatal("download modal close button was not recorded")
	}
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: r.X + r.W/2 + 1, Y: r.Y + 1, Press: true})
	if b.download != nil {
		t.Fatal("clicking the modal close button did not close the download popup")
	}

	b.showDownload(u.String(), "")
	drawQuiet(b)
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 1, Y: 1, Press: true})
	if b.download != nil {
		t.Fatal("clicking the backdrop did not close the download popup")
	}

	b.showNotice("Upload not supported", "not supported")
	drawQuiet(b)
	r = b.modalClose
	if r.W <= 0 {
		t.Fatal("notice modal close button was not recorded")
	}
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: r.X + r.W/2 + 1, Y: r.Y + 1, Press: true})
	if b.notice != nil {
		t.Fatal("clicking the modal close button did not close the notice")
	}
}

func TestJSDialogMouseClose(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `alert("Continue?");`)
	b := NewBrowser(60, 15)
	b.page = page
	// The alert is one line at width 60: box 56 wide, 7 tall, centered at
	// x=2, y=4. The close button sits on the top-right border at columns
	// 55-57 (zero-based); mouse coordinates are one-based.
	keys := make(chan string, 2)
	keys <- mouseToken(MouseEvent{Button: MouseButtonLeft, X: 57, Y: 5, Press: true})
	keys <- "esc"
	b.keys = keys
	withQuietStdout(t, func() { b.startJS(page) })
	if b.dialog != nil {
		t.Fatal("dialog is still open")
	}
	if len(keys) != 1 {
		t.Fatal("mouse click did not close the JavaScript dialog")
	}
}

func TestModalClearsPageGlyphs(t *testing.T) {
	u := mustParse(t, "https://example.com/file.zip")
	b := NewBrowser(60, 15)
	b.page = &Page{URL: u, Base: u, Root: ParseHTML(`<p>x</p>`)}
	b.url = u
	b.showDownload(u.String(), "")
	g := NewGrid(60, 15, RGB(0, 0, 0), RGB(255, 255, 255))
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			g.Put(x, y, 'X', &Style{Underline: true, Strike: true}, RGB(0, 0, 0), Color{})
		}
	}
	b.paintDownload(g)
	r := b.modalBox
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			c := g.Cells[y*g.W+x]
			if c.R == 'X' {
				t.Fatalf("page glyph survived inside the modal at (%d,%d)", x, y)
			}
			if c.Underline || c.Strike {
				t.Fatalf("page text decoration survived inside the modal at (%d,%d)", x, y)
			}
		}
	}
	// The page backdrop outside the box is dimmed rather than left readable,
	// so page text does not appear to leak into the modal edges.
	c := g.Cells[0]
	if c.Underline {
		t.Fatal("page link underline survived on the dimmed backdrop")
	}
	if c.BG != modalDimBG || c.FG != modalDimFG {
		t.Fatalf("page backdrop cell = fg %+v bg %+v, want dimmed fg %+v bg %+v", c.FG, c.BG, modalDimFG, modalDimBG)
	}
}

func TestBrowserFixedLayoutUsesViewportHeight(t *testing.T) {
	u := mustParse(t, "https://example.com/")
	doc := ParseHTML(`<div>top</div><div style="position:fixed;bottom:0;right:0">bar</div>`)
	b := NewBrowser(30, 10)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	bar := textFrag(b.layout, "bar")
	if bar == nil {
		t.Fatal("fixed fragment not laid out")
	}
	wantY := b.viewportHeight() - 1
	if bar.Y != wantY || bar.X != 27 {
		t.Fatalf("fixed box at (%d,%d), want (27,%d)", bar.X, bar.Y, wantY)
	}
}

func TestTextareaEnterInsertsNewlineAndEscExits(t *testing.T) {
	u := mustParse(t, "https://example.com/form")
	doc := ParseHTML(`<form action="/submit" method="post"><textarea name="t"></textarea></form>`)
	b := NewBrowser(60, 15)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	area := doc.FindFirst("textarea")
	if area == nil {
		t.Fatal("textarea not found")
	}
	b.setInputFocus(area)
	b.HandleKey("a")
	b.HandleKey("enter")
	b.HandleKey("b")
	if got := inputValue(area); got != "a\nb" {
		t.Fatalf("value = %q, want %q", got, "a\nb")
	}
	if b.inputFocus != area {
		t.Fatal("Enter left the textarea")
	}
	if b.url.String() != "https://example.com/form" {
		t.Fatalf("Enter navigated to %s", b.url)
	}
	b.HandleKey("esc")
	if b.inputFocus != nil || area.InputFocused {
		t.Fatal("Esc did not exit the textarea")
	}
}

func TestTextareaTypingPatchesLayout(t *testing.T) {
	u := mustParse(t, "https://example.com/form")
	doc := ParseHTML(`<form action="/submit"><textarea name="t">` + strings.Repeat("a", 50) + `</textarea></form>`)
	b := NewBrowser(60, 15)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	area := doc.FindFirst("textarea")
	b.setInputFocus(area)
	before := b.layout
	b.HandleKey("x")
	if b.layout != before {
		t.Fatal("typing triggered a full relayout")
	}
	found := false
	for _, f := range b.layout.Frags {
		if f.Kind == fragText && f.Node == area && strings.Contains(f.Text, "x") {
			found = true
		}
	}
	if !found {
		t.Fatal("patched layout does not show the typed character")
	}
}

func TestReaderModeModalLinkWarnsWithoutScrolling(t *testing.T) {
	for _, href := range []string{"#", "#missing"} {
		u := mustParse(t, "https://example.com/article")
		doc := ParseHTML(`<p>hello</p><p><a href="` + href + `">open modal</a></p>`)
		b := NewBrowser(40, 10)
		b.page = &Page{URL: u, Base: u, Root: doc}
		b.url = u
		b.reader = true
		b.readerRoot = doc
		b.rebuild()
		if len(b.layout.Links) == 0 {
			t.Fatal("link not laid out")
		}
		b.layout.Height = 100
		b.scroll = 3
		b.focus = 0
		b.followFocused()
		if b.status != readerLinkUnavailable {
			t.Fatalf("href %q status = %q, want %q", href, b.status, readerLinkUnavailable)
		}
		if b.scroll != 3 {
			t.Fatalf("href %q scrolled to %d", href, b.scroll)
		}
		if b.url.String() != "https://example.com/article" {
			t.Fatalf("href %q navigated to %s", href, b.url)
		}
	}
}

func TestReaderModeAnchorLinkStillScrolls(t *testing.T) {
	u := mustParse(t, "https://example.com/article")
	doc := ParseHTML(`<p>hello</p><h2 id="target">target</h2><p><a href="#target">jump</a></p>`)
	b := NewBrowser(40, 10)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.reader = true
	b.readerRoot = doc
	b.rebuild()
	b.layout.Height = 100
	b.focus = 0
	b.followFocused()
	if b.status == readerLinkUnavailable {
		t.Fatal("existing anchor was rejected in reader mode")
	}
	if b.scroll == 0 {
		t.Fatal("existing anchor did not scroll")
	}
}

func TestCopySelectionUsesOSC52(t *testing.T) {
	b := NewBrowser(40, 10)
	b.layout = &Layout{Frags: []Frag{{Kind: fragText, Text: "hello", X: 0, Y: 0, W: 5}}}
	b.selAnchor = cellPos{X: 0, Y: 0}
	b.selCursor = cellPos{X: 4, Y: 0}
	var buf bytes.Buffer
	b.clipboardWriter = &buf
	b.HandleKey("y")
	out := buf.String()
	if !strings.HasPrefix(out, "\x1b]52;c;") || !strings.HasSuffix(out, "\x07") {
		t.Fatalf("clipboard sequence = %q", out)
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(out, "\x1b]52;c;"), "\x07")
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || string(decoded) != "hello" {
		t.Fatalf("clipboard text = %q, %v", decoded, err)
	}
	if b.status != "Copied 5 chars" {
		t.Fatalf("status = %q", b.status)
	}
	if b.hasSelection() {
		t.Fatal("copy did not clear the selection")
	}
}

func TestCopySelectionWithoutSelection(t *testing.T) {
	b := NewBrowser(40, 10)
	var buf bytes.Buffer
	b.clipboardWriter = &buf
	b.HandleKey("y")
	if buf.Len() != 0 {
		t.Fatalf("wrote clipboard data without a selection: %q", buf.String())
	}
	if b.status != "No selection" {
		t.Fatalf("status = %q", b.status)
	}
}

func TestConsoleJavaScriptInputRow(t *testing.T) {
	page := newJSPage(`<html><body><p id="out">before</p></body></html>`, ``)
	b := NewBrowser(60, 20)
	b.page = page
	withQuietStdout(t, func() { b.startJS(page) })
	b.consoleOpen = true
	b.handleConsoleKey("i")
	if b.consolePane != consoleLogs || !b.consoleEdit {
		t.Fatalf("console state = pane %d edit %v, want logs input", b.consolePane, b.consoleEdit)
	}
	b.consoleText = `document.getElementById("out").textContent = "after"`
	b.consoleCursor = len(b.consoleText)
	b.handleConsoleKey("enter")
	if got := page.Root.FindFirst("p").TextContent(); got != "after" {
		t.Fatalf("input did not run: %q", got)
	}
	if b.consoleText != "" || b.consoleCursor != 0 {
		t.Fatalf("input was not cleared: %q cursor=%d", b.consoleText, b.consoleCursor)
	}
	joined := strings.Join(b.jsLog, "\n")
	if !strings.Contains(joined, "[input]") || !strings.Contains(joined, "[result] after") {
		t.Fatalf("console log = %q", joined)
	}
	if b.consolePane != consoleLogs {
		t.Fatalf("console pane after running input = %d, want logs", b.consolePane)
	}
}

func TestConsoleInputKeepsNavigationKeysAsText(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	b.consolePane = consoleLogs
	b.consoleEdit = true
	b.handleConsoleKey("q")
	b.handleConsoleKey("j")
	b.handleConsoleKey("!")
	if !b.consoleOpen {
		t.Fatal("typing q closed or quit the console")
	}
	if b.consoleText != "qj!" {
		t.Fatalf("console input = %q", b.consoleText)
	}
}

func TestConsoleTabCyclesSections(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	b.handleConsoleKey("tab")
	if b.consolePane != consoleRequests {
		t.Fatalf("pane after first Tab = %d, want requests", b.consolePane)
	}
	b.handleConsoleKey("tab")
	if b.consolePane != consoleStorage {
		t.Fatalf("pane after second Tab = %d, want storage", b.consolePane)
	}
	b.handleConsoleKey("tab")
	if b.consolePane != consoleHTML {
		t.Fatalf("pane after third Tab = %d, want HTML", b.consolePane)
	}
	b.handleConsoleKey("tab")
	if b.consolePane != consoleLogs {
		t.Fatalf("pane after fourth Tab = %d, want logs", b.consolePane)
	}
}

func TestConsoleClickInputRowFocusesIt(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	l := newConsoleLayout(20, consoleLogs)
	if l.inputY < 0 {
		t.Fatal("expected an input row inside the logs section")
	}
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: l.inputY + 1, Press: true})
	if b.consolePane != consoleLogs || !b.consoleEdit {
		t.Fatalf("console state = pane %d edit %v, want logs input", b.consolePane, b.consoleEdit)
	}
	// Clicking the log body above the input row stops editing but keeps logs open.
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: l.bodyY + 1, Press: true})
	if b.consolePane != consoleLogs || b.consoleEdit {
		t.Fatalf("console state after log click = pane %d edit %v", b.consolePane, b.consoleEdit)
	}
}

func TestConsoleInputRowStaysGrayWhenFocused(t *testing.T) {
	b := NewBrowser(60, 20)
	b.color = true
	b.consoleOpen = true
	b.consoleEdit = true
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	l := newConsoleLayout(20, consoleLogs)
	if l.inputY < 0 {
		t.Fatal("expected an input row")
	}
	const activeBlue = "48;2;47;111;191"
	if strings.Contains(b.prev[l.inputY], activeBlue) {
		t.Fatalf("input row turned blue when focused: %q", b.prev[l.inputY])
	}
	if !strings.Contains(b.prev[l.tabsY], activeBlue) {
		t.Fatalf("logs tab is not highlighted: %q", b.prev[l.tabsY])
	}
}

func TestConsoleInputHistory(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	b.consoleEdit = true
	for _, src := range []string{"one", "two"} {
		b.consoleText = src
		b.consoleCursor = len(src)
		b.handleConsoleKey("enter")
	}
	b.consoleText = "draft"
	b.consoleCursor = len(b.consoleText)
	b.handleConsoleKey("up")
	if b.consoleText != "two" {
		t.Fatalf("first Up = %q, want two", b.consoleText)
	}
	b.handleConsoleKey("up")
	if b.consoleText != "one" {
		t.Fatalf("second Up = %q, want one", b.consoleText)
	}
	b.handleConsoleKey("down")
	if b.consoleText != "two" {
		t.Fatalf("first Down = %q, want two", b.consoleText)
	}
	b.handleConsoleKey("down")
	if b.consoleText != "draft" {
		t.Fatalf("second Down = %q, want the draft", b.consoleText)
	}
	if b.consoleCursor != len(b.consoleText) {
		t.Fatalf("cursor = %d, want end of %q", b.consoleCursor, b.consoleText)
	}
	// Re-running the newest entry does not duplicate it in history.
	b.consoleText = "two"
	b.consoleCursor = len(b.consoleText)
	b.handleConsoleKey("enter")
	if len(b.consoleHist) != 2 {
		t.Fatalf("history = %v, want two entries", b.consoleHist)
	}
}

func TestHasModuleScripts(t *testing.T) {
	doc := ParseHTML(`<html><head><script type="module" src="app.js"></script><script>1</script></head></html>`)
	if !hasModuleScripts(doc) {
		t.Fatal("module script was not detected")
	}
	if hasModuleScripts(ParseHTML(`<html><head><script type="text/javascript">1</script></head></html>`)) {
		t.Fatal("classic script was reported as a module")
	}
}

func TestModulePageShowsStatusWarning(t *testing.T) {
	u := mustParse(t, "https://example.com/")
	page := &Page{
		URL: u, Base: u, Status: 200, Title: "FileGator", HasModules: true,
		Root: ParseHTML(`<html><body><p>Loading...</p></body></html>`),
	}
	b := NewBrowser(40, 10)
	withQuietStdout(t, func() { b.finishLoad(loadResult{page: page, u: u}) })
	if b.status != moduleUnsupportedStatus {
		t.Fatalf("status = %q, want %q", b.status, moduleUnsupportedStatus)
	}
	if !strings.Contains(b.status, "reader mode") {
		t.Fatalf("status has no reader-mode hint: %q", b.status)
	}
}

func TestFooterStatusColors(t *testing.T) {
	b := NewBrowser(80, 10)
	b.color = true
	b.layout = &Layout{Height: 10}
	cases := []struct {
		status string
		code   string
	}{
		{moduleUnsupportedStatus, "38;2;154;91;0"},
		{layoutTruncatedStatus, "38;2;154;91;0"},
		{"HTTP 404 Not Found", "38;2;154;91;0"},
		{"Error: boom", "38;2;179;29;29"},
		{"Copied 3 chars", "38;2;23;107;53"},
	}
	for _, tc := range cases {
		b.status = tc.status
		drawQuiet(b)
		line := b.prev[len(b.prev)-1]
		if !strings.Contains(line, tc.code) {
			t.Errorf("status %q footer = %q, want color %s", tc.status, line, tc.code)
		}
	}
}

func TestConsoleResetsOnSuccessfulNavigation(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `console.log("fresh");`)
	b := NewBrowser(40, 10)
	b.jsLog = []string{"[error] old"}
	b.jsErr = true
	b.consoleText = "draft"
	b.consoleCursor = 5
	b.consoleScroll = 3
	b.storageScroll = 2
	b.consolePane = consoleStorage
	b.consoleEdit = true
	withQuietStdout(t, func() {
		b.finishLoad(loadResult{page: page, u: page.URL})
	})
	if len(b.jsLog) != 1 || !strings.Contains(b.jsLog[0], "fresh") {
		t.Fatalf("console log = %v, want only the new page log", b.jsLog)
	}
	if b.jsErr {
		t.Fatal("JS error state survived navigation")
	}
	if b.consoleText != "" || b.consoleCursor != 0 || b.consoleScroll != 0 ||
		b.storageScroll != 0 || b.consolePane != consoleLogs || b.consoleEdit {
		t.Fatalf("console input state survived navigation: %q cursor=%d scroll=%d storage=%d pane=%d edit=%v",
			b.consoleText, b.consoleCursor, b.consoleScroll, b.storageScroll, b.consolePane, b.consoleEdit)
	}
}

func TestConsoleKeepsLogOnFailedNavigation(t *testing.T) {
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(40, 10)
	b.jsLog = []string{"[error] old"}
	withQuietStdout(t, func() {
		b.finishLoad(loadResult{err: errors.New("boom"), u: u})
	})
	if len(b.jsLog) != 1 || b.jsLog[0] != "[error] old" {
		t.Fatalf("failed navigation cleared the log: %v", b.jsLog)
	}
}
