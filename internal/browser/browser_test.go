package browser

import (
	"context"
	"errors"
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
	for _, want := range []string{"-dump", "-width", "-graphics", "-no-js", "-ignore-blocks",
		"-bookmarks", "-home", "-js-memory", "-text-scale", "-color", "-no-color", "-ua",
		"-search", "-cpuprofile", "-version", "startup"} {
		if !strings.Contains(text, want) {
			t.Errorf("config page missing %q", want)
		}
	}
	values := map[string]string{}
	for _, n := range page.Root.FindAll(func(n *Node) bool { return n.Tag == "input" }) {
		values[n.Attr("name")] = n.Attr("value")
	}
	want := map[string]string{
		"no-js": "on", "home": "https://example.com/home", "js-memory": "64",
		"text-scale": "off", "ua": "TestUA", "search": "https://search.example/?q=%s",
	}
	for k, v := range want {
		if values[k] != v {
			t.Errorf("input %s = %q, want %q", k, values[k], v)
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
		<input name="q" value="">
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
	if !strings.Contains(formLabel(q), "▏") {
		t.Fatalf("focused label = %q", formLabel(q))
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
	b := NewBrowser(80, 10)
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
	b := NewBrowser(80, 10)
	b.usage = procUsage{rss: 18 << 20}
	if strings.Contains(b.footerRight(80), "JS: Error") {
		t.Fatal("error hint shown without a JS error")
	}
	b.logJS("error", "TypeError: boom")
	if !b.jsErr {
		t.Fatal("JS error was not recorded")
	}
	right := b.footerRight(80)
	if !strings.Contains(right, jsErrorHint) || !strings.Contains(right, "MEM 18 MB") {
		t.Fatalf("footer right = %q", right)
	}
	if strings.Index(right, "JS: Error") > strings.Index(right, "MEM") {
		t.Fatalf("error hint is not before memory: %q", right)
	}
	if narrow := b.footerRight(24); !strings.HasPrefix(narrow, jsErrorShort) {
		t.Fatalf("narrow footer = %q", narrow)
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
	lines := b.requestLines()
	if len(lines) != 1 || !strings.Contains(lines[0], "200") ||
		!strings.Contains(lines[0], "2.0K") || !strings.Contains(lines[0], "css") ||
		!strings.Contains(lines[0], "https://example.com/a.css") {
		t.Fatalf("request lines = %q", lines)
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

func TestConsoleClickSwitchesPane(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	l := newConsoleLayout(20)
	if l.sepY < 0 {
		t.Fatal("expected a requests pane")
	}
	// Row 1 (1-based Y=2) is inside the JS log pane.
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: 2, Press: true})
	if b.consolePane != 0 {
		t.Fatalf("console pane after log click = %d, want logs", b.consolePane)
	}
	// The separator row selects the requests pane.
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: l.sepY + 1, Press: true})
	if b.consolePane != 1 {
		t.Fatalf("console pane after heading click = %d, want requests", b.consolePane)
	}
	// A body row in the requests pane keeps it selected.
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: l.sepY + 2, Press: true})
	if b.consolePane != 1 {
		t.Fatalf("console pane after request click = %d, want requests", b.consolePane)
	}
	// Clicking the top heading returns to the logs pane.
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: 1, Press: true})
	if b.consolePane != 0 {
		t.Fatalf("console pane after heading click = %d, want logs", b.consolePane)
	}
}

func TestConsoleActivePaneHeadingHighlight(t *testing.T) {
	b := NewBrowser(60, 20)
	b.color = true
	b.consoleOpen = true
	blue := "48;2;47;111;191"
	l := newConsoleLayout(20)

	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	if !strings.Contains(b.prev[0], blue) {
		t.Fatalf("logs heading not highlighted while active: %q", b.prev[0])
	}
	if strings.Contains(b.prev[l.sepY], blue) {
		t.Fatalf("requests heading highlighted while inactive: %q", b.prev[l.sepY])
	}

	b.consolePane = 1
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	if strings.Contains(b.prev[0], blue) {
		t.Fatalf("logs heading still highlighted while inactive: %q", b.prev[0])
	}
	if !strings.Contains(b.prev[l.sepY], blue) {
		t.Fatalf("requests heading not highlighted while active: %q", b.prev[l.sepY])
	}
}

func TestConsoleMouseWheelScrollsHoveredPane(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	l := newConsoleLayout(20)
	b.HandleMouse(MouseEvent{X: 5, Y: l.sepY + 2, Wheel: -1})
	if b.reqScroll != 3 || b.consoleScroll != 0 {
		t.Fatalf("wheel scroll = logs %d, requests %d", b.consoleScroll, b.reqScroll)
	}
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: 2, Press: true})
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

func TestDialogRendering(t *testing.T) {
	b := NewBrowser(60, 15)
	b.dialog = &jsDialog{kind: "confirm", message: "Continue with this action?"}
	drawQuiet(b)
	text := strings.Join(b.prev, "\n")
	if !strings.Contains(text, "JavaScript confirm") || !strings.Contains(text, "Continue with this action?") {
		t.Fatalf("dialog not rendered: %q", text)
	}
}
