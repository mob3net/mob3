package frontend

import "browser/internal/protocol"

// NoticeView is the front-end state of a small informational modal.
type NoticeView struct {
	Title   string
	Message string
}

// noticeOverlay is a small informational modal over the page.
type noticeOverlay struct {
	noticeActive bool
	noticeView   NoticeView
	noticeBase   protocol.State
}

func (w *noticeOverlay) isOpen() bool         { return w.noticeActive }
func (w *noticeOverlay) dismissOnModal() bool { return true }

func (w *noticeOverlay) reset(l *Tab) {
	w.noticeActive = false
	if l.Presenter != nil {
		l.Presenter.SetNotice(nil)
	}
}

func (w *noticeOverlay) install(l *Tab) bool {
	if l.Presenter != nil {
		l.Presenter.SetNotice(&w.noticeView)
	}
	return false
}

func (w *noticeOverlay) close(l *Tab) { l.closeNotice() }

func (w *noticeOverlay) applyState(l *Tab, st protocol.State) bool {
	w.noticeBase = st
	if !st.KeysEngine {
		l.closeNotice()
		return true
	}
	nextPageState(l, st)
	l.syncNotice()
	return true
}

func (w *noticeOverlay) handleKey(l *Tab, k string) (bool, bool) { return l.handleNoticeKey(k) }

func (w *noticeOverlay) handleMouse(l *Tab, ev MouseEvent) bool { return l.handleNoticeMouse(ev) }

func noticeLayout(st protocol.State, v *NoticeView) MenuLayout {
	if v == nil || st.Width < 8 {
		return MenuLayout{}
	}
	boxW := minInt(st.Width-4, 68)
	if boxW >= st.Width {
		boxW = st.Width
	}
	if boxW < 8 {
		return MenuLayout{}
	}
	inner := boxW - 4
	lines := wrapCells(protocol.StripControls(v.Message), inner)
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
		Title: protocol.StripControls(v.Title), Hint: "",
		BoxX: x, BoxY: y, BoxW: boxW, BoxH: boxH,
		Close: protocol.Rect{X: x + boxW - 3, Y: y, W: 3, H: 1},
	}
}

// ComposeNoticeFrame stacks the normal screen and draws the notice.
func ComposeNoticeFrame(st protocol.State, content protocol.Frame, v *NoticeView) (protocol.Frame, bool) {
	return ComposeOverlayFrame(st, content, func(c *chromeCanvas) { DrawNotice(c, st, v) })
}

// DrawNotice paints the informational modal on a full-screen canvas.
func DrawNotice(c *chromeCanvas, st protocol.State, v *NoticeView) MenuLayout {
	layout := noticeLayout(st, v)
	if layout.BoxW <= 0 {
		return layout
	}
	inner := layout.BoxW - 4
	lines := wrapCells(protocol.StripControls(v.Message), inner)
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
		padWidth(truncateWidth(" Enter / Esc: close ", layout.BoxW-4), layout.BoxW-4), fg, protocol.AttrBold)
	modalDim(c, layout)
	return layout
}
