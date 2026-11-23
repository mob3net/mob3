package browser

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/url"
	"strings"
	"sync/atomic"

	"browser/internal/engine"
	"browser/internal/protocol"
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

// Browser holds one page session's content and view state. The front end owns
// the trusted navigation list; this type keeps the memory-only page cache.
type Browser struct {
	*engine.Session
	loader *Loader

	// contentTop/contentHeight are the front-end-owned page rectangle. A
	// negative top means direct callers/tests use the legacy derivation.
	// pdfStarts holds the layout row where each PDF page begins, so the footer
	// can name the page under the viewport.
	pdfStarts []int
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

	// Dark mode remaps the rendered page palette.

	// History cache. The front end owns the ordered entry list and scroll
	// offsets; the engine only keeps a bounded memory-only page cache keyed by
	// the front-end entry ID.
	historyView     []string
	historyViewCur  int
	historyObserver func(protocol.HistoryCommit)
	// memLimitBytes is the process soft heap limit in bytes; zero leaves the
	// runtime default in place.
	memLimitBytes int64
	// nextScroll is consumed by the next navigation started from history.
	// autoNavigations counts consecutive scripted/meta redirects so a
	// self-redirect loop can be stopped. loopStopped reports that the cap was
	// hit while applying the current page.
	home      string
	cliConfig map[string]string
	// configNotice shows why an edited about:config value was rejected, such
	// as an unreadable -certs bundle.
	configNotice string
	// configToken is the one-shot token embedded in the current about:config
	// form. A config submission applies only when it presents this token, so
	// page content cannot rewrite session settings by navigating to
	// about:config with a query.
	configToken protocol.ConfigToken

	clipboardWriter io.Writer
	// Output is the terminal writer for the legacy direct draw path. The app
	// sets it to os.Stdout; tests use the package default hook.
	Output io.Writer

	color        bool
	prev         []string
	gfx          Graphics
	newGraphics  func(GraphicsMode, int, int) Graphics
	frameEncoder func(protocol.Frame, bool, bool) []string
	pngEncoder   func(protocol.Frame, int) ([]byte, error)
	// contentGrid is reused between frames and is exactly the page viewport.
	contentGrid *Grid
	frameBuf    frameScratch
	// zoom is the layout zoom factor: 1 is normal. It changes the CSS pixels
	// per terminal cell for everything except text metrics.
	// scaledLayout records whether the current layout was built with kitty
	// scaled text, so draw can rebuild when an overlay toggles it.
	// bigTextBlocked disables kitty scaled text for a page whose scaled runs
	// overlap neighbouring columns.

	// Graphics modes available to the i key, in cycle order, and the current
	// one. The list starts with native inline graphics when available.
	graphicsModes []GraphicsMode
	graphicsMode  GraphicsMode

	// hideFixed suppresses position:fixed boxes for the current page.
	// noSticky lays position:sticky boxes out as normal flow, so they scroll
	// with the page instead of following a top offset.
	// hideBackgrounds paints CSS background images as a neutral placeholder
	// instead of their pixels; the images stay loaded and laid out.

	// lastNativeImages tracks graphics-mode transitions (hints or a modal
	// opening/closing) so the next frame is fully repainted and no stale
	// terminal image survives.
	lastNativeImages bool

	// noPDF disables the built-in PDF text viewer, restoring the download
	// prompt for PDF responses.
	// pendingStatus replaces the computed load status once, so a JavaScript
	// toggle can report its new state instead of clearing the footer.
	pendingStatus string
	// jsPageBudget caps one page's load-time script phase. It defaults to
	// jsPageBudget and exists as a field so tests can shorten it.

	// interactive marks the terminal UI, where a background dynamic import can
	// wait for the event loop instead of blocking it.
	// imageTotal and imageDone drive the loading bar and footer count while a
	// deferred image batch is in flight.
	// pendingImageRow is the topmost laid-out row of images whose decoded data
	// arrived while they sat below the viewport. Their layout is deferred until
	// the viewport approaches that row or the end of the document, so a batch
	// far down the page cannot reflow and repaint the visible frame. Zero means
	// nothing is pending.

	// pendingDownloadName carries a download attribute from the clicked link
	// into the next load, so a non-renderable response keeps that filename.

	consoleOpen   bool
	consoleScroll int
	reqScroll     int
	storageScroll int
	htmlScroll    int
	toolScroll    int
	toolLog       []string
	consolePane   int // 0 = JS logs, 1 = requests, 2 = storage, 3 = HTML, 4 = tools
	consoleText   string
	consoleCursor int
	consoleEdit   bool // the input row inside the logs or tools section has focus
	consoleHist   consoleHistory
	toolHist      consoleHistory

	htmlPage  *Page
	htmlWidth int
	htmlCache []string
	jsLog     []string

	dialog *jsDialog
	// sessionCtx, sessionIn and sessionAbort are set while Serve owns the
	// event loop. A modal dialog then reads keys from the command stream
	// instead of a direct terminal channel.
	sessionCtx   context.Context
	sessionIn    <-chan protocol.Command
	sessionAbort <-chan struct{}
	sessionQuit  bool
	// emitFrames switches presentation to the front end: draw stashes the
	// screen frame and sends it over the transport instead of writing stdout.
	// Native image modes keep the engine-side writer until placements cross the
	// seam.
	emitFrames bool
	frameReady bool
	sessionOut protocol.EventSink
	// frontendChrome lets the normal path emit a content-only frame and a
	// State snapshot; the front end draws the header/footer from it.
	frontendChrome   bool
	nextDialogID     uint32
	nextSelectID     uint32
	selectFrontend   bool
	selectID         uint32
	downloadFrontend bool
	noticeFrontend   bool
	hintsFrontend    bool
	// abortGen counts user escape requests (Esc or Ctrl-C). A guarded script
	// compares it with the value captured at entry, so a press interrupts the
	// call in flight without leaking into later timer callbacks.
	abortGen atomic.Uint64
	// download holds the modal curl hint shown for download links and
	// non-renderable responses.
	download *downloadDialog
	// notice holds a small informational modal, such as the unsupported
	// file-upload message.
	notice *noticeDialog
	// selectMenu is the pending single-select choice awaiting the front end.
	selectMenu  *selectMenuDialog
	lastScroll  int
	usage       procUsage
	quitPending bool
}

// NewBrowser creates a browser with a viewport size.
func NewBrowser(w, h int) *Browser {
	if w < 20 {
		w = 80
	}
	if h < 5 {
		h = 24
	}
	loader := NewLoader()
	b := &Browser{
		Session: &engine.Session{
			Focus: -1, Hover: -1, Width: w, Height: h, ContentTop: -1,
			CellW: 8, CellH: 16, Zoom: 1, LayoutBudget: DefaultLayoutBudget,
			JSMemLimit: jsHeapBudget(effectiveMemoryLimit()), JSPageBudget: jsPageBudget,
			Cache:       engine.NewPageCache(historyCacheBudget(effectiveMemoryLimit())),
			ImageCh:     make(chan engine.ImageBatch, 1),
			LoadCh:      make(chan engine.LoadResult, 1),
			ImportCh:    make(chan engine.ImportTask, 16),
			NextEntryID: 1,
		},
		loader:         loader,
		historyViewCur: -1,
		home:           "about:home",
		configToken:    loader.ConfigToken(),
	}
	b.Cache.SetOnEvict(b.noteEvictedPage)
	b.setLoader(b.loader)
	setLayoutZoom(b.Zoom)
	return b
}

// setLoader installs a loader and wires it to this browser's session history so
// about:history reflects the current browser even when the CLI replaces the
// default loader.
func (b *Browser) setLoader(l *Loader) {
	b.loader = l
	if l != nil {
		b.configToken = l.ConfigToken()
	} else {
		b.configToken = nil
	}
	if l == nil {
		b.Session.Images = nil
	} else {
		b.Session.Images = l
	}
	if l != nil {
		l.HistoryFunc = b.historyViewSnapshot
		l.SetMemoryBudget(effectiveMemoryLimit())
		if b.Width > 0 {
			l.ViewportPx = cssWidthPx(b.Width)
		}
		b.applyImageCap()
	}
}

// imageCapPx returns the largest image box the terminal can paint, in CSS
// pixels. The floors keep a small terminal from hardening every image it
// loads; larger terminals get proportionally more pixels.
func (b *Browser) imageCapPx() (int, int) {
	cw, ch := b.CellW, b.CellH
	if cw <= 0 {
		cw = 8
	}
	if ch <= 0 {
		ch = 16
	}
	w, h := b.Width*cw, b.Height*ch
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
	b.loader.SetImageCap(b.imageCapPx())
}

// historyViewSnapshot supplies the stored display snapshot to the built-in
// history page. The front end owns the live list; this is only the latest
// bounded copy it sent for about:history.
func (b *Browser) historyViewSnapshot() ([]string, int) {
	return b.historyView, b.historyViewCur
}

// setHistoryView replaces the snapshot used by about:history. It is display
// data only; navigation state lives in the front end.
func (b *Browser) setHistoryView(v protocol.History) {
	// Keep in sync with the front-end snapshot cap and HistoryPageHTML.
	const limit = 50
	urls := make([]string, 0, len(v.Entries))
	start := 0
	if len(v.Entries) > limit {
		start = len(v.Entries) - limit
	}
	for _, e := range v.Entries[start:] {
		urls = append(urls, e.URL)
	}
	current := v.Current
	if current >= 0 {
		current -= start
		if current < 0 || current >= len(urls) {
			current = -1
		}
	}
	b.historyView = urls
	b.historyViewCur = current
}

// searchTemplate returns the configured search URL template.
func (b *Browser) searchTemplate() string {
	if b.loader != nil {
		return b.loader.SearchTemplate()
	}
	return DefaultSearchURL
}

// Navigate starts loading input in the background so the UI and Esc stay
// responsive. Results are applied by finishLoad from the main loop.
func (b *Browser) Navigate(input string, push bool) {
	b.navigateRequest(protocol.MethodGet, input, nil, "", push)
}

// nextHistoryID allocates an opaque ID for a new front-end history entry.
func (b *Browser) nextHistoryID() uint64 { return b.Session.NextHistoryID() }

func (b *Browser) navigateRequest(method, input string, body []byte, contentType string, push bool) {
	entryID, historyPush := b.EntryID, push
	if push || entryID == 0 {
		entryID = b.nextHistoryID()
		historyPush = true
	}
	b.navigateRequestEntry(method, input, body, contentType, entryID, historyPush, false)
}

// navigateRequestForce is navigateRequest with an optional one-shot block
// bypass. It backs the "Try anyway" link (about:retry) on the blocked page.
func (b *Browser) navigateRequestForce(method, input string, body []byte, contentType string, push bool, forceBlocks bool) {
	entryID, historyPush := b.EntryID, push
	if push || entryID == 0 {
		entryID = b.nextHistoryID()
		historyPush = true
	}
	b.navigateRequestEntry(method, input, body, contentType, entryID, historyPush, forceBlocks)
}

func (b *Browser) navigateRequestEntry(method, input string, body []byte, contentType string, entryID uint64, historyPush, forceBlocks bool) {
	// A download-attribute name belongs to this load only, so consume it even
	// when the navigation is rejected before a request is made.
	downloadName := b.PendingDownloadName
	b.PendingDownloadName = ""
	if strings.EqualFold(strings.TrimSpace(input), "about:retry") {
		if b.URL == nil {
			b.status = "Nothing to retry"
			b.draw()
			return
		}
		input = b.URL.String()
		forceBlocks = true
		historyPush = false
		b.NextScrollSet = false
	}
	u, err := ResolveInput(input, b.URL, b.searchTemplate())
	if err != nil {
		b.status = "Invalid URL: " + err.Error()
		b.draw()
		return
	}
	if method == "" {
		method = protocol.MethodGet
	}
	restoreScroll, hasRestoreScroll := b.NextScroll, b.NextScrollSet
	b.NextScrollSet = false
	if isAboutConfigURL(u) && b.Reader {
		b.status = "Config is not available in reader mode — press R to turn it off"
		b.draw()
		return
	}
	if isAboutConfigURL(u) {
		b.clearInputFocus()
		b.suspendCurrentPage()
		b.cancelLoadNow()
		if b.loader != nil {
			b.loader.BeginPage()
		}
		b.LoadSeq++
		q := configValues(method, u, body)
		applied := false
		if b.consumeConfigToken(q.Get(configTokenParam)) {
			b.applyConfig(q)
			applied = true
		} else if configHasSettings(q) {
			b.configNotice = "Settings not changed: use the Apply button on the config page"
		}
		page := b.configPage(u, applied)
		b.finishLoad(engine.LoadResult{
			Seq: b.LoadSeq, Page: page, URL: u, EntryID: entryID, HistoryPush: historyPush, Method: protocol.MethodGet,
			RestoreScroll: restoreScroll, HasRestoreScroll: hasRestoreScroll,
		})
		return
	}
	b.clearInputFocus()
	b.suspendCurrentPage()
	b.cancelDeferredImageLoad()
	b.ImagesPaused = false
	b.applyImageCap()
	if b.loader != nil {
		b.loader.BeginPage()
	}
	b.status = "Loading " + u.String() + " … (Esc stops)"
	b.clearSelection()

	target := u.String()
	load := b.LoadFn
	if load == nil {
		loader := b.loader
		load = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
			if forceBlocks {
				return loader.LoadRequestBypass(ctx, method, target, body, contentType, nil)
			}
			return loader.LoadRequest(ctx, method, target, body, contentType, nil)
		}
	}
	b.Session.BeginLoad(load, u, method, target, body, contentType, entryID, historyPush, restoreScroll, hasRestoreScroll, downloadName)
}

func (b *Browser) cancelLoadNow()       { b.Session.CancelLoadNow() }
func (b *Browser) startProgress()       { b.Session.StartProgress() }
func (b *Browser) finishProgress()      { b.Session.FinishProgress() }
func (b *Browser) clearProgress()       { b.Session.ClearProgress() }
func (b *Browser) progressActive() bool { return b.Session.ProgressActive() }
func (b *Browser) tickProgress() bool   { return b.Session.TickProgress() }

// stopLoading aborts the in-flight page load and ignores its late result.
func (b *Browser) stopLoading() {
	if !b.Loading {
		if b.DeferredImages {
			b.ImagesPaused = true
			b.cancelDeferredImageLoad()
			b.finishProgress()
			b.status = "Stopped loading images"
		}
		return
	}
	b.AutoNavigations = 0
	b.LoopStopped = false
	b.LoadSeq++
	b.cancelLoadNow()
	b.cancelDeferredImageLoad()
	b.status = "Stopped loading"
}

// finishLoad applies a completed load. Stale results from canceled navigations
// are ignored by the engine result sequence.
func (b *Browser) finishLoad(res engine.LoadResult) {
	if res.Seq != b.LoadSeq {
		return
	}
	b.Session.ApplyLoadResult(res, b)
}

// pageLoadStatus returns the status line for a freshly applied page, or an
// empty string when the page loaded cleanly.
func (b *Browser) pageLoadStatus(page *Page, readerFailed bool) string {
	switch {
	case page.Blocked:
		return "Site blocked — browser check not supported"
	case page.Status >= 400:
		return fmt.Sprintf("HTTP %d %s", page.Status, protocol.HTTPStatusText(page.Status))
	case b.moduleErr:
		return moduleFailedStatus
	case b.Layout != nil && b.Layout.Truncated:
		return layoutTruncatedStatus
	case readerFailed:
		return "Reader mode unavailable"
	}
	return ""
}

// consumePendingJS applies a queued location.href/assign navigation or
// programmatic form.submit() after the current script or callback has finished.
func (b *Browser) consumePendingJS() bool { return b.applyPendingJS(false, 0, false) }

// applyPendingJS applies a navigation queued by page JavaScript. duringLoad
// marks the queue drain that happens while the page is still being applied:
// such a redirect keeps the same history entry, matching browser behaviour,
// while a click or timer navigation after the page settled still pushes.
func (b *Browser) applyPendingJS(duringLoad bool, entryID uint64, historyPush bool) bool {
	if b.JS == nil {
		return false
	}
	if pf := b.JS.PendingForm(); pf != nil {
		b.JS.SetPendingForm(nil)
		if duringLoad {
			b.navigateRequestEntry(pf.Method, pf.Target, pf.Body, pf.ContentType, entryID, historyPush, false)
		} else {
			b.navigateRequest(pf.Method, pf.Target, pf.Body, pf.ContentType, true)
		}
		return true
	}
	if b.JS.PendingNav() == "" {
		return false
	}
	target := b.JS.PendingNav()
	replace := b.JS.PendingReplace()
	b.JS.SetPendingNav("")
	b.JS.SetPendingReplace(false)
	if b.AutoNavigations >= maxAutoNavigations {
		b.LoopStopped = true
		return false
	}
	b.AutoNavigations++
	if duringLoad {
		b.navigateRequestEntry(protocol.MethodGet, target, nil, "", entryID, historyPush, false)
	} else if replace {
		b.navigateRequestEntry(protocol.MethodGet, target, nil, "", b.EntryID, false, false)
	} else {
		b.Navigate(target, true)
	}
	return true
}

func (b *Browser) rebuild() {
	if b.Page == nil {
		return
	}
	// Keep an active hint session alive and recompute its hints against the
	// new layout instead of dismissing it.
	wasHints := b.LinkHints
	hintAsk := b.HintAsk
	hintPrefix := b.HintPrefix
	wasConfirm := b.HintConfirm
	var confirmNode *Node
	confirmHref := ""
	if wasConfirm && b.Layout != nil && b.HintConfirmLink >= 0 && b.HintConfirmLink < len(b.Layout.Links) {
		li := b.Layout.Links[b.HintConfirmLink]
		confirmNode = li.Node
		confirmHref = li.Href
	}
	b.clearLinkHints()
	b.clearHintConfirm()
	b.BigText = b.scaledTextWanted()
	if b.loader == nil {
		b.Session.Images = nil
	} else {
		b.Session.Images = b.loader
	}
	b.Session.RebuildLayout(b.viewportHeight(), b.BigText)
	b.reindexFind()
	if b.Layout != nil && b.Layout.Truncated {
		if b.status == "" {
			b.status = layoutTruncatedStatus
		}
	} else if b.status == layoutTruncatedStatus {
		b.status = ""
	}
	b.Hover = -1
	if wasHints {
		b.reopenLinkHints(hintAsk, hintPrefix)
	} else if wasConfirm {
		if link := b.findHintLink(confirmNode, confirmHref); link >= 0 {
			b.beginHintConfirm(link)
		}
	}
}

// overlayOpen reports whether a modal window or popup is drawn over the page.
// Terminal-side graphics composite above the cell grid, so scaled text is
// disabled while one is open and the layout is rebuilt without it.
func (b *Browser) overlayOpen() bool {
	return b.dialog != nil || (b.download != nil && !b.downloadFrontend) ||
		(b.notice != nil && !b.noticeFrontend) ||
		(b.selectMenu != nil && !b.selectFrontend)
}

// scaledTextWanted reports whether the page should use kitty scaled text.
// Modals disable it because terminal-side scaled glyphs composite above the
// cell grid and would paint over the modal.
func (b *Browser) scaledTextWanted() bool {
	return b.gfx != nil && b.gfx.TextSizing() && !b.NoBigText && !b.BigTextBlocked && !b.overlayOpen()
}

// toggleReader switches between the page and an extracted article view without
// touching history or the live page DOM.
func (b *Browser) toggleReader() {
	if !b.Reader && isAboutConfigURL(b.URL) {
		b.status = "Reader mode is unavailable on the config page"
		return
	}
	if b.Page == nil || b.Page.Root == nil {
		b.status = "Reader mode unavailable"
		return
	}
	if b.Reader {
		b.Reader = false
		b.ReaderRoot = nil
		b.rebuild()
		b.Scroll = b.ReaderSavedScroll
		b.ReaderSavedScroll = 0
		b.clampScroll()
		b.status = "Reader mode off"
		return
	}
	if b.Page.PDF != nil {
		b.status = "Reader mode unavailable"
		return
	}
	root, ok := extractReaderPage(b.Page.Root, b.Page.Title)
	if !ok {
		b.status = "Reader mode unavailable"
		return
	}
	b.ReaderSavedScroll = b.Scroll
	b.Reader = true
	b.ReaderRoot = root
	b.Scroll = 0
	b.Focus = -1
	b.clearSelection()
	b.rebuild()
	b.status = "Reader mode — R to exit"
}

func (b *Browser) toggleDark() {
	b.Dark = !b.Dark
	b.rebuild()
	b.status = "Dark mode on"
	if !b.Dark {
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
			if s > b.Zoom+1e-9 {
				b.applyZoom(s)
				return
			}
		}
		b.applyZoom(zoomSteps[len(zoomSteps)-1])
		return
	}
	for i := len(zoomSteps) - 1; i >= 0; i-- {
		if zoomSteps[i] < b.Zoom-1e-9 {
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
	if b.Zoom != z {
		b.Zoom = z
		setLayoutZoom(z)
		// A page whose scaled runs overlapped may be fine at another zoom, so
		// re-evaluate the fallback instead of keeping it for the session.
		b.BigTextBlocked = false
		b.prev = nil
		b.rebuild()
		if limit := b.maxScroll(); b.Scroll > limit {
			b.Scroll = limit
		}
		if b.Scroll < 0 {
			b.Scroll = 0
		}
	}
	b.status = fmt.Sprintf("Zoom %d%%", int(math.Round(z*100)))
}

// toggleJavaScript switches page scripting for the rest of the session. The
// current document is fetched again so its no-script or scripted variant is
// applied; the page's existing DOM is not reused.
func (b *Browser) toggleJavaScript() {
	b.NoJS = !b.NoJS
	if b.loader != nil {
		b.loader.NoScripts = b.NoJS
	}
	if b.NoJS {
		b.pendingStatus = "JavaScript off — press J to re-enable"
	} else {
		b.pendingStatus = "JavaScript on"
	}
	if b.URL == nil {
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
	b.resizeWithContent(w, h, -1, 0)
}

// resizeWithContent applies the terminal size and the front-end-owned content
// rectangle. A negative top keeps the legacy chrome derivation for direct
// callers and tests.
func (b *Browser) resizeWithContent(w, h, top, contentH int) {
	if w < 10 {
		w = 10
	}
	if h < 3 {
		h = 3
	}
	top, contentH = normalizeContentRect(h, top, contentH)
	if w == b.Width && h == b.Height && top == b.ContentTop && contentH == b.ContentHeight {
		return
	}
	b.Width, b.Height = w, h
	b.ContentTop, b.ContentHeight = top, contentH
	b.applyImageCap()
	b.prev = nil
	b.clearSelection()
	b.rebuild()
}

func (b *Browser) maxScroll() int { return b.Session.MaxScroll(b.viewportHeight()) }

func (b *Browser) clampScroll() { b.Session.ClampScroll(b.viewportHeight()) }

func (b *Browser) scrollBy(n int) {
	b.clearLinkHints()
	b.clearHintConfirm()
	b.Hover = -1
	b.Session.ScrollBy(n, b.viewportHeight())
	switch {
	case b.FindText != "":
		// Keep the confirmed find status visible while scrolling; only Esc
		// clears the search and its footer line.
		b.status = b.findStatusText()
	case !strings.HasPrefix(b.status, "HTTP ") && !strings.HasPrefix(b.status, "Error"):
		b.status = ""
	}
}

func (b *Browser) focusedLink() *LinkInfo { return b.Session.FocusedLink() }
func (b *Browser) hoveredLink() *LinkInfo { return b.Session.HoveredLink() }

// setPageHover updates the element chain used for CSS :hover and dispatches
// pointer enter/leave events. A style pass runs only when the page has hover
// rules or a handler mutated the DOM.
func (b *Browser) setPageHover(n *Node) {
	if n == b.HoverNode {
		return
	}
	old := b.HoverNode
	if old != nil {
		markHoverChain(old, false)
		if b.JS != nil && b.liveNode(old) {
			b.JS.FireNodeEvent(old, "mouseout")
			b.JS.FireNodeEvent(old, "mouseleave")
		}
	}
	b.HoverNode = n
	dirty := false
	if n != nil {
		markHoverChain(n, true)
		if b.JS != nil && b.liveNode(n) {
			b.JS.FireNodeEvent(n, "mouseover")
			b.JS.FireNodeEvent(n, "mouseenter")
		}
	}
	if b.JS != nil {
		dirty = b.JS.ConsumeDirty()
	}
	// Hover-driven style passes are delayed in interactive sessions so fast
	// pointer movement does not rebuild the page for every cell. Automation
	// and tests without an event loop need the result synchronously.
	if b.PageHasHover || dirty {
		if b.emitFrames || b.sessionIn != nil {
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
		idx = b.Session.LinkAt(x, b.Scroll+(y-top))
	}
	b.Hover = idx
}

func (b *Browser) resolveHref(href string) string { return b.Session.ResolveHref(href) }

func (b *Browser) followFocused() {
	li := b.focusedLink()
	if li == nil {
		b.status = "No link selected (press Tab)"
		return
	}
	// Run the link's click handlers before navigating. Bare "#" links often
	// open dialogs or menus from JavaScript and cancel the default navigation.
	if b.JS != nil && li.Node != nil && b.liveNode(li.Node) {
		prevented := b.JS.DispatchClick(li.Node)
		if prevented && !(b.JS.EventErr() && linkFallbackOK(li.Href)) {
			if b.JS.ConsumeDirty() {
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
		if b.Page != nil && b.Page.Base != nil {
			if u, err := b.Page.Base.Parse(href); err == nil {
				target = u.String()
			}
		}
		if isPDFDownloadLink(target, li.Node.Attr("download")) {
			// A PDF is worth fetching because the viewer can show it; the
			// attribute also names the file if the response is not a PDF after
			// all.
			b.PendingDownloadName = li.Node.Attr("download")
			b.Navigate(target, true)
			return
		}
		// Everything else keeps browser semantics: "download" means save, so
		// show the curl popup without spending a request on a large binary.
		b.showDownload(target, li.Node.Attr("download"))
		return
	}
	if b.Page != nil && b.Page.Base != nil {
		if u, err := b.Page.Base.Parse(href); err == nil {
			if b.URL != nil && sameDocument(u, b.URL) {
				if u.Fragment == "" {
					// A bare "#" link is a page-modal trigger on many sites.
					// In reader mode it would reload the page and jump to the
					// top, so warn instead of navigating.
					if b.Reader {
						b.status = readerLinkUnavailable
						return
					}
				} else {
					if !b.scrollToAnchor(u.Fragment) && b.Reader {
						b.status = readerLinkUnavailable
						return
					}
					b.EntryID = b.nextHistoryID()
					b.URL = u
					b.status = ""
					b.prev = nil
					title := ""
					if b.Page != nil {
						title = b.Page.Title
					}
					b.emitHistoryCommit(protocol.HistoryCommit{
						EntryID: b.EntryID, URL: u.String(), Title: title, Scroll: b.Scroll, Push: true,
					})
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

func sameDocument(a, b *url.URL) bool { return engine.SameDocument(a, b) }

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
	b.downloadFrontend = false
	b.notice = &noticeDialog{title: title, message: message}
	if b.sessionOut != nil {
		b.noticeFrontend = true
		if err := b.sessionOut.Send(protocol.Event{Body: protocol.Notice{Title: title, Message: message}}); err != nil {
			b.notice = nil
			b.noticeFrontend = false
		}
	}
}

// showDownload opens the modal curl hint for a download link or a response
// the browser cannot render. name, when empty, is derived from the URL path.
func (b *Browser) showDownload(target, name string) {
	if u, err := url.Parse(target); err == nil && strings.EqualFold(u.Scheme, "file") {
		b.showNotice("Download blocked", "Local file downloads are not supported.")
		return
	}
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
	if name == "" {
		name = "download"
	}
	if strings.HasPrefix(name, "-") {
		// Keep a name that starts with a dash from reading as an option even
		// if curl changes how -o consumes its argument.
		name = "./" + name
	}
	// The -- terminator keeps a URL that starts with a dash from being parsed
	// as a curl option.
	command := "curl -L -o " + shellQuote(name) + " -- " + shellQuote(target)
	b.notice = nil
	b.noticeFrontend = false
	b.download = &downloadDialog{url: target, name: name, command: command}
	if b.sessionOut != nil {
		b.downloadFrontend = true
		if err := b.sessionOut.Send(protocol.Event{Body: protocol.Download{
			URL: target, Name: name, Command: command,
		}}); err != nil {
			b.download = nil
			b.downloadFrontend = false
		}
	}
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
	return b.Session.ScrollToAnchor(frag, b.viewportHeight())
}

// EscapeHTML escapes text for error pages.
func EscapeHTML(s string) string { return engine.EscapeHTML(s) }

func (b *Browser) sampleUsage() {
	b.usage.sample()
}
