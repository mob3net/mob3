package frontend

import (
	"browser/internal/protocol"

	"browser/internal/textwidth"
)

// downloadOverlay is the download curl hint modal over the page.
type downloadOverlay struct {
	downloadActive bool
	downloadView   DownloadView
	downloadBase   protocol.State
}

func (w *downloadOverlay) isOpen() bool       { return w.downloadActive }
func (w *downloadOverlay) layer() windowLayer { return layerDownload }
func (w *downloadOverlay) persistent() bool   { return false }
func (w *downloadOverlay) reset(l *Tab) {
	w.downloadActive = false
	if l.Presenter != nil {
		l.Presenter.SetDownload(nil)
	}
}

func (w *downloadOverlay) install(l *Tab) bool {
	if l.Presenter != nil {
		l.Presenter.SetDownload(&w.downloadView)
	}
	return false
}

func (w *downloadOverlay) start(l *Tab, v protocol.Download) {
	l.dismissOverlays()
	w.downloadActive = true
	w.downloadView = DownloadView{Name: v.Name, Command: v.Command}
	w.downloadBase = l.State.Page
	w.sync(l)
}

func (w *downloadOverlay) sync(l *Tab) {
	st := l.State.Page
	st.FrontendChrome = true
	st.KeysEngine = true
	st.Menu = protocol.MenuNone
	st.PromptActive = false
	st.FindActive = false
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		l.Presenter.SetDownload(&w.downloadView)
		_ = l.Presenter.Refresh()
	}
}

func (w *downloadOverlay) close(l *Tab) { w.closeWith(l, false, 0) }

func (w *downloadOverlay) closeWith(l *Tab, copied bool, chars int) {
	if !w.downloadActive {
		return
	}
	w.downloadActive = false
	l.State.Page = w.downloadBase
	if l.Presenter != nil {
		l.Presenter.SetDownload(nil)
		l.Presenter.SetState(w.downloadBase)
		_ = l.Presenter.Refresh()
	}
	_ = l.Session.Send(protocol.Command{Body: protocol.ModalClose{
		Kind: protocol.ModalDownload, Copied: copied, Chars: chars,
	}})
}

func (w *downloadOverlay) applyState(l *Tab, st protocol.State) bool {
	w.downloadBase = st
	if !st.KeysEngine {
		w.closeWith(l, false, 0)
		return true
	}
	nextPageState(l, st)
	w.sync(l)
	return true
}

func (w *downloadOverlay) handleKey(l *Tab, k string) (handled, quit bool) {
	switch k {
	case "y", "Y":
		chars := 0
		copied := false
		if l.State.Clipboard != nil {
			chars = len([]rune(w.downloadView.Command))
			if _, err := l.State.Clipboard.Write([]byte(OSC52(w.downloadView.Command))); err == nil {
				copied = true
			}
		}
		w.closeWith(l, copied, chars)
		if !copied {
			l.clipboardUnavailable()
		}
	case "esc", "enter", "q", "ctrl-c":
		w.closeWith(l, false, 0)
	}
	return true, false
}

func (w *downloadOverlay) handleMouse(l *Tab, ev MouseEvent) bool {
	if ev.Wheel != 0 || ev.Motion || !ev.Press || ev.Button != MouseButtonLeft {
		return true
	}
	layout := downloadLayout(l.State.Page, &w.downloadView)
	if layout.BoxW <= 0 {
		return true
	}
	x, y := ev.X-1, ev.Y-1
	if x < layout.BoxX || x >= layout.BoxX+layout.BoxW || y < layout.BoxY || y >= layout.BoxY+layout.BoxH {
		w.closeWith(l, false, 0)
	}
	return true
}

// DownloadView is the front-end state of the download curl hint.
type DownloadView struct {
	Name    string
	Command string
}

func downloadLayout(st protocol.State, v *DownloadView) MenuLayout {
	if v == nil || st.Width < 8 {
		return MenuLayout{}
	}
	boxW := minInt(st.Width-4, 82)
	if boxW >= st.Width {
		boxW = st.Width
	}
	if boxW < 8 {
		return MenuLayout{}
	}
	inner := boxW - 4
	lines := textwidth.Wrap(protocol.StripControls(v.Command), inner)
	maxLines := st.Height - 6
	if maxLines < 1 {
		maxLines = 1
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	boxH := 6 + len(lines)
	if boxH > st.Height {
		boxH = st.Height
	}
	x := (st.Width - boxW) / 2
	y := (st.Height - boxH) / 2
	return MenuLayout{
		Title: "Download " + protocol.StripControls(v.Name), Hint: "",
		BoxX: x, BoxY: y, BoxW: boxW, BoxH: boxH,
		Close: protocol.Rect{X: x + boxW - 3, Y: y, W: 3, H: 1},
	}
}

// DrawDownload paints the download modal on a full-screen canvas.
func DrawDownload(c *chromeCanvas, st protocol.State, v *DownloadView) MenuLayout {
	layout := downloadLayout(st, v)
	if layout.BoxW <= 0 {
		return layout
	}
	inner := layout.BoxW - 4
	lines := textwidth.Wrap(protocol.StripControls(v.Command), inner)
	maxLines := st.Height - 6
	if maxLines < 1 {
		maxLines = 1
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	modalFrame(c, layout)
	fg := rgb(0x1f, 0x23, 0x28)
	for i, line := range lines {
		c.text(layout.BoxX+2, layout.BoxY+3+i,
			textwidth.Pad(textwidth.Truncate(line, inner), inner), fg, protocol.AttrBold)
	}
	c.text(layout.BoxX+2, layout.BoxY+layout.BoxH-2,
		textwidth.Pad(textwidth.Truncate(" y: copy curl command · Esc: close ", layout.BoxW-4), layout.BoxW-4), fg, protocol.AttrBold)
	modalDim(c, layout)
	return layout
}
