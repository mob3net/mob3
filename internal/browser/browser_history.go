package browser

import (
	"image"

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

// applyCachedPage restores a page kept by the back/forward cache.
func (b *Browser) applyCachedPage(entryID uint64, c *cachedPage, scroll int) {
	b.Session.ApplyCachedPage(entryID, c, scroll, b)
}

// restoreEntry returns to a front-end history entry.
func (b *Browser) restoreEntry(entryID uint64, rawURL string, scroll int) {
	b.Session.RestoreEntry(entryID, rawURL, scroll, b)
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
