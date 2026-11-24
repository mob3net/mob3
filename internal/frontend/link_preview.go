package frontend

import "browser/internal/protocol"

func (l *Tab) startLinkPreview(url string, target int) {
	if l.linkPreviewActive {
		l.closeLinkPreview()
	}
	l.cancelQuit()
	l.closePrompt()
	l.closeFind()
	l.closeMenu()
	l.closeSelect()
	l.closeHistory()
	l.closeDownload(false, 0)
	l.closeNotice()
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
