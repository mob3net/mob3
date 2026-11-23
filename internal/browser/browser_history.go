package browser

import (
	"image"
	"net/http"
	"net/url"
	"time"

	"browser/internal/engine"
	"browser/internal/protocol"
)

// maxCachedPages and cachedPage are engine-owned now.
const maxCachedPages = engine.MaxCachedPages

type cachedPage = engine.CachedPage

func pageWeight(page *Page, js *jsRuntime, body []byte, readerRoot *Node) int64 {
	return engine.PageWeight(page, js, body, readerRoot)
}
func domWeight(root *Node) int64        { return engine.DOMWeight(root) }
func imageWeight(img image.Image) int64 { return engine.ImageWeight(img) }

// maxAutoNavigations caps a chain of consecutive scripted or meta-refresh
// redirects. Each one needs a fresh page load, so a site that redirects to
// itself would otherwise loop forever and grow the URL and history without
// bound. User-started navigations and any page that settles reset the count.
const maxAutoNavigations = 20

// historyCachePageCap is the largest single page the back/forward cache will keep.
// Half the budget leaves room for the page you came from while stopping one
// heavy page from pinning the whole allowance.
func (b *Browser) historyCachePageCap() int64 { return b.Cache.PageCap() }

// setHistoryCacheLimitMB applies a new back/forward cache budget and trims the
// cache immediately. Zero disables page caching.
func (b *Browser) setHistoryCacheLimitMB(mb int) {
	if mb < 0 {
		mb = 0
	}
	b.Cache.SetLimitBytes(int64(mb) << 20)
}

// noteEvictedPage asks the event loop for one scavenge when the evicted page
// held a large part of the cache budget, so its memory returns to the OS now
// instead of waiting for the background scavenger. Small evictions do not
// justify a stop-the-world pass.
func (b *Browser) noteEvictedPage(size, limit int64) {
	if size <= 0 {
		return
	}
	if limit > 0 && size < limit/4 {
		return
	}
	b.scavengePending.Store(true)
}

func metaRefreshTarget(page *Page) string { return engine.MetaRefreshTarget(page) }

// suspendCurrentPage keeps the applied page for back/forward when it belongs
// to the current front-end entry, and detaches its JavaScript runtime without
// destroying it so timers and DOM state resume on return. Pages that cannot be
// cached (about: pages, downloads) simply have their runtime stopped.
func (b *Browser) suspendCurrentPage() {
	cached := b.cacheCurrentPage()
	if b.JS != nil {
		if cached {
			b.JS.Freeze()
		} else {
			b.JS.Stop()
		}
		b.JS = nil
	}
}

// cacheCurrentPage stores the applied page under the current front-end entry
// ID. It reports whether the page was cached.
func (b *Browser) cacheCurrentPage() bool {
	if b.Page == nil || b.Page.Root == nil || b.URL == nil || b.Page.Download || b.entryID == 0 {
		return false
	}
	switch b.URL.Scheme {
	case "", "about", "data", "javascript", "mailto", "tel":
		return false
	}
	weight := engine.PageWeight(b.Page, b.JS, b.LastBody, b.ReaderRoot)
	if weight > b.Cache.PageCap() {
		return false
	}
	b.Cache.Put(b.entryID, &cachedPage{
		Page: b.Page, JS: b.JS, ReaderRoot: b.ReaderRoot,
		ReaderSavedScroll: b.ReaderSavedScroll, Method: b.LastMethod,
		Body: b.LastBody, ContentType: b.LastContentType, At: time.Now(), Bytes: weight,
	})
	return true
}

// dropCachedEntryUnless removes an entry's cached page when its URL differs
// from u. A same-URL reload keeps the old cached page, matching the old history
// semantics.
func (b *Browser) dropCachedEntryUnless(id uint64, u string) { b.Cache.DeleteUnlessURL(id, u) }

// applyCachedPage restores a page kept by the back/forward cache without any
// network or script work. The scroll position and reader extraction from the
// entry are reapplied.
func (b *Browser) applyCachedPage(entryID uint64, c *cachedPage, scroll int) {
	b.suspendCurrentPage()
	b.clearInputFocus()
	b.cancelLoadNow()
	if b.loader != nil {
		b.loader.beginPage()
	}
	b.LoadSeq++
	b.entryID = entryID
	b.Page = c.Page
	b.URL = c.Page.URL
	b.ImagesPaused = false
	b.LastMethod = c.Method
	b.LastBody = c.Body
	b.LastContentType = c.ContentType
	b.Layout = nil
	b.BigTextBlocked = false
	b.resetConsole()
	if c.JS != nil {
		b.JS = c.JS
		c.JS.Resume()
	}
	b.ReaderRoot = nil
	b.ReaderSavedScroll = 0
	readerFailed := false
	if b.Reader {
		if c.ReaderRoot != nil {
			b.ReaderRoot = c.ReaderRoot
			b.ReaderSavedScroll = c.ReaderSavedScroll
		} else if c.Page.PDF != nil {
			readerFailed = true
		} else if root, ok := extractReaderPage(c.Page.Root, c.Page.Title); ok {
			b.ReaderRoot = root
		} else {
			readerFailed = true
		}
	}
	b.rebuild()
	b.Scroll = scroll
	b.clampScroll()
	b.Focus = -1
	b.Hover = -1
	b.clearSelection()
	b.status = b.pageLoadStatus(c.Page, readerFailed)
	b.prev = nil
}

// restoreEntry returns to a front-end history entry. A same-document target
// updates the fragment and scroll in place; a cached page is restored without
// a load; anything else is fetched with the entry's scroll offset.
func (b *Browser) restoreEntry(entryID uint64, rawURL string, scroll int) {
	if entryID == 0 {
		return
	}
	b.clearLinkHints()
	if b.URL != nil && b.Page != nil && b.Page.Root != nil {
		if u, err := url.Parse(rawURL); err == nil && sameDocument(u, b.URL) {
			b.entryID = entryID
			b.URL = u
			b.Scroll = scroll
			b.clampScroll()
			b.Focus = -1
			b.Hover = -1
			b.status = ""
			b.prev = nil
			return
		}
	}
	c := b.Cache.Get(entryID)
	if c != nil {
		b.applyCachedPage(entryID, c, scroll)
		return
	}
	b.suspendCurrentPage()
	b.NextScroll = scroll
	b.NextScrollSet = true
	b.navigateRequestEntry(http.MethodGet, rawURL, nil, "", entryID, false, false)
}

// emitHistoryCommit reports a completed load to the front end list and any
// headless observer. The engine never mutates the ordered list itself.
func (b *Browser) emitHistoryCommit(c protocol.HistoryCommit) {
	if b.sessionOut != nil {
		_ = b.sessionOut.Send(protocol.Event{Body: c})
	}
	if b.historyObserver != nil {
		b.historyObserver(c)
	}
}

func (b *Browser) reload() {
	if b.URL == nil {
		return
	}
	method := b.LastMethod
	if method == "" {
		method = http.MethodGet
	}
	b.navigateRequest(method, b.URL.String(), b.LastBody, b.LastContentType, false)
}

// dropCachedPages clears the back/forward page cache. It is used when a
// session-wide setting changes, because restoring a page built under the old
// setting would be inconsistent.
func (b *Browser) dropCachedPages() { b.Cache.DropAll() }
