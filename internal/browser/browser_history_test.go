package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

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

func TestHistoryPage(t *testing.T) {
	b := NewBrowser(60, 10)
	b.history = []historyEntry{{URL: "https://example.com/a"}, {URL: "about:start"}}
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

func TestJavaScriptToggleDropsPageCache(t *testing.T) {
	b := NewBrowser(80, 10)
	b.history = []historyEntry{
		{URL: "https://example.com/a", cached: &cachedPage{js: &jsRuntime{}}},
		{URL: "https://example.com/b", cached: &cachedPage{}},
	}
	b.histIdx = 1
	b.dropCachedPages()
	for i, e := range b.history {
		if e.cached != nil {
			t.Fatalf("history %d still cached", i)
		}
	}
}

func TestAnchorLinksPushHistoryAndRestoreScroll(t *testing.T) {
	b := NewBrowser(60, 10)
	b.setLoader(NewLoader())
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		u := mustParse(t, target)
		var doc strings.Builder
		doc.WriteString(`<html><body><p><a href="#target">jump</a></p>`)
		for i := 0; i < 40; i++ {
			doc.WriteString("<p>filler line</p>")
		}
		doc.WriteString(`<div id="target">target</div></body></html>`)
		return &Page{URL: u, Base: u, Title: "test", Root: ParseHTML(doc.String())}, nil
	}
	finish := func() {
		t.Helper()
		select {
		case res := <-b.loadCh:
			b.finishLoad(res)
		case <-time.After(2 * time.Second):
			t.Fatal("navigation did not finish")
		}
	}
	b.Navigate("https://example.com/", true)
	finish()
	if len(b.history) != 1 {
		t.Fatalf("history = %+v, want one entry", b.history)
	}

	b.scroll = 2
	idx := -1
	for i, li := range b.layout.Links {
		if li.Href == "#target" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("anchor link not laid out")
	}
	b.focus = idx
	b.followFocused()

	if len(b.history) != 2 {
		t.Fatalf("history after anchor = %+v, want two entries", b.history)
	}
	if b.url == nil || b.url.Fragment != "target" {
		t.Fatalf("url after anchor = %v", b.url)
	}
	anchorScroll := b.scroll
	if anchorScroll == 2 {
		t.Fatal("anchor did not scroll")
	}

	// Scrolling on the anchored entry should be remembered by forward.
	b.scroll = anchorScroll + 5
	b.clampScroll()
	rememberedScroll := b.scroll
	b.back()
	if b.url == nil || b.url.Fragment != "" {
		t.Fatalf("url after back = %v", b.url)
	}
	if b.scroll != 2 {
		t.Fatalf("back scroll = %d, want 2", b.scroll)
	}

	b.forward()
	if b.url == nil || b.url.Fragment != "target" {
		t.Fatalf("url after forward = %v", b.url)
	}
	if b.scroll != rememberedScroll {
		t.Fatalf("forward scroll = %d, want %d", b.scroll, rememberedScroll)
	}
}

func TestBackRestoresPageScroll(t *testing.T) {
	b := NewBrowser(60, 10)
	b.setLoader(NewLoader())
	loads := 0
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		loads++
		u := mustParse(t, target)
		return &Page{URL: u, Base: u, Title: target, Root: ParseHTML(`<html><body>` + strings.Repeat("<p>line</p>", 40) + `</body></html>`)}, nil
	}
	navigate := func(target string) {
		t.Helper()
		b.Navigate(target, true)
		select {
		case res := <-b.loadCh:
			b.finishLoad(res)
		case <-time.After(2 * time.Second):
			t.Fatalf("navigation to %s did not finish", target)
		}
	}
	navigate("https://example.com/a")
	b.scroll = 5
	navigate("https://example.com/b")
	if b.scroll != 0 {
		t.Fatalf("new page scroll = %d, want 0", b.scroll)
	}
	b.scroll = 7
	b.back()
	if loads != 2 {
		t.Fatalf("back refetched the page: loads = %d", loads)
	}
	if b.url == nil || b.url.Path != "/a" {
		t.Fatalf("url after back = %v", b.url)
	}
	if b.scroll != 5 {
		t.Fatalf("back scroll = %d, want 5", b.scroll)
	}
	if b.page == nil || b.page.Title != "https://example.com/a" {
		t.Fatalf("back did not restore the cached page: %+v", b.page)
	}
	b.forward()
	if loads != 2 {
		t.Fatalf("forward refetched the page: loads = %d", loads)
	}
	if b.url == nil || b.url.Path != "/b" {
		t.Fatalf("url after forward = %v", b.url)
	}
	if b.scroll != 7 {
		t.Fatalf("forward scroll = %d, want 7", b.scroll)
	}
}

func TestBackKeepsPageJavaScriptState(t *testing.T) {
	b := NewBrowser(60, 10)
	b.setLoader(NewLoader())
	loads := 0
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		loads++
		u := mustParse(t, target)
		page := &Page{URL: u, Base: u, Title: target, Root: ParseHTML(`<html><body><h1>page</h1></body></html>`)}
		if strings.HasSuffix(target, "/a") {
			page.Scripts = []Script{{Code: `window.__runs = (window.__runs || 0) + 1; document.title = "A" + window.__runs;`}}
		}
		return page, nil
	}
	navigate := func(target string) {
		t.Helper()
		b.Navigate(target, true)
		select {
		case res := <-b.loadCh:
			b.finishLoad(res)
		case <-time.After(2 * time.Second):
			t.Fatalf("navigation to %s did not finish", target)
		}
	}
	withQuietStdout(t, func() { navigate("https://example.com/a") })
	if b.page.Title != "A1" {
		t.Fatalf("script title = %q", b.page.Title)
	}
	navigate("https://example.com/b")
	withQuietStdout(t, b.back)
	if loads != 2 {
		t.Fatalf("back refetched the page: loads = %d", loads)
	}
	if b.page.Title != "A1" {
		t.Fatalf("cached title = %q, want A1", b.page.Title)
	}
	if b.js == nil {
		t.Fatal("cached page lost its JavaScript runtime")
	}
	v, err := b.js.runCode("test", "window.__runs")
	if err != nil {
		t.Fatal(err)
	}
	if got := v.String(); got != "1" {
		t.Fatalf("script reran on back: __runs = %s", got)
	}
}

func TestPageCacheIsBounded(t *testing.T) {
	b := NewBrowser(60, 10)
	b.setLoader(NewLoader())
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		u := mustParse(t, target)
		return &Page{URL: u, Base: u, Title: target, Root: ParseHTML(`<html><body><p>x</p></body></html>`)}, nil
	}
	for i := 0; i < maxCachedPages+3; i++ {
		target := fmt.Sprintf("https://example.com/p%d", i)
		b.Navigate(target, true)
		select {
		case res := <-b.loadCh:
			b.finishLoad(res)
		case <-time.After(2 * time.Second):
			t.Fatalf("navigation %d did not finish", i)
		}
	}
	cached := 0
	for _, entry := range b.history {
		if entry.cached != nil {
			cached++
		}
	}
	if cached != maxCachedPages {
		t.Fatalf("cached pages = %d, want %d", cached, maxCachedPages)
	}
	// The most recent page must still be cached for an immediate back.
	last := b.history[len(b.history)-2]
	if last.cached == nil {
		t.Fatal("most recent page was evicted from the cache")
	}
}

func TestAboutPagesAreNotCached(t *testing.T) {
	b := NewBrowser(60, 10)
	b.setLoader(NewLoader())
	b.Navigate("https://example.com/a", true)
	select {
	case res := <-b.loadCh:
		b.finishLoad(res)
	case <-time.After(2 * time.Second):
		t.Fatal("navigation did not finish")
	}
	b.Navigate("about:help", true)
	select {
	case res := <-b.loadCh:
		b.finishLoad(res)
	case <-time.After(2 * time.Second):
		t.Fatal("help navigation did not finish")
	}
	if b.history[b.histIdx].cached != nil {
		t.Fatal("about:help was cached")
	}
}

func TestJSRedirectsDuringLoadCoalesce(t *testing.T) {
	// A client redirect that runs while the page is being applied replaces the
	// redirecting entry, the way browsers coalesce it; Back must skip it.
	cases := []struct {
		name     string
		script   string
		wantURLs []string
	}{
		{
			name:     "replace",
			script:   `location.replace("/next")`,
			wantURLs: []string{"https://example.com/next"},
		},
		{
			name:     "assign",
			script:   `location.assign("/next")`,
			wantURLs: []string{"https://example.com/next"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBrowser(80, 10)
			b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
				u, err := url.Parse(target)
				if err != nil {
					return nil, err
				}
				doc := `<html><body>plain</body></html>`
				page := &Page{URL: u, Base: u, Root: ParseHTML(doc)}
				if u.Path == "/start" {
					page.Scripts = []Script{{Code: tc.script, Src: "test"}}
				}
				return page, nil
			}
			b.Navigate("https://example.com/start", true)
			for i := 0; i < 2; i++ {
				select {
				case res := <-b.loadCh:
					b.finishLoad(res)
				case <-time.After(2 * time.Second):
					t.Fatalf("load %d did not finish", i+1)
				}
			}
			var got []string
			for _, e := range b.history {
				got = append(got, e.URL)
			}
			if strings.Join(got, " ") != strings.Join(tc.wantURLs, " ") {
				t.Fatalf("history = %v, want %v", got, tc.wantURLs)
			}
		})
	}
}

func TestScriptRedirectLoopStops(t *testing.T) {
	b := NewBrowser(80, 10)
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		u, err := url.Parse(target)
		if err != nil {
			return nil, err
		}
		doc := `<html><body>loop</body></html>`
		return &Page{URL: u, Base: u, Root: ParseHTML(doc),
			Scripts: []Script{{Code: `location.replace(location.href + "x")`, Src: "test"}}}, nil
	}
	b.Navigate("https://example.com/start", true)
	stopped := false
	for i := 0; i < maxAutoNavigations+3; i++ {
		select {
		case res := <-b.loadCh:
			b.finishLoad(res)
		case <-time.After(2 * time.Second):
			t.Fatal("redirect loop stalled without a result")
		}
		if strings.HasPrefix(b.status, "Redirect loop stopped") {
			stopped = true
			break
		}
	}
	if !stopped {
		t.Fatalf("redirect loop was not stopped; status = %q", b.status)
	}
	if b.loading {
		t.Fatal("a load is still running after the loop was stopped")
	}
	if len(b.history) != 1 {
		t.Fatalf("history grew to %d entries: %v", len(b.history), b.history)
	}
}

func TestMetaRefreshLoopStops(t *testing.T) {
	b := NewBrowser(80, 10)
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		u, err := url.Parse(target)
		if err != nil {
			return nil, err
		}
		n, _ := strconv.Atoi(u.Query().Get("n"))
		doc := `<html><head><meta http-equiv="refresh" content="0;url=/loop?n=` + strconv.Itoa(n+1) + `"></head><body>loop</body></html>`
		return &Page{URL: u, Base: u, Root: ParseHTML(doc)}, nil
	}
	b.Navigate("https://example.com/loop?n=0", true)
	stopped := false
	for i := 0; i < maxAutoNavigations+3; i++ {
		select {
		case res := <-b.loadCh:
			b.finishLoad(res)
		case <-time.After(2 * time.Second):
			t.Fatal("meta refresh loop stalled without a result")
		}
		if strings.HasPrefix(b.status, "Redirect loop stopped") {
			stopped = true
			break
		}
	}
	if !stopped {
		t.Fatalf("meta refresh loop was not stopped; status = %q", b.status)
	}
	if b.loading {
		t.Fatal("a load is still running after the loop was stopped")
	}
}

func TestMetaRefreshCoalescesHistoryEntry(t *testing.T) {
	b := NewBrowser(60, 12)
	uRedir := mustParse(t, "https://example.com/redir")
	uTarget := mustParse(t, "https://example.com/target")
	redir := &Page{URL: uRedir, Base: uRedir, Root: ParseHTML(
		`<html><head><meta http-equiv="refresh" content="0;url=/target"></head><body>redirecting</body></html>`)}
	target := &Page{URL: uTarget, Base: uTarget, Root: ParseHTML(`<html><body>target</body></html>`)}
	b.loadFn = func(context.Context, string, string, []byte, string) (*Page, error) { return target, nil }

	b.finishLoad(loadResult{seq: b.loadSeq, page: redir, u: uRedir, push: true, method: http.MethodGet})
	select {
	case res := <-b.loadCh:
		b.finishLoad(res)
	case <-time.After(2 * time.Second):
		t.Fatal("meta refresh navigation did not finish")
	}
	urls, idx := b.historySnapshot()
	if len(urls) != 1 || urls[0] != "https://example.com/target" || idx != 0 {
		t.Fatalf("history = %v at %d, want only the target", urls, idx)
	}
}

func TestClickTimeJSNavigationPushesHistory(t *testing.T) {
	// A navigation queued after the page settled (a click or timer handler)
	// still adds an entry, so Back returns to the page the user was on.
	b := NewBrowser(60, 12)
	u := mustParse(t, "https://example.com/start")
	next := mustParse(t, "https://example.com/next")
	b.loadFn = func(context.Context, string, string, []byte, string) (*Page, error) {
		return &Page{URL: next, Base: next, Root: ParseHTML(`<html><body>next</body></html>`)}, nil
	}
	b.finishLoad(loadResult{seq: b.loadSeq, page: &Page{URL: u, Base: u, Root: ParseHTML(`<html><body>start</body></html>`)}, u: u, push: true, method: http.MethodGet})
	if b.js == nil {
		t.Fatal("no JS runtime")
	}
	b.js.pendingNav = "/next"
	if !b.consumePendingJS() {
		t.Fatal("queued navigation was not applied")
	}
	select {
	case res := <-b.loadCh:
		b.finishLoad(res)
	case <-time.After(2 * time.Second):
		t.Fatal("navigation did not finish")
	}
	urls, _ := b.historySnapshot()
	if len(urls) != 2 || urls[0] != "https://example.com/start" || urls[1] != "https://example.com/next" {
		t.Fatalf("history = %v, want start then next", urls)
	}
}
