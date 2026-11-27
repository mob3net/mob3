package frontend

import "browser/internal/protocol"

// windowLayer orders windows from the base surface upwards. Higher layers own
// input first and are painted later.
type windowLayer int

const (
	layerHint windowLayer = iota + 1
	layerConsole
	layerFind
	layerPrompt
	layerLinkPreview
	layerMenu
	layerHistory
	layerConfig
	layerSelect
	layerNotice
	layerDownload
	layerDialog
	layerCrash
)

// overlay is one front-end window layered over the current tab. Each window
// lives in the file that owns its subsystem, keeps its own state and restore
// point, and appears once in allWindows. Key, mouse, page-state, dismissal,
// tab-switch and crash handling walk windowStack, so the layer is the single
// input and paint order instead of a list position.
type overlay interface {
	// isOpen reports whether the window currently covers the page.
	isOpen() bool
	// layer reports the window's stacking order; higher wins input first.
	layer() windowLayer
	// persistent reports whether opening another modal leaves this window open.
	persistent() bool
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
}

// allWindows returns every front-end window in registration order. The order
// only identifies them; windowStack derives priority from layer. The console is
// a full-screen surface, not a window, and is dispatched directly by the tab.
func (l *Tab) allWindows() [11]overlay {
	return [11]overlay{
		&l.linkPreviewOverlay,
		&l.hintOverlay,
		&l.downloadOverlay,
		&l.noticeOverlay,
		&l.selectOverlay,
		&l.configOverlay,
		&l.historyOverlay,
		&l.dialogOverlay,
		&l.promptOverlay,
		&l.findOverlay,
		&l.menuOverlay,
	}
}

// windowStack returns the open windows in input order, topmost first. Equal
// layers keep registration order. The fixed array avoids an allocation on the
// input path.
func (l *Tab) windowStack() (stack [11]overlay, count int) {
	for _, o := range l.allWindows() {
		if !o.isOpen() {
			continue
		}
		at := count
		for at > 0 && stack[at-1].layer() < o.layer() {
			stack[at] = stack[at-1]
			at--
		}
		stack[at] = o
		count++
	}
	return stack, count
}

// nextPageState stores st as the page state while a window stays open.
func nextPageState(l *Tab, st protocol.State) {
	l.State.Page = st
	l.State.Scroll = st.Scroll
	l.State.Native = st.Flags&protocol.StateNativeImages != 0
}
