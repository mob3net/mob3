package browser

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// jsRedrawInterval is the minimum gap between timer-driven rebuilds. The
	// previous rebuild cost is added so a slow page cannot spend all its time
	// re-laying out from timers.
	jsRedrawInterval = 250 * time.Millisecond
	// Loading progress eases toward 90% while a page loads, then completes and
	// lingers for a few ticks before the URL row returns to its normal color.
	progressTickInterval = 80 * time.Millisecond
	progressLingerTicks  = 4

	layoutTruncatedStatus = "Page truncated (layout limit)"
	moduleFailedStatus    = "Module script failed — see ! console"
	maxClipboardBytes     = 100 * 1024
)

// Browser holds navigation and view state.
type Browser struct {
	loader *Loader
	url    *url.URL
	page   *Page

	width, height int
	cellW, cellH  int
	layout        *Layout
	scroll        int
	// pdfStarts holds the layout row where each PDF page begins, so the footer
	// can name the page under the viewport.
	pdfStarts    []int
	focus        int
	hover        int
	hoverNode    *Node
	pageHasHover bool
	// hoverRebuildPending coalesces the style pass for pointer hover changes so
	// moving across a large page cannot rebuild the whole document per cell.
	hoverRebuildPending bool
	hoverRebuildAt      time.Time
	jsErr               bool
	// moduleErr records that an ES module script failed to load, link or
	// evaluate, so the footer can point at the console.
	moduleErr bool
	status    string

	// Reader mode renders an extracted article document instead of the page.
	reader            bool
	readerRoot        *Node
	readerSavedScroll int

	// Dark mode remaps the rendered page palette.
	dark bool

	// History and bookmarks.
	history []historyEntry
	histIdx int
	histMu  sync.RWMutex
	// historyCacheBytes bounds the estimated size of the back/forward page cache.
	// Zero disables page caching.
	historyCacheBytes int64
	// memLimitBytes is the process soft heap limit in bytes; zero leaves the
	// runtime default in place.
	memLimitBytes int64
	// scavengePending asks the event loop for one debug.FreeOSMemory call after a
	// heavy cached page was evicted, so its memory returns to the OS at once.
	scavengePending atomic.Bool
	// nextScroll is consumed by the next navigation started from history.
	nextScroll    int
	nextScrollSet bool
	// autoNavigations counts consecutive scripted/meta redirects so a
	// self-redirect loop can be stopped. loopStopped reports that the cap was
	// hit while applying the current page.
	autoNavigations int
	loopStopped     bool
	home            string
	cliConfig       map[string]string
	// configNotice shows why an edited about:config value was rejected, such
	// as an unreadable -certs bundle.
	configNotice string

	prompt          bool
	promptText      string
	promptCursor    int
	promptSelectAll bool
	// promptVim marks a prompt opened with `:`, where `q` quits.
	promptVim       bool
	promptHist      consoleHistory
	skipDraw        bool
	noDraw          bool
	clipboardWriter io.Writer

	color   bool
	prev    []string
	gfx     *gfxRenderer
	bigText bool
	// zoom is the layout zoom factor: 1 is normal. It changes the CSS pixels
	// per terminal cell for everything except text metrics.
	zoom float64
	// scaledLayout records whether the current layout was built with kitty
	// scaled text, so draw can rebuild when an overlay toggles it.
	scaledLayout bool
	// bigTextBlocked disables kitty scaled text for a page whose scaled runs
	// overlap neighbouring columns.
	bigTextBlocked bool

	// Graphics modes available to the i key, in cycle order, and the current
	// one. The list starts with native inline graphics when available.
	graphicsModes []GraphicsMode
	graphicsMode  GraphicsMode

	// hideFixed suppresses position:fixed boxes for the current page.
	hideFixed bool
	// hideBackgrounds paints CSS background images as a neutral placeholder
	// instead of their pixels; the images stay loaded and laid out.
	hideBackgrounds bool

	// lastNativeImages tracks graphics-mode transitions (hints or a modal
	// opening/closing) so the next frame is fully repainted and no stale
	// terminal image survives.
	lastNativeImages bool

	noJS bool
	// noPDF disables the built-in PDF text viewer, restoring the download
	// prompt for PDF responses.
	noPDF bool
	// pendingStatus replaces the computed load status once, so a JavaScript
	// toggle can report its new state instead of clearing the footer.
	pendingStatus string
	noBigText     bool
	jsMemLimit    int64
	layoutBudget  LayoutBudget
	js            *jsRuntime

	loadCh chan loadResult
	// interactive marks the terminal UI, where a background dynamic import can
	// wait for the event loop instead of blocking it.
	interactive bool
	// importCh delivers completed background dynamic imports to the event loop.
	importCh chan jsImportTask
	// imageCh delivers background image loads for the current page.
	imageCh        chan imageBatch
	deferredImages bool
	// imagesPaused stops timer-driven rebuilds from restarting a background
	// image load after the user pressed Esc.
	imagesPaused bool
	imageCancel  context.CancelFunc
	// imageTotal and imageDone drive the loading bar and footer count while a
	// deferred image batch is in flight.
	imageTotal int
	imageDone  int
	// pendingImageRow is the topmost laid-out row of images whose decoded data
	// arrived while they sat below the viewport. Their layout is deferred until
	// the viewport approaches that row or the end of the document, so a batch
	// far down the page cannot reflow and repaint the visible frame. Zero means
	// nothing is pending.
	pendingImageRow int
	loadSeq         int
	cancelLoad      context.CancelFunc
	loading         bool
	progress        float64
	progressHold    int
	loadFn          func(context.Context, string, string, []byte, string) (*Page, error)

	lastMethod      string
	lastBody        []byte
	lastContentType string
	// pendingDownloadName carries a download attribute from the clicked link
	// into the next load, so a non-renderable response keeps that filename.
	pendingDownloadName string

	lastRebuild time.Time
	lastBuild   time.Duration

	inputFocus *Node
	// inputSelectAll marks the focused field's whole value as selected by
	// Ctrl-A. Typing or deleting replaces the value; caret moves cancel it.
	inputSelectAll bool
	controlFocus   *Node
	controlOption  *Node
	consoleOpen    bool
	consoleScroll  int
	reqScroll      int
	storageScroll  int
	htmlScroll     int
	toolScroll     int
	toolLog        []string
	consolePane    int // 0 = JS logs, 1 = requests, 2 = storage, 3 = HTML, 4 = tools
	consoleText    string
	consoleCursor  int
	consoleEdit    bool // the input row inside the logs or tools section has focus
	consoleHist    consoleHistory
	toolHist       consoleHistory

	findActive  bool
	findText    string
	findCursor  int
	findScope   int
	findMatches []findMatch
	findIdx     int

	// Vimium-style link hints for the links visible in the viewport.
	linkHints   bool
	hintItems   []linkHint
	hintTargets []hintTarget
	hintPrefix  string
	// hintAsk is set by F; choosing a hint then waits for confirmation.
	hintAsk         bool
	hintConfirm     bool
	hintConfirmLink int

	htmlPage  *Page
	htmlWidth int
	htmlCache []string
	jsLog     []string

	keys   <-chan string
	dialog *jsDialog
	// download holds the modal curl hint shown for download links and
	// non-renderable responses.
	download *downloadDialog
	// notice holds a small informational modal, such as the unsupported
	// file-upload message.
	notice *noticeDialog
	// selectMenu is the open popup listing every option of a single select.
	selectMenu *selectMenuDialog
	// selectHits are the clickable option rows of the open select popup.
	selectHits []selectHit
	// viewMenu is the open window listing page view switches.
	viewMenu *viewMenuDialog
	// viewMenuHits are the clickable rows of the open view menu.
	viewMenuHits []viewMenuHit
	// zoomMenu is the open window listing layout zoom presets.
	zoomMenu *zoomMenuDialog
	// zoomMenuHits are the clickable rows of the open zoom menu.
	zoomMenuHits []zoomMenuHit
	// modalClose is the close-button hit region of the modal drawn last.
	modalClose Rect
	// modalBox is the full modal rectangle, used to close on backdrop clicks.
	modalBox Rect

	lastScroll  int
	usage       procUsage
	scrollBar   *scrollBarInfo
	scrollDrag  bool
	quitPending bool

	headerHits     []headerHit
	footerHits     []headerHit
	consoleTabHits []consoleTabHit

	selAnchor cellPos
	selCursor cellPos
	selecting bool
}

type loadResult struct {
	seq         int
	page        *Page
	err         error
	u           *url.URL
	push        bool
	method      string
	body        []byte
	contentType string
	// restoreScroll, when set, overrides the usual start-at-top behaviour.
	// It is used by history back/forward to return to the position the user
	// last had on that entry.
	restoreScroll    int
	hasRestoreScroll bool
	// downloadName is the anchor's download attribute, so a non-renderable
	// response still offers the filename the page asked for.
	downloadName string
}

// NewBrowser creates a browser with a viewport size.
func NewBrowser(w, h int) *Browser {
	if w < 20 {
		w = 80
	}
	if h < 5 {
		h = 24
	}
	b := &Browser{
		loader: NewLoader(), width: w, height: h, cellW: 8, cellH: 16, focus: -1, hover: -1, histIdx: -1, zoom: 1,
		jsMemLimit:        defaultJSMemoryLimit,
		layoutBudget:      DefaultLayoutBudget,
		historyCacheBytes: defaultHistoryCacheBytes,
		loadCh:            make(chan loadResult, 1),
		importCh:          make(chan jsImportTask, 16),
		imageCh:           make(chan imageBatch, 1),
		home:              "about:home",
	}
	b.setLoader(b.loader)
	setLayoutZoom(b.zoom)
	return b
}

// setLoader installs a loader and wires it to this browser's session history so
// about:history reflects the current browser even when the CLI replaces the
// default loader.
func (b *Browser) setLoader(l *Loader) {
	b.loader = l
	if l != nil {
		l.historyFn = b.historySnapshot
		if b.width > 0 {
			l.imageViewportPx = cssWidthPx(b.width)
		}
		b.applyImageCap()
	}
}

// imageCapPx returns the largest image box the terminal can paint, in CSS
// pixels. The floors keep a small terminal from hardening every image it
// loads; larger terminals get proportionally more pixels.
func (b *Browser) imageCapPx() (int, int) {
	cw, ch := b.cellW, b.cellH
	if cw <= 0 {
		cw = 8
	}
	if ch <= 0 {
		ch = 16
	}
	w, h := b.width*cw, b.height*ch
	if w < 1024 {
		w = 1024
	}
	if h < 512 {
		h = 512
	}
	return w, h
}

// applyImageCap keeps the loader's decode cap in step with the viewport.
func (b *Browser) applyImageCap() {
	if b.loader == nil {
		return
	}
	b.loader.maxImageW, b.loader.maxImageH = b.imageCapPx()
}

func (b *Browser) historySnapshot() ([]string, int) {
	b.histMu.RLock()
	defer b.histMu.RUnlock()
	urls := make([]string, len(b.history))
	for i, entry := range b.history {
		urls[i] = entry.URL
	}
	return urls, b.histIdx
}

// hasBookmarks reports whether a bookmark file was supplied to the browser.
func (b *Browser) hasBookmarks() bool {
	return b.loader != nil && (b.loader.bookmarksSet || len(b.loader.bookmarks) > 0)
}

// searchTemplate returns the configured search URL template.
func (b *Browser) searchTemplate() string {
	if b.loader != nil {
		return b.loader.searchURL
	}
	return DefaultSearchURL
}

// headerRows returns the number of rows used by the top bar.
func (b *Browser) headerRows() int {
	if b.height < 3 {
		return 0
	}
	if b.height < 5 {
		return 1
	}
	return 2
}

// viewport returns the first and last content row (exclusive end).
func (b *Browser) viewport() (int, int) {
	if b.height < 3 {
		return 0, b.height
	}
	return b.headerRows(), b.height - 1
}

func (b *Browser) viewportHeight() int {
	top, bottom := b.viewport()
	if bottom < top {
		return 1
	}
	return bottom - top
}

// Navigate starts loading input in the background so the UI and Esc stay
// responsive. Results are applied by finishLoad from the main loop.
func (b *Browser) Navigate(input string, push bool) {
	b.navigateRequest(http.MethodGet, input, nil, "", push)
}

func (b *Browser) navigateRequest(method, input string, body []byte, contentType string, push bool) {
	b.navigateRequestForce(method, input, body, contentType, push, false)
}

// navigateRequestForce is navigateRequest with an optional one-shot block
// bypass. It backs the "Try anyway" link (about:retry) on the blocked page.
func (b *Browser) navigateRequestForce(method, input string, body []byte, contentType string, push bool, forceBlocks bool) {
	// A download-attribute name belongs to this load only, so consume it even
	// when the navigation is rejected before a request is made.
	downloadName := b.pendingDownloadName
	b.pendingDownloadName = ""
	if strings.EqualFold(strings.TrimSpace(input), "about:retry") {
		if b.url == nil {
			b.status = "Nothing to retry"
			b.draw()
			return
		}
		input = b.url.String()
		forceBlocks = true
		push = false
		b.nextScrollSet = false
	}
	u, err := ResolveInput(input, b.url, b.searchTemplate())
	if err != nil {
		b.status = "Invalid URL: " + err.Error()
		b.draw()
		return
	}
	if method == "" {
		method = http.MethodGet
	}
	restoreScroll, hasRestoreScroll := b.nextScroll, b.nextScrollSet
	b.nextScrollSet = false
	b.recordHistoryScroll()
	if isAboutConfigURL(u) && b.reader {
		b.status = "Config is not available in reader mode — press R to turn it off"
		b.draw()
		return
	}
	if isAboutConfigURL(u) {
		b.clearInputFocus()
		b.suspendCurrentPage()
		b.cancelLoadNow()
		if b.loader != nil {
			b.loader.beginPage()
		}
		b.loadSeq++
		b.applyConfig(u.Query())
		page := b.configPage(u, len(u.Query()) > 0)
		b.finishLoad(loadResult{
			seq: b.loadSeq, page: page, u: u, push: push, method: http.MethodGet,
			restoreScroll: restoreScroll, hasRestoreScroll: hasRestoreScroll,
		})
		return
	}
	if u.Scheme == "file" && b.url != nil {
		switch b.url.Scheme {
		case "http", "https", "data":
			b.status = "Blocked local file navigation from a remote page"
			b.draw()
			return
		}
	}
	b.clearInputFocus()
	b.suspendCurrentPage()
	b.cancelLoadNow()
	b.cancelDeferredImageLoad()
	b.imagesPaused = false
	b.applyImageCap()
	if b.loader != nil {
		b.loader.beginPage()
	}
	b.loadSeq++
	seq := b.loadSeq
	ctx, cancel := context.WithCancel(context.Background())
	b.cancelLoad = cancel
	b.loading = true
	b.startProgress()
	b.status = "Loading " + u.String() + " … (Esc stops)"
	b.clearSelection()

	target := u.String()
	load := b.loadFn
	if load == nil {
		loader := b.loader
		load = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
			if forceBlocks {
				return loader.LoadRequestBypass(ctx, method, target, body, contentType, nil)
			}
			return loader.LoadRequest(ctx, method, target, body, contentType, nil)
		}
	}
	go func() {
		page, loadErr := load(ctx, method, target, body, contentType)
		select {
		case b.loadCh <- loadResult{
			seq: seq, page: page, err: loadErr, u: u, push: push,
			method: method, body: body, contentType: contentType,
			restoreScroll: restoreScroll, hasRestoreScroll: hasRestoreScroll,
			downloadName: downloadName,
		}:
		case <-ctx.Done():
		}
	}()
}

func (b *Browser) cancelLoadNow() {
	if b.cancelLoad != nil {
		b.cancelLoad()
		b.cancelLoad = nil
	}
	b.loading = false
	b.clearProgress()
}

// startProgress resets the URL-row loading indicator for a new navigation.
func (b *Browser) startProgress() {
	b.progress = 0.05
	b.progressHold = 0
}

// finishProgress completes the indicator and leaves it visible briefly so the
// end of a fast load is still perceptible.
func (b *Browser) finishProgress() {
	if b.progress <= 0 {
		return
	}
	b.progress = 1
	b.progressHold = progressLingerTicks
}

func (b *Browser) clearProgress() {
	b.progress = 0
	b.progressHold = 0
}

// progressActive reports whether the event loop should keep ticking the
// loading indicator.
func (b *Browser) progressActive() bool {
	return b.loading || b.progressHold > 0
}

// tickProgress advances the indicator. It reports whether the bar changed
// enough to justify a repaint.
func (b *Browser) tickProgress() bool {
	if b.loading {
		b.progress += (0.9 - b.progress) * 0.08
		if b.progress > 0.9 {
			b.progress = 0.9
		}
		return true
	}
	if b.progressHold > 0 {
		b.progressHold--
		if b.progressHold == 0 {
			b.progress = 0
			return true
		}
	}
	return false
}

// stopLoading aborts the in-flight page load and ignores its late result.
func (b *Browser) stopLoading() {
	if !b.loading {
		if b.deferredImages {
			b.imagesPaused = true
			b.cancelDeferredImageLoad()
			b.finishProgress()
			b.status = "Stopped loading images"
		}
		return
	}
	b.autoNavigations = 0
	b.loopStopped = false
	b.loadSeq++
	b.cancelLoadNow()
	b.cancelDeferredImageLoad()
	b.status = "Stopped loading"
}

// finishLoad applies a completed load. Stale results from canceled navigations
// are ignored.
func (b *Browser) finishLoad(res loadResult) {
	if res.seq != b.loadSeq {
		return
	}
	note := b.pendingStatus
	b.pendingStatus = ""
	b.clearLinkHints()
	b.clearHintConfirm()
	b.selectMenu = nil
	b.selectHits = nil
	b.viewMenu = nil
	b.viewMenuHits = nil
	b.zoomMenu = nil
	b.zoomMenuHits = nil
	b.quitPending = false
	b.jsErr = false
	b.moduleErr = false
	b.cancelLoad = nil
	b.loading = false
	if res.err != nil {
		b.autoNavigations = 0
		b.loopStopped = false
		b.status = "Error: " + res.err.Error()
		b.finishProgress()
		if b.page == nil {
			doc := loadErrorPageHTML(res.err.Error())
			root := ParseHTML(doc)
			page := &Page{URL: res.u, Base: res.u, Root: root, RawHTML: doc, Title: "Error", Status: 0}
			applyPageStyles(root, page)
			b.page = page
			b.url = res.u
			b.rebuild()
		}
		return
	}
	page := res.page
	if page != nil && page.Download && res.method == http.MethodGet && page.URL != nil {
		name := page.DownloadName
		if res.downloadName != "" {
			name = res.downloadName
		}
		b.showDownload(page.URL.String(), name)
		b.status = "Download link — y copies the curl command"
		b.finishProgress()
		b.prev = nil
		return
	}
	b.page = page
	b.url = page.URL
	b.imagesPaused = false
	if b.url != nil && res.u != nil && res.u.Fragment != "" && b.url.Fragment == "" {
		u := *b.url
		u.Fragment = res.u.Fragment
		b.url = &u
	}
	b.lastMethod = res.method
	b.lastBody = res.body
	b.lastContentType = res.contentType
	b.layout = nil
	b.bigTextBlocked = false
	// <noscript> is not active when scripting is enabled. Remove it before
	// evaluating meta refresh, otherwise no-JS bypass pages redirect us to
	// their "enable JavaScript" endpoint.
	if !b.noJS {
		removeNoscript(page.Root)
	}
	loopStopped := false
	if target := metaRefreshTarget(page); target != "" && target != page.URL.String() {
		if b.autoNavigations >= maxAutoNavigations {
			loopStopped = true
		} else {
			// Record this document first, then let the target replace it. A
			// meta refresh is a client redirect, so browsers coalesce it into
			// one history entry; Back should skip the redirecting page rather
			// than land on it and be forwarded again.
			b.prepareHistory(res.push)
			b.autoNavigations++
			b.Navigate(target, false)
			return
		}
	}
	b.resetConsole()
	b.startJS(page)
	if b.js != nil && (b.js.pendingNav != "" || b.js.pendingForm != nil) {
		b.prepareHistory(res.push)
	}
	// A navigation queued by the page's own load-time scripts is also a
	// client redirect: replace the entry recorded above instead of adding one.
	if b.applyPendingJS(true) {
		return
	}
	if b.loopStopped {
		b.loopStopped = false
		loopStopped = true
	}
	b.readerRoot = nil
	b.readerSavedScroll = 0
	readerFailed := false
	if b.reader {
		// PDF pages are already a clean text view; reader mode stays enabled
		// for the next page but keeps the full document.
		if page.PDF != nil {
			readerFailed = true
		} else if root, ok := extractReaderPage(page.Root, page.Title); ok {
			b.readerRoot = root
		} else {
			readerFailed = true
		}
	}
	if !b.reader && (b.loader == nil || !b.loader.deferImages) {
		b.refreshImages()
	}
	b.rebuild()
	b.scroll = 0
	b.focus = -1
	b.clearControlFocus()
	b.status = b.pageLoadStatus(page, readerFailed)
	switch {
	case loopStopped:
		b.status = "Redirect loop stopped"
	case note != "" && b.status == "":
		b.status = note
	}
	if frag := res.u.Fragment; frag != "" {
		b.scrollToAnchor(frag)
	}
	if res.hasRestoreScroll {
		b.scroll = res.restoreScroll
		b.clampScroll()
	}
	if b.url != nil {
		if res.push {
			b.pushHistory(b.url.String(), b.scroll)
		} else {
			b.replaceHistoryEntry(b.url.String(), b.scroll)
		}
	}
	b.autoNavigations = 0
	b.prev = nil
	if b.deferredImages {
		b.progress = 0.9
		b.progressHold = 0
	} else {
		b.finishProgress()
	}
}

// pageLoadStatus returns the status line for a freshly applied page, or an
// empty string when the page loaded cleanly.
func (b *Browser) pageLoadStatus(page *Page, readerFailed bool) string {
	switch {
	case page.Blocked:
		return "Site blocked — browser check not supported"
	case page.Status >= 400:
		return fmt.Sprintf("HTTP %d %s", page.Status, http.StatusText(page.Status))
	case b.moduleErr:
		return moduleFailedStatus
	case b.layout != nil && b.layout.Truncated:
		return layoutTruncatedStatus
	case readerFailed:
		return "Reader mode unavailable"
	}
	return ""
}

// consumePendingJS applies a queued location.href/assign navigation or
// programmatic form.submit() after the current script or callback has finished.
func (b *Browser) consumePendingJS() bool { return b.applyPendingJS(false) }

// applyPendingJS applies a navigation queued by page JavaScript. duringLoad
// marks the queue drain that happens while the page is still being applied:
// such a redirect replaces the redirecting entry, matching browser behaviour,
// while a click or timer navigation after the page settled still pushes.
func (b *Browser) applyPendingJS(duringLoad bool) bool {
	if b.js == nil {
		return false
	}
	if pf := b.js.pendingForm; pf != nil {
		b.js.pendingForm = nil
		if pf.method == http.MethodPost {
			b.navigateRequest(pf.method, pf.target, pf.body, pf.contentType, true)
		} else {
			b.Navigate(pf.target, true)
		}
		return true
	}
	if b.js.pendingNav == "" {
		return false
	}
	target := b.js.pendingNav
	replace := b.js.pendingReplace
	b.js.pendingNav = ""
	b.js.pendingReplace = false
	if b.autoNavigations >= maxAutoNavigations {
		b.loopStopped = true
		return false
	}
	b.autoNavigations++
	if replace || duringLoad {
		b.Navigate(target, false)
	} else {
		b.Navigate(target, true)
	}
	return true
}

func (b *Browser) rebuild() {
	if b.page == nil {
		return
	}
	// Frameworks inject their component CSS as <style> elements after load;
	// collect new or changed ones before this layout pass.
	if b.js != nil && !b.reader {
		b.page.collectDynamicStyles()
	}
	// The new layout includes every image decoded so far, so nothing is left
	// waiting for the viewport.
	b.pendingImageRow = 0
	// Scripts may set image sources on any interaction that triggers a rebuild.
	// Scanning here is cheap when nothing changed.
	if b.js != nil && !b.reader && (b.loader == nil || !b.loader.deferImages) {
		// Deferred image loading is scheduled after layout, where the image
		// positions are known.
		b.refreshImages()
	}
	b.hoverRebuildPending = false
	// Timer-driven DOM changes (for example marquee animations) rebuild the
	// page constantly. Keep an active hint session alive and recompute its
	// hints against the new layout instead of dismissing it.
	wasHints := b.linkHints
	hintAsk := b.hintAsk
	hintPrefix := b.hintPrefix
	wasConfirm := b.hintConfirm
	var confirmNode *Node
	confirmHref := ""
	if wasConfirm && b.layout != nil && b.hintConfirmLink >= 0 && b.hintConfirmLink < len(b.layout.Links) {
		li := b.layout.Links[b.hintConfirmLink]
		confirmNode = li.Node
		confirmHref = li.Href
	}
	b.clearLinkHints()
	b.clearHintConfirm()
	start := time.Now()
	b.bigText = b.scaledTextWanted()
	root, author := b.page.Root, b.page.Author
	if b.reader && b.readerRoot != nil {
		root, author = b.readerRoot, readerRules
	}
	deadline := time.Time{}
	if b.layoutBudget.Time > 0 {
		deadline = start.Add(b.layoutBudget.Time)
	}
	layoutWidth := b.width
	// Interactive pages reserve the rightmost column for the scrollbar, so
	// text and images wrap before it instead of painting underneath it.
	if !b.noDraw && layoutWidth >= 2 {
		layoutWidth--
	}
	b.bigText = b.scaledTextWanted()
	imagesOff := b.loader != nil && b.loader.noImages
	b.layout = BuildLayoutBudgetDeadlineImages(root, author, layoutWidth, b.viewportHeight(), b.bigText, b.dark, b.hideFixed, imagesOff, deadline, b.layoutBudget)
	b.layout.BackgroundsOff = b.hideBackgrounds
	b.updatePDFPageStarts()
	if b.bigText && layoutTextOverlaps(b.layout) {
		// Scaled kitty glyphs are wider than their base font. On dense
		// multi-column pages that pushes text from one column over another, so
		// this page falls back to normal-size text for the rest of its session.
		b.bigTextBlocked = true
		b.bigText = false
		start = time.Now()
		deadline = time.Time{}
		if b.layoutBudget.Time > 0 {
			deadline = start.Add(b.layoutBudget.Time)
		}
		b.layout = BuildLayoutBudgetDeadlineImages(root, author, layoutWidth, b.viewportHeight(), false, b.dark, b.hideFixed, imagesOff, deadline, b.layoutBudget)
		b.layout.BackgroundsOff = b.hideBackgrounds
		b.updatePDFPageStarts()
	}
	b.scaledLayout = b.bigText
	b.pageHasHover = rulesUseHover(author)
	b.lastBuild = time.Since(start)
	b.lastRebuild = time.Now()
	if b.layout != nil {
		b.layout.SetFixedScroll(b.scroll)
	}
	b.reindexFind()
	if b.layout.Truncated {
		if b.status == "" {
			b.status = layoutTruncatedStatus
		}
	} else if b.status == layoutTruncatedStatus {
		b.status = ""
	}
	b.hover = -1
	b.clampScroll()
	if wasHints {
		b.reopenLinkHints(hintAsk, hintPrefix)
	} else if wasConfirm {
		if link := b.findHintLink(confirmNode, confirmHref); link >= 0 {
			b.beginHintConfirm(link)
		}
	}
	// Layout now knows where each image sits, so a deferred batch can be
	// ordered viewport-first.
	if b.loader != nil && b.loader.deferImages && !b.reader && !b.deferredImages {
		b.startDeferredImageLoad()
	}
}

// overlayOpen reports whether a modal window or popup is drawn over the page.
// Terminal-side graphics composite above the cell grid, so scaled text is
// disabled while one is open and the layout is rebuilt without it.
func (b *Browser) overlayOpen() bool {
	return b.dialog != nil || b.download != nil || b.notice != nil ||
		b.selectMenu != nil || b.viewMenu != nil || b.zoomMenu != nil
}

// scaledTextWanted reports whether the page should use kitty scaled text.
// Modals disable it because terminal-side scaled glyphs composite above the
// cell grid and would paint over the modal.
func (b *Browser) scaledTextWanted() bool {
	return b.gfx != nil && b.gfx.TextSizing() && !b.noBigText && !b.bigTextBlocked && !b.overlayOpen()
}

// toggleReader switches between the page and an extracted article view without
// touching history or the live page DOM.
func (b *Browser) toggleReader() {
	if !b.reader && isAboutConfigURL(b.url) {
		b.status = "Reader mode is unavailable on the config page"
		return
	}
	if b.page == nil || b.page.Root == nil {
		b.status = "Reader mode unavailable"
		return
	}
	if b.reader {
		b.reader = false
		b.readerRoot = nil
		b.rebuild()
		b.scroll = b.readerSavedScroll
		b.readerSavedScroll = 0
		b.clampScroll()
		b.status = "Reader mode off"
		return
	}
	if b.page.PDF != nil {
		b.status = "Reader mode unavailable"
		return
	}
	root, ok := extractReaderPage(b.page.Root, b.page.Title)
	if !ok {
		b.status = "Reader mode unavailable"
		return
	}
	b.readerSavedScroll = b.scroll
	b.reader = true
	b.readerRoot = root
	b.scroll = 0
	b.focus = -1
	b.clearSelection()
	b.rebuild()
	b.status = "Reader mode — R to exit"
}

func (b *Browser) toggleDark() {
	b.dark = !b.dark
	b.rebuild()
	b.status = "Dark mode on"
	if !b.dark {
		b.status = "Dark mode off"
	}
}

// zoomSteps are the layout zoom levels, smallest first.
var zoomSteps = []float64{0.25, 0.5, 0.67, 0.8, 1, 1.25, 1.5, 1.75, 2}

// zoomBy steps the layout zoom to the next preset. Zoom changes the CSS pixels
// per terminal cell, so fixed pixel sizes, images, spacing and kitty scaled
// headings grow or shrink while text keeps its normal cell size.
func (b *Browser) zoomBy(dir int) {
	if dir > 0 {
		for _, s := range zoomSteps {
			if s > b.zoom+1e-9 {
				b.applyZoom(s)
				return
			}
		}
		b.applyZoom(zoomSteps[len(zoomSteps)-1])
		return
	}
	for i := len(zoomSteps) - 1; i >= 0; i-- {
		if zoomSteps[i] < b.zoom-1e-9 {
			b.applyZoom(zoomSteps[i])
			return
		}
	}
	b.applyZoom(zoomSteps[0])
}

// resetZoom returns the layout zoom to 100%.
func (b *Browser) resetZoom() {
	b.applyZoom(1)
}

// applyZoom installs a layout zoom factor and rebuilds the page so media
// queries and every pixel-based length see the new cell ratio.
func (b *Browser) applyZoom(z float64) {
	if b.zoom != z {
		b.zoom = z
		setLayoutZoom(z)
		// A page whose scaled runs overlapped may be fine at another zoom, so
		// re-evaluate the fallback instead of keeping it for the session.
		b.bigTextBlocked = false
		b.prev = nil
		b.rebuild()
		if limit := b.maxScroll(); b.scroll > limit {
			b.scroll = limit
		}
		if b.scroll < 0 {
			b.scroll = 0
		}
	}
	b.status = fmt.Sprintf("Zoom %d%%", int(math.Round(z*100)))
}

// toggleJavaScript switches page scripting for the rest of the session. The
// current document is fetched again so its no-script or scripted variant is
// applied; the page's existing DOM is not reused.
func (b *Browser) toggleJavaScript() {
	b.noJS = !b.noJS
	if b.loader != nil {
		b.loader.noScripts = b.noJS
	}
	if b.noJS {
		b.pendingStatus = "JavaScript off — press J to re-enable"
	} else {
		b.pendingStatus = "JavaScript on"
	}
	if b.url == nil {
		b.status = b.pendingStatus
		b.pendingStatus = ""
		b.draw()
		return
	}
	b.reload()
	// Cached pages keep the previous mode's DOM and runtime, so drop them.
	b.dropCachedPages()
}

func (b *Browser) resize(w, h int) {
	if w < 10 {
		w = 10
	}
	if h < 3 {
		h = 3
	}
	if w == b.width && h == b.height {
		return
	}
	b.width, b.height = w, h
	b.applyImageCap()
	b.prev = nil
	b.clearSelection()
	b.rebuild()
}

func (b *Browser) maxScroll() int {
	if b.layout == nil {
		return 0
	}
	m := b.layout.Height - b.viewportHeight()
	if m < 0 {
		return 0
	}
	return m
}

func (b *Browser) clampScroll() {
	max := b.maxScroll()
	if b.scroll < 0 {
		b.scroll = 0
	}
	if b.scroll > max {
		b.scroll = max
	}
}

func (b *Browser) scrollBy(n int) {
	b.clearLinkHints()
	b.clearHintConfirm()
	b.hover = -1
	b.scroll += n
	b.clampScroll()
	switch {
	case b.findText != "":
		// Keep the confirmed find status visible while scrolling; only Esc
		// clears the search and its footer line.
		b.status = b.findStatusText()
	case !strings.HasPrefix(b.status, "HTTP ") && !strings.HasPrefix(b.status, "Error"):
		b.status = ""
	}
}

func (b *Browser) focusedLink() *LinkInfo {
	if b.layout == nil || b.focus < 0 || b.focus >= len(b.layout.Links) {
		return nil
	}
	return b.layout.Links[b.focus]
}

func (b *Browser) hoveredLink() *LinkInfo {
	if b.layout == nil || b.hover < 0 || b.hover >= len(b.layout.Links) {
		return nil
	}
	return b.layout.Links[b.hover]
}

// setPageHover updates the element chain used for CSS :hover and dispatches
// pointer enter/leave events. A style pass runs only when the page has hover
// rules or a handler mutated the DOM.
func (b *Browser) setPageHover(n *Node) {
	if n == b.hoverNode {
		return
	}
	old := b.hoverNode
	if old != nil {
		markHoverChain(old, false)
		if b.js != nil && b.liveNode(old) {
			b.js.fireNodeEvent(old, "mouseout")
			b.js.fireNodeEvent(old, "mouseleave")
		}
	}
	b.hoverNode = n
	dirty := false
	if n != nil {
		markHoverChain(n, true)
		if b.js != nil && b.liveNode(n) {
			b.js.fireNodeEvent(n, "mouseover")
			b.js.fireNodeEvent(n, "mouseenter")
		}
	}
	if b.js != nil {
		dirty = b.js.consumeDirty()
	}
	// Hover-driven style passes are delayed in interactive sessions so fast
	// pointer movement does not rebuild the page for every cell. Automation
	// and tests without an event loop need the result synchronously.
	if b.pageHasHover || dirty {
		if b.keys != nil {
			b.hoverRebuildPending = true
			b.hoverRebuildAt = time.Now().Add(hoverRebuildDelay)
		} else {
			b.rebuild()
		}
	}
}

// hoverRebuildDelay leaves enough time for the pointer to settle on a menu
// item while still feeling immediate.
const hoverRebuildDelay = 120 * time.Millisecond

func (b *Browser) flushHoverRebuild() bool {
	if !b.hoverRebuildPending {
		return false
	}
	b.hoverRebuildPending = false
	b.rebuild()
	return true
}

func markHoverChain(n *Node, on bool) {
	for p := n; p != nil; p = p.Parent {
		if p.Type == ElementNode {
			p.Hovered = on
		}
	}
}

// updateHover records the link under the pointer in content coordinates for
// the footer readout.
func (b *Browser) updateHover(x, y, top, bottom int) {
	idx := -1
	if y >= top && y < bottom {
		idx = b.linkAt(x, b.scroll+(y-top))
	}
	b.hover = idx
}

func (b *Browser) resolveHref(href string) string {
	if b.page == nil || b.page.Base == nil {
		return href
	}
	if u, err := b.page.Base.Parse(href); err == nil {
		return u.String()
	}
	return href
}

func (b *Browser) followFocused() {
	li := b.focusedLink()
	if li == nil {
		b.status = "No link selected (press Tab)"
		return
	}
	// Run the link's click handlers before navigating. Bare "#" links often
	// open dialogs or menus from JavaScript and cancel the default navigation.
	if b.js != nil && li.Node != nil && b.liveNode(li.Node) {
		prevented := b.js.dispatchClick(li.Node)
		if prevented && !(b.js.eventErr && linkFallbackOK(li.Href)) {
			if b.js.consumeDirty() {
				b.rebuild()
			}
			b.consumePendingJS()
			return
		}
	}
	if b.consumePendingJS() {
		return
	}
	b.followLink(li)
}

// linkFallbackOK reports whether a prevented link click should still navigate
// because its handler threw an error and left no replacement action.
func linkFallbackOK(href string) bool {
	low := strings.ToLower(strings.TrimSpace(href))
	if low == "" || strings.HasPrefix(low, "#") || strings.HasPrefix(low, "javascript:") ||
		strings.HasPrefix(low, "mailto:") || strings.HasPrefix(low, "tel:") {
		return false
	}
	return true
}

// isPDFDownloadLink reports whether a download link points at a PDF, either by
// its path suffix or by the filename in the download attribute. Only these are
// fetched: the response opens in the PDF viewer instead of the curl popup.
func isPDFDownloadLink(target, attr string) bool {
	if u, err := url.Parse(target); err == nil && strings.HasSuffix(strings.ToLower(u.Path), ".pdf") {
		return true
	}
	name := strings.TrimSpace(attr)
	return strings.HasSuffix(strings.ToLower(name), ".pdf")
}

func (b *Browser) followLink(li *LinkInfo) {
	href := li.Href
	low := strings.ToLower(strings.TrimSpace(href))
	if strings.HasPrefix(low, "javascript:") {
		b.status = "JavaScript links are not supported"
		return
	}
	if strings.HasPrefix(low, "mailto:") || strings.HasPrefix(low, "tel:") {
		b.status = "External protocol: " + href
		return
	}
	if li.Node != nil && li.Node.HasAttr("download") {
		target := href
		if b.page != nil && b.page.Base != nil {
			if u, err := b.page.Base.Parse(href); err == nil {
				target = u.String()
			}
		}
		if isPDFDownloadLink(target, li.Node.Attr("download")) {
			// A PDF is worth fetching because the viewer can show it; the
			// attribute also names the file if the response is not a PDF after
			// all.
			b.pendingDownloadName = li.Node.Attr("download")
			b.Navigate(target, true)
			return
		}
		// Everything else keeps browser semantics: "download" means save, so
		// show the curl popup without spending a request on a large binary.
		b.showDownload(target, li.Node.Attr("download"))
		return
	}
	if b.page != nil && b.page.Base != nil {
		if u, err := b.page.Base.Parse(href); err == nil {
			if b.url != nil && sameDocument(u, b.url) {
				if u.Fragment == "" {
					// A bare "#" link is a page-modal trigger on many sites.
					// In reader mode it would reload the page and jump to the
					// top, so warn instead of navigating.
					if b.reader {
						b.status = readerLinkUnavailable
						return
					}
				} else {
					b.recordHistoryScroll()
					if !b.scrollToAnchor(u.Fragment) && b.reader {
						b.status = readerLinkUnavailable
						return
					}
					b.url = u
					b.pushHistory(u.String(), b.scroll)
					b.status = ""
					b.prev = nil
					return
				}
			}
			b.Navigate(u.String(), true)
			return
		}
	}
	b.Navigate(href, true)
}

// readerLinkUnavailable is shown when a same-document link cannot work in the
// reader snapshot.
const readerLinkUnavailable = "Reader mode: link is not available"

func sameDocument(a, b *url.URL) bool {
	return a.Scheme == b.Scheme && a.Host == b.Host && a.Path == b.Path && a.RawQuery == b.RawQuery
}

// showNotice opens a small informational modal.
// showStatusDetails opens the full warning or error text in a modal, because
// the footer has to truncate long messages.
func (b *Browser) showStatusDetails() {
	msg := strings.TrimSpace(b.status)
	if msg == "" {
		return
	}
	switch statusLevelFor(b.status) {
	case statusError:
		b.showNotice("Error", msg)
	case statusWarning:
		b.showNotice("Warning", msg)
	}
}

// showUploadNotice explains that file uploads are not implemented, in the modal
// and on the status line so the reason stays visible after the modal closes.
func (b *Browser) showUploadNotice() {
	const msg = "File uploads and multipart forms are not supported"
	b.showNotice("Upload not supported", msg+".")
	b.status = "Upload not supported — " + msg
}

func (b *Browser) showNotice(title, message string) {
	b.download = nil
	b.notice = &noticeDialog{title: title, message: message}
}

// showDownload opens the modal curl hint for a download link or a response
// the browser cannot render. name, when empty, is derived from the URL path.
func (b *Browser) showDownload(target, name string) {
	if name == "" {
		if u, err := url.Parse(target); err == nil {
			name = u.Path
			if i := strings.LastIndexByte(name, '/'); i >= 0 {
				name = name[i+1:]
			}
			if unescaped, err := url.PathUnescape(name); err == nil {
				name = unescaped
			}
		}
	}
	name = sanitizeDownloadName(name)
	command := "curl -L -o " + shellQuote(name) + " " + shellQuote(target)
	b.notice = nil
	b.download = &downloadDialog{url: target, name: name, command: command}
}

// sanitizeDownloadName keeps a derived filename safe to print and use in the
// suggested curl command.
func sanitizeDownloadName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == '/' || r == '\\' || strings.ContainsRune(`:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		return "download"
	}
	return name
}

// shellQuote wraps s in single quotes for the suggested shell command,
// escaping embedded single quotes.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (b *Browser) scrollToAnchor(frag string) bool {
	if b.layout == nil || frag == "" {
		return false
	}
	if unescaped, err := url.PathUnescape(frag); err == nil {
		frag = unescaped
	}
	best := -1
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		for p := f.Node; p != nil; p = p.Parent {
			if p.Type != ElementNode {
				continue
			}
			if p.Attr("id") == frag || p.Attr("name") == frag {
				if best < 0 || f.Y < best {
					best = f.Y
				}
				break
			}
		}
	}
	if best >= 0 {
		b.scroll = best - 1
		b.clampScroll()
		return true
	}
	return false
}

// EscapeHTML escapes text for error pages.
func EscapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

func (b *Browser) sampleUsage() {
	b.usage.sample()
}
