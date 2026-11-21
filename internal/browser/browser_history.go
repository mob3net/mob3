package browser

import (
	"image"
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

// defaultHistoryCacheBytes is the estimated total size the back/forward cache may
// keep by default. It is a soft accounting budget, not an allocation: pages are
// weighed when they are suspended and dropped once the total would exceed it.
const defaultHistoryCacheBytes = 128 << 20

// Weight estimates for a cached page. They are deliberately coarse: the cache
// only needs to compare pages, and decoded images plus a live JS runtime
// dominate the real cost. A DOM node with its slices, attribute strings and
// presentation style measures about 200 bytes in our own heap measurements,
// and the JavaScript runtime keeps one wrapper per wrapped node.
const (
	nodeWeightBase  = 200
	nodeAttrWeight  = 48
	ruleWeightBase  = 384
	jsRuntimeWeight = 4 << 20
	jsNodeWeight    = 128
)

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
	// bytes is the estimated retained size used to keep the cache within its
	// budget. It is computed once when the page is suspended.
	bytes int64
}

// pageWeight estimates the retained bytes of a page, its scripts, its decoded
// images, its DOM and the JavaScript runtime that keeps it alive.
func pageWeight(page *Page, js *jsRuntime, body []byte, readerRoot *Node) int64 {
	if page == nil || page.Root == nil {
		return 0
	}
	w := int64(len(page.RawHTML)) + int64(len(body))
	for _, s := range page.Scripts {
		w += int64(len(s.Code))
	}
	w += int64(len(page.Author)) * ruleWeightBase
	w += domWeight(page.Root)
	if readerRoot != nil && readerRoot != page.Root {
		w += domWeight(readerRoot)
	}
	if js != nil {
		w += jsRuntimeWeight + int64(len(js.nodes))*jsNodeWeight
		w += int64(len(js.templates)) * nodeWeightBase
		w += int64(len(js.datasetObjs)) * jsNodeWeight
		w += int64(js.modules.sourceBytes())
	}
	return w
}

// domWeight estimates a subtree's retained bytes: nodes, their attribute
// strings, text, editable values and decoded images.
func domWeight(root *Node) int64 {
	if root == nil {
		return 0
	}
	w := int64(0)
	var walk func(*Node)
	walk = func(n *Node) {
		w += nodeWeightBase
		for i := range n.Attrs {
			w += int64(len(n.Attrs[i].Key)+len(n.Attrs[i].Value)) + nodeAttrWeight
		}
		w += int64(len(n.Text) + len(n.Value) + len(n.ImgSrc))
		if n.Img != nil {
			w += imageWeight(n.Img)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return w
}

// imageWeight estimates the bytes a decoded image keeps resident. Most decoders
// produce 4 bytes per pixel; the common compact formats are cheaper.
func imageWeight(img image.Image) int64 {
	if img == nil {
		return 0
	}
	b := img.Bounds()
	px := int64(b.Dx()) * int64(b.Dy())
	if px <= 0 {
		return 0
	}
	switch img.(type) {
	case *image.Paletted, *image.Gray:
		return px
	case *image.Gray16:
		return px * 2
	case *image.YCbCr:
		// 4:2:0 keeps one luma byte and half a chroma sample per pixel.
		return px + px/2
	}
	return px * 4
}

// historyCachePageCap is the largest single page the back/forward cache will keep.
// Half the budget leaves room for the page you came from while stopping one
// heavy page from pinning the whole allowance.
func (b *Browser) historyCachePageCap() int64 {
	if b.historyCacheBytes <= 0 {
		return 0
	}
	return b.historyCacheBytes / 2
}

// setHistoryCacheLimitMB applies a new back/forward cache budget and trims the
// cache immediately. Zero disables page caching.
func (b *Browser) setHistoryCacheLimitMB(mb int) {
	if mb < 0 {
		mb = 0
	}
	b.historyCacheBytes = int64(mb) << 20
	b.histMu.Lock()
	defer b.histMu.Unlock()
	b.trimHistoryCacheLocked()
}

// noteEvictedPage asks the event loop for one scavenge when the evicted page
// held a large part of the cache budget, so its memory returns to the OS now
// instead of waiting for the background scavenger. Small evictions do not
// justify a stop-the-world pass.
func (b *Browser) noteEvictedPage(size int64) {
	if size <= 0 {
		return
	}
	if b.historyCacheBytes > 0 && size < b.historyCacheBytes/4 {
		return
	}
	b.scavengePending.Store(true)
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
	weight := pageWeight(b.page, b.js, b.lastBody, b.readerRoot)
	if weight > b.historyCachePageCap() {
		// A page heavier than the single-page allowance would pin the whole
		// cache, so leave it out and let Back reload it instead.
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
		bytes:             weight,
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
	b.trimHistoryCacheLocked()
	return true
}

// trimHistoryCacheLocked drops cached pages until the cache is within its page
// count and estimated byte budget. The least recently cached pages go first;
// the newest entry survives even when the budget is tiny so Back can restore
// one step without a load. The caller holds histMu.
func (b *Browser) trimHistoryCacheLocked() {
	type slot struct {
		idx   int
		at    time.Time
		bytes int64
	}
	var slots []slot
	total := int64(0)
	for i := range b.history {
		if c := b.history[i].cached; c != nil {
			slots = append(slots, slot{idx: i, at: c.at, bytes: c.bytes})
			total += c.bytes
		}
	}
	if b.historyCacheBytes <= 0 {
		for _, s := range slots {
			b.history[s.idx].cached = nil
		}
		return
	}
	evictOldest := func() bool {
		if len(slots) == 0 {
			return false
		}
		oldest := 0
		for i := 1; i < len(slots); i++ {
			if slots[i].at.Before(slots[oldest].at) {
				oldest = i
			}
		}
		total -= slots[oldest].bytes
		b.noteEvictedPage(slots[oldest].bytes)
		b.history[slots[oldest].idx].cached = nil
		slots = append(slots[:oldest], slots[oldest+1:]...)
		return true
	}
	for len(slots) > maxCachedPages {
		evictOldest()
	}
	for total > b.historyCacheBytes && len(slots) > 1 {
		evictOldest()
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
		} else if c.page.PDF != nil {
			readerFailed = true
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
