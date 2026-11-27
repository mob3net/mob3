package frontend

import (
	"browser/internal/protocol"

	"browser/internal/textwidth"
)

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

func (w *noticeOverlay) isOpen() bool       { return w.noticeActive }
func (w *noticeOverlay) layer() windowLayer { return layerNotice }
func (w *noticeOverlay) persistent() bool   { return false }

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

func (w *noticeOverlay) start(l *Tab, v protocol.Notice) {
	l.dismissOverlays()
	w.noticeActive = true
	w.noticeView = NoticeView{Title: v.Title, Message: v.Message}
	w.noticeBase = l.State.Page
	w.sync(l)
}

func (w *noticeOverlay) sync(l *Tab) {
	st := l.State.Page
	st.FrontendChrome = true
	st.KeysEngine = true
	st.Menu = protocol.MenuNone
	st.PromptActive = false
	st.FindActive = false
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		l.Presenter.SetNotice(&w.noticeView)
		_ = l.Presenter.Refresh()
	}
}

func (w *noticeOverlay) close(l *Tab) {
	if !w.noticeActive {
		return
	}
	w.noticeActive = false
	l.State.Page = w.noticeBase
	if l.Presenter != nil {
		l.Presenter.SetNotice(nil)
		l.Presenter.SetState(w.noticeBase)
		_ = l.Presenter.Refresh()
	}
	_ = l.Session.Send(protocol.Command{Body: protocol.ModalClose{Kind: protocol.ModalNotice}})
}

func (w *noticeOverlay) applyState(l *Tab, st protocol.State) bool {
	w.noticeBase = st
	if !st.KeysEngine {
		w.close(l)
		return true
	}
	nextPageState(l, st)
	w.sync(l)
	return true
}

func (w *noticeOverlay) handleKey(l *Tab, k string) (handled, quit bool) {
	switch k {
	case "enter", "esc", "q", "ctrl-c":
		w.close(l)
	}
	return true, false
}

func (w *noticeOverlay) handleMouse(l *Tab, ev MouseEvent) bool {
	if ev.Wheel != 0 || ev.Motion || !ev.Press || ev.Button != MouseButtonLeft {
		return true
	}
	layout := noticeLayout(l.State.Page, &w.noticeView)
	if layout.BoxW <= 0 {
		return true
	}
	x, y := ev.X-1, ev.Y-1
	if x < layout.BoxX || x >= layout.BoxX+layout.BoxW || y < layout.BoxY || y >= layout.BoxY+layout.BoxH {
		w.close(l)
	}
	return true
}

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
	lines := textwidth.Wrap(protocol.StripControls(v.Message), inner)
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

// DrawNotice paints the informational modal on a full-screen canvas.
func DrawNotice(c *chromeCanvas, st protocol.State, v *NoticeView) MenuLayout {
	layout := noticeLayout(st, v)
	if layout.BoxW <= 0 {
		return layout
	}
	inner := layout.BoxW - 4
	lines := textwidth.Wrap(protocol.StripControls(v.Message), inner)
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
		textwidth.Pad(textwidth.Truncate(" Enter / Esc: close ", layout.BoxW-4), layout.BoxW-4), fg, protocol.AttrBold)
	modalDim(c, layout)
	return layout
}
