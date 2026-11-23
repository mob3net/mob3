package frontend

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

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

// SessionLoop drives one protocol session from the terminal. It owns the
// event loop; the caller owns terminal setup, session construction and the
// initial Init/Navigate commands.
type SessionLoop struct {
	Session   protocol.Session
	Presenter *Presenter
	Keys      <-chan string
	Resize    <-chan os.Signal
	Quit      <-chan os.Signal

	// TerminalSize supplies the current width/height for a resize command.
	TerminalSize func() (w, h int, ok bool)
	CellW        int
	CellH        int

	Stderr io.Writer
	State  LoopState

	quitPending bool
	quitBase    protocol.State
	scrollDrag  bool

	// History is front-end-owned: the engine receives entry IDs for its page
	// cache and reports the final URL with HistoryCommit.
	history      []historyEntry
	historyIndex int

	findActive  bool
	findText    string
	findCursor  int
	findCurrent int
	findTotal   int
	findBase    protocol.State

	menuKind  protocol.MenuKind
	menuIndex int
	menuBase  protocol.State

	dialogActive bool
	dialog       protocol.Dialog
	dialogView   DialogView
	dialogBase   protocol.State

	selectActive bool
	selectView   SelectView
	selectBase   protocol.State

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

	// Report handles a contained crash. When nil, the loop writes one short
	// line to Stderr and sends a ClientError command back to the session.
	Report func(string)
}

// Run processes keys, resizes and session events until the session quits or
// the terminal closes.
func (l *SessionLoop) Run() error {
	if l.Session == nil {
		return errors.New("nil session")
	}
	if l.Stderr == nil {
		l.Stderr = os.Stderr
	}
	for {
		select {
		case k := <-l.Keys:
			if k == "eof" {
				return nil
			}
			if ev, ok := ParseMouseToken(k); ok {
				if ev.Press && ev.Button == MouseButtonBack {
					l.historyBack()
					continue
				}
				if ev.Press && ev.Button == MouseButtonForward {
					l.historyForward()
					continue
				}
				if l.handleChromeMouse(ev) {
					continue
				}
			}
			if handled, quit := l.handleShellKey(k); handled {
				if quit {
					return nil
				}
				continue
			}
			if k == "esc" || k == "ctrl-c" {
				l.Session.Abort()
			}
			if err := l.Session.Send(protocol.Command{Body: protocol.Key{Token: k}}); err != nil {
				if errors.Is(err, protocol.ErrSessionClosed) {
					continue
				}
				return fmt.Errorf("session key: %w", err)
			}
		case <-l.Resize:
			if l.TerminalSize == nil {
				continue
			}
			if w, h, ok := l.TerminalSize(); ok {
				if err := l.Session.Send(protocol.Command{Body: l.resizeCommand(w, h)}); err != nil && !errors.Is(err, protocol.ErrSessionClosed) {
					return fmt.Errorf("session resize: %w", err)
				}
			}
		case ev, ok := <-l.Session.Events():
			if !ok {
				return nil
			}
			quit, err := l.applyEvent(ev)
			if err != nil {
				return err
			}
			if quit {
				return nil
			}
		case <-l.Quit:
			return nil
		}
	}
}

// resizeCommand builds the terminal resize plus the front-end-owned content
// rectangle.
func (l *SessionLoop) resizeCommand(w, h int) protocol.Resize {
	top, contentH := ContentBounds(h)
	return protocol.Resize{
		Width: w, Height: h, ContentTop: top, ContentHeight: contentH,
		CellW: l.CellW, CellH: l.CellH,
	}
}

func (l *SessionLoop) applyEvent(ev protocol.Event) (bool, error) {
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
		msg = clipCrashMessage(msg)
		if l.Report != nil {
			l.Report(msg)
		} else {
			fmt.Fprintf(l.Stderr, "mob3: page engine: %s\n", msg)
			if l.Session != nil {
				_ = l.Session.Send(protocol.Command{Body: protocol.ClientError{Message: msg}})
			}
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
	case protocol.Clipboard:
		if l.State.Clipboard != nil {
			_, _ = l.State.Clipboard.Write([]byte(OSC52(v.Text)))
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
func (l *SessionLoop) handleShellKey(k string) (handled, quit bool) {
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
	case "r":
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
		return send(protocol.App{Action: protocol.AppToggleImages})
	case "E":
		return send(protocol.App{Action: protocol.AppStatusDetails})
	case "d":
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
		return send(protocol.Navigate{URL: "about:help", Push: true})
	case "H":
		l.openHistory()
		return true, false
	}
	return false, false
}

func (l *SessionLoop) applyConsoleView(v protocol.ConsoleView) {
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

func (l *SessionLoop) presentConsole() {
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

func (l *SessionLoop) consoleAction(a protocol.ConsoleAction) {
	_ = l.Session.Send(protocol.Command{Body: a})
}

func (l *SessionLoop) handleConsoleKey(k string) (handled, quit bool) {
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
		text, cur, _ := editLineKey(l.consoleText, l.consoleCursor, k, editConfig{maxRunes: 8192})
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
			_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Next: true}})
		}
	case "N":
		if l.consoleView.FindText != "" {
			_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Prev: true}})
		}
	}
	return true, false
}

func (l *SessionLoop) handleConsoleFindKey(k string) (handled, quit bool) {
	switch k {
	case "esc", "enter", "tab", "backtab":
		l.consoleFindActive = false
		l.presentConsole()
		return true, false
	case "down":
		_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Next: true}})
		return true, false
	case "up":
		_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Prev: true}})
		return true, false
	}
	if k == "ctrl-a" {
		k = "home"
	}
	text, cur, _ := editLineKey(l.consoleFindText, l.consoleFindCursor, k, editConfig{maxRunes: 512})
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

func (l *SessionLoop) startHints(v protocol.Hints) {
	l.cancelQuit()
	l.closePrompt()
	l.closeFind()
	l.closeMenu()
	l.closeSelect()
	l.closeDownload(false, 0)
	l.closeNotice()
	l.hintActive = true
	l.hintView = HintView{Ask: v.Ask, Prefix: v.Prefix, Items: v.Items}
	l.hintBase = l.State.Page
	if l.hintView.Prefix != "" {
		l.State.Page.FooterLeft = " Choose hint: " + l.hintView.Prefix + " (Esc cancels)"
	}
	l.syncHint()
}

func (l *SessionLoop) syncHint() {
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

func (l *SessionLoop) closeHint() {
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

func (l *SessionLoop) answerHint(target int) {
	l.closeHint()
	_ = l.Session.Send(protocol.Command{Body: protocol.HintResult{Target: target}})
}

func (l *SessionLoop) cancelHint() {
	if !l.hintActive {
		return
	}
	l.closeHint()
	_ = l.Session.Send(protocol.Command{Body: protocol.HintResult{Cancel: true}})
}

func (l *SessionLoop) handleHintKey(k string) (handled, quit bool) {
	if l.hintView.Confirm {
		switch k {
		case "enter", "y", "Y":
			l.answerHint(l.hintView.ConfirmTarget)
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
				if l.hintView.Ask && item.Link {
					l.hintView.Confirm = true
					l.hintView.ConfirmTarget = item.Target
					l.hintView.ConfirmHref = item.Href
					l.hintView.Items = nil
					l.State.Page.FooterLeft = " " + item.Href + "  (Enter/y follows · Esc cancels)"
					l.syncHint()
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

func (l *SessionLoop) startDownload(v protocol.Download) {
	l.cancelQuit()
	l.closePrompt()
	l.closeFind()
	l.closeMenu()
	l.closeSelect()
	l.closeNotice()
	l.downloadActive = true
	l.downloadView = DownloadView{Name: v.Name, Command: v.Command}
	l.downloadBase = l.State.Page
	l.syncDownload()
}

func (l *SessionLoop) syncDownload() {
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

func (l *SessionLoop) closeDownload(copied bool, chars int) {
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

func (l *SessionLoop) handleDownloadKey(k string) (handled, quit bool) {
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
	case "esc", "enter", "q", "ctrl-c":
		l.closeDownload(false, 0)
	}
	return true, false
}

func (l *SessionLoop) handleDownloadMouse(ev MouseEvent) bool {
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

func (l *SessionLoop) startNotice(v protocol.Notice) {
	l.cancelQuit()
	l.closePrompt()
	l.closeFind()
	l.closeMenu()
	l.closeSelect()
	l.closeDownload(false, 0)
	l.noticeActive = true
	l.noticeView = NoticeView{Title: v.Title, Message: v.Message}
	l.noticeBase = l.State.Page
	l.syncNotice()
}

func (l *SessionLoop) syncNotice() {
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

func (l *SessionLoop) closeNotice() {
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

func (l *SessionLoop) handleNoticeKey(k string) (handled, quit bool) {
	switch k {
	case "enter", "esc", "q", "ctrl-c":
		l.closeNotice()
	}
	return true, false
}

func (l *SessionLoop) handleNoticeMouse(ev MouseEvent) bool {
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

func (l *SessionLoop) startSelect(v protocol.Select) {
	l.cancelQuit()
	l.closePrompt()
	l.closeFind()
	l.closeMenu()
	l.selectActive = true
	l.selectView = SelectView{ID: v.ID, Options: v.Options, Selected: v.Selected, Index: v.Selected}
	if l.selectView.Index < 0 || l.selectView.Index >= len(l.selectView.Options) {
		l.selectView.Index = 0
	}
	l.selectBase = l.State.Page
	l.syncSelect()
}

func (l *SessionLoop) syncSelect() {
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

func (l *SessionLoop) closeSelect() {
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

func (l *SessionLoop) answerSelect(index int, cancel bool) {
	id := l.selectView.ID
	l.closeSelect()
	_ = l.Session.Send(protocol.Command{Body: protocol.SelectResult{ID: id, Index: index, Cancel: cancel}})
}

func (l *SessionLoop) handleSelectKey(k string) (handled, quit bool) {
	n := len(l.selectView.Options)
	switch k {
	case "esc", "q", "ctrl-c":
		l.answerSelect(0, true)
		return true, false
	case "up", "k", "ctrl-p":
		if l.selectView.Index > 0 {
			l.selectView.Index--
		}
	case "down", "j", "ctrl-n":
		if l.selectView.Index < n-1 {
			l.selectView.Index++
		}
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

func (l *SessionLoop) handleSelectMouse(ev MouseEvent) bool {
	if ev.Wheel != 0 || ev.Motion || !ev.Press || ev.Button != MouseButtonLeft {
		return true
	}
	x, y := ev.X-1, ev.Y-1
	layout := LayoutSelect(l.State.Page, &l.selectView)
	if x >= layout.Close.X && x < layout.Close.X+layout.Close.W &&
		y >= layout.Close.Y && y < layout.Close.Y+layout.Close.H {
		l.answerSelect(0, true)
		return true
	}
	for i, r := range layout.Rects {
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			l.answerSelect(layout.Top+i, false)
			return true
		}
	}
	l.answerSelect(0, true)
	return true
}

func (l *SessionLoop) startDialog(v protocol.Dialog) {
	l.cancelQuit()
	l.closePrompt()
	l.closeFind()
	l.closeMenu()
	l.closeSelect()
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

func (l *SessionLoop) syncDialog() {
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

func (l *SessionLoop) closeDialog() {
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

func (l *SessionLoop) answerDialog(ok bool, text string) {
	id := l.dialog.ID
	l.closeDialog()
	_ = l.Session.Send(protocol.Command{Body: protocol.DialogResult{ID: id, OK: ok, Text: text}})
}

func (l *SessionLoop) handleDialogKey(k string) (handled, quit bool) {
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
		value, cur, _ := editLineKey(l.dialogView.Value, l.dialogView.Cursor, k, editConfig{
			selAll:            &l.dialogView.SelectAll,
			collapseSelection: true,
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

func (l *SessionLoop) handleDialogMouse(ev MouseEvent) bool {
	if ev.Wheel != 0 || ev.Motion || !ev.Press || ev.Button != MouseButtonLeft {
		return true
	}
	layout := LayoutDialog(l.State.Page, &l.dialogView)
	x, y := ev.X-1, ev.Y-1
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

func (l *SessionLoop) startMenu(kind protocol.MenuKind) {
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

func (l *SessionLoop) syncMenu() {
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

func (l *SessionLoop) closeMenu() {
	if l.menuKind == protocol.MenuNone {
		return
	}
	l.menuKind = protocol.MenuNone
	l.State.Page = l.menuBase
	if l.Presenter != nil {
		l.Presenter.SetState(l.menuBase)
		_ = l.Presenter.Refresh()
	}
}

func (l *SessionLoop) handleMenuKey(k string) (handled, quit bool) {
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
		if l.menuIndex > 0 {
			l.menuIndex--
		}
		l.syncMenu()
		return true, false
	case "down", "j", "ctrl-n":
		if l.menuIndex < menuRowCount(l.menuKind)-1 {
			l.menuIndex++
		}
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

func (l *SessionLoop) activateMenu() {
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

func (l *SessionLoop) handleMenuMouse(ev MouseEvent) bool {
	if ev.Wheel != 0 || ev.Motion || !ev.Press || ev.Button != MouseButtonLeft {
		return true
	}
	x, y := ev.X-1, ev.Y-1
	layout := LayoutMenu(l.State.Page)
	if x >= layout.Close.X && x < layout.Close.X+layout.Close.W &&
		y >= layout.Close.Y && y < layout.Close.Y+layout.Close.H {
		l.closeMenu()
		return true
	}
	for i, r := range layout.Rects {
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			l.menuIndex = layout.Top + i
			l.activateMenu()
			return true
		}
	}
	l.closeMenu()
	return true
}

func (l *SessionLoop) startFind() {
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

func (l *SessionLoop) syncFind() {
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

func (l *SessionLoop) closeFind() {
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

func (l *SessionLoop) findStatus() string {
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

func (l *SessionLoop) handleFindKey(k string) (handled, quit bool) {
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
	text, cur, _ := editLineKey(l.findText, l.findCursor, k, editConfig{maxRunes: 512})
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

func (l *SessionLoop) startPrompt(vim bool) {
	if l.promptActive {
		return
	}
	l.promptActive = true
	l.promptVim = vim
	l.promptBase = l.State.Page
	l.promptText = l.State.Page.URL
	l.promptCursor = len(l.promptText)
	l.promptSelectAll = true
	l.promptHist.reset()
	l.syncPrompt()
}

func (l *SessionLoop) syncPrompt() {
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

func (l *SessionLoop) closePrompt() {
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

func (l *SessionLoop) handlePromptKey(k string) (handled, quit bool) {
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
	text, cur, _ := editLineKey(l.promptText, l.promptCursor, k, editConfig{
		selAll:            &l.promptSelectAll,
		collapseSelection: true,
	})
	l.promptText = text
	if cur > len(text) {
		cur = len(text)
	}
	l.promptCursor = cur
	l.syncPrompt()
	return true, false
}

func (l *SessionLoop) startQuit() {
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

func (l *SessionLoop) cancelQuit() {
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

// handleChromeMouse consumes a pointer report on the front-end-owned header or
// footer and sends the corresponding action. It reports whether the report was
// consumed; content-area mouse events still go to the engine as key tokens.
func (l *SessionLoop) handleChromeMouse(ev MouseEvent) bool {
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
	if l.dialogActive {
		return l.handleDialogMouse(ev)
	}
	if l.quitPending {
		return true
	}
	if l.menuKind != protocol.MenuNone {
		return l.handleMenuMouse(ev)
	}
	if l.promptActive {
		return l.handlePromptMouse(ev)
	}
	if l.findActive && ev.Y == l.State.Page.Height {
		// The find bar replaces the footer; it has no clickable labels.
		return true
	}
	st := l.State.Page
	if !st.FrontendChrome || st.KeysEngine || st.PromptActive || st.Width <= 0 || st.Height <= 0 {
		return false
	}
	x, y := ev.X-1, ev.Y-1
	if x < 0 || x >= st.Width || y < 0 || y >= st.Height {
		return false
	}
	if y >= st.HeaderRows && y != st.Height-1 {
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
	layout := LayoutChrome(st)
	for _, hit := range layout.Hits {
		if x < hit.X || x >= hit.X+hit.W || y < hit.Y || y >= hit.Y+hit.H {
			continue
		}
		l.activateChromeHit(hit.Kind)
		return true
	}
	return true
}

func (l *SessionLoop) handlePromptMouse(ev MouseEvent) bool {
	if ev.Wheel != 0 {
		return false
	}
	if ev.Motion || !ev.Press {
		return true
	}
	st := l.State.Page
	x, y := ev.X-1, ev.Y-1
	for _, hit := range LayoutChrome(st).Hits {
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
		}
	}
	l.closePrompt()
	return true
}

// handleScrollbarMouse owns the scrollbar track in the last content column.
// It reports whether the report was consumed; other content mouse events pass
// through to the engine.
func (l *SessionLoop) handleScrollbarMouse(ev MouseEvent) bool {
	st := l.State.Page
	if st.MaxScroll <= 0 || st.Width < 2 {
		return false
	}
	contentH := st.Height - st.HeaderRows - 1
	if contentH < 2 || ev.X != st.Width {
		return false
	}
	row := ev.Y - 1 - st.HeaderRows
	if row < 0 || row >= contentH {
		return false
	}
	if ev.Wheel != 0 {
		return false
	}
	if !l.scrollDrag {
		if !ev.Press || ev.Button != 0 {
			return false
		}
		l.scrollDrag = true
		l.scrollTo(row, contentH)
		return true
	}
	if ev.Press || ev.Motion {
		l.scrollTo(row, contentH)
		return true
	}
	l.scrollDrag = false
	return true
}

func (l *SessionLoop) scrollTo(row, contentH int) {
	st := l.State.Page
	max := st.MaxScroll
	thumbH := contentH * contentH / (contentH + max)
	if thumbH < 1 {
		thumbH = 1
	}
	if thumbH > contentH {
		thumbH = contentH
	}
	span := contentH - thumbH
	rel := row - thumbH/2
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

func (l *SessionLoop) activateChromeHit(kind ChromeHitKind) {
	var body protocol.CommandBody
	switch kind {
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
		body = protocol.Navigate{URL: "about:help", Push: true}
	case ChromeHitConfig:
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
