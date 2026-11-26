package frontend

import "browser/internal/protocol"

// selectOverlay is the page select popup.
type selectOverlay struct {
	selectActive bool
	selectView   SelectView
	selectBase   protocol.State
}

func (w *selectOverlay) isOpen() bool         { return w.selectActive }
func (w *selectOverlay) dismissOnModal() bool { return true }
func (w *selectOverlay) reset(l *Tab) {
	w.selectActive = false
	if l.Presenter != nil {
		l.Presenter.SetSelect(nil)
	}
}

func (w *selectOverlay) install(l *Tab) bool {
	if l.Presenter != nil {
		l.Presenter.SetSelect(&w.selectView)
	}
	return false
}

func (w *selectOverlay) start(l *Tab, v protocol.Select) {
	l.dismissOverlays()
	w.selectActive = true
	w.selectView = SelectView{ID: v.ID, Options: v.Options, Selected: v.Selected, Index: v.Selected}
	if w.selectView.Index < 0 || w.selectView.Index >= len(w.selectView.Options) {
		w.selectView.Index = 0
	}
	w.selectBase = l.State.Page
	w.sync(l)
}

func (w *selectOverlay) sync(l *Tab) {
	st := l.State.Page
	st.FrontendChrome = true
	st.KeysEngine = true
	st.Menu = protocol.MenuNone
	st.PromptActive = false
	st.FindActive = false
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		l.Presenter.SetSelect(&w.selectView)
		_ = l.Presenter.Refresh()
	}
}

func (w *selectOverlay) close(l *Tab) {
	if !w.selectActive {
		return
	}
	w.selectActive = false
	l.State.Page = w.selectBase
	if l.Presenter != nil {
		l.Presenter.SetSelect(nil)
		l.Presenter.SetState(w.selectBase)
		_ = l.Presenter.Refresh()
	}
}

func (w *selectOverlay) answer(l *Tab, index int, cancel bool) {
	id := w.selectView.ID
	w.close(l)
	_ = l.Session.Send(protocol.Command{Body: protocol.SelectResult{ID: id, Index: index, Cancel: cancel}})
}

func (w *selectOverlay) applyState(l *Tab, st protocol.State) bool {
	w.selectBase = st
	if !st.KeysEngine {
		w.close(l)
		return true
	}
	nextPageState(l, st)
	w.sync(l)
	return true
}

func (w *selectOverlay) handleKey(l *Tab, k string) (handled, quit bool) {
	n := len(w.selectView.Options)
	switch k {
	case "esc", "q", "ctrl-c":
		w.answer(l, 0, true)
		return true, false
	case "enter", " ":
		if n > 0 {
			w.answer(l, w.selectView.Index, false)
		}
		return true, false
	}
	if next, ok := moveList(k, w.selectView.Index, n); ok {
		w.selectView.Index = next
	}
	w.sync(l)
	return true, false
}

func (w *selectOverlay) handleMouse(l *Tab, ev MouseEvent) bool {
	if ev.Wheel != 0 {
		delta := 1
		if ev.Wheel < 0 {
			delta = -1
		}
		w.selectView.Index = moveChoice(w.selectView.Index, delta, len(w.selectView.Options))
		w.sync(l)
		return true
	}
	layout := LayoutSelect(l.State.Page, &w.selectView)
	if ev.Motion {
		if index, ok := choiceRowAt(layout, ev); ok && index != w.selectView.Index {
			w.selectView.Index = index
			w.sync(l)
		}
		return true
	}
	if !ev.Press || ev.Button != MouseButtonLeft {
		return true
	}
	x, y := ev.X-1, ev.Y-1
	if x >= layout.Close.X && x < layout.Close.X+layout.Close.W &&
		y >= layout.Close.Y && y < layout.Close.Y+layout.Close.H {
		w.answer(l, 0, true)
		return true
	}
	if index, ok := choiceRowAt(layout, ev); ok {
		w.answer(l, index, false)
		return true
	}
	w.answer(l, 0, true)
	return true
}

// SelectView is the front-end state of one page select popup.
type SelectView struct {
	ID       uint32
	Options  []string
	Selected int
	Index    int
}

// LayoutSelect computes the select popup geometry without drawing it.
func LayoutSelect(st protocol.State, view *SelectView) MenuLayout {
	if view == nil || len(view.Options) == 0 {
		return MenuLayout{}
	}
	rows := make([]MenuRow, len(view.Options))
	for i, opt := range view.Options {
		marker := "  "
		if i == view.Selected {
			marker = "✓ "
		}
		rows[i] = MenuRow{Label: protocol.StripControls(opt), Marker: marker, On: i == view.Selected}
	}
	active := view.Index
	if active < 0 || active >= len(rows) {
		active = 0
	}
	return layoutChoice(st, ChoiceView{
		Title:  "Select an option",
		Hint:   " ↑↓ choose · Enter select · Esc close ",
		Rows:   rows,
		Active: active,
		MinW:   40,
	})
}

// ComposeSelectFrame stacks the normal screen and draws the select popup.
func ComposeSelectFrame(st protocol.State, content protocol.Frame, view *SelectView) (protocol.Frame, bool) {
	return ComposeOverlayFrame(st, content, func(c *chromeCanvas) { DrawSelect(c, st, view) })
}
