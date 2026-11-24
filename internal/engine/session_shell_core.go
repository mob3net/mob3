package engine

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strings"

	"browser/internal/protocol"
	"time"
)

const (
	layoutTruncatedStatus = "Page truncated (layout limit)"
	moduleFailedStatus    = "Module script failed — see ! console"
	maxClipboardBytes     = 100 * 1024
)

// NewSession creates a session with a viewport size, a default loader and a
// private browser context. The app replaces the loader and supplies the shared
// context when it constructs a configured session.
func NewSession(w, h int) *Session {
	return NewSessionWithBrowserContext(w, h, nil)
}

// NewSessionWithBrowserContext creates a session that allocates history-entry
// IDs from bc. A nil bc creates a private context, which is what direct
// callers and tests use.
func NewSessionWithBrowserContext(w, h int, bc *BrowserContext) *Session {
	return newSessionWithBrowserContext(w, h, bc, true)
}

func newSessionWithBrowserContext(w, h int, bc *BrowserContext, resetZoom bool) *Session {
	if w < 20 {
		w = 80
	}
	if h < 5 {
		h = 24
	}
	if bc == nil {
		bc = NewBrowserContext()
	}
	tabID := bc.NextTabID()
	loader := NewLoader(nil)
	b := &Session{
		Frame: NewFrame(),
		Focus: -1, Hover: -1, Width: w, Height: h, ContentTop: -1,
		CellW: 8, CellH: 16, Zoom: 1, LayoutBudget: DefaultLayoutBudget,
		JSMemLimit: jsHeapBudget(effectiveMemoryLimit()), JSPageBudget: jsPageBudget,
		Cache:          NewPageCache(historyCacheBudget(effectiveMemoryLimit())),
		ImageCh:        make(chan ImageBatch, 1),
		LoadCh:         make(chan LoadResult, 1),
		ImportCh:       make(chan ImportTask, 16),
		loader:         loader,
		browser:        bc,
		tabID:          tabID,
		historyViewCur: -1,
		home:           "about:home",
		configToken:    loader.ConfigToken(),
		active:         true,
	}
	b.router = newFrameRouter()
	b.router.Register(b.Frame)
	b.budgets = bc.Budgets()
	if b.budgets != nil {
		b.budgets.Register(b)
	}
	b.Cache.SetOnEvict(b.noteEvictedPage)
	b.setLoader(b.loader)
	if resetZoom {
		SetZoom(b.Zoom)
	} else {
		b.Zoom = Zoom()
	}
	b.zoomGen = zoomGen()
	b.usage.sample()
	return b
}

// setLoader installs a loader and wires it to this browser's session history so
// about:history reflects the current browser even when the CLI replaces the
// default loader.
func (b *Session) setLoader(l *Loader) {
	b.loader = l
	if l != nil {
		b.configToken = l.ConfigToken()
	} else {
		b.configToken = nil
	}
	if l == nil {
		b.Images = nil
	} else {
		b.Images = l
	}
	if l != nil {
		l.HistoryFunc = b.historyViewSnapshot
		if b.budgets != nil {
			b.applyBudgetShare(b.budgetShare)
		} else {
			l.SetMemoryBudget(effectiveMemoryLimit())
		}
		if b.Width > 0 {
			l.ViewportPx = cssWidthPx(b.Width)
		}
		b.applyImageCap()
	}
}

// imageCapPx returns the largest image box the terminal can paint, in CSS
// pixels. The floors keep a small terminal from hardening every image it
// loads; larger terminals get proportionally more pixels.
func (b *Session) imageCapPx() (int, int) {
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
func (b *Session) applyImageCap() {
	if b.loader == nil {
		return
	}
	b.loader.SetImageCap(b.imageCapPx())
}

// historyViewSnapshot supplies the stored display snapshot to the built-in
// history page. The front end owns the live list; this is only the latest
// bounded copy it sent for about:history.
func (b *Session) historyViewSnapshot() ([]string, int) {
	return b.historyView, b.historyViewCur
}

// setHistoryView replaces the snapshot used by about:history. It is display
// data only; navigation state lives in the front end.
func (b *Session) setHistoryView(v protocol.History) {
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
func (b *Session) searchTemplate() string {
	if b.loader != nil {
		return b.loader.SearchTemplate()
	}
	return DefaultSearchURL
}

// Navigate starts loading input in the background so the UI and Esc stay
// responsive. Results are applied by finishLoad from the main loop.
func (b *Session) Navigate(input string, push bool) {
	b.navigateRequest(protocol.MethodGet, input, nil, "", push)
}

// nextHistoryID allocates an opaque ID for a new front-end history entry.
func (b *Session) nextHistoryID() uint64 { return b.NextHistoryID() }

func (b *Session) navigateRequest(method, input string, body []byte, contentType string, push bool) {
	entryID, historyPush := b.EntryID, push
	if push || entryID == 0 {
		entryID = b.nextHistoryID()
		historyPush = true
	}
	b.navigateRequestEntry(method, input, body, contentType, entryID, historyPush, false)
}

// navigateRequestForce is navigateRequest with an optional one-shot block
// bypass. It backs the "Try anyway" link (about:retry) on the blocked page.
func (b *Session) navigateRequestForce(method, input string, body []byte, contentType string, push bool, forceBlocks bool) {
	entryID, historyPush := b.EntryID, push
	if push || entryID == 0 {
		entryID = b.nextHistoryID()
		historyPush = true
	}
	b.navigateRequestEntry(method, input, body, contentType, entryID, historyPush, forceBlocks)
}

func (b *Session) navigateRequestEntry(method, input string, body []byte, contentType string, entryID uint64, historyPush, forceBlocks bool) {
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
		b.finishLoad(LoadResult{
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
	// Attach the initiating page before the load goroutine starts (b.URL can
	// change while the load is in flight). It supplies the SameSite top-level
	// site, and a form POST also carries its origin.
	if b.URL != nil {
		initiator := b.URL.String()
		origin := ""
		if !strings.EqualFold(method, protocol.MethodGet) {
			origin = requestOrigin(b.URL)
		}
		inner := load
		load = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
			ctx = protocol.WithRequestInitiator(ctx, initiator)
			if origin != "" {
				ctx = protocol.WithRequestOrigin(ctx, origin)
			}
			return inner(ctx, method, target, body, contentType)
		}
	}
	b.BeginLoad(load, u, method, target, body, contentType, entryID, historyPush, restoreScroll, hasRestoreScroll, downloadName)
}

func (b *Session) cancelLoadNow()       { b.CancelLoadNow() }
func (b *Session) startProgress()       { b.StartProgress() }
func (b *Session) finishProgress()      { b.FinishProgress() }
func (b *Session) clearProgress()       { b.ClearProgress() }
func (b *Session) progressActive() bool { return b.ProgressActive() }
func (b *Session) tickProgress() bool   { return b.TickProgress() }

// stopLoading aborts the in-flight page load and ignores its late result.
func (b *Session) stopLoading() {
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
func (b *Session) finishLoad(res LoadResult) {
	if res.Seq != b.LoadSeq {
		return
	}
	b.ApplyLoadResult(res, b)
}

// pageLoadStatus returns the status line for a freshly applied page, or an
// empty string when the page loaded cleanly.
func (b *Session) pageLoadStatus(page *Page, readerFailed bool) string {
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
func (b *Session) consumePendingJS() bool { return b.applyPendingJS(false, 0, false) }

// applyPendingJS applies a navigation queued by page JavaScript. duringLoad
// marks the queue drain that happens while the page is still being applied:
// such a redirect keeps the same history entry, matching browser behaviour,
// while a click or timer navigation after the page settled still pushes.
func (b *Session) applyPendingJS(duringLoad bool, entryID uint64, historyPush bool) bool {
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

func (b *Session) rebuild() {
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
		b.Images = nil
	} else {
		b.Images = b.loader
	}
	b.RebuildLayout(b.viewportHeight(), b.BigText)
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
func (b *Session) overlayOpen() bool {
	return b.frontendOverlay || b.dialog != nil || (b.download != nil && !b.downloadFrontend) ||
		(b.notice != nil && !b.noticeFrontend) ||
		(b.selectMenu != nil && !b.selectFrontend)
}

// nativeGraphicsEnabled reports whether the current graphics mode may emit
// terminal-native image placements. The renderer itself lives in the front
// end; the engine only records the mode and capability.
func (b *Session) nativeGraphicsEnabled() bool {
	if b.graphicsMode == GraphicsBlocks || b.graphicsMode == GraphicsNone {
		return false
	}
	return b.caps&(protocol.CapKittyGraphics|protocol.CapITermGraphics) != 0
}

// nativeGraphicsMode reports the best native protocol the terminal supports.
func (b *Session) nativeGraphicsMode() GraphicsMode {
	switch {
	case b.caps&protocol.CapKittyGraphics != 0:
		return GraphicsKitty
	case b.caps&protocol.CapITermGraphics != 0:
		return GraphicsITerm
	}
	return GraphicsNone
}

// textSizingEnabled reports whether kitty scaled text is available in the
// current mode.
func (b *Session) textSizingEnabled() bool {
	return b.caps&protocol.CapKittyText != 0 && b.nativeGraphicsEnabled()
}

// scaledTextWanted reports whether the page should use kitty scaled text.
// Modals disable it because terminal-side scaled glyphs composite above the
// cell grid and would paint over the modal.
func (b *Session) scaledTextWanted() bool {
	return b.textSizingEnabled() && !b.NoBigText && !b.BigTextBlocked && !b.overlayOpen()
}

// toggleReader switches between the page and an extracted article view without
// touching history or the live page DOM.
func (b *Session) toggleReader() {
	if !b.Reader && isAboutConfigURL(b.URL) {
		b.status = "Reader mode is unavailable on the config page"
		return
	}
	if !b.Reader && readerPageSkipped(b.URL) {
		b.status = "Reader mode is unavailable on the history page"
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
	root, ok := ExtractReaderPage(b.Page.Root, b.Page.Title)
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

func (b *Session) toggleDark() {
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
func (b *Session) zoomBy(dir int) {
	current := Zoom()
	if dir > 0 {
		for _, s := range zoomSteps {
			if s > current+1e-9 {
				b.applyZoom(s)
				return
			}
		}
		b.applyZoom(zoomSteps[len(zoomSteps)-1])
		return
	}
	for i := len(zoomSteps) - 1; i >= 0; i-- {
		if zoomSteps[i] < current-1e-9 {
			b.applyZoom(zoomSteps[i])
			return
		}
	}
	b.applyZoom(zoomSteps[0])
}

// resetZoom returns the layout zoom to 100%.
func (b *Session) resetZoom() {
	b.applyZoom(1)
}

// applyZoom installs a layout zoom factor and rebuilds the page so media
// queries and every pixel-based length see the new cell ratio.
func (b *Session) applyZoom(z float64) {
	if Zoom() != z {
		SetZoom(z)
		b.Zoom = z
		b.zoomGen = zoomGen()
		// A page whose scaled runs overlapped may be fine at another zoom, so
		// re-evaluate the fallback instead of keeping it for the session.
		b.BigTextBlocked = false
		b.rebuild()
		if limit := b.maxScroll(); b.Scroll > limit {
			b.Scroll = limit
		}
		if b.Scroll < 0 {
			b.Scroll = 0
		}
	} else {
		b.Zoom = z
		b.zoomGen = zoomGen()
	}
	b.status = fmt.Sprintf("Zoom %d%%", int(math.Round(z*100)))
}

// toggleJavaScript switches page scripting for the rest of the session. The
// current document is fetched again so its no-script or scripted variant is
// applied; the page's existing DOM is not reused.
func (b *Session) toggleJavaScript() {
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

func (b *Session) resize(w, h int) {
	b.resizeWithContent(w, h, -1, 0)
}

// resizeWithContent applies the terminal size and the front-end-owned content
// rectangle. A negative top keeps the legacy chrome derivation for direct
// callers and tests.
func (b *Session) resizeWithContent(w, h, top, contentH int) {
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
	b.clearSelection()
	b.rebuild()
}

func (b *Session) maxScroll() int { return b.MaxScroll(b.viewportHeight()) }

func (b *Session) clampScroll() { b.ClampScroll(b.viewportHeight()) }

func (b *Session) scrollBy(n int) {
	b.clearLinkHints()
	b.clearHintConfirm()
	b.Hover = -1
	b.ScrollBy(n, b.viewportHeight())
	switch {
	case b.FindText != "":
		// Keep the confirmed find status visible while scrolling; only Esc
		// clears the search and its footer line.
		b.status = b.findStatusText()
	case !strings.HasPrefix(b.status, "HTTP ") && !strings.HasPrefix(b.status, "Error"):
		b.status = ""
	}
}

func (b *Session) focusedLink() *LinkInfo { return b.FocusedLink() }
func (b *Session) hoveredLink() *LinkInfo { return b.HoveredLink() }

// setPageHover updates the element chain used for CSS :hover and dispatches
// pointer enter/leave events. A style pass runs only when the page has hover
// rules or a handler mutated the DOM.
func (b *Session) setPageHover(n *Node) {
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

func (b *Session) flushHoverRebuild() bool {
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
func (b *Session) updateHover(x, y, top, bottom int) {
	idx := -1
	if y >= top && y < bottom {
		idx = b.LinkAt(x, b.Scroll+(y-top))
	}
	b.Hover = idx
}

func (b *Session) resolveHref(href string) string { return b.ResolveHref(href) }

func (b *Session) followFocused() {
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

func (b *Session) followLink(li *LinkInfo) {
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
					b.emitHistoryCommit(protocol.HistoryCommit{
						EntryID: b.EntryID, URL: u.String(), Scroll: b.Scroll, Push: true,
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

func sameDocument(a, b *url.URL) bool { return SameDocument(a, b) }

// showNotice opens a small informational modal.
// showStatusDetails opens the full warning or error text in a modal, because
// the footer has to truncate long messages.
func (b *Session) showStatusDetails() {
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
func (b *Session) showUploadNotice() {
	const msg = "File uploads and multipart forms are not supported"
	b.showNotice("Upload not supported", msg+".")
	b.status = "Upload not supported — " + msg
}

func (b *Session) showNotice(title, message string) {
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
func (b *Session) showDownload(target, name string) {
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

func (b *Session) scrollToAnchor(frag string) bool {
	return b.ScrollToAnchor(frag, b.viewportHeight())
}

func (b *Session) sampleUsage() {
	b.usage.sample()
}
