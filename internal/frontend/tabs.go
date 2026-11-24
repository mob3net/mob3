package frontend

import (
	"errors"
	"fmt"
	"os"
	"reflect"

	"browser/internal/protocol"
)

// SessionLoop owns the terminal side of the browser: one shared presenter,
// input channels and the active tab. It drains every tab's event stream so a
// background session never blocks on its queue, but only the active tab draws.
type SessionLoop struct {
	Tabs   []*Tab
	Active *Tab

	// Presenter is the one terminal presenter. It moves between tabs on a
	// switch; the old tab's overlay pointers and native placements are cleared.
	Presenter *Presenter

	Keys   <-chan string
	Resize <-chan os.Signal
	Quit   <-chan os.Signal

	// TerminalSize supplies the current width/height for a resize command.
	TerminalSize func() (w, h int, ok bool)
	CellW        int
	CellH        int

	lastTabs []TabView
}

// Run processes keys, resizes and every tab's events until it quits or the
// terminal closes.
func (l *SessionLoop) Run() error {
	if l.Active == nil || l.Active.Session == nil {
		return errors.New("no active tab")
	}
	if l.Active.Stderr == nil {
		l.Active.Stderr = os.Stderr
	}
	if l.Presenter == nil {
		l.Presenter = l.Active.Presenter
	}
	for _, tab := range l.Tabs {
		if tab != nil && tab.Stderr == nil {
			tab.Stderr = os.Stderr
		}
	}
	l.syncTabBar()

	for {
		cases := make([]reflect.SelectCase, 0, 3+len(l.Tabs))
		kinds := make([]int, 0, 3+len(l.Tabs))
		eventTabs := make([]*Tab, 0, len(l.Tabs))
		add := func(ch any, kind int, tab *Tab) {
			if ch == nil {
				return
			}
			v := reflect.ValueOf(ch)
			if v.Kind() != reflect.Chan || v.IsNil() {
				return
			}
			cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: v})
			kinds = append(kinds, kind)
			eventTabs = append(eventTabs, tab)
		}
		add(l.Keys, 0, nil)
		add(l.Resize, 1, nil)
		add(l.Quit, 2, nil)
		for _, tab := range l.Tabs {
			if tab == nil || tab.Session == nil {
				continue
			}
			add(tab.Session.Events(), 3, tab)
		}
		if len(cases) == 0 {
			return nil
		}
		chosen, recv, ok := reflect.Select(cases)
		switch kinds[chosen] {
		case 0:
			if !ok {
				return nil
			}
			if l.Active == nil {
				return nil
			}
			k, _ := recv.Interface().(string)
			quit, err := l.Active.handleInput(k)
			if err != nil {
				return err
			}
			if quit {
				return nil
			}
		case 1:
			if !ok {
				l.Resize = nil
				continue
			}
			if l.TerminalSize == nil {
				continue
			}
			if w, h, ok := l.TerminalSize(); ok {
				if err := l.Active.Session.Send(protocol.Command{Body: l.resizeCommand(w, h)}); err != nil && !errors.Is(err, protocol.ErrSessionClosed) {
					return fmt.Errorf("session resize: %w", err)
				}
			}
		case 2:
			if !ok {
				l.Quit = nil
				continue
			}
			return nil
		case 3:
			tab := eventTabs[chosen]
			if !ok {
				l.removeTab(tab)
				if l.Active == nil {
					return nil
				}
				continue
			}
			ev, _ := recv.Interface().(protocol.Event)
			if quit, err := l.handleEvent(tab, ev); err != nil {
				return err
			} else if quit {
				return nil
			}
		}
	}
}

// handleEvent applies one event. Background frames are discarded, but state,
// history, titles and overlay events still update that tab's own model.
func (l *SessionLoop) handleEvent(tab *Tab, ev protocol.Event) (bool, error) {
	if tab != l.Active {
		if _, isFrame := ev.Body.(protocol.Frame); isFrame {
			return false, nil
		}
		quit, err := tab.applyEvent(ev)
		if err != nil {
			return false, err
		}
		if quit {
			// A background tab asking to quit is closed; the application keeps
			// running for the other tabs.
			l.removeTab(tab)
			return false, nil
		}
		l.syncTabBar()
		return false, nil
	}
	quit, err := l.Active.applyEvent(ev)
	if err != nil {
		return false, err
	}
	l.syncTabBar()
	return quit, nil
}

func (l *SessionLoop) removeTab(tab *Tab) {
	if tab == nil {
		return
	}
	if tab == l.Active {
		l.Active = nil
	}
	for i, candidate := range l.Tabs {
		if candidate != tab {
			continue
		}
		l.Tabs = append(l.Tabs[:i], l.Tabs[i+1:]...)
		break
	}
	if tab.Session != nil {
		_ = tab.Session.Close()
		tab.Session = nil
	}
	tab.Presenter = nil
	if l.Active == nil && len(l.Tabs) > 0 {
		l.activateTab(0)
	}
	l.syncTabBar()
}

func (l *SessionLoop) selectTab(index int) {
	if index < 0 || index >= len(l.Tabs) {
		return
	}
	l.activateTab(index)
}

func (l *SessionLoop) cycleTab(delta int) {
	if len(l.Tabs) < 2 {
		return
	}
	index := 0
	for i, tab := range l.Tabs {
		if tab == l.Active {
			index = i
			break
		}
	}
	index = (index + delta) % len(l.Tabs)
	if index < 0 {
		index += len(l.Tabs)
	}
	l.activateTab(index)
}

// activateTab moves the shared presenter to another tab. The old tab keeps its
// own state and overlay models but no longer owns any presenter pointers.
func (l *SessionLoop) activateTab(index int) {
	if index < 0 || index >= len(l.Tabs) {
		return
	}
	target := l.Tabs[index]
	if target == nil || target == l.Active {
		return
	}
	if l.Active != nil {
		l.Active.DetachPresenter()
	}
	l.Active = target
	l.syncTabBar()
	if l.Presenter != nil {
		target.AttachPresenter(l.Presenter)
	}
}

func (l *SessionLoop) syncTabBar() {
	views := make([]TabView, 0, len(l.Tabs))
	for _, tab := range l.Tabs {
		if tab == nil {
			continue
		}
		views = append(views, tab.BarView(tab == l.Active))
	}
	oldBar := len(l.lastTabs) > 1
	newBar := len(views) > 1
	if tabsEqual(l.lastTabs, views) {
		return
	}
	l.lastTabs = append(l.lastTabs[:0], views...)
	// The app wires the + button in the next step; do not show a tab bar for
	// the current single-tab session until then, but clear one after switching
	// back from several tabs.
	if l.Presenter != nil && (newBar || oldBar) {
		l.Presenter.SetTabs(views)
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
