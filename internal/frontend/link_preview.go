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

func (w *linkPreviewOverlay) close(l *Tab) { l.closeLinkPreview() }

func (w *linkPreviewOverlay) applyState(l *Tab, st protocol.State) bool {
	w.linkPreviewBase = st
	if st.KeysEngine {
		l.closeLinkPreview()
		return true
	}
	nextPageState(l, st)
	l.syncLinkPreview()
	return true
}

func (w *linkPreviewOverlay) handleKey(l *Tab, k string) (bool, bool) {
	return l.handleLinkPreviewKey(k)
}

// handleMouse consumes every report while the preview is open and dismisses it
// on a press, matching the link-preview strip's modal footer.
func (w *linkPreviewOverlay) handleMouse(l *Tab, ev MouseEvent) bool {
	if ev.Press {
		l.closeLinkPreview()
	}
	return true
}

func (l *Tab) startLinkPreview(url string, target int) {
	l.dismissOverlays()
	l.cancelHint()
	l.linkPreviewActive = true
	l.linkPreviewURL = url
	l.linkPreviewTarget = target
	l.linkPreviewBase = l.State.Page
	l.syncLinkPreview()
}

func (l *Tab) syncLinkPreview() {
	st := l.State.Page
	st.FrontendChrome = true
	st.KeysEngine = true
	st.Menu = protocol.MenuNone
	st.PromptActive = false
	st.FindActive = false
	st.FooterLeft = " " + l.linkPreviewURL + "  (Enter follows · y copies · Esc cancels)"
	st.FooterLevel = protocol.FooterNormal
	st.FooterStatusHit = false
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) closeLinkPreview() {
	if !l.linkPreviewActive {
		return
	}
	l.linkPreviewActive = false
	l.State.Page = l.linkPreviewBase
	if l.Presenter != nil {
		l.Presenter.SetState(l.linkPreviewBase)
		_ = l.Presenter.Refresh()
	}
}

func (l *Tab) handleLinkPreviewKey(k string) (handled, quit bool) {
	switch k {
	case "enter":
		target := l.linkPreviewTarget
		l.closeLinkPreview()
		if l.Session != nil {
			_ = l.Session.Send(protocol.Command{Body: protocol.FollowLink{Target: target}})
		}
	case "y", "Y":
		url := l.linkPreviewURL
		l.closeLinkPreview()
		l.copyURLToClipboard(url)
	case "esc", "n", "N", "ctrl-c":
		l.closeLinkPreview()
	default:
		l.closeLinkPreview()
		return false, false
	}
	return true, false
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
