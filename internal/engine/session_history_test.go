package engine

import (
	"context"
	"fmt"
	"image"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	"browser/internal/protocol"
)

// testHistory mirrors the front-end's ordered list so the engine tests can
// drive restore and inspect the commits the engine emits.
type testHistory struct {
	entries []testHistoryEntry
	index   int
}

type testHistoryEntry struct {
	id     uint64
	url    string
	scroll int
}

func attachHistory(b *Session) *testHistory {
	h := &testHistory{index: -1}
	b.historyObserver = h.apply
	return h
}

func (h *testHistory) apply(c protocol.HistoryCommit) {
	e := testHistoryEntry{id: c.EntryID, url: c.URL, scroll: c.Scroll}
	if c.Push {
		if h.index >= 0 && h.index < len(h.entries) && h.entries[h.index].url == c.URL {
			h.entries[h.index] = e
			return
		}
		if h.index+1 < len(h.entries) {
			h.entries = h.entries[:h.index+1]
		}
		h.entries = append(h.entries, e)
		h.index = len(h.entries) - 1
		return
	}
	if len(h.entries) == 0 {
		h.entries = append(h.entries, e)
		h.index = 0
		return
	}
	if h.index < 0 || h.index >= len(h.entries) {
		h.index = len(h.entries) - 1
	}
	h.entries[h.index] = e
}

func (h *testHistory) record(b *Session) {
	if h.index >= 0 && h.index < len(h.entries) {
		h.entries[h.index].scroll = b.Scroll
	}
}

func (h *testHistory) back(b *Session) {
	h.record(b)
	if h.index <= 0 {
		b.status = "No previous page"
		return
	}
	h.index--
	e := h.entries[h.index]
	b.restoreEntry(e.id, e.url, e.scroll)
}

func (h *testHistory) forward(b *Session) {
	h.record(b)
	if len(h.entries) == 0 || h.index+1 >= len(h.entries) {
		b.status = "No next page"
		return
	}
	h.index++
	e := h.entries[h.index]
	b.restoreEntry(e.id, e.url, e.scroll)
}

func (h *testHistory) urls() []string {
	out := make([]string, len(h.entries))
	for i, e := range h.entries {
		out[i] = e.url
	}
	return out
}

func navigateAndFinish(t *testing.T, b *Session, target string) {
	t.Helper()
	b.Navigate(target, true)
	select {
	case res := <-b.LoadCh:
		b.finishLoad(res)
	case <-time.After(2 * time.Second):
		t.Fatalf("navigation to %s did not finish", target)
	}
}

func TestHistoryCommitRecordsSession(t *testing.T) {
	b := newTestSession(60, 10)
	b.setLoader(newTestLoader())
	h := attachHistory(b)
	b.LoadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		u := mustParse(t, target)
		return &Page{URL: u, Base: u, Title: u.Path, Root: ParseHTML("<html><body>ok</body></html>")}, nil
	}
	navigateAndFinish(t, b, "https://example.com/a")
	navigateAndFinish(t, b, "https://example.com/b")
	if got := strings.Join(h.urls(), " "); got != "https://example.com/a https://example.com/b" {
		t.Fatalf("history = %q", got)
	}
	if h.index != 1 || h.entries[0].id == 0 || h.entries[1].id == 0 || h.entries[0].id == h.entries[1].id {
		t.Fatalf("history index/ids = %+v at %d", h.entries, h.index)
	}

	b.setHistoryView(protocol.History{Entries: []protocol.HistoryEntry{
		{URL: "https://example.com/a"}, {URL: "https://example.com/b"},
	}, Current: 1})
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
	b := newTestSession(60, 10)
	b.setHistoryView(protocol.History{Entries: []protocol.HistoryEntry{
		{URL: "https://example.com/a"}, {URL: "about:start"},
	}, Current: 1})
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

	empty := newTestSession(60, 10)
	p2, err := empty.loader.LoadContext(context.Background(), "about:history", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p2.Root.TextContent(), "No pages in this session") {
		t.Fatalf("empty history = %q", p2.Root.TextContent())
	}
}

func TestJavaScriptToggleDropsPageCache(t *testing.T) {
	b := newTestSession(80, 10)
	b.Cache.Put(1, &cachedPage{JS: &jsRuntime{}})
	b.Cache.Put(2, &cachedPage{})
	b.dropCachedPages()
	if b.Cache.Len() != 0 {
		t.Fatalf("cache still has %d pages", b.Cache.Len())
	}
}

func TestAnchorLinksPushHistoryAndRestoreScroll(t *testing.T) {
	b := newTestSession(60, 10)
	b.setLoader(newTestLoader())
	h := attachHistory(b)
	b.LoadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		u := mustParse(t, target)
		var doc strings.Builder
		doc.WriteString(`<html><body><p><a href="#target">jump</a></p>`)
		for i := 0; i < 40; i++ {
			doc.WriteString("<p>filler line</p>")
		}
		doc.WriteString(`<div id="target">target</div></body></html>`)
		return &Page{URL: u, Base: u, Title: "test", Root: ParseHTML(doc.String())}, nil
	}
	navigateAndFinish(t, b, "https://example.com/")
	if len(h.entries) != 1 {
		t.Fatalf("history = %+v, want one entry", h.entries)
	}

	b.Scroll = 2
	h.record(b)
	idx := -1
	for i, li := range b.Layout.Links {
		if li.Href == "#target" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("anchor link not laid out")
	}
	b.Focus = idx
	b.followFocused()

	if len(h.entries) != 2 {
		t.Fatalf("history after anchor = %+v, want two entries", h.entries)
	}
	if b.URL == nil || b.URL.Fragment != "target" {
		t.Fatalf("url after anchor = %v", b.URL)
	}
	anchorScroll := b.Scroll
	if anchorScroll == 2 {
		t.Fatal("anchor did not scroll")
	}

	// Scrolling on the anchored entry should be remembered by forward.
	b.Scroll = anchorScroll + 5
	b.clampScroll()
	rememberedScroll := b.Scroll
	h.back(b)
	if b.URL == nil || b.URL.Fragment != "" {
		t.Fatalf("url after back = %v", b.URL)
	}
	if b.Scroll != 2 {
		t.Fatalf("back scroll = %d, want 2", b.Scroll)
	}

	h.forward(b)
	if b.URL == nil || b.URL.Fragment != "target" {
		t.Fatalf("url after forward = %v", b.URL)
	}
	if b.Scroll != rememberedScroll {
		t.Fatalf("forward scroll = %d, want %d", b.Scroll, rememberedScroll)
	}
}

func TestBackRestoresPageScroll(t *testing.T) {
	b := newTestSession(60, 10)
	b.setLoader(newTestLoader())
	h := attachHistory(b)
	loads := 0
	b.LoadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		loads++
		u := mustParse(t, target)
		return &Page{URL: u, Base: u, Title: target, Root: ParseHTML(`<html><body>` + strings.Repeat("<p>line</p>", 40) + `</body></html>`)}, nil
	}
	navigateAndFinish(t, b, "https://example.com/a")
	b.Scroll = 5
	h.record(b)
	navigateAndFinish(t, b, "https://example.com/b")
	if b.Scroll != 0 {
		t.Fatalf("new page scroll = %d, want 0", b.Scroll)
	}
	b.Scroll = 7
	h.back(b)
	if loads != 2 {
		t.Fatalf("back refetched the page: loads = %d", loads)
	}
	if b.URL == nil || b.URL.Path != "/a" {
		t.Fatalf("url after back = %v", b.URL)
	}
	if b.Scroll != 5 {
		t.Fatalf("back scroll = %d, want 5", b.Scroll)
	}
	if b.Page == nil || b.Page.Title != "https://example.com/a" {
		t.Fatalf("back did not restore the cached page: %+v", b.Page)
	}
	h.forward(b)
	if loads != 2 {
		t.Fatalf("forward refetched the page: loads = %d", loads)
	}
	if b.URL == nil || b.URL.Path != "/b" {
		t.Fatalf("url after forward = %v", b.URL)
	}
	if b.Scroll != 7 {
		t.Fatalf("forward scroll = %d, want 7", b.Scroll)
	}
}

func TestBackKeepsPageJavaScriptState(t *testing.T) {
	b := newTestSession(60, 10)
	b.setLoader(newTestLoader())
	h := attachHistory(b)
	loads := 0
	b.LoadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		loads++
		u := mustParse(t, target)
		page := &Page{URL: u, Base: u, Title: target, Root: ParseHTML(`<html><body><h1>page</h1></body></html>`)}
		if strings.HasSuffix(target, "/a") {
			page.Scripts = []Script{{Code: `window.__runs = (window.__runs || 0) + 1; document.title = "A" + window.__runs;`}}
		}
		return page, nil
	}
	withQuietStdout(t, func() { navigateAndFinish(t, b, "https://example.com/a") })
	if b.Page.Title != "A1" {
		t.Fatalf("script title = %q", b.Page.Title)
	}
	navigateAndFinish(t, b, "https://example.com/b")
	withQuietStdout(t, func() { h.back(b) })
	if loads != 2 {
		t.Fatalf("back refetched the page: loads = %d", loads)
	}
	if b.Page.Title != "A1" {
		t.Fatalf("cached title = %q, want A1", b.Page.Title)
	}
	if b.JS == nil {
		t.Fatal("cached page lost its JavaScript runtime")
	}
	v, err := b.JS.RunCode("test", "window.__runs")
	if err != nil {
		t.Fatal(err)
	}
	if got := v.String(); got != "1" {
		t.Fatalf("script reran on back: __runs = %s", got)
	}
}

func TestHistoryCacheIsBounded(t *testing.T) {
	b := newTestSession(60, 10)
	b.setLoader(newTestLoader())
	attachHistory(b)
	b.LoadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		u := mustParse(t, target)
		return &Page{URL: u, Base: u, Title: target, Root: ParseHTML(`<html><body><p>x</p></body></html>`)}, nil
	}
	for i := 0; i < maxCachedPages+3; i++ {
		navigateAndFinish(t, b, fmt.Sprintf("https://example.com/p%d", i))
	}
	if got := b.Cache.Len(); got != maxCachedPages {
		t.Fatalf("cached pages = %d, want %d", got, maxCachedPages)
	}
	// The most recently replaced page must still be cached for an immediate back.
	if b.Cache.Get(b.NextEntryID-2) == nil {
		t.Fatal("most recent page was evicted from the cache")
	}
}

func TestAboutPagesAreNotCached(t *testing.T) {
	b := newTestSession(60, 10)
	b.setLoader(newTestLoader())
	attachHistory(b)
	navigateAndFinish(t, b, "https://example.com/a")
	navigateAndFinish(t, b, "about:help")
	if b.Cache.Get(b.EntryID) != nil {
		t.Fatal("about:help was cached")
	}
}

func TestJSRedirectsDuringLoadCoalesce(t *testing.T) {
	// A client redirect that runs while the page is being applied keeps the
	// same entry, the way browsers coalesce it; Back must skip it.
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
			b := newTestSession(80, 10)
			h := attachHistory(b)
			b.LoadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
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
				case res := <-b.LoadCh:
					b.finishLoad(res)
				case <-time.After(2 * time.Second):
					t.Fatalf("load %d did not finish", i+1)
				}
			}
			if got := strings.Join(h.urls(), " "); got != strings.Join(tc.wantURLs, " ") {
				t.Fatalf("history = %v, want %v", h.urls(), tc.wantURLs)
			}
		})
	}
}

func TestScriptRedirectLoopStops(t *testing.T) {
	b := newTestSession(80, 10)
	h := attachHistory(b)
	b.LoadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
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
		case res := <-b.LoadCh:
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
	if b.Loading {
		t.Fatal("a load is still running after the loop was stopped")
	}
	if len(h.entries) != 1 {
		t.Fatalf("history grew to %d entries: %v", len(h.entries), h.urls())
	}
}

func TestMetaRefreshLoopStops(t *testing.T) {
	b := newTestSession(80, 10)
	b.LoadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
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
		case res := <-b.LoadCh:
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
	if b.Loading {
		t.Fatal("a load is still running after the loop was stopped")
	}
}

func TestMetaRefreshCoalescesHistoryEntry(t *testing.T) {
	b := newTestSession(60, 12)
	h := attachHistory(b)
	uRedir := mustParse(t, "https://example.com/redir")
	uTarget := mustParse(t, "https://example.com/target")
	redir := &Page{URL: uRedir, Base: uRedir, Root: ParseHTML(
		`<html><head><meta http-equiv="refresh" content="0;url=/target"></head><body>redirecting</body></html>`)}
	target := &Page{URL: uTarget, Base: uTarget, Root: ParseHTML(`<html><body>target</body></html>`)}
	b.LoadFn = func(context.Context, string, string, []byte, string) (*Page, error) { return target, nil }

	id := b.nextHistoryID()
	b.finishLoad(LoadResult{Seq: b.LoadSeq, Page: redir, URL: uRedir, EntryID: id, HistoryPush: true, Method: http.MethodGet})
	select {
	case res := <-b.LoadCh:
		b.finishLoad(res)
	case <-time.After(2 * time.Second):
		t.Fatal("meta refresh navigation did not finish")
	}
	if got := strings.Join(h.urls(), " "); got != "https://example.com/target" {
		t.Fatalf("history = %v, want only the target", h.urls())
	}
	if h.index != 0 || h.entries[0].id != id {
		t.Fatalf("history entry = %+v, want id %d", h.entries, id)
	}
}

func TestClickTimeJSNavigationPushesHistory(t *testing.T) {
	// A navigation queued after the page settled (a click or timer handler)
	// still adds an entry, so Back returns to the page the user was on.
	b := newTestSession(60, 12)
	h := attachHistory(b)
	u := mustParse(t, "https://example.com/start")
	next := mustParse(t, "https://example.com/next")
	b.LoadFn = func(context.Context, string, string, []byte, string) (*Page, error) {
		return &Page{URL: next, Base: next, Root: ParseHTML(`<html><body>next</body></html>`)}, nil
	}
	id := b.nextHistoryID()
	b.finishLoad(LoadResult{Seq: b.LoadSeq, EntryID: id, Page: &Page{URL: u, Base: u, Root: ParseHTML(`<html><body>start</body></html>`)}, URL: u, HistoryPush: true, Method: http.MethodGet})
	if b.JS == nil {
		t.Fatal("no JS runtime")
	}
	b.JS.SetPendingNav("/next")
	if !b.consumePendingJS() {
		t.Fatal("queued navigation was not applied")
	}
	select {
	case res := <-b.LoadCh:
		b.finishLoad(res)
	case <-time.After(2 * time.Second):
		t.Fatal("navigation did not finish")
	}
	want := "https://example.com/start https://example.com/next"
	if got := strings.Join(h.urls(), " "); got != want {
		t.Fatalf("history = %q, want %q", got, want)
	}
}

func TestHistoryCacheEvictsByWeight(t *testing.T) {
	b := newTestSession(60, 10)
	b.Cache.SetLimitBytes(3 << 20)
	now := time.Now()
	for i := 0; i < 4; i++ {
		b.Cache.Put(uint64(i+1), &cachedPage{Bytes: 1 << 20, At: now.Add(time.Duration(i) * time.Second)})
	}
	if total := b.Cache.CachedBytes(); total > b.Cache.LimitBytes() {
		t.Fatalf("cached bytes = %d, budget = %d", total, b.Cache.LimitBytes())
	}
	if b.Cache.Len() != 3 {
		t.Fatalf("cached pages = %d, want 3", b.Cache.Len())
	}
	if b.Cache.Get(1) != nil {
		t.Fatal("oldest page survived the weight budget")
	}
	if b.Cache.Get(4) == nil {
		t.Fatal("newest page was evicted")
	}
}

func TestHistoryCacheSkipsHeavyPage(t *testing.T) {
	b := newTestSession(60, 10)
	b.Cache.SetLimitBytes(2 << 20) // single-page cap is 1 MiB
	u := mustParse(t, "https://example.com/heavy")
	b.URL = u
	b.EntryID = 1
	b.Page = &Page{URL: u, Base: u, Root: ParseHTML(`<html><body><p>heavy</p></body></html>`),
		RawHTML: strings.Repeat("x", 1200*1024)}
	if b.cacheCurrentPage() {
		t.Fatal("page heavier than the single-page cap was cached")
	}
	if b.Cache.Get(1) != nil {
		t.Fatal("heavy page was stored in the cache")
	}
	b.Page.RawHTML = "<html><body><p>light</p></body></html>"
	if !b.cacheCurrentPage() {
		t.Fatal("light page was not cached")
	}
	if c := b.Cache.Get(1); c == nil || c.Bytes <= 0 {
		t.Fatal("light page has no recorded weight")
	}
}

func TestPageWeightCountsScriptsImagesAndRuntime(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	if got := imageWeight(img); got != 100*100*4 {
		t.Fatalf("RGBA weight = %d", got)
	}
	if got := imageWeight(image.NewGray(image.Rect(0, 0, 50, 40))); got != 50*40 {
		t.Fatalf("gray weight = %d", got)
	}
	yc := image.NewYCbCr(image.Rect(0, 0, 40, 30), image.YCbCrSubsampleRatio420)
	if got, want := imageWeight(yc), int64(40*30*3/2); got != want {
		t.Fatalf("YCbCr weight = %d, want %d", got, want)
	}

	doc := ParseHTML(`<html><body><img src="a.png" alt="a"><p>text</p></body></html>`)
	doc.FindFirst("img").Img = img
	page := &Page{Root: doc, RawHTML: strings.Repeat("h", 1000),
		Scripts: []Script{{Code: strings.Repeat("s", 500)}}, Author: make([]Rule, 3)}
	plain := pageWeight(page, nil, nil, nil)
	if plain < 1000+500+100*100*4 {
		t.Fatalf("weight = %d, does not count source, scripts and image", plain)
	}
	withJS := pageWeight(page, &jsRuntime{}, nil, nil)
	if withJS-plain < 4<<20 {
		t.Fatalf("runtime allowance = %d, want at least %d", withJS-plain, 4<<20)
	}
}

func TestHistoryCacheDisabledDropsEntries(t *testing.T) {
	b := newTestSession(60, 10)
	b.Cache.Put(1, &cachedPage{Bytes: 1 << 20})
	b.setHistoryCacheLimitMB(0)
	if b.Cache.LimitBytes() != 0 {
		t.Fatalf("budget = %d, want 0", b.Cache.LimitBytes())
	}
	if b.Cache.Len() != 0 {
		t.Fatal("disabling the page cache kept an entry")
	}
	if b.cacheCurrentPage() {
		t.Fatal("a page was cached with the cache disabled")
	}
}

func TestMemoryLimitDerivesCaches(t *testing.T) {
	prev := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
	b := newTestSession(60, 10)
	b.setMemoryLimitMB(1024)
	if got, want := b.Cache.LimitBytes(), MemoryFraction(int64(1)<<30, historyCachePercent); got != want {
		t.Fatalf("history cache = %d, want %d", got, want)
	}
	if got, want := testHost(b.loader).Cache().LimitBytes(), MemoryFraction(int64(1)<<30, 3); got != want {
		t.Fatalf("resource cache = %d, want %d", got, want)
	}
	// Clearing the limit restores the fallback budgets.
	b.setMemoryLimitMB(0)
	if b.Cache.LimitBytes() != fallbackHistoryCacheBytes {
		t.Fatalf("history cache fallback = %d, want %d", b.Cache.LimitBytes(), fallbackHistoryCacheBytes)
	}
	if got := testHost(b.loader).Cache().LimitBytes(); got != int64(32)<<20 {
		t.Fatalf("resource cache fallback = %d, want %d", got, int64(32)<<20)
	}
}

func TestLightEvictionDoesNotScavenge(t *testing.T) {
	b := newTestSession(60, 10)
	now := time.Now()
	for i := 0; i < 12; i++ {
		b.Cache.Put(uint64(i+1), &cachedPage{Bytes: 1 << 20, At: now.Add(time.Duration(i) * time.Second)})
	}
	b.Cache.SetLimitBytes(8 << 20)
	if b.Cache.CachedBytes() > b.Cache.LimitBytes() {
		t.Fatalf("cached bytes = %d, budget = %d", b.Cache.CachedBytes(), b.Cache.LimitBytes())
	}
	if b.takeScavengePending() {
		t.Fatal("small evictions requested a scavenge")
	}
}

func TestHeavyEvictionRequestsScavenge(t *testing.T) {
	b := newTestSession(60, 10)
	now := time.Now()
	b.Cache.Put(1, &cachedPage{Bytes: 1 << 20, At: now})
	b.Cache.Put(2, &cachedPage{Bytes: 4 << 20, At: now.Add(time.Second)})
	b.Cache.Put(3, &cachedPage{Bytes: 4 << 20, At: now.Add(2 * time.Second)})
	b.Cache.SetLimitBytes(8 << 20)
	if b.takeScavengePending() {
		t.Fatal("fitting the budget with a light eviction requested a scavenge")
	}
	b.Cache.Put(4, &cachedPage{Bytes: 4 << 20, At: now.Add(3 * time.Second)})
	if !b.takeScavengePending() {
		t.Fatal("heavy eviction did not request a scavenge")
	}
	if b.takeScavengePending() {
		t.Fatal("scavenge request was not consumed exactly once")
	}
}

func TestRestoreFetchesUncachedEntryWithScroll(t *testing.T) {
	b := newTestSession(60, 10)
	b.setLoader(newTestLoader())
	h := attachHistory(b)
	b.LoadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		u := mustParse(t, target)
		return &Page{URL: u, Base: u, Title: target, Root: ParseHTML(`<html><body>` + strings.Repeat("<p>line</p>", 40) + `</body></html>`)}, nil
	}
	navigateAndFinish(t, b, "https://example.com/a")
	b.restoreEntry(9, "https://example.com/c", 7)
	select {
	case res := <-b.LoadCh:
		b.finishLoad(res)
	case <-time.After(2 * time.Second):
		t.Fatal("restore did not load")
	}
	if b.EntryID != 9 || b.URL == nil || b.URL.Path != "/c" {
		t.Fatalf("entry=%d url=%v", b.EntryID, b.URL)
	}
	if b.Scroll != 7 {
		t.Fatalf("scroll = %d, want 7", b.Scroll)
	}
	if got := h.entries[len(h.entries)-1]; got.id != 9 || got.url != "https://example.com/c" || got.scroll != 7 {
		t.Fatalf("restore commit = %+v", got)
	}
	if h.lastPush() {
		t.Fatal("restore pushed a new entry")
	}
}

func (h *testHistory) lastPush() bool {
	// The helper only stores entries; a restore commit updates the active
	// entry in place, so the list length stays at two.
	return len(h.entries) > 2
}

func TestHistoryCommandStoresSnapshot(t *testing.T) {
	b := newTestSession(60, 10)
	if _, err := b.handleCommand(protocol.Command{Body: protocol.History{
		Entries: []protocol.HistoryEntry{{URL: "https://example.com/a"}, {URL: "https://example.com/b"}},
		Current: 0,
	}}); err != nil {
		t.Fatal(err)
	}
	page, err := b.loader.LoadContext(context.Background(), "about:history", nil)
	if err != nil {
		t.Fatal(err)
	}
	text := page.Root.TextContent()
	if !strings.Contains(text, "https://example.com/a") || !strings.Contains(text, "(current)") {
		t.Fatalf("history text = %q", text)
	}
}
