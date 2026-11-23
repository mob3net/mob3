package browser

import (
	"browser/internal/engine"
	"browser/internal/host"
	"browser/internal/protocol"
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func init() {
	SetDefaultOutput(func(p []byte) (int, error) { return os.Stdout.Write(p) })
	SetHostFactory(func() protocol.Host { return host.NewSession() })
}

var testSGR = regexp.MustCompile("\x1b\\[[0-9;]*m")

// plainLine removes SGR sequences from a rendered row.
func plainLine(s string) string {
	return testSGR.ReplaceAllString(s, "")
}

// newTestHTTPClient wraps an injected client for the loader.
func newTestHTTPClient(c *http.Client) *host.Client { return host.NewClientFromHTTP(c) }

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

func TestGridTracksAndPlacement(t *testing.T) {
	tracks := parseGridTracks("1fr 2fr", 90, 16)
	if len(tracks) != 2 || tracks[0].Flex != 1 || tracks[1].Flex != 2 {
		t.Fatalf("tracks = %+v", tracks)
	}
	widths := resolveGridTracks(tracks, 90, 0, nil, nil)
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
	if !mediaFeatureMatches("width>=960px", 1280, false) {
		t.Fatal("width>=960px did not match 1280")
	}
	if mediaFeatureMatches("width>=960px", 800, false) {
		t.Fatal("width>=960px matched 800")
	}
	if !mediaFeatureMatches("width<=959px", 800, false) {
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
	w, h := imageSize(n, st, 100, 40)
	if w != 40 || h != 13 {
		t.Fatalf("capped image = %dx%d, want 40x13", w, h)
	}

	tall := image.NewNRGBA(image.Rect(0, 0, 3000, 3000))
	n.Img = tall
	w, h = imageSize(n, st, 100, 40)
	if h != maxImageRows || w != 32 {
		t.Fatalf("tall capped image = %dx%d, want 32x%d", w, h, maxImageRows)
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

func TestPreventedLinkClickWithErrorFallsBack(t *testing.T) {
	page := newJSPage(`<html><body>
<a id="a" href="/next" onclick="event.preventDefault(); throw new Error('boom');">next</a>
</body></html>`, ``)
	b := NewBrowser(60, 12)
	b.Page = page
	b.URL = page.URL
	b.setLoader(NewLoader())
	b.LoadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		u := mustParse(t, target)
		return &Page{URL: u, Base: u, Root: ParseHTML(`<html><body>next</body></html>`)}, nil
	}
	withQuietStdout(t, func() { b.startJS(page) })
	b.rebuild()
	b.Focus = 0
	withQuietStdout(t, b.followFocused)
	if !b.Loading {
		t.Fatal("a throwing click handler swallowed the link navigation")
	}
	select {
	case res := <-b.LoadCh:
		b.finishLoad(res)
	case <-time.After(2 * time.Second):
		t.Fatal("fallback navigation did not finish")
	}
}

func TestPreventedLinkClickWithoutErrorStays(t *testing.T) {
	page := newJSPage(`<html><body>
<a id="a" href="/next" onclick="event.preventDefault();">next</a>
</body></html>`, ``)
	b := NewBrowser(60, 12)
	b.Page = page
	b.URL = page.URL
	withQuietStdout(t, func() { b.startJS(page) })
	b.rebuild()
	b.Focus = 0
	b.followFocused()
	if b.Loading {
		t.Fatal("a healthy preventDefault handler should suppress navigation")
	}
}

func TestKeyboardLinkRunsClickHandler(t *testing.T) {
	page := newJSPage(`<html><body>
<p><a id="a" href="#" onclick="document.body.setAttribute('data-clicked','yes'); return false;">show an alert</a></p>
</body></html>`, ``)
	b := NewBrowser(60, 12)
	b.Page = page
	b.URL = page.URL
	withQuietStdout(t, func() { b.startJS(page) })
	b.rebuild()
	b.Scroll = 1
	b.Focus = 0
	b.followFocused()
	body := page.Root.FindFirst("body")
	if body == nil || body.Attr("data-clicked") != "yes" {
		t.Fatal("keyboard follow did not run the link click handler")
	}
	if b.EntryID != 0 {
		t.Fatalf("prevented click navigated: entry=%d", b.EntryID)
	}
}

func TestNavigateSearchesQuery(t *testing.T) {
	b := NewBrowser(40, 10)
	b.loader.setSearchURL("https://www.bing.com/search?q=%s")
	ch := make(chan string, 1)
	b.LoadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
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
	b.URL = mustParse(t, "https://example.com/")
	called := false
	b.LoadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		called = true
		return &Page{}, nil
	}
	b.Navigate("file:///etc/passwd", false)
	if called {
		t.Fatal("remote page navigation to file:// was allowed")
	}
	if !strings.Contains(b.status, "local files are not supported") {
		t.Fatalf("status = %q", b.status)
	}
}

func TestNavigateAsyncAndStop(t *testing.T) {
	b := NewBrowser(40, 10)
	started := make(chan struct{})
	b.LoadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
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
	if !b.Loading {
		t.Fatal("browser is not marked as loading")
	}
	b.HandleKey("esc")
	if b.Loading {
		t.Fatal("Esc did not stop loading")
	}
	if b.status != "Stopped loading" {
		t.Fatalf("status = %q", b.status)
	}
	// A late result from the canceled load must be ignored.
	select {
	case res := <-b.LoadCh:
		b.finishLoad(res)
	case <-time.After(200 * time.Millisecond):
	}
	if b.Page != nil {
		t.Fatal("canceled load was applied")
	}
}

func TestReloadButtonBecomesStopWhileLoading(t *testing.T) {
	b := NewBrowser(60, 10)
	started := make(chan struct{})
	b.LoadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
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
	if b.Loading {
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
	var res engine.LoadResult
	select {
	case res = <-b.LoadCh:
	case <-time.After(2 * time.Second):
		t.Fatal("load did not finish")
	}
	b.finishLoad(res)
	if b.Page == nil || b.Page.Title != "Loaded" {
		t.Fatalf("page = %+v", b.Page)
	}
}

func TestLoadingProgress(t *testing.T) {
	b := NewBrowser(80, 10)
	if b.progressActive() {
		t.Fatal("idle browser reports active progress")
	}
	b.Loading = true
	b.startProgress()
	if !b.progressActive() || b.Progress != 0.05 {
		t.Fatalf("start = %v active=%v", b.Progress, b.progressActive())
	}
	for i := 0; i < 200; i++ {
		if !b.tickProgress() {
			t.Fatal("loading tick did not repaint")
		}
	}
	if b.Progress <= 0.5 || b.Progress > 0.9 {
		t.Fatalf("progress after loading ticks = %v", b.Progress)
	}

	b.Loading = false
	b.finishProgress()
	if b.Progress != 1 {
		t.Fatalf("finish progress = %v", b.Progress)
	}
	for i := 0; i < progressLingerTicks-1; i++ {
		if b.tickProgress() {
			t.Fatal("lingering progress repainted before it cleared")
		}
	}
	if !b.progressActive() {
		t.Fatal("progress cleared too early")
	}
	if !b.tickProgress() || b.Progress != 0 || b.progressActive() {
		t.Fatalf("progress did not clear: %v active=%v", b.Progress, b.progressActive())
	}

	b.Loading = true
	b.startProgress()
	b.cancelLoadNow()
	if b.progressActive() || b.Progress != 0 {
		t.Fatalf("cancel did not clear progress: %v active=%v", b.Progress, b.progressActive())
	}
}

func TestTryAnywayRetriesBlockedURL(t *testing.T) {
	b := NewBrowser(40, 10)
	b.URL = mustParse(t, "https://example.com/challenge")
	var got string
	b.LoadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		got = target
		u := mustParse(t, target)
		return &Page{URL: u, Base: u, Root: ParseHTML("<html><body><p>challenge</p></body></html>"), Title: "challenge"}, nil
	}
	b.Navigate("about:retry", true)
	select {
	case res := <-b.LoadCh:
		b.finishLoad(res)
	case <-time.After(2 * time.Second):
		t.Fatal("retry did not start a load")
	}
	if got != "https://example.com/challenge" {
		t.Fatalf("retry target = %q", got)
	}
	if b.EntryID != 0 {
		t.Fatalf("retry pushed history: entry=%d", b.EntryID)
	}
}

func TestTryAnywayWithoutURL(t *testing.T) {
	b := NewBrowser(40, 10)
	b.URL = nil
	b.Navigate("about:retry", true)
	if b.status != "Nothing to retry" {
		t.Fatalf("status = %q", b.status)
	}
	select {
	case <-b.LoadCh:
		t.Fatal("retry without a URL started a load")
	default:
	}
}

func TestJavaScriptKeyTogglesAndReloads(t *testing.T) {
	b := NewBrowser(80, 10)
	u := mustParse(t, "https://example.com/")
	b.Page = &Page{URL: u, Base: u, Root: ParseHTML("<html><body>scripted</body></html>")}
	b.URL = u
	b.LastMethod = "GET"
	b.rebuild()

	requested := make(chan string, 1)
	b.LoadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		requested <- target
		p := &Page{URL: u, Base: u, Root: ParseHTML("<html><body>no scripts</body></html>")}
		return p, nil
	}
	b.HandleKey("J")
	if !b.NoJS {
		t.Fatal("J did not disable JavaScript")
	}
	if b.loader == nil || !b.loader.noScripts {
		t.Fatal("loader still fetches scripts after J")
	}
	select {
	case target := <-requested:
		if target != u.String() {
			t.Fatalf("reload target = %q", target)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("J did not reload the page")
	}
	var res engine.LoadResult
	select {
	case res = <-b.LoadCh:
	case <-time.After(2 * time.Second):
		t.Fatal("reload result did not arrive")
	}
	b.finishLoad(res)
	if b.NoJS != true {
		t.Fatal("applying the reload enabled JavaScript again")
	}
	if b.status != "JavaScript off — press J to re-enable" {
		t.Fatalf("status = %q", b.status)
	}

	// Pressing it again re-enables scripting for the session.
	b.pendingStatus = ""
	b.HandleKey("J")
	if b.NoJS {
		t.Fatal("second J did not enable JavaScript")
	}
	if b.loader.noScripts {
		t.Fatal("loader still skips scripts after the second J")
	}
}

func TestHoverRebuildIsDebounced(t *testing.T) {
	doc := ParseHTML(`<html><body>
<div class="menu"><a href="#m">Menu</a>
<div class="sub"><a href="#x">Item</a></div></div>
</body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 20)
	b.keys = make(chan string)
	b.Page = &Page{URL: u, Base: u, Root: doc, Author: ParseStylesheet(`.menu .sub{display:none}.menu:hover .sub{display:block}`)}
	b.URL = u
	b.rebuild()
	var menu *Frag
	for i := range b.Layout.Frags {
		if b.Layout.Frags[i].Kind == fragText && strings.TrimSpace(b.Layout.Frags[i].Text) == "Menu" {
			menu = &b.Layout.Frags[i]
		}
	}
	if menu == nil {
		t.Fatal("menu link missing")
	}
	layout := b.Layout
	top, _ := b.viewport()
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, Motion: true, X: menu.X + 1, Y: menu.Y - b.Scroll + top + 1})
	if b.Layout != layout {
		t.Fatal("hover rebuilt the page immediately instead of debouncing")
	}
	if !b.hoverRebuildPending {
		t.Fatal("hover did not schedule a rebuild")
	}
	b.hoverRebuildAt = time.Now().Add(-time.Millisecond)
	if !b.runJSTimers() {
		t.Fatal("deferred hover rebuild did not run")
	}
	found := false
	for i := range b.Layout.Frags {
		if b.Layout.Frags[i].Kind == fragText && strings.TrimSpace(b.Layout.Frags[i].Text) == "Item" {
			found = true
		}
	}
	if !found {
		t.Fatal("deferred hover rebuild did not open the menu")
	}
}

func TestHoverOpensJSMenu(t *testing.T) {
	page := newJSPage(`<html><body>
<div id="menu">Menu<div id="sub" style="display:none">Item</div></div>
</body></html>`, `document.getElementById("menu").addEventListener("mouseover", function () {
  document.getElementById("sub").style.display = "block";
});`)
	b := NewBrowser(60, 20)
	b.Page = page
	b.URL = page.URL
	withQuietStdout(t, func() { b.startJS(page) })
	b.rebuild()
	findText := func(s string) *Frag {
		for i := range b.Layout.Frags {
			if b.Layout.Frags[i].Kind == fragText && strings.TrimSpace(b.Layout.Frags[i].Text) == s {
				return &b.Layout.Frags[i]
			}
		}
		return nil
	}
	menu := findText("Menu")
	if menu == nil {
		t.Fatal("menu text missing")
	}
	top, _ := b.viewport()
	withQuietStdout(t, func() {
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, Motion: true, X: menu.X + 1, Y: menu.Y - b.Scroll + top + 1})
	})
	if findText("Item") == nil {
		t.Fatal("mouseover handler did not open the menu")
	}
}

func TestHoverMenuSurvivesPaddingGaps(t *testing.T) {
	doc := ParseHTML(`<html><body>
<div class="menu"><a href="#m">Menu</a>
<div class="sub"><a href="#x">Item</a></div></div>
</body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 20)
	b.Page = &Page{URL: u, Base: u, Root: doc, Author: ParseStylesheet(
		`.menu .sub{display:none;position:absolute}.menu:hover .sub{display:block}.sub a{display:block;padding:1em}`)}
	b.URL = u
	b.rebuild()
	textFrag := func(s string) *Frag {
		for i := range b.Layout.Frags {
			if b.Layout.Frags[i].Kind == fragText && strings.TrimSpace(b.Layout.Frags[i].Text) == s {
				return &b.Layout.Frags[i]
			}
		}
		return nil
	}
	menu := textFrag("Menu")
	if menu == nil {
		t.Fatal("menu link missing")
	}
	top, _ := b.viewport()
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, Motion: true, X: menu.X + 1, Y: menu.Y - b.Scroll + top + 1})
	item := textFrag("Item")
	if item == nil {
		t.Fatal("hover did not open the menu")
	}
	// Move into the link's top padding, where there is no text fragment.
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, Motion: true, X: item.X + 1, Y: item.Y - 1 - b.Scroll + top + 1})
	if textFrag("Item") == nil {
		t.Fatal("moving through the menu padding closed it")
	}
	for p := b.HoverNode; p != nil; p = p.Parent {
		if p.Attr("class") == "menu" {
			return
		}
	}
	t.Fatalf("pointer is not inside the menu hover chain: %v", b.HoverNode)
}

func TestHoverOpensCSSMenu(t *testing.T) {
	doc := ParseHTML(`<html><body>
<div class="menu"><a href="#m">Menu</a>
<div class="sub"><a href="#x">Item</a></div></div>
</body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 20)
	b.Page = &Page{URL: u, Base: u, Root: doc, Author: ParseStylesheet(`.menu .sub{display:none}.menu:hover .sub{display:block}`)}
	b.URL = u
	b.rebuild()
	findText := func(s string) *Frag {
		for i := range b.Layout.Frags {
			if b.Layout.Frags[i].Kind == fragText && strings.TrimSpace(b.Layout.Frags[i].Text) == s {
				return &b.Layout.Frags[i]
			}
		}
		return nil
	}
	menu := findText("Menu")
	if menu == nil {
		t.Fatal("menu link missing")
	}
	top, _ := b.viewport()
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, Motion: true, X: menu.X + 1, Y: menu.Y - b.Scroll + top + 1})
	if findText("Item") == nil {
		t.Fatal("hover did not open the CSS menu")
	}
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, Motion: true, X: 1, Y: 1})
	if findText("Item") != nil {
		t.Fatal("leaving the menu did not close it")
	}
}

func TestMouseHoverShowsLinkInFooter(t *testing.T) {
	root := ParseHTML(`<html><body><p><a href="/target">target</a></p></body></html>`)
	u := mustParse(t, "https://example.com/page")
	b := NewBrowser(40, 10)
	b.Page = &Page{URL: u, Base: u, Root: root}
	b.rebuild()
	if len(b.Layout.Links) == 0 {
		t.Fatal("no links in layout")
	}
	r := b.Layout.Links[0].Rects[0]
	top, _ := b.viewport()
	b.HandleMouse(MouseEvent{X: r.X + 1, Y: top + r.Y + 1, Motion: true})
	if b.Hover != 0 {
		t.Fatalf("hover = %d, want 0", b.Hover)
	}
	if got, want := b.footerLeft(), " → https://example.com/target"; got != want {
		t.Fatalf("footer = %q, want %q", got, want)
	}
	b.HandleMouse(MouseEvent{X: b.Width - 1, Y: top + 1, Motion: true})
	if b.Hover != -1 {
		t.Fatalf("hover = %d after leaving link", b.Hover)
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
	b.Page = page
	keys := make(chan string, 2)
	keys <- "enter"
	b.keys = keys
	withQuietStdout(t, func() {
		b.startJS(page)
		b.JS.DispatchClick(link)
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

func TestNextJSTimerDelayTracksPendingTimers(t *testing.T) {
	b := NewBrowser(60, 15)
	if _, ok := b.nextJSTimerDelay(); ok {
		t.Fatal("reported a timer without a JS runtime")
	}
	page := newJSPage(`<html><body><p>idle</p></body></html>`, `
		setTimeout(function () {}, 5000);
	`)
	b.Page = page
	withQuietStdout(t, func() { b.startJS(page) })
	d, ok := b.nextJSTimerDelay()
	if !ok {
		t.Fatal("pending timer was not reported")
	}
	if d <= 0 || d > 5*time.Second {
		t.Fatalf("delay = %v, want about 5s", d)
	}
	b.consoleOpen = true
	if _, ok := b.nextJSTimerDelay(); ok {
		t.Fatal("paused timers must not wake the event loop")
	}
}

func TestPageContentCannotInjectTerminalControls(t *testing.T) {
	u := mustParse(t, "https://example.com/")
	root := ParseHTML("<html><head><title>evil\x1b]52;c;x\x07</title></head><body><p>hi\x1b[2Jthere</p></body></html>")
	page := &Page{URL: u, Base: u, Root: root, Title: "evil\x1b]52;c;x\x07", RawHTML: "<html>\x1b]0;pwn\x07</html>"}
	b := NewBrowser(60, 20)
	b.Page = page
	b.URL = u
	b.Layout = BuildLayout(page.Root, nil, 60)
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

func TestTimerRedrawsAreCoalesced(t *testing.T) {
	page := newJSPage(`<html><body><p id="out">before</p></body></html>`, `
		setInterval(function () {
			document.getElementById("out").textContent = String(Date.now());
		}, 10);
	`)
	b := NewBrowser(60, 15)
	b.Page = page
	withQuietStdout(t, func() { b.startJS(page) })
	b.rebuild()
	first := b.Layout
	b.lastBuild = 100 * time.Millisecond
	b.lastRebuild = time.Now()
	time.Sleep(25 * time.Millisecond)
	if b.runJSTimers() {
		t.Fatal("timer rebuilt inside the coalescing interval")
	}
	if b.Layout != first {
		t.Fatal("layout changed inside the coalescing interval")
	}
	if !b.JS.Dirty() {
		t.Fatal("dirty flag was cleared before the deferred rebuild")
	}
	b.lastRebuild = time.Now().Add(-time.Second)
	if !b.runJSTimers() {
		t.Fatal("deferred timer rebuild did not run after the interval")
	}
	if b.JS.Dirty() {
		t.Fatal("dirty flag was not cleared after rebuilding")
	}
	if b.Layout == first {
		t.Fatal("layout was not rebuilt after the interval")
	}
}

func TestDownloadLinkOpensPopupWithoutFetching(t *testing.T) {
	u := mustParse(t, "https://filesamples.com/formats/zip")
	doc := ParseHTML(`<a href="/files/sample.zip" download>Download</a>`)
	b := NewBrowser(60, 15)
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.URL = u
	b.Layout = BuildLayout(doc, nil, 60)
	if len(b.Layout.Links) != 1 {
		t.Fatalf("links = %d", len(b.Layout.Links))
	}
	b.Focus = 0
	b.followFocused()
	if b.download == nil {
		t.Fatal("non-PDF download did not open the curl popup")
	}
	want := "curl -L -o 'sample.zip' -- 'https://filesamples.com/files/sample.zip'"
	if b.download.command != want {
		t.Fatalf("command = %q, want %q", b.download.command, want)
	}
	// No request is made for a non-PDF download link.
	if b.Loading {
		t.Fatal("non-PDF download link started a load")
	}
	if b.URL.String() != "https://filesamples.com/formats/zip" {
		t.Fatalf("non-PDF download link navigated to %s", b.URL)
	}
}

func TestDownloadPDFLinkNavigatesToViewer(t *testing.T) {
	u := mustParse(t, "https://samplepdf.org/")
	doc := ParseHTML(`<a href="/files/sample.pdf" download="chosen.pdf">Download</a>`)
	b := NewBrowser(60, 15)
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.URL = u
	b.Layout = BuildLayout(doc, nil, 60)
	b.LoadFn = func(_ context.Context, _, target string, _ []byte, _ string) (*Page, error) {
		tu := mustParse(t, target)
		return &Page{URL: tu, Base: tu, Root: ParseHTML("<html><body>pdf</body></html>")}, nil
	}
	b.Focus = 0
	b.followFocused()
	// A PDF download is fetched so the viewer can show it.
	if b.download != nil {
		t.Fatal("PDF download opened the curl popup without fetching")
	}
	if !b.Loading {
		t.Fatal("PDF download did not start a load")
	}
	var res engine.LoadResult
	select {
	case res = <-b.LoadCh:
	case <-time.After(2 * time.Second):
		t.Fatal("PDF download did not finish")
	}
	if res.DownloadName != "chosen.pdf" {
		t.Fatalf("download name carried = %q", res.DownloadName)
	}
	b.finishLoad(res)
	if b.download != nil {
		t.Fatal("PDF download fell back to the curl popup")
	}
	if b.URL.String() != "https://samplepdf.org/files/sample.pdf" {
		t.Fatalf("PDF download navigated to %s", b.URL)
	}
}

func TestDownloadPDFDetection(t *testing.T) {
	cases := []struct {
		href string
		attr string
		pdf  bool
	}{
		{"/files/sample.pdf", "", true},
		{"/files/SAMPLE.PDF", "", true},
		{"/files/sample.pdf?dl=1", "", true},
		{"/download?id=7", "scan.pdf", true},
		{"/files/sample.zip", "", false},
		{"/download?id=7", "archive.zip", false},
		{"/download?id=7", "", false},
	}
	for _, c := range cases {
		doc := ParseHTML(`<a href="` + c.href + `" download="` + c.attr + `">Download</a>`)
		u := mustParse(t, "https://example.com/page")
		b := NewBrowser(60, 15)
		b.Page = &Page{URL: u, Base: u, Root: doc}
		b.URL = u
		b.Layout = BuildLayout(doc, nil, 60)
		b.Focus = 0
		b.followFocused()
		got := b.download == nil
		if got != c.pdf {
			t.Fatalf("href=%q attr=%q: fetched=%v, want %v", c.href, c.attr, got, c.pdf)
		}
	}
}

func TestDownloadAttributeNameWins(t *testing.T) {
	u := mustParse(t, "https://example.com/files/")
	doc := ParseHTML(`<a href="x.bin" download="chosen.zip">Download</a>`)
	b := NewBrowser(60, 15)
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.URL = u
	b.Layout = BuildLayout(doc, nil, 60)
	b.Focus = 0
	b.followFocused()
	if b.download == nil {
		t.Fatal("download link did not open the popup")
	}
	want := "curl -L -o 'chosen.zip' -- 'https://example.com/files/x.bin'"
	if b.download.command != want {
		t.Fatalf("command = %q, want %q", b.download.command, want)
	}
}

func TestDownloadCommandNeutralizesOptionsAndLocalFiles(t *testing.T) {
	b := NewBrowser(60, 15)
	b.showDownload("-o/tmp/x", "-K/tmp/cfg")
	if b.download == nil {
		t.Fatal("dash-prefixed target did not open the popup")
	}
	want := "curl -L -o './-K_tmp_cfg' -- '-o/tmp/x'"
	if b.download.command != want {
		t.Fatalf("command = %q, want %q", b.download.command, want)
	}

	b.showDownload("file:///etc/passwd", "passwd")
	if b.download != nil {
		t.Fatal("local file download was accepted")
	}
	if b.notice == nil || !strings.Contains(b.notice.message, "Local file downloads") {
		t.Fatalf("notice = %+v", b.notice)
	}
}

func TestDownloadPopupCopiesWithY(t *testing.T) {
	u := mustParse(t, "https://example.com/file.zip")
	b := NewBrowser(60, 15)
	b.Page = &Page{URL: u, Base: u, Root: ParseHTML(`<p>x</p>`)}
	b.URL = u
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
	b.LoadSeq = 1
	b.finishLoad(engine.LoadResult{Seq: 1, Page: page, URL: u, EntryID: b.nextHistoryID(), HistoryPush: true, Method: http.MethodGet})
	if b.download == nil {
		t.Fatal("non-HTML response did not offer the curl popup")
	}
	if b.Page != nil {
		t.Fatal("non-HTML response was applied as a page")
	}
	if !strings.Contains(b.download.command, "archive.zip") || !strings.Contains(b.download.command, u.String()) {
		t.Fatalf("command = %q", b.download.command)
	}
	if b.EntryID != 0 {
		t.Fatalf("download pushed history: entry=%d", b.EntryID)
	}
}

func TestDownloadCommandIsNotDrawnSelected(t *testing.T) {
	u := mustParse(t, "https://example.com/file.zip")
	b := NewBrowser(60, 15)
	b.Page = &Page{URL: u, Base: u, Root: ParseHTML(`<p>x</p>`)}
	b.URL = u
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

func TestLayoutTextOverlapDetection(t *testing.T) {
	big := &Style{FontSize: 32}
	ly := &Layout{Frags: []Frag{
		{Kind: fragText, Text: "one", X: 0, Y: 0, W: 5, H: 1},
		{Kind: fragText, Text: "two", X: 5, Y: 0, W: 5, H: 1},
	}}
	if layoutTextOverlaps(ly) {
		t.Fatal("adjacent text runs reported as overlapping")
	}
	// Ordinary normal-size overlays do not disable scaled text.
	ly.Frags = append(ly.Frags, Frag{Kind: fragText, Text: "bad", X: 3, Y: 0, W: 5, H: 1})
	if layoutTextOverlaps(ly) {
		t.Fatal("normal-size overlay disabled scaled text")
	}
	// A scaled run sharing cells with another run does.
	ly.Frags = append(ly.Frags, Frag{Kind: fragText, Text: "BIG", X: 2, Y: 0, W: 6, H: 1, Style: big})
	if !layoutTextOverlaps(ly) {
		t.Fatal("overlapping scaled text was not detected")
	}
}

func TestInteractiveLayoutReservesScrollbarColumn(t *testing.T) {
	u := mustParse(t, "https://example.com/")
	doc := ParseHTML(`<html><body><p>text that wraps normally</p></body></html>`)
	b := NewBrowser(20, 6)
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.URL = u
	b.rebuild()
	if b.Layout.Width != 19 {
		t.Fatalf("interactive layout width = %d, want 19", b.Layout.Width)
	}
	b.NoDraw = true
	b.rebuild()
	if b.Layout.Width != 20 {
		t.Fatalf("headless layout width = %d, want 20", b.Layout.Width)
	}
}

func TestBrowserFixedLayoutUsesViewportHeight(t *testing.T) {
	u := mustParse(t, "https://example.com/")
	doc := ParseHTML(`<div>top</div><div style="position:fixed;bottom:0;right:0">bar</div>`)
	b := NewBrowser(30, 10)
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.URL = u
	b.rebuild()
	bar := textFrag(b.Layout, "bar")
	if bar == nil {
		t.Fatal("fixed fragment not laid out")
	}
	wantY := b.viewportHeight() - 1
	wantX := b.Layout.Width - 3
	if bar.Y != wantY || bar.X != wantX {
		t.Fatalf("fixed box at (%d,%d), want (%d,%d)", bar.X, bar.Y, wantX, wantY)
	}
}

func TestReaderModeModalLinkWarnsWithoutScrolling(t *testing.T) {
	for _, href := range []string{"#", "#missing"} {
		u := mustParse(t, "https://example.com/article")
		doc := ParseHTML(`<p>hello</p><p><a href="` + href + `">open modal</a></p>`)
		b := NewBrowser(40, 10)
		b.Page = &Page{URL: u, Base: u, Root: doc}
		b.URL = u
		b.Reader = true
		b.ReaderRoot = doc
		b.rebuild()
		if len(b.Layout.Links) == 0 {
			t.Fatal("link not laid out")
		}
		b.Layout.Height = 100
		b.Scroll = 3
		b.Focus = 0
		b.followFocused()
		if b.status != readerLinkUnavailable {
			t.Fatalf("href %q status = %q, want %q", href, b.status, readerLinkUnavailable)
		}
		if b.Scroll != 3 {
			t.Fatalf("href %q scrolled to %d", href, b.Scroll)
		}
		if b.URL.String() != "https://example.com/article" {
			t.Fatalf("href %q navigated to %s", href, b.URL)
		}
	}
}

func TestReaderModeAnchorLinkStillScrolls(t *testing.T) {
	u := mustParse(t, "https://example.com/article")
	doc := ParseHTML(`<p>hello</p><h2 id="target">target</h2><p><a href="#target">jump</a></p>`)
	b := NewBrowser(40, 10)
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.URL = u
	b.Reader = true
	b.ReaderRoot = doc
	b.rebuild()
	b.Layout.Height = 100
	b.Focus = 0
	b.followFocused()
	if b.status == readerLinkUnavailable {
		t.Fatal("existing anchor was rejected in reader mode")
	}
	if b.Scroll == 0 {
		t.Fatal("existing anchor did not scroll")
	}
}

func TestViewMenuTogglesFixedElements(t *testing.T) {
	b := NewBrowser(40, 10)
	b.HandleKey("X")
	if b.viewMenu == nil {
		t.Fatal("X did not open the view menu")
	}
	if b.HideFixed {
		t.Fatal("fixed elements should start shown, and opening the menu must not toggle")
	}
	b.HandleKey(" ")
	if !b.HideFixed || b.status != "Fixed elements hidden" {
		t.Fatalf("after Space: hideFixed=%v status=%q", b.HideFixed, b.status)
	}
	if b.viewMenu == nil {
		t.Fatal("toggling should keep the view menu open")
	}
	b.HandleKey("enter")
	if b.HideFixed || b.status != "Fixed elements shown" {
		t.Fatalf("after Enter: hideFixed=%v status=%q", b.HideFixed, b.status)
	}
	b.HandleKey("esc")
	if b.viewMenu != nil {
		t.Fatalf("esc left the view menu open: %+v", b.viewMenu)
	}
	if b.status != "" {
		t.Fatalf("status after closing = %q, want empty", b.status)
	}
}

func TestViewMenuTogglesStickyElements(t *testing.T) {
	b := NewBrowser(40, 10)
	b.HandleKey("X")
	b.HandleKey("down")
	b.HandleKey(" ")
	if !b.NoSticky || b.status != "Sticky elements disabled" {
		t.Fatalf("after Space: noSticky=%v status=%q", b.NoSticky, b.status)
	}
	b.HandleKey("enter")
	if b.NoSticky || b.status != "Sticky elements enabled" {
		t.Fatalf("after Enter: noSticky=%v status=%q", b.NoSticky, b.status)
	}
}

func TestViewMenuIsNotCtrlX(t *testing.T) {
	b := NewBrowser(40, 10)
	b.HandleKey("ctrl-x")
	if b.viewMenu != nil {
		t.Fatal("ctrl-x should not open the view menu")
	}
}

func TestViewMenuIgnoredWhileEditingField(t *testing.T) {
	b := NewBrowser(40, 10)
	b.Forms.InputFocus = &Node{}
	b.HandleKey("X")
	if b.viewMenu != nil {
		t.Fatal("X should stay literal text while a field has focus")
	}
}

func TestViewMenuClosesWithShiftX(t *testing.T) {
	b := NewBrowser(40, 10)
	b.HandleKey("X")
	b.HandleKey("X")
	if b.viewMenu != nil {
		t.Fatal("second X did not close the view menu")
	}
}

func TestViewMenuMouseTogglesRow(t *testing.T) {
	b := NewBrowser(40, 10)
	b.HandleKey("X")
	g := NewGrid(40, 10, RGB(0, 0, 0), RGB(255, 255, 255))
	b.paintViewMenu(g)
	if len(b.viewMenuHits) != 3 {
		t.Fatalf("view menu hits = %d, want 3", len(b.viewMenuHits))
	}
	r := b.viewMenuHits[0].rect
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: r.X + 1, Y: r.Y + 1, Press: true})
	if !b.HideFixed {
		t.Fatal("clicking the fixed-elements row did not hide them")
	}
	if b.viewMenu == nil {
		t.Fatal("clicking a row closed the view menu")
	}
	// A click on the dimmed backdrop closes the menu without toggling again.
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 1, Y: 9, Press: true})
	if b.viewMenu != nil {
		t.Fatal("clicking the backdrop did not close the view menu")
	}
	if !b.HideFixed {
		t.Fatal("backdrop click should not toggle the option")
	}
}

func TestNoColorLinkHintsUseReverse(t *testing.T) {
	doc := ParseHTML(`<html><body><p><a href="/one">Documentation</a></p></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(40, 10)
	b.color = false
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.URL = u
	b.rebuild()
	withQuietStdout(t, func() {
		b.openLinkHints(false)
		b.draw()
	})
	found := false
	for _, line := range b.prev {
		if strings.Contains(line, "38;2") || strings.Contains(line, "48;2") {
			t.Fatalf("no-color hint line kept colors: %q", line)
		}
		if strings.Contains(line, ";7") {
			found = true
		}
	}
	if !found {
		t.Fatal("no-color hint did not use reverse video")
	}
}

func TestRightClickCopiesLinkURL(t *testing.T) {
	doc := ParseHTML(`<p><a href="/story">Story link</a></p>`)
	u := mustParse(t, "https://example.com/news")
	b := NewBrowser(40, 10)
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.Layout = BuildLayout(doc, nil, b.Width)

	var frag *Frag
	for i := range b.Layout.Frags {
		if b.Layout.Frags[i].Kind == fragText && strings.Contains(b.Layout.Frags[i].Text, "Story") {
			frag = &b.Layout.Frags[i]
			break
		}
	}
	if frag == nil {
		t.Fatal("link text fragment not found")
	}
	var buf bytes.Buffer
	b.clipboardWriter = &buf
	top, _ := b.viewport()
	// Hover the link first: the footer would otherwise keep showing its URL
	// instead of the copy confirmation.
	b.HandleMouse(MouseEvent{X: frag.X + 1, Y: top + frag.Y + 1, Motion: true})
	if b.hoveredLink() == nil {
		t.Fatal("hover did not select the link")
	}
	b.HandleMouse(MouseEvent{Button: MouseButtonRight, X: frag.X + 1, Y: top + frag.Y + 1, Press: true})

	if got := b.footerLeft(); !strings.Contains(got, "Copied URL") {
		t.Fatalf("footer = %q, want the copy confirmation", got)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "\x1b]52;c;") {
		t.Fatalf("clipboard sequence = %q", out)
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(out, "\x1b]52;c;"), "\x07")
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || string(decoded) != "https://example.com/story" {
		t.Fatalf("clipboard text = %q, %v", decoded, err)
	}
	if b.status != "Copied URL" {
		t.Fatalf("status = %q", b.status)
	}
}

func TestRightClickAwayFromLinkDoesNothing(t *testing.T) {
	doc := ParseHTML(`<p>Plain text without a link</p>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(40, 10)
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.Layout = BuildLayout(doc, nil, b.Width)

	var buf bytes.Buffer
	b.clipboardWriter = &buf
	top, _ := b.viewport()
	b.HandleMouse(MouseEvent{Button: MouseButtonRight, X: 2, Y: top + 1, Press: true})

	if buf.Len() != 0 {
		t.Fatalf("right-click away from a link copied %q", buf.String())
	}
	if b.status != "" {
		t.Fatalf("status = %q, want empty", b.status)
	}
	if b.EntryID != 0 {
		t.Fatalf("right-click navigated: entry=%d", b.EntryID)
	}
}

func TestCopyStatusClearsOnMotionAndScroll(t *testing.T) {
	doc := ParseHTML(`<p><a href="/story">Story link</a></p>`)
	u := mustParse(t, "https://example.com/news")
	b := NewBrowser(40, 10)
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.Layout = BuildLayout(doc, nil, b.Width)
	var buf bytes.Buffer
	b.clipboardWriter = &buf

	var frag *Frag
	for i := range b.Layout.Frags {
		if b.Layout.Frags[i].Kind == fragText && strings.Contains(b.Layout.Frags[i].Text, "Story") {
			frag = &b.Layout.Frags[i]
			break
		}
	}
	if frag == nil {
		t.Fatal("link text fragment not found")
	}
	top, _ := b.viewport()
	copyAt := func() {
		b.HandleMouse(MouseEvent{Button: MouseButtonRight, X: frag.X + 1, Y: top + frag.Y + 1, Press: true})
		// Releasing the button must not hide the confirmation.
		b.HandleMouse(MouseEvent{Button: MouseButtonRight, X: frag.X + 1, Y: top + frag.Y + 1})
		if b.status != "Copied URL" {
			t.Fatalf("status after right-click = %q", b.status)
		}
		// A redraw between events must not drop the message.
		if got := b.footerLeft(); !strings.Contains(got, "Copied URL") {
			t.Fatalf("footer dropped the confirmation: %q", got)
		}
	}

	copyAt()
	b.HandleMouse(MouseEvent{X: 2, Y: top + 1, Motion: true})
	if b.status != "" {
		t.Fatalf("pointer motion did not clear the status: %q", b.status)
	}

	copyAt()
	b.scrollBy(1)
	if b.status != "" {
		t.Fatalf("scrolling did not clear the status: %q", b.status)
	}
}

func TestEngineNoLongerOwnsHistoryKeys(t *testing.T) {
	b := NewBrowser(40, 10)
	for _, k := range []string{"ctrl-o", "ctrl-i", "[", "]", "h", "l", "left", "right"} {
		b.status = ""
		b.HandleKey(k)
		if b.status != "" {
			t.Fatalf("%q still changes history status: %q", k, b.status)
		}
	}
}

func TestStatusDetailsShortcut(t *testing.T) {
	b := NewBrowser(40, 10)
	b.status = `Error: Invalid URL: parse "x": invalid port`
	b.HandleKey("E")
	if b.notice == nil {
		t.Fatal("E did not open the message window")
	}
	if b.notice.title != "Error" || b.notice.message != b.status {
		t.Fatalf("notice = %q / %q", b.notice.title, b.notice.message)
	}

	b.notice = nil
	b.status = "Site blocked (CAPTCHA challenge)"
	b.HandleKey("E")
	if b.notice == nil || b.notice.title != "Warning" {
		t.Fatalf("warning notice = %+v", b.notice)
	}

	b.notice = nil
	b.status = "Ready"
	b.HandleKey("E")
	if b.notice != nil {
		t.Fatal("E opened a window for a normal status")
	}
}

func TestStatusDetailsClick(t *testing.T) {
	b := NewBrowser(40, 10)
	b.status = "Error: something long that did not fit in the footer"
	drawQuiet(b)
	found := false
	for _, hit := range b.footerHits {
		if hit.kind == headerStatus {
			found = true
			if hit.w <= 0 || hit.w > b.Width {
				t.Fatalf("status hit width = %d", hit.w)
			}
		}
	}
	if !found {
		t.Fatal("footer status was not clickable")
	}
	b.HandleMouse(MouseEvent{X: 2, Y: b.Height, Press: true})
	if b.notice == nil || b.notice.title != "Error" {
		t.Fatalf("click did not open the message: %+v", b.notice)
	}
}

func TestViewMenuTogglesBackgrounds(t *testing.T) {
	b := NewBrowser(40, 10)
	b.HandleKey("X")
	g := NewGrid(40, 10, RGB(0, 0, 0), RGB(255, 255, 255))
	b.paintViewMenu(g)
	if len(b.viewMenuHits) != 3 {
		t.Fatalf("view menu hits = %d, want 3", len(b.viewMenuHits))
	}
	r := b.viewMenuHits[2].rect
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: r.X + 1, Y: r.Y + 1, Press: true})
	if !b.HideBackgrounds {
		t.Fatal("clicking Background images did not hide them")
	}
	if b.status != "Background images hidden" {
		t.Fatalf("status = %q", b.status)
	}
}

func TestZoomKeysStepThroughPresets(t *testing.T) {
	b := NewBrowser(80, 24)
	if b.Zoom != 1 {
		t.Fatalf("new browser zoom = %g, want 1", b.Zoom)
	}
	// Control-modified aliases are deliberately not bound.
	b.HandleKey("ctrl-=")
	b.HandleKey("ctrl-+")
	b.HandleKey("ctrl--")
	b.HandleKey("ctrl-0")
	if b.Zoom != 1 {
		t.Fatalf("ctrl zoom aliases changed zoom to %g, want 1", b.Zoom)
	}
	b.HandleKey("+")
	if b.Zoom != 1.25 || b.status != "Zoom 125%" {
		t.Fatalf("+ zoom=%g status=%q, want 1.25 / Zoom 125%%", b.Zoom, b.status)
	}
	b.HandleKey("+")
	if b.Zoom != 1.5 {
		t.Fatalf("+ zoom = %g, want 1.5", b.Zoom)
	}
	b.HandleKey("-")
	if b.Zoom != 1.25 {
		t.Fatalf("- zoom = %g, want 1.25", b.Zoom)
	}
	b.HandleKey("-")
	if b.Zoom != 1 {
		t.Fatalf("- zoom = %g, want 1", b.Zoom)
	}
	b.HandleKey("0")
	if b.Zoom != 1 {
		t.Fatalf("0 zoom = %g, want 1", b.Zoom)
	}
	for i := 0; i < 20; i++ {
		b.HandleKey("-")
	}
	if b.Zoom != zoomSteps[0] || zoomSteps[0] != 0.25 {
		t.Fatalf("zoom floor = %g (steps[0]=%g), want 0.25", b.Zoom, zoomSteps[0])
	}
	if b.status != "Zoom 25%" {
		t.Fatalf("zoom floor status = %q, want Zoom 25%%", b.status)
	}
	for i := 0; i < 20; i++ {
		b.HandleKey("+")
	}
	if b.Zoom != zoomSteps[len(zoomSteps)-1] {
		t.Fatalf("zoom ceiling = %g, want %g", b.Zoom, zoomSteps[len(zoomSteps)-1])
	}
	b.applyZoom(1)
}

func TestZoomStepsFromArbitraryStart(t *testing.T) {
	b := NewBrowser(80, 24)
	defer setLayoutZoom(1)
	b.applyZoom(1.4)
	b.HandleKey("+")
	if b.Zoom != 1.5 {
		t.Fatalf("+ from 140%% = %g, want 1.5", b.Zoom)
	}
	b.HandleKey("-")
	if b.Zoom != 1.25 {
		t.Fatalf("- from 150%% = %g, want 1.25", b.Zoom)
	}
}
