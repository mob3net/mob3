package frontend

import (
	"fmt"

	"browser/internal/protocol"
)

// findOverlay is the in-page find bar.
type findOverlay struct {
	findActive  bool
	find        findBar
	findCurrent int
	findTotal   int
	findBase    protocol.State
}

func (w *findOverlay) isOpen() bool       { return w.findActive }
func (w *findOverlay) layer() windowLayer { return layerFind }
func (w *findOverlay) persistent() bool   { return false }

func (w *findOverlay) reset(l *Tab) { w.findActive = false }

// install is a no-op: the find bar travels in protocol.State.
func (w *findOverlay) install(l *Tab) bool { return false }

func (w *findOverlay) start(l *Tab) {
	if w.findActive {
		return
	}
	w.findActive = true
	w.findBase = l.State.Page
	w.find.reset()
	w.findCurrent, w.findTotal = 0, 0
	w.sync(l)
	w.find.open(l, protocol.FindPage)
}

func (w *findOverlay) sync(l *Tab) {
	st := l.State.Page
	st.FindActive = true
	st.FindText = w.find.text
	st.FindCursor = w.find.cursor
	st.FindCurrent = w.findCurrent
	st.FindTotal = w.findTotal
	st.KeysEngine = true
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		_ = l.Presenter.Refresh()
	}
}

func (w *findOverlay) close(l *Tab) {
	if !w.findActive {
		return
	}
	w.findActive = false
	st := w.findBase
	if status := w.status(); status != "" {
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

func (w *findOverlay) status() string {
	if w.find.text == "" {
		return ""
	}
	if w.findTotal == 0 {
		return "Find: no matches"
	}
	cur := w.findCurrent
	if cur < 1 {
		cur = 1
	}
	return fmt.Sprintf("Find %d/%d: %s · n/N next/prev", cur, w.findTotal, w.find.text)
}

func (w *findOverlay) setResults(l *Tab, current, total int) {
	w.findCurrent, w.findTotal = current, total
	w.sync(l)
}

func (w *findOverlay) applyState(l *Tab, st protocol.State) bool {
	w.findBase = st
	if st.KeysEngine {
		w.close(l)
		return true
	}
	nextPageState(l, st)
	w.sync(l)
	return true
}

func (w *findOverlay) handleKey(l *Tab, k string) (handled, quit bool) {
	if k == "ctrl-c" {
		l.Session.Abort()
	}
	action, _ := w.find.handleKey(l, k, protocol.FindPage)
	switch action {
	case findKeyClose:
		w.close(l)
		return true, false
	case findKeyNext, findKeyPrev:
		return true, false
	}
	w.sync(l)
	return true, false
}

// handleMouse claims only the footer row the find bar occupies; every other
// row falls through to the normal chrome handling.
func (w *findOverlay) handleMouse(l *Tab, ev MouseEvent) bool {
	return ev.Y == l.State.Page.Height
}
