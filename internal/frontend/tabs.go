package frontend

import (
	"errors"
	"fmt"
	"os"

	"browser/internal/protocol"
)

// SessionLoop owns the terminal side of the browser: one shared presenter,
// input channels and the active tab. A tab holds only its own page session,
// history and overlay state, so a later tab bar can switch the active pointer.
type SessionLoop struct {
	Tabs   []*Tab
	Active *Tab

	Keys   <-chan string
	Resize <-chan os.Signal
	Quit   <-chan os.Signal

	// TerminalSize supplies the current width/height for a resize command.
	TerminalSize func() (w, h int, ok bool)
	CellW        int
	CellH        int
}

// Run processes keys, resizes and the active tab's events until it quits or
// the terminal closes.
func (l *SessionLoop) Run() error {
	if l.Active == nil || l.Active.Session == nil {
		return errors.New("no active tab")
	}
	if l.Active.Stderr == nil {
		l.Active.Stderr = os.Stderr
	}
	for {
		select {
		case k := <-l.Keys:
			quit, err := l.Active.handleInput(k)
			if err != nil {
				return err
			}
			if quit {
				return nil
			}
		case <-l.Resize:
			if l.TerminalSize == nil {
				continue
			}
			if w, h, ok := l.TerminalSize(); ok {
				if err := l.Active.Session.Send(protocol.Command{Body: l.resizeCommand(w, h)}); err != nil && !errors.Is(err, protocol.ErrSessionClosed) {
					return fmt.Errorf("session resize: %w", err)
				}
			}
		case ev, ok := <-l.Active.Session.Events():
			if !ok {
				return nil
			}
			quit, err := l.Active.applyEvent(ev)
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
