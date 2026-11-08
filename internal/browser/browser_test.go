package browser

import (
	"context"
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

	if len(b.headerHits) != 4 {
		t.Fatalf("header hits = %+v", b.headerHits)
	}
	want := []headerHit{
		{headerBack, 0, 0, 8, 1},
		{headerReload, 8, 0, 10, 1},
		{headerHelp, 52, 0, 8, 1},
		{headerURL, 0, 1, 60, 1},
	}
	for i := range want {
		if b.headerHits[i] != want[i] {
			t.Errorf("hit %d = %+v, want %+v", i, b.headerHits[i], want[i])
		}
	}
	if b.headerHits[2].x+b.headerHits[2].w != b.width {
		t.Error("help is not at the far right")
	}
	if top, _ := b.viewport(); top != 2 {
		t.Errorf("viewport top = %d, want 2 for a two-row bar", top)
	}
}

func TestHeaderNarrow(t *testing.T) {
	b := NewBrowser(20, 10)
	drawQuiet(b)
	if len(b.headerHits) != 2 || b.headerHits[0].kind != headerHelp || b.headerHits[1].kind != headerURL {
		t.Fatalf("narrow header hits = %+v", b.headerHits)
	}
	if b.headerHits[0].x+b.headerHits[0].w != b.width {
		t.Error("narrow help is not at the far right")
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
	if !strings.HasSuffix(line, "50%") {
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
	for _, unwanted := range []string{"CPU", "Tab:", "q: quit", "h/l"} {
		if strings.Contains(line, unwanted) {
			t.Errorf("footer still contains %q: %q", unwanted, line)
		}
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
	if !strings.HasSuffix(line, "0%") {
		t.Fatalf("narrow footer = %q", line)
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
	if !strings.Contains(b.prev[0], "Back") {
		t.Fatalf("header line = %q", b.prev[0])
	}

	// The chrome is rewritten even when nothing changed.
	b.prev[0] = "stale header"
	drawQuiet(b)
	if b.prev[0] == "stale header" || !strings.Contains(b.prev[0], "Back") {
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

	targetCh := make(chan string, 1)
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		select {
		case targetCh <- target:
		default:
		}
		return &Page{}, nil
	}
	b.HandleMouse(MouseEvent{Button: 0, X: 58, Y: 1, Press: true})
	select {
	case target := <-targetCh:
		if target != "about:help" {
			t.Fatalf("help target = %q", target)
		}
	case <-time.After(time.Second):
		t.Fatal("help button did not navigate")
	}
}
