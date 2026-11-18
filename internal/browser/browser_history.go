package browser

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// historyEntry is one session-history position. Scroll records the document
// row the user last had on that entry so back and forward can return to it.
// Cached holds the applied page so back and forward restore it without a
// network load, as long as it is still in the bounded in-memory page cache.
type historyEntry struct {
	URL    string
	Scroll int
	cached *cachedPage
}

// maxCachedPages bounds the session's back/forward page cache. Oldest entries
// are dropped first; nothing is written to disk.
const maxCachedPages = 10

// maxAutoNavigations caps a chain of consecutive scripted or meta-refresh
// redirects. Each one needs a fresh page load, so a site that redirects to
// itself would otherwise loop forever and grow the URL and history without
// bound. User-started navigations and any page that settles reset the count.
const maxAutoNavigations = 20

// cachedPage is a fully applied page kept for back and forward navigation. The
// DOM, scripts' runtime state and reader extraction survive, so returning to
// the entry skips fetching, parsing and script execution.
type cachedPage struct {
	page              *Page
	js                *jsRuntime
	readerRoot        *Node
	readerSavedScroll int
	method            string
	body              []byte
	contentType       string
	at                time.Time
}

func metaRefreshTarget(page *Page) string {
	if page == nil || page.Root == nil {
		return ""
	}
	metas := page.Root.FindAll(func(n *Node) bool {
		return n.Tag == "meta" && strings.EqualFold(n.Attr("http-equiv"), "refresh")
	})
	for _, m := range metas {
		content := strings.TrimSpace(m.Attr("content"))
		if content == "" {
			continue
		}
		rest := content
		if i := strings.IndexByte(content, ';'); i >= 0 {
			rest = content[i+1:]
		}
		rest = strings.TrimSpace(rest)
		if len(rest) >= 4 && strings.EqualFold(rest[:4], "url=") {
			rest = rest[4:]
		}
		rest = strings.Trim(strings.TrimSpace(rest), `"'`)
		if rest == "" {
			continue
		}
		base := page.Base
		if base == nil {
			base = page.URL
		}
		if base == nil {
			return ""
		}
		if u, err := base.Parse(rest); err == nil {
			return u.String()
		}
	}
	return ""
}

// recordHistoryScroll stores the current scroll offset on the active entry.
func (b *Browser) recordHistoryScroll() {
	b.histMu.Lock()
	defer b.histMu.Unlock()
	if b.histIdx >= 0 && b.histIdx < len(b.history) {
		b.history[b.histIdx].Scroll = b.scroll
	}
}

// suspendCurrentPage keeps the applied page for back/forward when it belongs
// to the active history entry, and detaches its JavaScript runtime without
// destroying it so timers and DOM state resume on return. Pages that cannot be
// cached (about: pages, downloads) simply have their runtime stopped.
func (b *Browser) suspendCurrentPage() {
	cached := b.cacheCurrentPage()
	if b.js != nil {
		if cached {
			b.js.freeze()
		} else {
			b.js.stop()
		}
		b.js = nil
	}
}

// cacheCurrentPage stores the applied page on the active history entry. It
// reports whether the page was cached.
func (b *Browser) cacheCurrentPage() bool {
	if b.page == nil || b.page.Root == nil || b.url == nil || b.page.Download {
		return false
	}
	switch b.url.Scheme {
	case "", "about", "data", "javascript", "mailto", "tel":
		return false
	}
	c := &cachedPage{
		page:              b.page,
		js:                b.js,
		readerRoot:        b.readerRoot,
		readerSavedScroll: b.readerSavedScroll,
		method:            b.lastMethod,
		body:              b.lastBody,
		contentType:       b.lastContentType,
		at:                time.Now(),
	}
	b.histMu.Lock()
	defer b.histMu.Unlock()
	if b.histIdx < 0 || b.histIdx >= len(b.history) {
		return false
	}
	if b.history[b.histIdx].URL != b.url.String() {
		return false
	}
	b.history[b.histIdx].cached = c
	b.history[b.histIdx].Scroll = b.scroll
	b.trimPageCacheLocked()
	return true
}

// trimPageCacheLocked drops the least recently cached pages when the cache
// exceeds maxCachedPages. The caller holds histMu.
func (b *Browser) trimPageCacheLocked() {
	type slot struct {
		idx int
		at  time.Time
	}
	var slots []slot
	for i := range b.history {
		if b.history[i].cached != nil {
			slots = append(slots, slot{idx: i, at: b.history[i].cached.at})
		}
	}
	for len(slots) > maxCachedPages {
		oldest := 0
		for i := 1; i < len(slots); i++ {
			if slots[i].at.Before(slots[oldest].at) {
				oldest = i
			}
		}
		b.history[slots[oldest].idx].cached = nil
		slots = append(slots[:oldest], slots[oldest+1:]...)
	}
}

// applyCachedPage restores a page kept by the back/forward cache without any
// network or script work. The scroll position and reader extraction from the
// entry are reapplied.
func (b *Browser) applyCachedPage(idx int, entry historyEntry) {
	c := entry.cached
	b.suspendCurrentPage()
	b.clearInputFocus()
	b.cancelLoadNow()
	if b.loader != nil {
		b.loader.beginPage()
	}
	b.loadSeq++
	b.histMu.Lock()
	b.histIdx = idx
	// Restoring counts as the entry's most recent use, so keep it cached even
	// when trimming evicted it while another page was being stashed.
	c.at = time.Now()
	b.history[idx].cached = c
	b.histMu.Unlock()
	b.page = c.page
	b.url = c.page.URL
	b.imagesPaused = false
	b.lastMethod = c.method
	b.lastBody = c.body
	b.lastContentType = c.contentType
	b.layout = nil
	b.bigTextBlocked = false
	b.resetConsole()
	if c.js != nil {
		b.js = c.js
		c.js.resume()
	}
	b.readerRoot = nil
	b.readerSavedScroll = 0
	readerFailed := false
	if b.reader {
		if c.readerRoot != nil {
			b.readerRoot = c.readerRoot
			b.readerSavedScroll = c.readerSavedScroll
		} else if root, ok := extractReaderPage(c.page.Root, c.page.Title); ok {
			b.readerRoot = root
		} else {
			readerFailed = true
		}
	}
	b.rebuild()
	b.scroll = entry.Scroll
	b.clampScroll()
	b.focus = -1
	b.hover = -1
	b.clearSelection()
	b.status = b.pageLoadStatus(c.page, readerFailed)
	b.prev = nil
}

func (b *Browser) pushHistory(u string, scroll int) {
	b.histMu.Lock()
	defer b.histMu.Unlock()
	if b.histIdx >= 0 && b.histIdx < len(b.history) && b.history[b.histIdx].URL == u {
		b.history[b.histIdx].Scroll = scroll
		return
	}
	if b.histIdx+1 < len(b.history) {
		b.history = b.history[:b.histIdx+1]
	}
	b.history = append(b.history, historyEntry{URL: u, Scroll: scroll})
	b.histIdx = len(b.history) - 1
}

// prepareHistory records the current document in the session history before a
// potential automatic redirect. push is the navigation's normal push flag; a
// replace navigation only needs an entry when none exists yet.
func (b *Browser) prepareHistory(push bool) {
	if b.url == nil {
		return
	}
	if push {
		b.pushHistory(b.url.String(), 0)
		return
	}
	b.histMu.RLock()
	empty := b.histIdx < 0
	b.histMu.RUnlock()
	if empty {
		b.pushHistory(b.url.String(), 0)
	}
}

// replaceHistoryEntry updates the active history entry in place after a
// navigation that must not create a new entry (location.replace and reload).
func (b *Browser) replaceHistoryEntry(u string, scroll int) {
	b.histMu.Lock()
	defer b.histMu.Unlock()
	if b.histIdx < 0 || b.histIdx >= len(b.history) {
		return
	}
	e := &b.history[b.histIdx]
	if e.URL != u {
		if e.cached != nil && e.cached.js != nil {
			e.cached.js.stop()
		}
		e.cached = nil
	}
	e.URL = u
	e.Scroll = scroll
}

func (b *Browser) back() {
	b.clearLinkHints()
	b.recordHistoryScroll()
	b.histMu.Lock()
	if b.histIdx <= 0 {
		b.histMu.Unlock()
		b.status = "No previous page"
		return
	}
	idx := b.histIdx - 1
	entry := b.history[idx]
	b.histMu.Unlock()
	b.navigateHistoryEntry(idx, entry)
}

func (b *Browser) forward() {
	b.clearLinkHints()
	b.recordHistoryScroll()
	b.histMu.Lock()
	if b.histIdx < 0 || b.histIdx+1 >= len(b.history) {
		b.histMu.Unlock()
		b.status = "No next page"
		return
	}
	idx := b.histIdx + 1
	entry := b.history[idx]
	b.histMu.Unlock()
	b.navigateHistoryEntry(idx, entry)
}

// navigateHistoryEntry moves to a history entry. When the target is the same
// document as the current page, it updates the fragment and scroll offset in
// place without another network load. Otherwise a cached page is restored
// directly and anything else is fetched normally.
func (b *Browser) navigateHistoryEntry(idx int, entry historyEntry) {
	if b.url != nil && b.page != nil && b.page.Root != nil {
		if u, err := url.Parse(entry.URL); err == nil && sameDocument(u, b.url) {
			b.histMu.Lock()
			b.histIdx = idx
			b.histMu.Unlock()
			b.url = u
			b.scroll = entry.Scroll
			b.clampScroll()
			b.focus = -1
			b.hover = -1
			b.status = ""
			b.prev = nil
			return
		}
	}
	if entry.cached != nil {
		b.applyCachedPage(idx, entry)
		return
	}
	b.suspendCurrentPage()
	b.histMu.Lock()
	b.histIdx = idx
	b.histMu.Unlock()
	b.nextScroll = entry.Scroll
	b.nextScrollSet = true
	b.Navigate(entry.URL, false)
}

func (b *Browser) reload() {
	if b.url == nil {
		return
	}
	method := b.lastMethod
	if method == "" {
		method = http.MethodGet
	}
	b.navigateRequest(method, b.url.String(), b.lastBody, b.lastContentType, false)
}

// dropCachedPages clears the back/forward page cache. It is used when a
// session-wide setting changes, because restoring a page built under the old
// setting would be inconsistent.
func (b *Browser) dropCachedPages() {
	b.histMu.Lock()
	defer b.histMu.Unlock()
	for i := range b.history {
		if c := b.history[i].cached; c != nil {
			if c.js != nil {
				c.js.stop()
			}
			b.history[i].cached = nil
		}
	}
}
