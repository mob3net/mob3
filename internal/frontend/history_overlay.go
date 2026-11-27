package frontend

import "browser/internal/protocol"

// historyOverlay is the front-end session-history popup.
type historyOverlay struct {
	historyActive bool
	historyView   historyView
	historyBase   protocol.State
}

func (w *historyOverlay) isOpen() bool       { return w.historyActive }
func (w *historyOverlay) layer() windowLayer { return layerHistory }
func (w *historyOverlay) persistent() bool   { return false }
func (w *historyOverlay) reset(l *Tab) {
	w.historyActive = false
	if l.Presenter != nil {
		l.Presenter.SetHistory(nil)
	}
}

func (w *historyOverlay) install(l *Tab) bool {
	if l.Presenter != nil {
		l.Presenter.SetHistory(&w.historyView)
	}
	return false
}

func (w *historyOverlay) close(l *Tab) {
	if !w.historyActive {
		return
	}
	w.historyActive = false
	l.State.Page = w.historyBase
	if l.Presenter != nil {
		l.Presenter.SetHistory(nil)
		l.Presenter.SetState(w.historyBase)
		_ = l.Presenter.Refresh()
	}
}

func (w *historyOverlay) applyState(l *Tab, st protocol.State) bool {
	w.historyBase = st
	if st.KeysEngine {
		w.close(l)
		return true
	}
	nextPageState(l, st)
	w.sync(l)
	return true
}

func (w *historyOverlay) handleKey(l *Tab, k string) (handled, quit bool) {
	n := len(w.historyView.entries)
	switch k {
	case "esc", "q", "ctrl-c":
		w.close(l)
		return true, false
	case "enter", " ":
		if n > 0 {
			w.choose(l, w.historyView.index)
		}
		return true, false
	}
	if next, ok := moveList(k, w.historyView.index, n); ok {
		w.historyView.index = next
	}
	w.sync(l)
	return true, false
}

func (w *historyOverlay) handleMouse(l *Tab, ev MouseEvent) bool {
	if ev.Wheel != 0 || ev.Motion || !ev.Press || ev.Button != MouseButtonLeft {
		return true
	}
	x, y := ev.X-1, ev.Y-1
	layout := LayoutHistory(l.State.Page, &w.historyView)
	if x >= layout.Close.X && x < layout.Close.X+layout.Close.W &&
		y >= layout.Close.Y && y < layout.Close.Y+layout.Close.H {
		w.close(l)
		return true
	}
	for i, r := range layout.Rects {
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			w.choose(l, layout.Top+i)
			return true
		}
	}
	w.close(l)
	return true
}

// historyView is one front-end history popup. Entries are newest first, and
// indices map each row back to the session history slice.
type historyView struct {
	entries []historyEntry
	current int
	index   int
}

// LayoutHistory computes the history popup geometry without drawing it.
func LayoutHistory(st protocol.State, view *historyView) MenuLayout {
	if view == nil || len(view.entries) == 0 {
		return MenuLayout{}
	}
	rows := make([]MenuRow, len(view.entries))
	for i, entry := range view.entries {
		marker, on := "  ", false
		if i == view.current {
			marker, on = "● ", true
		}
		rows[i] = MenuRow{Label: protocol.StripControls(entry.url), Marker: marker, On: on}
	}
	active := view.index
	if active < 0 || active >= len(rows) {
		active = 0
	}
	return layoutChoice(st, ChoiceView{
		Title:  "History",
		Hint:   " ↑↓ choose · Enter open · Esc close ",
		Rows:   rows,
		Active: active,
		MinW:   50,
	})
}

// ComposeHistoryFrame stacks the normal screen and draws the history popup.
func ComposeHistoryFrame(st protocol.State, content protocol.Frame, view *historyView) (protocol.Frame, bool) {
	return ComposeOverlayFrame(st, content, func(c *chromeCanvas) { DrawHistory(c, st, view) })
}

// DrawHistory paints the history popup over a full-screen canvas.
func DrawHistory(c *chromeCanvas, st protocol.State, view *historyView) MenuLayout {
	layout := LayoutHistory(st, view)
	drawChoiceLayout(c, layout)
	return layout
}

// openHistory builds the popup from the current session history.
// start builds the popup from the current session history.
func (w *historyOverlay) start(l *Tab) {
	l.recordHistoryScroll()
	l.dismissOverlays()
	l.hintOverlay.cancel(l)

	view := w.build(l)
	if len(view.entries) == 0 {
		l.showStatus("No pages in this session yet")
		return
	}
	w.historyActive = true
	w.historyView = view
	w.historyBase = l.State.Page
	w.sync(l)
}

func (w *historyOverlay) build(l *Tab) historyView {
	view := historyView{current: -1}
	all := l.historyLogEntries()
	start := 0
	if len(all) > historyLimit {
		start = len(all) - historyLimit
	}
	currentID := l.currentHistoryID()
	for i := len(all) - 1; i >= start; i-- {
		entry := all[i]
		if entry.url == "" || isAboutHistory(entry.url) {
			continue
		}
		view.entries = append(view.entries, entry)
		if entry.id == currentID {
			view.current = len(view.entries) - 1
			view.index = view.current
		}
	}
	if view.current < 0 && len(view.entries) > 0 {
		view.index = 0
	}
	return view
}

func (w *historyOverlay) sync(l *Tab) {
	st := l.State.Page
	st.FrontendChrome = true
	st.KeysEngine = true
	st.Menu = protocol.MenuNone
	st.PromptActive = false
	st.FindActive = false
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		l.Presenter.SetHistory(&w.historyView)
		_ = l.Presenter.Refresh()
	}
}

func (w *historyOverlay) choose(l *Tab, index int) {
	if !w.historyActive || index < 0 || index >= len(w.historyView.entries) {
		return
	}
	target := w.historyView.entries[index].url
	w.close(l)
	l.navigate(target)
}
