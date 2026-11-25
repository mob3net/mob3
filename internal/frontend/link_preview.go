package frontend

import "browser/internal/protocol"

// linkPreviewOverlay is the footer strip previewing a link destination.
type linkPreviewOverlay struct {
	linkPreviewActive bool
	linkPreviewURL    string
	linkPreviewTarget int
	linkPreviewBase   protocol.State
}

func (w *linkPreviewOverlay) isOpen() bool         { return w.linkPreviewActive }
func (w *linkPreviewOverlay) dismissOnModal() bool { return true }
func (w *linkPreviewOverlay) reset(l *Tab)         { w.linkPreviewActive = false }

// install is a no-op: the preview lives in the footer of protocol.State.
func (w *linkPreviewOverlay) install(l *Tab) bool { return false }

func (w *linkPreviewOverlay) start(l *Tab, url string, target int) {
	l.dismissOverlays()
	l.hintOverlay.cancel(l)
	w.linkPreviewActive = true
	w.linkPreviewURL = url
	w.linkPreviewTarget = target
	w.linkPreviewBase = l.State.Page
	w.sync(l)
}

func (w *linkPreviewOverlay) sync(l *Tab) {
	st := l.State.Page
	st.FrontendChrome = true
	st.KeysEngine = true
	st.Menu = protocol.MenuNone
	st.PromptActive = false
	st.FindActive = false
	st.FooterLeft = " " + w.linkPreviewURL + "  (Enter follows · y copies · Esc cancels)"
	st.FooterLevel = protocol.FooterNormal
	st.FooterStatusHit = false
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		_ = l.Presenter.Refresh()
	}
}

func (w *linkPreviewOverlay) close(l *Tab) {
	if !w.linkPreviewActive {
		return
	}
	w.linkPreviewActive = false
	l.State.Page = w.linkPreviewBase
	if l.Presenter != nil {
		l.Presenter.SetState(w.linkPreviewBase)
		_ = l.Presenter.Refresh()
	}
}

func (w *linkPreviewOverlay) applyState(l *Tab, st protocol.State) bool {
	w.linkPreviewBase = st
	if st.KeysEngine {
		w.close(l)
		return true
	}
	nextPageState(l, st)
	w.sync(l)
	return true
}

func (w *linkPreviewOverlay) handleKey(l *Tab, k string) (handled, quit bool) {
	switch k {
	case "enter":
		target := w.linkPreviewTarget
		w.close(l)
		if l.Session != nil {
			_ = l.Session.Send(protocol.Command{Body: protocol.FollowLink{Target: target}})
		}
	case "y", "Y":
		url := w.linkPreviewURL
		w.close(l)
		l.copyURLToClipboard(url)
	case "esc", "n", "N", "ctrl-c":
		w.close(l)
	default:
		w.close(l)
		return false, false
	}
	return true, false
}

// handleMouse consumes every report while the preview is open and dismisses it
// on a press, matching the link-preview strip's modal footer.
func (w *linkPreviewOverlay) handleMouse(l *Tab, ev MouseEvent) bool {
	if ev.Press {
		w.close(l)
	}
	return true
}

// copyURLToClipboard writes one user-selected URL through OSC 52.
func (l *Tab) copyURLToClipboard(url string) bool {
	if l.State.Clipboard == nil {
		l.clipboardUnavailable()
		return false
	}
	if _, err := l.State.Clipboard.Write([]byte(OSC52(url))); err != nil {
		l.clipboardUnavailable()
		return false
	}
	l.showStatus("Copied URL")
	return true
}
