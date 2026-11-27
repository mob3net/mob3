package frontend

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"browser/internal/protocol"
)

// LoopState is the presentation state the session loop updates as events
// arrive, including the Page snapshot the front end uses to draw chrome. It is
// exported for tests and for the app wiring layer.
type LoopState struct {
	Scroll    int
	Native    bool
	LastCrash string
	Clipboard io.Writer
	Page      protocol.State
}

// TabActions are the session-loop operations a tab can request from its shell
// keys and tab-bar clicks. Close with index -1 means the active tab.
type TabActions struct {
	Select            func(index int)
	Cycle             func(delta int)
	Close             func(index int) (quit bool)
	New               func()
	NewTabURL         func(url string)
	OpenTabBackground func(url string)
	Scroll            func(delta int)
	ToggleGraphics    func()
}

// Tab is one terminal browsing context: its protocol session, per-tab state,
// history and overlays. The SessionLoop owns terminal input, the shared
// presenter and the event loop; a tab only draws when it is the active one.
type Tab struct {
	Session   protocol.Session
	SessionID protocol.SessionID
	Presenter *Presenter

	Stderr io.Writer
	State  LoopState

	// Actions are installed by the SessionLoop; TabBar and TabOffset mirror the
	// shared bar so mouse handling uses the same labels and scrolling window.
	Actions   TabActions
	TabBar    []TabView
	TabOffset int

	quitPending bool
	quitBase    protocol.State
	scrollDrag  bool
	scrollGrab  int // rows between the thumb top and the grabbed row

	// overlaySent records whether the engine was told a front-end window covers
	// the page, so native images are drawn as half-block art underneath it.
	overlaySent bool

	// History is front-end-owned. AttestedURL reports the host's final URL for
	// a document navigation; the engine's HistoryCommit supplies the title,
	// scroll and push flag but never the trusted URL.
	history      []historyEntry
	historyIndex int
	historyLog   *historyLog
	AttestedURL  func(entryID uint64) (string, bool)

	// Each window owns its state in the file for its subsystem. overlayList
	// sets their shared dispatch order.
	linkPreviewOverlay
	consoleOverlay
	hintOverlay
	downloadOverlay
	noticeOverlay
	selectOverlay
	configOverlay
	historyOverlay
	dialogOverlay
	promptOverlay
	findOverlay
	menuOverlay

	// clipboardPendingUntil authorizes one Clipboard event after a copy action.
	// It keeps a compromised engine from overwriting the clipboard at will.
	clipboardPendingUntil time.Time

	// Report handles a contained crash. When nil, the error is shown in the
	// tab's footer and the tab stays in the bar.
	Report func(string)
	// tabCloseQuit carries a quit request from a tab-bar close click, which is
	// routed through the boolean handleChromeMouse return.
	tabCloseQuit bool

	// Dead marks a tab whose protocol session ended. It keeps its place in the
	// tab bar so the error stays visible and the user can close it without
	// taking down the other tabs.
	Dead         bool
	CrashMessage string
}

// AttachPresenter gives the shared presenter to this tab, rebuilding every
// overlay pointer from the tab's own state. The previous tab must drop its
// presenter first.
func (l *Tab) AttachPresenter(p *Presenter) {
	l.Presenter = p
	if p == nil {
		return
	}
	p.ResetForTab()
	p.SetState(l.State.Page)
	full := false
	for _, o := range l.overlayList() {
		if o.isOpen() && o.install(l) {
			full = true
		}
	}
	if full {
		l.consoleOverlay.present(l)
		return
	}
	_ = p.Refresh()
}

// DetachPresenter removes the shared presenter from this tab without touching
// the presenter itself; the loop resets it before the next tab draws.
func (l *Tab) DetachPresenter() { l.Presenter = nil }

// setFrontendOverlay tells the engine whether a front-end window covers the
// page. The engine then repaints native images as half-block art and scaled
// text at normal size, so neither can composite above the window.
func (l *Tab) setFrontendOverlay(open bool) {
	if l.overlaySent == open {
		return
	}
	l.overlaySent = open
	arg := 0
	if open {
		arg = 1
	}
	if l.Session != nil {
		_ = l.Session.Send(protocol.Command{Body: protocol.App{Action: protocol.AppFrontendOverlay, Arg: arg}})
	}
}

// BarView is this tab's label in the tab bar.
func (l *Tab) BarView(active bool) TabView {
	title := l.State.Page.Title
	if title == "" || isNewTabPage(l.State.Page.URL) {
		title = "New Tab"
	}
	if l.Dead {
		title = "Crashed"
	}
	return TabView{Title: title, Active: active}
}

func isNewTabPage(rawURL string) bool {
	switch rawURL {
	case "about:home", "about:start", "about:blank":
		return true
	}
	return false
}

// MarkDead records a contained session failure. The tab stays in the bar so
// its error remains visible and the rest of the browser keeps running.
func (l *Tab) MarkDead(message string) {
	if l.Dead {
		return
	}
	l.Dead = true
	if l.CrashMessage == "" {
		l.CrashMessage = clipCrashMessage(protocol.StripControls(message))
	}
	l.clearOverlaysAfterCrash()
	st := l.State.Page
	st.FrontendChrome = true
	st.KeysEngine = false
	st.QuitPending = false
	st.PromptActive = false
	st.FindActive = false
	st.Menu = protocol.MenuNone
	st.FooterLeft = " Page engine stopped: " + l.CrashMessage
	st.FooterLevel = protocol.FooterError
	st.FooterStatusHit = false
	st.PDFHint, st.PDFHintX = "", -1
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) clearOverlaysAfterCrash() {
	for _, o := range l.overlayList() {
		o.reset(l)
	}
	l.quitPending = false
}

// handleDeadInput keeps tab management and quit usable after the engine has
// stopped. Page keys and content mouse reports are ignored.
func (l *Tab) handleDeadInput(k string) (quit bool, err error) {
	if k == "eof" {
		return true, nil
	}
	if ev, ok := ParseMouseToken(k); ok {
		if l.dismissQuitOnClick(ev) {
			return false, nil
		}
		if l.handleTabBarMouse(ev) {
			quit := l.tabCloseQuit
			l.tabCloseQuit = false
			return quit, nil
		}
		return false, nil
	}
	if l.quitPending {
		switch k {
		case "y", "Y", "enter":
			return true, nil
		default:
			l.cancelQuit()
			return false, nil
		}
	}
	if handled, quit := l.handleTabKey(k); handled {
		return quit, nil
	}
	switch k {
	case "q", "ctrl-c":
		l.startQuit()
	}
	return false, nil
}

// handleTabBarMouse consumes clicks on the tab bar and its new-tab button. It
// is safe without a session because those actions are owned by the loop.
func (l *Tab) handleTabBarMouse(ev MouseEvent) bool {
	if ev.Wheel != 0 || ev.Motion || !ev.Press {
		return false
	}
	if ev.Button == MouseButtonMiddle {
		return l.handleMiddleTabClick(ev)
	}
	if ev.Button != MouseButtonLeft {
		return false
	}
	st := l.State.Page
	if st.Width <= 0 || st.Height <= 0 {
		return false
	}
	x, y := ev.X-1, ev.Y-1
	for _, hit := range LayoutChromeWithTabs(st, l.TabBar, l.TabOffset).Hits {
		switch hit.Kind {
		case ChromeHitTab, ChromeHitTabClose, ChromeHitNewTab, ChromeHitTabPrev, ChromeHitTabNext:
		default:
			continue
		}
		if x < hit.X || x >= hit.X+hit.W || y < hit.Y || y >= hit.Y+hit.H {
			continue
		}
		l.activateChromeHit(hit)
		return true
	}
	return false
}

// handleMiddleTabClick closes the tab under a middle-button press. An open
// front-end window owns the pointer first, so the tab strip is out of reach
// until the window is dismissed.
func (l *Tab) handleMiddleTabClick(ev MouseEvent) bool {
	if ev.Button != MouseButtonMiddle || ev.Wheel != 0 || ev.Motion || !ev.Press {
		return false
	}
	st := l.State.Page
	if !st.FrontendChrome || st.Width <= 0 || st.Height <= 0 {
		return false
	}
	x, y := ev.X-1, ev.Y-1
	if x < 0 || x >= st.Width || y < 0 || y >= st.Height {
		return false
	}
	for _, hit := range LayoutChromeWithTabs(st, l.TabBar, l.TabOffset).Hits {
		if hit.Kind != ChromeHitTab && hit.Kind != ChromeHitTabClose {
			continue
		}
		if x < hit.X || x >= hit.X+hit.W || y < hit.Y || y >= hit.Y+hit.H {
			continue
		}
		hit.Kind = ChromeHitTabClose
		l.activateChromeHit(hit)
		return true
	}
	return false
}

// handleInput applies one key or mouse token to this tab. It reports whether
// the tab asked to quit. The SessionLoop owns the terminal channel.
func (l *Tab) handleInput(k string) (quit bool, err error) {
	if l.Session == nil {
		return false, errors.New("nil session")
	}
	if k == "eof" {
		return true, nil
	}
	if ev, ok := ParseMouseToken(k); ok {
		// All-motion reporting sends drag reports for a held side button too;
		// only the initial press may step through history, or one click would
		// skip several entries. The rest of the report is still a side-button
		// event, so keep it off the page.
		if ev.Button == MouseButtonBack {
			if ev.Press && !ev.Motion {
				l.historyBack()
			}
			return false, nil
		}
		if ev.Button == MouseButtonForward {
			if ev.Press && !ev.Motion {
				l.historyForward()
			}
			return false, nil
		}
		if l.handleChromeMouse(ev) {
			quit := l.tabCloseQuit
			l.tabCloseQuit = false
			return quit, nil
		}
		if ev.Button == MouseButtonRight && ev.Press && !ev.Motion {
			l.expectClipboard()
		}
	}
	if handled, quit := l.handleShellKey(k); handled {
		return quit, nil
	}
	if k == "esc" || k == "ctrl-c" {
		l.Session.Abort()
	}
	if !l.State.Page.KeysEngine {
		switch k {
		case "y", "Y", "ctrl-y":
			l.expectClipboard()
		}
	}
	if err := l.Session.Send(protocol.Command{Body: protocol.Key{Token: k}}); err != nil {
		if errors.Is(err, protocol.ErrSessionClosed) {
			return false, nil
		}
		return false, fmt.Errorf("session key: %w", err)
	}
	return false, nil
}

func (l *Tab) applyEvent(ev protocol.Event) (bool, error) {
	switch v := ev.Body.(type) {
	case protocol.Quit:
		return true, nil
	case protocol.Crash:
		msg := protocol.StripControls(v.Message)
		if msg == "" || msg == l.State.LastCrash {
			return false, nil
		}
		// The crash line was written over the alternate screen. Force the next
		// frame to repaint every row so the terminal is usable again.
		if l.Presenter != nil {
			l.Presenter.Reset()
		}
		l.State.LastCrash = msg
		l.CrashMessage = msg
		msg = clipCrashMessage(msg)
		if l.Report != nil {
			l.Report(msg)
		}
	case protocol.State:
		v = l.pageState(v)
		for _, o := range l.overlayList() {
			if o.isOpen() && o.applyState(l, v) {
				return false, nil
			}
		}
		if l.quitPending {
			l.quitBase = v
			if v.KeysEngine {
				l.cancelQuit()
			}
			return false, nil
		}
		l.State.Page = v
		l.State.Scroll = v.Scroll
		l.State.Native = v.Flags&protocol.StateNativeImages != 0
		l.recordHistoryScroll()
		if l.Presenter != nil {
			l.Presenter.SetState(v)
		}
	case protocol.ConsoleView:
		l.consoleOverlay.applyView(l, v)
	case protocol.Hints:
		l.hintOverlay.start(l, v)
	case protocol.Download:
		l.downloadOverlay.start(l, v)
	case protocol.Notice:
		l.noticeOverlay.start(l, v)
	case protocol.Select:
		l.selectOverlay.start(l, v)
	case protocol.ConfigState:
		l.configOverlay.applyConfigState(l, v)
	case protocol.OpenConfig:
		l.configOverlay.start(l)
	case protocol.Dialog:
		l.dialogOverlay.start(l, v)
	case protocol.FindResults:
		if l.findOverlay.isOpen() {
			l.findOverlay.setResults(l, v.Current, v.Total)
		}
	case protocol.HistoryCommit:
		l.applyHistoryCommit(v)
	case protocol.OpenTab:
		if l.Actions.OpenTabBackground != nil && validDisplayURL(v.URL) {
			l.Actions.OpenTabBackground(v.URL)
		}
	case protocol.LinkPreview:
		if validDisplayURL(v.URL) {
			l.linkPreviewOverlay.start(l, v.URL, v.Target)
		}
	case protocol.Clipboard:
		if !l.takeClipboardPermission() {
			return false, nil
		}
		if l.State.Clipboard == nil {
			l.clipboardUnavailable()
			return false, nil
		}
		if _, err := l.State.Clipboard.Write([]byte(OSC52(v.Text))); err != nil {
			l.clipboardUnavailable()
		}
	case protocol.Frame:
		l.consoleOverlay.reset(l)
		if l.Presenter != nil {
			if err := l.Presenter.Present(v, l.State.Scroll, l.State.Native); err != nil {
				return false, err
			}
		}
		l.State.LastCrash = ""
	}
	return false, nil
}

const quitPrompt = "Are you sure you want to quit? (y)"

// handleShellKey consumes base shell keys while the engine is not in a prompt,
// find bar, menu, console, dialog or editable page control. It reports whether
// the key was consumed and whether the loop should exit.
func (l *Tab) handleShellKey(k string) (handled, quit bool) {
	for _, o := range l.overlayList() {
		if !o.isOpen() {
			continue
		}
		if handled, quit := o.handleKey(l, k); handled {
			return handled, quit
		}
	}
	if l.quitPending {
		switch k {
		case "y", "Y", "enter":
			return true, true
		default:
			l.cancelQuit()
			return true, false
		}
	}
	switch k {
	case "ctrl-o", "[":
		l.historyBack()
		return true, false
	case "ctrl-i", "]":
		l.historyForward()
		return true, false
	}
	if l.State.Page.KeysEngine {
		return false, false
	}
	send := func(body protocol.CommandBody) (bool, bool) {
		_ = l.Session.Send(protocol.Command{Body: body})
		return true, false
	}
	switch k {
	case "q":
		l.startQuit()
		return true, false
	case "ctrl-c":
		l.Session.Abort()
		l.startQuit()
		return true, false
	case "/", "ctrl-f":
		l.findOverlay.start(l)
		return true, false
	case "o", "ctrl-l":
		l.promptOverlay.start(l, false)
		return true, false
	case ":":
		l.promptOverlay.start(l, true)
		return true, false
	case "r", "ctrl-r":
		l.reload()
		return true, false
	case "R":
		return send(protocol.App{Action: protocol.AppToggleReader})
	case "D":
		return send(protocol.App{Action: protocol.AppToggleDark})
	case "+":
		return send(protocol.App{Action: protocol.AppZoomIn})
	case "-":
		return send(protocol.App{Action: protocol.AppZoomOut})
	case "0":
		return send(protocol.App{Action: protocol.AppZoomReset})
	case "J":
		return send(protocol.App{Action: protocol.AppToggleJS})
	case "i":
		if l.Actions.ToggleGraphics != nil {
			l.Actions.ToggleGraphics()
			return true, false
		}
		return send(protocol.App{Action: protocol.AppToggleImages})
	case "E":
		return send(protocol.App{Action: protocol.AppStatusDetails})
	case "S":
		return send(protocol.App{Action: protocol.AppDownload})
	case "!":
		l.consoleOverlay.toggle(l)
		return true, false
	case "X":
		l.menuOverlay.start(l, protocol.MenuView)
		return true, false
	case "Z":
		l.menuOverlay.start(l, protocol.MenuZoom)
		return true, false
	case "?":
		if l.Actions.NewTabURL != nil {
			l.Actions.NewTabURL("about:help")
			return true, false
		}
		return send(protocol.Navigate{URL: "about:help", Push: true})
	case "H", "ctrl-h":
		l.historyOverlay.start(l)
		return true, false
	}
	if handled, quit := l.handleTabKey(k); handled {
		return true, quit
	}
	return false, false
}

// handleTabKey applies the tab-manager bindings. It runs after the overlay and
// page-form paths so typing in a prompt or control keeps its own keys.
func (l *Tab) handleTabKey(k string) (handled, quit bool) {
	switch k {
	case "t", "ctrl-t":
		if l.Actions.New != nil {
			l.Actions.New()
		}
		return true, false
	case "W", "ctrl-w":
		if l.Actions.Close != nil {
			return true, l.Actions.Close(-1)
		}
		return true, false
	case "ctrl-n", "alt-right", "ctrl-tab":
		if l.Actions.Cycle != nil {
			l.Actions.Cycle(1)
		}
		return true, false
	case "ctrl-p", "alt-left", "ctrl-backtab":
		if l.Actions.Cycle != nil {
			l.Actions.Cycle(-1)
		}
		return true, false
	}
	if index, ok := tabIndexKey(k); ok {
		if l.Actions.Select != nil {
			l.Actions.Select(index)
		}
		return true, false
	}
	return false, false
}

func tabIndexKey(k string) (int, bool) {
	s := k
	if strings.HasPrefix(s, "alt-") {
		s = s[4:]
	}
	if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		return int(s[0] - '1'), true
	}
	return 0, false
}

// dismissOverlays clears the page overlays that compete with a new modal.
// JS dialogs are answered explicitly and are intentionally left alone.
func (l *Tab) dismissOverlays() {
	l.cancelQuit()
	for _, o := range l.overlayList() {
		if o.dismissOnModal() && o.isOpen() {
			o.close(l)
		}
	}
}

func (l *Tab) startQuit() {
	if l.quitPending {
		return
	}
	l.quitPending = true
	l.quitBase = l.State.Page
	st := l.State.Page
	st.QuitPending = true
	st.FooterLeft = " " + quitPrompt
	st.FooterLevel = protocol.FooterWarning
	st.FooterStatusHit = false
	st.PDFHint, st.PDFHintX = "", -1
	st.KeysEngine = true
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) cancelQuit() {
	if !l.quitPending {
		return
	}
	l.quitPending = false
	l.State.Page = l.quitBase
	if l.Presenter != nil {
		l.Presenter.SetState(l.quitBase)
		_ = l.Presenter.Refresh()
	}
}

// dismissQuitOnClick cancels the quit question on any click press, matching Esc.
// It consumes every pointer report while the question is visible.
func (l *Tab) dismissQuitOnClick(ev MouseEvent) bool {
	if !l.quitPending {
		return false
	}
	if ev.Press && !ev.Motion && ev.Wheel == 0 {
		l.cancelQuit()
	}
	return true
}

// handleChromeMouse consumes a pointer report on the front-end-owned header or
// footer and sends the corresponding action. It reports whether the report was
// consumed; content-area mouse events still go to the engine as key tokens.
func (l *Tab) handleChromeMouse(ev MouseEvent) bool {
	if l.dismissQuitOnClick(ev) {
		return true
	}
	if l.linkPreviewOverlay.isOpen() {
		return l.linkPreviewOverlay.handleMouse(l, ev)
	}
	// A scrollbar drag owns the pointer until release: it keeps tracking when
	// the pointer leaves the track or the content rows, and a release anywhere
	// ends it so a lost release cannot leave the bar following the pointer.
	if l.scrollDrag {
		switch {
		case ev.Wheel != 0:
			l.scrollDrag = false
		case !ev.Press || (ev.Motion && ev.Button != MouseButtonLeft):
			l.scrollDrag = false
			return true
		default:
			if l.handleScrollbarMouse(ev) {
				return true
			}
			l.scrollDrag = false
		}
	}
	// The first open window owns the pointer. The find bar is a footer strip,
	// not a modal, so it only claims its own row and every other open window
	// still outranks it.
	for _, o := range l.overlayList() {
		if o == &l.findOverlay {
			continue
		}
		if o.isOpen() {
			return o.handleMouse(l, ev)
		}
	}
	if l.findOverlay.isOpen() && l.findOverlay.handleMouse(l, ev) {
		// The find bar replaces the footer; it has no clickable labels.
		return true
	}
	if l.handleMiddleTabClick(ev) {
		return true
	}
	st := l.State.Page
	if !st.FrontendChrome || st.PromptActive || st.Width <= 0 || st.Height <= 0 {
		return false
	}
	x, y := ev.X-1, ev.Y-1
	if x < 0 || x >= st.Width || y < 0 || y >= st.Height {
		return false
	}
	if y >= st.HeaderRows && y != st.Height-1 {
		// Content mouse reports still belong to the page while a form field
		// has focus; only the chrome rows stay front-end-owned.
		if st.KeysEngine {
			return false
		}
		return l.handleScrollbarMouse(ev)
	}
	if ev.Wheel != 0 {
		// Wheels scroll the page regardless of the pointer row.
		return false
	}
	if !ev.Press || ev.Motion {
		// Header and footer motion must not reach page hover/selection.
		return true
	}
	if st.KeysEngine {
		// A chrome click acts like Esc for a focused page control, then performs
		// the clicked action in the same event.
		_ = l.Session.Send(protocol.Command{Body: protocol.Key{Token: "esc"}})
	}
	layout := LayoutChromeWithTabs(st, l.TabBar, l.TabOffset)
	for _, hit := range layout.Hits {
		if x < hit.X || x >= hit.X+hit.W || y < hit.Y || y >= hit.Y+hit.H {
			continue
		}
		l.activateChromeHit(hit)
		return true
	}
	return true
}

// handleScrollbarMouse owns the scrollbar track in the last content column.
// It reports whether the report was consumed; other content mouse events pass
// through to the engine.
func (l *Tab) handleScrollbarMouse(ev MouseEvent) bool {
	st := l.State.Page
	if st.MaxScroll <= 0 || st.Width < 2 {
		return false
	}
	contentH := st.Height - st.HeaderRows - 1
	if contentH < 2 || ev.Wheel != 0 {
		return false
	}
	row := ev.Y - 1 - st.HeaderRows
	if l.scrollDrag {
		// A held drag follows the pointer everywhere; scrollTo clamps the row.
		if ev.Press || ev.Motion {
			l.scrollTo(row, contentH)
			return true
		}
		l.scrollDrag = false
		return true
	}
	if ev.X != st.Width || row < 0 || row >= contentH {
		return false
	}
	if !ev.Press || ev.Button != 0 {
		return false
	}
	top, size := scrollThumb(contentH, st.MaxScroll, st.Scroll)
	l.scrollDrag = true
	if row >= top && row < top+size {
		// Grab the thumb where it was pressed instead of recentering it.
		l.scrollGrab = row - top
		return true
	}
	// A track press centers the thumb under the pointer, then keeps it there.
	l.scrollGrab = size / 2
	l.scrollTo(row, contentH)
	return true
}

func (l *Tab) scrollTo(row, contentH int) {
	st := l.State.Page
	max := st.MaxScroll
	_, thumbH := scrollThumb(contentH, max, st.Scroll)
	span := contentH - thumbH
	rel := row - l.scrollGrab
	if span <= 0 {
		rel = 0
	}
	if rel < 0 {
		rel = 0
	}
	if rel > span {
		rel = span
	}
	target := 0
	if span > 0 {
		target = rel * max / span
	}
	_ = l.Session.Send(protocol.Command{Body: protocol.Scroll{To: target, HasTo: true}})
}

func (l *Tab) activateChromeHit(hit ChromeHit) {
	var body protocol.CommandBody
	switch hit.Kind {
	case ChromeHitTab:
		if l.Actions.Select != nil {
			l.Actions.Select(hit.Index)
		}
		return
	case ChromeHitTabClose:
		if l.Actions.Close != nil && l.Actions.Close(hit.Index) {
			l.tabCloseQuit = true
		}
		return
	case ChromeHitNewTab:
		if l.Actions.New != nil {
			l.Actions.New()
		}
		return
	case ChromeHitTabPrev:
		if l.Actions.Scroll != nil {
			l.Actions.Scroll(-1)
		}
		return
	case ChromeHitTabNext:
		if l.Actions.Scroll != nil {
			l.Actions.Scroll(1)
		}
		return
	case ChromeHitBack:
		l.historyBack()
		return
	case ChromeHitForward:
		l.historyForward()
		return
	case ChromeHitReload:
		l.reload()
		return
	case ChromeHitStop:
		body = protocol.App{Action: protocol.AppStop}
	case ChromeHitHome:
		body = protocol.App{Action: protocol.AppHome}
	case ChromeHitHelp:
		if l.Actions.NewTabURL != nil {
			l.Actions.NewTabURL("about:help")
			return
		}
		body = protocol.Navigate{URL: "about:help", Push: true}
	case ChromeHitConfig:
		l.configOverlay.start(l)
		return
	case ChromeHitURL:
		l.promptOverlay.start(l, false)
		return
	case ChromeHitZoom:
		l.menuOverlay.start(l, protocol.MenuZoom)
		return
	case ChromeHitConsole:
		l.consoleOverlay.toggle(l)
		return
	case ChromeHitJS:
		body = protocol.App{Action: protocol.AppToggleJS}
	case ChromeHitReader:
		body = protocol.App{Action: protocol.AppToggleReader}
	case ChromeHitDark:
		body = protocol.App{Action: protocol.AppToggleDark}
	case ChromeHitGraphics:
		if l.Actions.ToggleGraphics != nil {
			l.Actions.ToggleGraphics()
			return
		}
		body = protocol.App{Action: protocol.AppToggleImages}
	case ChromeHitDownload:
		body = protocol.App{Action: protocol.AppDownload}
	case ChromeHitStatus:
		body = protocol.App{Action: protocol.AppStatusDetails}
	default:
		return
	}
	_ = l.Session.Send(protocol.Command{Body: body})
}

func clipCrashMessage(msg string) string {
	const max = 200
	if len(msg) <= max {
		return msg
	}
	runes := []rune(msg)
	if len(runes) <= max {
		return msg
	}
	return string(runes[:max]) + "…"
}
