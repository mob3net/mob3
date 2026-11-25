package frontend

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"browser/internal/protocol"
	"browser/internal/textedit"
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

	// History is front-end-owned. AttestedURL reports the host's final URL for
	// a document navigation; the engine's HistoryCommit supplies the title,
	// scroll and push flag but never the trusted URL.
	history      []historyEntry
	historyIndex int
	historyLog   *historyLog
	AttestedURL  func(entryID uint64) (string, bool)

	findActive  bool
	findText    string
	findCursor  int
	findCurrent int
	findTotal   int
	findBase    protocol.State

	menuKind    protocol.MenuKind
	menuIndex   int
	menuBase    protocol.State
	overlaySent bool

	dialogActive bool
	dialog       protocol.Dialog
	dialogView   DialogView
	dialogBase   protocol.State

	selectActive bool
	selectView   SelectView
	selectBase   protocol.State

	historyActive bool
	historyView   historyView
	historyBase   protocol.State

	linkPreviewActive bool
	linkPreviewURL    string
	linkPreviewTarget int
	linkPreviewBase   protocol.State

	downloadActive bool
	downloadView   DownloadView
	downloadBase   protocol.State

	noticeActive bool
	noticeView   NoticeView
	noticeBase   protocol.State

	hintActive bool
	hintView   HintView
	hintBase   protocol.State

	consoleActive bool
	consoleView   protocol.ConsoleView
	consolePane   int
	consoleEdit   bool
	consoleText   string
	consoleCursor int
	consoleHist   [5]promptHistory

	consoleFindActive bool
	consoleFindText   string
	consoleFindCursor int

	promptActive    bool
	promptVim       bool
	promptText      string
	promptCursor    int
	promptSelectAll bool
	promptBase      protocol.State
	promptHist      promptHistory

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
	if l.dialogActive {
		p.SetDialog(&l.dialogView)
	}
	if l.selectActive {
		p.SetSelect(&l.selectView)
	}
	if l.historyActive {
		p.SetHistory(&l.historyView)
	}
	if l.downloadActive {
		p.SetDownload(&l.downloadView)
	}
	if l.noticeActive {
		p.SetNotice(&l.noticeView)
	}
	if l.hintActive {
		p.SetHint(&l.hintView)
	}
	if l.consoleActive {
		l.presentConsole()
		return
	}
	_ = p.Refresh()
}

// DetachPresenter removes the shared presenter from this tab without touching
// the presenter itself; the loop resets it before the next tab draws.
func (l *Tab) DetachPresenter() { l.Presenter = nil }

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
	l.hintActive = false
	l.dialogActive = false
	l.selectActive = false
	l.historyActive = false
	l.linkPreviewActive = false
	l.downloadActive = false
	l.noticeActive = false
	l.consoleActive = false
	l.consoleFindActive = false
	l.promptActive = false
	l.findActive = false
	l.menuKind = protocol.MenuNone
	l.quitPending = false
	if l.Presenter != nil {
		l.Presenter.SetHint(nil)
		l.Presenter.SetDialog(nil)
		l.Presenter.SetSelect(nil)
		l.Presenter.SetHistory(nil)
		l.Presenter.SetDownload(nil)
		l.Presenter.SetNotice(nil)
	}
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

// handleMiddleTabClick closes the tab under a middle-button press. It works
// even when an overlay owns input, because the tab strip is always visible.
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
		if l.hintActive {
			l.hintBase = v
			if !v.KeysEngine {
				l.closeHint()
				return false, nil
			}
			l.State.Page = v
			l.State.Scroll = v.Scroll
			l.State.Native = v.Flags&protocol.StateNativeImages != 0
			l.syncHint()
			return false, nil
		}
		if l.downloadActive {
			l.downloadBase = v
			if !v.KeysEngine {
				l.closeDownload(false, 0)
				return false, nil
			}
			l.State.Page = v
			l.State.Scroll = v.Scroll
			l.State.Native = v.Flags&protocol.StateNativeImages != 0
			l.syncDownload()
			return false, nil
		}
		if l.noticeActive {
			l.noticeBase = v
			if !v.KeysEngine {
				l.closeNotice()
				return false, nil
			}
			l.State.Page = v
			l.State.Scroll = v.Scroll
			l.State.Native = v.Flags&protocol.StateNativeImages != 0
			l.syncNotice()
			return false, nil
		}
		if l.selectActive {
			l.selectBase = v
			if !v.KeysEngine {
				l.closeSelect()
				return false, nil
			}
			l.State.Page = v
			l.State.Scroll = v.Scroll
			l.State.Native = v.Flags&protocol.StateNativeImages != 0
			l.syncSelect()
			return false, nil
		}
		if l.historyActive {
			l.historyBase = v
			if v.KeysEngine {
				l.closeHistory()
				return false, nil
			}
			l.State.Page = v
			l.State.Scroll = v.Scroll
			l.State.Native = v.Flags&protocol.StateNativeImages != 0
			l.syncHistory()
			return false, nil
		}
		if l.linkPreviewActive {
			l.linkPreviewBase = v
			if v.KeysEngine {
				l.closeLinkPreview()
				return false, nil
			}
			l.State.Page = v
			l.State.Scroll = v.Scroll
			l.State.Native = v.Flags&protocol.StateNativeImages != 0
			l.syncLinkPreview()
			return false, nil
		}
		if l.menuKind != protocol.MenuNone {
			l.menuBase = v
			if v.KeysEngine {
				l.closeMenu()
				return false, nil
			}
			l.State.Page = v
			l.State.Scroll = v.Scroll
			l.State.Native = v.Flags&protocol.StateNativeImages != 0
			l.syncMenu()
			return false, nil
		}
		if l.findActive {
			l.findBase = v
			if v.KeysEngine {
				l.closeFind()
				return false, nil
			}
			l.State.Page = v
			l.State.Scroll = v.Scroll
			l.State.Native = v.Flags&protocol.StateNativeImages != 0
			l.syncFind()
			return false, nil
		}
		if l.promptActive {
			l.promptBase = v
			if v.KeysEngine {
				l.closePrompt()
				return false, nil
			}
			l.State.Page = v
			l.State.Scroll = v.Scroll
			l.State.Native = v.Flags&protocol.StateNativeImages != 0
			l.syncPrompt()
			return false, nil
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
		l.applyConsoleView(v)
	case protocol.Hints:
		l.startHints(v)
	case protocol.Download:
		l.startDownload(v)
	case protocol.Notice:
		l.startNotice(v)
	case protocol.Select:
		l.startSelect(v)
	case protocol.Dialog:
		l.startDialog(v)
	case protocol.FindResults:
		if l.findActive {
			l.findCurrent, l.findTotal = v.Current, v.Total
			l.syncFind()
		}
	case protocol.HistoryCommit:
		l.applyHistoryCommit(v)
	case protocol.OpenTab:
		if l.Actions.OpenTabBackground != nil && validDisplayURL(v.URL) {
			l.Actions.OpenTabBackground(v.URL)
		}
	case protocol.LinkPreview:
		if validDisplayURL(v.URL) {
			l.startLinkPreview(v.URL, v.Target)
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
		l.consoleActive = false
		l.consoleFindActive = false
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
	if l.linkPreviewActive {
		return l.handleLinkPreviewKey(k)
	}
	if l.consoleActive {
		return l.handleConsoleKey(k)
	}
	if l.hintActive {
		return l.handleHintKey(k)
	}
	if l.downloadActive {
		return l.handleDownloadKey(k)
	}
	if l.noticeActive {
		return l.handleNoticeKey(k)
	}
	if l.selectActive {
		return l.handleSelectKey(k)
	}
	if l.historyActive {
		return l.handleHistoryKey(k)
	}
	if l.dialogActive {
		return l.handleDialogKey(k)
	}
	if l.promptActive {
		return l.handlePromptKey(k)
	}
	if l.findActive {
		return l.handleFindKey(k)
	}
	if l.menuKind != protocol.MenuNone {
		return l.handleMenuKey(k)
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
	case "/":
		l.startFind()
		return true, false
	case "o", "ctrl-l":
		l.startPrompt(false)
		return true, false
	case ":":
		l.startPrompt(true)
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
		return send(protocol.App{Action: protocol.AppToggleConsole})
	case "X":
		l.startMenu(protocol.MenuView)
		return true, false
	case "Z":
		l.startMenu(protocol.MenuZoom)
		return true, false
	case "?":
		if l.Actions.NewTabURL != nil {
			l.Actions.NewTabURL("about:help")
			return true, false
		}
		return send(protocol.Navigate{URL: "about:help", Push: true})
	case "H":
		l.openHistory()
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

func (l *Tab) applyConsoleView(v protocol.ConsoleView) {
	if !l.consoleActive {
		l.consolePane = v.Pane
		l.consoleEdit = false
		l.consoleText = ""
		l.consoleCursor = 0
	}
	l.consoleActive = true
	l.consolePane = v.Pane
	l.consoleView = v
	if l.consoleFindActive {
		l.consoleView.FindActive = true
		l.consoleView.FindText = l.consoleFindText
		l.consoleView.FindCursor = l.consoleFindCursor
	}
	if l.consoleEdit {
		l.consoleView.Input = l.consoleText
		l.consoleView.Cursor = l.consoleCursor
		l.consoleView.Edit = true
		l.consoleView.Hint = ""
	}
	l.presentConsole()
}

func (l *Tab) presentConsole() {
	if l.Presenter == nil {
		return
	}
	l.consoleView.Input = l.consoleText
	l.consoleView.Cursor = l.consoleCursor
	l.consoleView.Edit = l.consoleEdit
	if l.consoleEdit {
		l.consoleView.Hint = ""
	}
	l.consoleView.Pane = l.consolePane
	if l.consoleFindActive {
		l.consoleView.FindActive = true
		l.consoleView.FindText = l.consoleFindText
		l.consoleView.FindCursor = l.consoleFindCursor
	} else {
		l.consoleView.FindActive = false
	}
	_ = l.Presenter.PresentConsole(&l.consoleView)
}

func (l *Tab) consoleAction(a protocol.ConsoleAction) {
	_ = l.Session.Send(protocol.Command{Body: a})
}

func (l *Tab) handleConsoleKey(k string) (handled, quit bool) {
	if l.consoleFindActive {
		return l.handleConsoleFindKey(k)
	}
	if l.consoleView.Quit {
		switch k {
		case "y", "Y", "enter":
			return true, true
		default:
			l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsoleCancelQuit})
			return true, false
		}
	}
	if l.consoleEdit {
		switch k {
		case "esc":
			l.consoleEdit = false
			l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsoleCancelEdit})
			l.presentConsole()
			return true, false
		case "ctrl-c":
			return true, false
		case "tab":
			l.consoleEdit = false
			l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsoleCycle, Delta: 1})
			l.presentConsole()
			return true, false
		case "backtab":
			l.consoleEdit = false
			l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsoleCycle, Delta: -1})
			l.presentConsole()
			return true, false
		case "enter":
			text := strings.TrimSpace(l.consoleText)
			if text != "" {
				l.consoleHist[l.consolePane].record(text)
				l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsoleInput, Text: text})
			}
			l.consoleText = ""
			l.consoleCursor = 0
			l.presentConsole()
			return true, false
		case "up", "ctrl-p":
			if prev, ok := l.consoleHist[l.consolePane].up(l.consoleText); ok {
				l.consoleText = prev
				l.consoleCursor = len(prev)
			}
			l.presentConsole()
			return true, false
		case "down", "ctrl-n":
			if next, ok := l.consoleHist[l.consolePane].down(l.consoleText); ok {
				l.consoleText = next
				l.consoleCursor = len(next)
			}
			l.presentConsole()
			return true, false
		}
		if k == "ctrl-a" {
			k = "home"
		}
		text, cur, _ := textedit.EditLineKey(l.consoleText, l.consoleCursor, k, textedit.EditConfig{MaxRunes: 8192})
		l.consoleText = text
		if cur > len(text) {
			cur = len(text)
		}
		l.consoleCursor = cur
		l.presentConsole()
		return true, false
	}
	switch k {
	case "esc", "!":
		l.consoleActive = false
		l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsoleClose})
	case "q", "ctrl-c":
		l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsoleQuit})
	case "tab", "l", "right":
		l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsoleCycle, Delta: 1})
	case "backtab", "h", "left":
		l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsoleCycle, Delta: -1})
	case ":", "enter":
		if l.consolePane != 4 {
			l.consolePane = 0
		}
		l.consoleEdit = true
		l.consoleText = ""
		l.consoleCursor = 0
		l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsoleFocus})
		l.presentConsole()
	case "k", "up":
		l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsoleScroll, Delta: 1})
	case "j", "down":
		l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsoleScroll, Delta: -1})
	case "g":
		l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsoleTop})
	case "G":
		l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsoleBottom})
	case "y":
		l.expectClipboard()
		l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsoleCopy})
	case "/":
		if l.consolePane == 3 {
			l.consoleFindActive = true
			l.consoleFindText = ""
			l.consoleFindCursor = 0
			_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Query: ""}})
			l.presentConsole()
		}
	case "n":
		if l.consoleView.FindText != "" {
			_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Query: l.consoleView.FindText, Next: true}})
		}
	case "N":
		if l.consoleView.FindText != "" {
			_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Query: l.consoleView.FindText, Prev: true}})
		}
	}
	return true, false
}

// handleConsoleMouse owns pointer reports on the console screen. Tabs switch
// sections, the input row focuses the JS/Tools prompt, the footer >_ icon
// closes the console, and the wheel scrolls the active section. Every report is
// consumed so a click cannot reach the page underneath.
func (l *Tab) handleConsoleMouse(ev MouseEvent) bool {
	v := l.consoleView
	if v.Width <= 0 || v.Height <= 0 || v.Quit || l.consoleFindActive {
		return true
	}
	if ev.Wheel != 0 {
		delta := 3
		if ev.Wheel > 0 {
			delta = -3
		}
		l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsoleScroll, Delta: delta})
		return true
	}
	if ev.Motion || !ev.Press || ev.Button != MouseButtonLeft {
		return true
	}
	x, y := ev.X-1, ev.Y-1
	if x < 0 || y < 0 || x >= v.Width || y >= v.Height {
		return true
	}
	layout := consoleLayoutFor(v.Height, l.consolePane)
	switch {
	case y == layout.tabsY:
		if pane := consoleTabAt(v.Tabs, v.Width, x); pane >= 0 {
			l.consolePane = pane
			l.consoleEdit = false
			l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsolePane, Pane: pane})
			l.presentConsole()
		}
	case layout.inputY >= 0 && y == layout.inputY:
		if l.consolePane != 4 {
			l.consolePane = 0
		}
		l.consoleEdit = true
		l.consoleCursor = len(l.consoleText)
		l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsoleFocus})
		l.presentConsole()
	case y == layout.footerY && v.Width >= 6 && x >= v.Width-stringWidth(">_"):
		l.consoleActive = false
		l.consoleAction(protocol.ConsoleAction{Op: protocol.ConsoleClose})
	}
	return true
}

func (l *Tab) handleConsoleFindKey(k string) (handled, quit bool) {
	switch k {
	case "esc", "enter", "tab", "backtab":
		l.consoleFindActive = false
		l.presentConsole()
		return true, false
	case "down":
		_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Query: l.consoleFindText, Next: true}})
		return true, false
	case "up":
		_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Query: l.consoleFindText, Prev: true}})
		return true, false
	}
	if k == "ctrl-a" {
		k = "home"
	}
	text, cur, _ := textedit.EditLineKey(l.consoleFindText, l.consoleFindCursor, k, textedit.EditConfig{MaxRunes: 512})
	changed := text != l.consoleFindText
	l.consoleFindText = text
	if cur > len(text) {
		cur = len(text)
	}
	l.consoleFindCursor = cur
	if changed {
		_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Query: text}})
	}
	l.presentConsole()
	return true, false
}

func (l *Tab) startHints(v protocol.Hints) {
	l.cancelQuit()
	l.closePrompt()
	l.closeFind()
	l.closeMenu()
	l.closeSelect()
	l.closeHistory()
	l.closeLinkPreview()
	l.closeDownload(false, 0)
	l.closeNotice()
	l.hintActive = true
	l.hintView = HintView{Mode: v.Mode, Prefix: v.Prefix, Items: v.Items}
	l.hintBase = l.State.Page
	if l.hintView.Prefix != "" {
		l.State.Page.FooterLeft = " Choose hint: " + l.hintView.Prefix + " (Esc cancels)"
	}
	l.syncHint()
}

func (l *Tab) syncHint() {
	st := l.State.Page
	st.FrontendChrome = true
	st.KeysEngine = true
	st.Menu = protocol.MenuNone
	st.PromptActive = false
	st.FindActive = false
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		l.Presenter.SetHint(&l.hintView)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) closeHint() {
	if !l.hintActive {
		return
	}
	l.hintActive = false
	l.State.Page = l.hintBase
	if l.Presenter != nil {
		l.Presenter.SetHint(nil)
		l.Presenter.SetState(l.hintBase)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) answerHint(target int) {
	l.closeHint()
	_ = l.Session.Send(protocol.Command{Body: protocol.HintResult{Target: target}})
}

func (l *Tab) cancelHint() {
	if !l.hintActive {
		return
	}
	l.closeHint()
	_ = l.Session.Send(protocol.Command{Body: protocol.HintResult{Cancel: true}})
}

func (l *Tab) handleHintKey(k string) (handled, quit bool) {
	if l.hintView.Preview {
		switch k {
		case "enter":
			l.answerHint(l.hintView.PreviewTarget)
		case "y", "Y":
			l.copyHintURL()
		case "esc", "n", "N", "ctrl-c":
			l.cancelHint()
		default:
			l.cancelHint()
			return false, false
		}
		return true, false
	}
	if k == "esc" || k == "ctrl-c" {
		l.cancelHint()
		return true, false
	}
	if len(k) == 1 {
		c := k[0]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c >= 'a' && c <= 'z' {
			next := l.hintView.Prefix + string(c)
			matches := 0
			for _, item := range l.hintView.Items {
				if strings.HasPrefix(item.Label, next) {
					matches++
				}
			}
			if matches == 0 {
				return true, false
			}
			if matches == 1 {
				var item protocol.HintItem
				for _, it := range l.hintView.Items {
					if strings.HasPrefix(it.Label, next) {
						item = it
						break
					}
				}
				if item.Link && l.hintView.Mode == protocol.HintNewTab {
					l.openHintInNewTab(item.Href)
					return true, false
				}
				if item.Link && l.hintView.Mode == protocol.HintPreview {
					l.previewHint(item)
					return true, false
				}
				l.answerHint(item.Target)
				return true, false
			}
			l.hintView.Prefix = next
			l.State.Page.FooterLeft = " Choose hint: " + next + " (Esc cancels)"
			l.syncHint()
			return true, false
		}
	}
	l.cancelHint()
	return false, false
}

// previewHint shows a chosen link's destination and waits for Enter or y.
func (l *Tab) previewHint(item protocol.HintItem) {
	l.hintView.Preview = true
	l.hintView.PreviewTarget = item.Target
	l.hintView.PreviewHref = item.Href
	l.hintView.Items = nil
	l.State.Page.FooterLeft = " " + item.Href + "  (Enter follows · y copies · Esc cancels)"
	l.syncHint()
}

// copyHintURL cancels the pending hint and copies its destination.
func (l *Tab) copyHintURL() {
	href := l.hintView.PreviewHref
	l.closeHint()
	if l.Session != nil {
		_ = l.Session.Send(protocol.Command{Body: protocol.HintResult{Cancel: true}})
	}
	l.copyURLToClipboard(href)
}

// openHintInNewTab clears the engine hint state and opens a resolved link in a
// background tab.
func (l *Tab) openHintInNewTab(href string) {
	l.closeHint()
	if l.Session != nil {
		_ = l.Session.Send(protocol.Command{Body: protocol.HintResult{Cancel: true}})
	}
	if l.Actions.OpenTabBackground != nil && validDisplayURL(href) {
		l.Actions.OpenTabBackground(href)
	}
}

func (l *Tab) startDownload(v protocol.Download) {
	l.cancelQuit()
	l.closePrompt()
	l.closeFind()
	l.closeMenu()
	l.closeSelect()
	l.closeHistory()
	l.closeLinkPreview()
	l.closeNotice()
	l.downloadActive = true
	l.downloadView = DownloadView{Name: v.Name, Command: v.Command}
	l.downloadBase = l.State.Page
	l.syncDownload()
}

func (l *Tab) syncDownload() {
	st := l.State.Page
	st.FrontendChrome = true
	st.KeysEngine = true
	st.Menu = protocol.MenuNone
	st.PromptActive = false
	st.FindActive = false
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		l.Presenter.SetDownload(&l.downloadView)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) closeDownload(copied bool, chars int) {
	if !l.downloadActive {
		return
	}
	l.downloadActive = false
	l.State.Page = l.downloadBase
	if l.Presenter != nil {
		l.Presenter.SetDownload(nil)
		l.Presenter.SetState(l.downloadBase)
		_ = l.Presenter.Refresh()
	}
	_ = l.Session.Send(protocol.Command{Body: protocol.ModalClose{
		Kind: protocol.ModalDownload, Copied: copied, Chars: chars,
	}})
}

func (l *Tab) handleDownloadKey(k string) (handled, quit bool) {
	switch k {
	case "y", "Y":
		chars := 0
		copied := false
		if l.State.Clipboard != nil {
			chars = len([]rune(l.downloadView.Command))
			if _, err := l.State.Clipboard.Write([]byte(OSC52(l.downloadView.Command))); err == nil {
				copied = true
			}
		}
		l.closeDownload(copied, chars)
		if !copied {
			l.clipboardUnavailable()
		}
	case "esc", "enter", "q", "ctrl-c":
		l.closeDownload(false, 0)
	}
	return true, false
}

func (l *Tab) handleDownloadMouse(ev MouseEvent) bool {
	if ev.Wheel != 0 || ev.Motion || !ev.Press || ev.Button != MouseButtonLeft {
		return true
	}
	layout := downloadLayout(l.State.Page, &l.downloadView)
	if layout.BoxW <= 0 {
		return true
	}
	x, y := ev.X-1, ev.Y-1
	if x < layout.BoxX || x >= layout.BoxX+layout.BoxW || y < layout.BoxY || y >= layout.BoxY+layout.BoxH {
		l.closeDownload(false, 0)
	}
	return true
}

func (l *Tab) startNotice(v protocol.Notice) {
	l.cancelQuit()
	l.closePrompt()
	l.closeFind()
	l.closeMenu()
	l.closeSelect()
	l.closeHistory()
	l.closeLinkPreview()
	l.closeDownload(false, 0)
	l.noticeActive = true
	l.noticeView = NoticeView{Title: v.Title, Message: v.Message}
	l.noticeBase = l.State.Page
	l.syncNotice()
}

func (l *Tab) syncNotice() {
	st := l.State.Page
	st.FrontendChrome = true
	st.KeysEngine = true
	st.Menu = protocol.MenuNone
	st.PromptActive = false
	st.FindActive = false
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		l.Presenter.SetNotice(&l.noticeView)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) closeNotice() {
	if !l.noticeActive {
		return
	}
	l.noticeActive = false
	l.State.Page = l.noticeBase
	if l.Presenter != nil {
		l.Presenter.SetNotice(nil)
		l.Presenter.SetState(l.noticeBase)
		_ = l.Presenter.Refresh()
	}
	_ = l.Session.Send(protocol.Command{Body: protocol.ModalClose{Kind: protocol.ModalNotice}})
}

func (l *Tab) handleNoticeKey(k string) (handled, quit bool) {
	switch k {
	case "enter", "esc", "q", "ctrl-c":
		l.closeNotice()
	}
	return true, false
}

func (l *Tab) handleNoticeMouse(ev MouseEvent) bool {
	if ev.Wheel != 0 || ev.Motion || !ev.Press || ev.Button != MouseButtonLeft {
		return true
	}
	layout := noticeLayout(l.State.Page, &l.noticeView)
	if layout.BoxW <= 0 {
		return true
	}
	x, y := ev.X-1, ev.Y-1
	if x < layout.BoxX || x >= layout.BoxX+layout.BoxW || y < layout.BoxY || y >= layout.BoxY+layout.BoxH {
		l.closeNotice()
	}
	return true
}

func (l *Tab) startSelect(v protocol.Select) {
	l.cancelQuit()
	l.closePrompt()
	l.closeFind()
	l.closeMenu()
	l.closeHistory()
	l.closeLinkPreview()
	l.selectActive = true
	l.selectView = SelectView{ID: v.ID, Options: v.Options, Selected: v.Selected, Index: v.Selected}
	if l.selectView.Index < 0 || l.selectView.Index >= len(l.selectView.Options) {
		l.selectView.Index = 0
	}
	l.selectBase = l.State.Page
	l.syncSelect()
}

func (l *Tab) syncSelect() {
	st := l.State.Page
	st.FrontendChrome = true
	st.KeysEngine = true
	st.Menu = protocol.MenuNone
	st.PromptActive = false
	st.FindActive = false
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		l.Presenter.SetSelect(&l.selectView)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) closeSelect() {
	if !l.selectActive {
		return
	}
	l.selectActive = false
	l.State.Page = l.selectBase
	if l.Presenter != nil {
		l.Presenter.SetSelect(nil)
		l.Presenter.SetState(l.selectBase)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) answerSelect(index int, cancel bool) {
	id := l.selectView.ID
	l.closeSelect()
	_ = l.Session.Send(protocol.Command{Body: protocol.SelectResult{ID: id, Index: index, Cancel: cancel}})
}

func (l *Tab) handleSelectKey(k string) (handled, quit bool) {
	n := len(l.selectView.Options)
	switch k {
	case "esc", "q", "ctrl-c":
		l.answerSelect(0, true)
		return true, false
	case "up", "k", "ctrl-p":
		l.selectView.Index = moveChoice(l.selectView.Index, -1, n)
	case "down", "j", "ctrl-n":
		l.selectView.Index = moveChoice(l.selectView.Index, 1, n)
	case "home", "g":
		l.selectView.Index = 0
	case "end", "G":
		if n > 0 {
			l.selectView.Index = n - 1
		}
	case "enter", " ":
		if n > 0 {
			l.answerSelect(l.selectView.Index, false)
		}
		return true, false
	}
	l.syncSelect()
	return true, false
}

func (l *Tab) handleSelectMouse(ev MouseEvent) bool {
	if ev.Wheel != 0 {
		delta := 1
		if ev.Wheel < 0 {
			delta = -1
		}
		l.selectView.Index = moveChoice(l.selectView.Index, delta, len(l.selectView.Options))
		l.syncSelect()
		return true
	}
	layout := LayoutSelect(l.State.Page, &l.selectView)
	if ev.Motion {
		if index, ok := choiceRowAt(layout, ev); ok && index != l.selectView.Index {
			l.selectView.Index = index
			l.syncSelect()
		}
		return true
	}
	if !ev.Press || ev.Button != MouseButtonLeft {
		return true
	}
	x, y := ev.X-1, ev.Y-1
	if x >= layout.Close.X && x < layout.Close.X+layout.Close.W &&
		y >= layout.Close.Y && y < layout.Close.Y+layout.Close.H {
		l.answerSelect(0, true)
		return true
	}
	if index, ok := choiceRowAt(layout, ev); ok {
		l.answerSelect(index, false)
		return true
	}
	l.answerSelect(0, true)
	return true
}

func (l *Tab) startDialog(v protocol.Dialog) {
	l.cancelQuit()
	l.closePrompt()
	l.closeFind()
	l.closeMenu()
	l.closeSelect()
	l.closeHistory()
	l.closeLinkPreview()
	l.closeDownload(false, 0)
	l.closeNotice()
	l.cancelHint()
	l.dialogActive = true
	l.dialog = v
	l.dialogBase = l.State.Page
	l.dialogView = DialogView{
		ID: v.ID, Kind: v.Kind, Text: v.Text,
		Value: v.Default, Cursor: len(v.Default), SelectAll: v.Kind == protocol.DialogPrompt,
	}
	l.syncDialog()
}

func (l *Tab) syncDialog() {
	st := l.State.Page
	st.FrontendChrome = true
	st.KeysEngine = true
	st.Menu = protocol.MenuNone
	st.PromptActive = false
	st.FindActive = false
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		l.Presenter.SetDialog(&l.dialogView)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) closeDialog() {
	if !l.dialogActive {
		return
	}
	l.dialogActive = false
	l.State.Page = l.dialogBase
	if l.Presenter != nil {
		l.Presenter.SetDialog(nil)
		l.Presenter.SetState(l.dialogBase)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) answerDialog(ok bool, text string) {
	id := l.dialog.ID
	l.closeDialog()
	_ = l.Session.Send(protocol.Command{Body: protocol.DialogResult{ID: id, OK: ok, Text: text}})
}

func (l *Tab) handleDialogKey(k string) (handled, quit bool) {
	switch l.dialog.Kind {
	case protocol.DialogAlert:
		switch k {
		case "enter", " ", "esc":
			l.answerDialog(true, "")
		}
		return true, false
	case protocol.DialogConfirm:
		switch k {
		case "enter", "y", "Y":
			l.answerDialog(true, "")
		case "esc", "n", "N", "ctrl-c":
			l.answerDialog(false, "")
		}
		return true, false
	case protocol.DialogPrompt:
		switch k {
		case "enter":
			l.answerDialog(true, l.dialogView.Value)
			return true, false
		case "esc", "ctrl-c":
			l.answerDialog(false, "")
			return true, false
		}
		value, cur, _ := textedit.EditLineKey(l.dialogView.Value, l.dialogView.Cursor, k, textedit.EditConfig{
			SelAll:            &l.dialogView.SelectAll,
			CollapseSelection: true,
		})
		l.dialogView.Value = value
		if cur > len(value) {
			cur = len(value)
		}
		l.dialogView.Cursor = cur
		l.syncDialog()
		return true, false
	}
	return true, false
}

func (l *Tab) handleDialogMouse(ev MouseEvent) bool {
	if ev.Wheel != 0 || ev.Motion || !ev.Press || ev.Button != MouseButtonLeft {
		return true
	}
	layout := LayoutDialog(l.State.Page, &l.dialogView)
	x, y := ev.X-1, ev.Y-1
	if l.dialog.Kind == protocol.DialogPrompt &&
		layout.PromptW > 0 &&
		x >= layout.PromptX && x < layout.PromptX+layout.PromptW &&
		y == layout.PromptY {
		rel := x - layout.PromptX
		l.dialogView.Cursor = textOffsetAt(l.dialogView.Value, l.dialogView.Cursor, l.dialogView.SelectAll, rel, layout.PromptW)
		l.dialogView.SelectAll = false
		l.syncDialog()
		return true
	}
	if layout.BoxW > 0 &&
		x >= layout.BoxX && x < layout.BoxX+layout.BoxW &&
		y >= layout.BoxY && y < layout.BoxY+layout.BoxH {
		if x >= layout.Close.X && x < layout.Close.X+layout.Close.W &&
			y >= layout.Close.Y && y < layout.Close.Y+layout.Close.H {
			l.answerDialog(l.dialog.Kind == protocol.DialogAlert, "")
			return true
		}
		return true
	}
	l.answerDialog(l.dialog.Kind == protocol.DialogAlert, "")
	return true
}

func zoomPresetIndex(percent int) int {
	best, idx := 0, 0
	for i, pct := range zoomPresets {
		d := pct - percent
		if d < 0 {
			d = -d
		}
		if i == 0 || d < best {
			best, idx = d, i
		}
	}
	return idx
}

func (l *Tab) startMenu(kind protocol.MenuKind) {
	if l.menuKind != protocol.MenuNone {
		return
	}
	l.menuKind = kind
	l.menuBase = l.State.Page
	l.menuIndex = 0
	if kind == protocol.MenuZoom {
		l.menuIndex = zoomPresetIndex(l.State.Page.ZoomPercent)
	}
	l.syncMenu()
}

// notifyOverlay tells the engine whether a front-end window covers the page.
// The engine then repaints native images as half-block art and scaled text at
// normal size, so neither can composite above the window.
func (l *Tab) notifyOverlay(open bool) {
	if l.overlaySent == open {
		return
	}
	l.overlaySent = open
	arg := 0
	if open {
		arg = 1
	}
	_ = l.Session.Send(protocol.Command{Body: protocol.App{Action: protocol.AppFrontendOverlay, Arg: arg}})
}

func (l *Tab) syncMenu() {
	l.notifyOverlay(true)
	st := l.State.Page
	st.Menu = l.menuKind
	st.MenuIndex = l.menuIndex
	st.KeysEngine = true
	st.FrontendChrome = true
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) closeMenu() {
	if l.menuKind == protocol.MenuNone {
		return
	}
	l.menuKind = protocol.MenuNone
	l.notifyOverlay(false)
	l.State.Page = l.menuBase
	if l.Presenter != nil {
		l.Presenter.SetState(l.menuBase)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) handleMenuKey(k string) (handled, quit bool) {
	if k == "ctrl-c" {
		l.Session.Abort()
	}
	switch k {
	case "esc", "q", "ctrl-c":
		l.closeMenu()
		return true, false
	case "X":
		if l.menuKind == protocol.MenuView {
			l.closeMenu()
			return true, false
		}
	case "up", "k", "ctrl-p":
		l.menuIndex = moveChoice(l.menuIndex, -1, menuRowCount(l.menuKind))
		l.syncMenu()
		return true, false
	case "down", "j", "ctrl-n":
		l.menuIndex = moveChoice(l.menuIndex, 1, menuRowCount(l.menuKind))
		l.syncMenu()
		return true, false
	case "home", "g":
		l.menuIndex = 0
		l.syncMenu()
		return true, false
	case "end", "G":
		l.menuIndex = menuRowCount(l.menuKind) - 1
		l.syncMenu()
		return true, false
	case " ", "enter":
		l.activateMenu()
		return true, false
	}
	return true, false
}

func menuRowCount(kind protocol.MenuKind) int {
	if kind == protocol.MenuZoom {
		return len(zoomPresets)
	}
	return 3
}

func (l *Tab) activateMenu() {
	if l.menuKind == protocol.MenuView {
		switch l.menuIndex {
		case 0:
			_ = l.Session.Send(protocol.Command{Body: protocol.App{Action: protocol.AppToggleFixed}})
		case 1:
			_ = l.Session.Send(protocol.Command{Body: protocol.App{Action: protocol.AppToggleSticky}})
		case 2:
			_ = l.Session.Send(protocol.Command{Body: protocol.App{Action: protocol.AppToggleBackgrounds}})
		}
		return
	}
	if l.menuKind == protocol.MenuZoom && l.menuIndex >= 0 && l.menuIndex < len(zoomPresets) {
		pct := zoomPresets[l.menuIndex]
		l.closeMenu()
		_ = l.Session.Send(protocol.Command{Body: protocol.App{Action: protocol.AppZoomSet, Arg: pct}})
	}
}

func (l *Tab) handleMenuMouse(ev MouseEvent) bool {
	if ev.Wheel != 0 {
		delta := 1
		if ev.Wheel < 0 {
			delta = -1
		}
		l.menuIndex = moveChoice(l.menuIndex, delta, menuRowCount(l.menuKind))
		l.syncMenu()
		return true
	}
	layout := LayoutMenu(l.State.Page)
	if ev.Motion {
		if index, ok := choiceRowAt(layout, ev); ok && index != l.menuIndex {
			l.menuIndex = index
			l.syncMenu()
		}
		return true
	}
	if !ev.Press || ev.Button != MouseButtonLeft {
		return true
	}
	x, y := ev.X-1, ev.Y-1
	if x >= layout.Close.X && x < layout.Close.X+layout.Close.W &&
		y >= layout.Close.Y && y < layout.Close.Y+layout.Close.H {
		l.closeMenu()
		return true
	}
	if index, ok := choiceRowAt(layout, ev); ok {
		l.menuIndex = index
		l.activateMenu()
		return true
	}
	l.closeMenu()
	return true
}

func (l *Tab) startFind() {
	if l.findActive {
		return
	}
	l.findActive = true
	l.findBase = l.State.Page
	l.findText = ""
	l.findCursor = 0
	l.findCurrent, l.findTotal = 0, 0
	l.syncFind()
	_ = l.Session.Send(protocol.Command{Body: protocol.Find{Query: ""}})
}

func (l *Tab) syncFind() {
	st := l.State.Page
	st.FindActive = true
	st.FindText = l.findText
	st.FindCursor = l.findCursor
	st.FindCurrent = l.findCurrent
	st.FindTotal = l.findTotal
	st.KeysEngine = true
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) closeFind() {
	if !l.findActive {
		return
	}
	l.findActive = false
	st := l.findBase
	if status := l.findStatus(); status != "" {
		st.FooterLeft = " " + status
		st.FooterLevel = protocol.FooterNormal
		st.FooterStatusHit = false
	}
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) findStatus() string {
	if l.findText == "" {
		return ""
	}
	if l.findTotal == 0 {
		return "Find: no matches"
	}
	cur := l.findCurrent
	if cur < 1 {
		cur = 1
	}
	return fmt.Sprintf("Find %d/%d: %s", cur, l.findTotal, l.findText)
}

func (l *Tab) handleFindKey(k string) (handled, quit bool) {
	if k == "ctrl-c" {
		l.Session.Abort()
	}
	switch k {
	case "esc", "enter":
		l.closeFind()
		return true, false
	case "down":
		_ = l.Session.Send(protocol.Command{Body: protocol.Find{Query: l.findText, Next: true}})
		return true, false
	case "up":
		_ = l.Session.Send(protocol.Command{Body: protocol.Find{Query: l.findText, Prev: true}})
		return true, false
	case "tab", "backtab":
		l.closeFind()
		return true, false
	}
	if k == "ctrl-a" {
		k = "home"
	}
	text, cur, _ := textedit.EditLineKey(l.findText, l.findCursor, k, textedit.EditConfig{MaxRunes: 512})
	changed := text != l.findText
	l.findText = text
	if cur > len(text) {
		cur = len(text)
	}
	l.findCursor = cur
	if changed {
		_ = l.Session.Send(protocol.Command{Body: protocol.Find{Query: text}})
	}
	l.syncFind()
	return true, false
}

func (l *Tab) startPrompt(vim bool) {
	l.startPromptValue(l.State.Page.URL, vim, true)
}

// startPromptEmpty opens the URL prompt with no text, used by a fresh tab so
// the next keystrokes go straight into the address.
func (l *Tab) startPromptEmpty() {
	l.startPromptValue("", false, false)
}

func (l *Tab) startPromptValue(text string, vim, selectAll bool) {
	if l.promptActive {
		return
	}
	l.promptActive = true
	l.promptVim = vim
	l.promptBase = l.State.Page
	l.promptText = text
	l.promptCursor = len(l.promptText)
	l.promptSelectAll = selectAll
	l.promptHist.reset()
	l.syncPrompt()
}

func (l *Tab) syncPrompt() {
	st := l.State.Page
	st.PromptActive = true
	st.PromptText = l.promptText
	st.PromptCursor = l.promptCursor
	st.PromptSelectAll = l.promptSelectAll
	st.KeysEngine = true
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) closePrompt() {
	if !l.promptActive {
		return
	}
	l.promptActive = false
	l.promptVim = false
	l.promptText = ""
	l.promptCursor = 0
	l.promptSelectAll = false
	l.State.Page = l.promptBase
	if l.Presenter != nil {
		l.Presenter.SetState(l.promptBase)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) handlePromptKey(k string) (handled, quit bool) {
	switch k {
	case "enter":
		text := strings.TrimSpace(l.promptText)
		q := l.promptVim && (text == "q" || text == ":q")
		l.closePrompt()
		if q {
			return true, true
		}
		if text != "" {
			l.promptHist.record(text)
			l.navigate(text)
		}
		return true, false
	case "esc":
		l.closePrompt()
		return true, false
	case "ctrl-c":
		l.Session.Abort()
		l.closePrompt()
		return true, false
	case "up", "ctrl-p":
		if prev, ok := l.promptHist.up(l.promptText); ok {
			l.promptText = prev
			l.promptCursor = len(prev)
		}
		l.promptSelectAll = false
		l.syncPrompt()
		return true, false
	case "down", "ctrl-n":
		if next, ok := l.promptHist.down(l.promptText); ok {
			l.promptText = next
			l.promptCursor = len(next)
		}
		l.promptSelectAll = false
		l.syncPrompt()
		return true, false
	}
	text, cur, _ := textedit.EditLineKey(l.promptText, l.promptCursor, k, textedit.EditConfig{
		SelAll:            &l.promptSelectAll,
		CollapseSelection: true,
	})
	l.promptText = text
	if cur > len(text) {
		cur = len(text)
	}
	l.promptCursor = cur
	l.syncPrompt()
	return true, false
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
	if l.linkPreviewActive {
		if ev.Press {
			l.closeLinkPreview()
		}
		return true
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
	if l.handleMiddleTabClick(ev) {
		return true
	}
	if l.hintActive {
		l.cancelHint()
		return false
	}
	if l.downloadActive {
		return l.handleDownloadMouse(ev)
	}
	if l.noticeActive {
		return l.handleNoticeMouse(ev)
	}
	if l.selectActive {
		return l.handleSelectMouse(ev)
	}
	if l.historyActive {
		return l.handleHistoryMouse(ev)
	}
	if l.dialogActive {
		return l.handleDialogMouse(ev)
	}
	if l.menuKind != protocol.MenuNone {
		return l.handleMenuMouse(ev)
	}
	if l.promptActive {
		return l.handlePromptMouse(ev)
	}
	if l.consoleActive {
		return l.handleConsoleMouse(ev)
	}
	if l.findActive && ev.Y == l.State.Page.Height {
		// The find bar replaces the footer; it has no clickable labels.
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

func (l *Tab) handlePromptMouse(ev MouseEvent) bool {
	if ev.Wheel != 0 {
		return false
	}
	if ev.Motion || !ev.Press {
		return true
	}
	st := l.State.Page
	x, y := ev.X-1, ev.Y-1
	for _, hit := range LayoutChromeWithTabs(st, l.TabBar, l.TabOffset).Hits {
		if x < hit.X || x >= hit.X+hit.W || y < hit.Y || y >= hit.Y+hit.H {
			continue
		}
		switch hit.Kind {
		case ChromeHitURL:
			l.promptCursor = promptOffsetAt(l.promptText, l.promptCursor, l.promptSelectAll, x-hit.X, hit.W)
			l.promptSelectAll = false
			l.syncPrompt()
			return true
		case ChromeHitZoom:
			l.closePrompt()
			_ = l.Session.Send(protocol.Command{Body: protocol.App{Action: protocol.AppOpenZoom}})
			return true
		default:
			// A click on another chrome control closes the prompt and runs that
			// control in the same event, like Esc followed by the click.
			l.closePrompt()
			l.activateChromeHit(hit)
			return true
		}
	}
	l.closePrompt()
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
	case ChromeHitHistory:
		l.openHistory()
		return
	case ChromeHitHelp:
		if l.Actions.NewTabURL != nil {
			l.Actions.NewTabURL("about:help")
			return
		}
		body = protocol.Navigate{URL: "about:help", Push: true}
	case ChromeHitConfig:
		if l.Actions.NewTabURL != nil {
			l.Actions.NewTabURL("about:config")
			return
		}
		body = protocol.Navigate{URL: "about:config", Push: true}
	case ChromeHitURL:
		l.startPrompt(false)
		return
	case ChromeHitZoom:
		l.startMenu(protocol.MenuZoom)
		return
	case ChromeHitConsole:
		body = protocol.App{Action: protocol.AppToggleConsole}
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
