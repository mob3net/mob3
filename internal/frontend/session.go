package frontend

import (
	"errors"
	"fmt"
	"io"
	"os"

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
			if k == "esc" || k == "ctrl-c" {
				l.Session.Abort()
			}
			if ev, ok := ParseMouseToken(k); ok && l.handleChromeMouse(ev) {
				continue
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

// handleChromeMouse consumes a pointer report on the front-end-owned header or
// footer and sends the corresponding action. It reports whether the report was
// consumed; content-area mouse events still go to the engine as key tokens.
func (l *SessionLoop) handleChromeMouse(ev MouseEvent) bool {
	st := l.State.Page
	if !st.FrontendChrome || st.PromptActive || st.Width <= 0 || st.Height <= 0 {
		return false
	}
	x, y := ev.X-1, ev.Y-1
	if x < 0 || x >= st.Width || y < 0 || y >= st.Height {
		return false
	}
	if y >= st.HeaderRows && y != st.Height-1 {
		return false
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
		body = protocol.App{Action: protocol.AppOpenURL}
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
