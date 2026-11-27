package frontend

import "browser/internal/protocol"

// overlay is one front-end window layered over the page. Each window lives in
// the file that owns its subsystem, keeps its own state and restore point, and
// appears once in overlayList. Key, mouse, page-state, dismissal, tab-switch
// and crash handling all walk that one list, so a new window joins the order in
// one place instead of six.
type overlay interface {
	// isOpen reports whether the window currently covers the page.
	isOpen() bool
	// applyState folds a page State event into the window. It reports whether
	// the window consumed the event.
	applyState(l *Tab, st protocol.State) bool
	// handleKey routes one key token to the window.
	handleKey(l *Tab, k string) (handled, quit bool)
	// handleMouse routes one pointer report to the window.
	handleMouse(l *Tab, ev MouseEvent) bool
	// close dismisses the window and restores the saved page state.
	close(l *Tab)
	// install republishes the window's presenter view after a tab switch. It
	// reports that the window owns the whole screen instead of the page.
	install(l *Tab) bool
	// reset drops the window without restoring state or sending commands, for
	// a session that has stopped.
	reset(l *Tab)
	// dismissOnModal reports whether opening another modal closes the window.
	dismissOnModal() bool
}

// overlayList returns every front-end window in dispatch priority order. This
// is the only declaration of which windows exist and which owns input first.
func (l *Tab) overlayList() [12]overlay {
	return [12]overlay{
		&l.linkPreviewOverlay,
		// A JavaScript dialog is drawn over the console and must answer for the
		// keyboard and pointer first; the engine is blocked until it returns.
		&l.dialogOverlay,
		&l.consoleOverlay,
		&l.hintOverlay,
		&l.downloadOverlay,
		&l.noticeOverlay,
		&l.selectOverlay,
		&l.configOverlay,
		&l.historyOverlay,
		&l.promptOverlay,
		&l.findOverlay,
		&l.menuOverlay,
	}
}

// nextPageState stores st as the page state while a window stays open.
func nextPageState(l *Tab, st protocol.State) {
	l.State.Page = st
	l.State.Scroll = st.Scroll
	l.State.Native = st.Flags&protocol.StateNativeImages != 0
}
