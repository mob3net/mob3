package frontend

import "browser/internal/protocol"

// downloadOverlay is the download curl hint modal over the page.
type downloadOverlay struct {
	downloadActive bool
	downloadView   DownloadView
	downloadBase   protocol.State
}

func (w *downloadOverlay) isOpen() bool         { return w.downloadActive }
func (w *downloadOverlay) dismissOnModal() bool { return true }
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

func (w *downloadOverlay) close(l *Tab) { l.closeDownload(false, 0) }

func (w *downloadOverlay) applyState(l *Tab, st protocol.State) bool {
	w.downloadBase = st
	if !st.KeysEngine {
		l.closeDownload(false, 0)
		return true
	}
	nextPageState(l, st)
	l.syncDownload()
	return true
}

func (w *downloadOverlay) handleKey(l *Tab, k string) (bool, bool) { return l.handleDownloadKey(k) }

func (w *downloadOverlay) handleMouse(l *Tab, ev MouseEvent) bool { return l.handleDownloadMouse(ev) }

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
	lines := wrapCells(protocol.StripControls(v.Command), inner)
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

// ComposeDownloadFrame stacks the normal screen and draws the curl hint.
func ComposeDownloadFrame(st protocol.State, content protocol.Frame, v *DownloadView) (protocol.Frame, bool) {
	return ComposeOverlayFrame(st, content, func(c *chromeCanvas) { DrawDownload(c, st, v) })
}

// DrawDownload paints the download modal on a full-screen canvas.
func DrawDownload(c *chromeCanvas, st protocol.State, v *DownloadView) MenuLayout {
	layout := downloadLayout(st, v)
	if layout.BoxW <= 0 {
		return layout
	}
	inner := layout.BoxW - 4
	lines := wrapCells(protocol.StripControls(v.Command), inner)
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
			padWidth(truncateWidth(line, inner), inner), fg, protocol.AttrBold)
	}
	c.text(layout.BoxX+2, layout.BoxY+layout.BoxH-2,
		padWidth(truncateWidth(" y: copy curl command · Esc: close ", layout.BoxW-4), layout.BoxW-4), fg, protocol.AttrBold)
	modalDim(c, layout)
	return layout
}
