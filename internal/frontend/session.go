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
			if ev, ok := ParseMouseToken(k); ok && l.handleChromeMouse(ev) {
				continue
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
				if err := l.Session.Send(protocol.Command{Body: protocol.Resize{
					Width: w, Height: h, CellW: l.CellW, CellH: l.CellH,
				}}); err != nil && !errors.Is(err, protocol.ErrSessionClosed) {
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
		if l.Presenter != nil {
			l.Presenter.SetState(v)
		}
	case protocol.Chrome:
		if l.Presenter != nil {
			l.Presenter.SetChrome(v)
		}
	case protocol.Clipboard:
		if l.State.Clipboard != nil {
			_, _ = l.State.Clipboard.Write([]byte(OSC52(v.Text)))
		}
	case protocol.Frame:
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
	if l.promptActive {
		return l.handlePromptKey(k)
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
	case "o", "ctrl-l":
		l.startPrompt(false)
		return true, false
	case ":":
		l.startPrompt(true)
		return true, false
	case "r":
		return send(protocol.App{Action: protocol.AppReload})
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
		return send(protocol.App{Action: protocol.AppOpenViewMenu})
	case "Z":
		return send(protocol.App{Action: protocol.AppOpenZoom})
	case "?":
		return send(protocol.Navigate{URL: "about:help", Push: true})
	case "H":
		return send(protocol.Navigate{URL: "about:history", Push: true})
	}
	return false, false
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
			_ = l.Session.Send(protocol.Command{Body: protocol.Navigate{URL: text, Push: true}})
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
	if l.quitPending {
		return true
	}
	if l.promptActive {
		return l.handlePromptMouse(ev)
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
		body = protocol.App{Action: protocol.AppBack}
	case ChromeHitForward:
		body = protocol.App{Action: protocol.AppForward}
	case ChromeHitReload:
		body = protocol.App{Action: protocol.AppReload}
	case ChromeHitStop:
		body = protocol.App{Action: protocol.AppStop}
	case ChromeHitHome:
		body = protocol.App{Action: protocol.AppHome}
	case ChromeHitHistory:
		body = protocol.Navigate{URL: "about:history", Push: true}
	case ChromeHitHelp:
		body = protocol.Navigate{URL: "about:help", Push: true}
	case ChromeHitConfig:
		body = protocol.Navigate{URL: "about:config", Push: true}
	case ChromeHitURL:
		l.startPrompt(false)
		return
	case ChromeHitZoom:
		body = protocol.App{Action: protocol.AppOpenZoom}
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
