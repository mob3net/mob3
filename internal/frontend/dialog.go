package frontend

import (
	"strings"

	"browser/internal/protocol"
)

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
	msg := wrapCells(protocol.StripControls(d.Text), inner)
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
	msg := wrapCells(protocol.StripControls(d.Text), inner)
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
			padWidth(truncateWidth(line, inner), inner), fg, protocol.AttrBold)
	}
	row := layout.BoxY + 3 + len(msg)
	if d.Kind == protocol.DialogPrompt {
		c.fill(layout.BoxX+2, row, inner, 1, rgb(0xe8, 0xf0, 0xfe))
		head, at, tail, show := promptParts("", d.Value, d.Cursor, d.SelectAll, inner)
		c.text(layout.BoxX+2, row, head, fg, protocol.AttrBold)
		cx := layout.BoxX + 2 + stringWidth(head)
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
		padWidth(truncateWidth(hint, layout.BoxW-4), layout.BoxW-4), fg, protocol.AttrBold)
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

func wrapCells(s string, width int) []string {
	var out []string
	var line strings.Builder
	col := 0
	for _, r := range s {
		rw := runeWidth(r)
		if rw == 0 {
			line.WriteRune(r)
			continue
		}
		if col+rw > width {
			out = append(out, line.String())
			line.Reset()
			col = 0
		}
		line.WriteRune(r)
		col += rw
	}
	if line.Len() > 0 {
		out = append(out, line.String())
	}
	if len(out) == 0 {
		out = append(out, "")
	}
	return out
}
