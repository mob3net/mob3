package frontend

import "browser/internal/protocol"

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
func (l *Tab) openHistory() {
	l.recordHistoryScroll()
	l.cancelQuit()
	l.closePrompt()
	l.closeFind()
	l.closeMenu()
	l.closeSelect()
	l.closeLinkPreview()
	l.closeDownload(false, 0)
	l.closeNotice()
	l.cancelHint()

	view := l.buildHistoryView()
	if len(view.entries) == 0 {
		l.showStatus("No pages in this session yet")
		return
	}
	l.historyActive = true
	l.historyView = view
	l.historyBase = l.State.Page
	l.syncHistory()
}

func (l *Tab) buildHistoryView() historyView {
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

func (l *Tab) syncHistory() {
	st := l.State.Page
	st.FrontendChrome = true
	st.KeysEngine = true
	st.Menu = protocol.MenuNone
	st.PromptActive = false
	st.FindActive = false
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		l.Presenter.SetHistory(&l.historyView)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) closeHistory() {
	if !l.historyActive {
		return
	}
	l.historyActive = false
	l.State.Page = l.historyBase
	if l.Presenter != nil {
		l.Presenter.SetHistory(nil)
		l.Presenter.SetState(l.historyBase)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) chooseHistory(index int) {
	if !l.historyActive || index < 0 || index >= len(l.historyView.entries) {
		return
	}
	target := l.historyView.entries[index].url
	l.closeHistory()
	l.navigate(target)
}

func (l *Tab) handleHistoryKey(k string) (handled, quit bool) {
	n := len(l.historyView.entries)
	switch k {
	case "esc", "q", "ctrl-c":
		l.closeHistory()
		return true, false
	case "up", "k", "ctrl-p":
		if l.historyView.index > 0 {
			l.historyView.index--
		}
	case "down", "j", "ctrl-n":
		if l.historyView.index < n-1 {
			l.historyView.index++
		}
	case "home", "g":
		l.historyView.index = 0
	case "end", "G":
		if n > 0 {
			l.historyView.index = n - 1
		}
	case "enter", " ":
		if n > 0 {
			l.chooseHistory(l.historyView.index)
		}
		return true, false
	}
	l.syncHistory()
	return true, false
}

func (l *Tab) handleHistoryMouse(ev MouseEvent) bool {
	if ev.Wheel != 0 || ev.Motion || !ev.Press || ev.Button != MouseButtonLeft {
		return true
	}
	x, y := ev.X-1, ev.Y-1
	layout := LayoutHistory(l.State.Page, &l.historyView)
	if x >= layout.Close.X && x < layout.Close.X+layout.Close.W &&
		y >= layout.Close.Y && y < layout.Close.Y+layout.Close.H {
		l.closeHistory()
		return true
	}
	for i, r := range layout.Rects {
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			l.chooseHistory(layout.Top + i)
			return true
		}
	}
	l.closeHistory()
	return true
}
