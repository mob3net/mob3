package frontend

import (
	"errors"
	"fmt"
	"io"
	"os"

	"browser/internal/protocol"
)

// LoopState is the presentation state the session loop updates as events
// arrive. It is exported for tests and for a future app wiring layer.
type LoopState struct {
	Scroll    int
	Native    bool
	LastCrash string
	Clipboard io.Writer
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
		l.State.Scroll = v.Scroll
		l.State.Native = v.Flags&protocol.StateNativeImages != 0
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
