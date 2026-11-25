package frontend

import "browser/internal/protocol"

// findOverlay is the in-page find bar.
type findOverlay struct {
	findActive  bool
	findText    string
	findCursor  int
	findCurrent int
	findTotal   int
	findBase    protocol.State
}

func (w *findOverlay) isOpen() bool         { return w.findActive }
func (w *findOverlay) dismissOnModal() bool { return true }

func (w *findOverlay) reset(l *Tab) { w.findActive = false }

// install is a no-op: the find bar travels in protocol.State.
func (w *findOverlay) install(l *Tab) bool { return false }

func (w *findOverlay) close(l *Tab) { l.closeFind() }

func (w *findOverlay) applyState(l *Tab, st protocol.State) bool {
	w.findBase = st
	if st.KeysEngine {
		l.closeFind()
		return true
	}
	nextPageState(l, st)
	l.syncFind()
	return true
}

func (w *findOverlay) handleKey(l *Tab, k string) (bool, bool) { return l.handleFindKey(k) }

// handleMouse claims only the footer row the find bar occupies; every other
// row falls through to the normal chrome handling.
func (w *findOverlay) handleMouse(l *Tab, ev MouseEvent) bool {
	return ev.Y == l.State.Page.Height
}
