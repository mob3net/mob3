package browser

import (
	"image"
	"net/url"

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

// historyCachePageCap is the largest single page the back/forward cache keeps.
func (b *Browser) historyCachePageCap() int64 { return b.Session.HistoryCachePageCap() }

// setHistoryCacheLimitMB applies a new back/forward cache budget.
func (b *Browser) setHistoryCacheLimitMB(mb int) { b.Session.SetHistoryCacheLimitMB(mb) }

// noteEvictedPage asks the event loop for one scavenge after a large eviction.
func (b *Browser) noteEvictedPage(size, limit int64) { b.Session.NoteEvictedPage(size, limit) }

func metaRefreshTarget(page *Page) string { return engine.MetaRefreshTarget(page) }

// suspendCurrentPage keeps the applied page for back/forward.
func (b *Browser) suspendCurrentPage() { b.Session.SuspendCurrentPage() }

// cacheCurrentPage stores the applied page under the current entry ID.
func (b *Browser) cacheCurrentPage() bool { return b.Session.CacheCurrentPage() }

// dropCachedEntryUnless removes a cached page whose URL changed.
func (b *Browser) dropCachedEntryUnless(id uint64, u string) {
	b.Session.DropCachedEntryUnless(id, u)
}

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
	b.EntryID = entryID
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
			b.EntryID = entryID
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
	b.navigateRequestEntry(protocol.MethodGet, rawURL, nil, "", entryID, false, false)
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
		method = protocol.MethodGet
	}
	b.navigateRequest(method, b.URL.String(), b.LastBody, b.LastContentType, false)
}

// dropCachedPages clears the back/forward page cache. It is used when a
// session-wide setting changes, because restoring a page built under the old
// setting would be inconsistent.
func (b *Browser) dropCachedPages() { b.Session.DropCachedPages() }
