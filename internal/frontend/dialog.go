package frontend

import (
	"browser/internal/protocol"
	"browser/internal/textedit"
	"browser/internal/textwidth"
)

// dialogOverlay is a JavaScript alert, confirm or prompt modal.
type dialogOverlay struct {
	dialogActive bool
	dialog       protocol.Dialog
	dialogView   DialogView
	dialogBase   protocol.State
}

func (w *dialogOverlay) isOpen() bool       { return w.dialogActive }
func (w *dialogOverlay) layer() windowLayer { return layerDialog }
func (w *dialogOverlay) persistent() bool   { return true }
func (w *dialogOverlay) reset(l *Tab) {
	w.dialogActive = false
	if l.Presenter != nil {
		l.Presenter.SetDialog(nil)
	}
}

func (w *dialogOverlay) install(l *Tab) bool {
	if l.Presenter != nil {
		l.Presenter.SetDialog(&w.dialogView)
	}
	return false
}

func (w *dialogOverlay) start(l *Tab, v protocol.Dialog) {
	l.dismissOverlays()
	l.hintOverlay.cancel(l)
	w.dialogActive = true
	w.dialog = v
	w.dialogBase = l.State.Page
	w.dialogView = DialogView{
		ID: v.ID, Kind: v.Kind, Text: v.Text,
		Value: v.Default, Cursor: len(v.Default), SelectAll: v.Kind == protocol.DialogPrompt,
	}
	w.sync(l)
}

func (w *dialogOverlay) sync(l *Tab) {
	st := l.State.Page
	st.FrontendChrome = true
	st.KeysEngine = true
	st.Menu = protocol.MenuNone
	st.PromptActive = false
	st.FindActive = false
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		l.Presenter.SetDialog(&w.dialogView)
		_ = l.Presenter.Refresh()
	}
}

func (w *dialogOverlay) close(l *Tab) {
	if !w.dialogActive {
		return
	}
	w.dialogActive = false
	l.State.Page = w.dialogBase
	if l.Presenter != nil {
		l.Presenter.SetDialog(nil)
		l.Presenter.SetState(w.dialogBase)
		_ = l.Presenter.Refresh()
	}
}

func (w *dialogOverlay) answer(l *Tab, ok bool, text string) {
	id := w.dialog.ID
	w.close(l)
	_ = l.Session.Send(protocol.Command{Body: protocol.DialogResult{ID: id, OK: ok, Text: text}})
}

// applyState reports false: a dialog is answered explicitly and lets page state
// updates through so the page underneath keeps its geometry.
func (w *dialogOverlay) applyState(l *Tab, st protocol.State) bool { return false }

func (w *dialogOverlay) handleKey(l *Tab, k string) (handled, quit bool) {
	switch w.dialog.Kind {
	case protocol.DialogAlert:
		switch k {
		case "enter", " ", "esc":
			w.answer(l, true, "")
		}
		return true, false
	case protocol.DialogConfirm:
		switch k {
		case "enter", "y", "Y":
			w.answer(l, true, "")
		case "esc", "n", "N", "ctrl-c":
			w.answer(l, false, "")
		}
		return true, false
	case protocol.DialogPrompt:
		switch k {
		case "enter":
			w.answer(l, true, w.dialogView.Value)
			return true, false
		case "esc", "ctrl-c":
			w.answer(l, false, "")
			return true, false
		}
		value, cur, _ := textedit.EditLineKey(w.dialogView.Value, w.dialogView.Cursor, k, textedit.EditConfig{
			SelAll:            &w.dialogView.SelectAll,
			CollapseSelection: true,
		})
		w.dialogView.Value = value
		if cur > len(value) {
			cur = len(value)
		}
		w.dialogView.Cursor = cur
		w.sync(l)
		return true, false
	}
	return true, false
}

func (w *dialogOverlay) handleMouse(l *Tab, ev MouseEvent) bool {
	if ev.Wheel != 0 || ev.Motion || !ev.Press || ev.Button != MouseButtonLeft {
		return true
	}
	layout := LayoutDialog(l.State.Page, &w.dialogView)
	x, y := ev.X-1, ev.Y-1
	if w.dialog.Kind == protocol.DialogPrompt &&
		layout.PromptW > 0 &&
		x >= layout.PromptX && x < layout.PromptX+layout.PromptW &&
		y == layout.PromptY {
		rel := x - layout.PromptX
		w.dialogView.Cursor = textOffsetAt(w.dialogView.Value, w.dialogView.Cursor, w.dialogView.SelectAll, rel, layout.PromptW)
		w.dialogView.SelectAll = false
		w.sync(l)
		return true
	}
	if layout.BoxW > 0 &&
		x >= layout.BoxX && x < layout.BoxX+layout.BoxW &&
		y >= layout.BoxY && y < layout.BoxY+layout.BoxH {
		if x >= layout.Close.X && x < layout.Close.X+layout.Close.W &&
			y >= layout.Close.Y && y < layout.Close.Y+layout.Close.H {
			w.answer(l, w.dialog.Kind == protocol.DialogAlert, "")
			return true
		}
		return true
	}
	w.answer(l, w.dialog.Kind == protocol.DialogAlert, "")
	return true
}

// DialogView is the front-end state of one JavaScript alert, confirm or prompt.
type DialogView struct {
	ID        uint32
	Kind      protocol.DialogKind
	Text      string
	Value     string
	Cursor    int
	SelectAll bool
}

// DialogLayout describes the modal box in terminal cells.
type DialogLayout struct {
	BoxX, BoxY, BoxW, BoxH int
	Close                  protocol.Rect
	PromptX, PromptY       int
	PromptW                int
}

// LayoutDialog computes the modal geometry from the state and view.
func LayoutDialog(st protocol.State, d *DialogView) DialogLayout {
	if d == nil || st.Width < 8 || st.Height < 6 {
		return DialogLayout{}
	}
	boxW := minInt(st.Width-4, 68)
	if boxW < 20 {
		boxW = st.Width
	}
	if boxW < 8 {
		return DialogLayout{}
	}
	inner := boxW - 4
	promptRow := 0
	if d.Kind == protocol.DialogPrompt {
		promptRow = 1
	}
	msg := textwidth.Wrap(protocol.StripControls(d.Text), inner)
	maxLines := st.Height - 6 - promptRow
	if maxLines < 1 {
		maxLines = 1
	}
	if len(msg) > maxLines {
		msg = msg[:maxLines]
	}
	boxH := 6 + len(msg) + promptRow
	if boxH > st.Height {
		boxH = st.Height
	}
	x := (st.Width - boxW) / 2
	y := (st.Height - boxH) / 2
	promptX, promptY, promptW := 0, 0, 0
	if d.Kind == protocol.DialogPrompt {
		promptX = x + 2
		promptY = y + 3 + len(msg)
		promptW = boxW - 4
	}
	return DialogLayout{
		BoxX: x, BoxY: y, BoxW: boxW, BoxH: boxH,
		Close:   protocol.Rect{X: x + boxW - 3, Y: y, W: 3, H: 1},
		PromptX: promptX, PromptY: promptY, PromptW: promptW,
	}
}

// ComposeDialogFrame stacks the normal screen and draws the dialog over it.
func ComposeDialogFrame(st protocol.State, content protocol.Frame, d *DialogView) (protocol.Frame, bool) {
	full, ok := ComposeOverlayFrame(st, content, func(c *chromeCanvas) {
		DrawDialog(c, st, d)
	})
	return full, ok
}

// DrawDialog paints one dialog on a full-screen canvas.
func DrawDialog(c *chromeCanvas, st protocol.State, d *DialogView) DialogLayout {
	layout := LayoutDialog(st, d)
	if layout.BoxW <= 0 {
		return layout
	}
	inner := layout.BoxW - 4
	promptRow := 0
	if d.Kind == protocol.DialogPrompt {
		promptRow = 1
	}
	msg := textwidth.Wrap(protocol.StripControls(d.Text), inner)
	maxLines := st.Height - 6 - promptRow
	if maxLines < 1 {
		maxLines = 1
	}
	if len(msg) > maxLines {
		msg = msg[:maxLines]
	}
	modalFrame(c, MenuLayout{
		Title: dialogTitle(d.Kind), Hint: "",
		BoxX: layout.BoxX, BoxY: layout.BoxY, BoxW: layout.BoxW, BoxH: layout.BoxH,
		Close: layout.Close,
	})
	fg := rgb(0x1f, 0x23, 0x28)
	for i, line := range msg {
		c.text(layout.BoxX+2, layout.BoxY+3+i,
			textwidth.Pad(textwidth.Truncate(line, inner), inner), fg, protocol.AttrBold)
	}
	row := layout.BoxY + 3 + len(msg)
	if d.Kind == protocol.DialogPrompt {
		c.fill(layout.BoxX+2, row, inner, 1, rgb(0xe8, 0xf0, 0xfe))
		head, at, tail, show := promptParts("", d.Value, d.Cursor, d.SelectAll, inner)
		c.text(layout.BoxX+2, row, head, fg, protocol.AttrBold)
		cx := layout.BoxX + 2 + textwidth.StringWidth(head)
		if show {
			c.put(cx, row, at, rgb(0xff, 0xff, 0xff), protocol.AttrBold)
			if cx < c.w {
				c.cells[row*c.w+cx].BG = rgb(0x2f, 0x6f, 0xbf)
			}
			cx++
		}
		c.text(cx, row, tail, fg, protocol.AttrBold)
	}
	hint := " Enter: OK · Esc: Cancel "
	switch d.Kind {
	case protocol.DialogAlert:
		hint = " Enter / Space / Esc: OK "
	case protocol.DialogConfirm:
		hint = " Enter/Y: Yes · N/Esc: No "
	}
	c.text(layout.BoxX+2, layout.BoxY+layout.BoxH-2,
		textwidth.Pad(textwidth.Truncate(hint, layout.BoxW-4), layout.BoxW-4), fg, protocol.AttrBold)
	modalDim(c, MenuLayout{
		BoxX: layout.BoxX, BoxY: layout.BoxY, BoxW: layout.BoxW, BoxH: layout.BoxH,
	})
	return layout
}

func dialogTitle(k protocol.DialogKind) string {
	switch k {
	case protocol.DialogConfirm:
		return "JavaScript confirm"
	case protocol.DialogPrompt:
		return "JavaScript prompt"
	default:
		return "JavaScript alert"
	}
}
