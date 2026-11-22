package frontend

import (
	"browser/internal/protocol"
)

// MenuRow is one option in a front-end-owned choice window.
type MenuRow struct {
	Label  string
	Marker string
	On     bool
}

// MenuLayout describes the view or zoom window in terminal cells.
type MenuLayout struct {
	Title, Hint string
	Rows        []MenuRow
	Active      int
	Top         int
	BoxX, BoxY  int
	BoxW, BoxH  int
	Rects       []protocol.Rect
	Close       protocol.Rect
}

var zoomPresets = []int{25, 50, 67, 80, 100, 125, 150, 175, 200}

// LayoutMenu computes the choice-window geometry without drawing it.
func LayoutMenu(st protocol.State) MenuLayout {
	w, h := st.Width, st.Height
	var title, hint string
	var rows []MenuRow
	active := st.MenuIndex
	switch st.Menu {
	case protocol.MenuView:
		title = "View options"
		hint = " ↑↓ choose · Space toggle · Esc close "
		rows = []MenuRow{
			{Label: "Fixed elements", Marker: marker(st.ViewFlags&protocol.ViewFlagFixed != 0), On: st.ViewFlags&protocol.ViewFlagFixed != 0},
			{Label: "Sticky elements", Marker: marker(st.ViewFlags&protocol.ViewFlagSticky != 0), On: st.ViewFlags&protocol.ViewFlagSticky != 0},
			{Label: "Background images", Marker: marker(st.ViewFlags&protocol.ViewFlagBackgrounds != 0), On: st.ViewFlags&protocol.ViewFlagBackgrounds != 0},
		}
	case protocol.MenuZoom:
		title = "Zoom"
		hint = " ↑↓ choose · Enter apply · Esc close "
		for _, pct := range zoomPresets {
			m := "  "
			if pct == st.ZoomPercent {
				m = "● "
			}
			rows = append(rows, MenuRow{Label: itoa(pct) + "%", Marker: m, On: pct == st.ZoomPercent})
		}
	default:
		return MenuLayout{}
	}
	if w < 8 || h < 4 || len(rows) == 0 {
		return MenuLayout{}
	}
	maxLabel := 0
	for _, row := range rows {
		if n := stringWidth(row.Marker + row.Label); n > maxLabel {
			maxLabel = n
		}
	}
	minW := 40
	if st.Menu == protocol.MenuZoom {
		minW = 32
	}
	boxW := maxLabel + 6
	if n := stringWidth(hint) + 4; n > boxW {
		boxW = n
	}
	if boxW < minW {
		boxW = minW
	}
	if boxW > w {
		boxW = w
	}
	if boxW < 8 {
		return MenuLayout{}
	}
	inner := boxW - 4
	maxRows := h - 4
	if maxRows < 1 {
		maxRows = 1
	}
	visible := len(rows)
	if visible > maxRows {
		visible = maxRows
	}
	top := 0
	if active < top {
		top = active
	}
	if active >= top+visible {
		top = active - visible + 1
	}
	if top < 0 {
		top = 0
	}
	if top+visible > len(rows) {
		top = len(rows) - visible
	}
	if top < 0 {
		top = 0
	}
	boxH := visible + 4
	if boxH > h {
		boxH = h
	}
	x := (w - boxW) / 2
	y := (h - boxH) / 2
	rects := make([]protocol.Rect, 0, visible)
	for i := 0; i < visible; i++ {
		rects = append(rects, protocol.Rect{X: x + 1, Y: y + 2 + i, W: boxW - 2, H: 1})
	}
	_ = inner
	return MenuLayout{
		Title: title, Hint: hint, Rows: rows, Active: active, Top: top,
		BoxX: x, BoxY: y, BoxW: boxW, BoxH: boxH, Rects: rects,
		Close: protocol.Rect{X: x + boxW - 3, Y: y, W: 3, H: 1},
	}
}

func marker(on bool) string {
	if on {
		return "☑ "
	}
	return "☐ "
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [4]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// ComposeMenuFrame stacks the normal header, content and footer into one full
// frame and draws the active choice window over it.
func ComposeMenuFrame(st protocol.State, content protocol.Frame) (protocol.Frame, bool) {
	if st.Menu == protocol.MenuNone || !st.FrontendChrome ||
		st.Width != content.W || st.HeaderRows <= 0 ||
		st.Height != content.H+st.HeaderRows+1 {
		return protocol.Frame{}, false
	}
	layout := LayoutChrome(st)
	if len(layout.Header) != st.Width*st.HeaderRows || len(layout.Footer) != st.Width {
		return protocol.Frame{}, false
	}
	full := protocol.Frame{
		Version: content.Version, W: st.Width, H: st.Height,
		Cells: make([]protocol.Cell, st.Width*st.Height),
	}
	rows := st.HeaderRows
	copy(full.Cells[:rows*st.Width], layout.Header)
	copy(full.Cells[rows*st.Width:(rows+content.H)*st.Width], content.Cells)
	copy(full.Cells[(st.Height-1)*st.Width:], layout.Footer)
	c := &chromeCanvas{w: st.Width, h: st.Height, cells: full.Cells}
	DrawMenu(c, st)
	return full, true
}

// DrawMenu paints a choice window on a full-screen canvas from state.
func DrawMenu(c *chromeCanvas, st protocol.State) MenuLayout {
	layout := LayoutMenu(st)
	if layout.BoxW <= 0 || layout.BoxH <= 0 {
		return layout
	}
	modalFrame(c, layout)
	fg := rgb(0x1f, 0x23, 0x28)
	activeBG := rgb(0x1f, 0x4f, 0x9f)
	activeFG := rgb(0xff, 0xff, 0xff)
	inner := layout.BoxW - 4
	for i := 0; i < len(layout.Rects); i++ {
		idx := layout.Top + i
		if idx >= len(layout.Rows) {
			break
		}
		row := layout.Rows[idx]
		rowBG, rowFG := colModalBG, fg
		if idx == layout.Active {
			rowBG, rowFG = activeBG, activeFG
		}
		y := layout.BoxY + 2 + i
		c.fill(layout.BoxX+1, y, layout.BoxW-2, 1, rowBG)
		c.text(layout.BoxX+2, y, padWidth(truncateWidth(row.Marker+row.Label, inner), inner), rowFG, protocol.AttrBold)
	}
	c.text(layout.BoxX+2, layout.BoxY+layout.BoxH-2,
		padWidth(truncateWidth(layout.Hint, layout.BoxW-4), layout.BoxW-4), fg, protocol.AttrBold)
	modalDim(c, layout)
	return layout
}

var (
	colModalBG    = rgb(0xff, 0xff, 0xff)
	colModalDimBG = rgb(0xdf, 0xe4, 0xea)
	colModalDimFG = rgb(0xb2, 0xbc, 0xc8)
)

// modalFrame fills a solid box, title bar and close button.
func modalFrame(c *chromeCanvas, l MenuLayout) {
	border := rgb(0x2f, 0x6f, 0xbf)
	titleFG := rgb(0xff, 0xff, 0xff)
	c.fill(l.BoxX, l.BoxY, l.BoxW, l.BoxH, colModalBG)
	x0, y0 := maxInt(l.BoxX, 0), maxInt(l.BoxY, 0)
	x1, y1 := minInt(l.BoxX+l.BoxW, c.w), minInt(l.BoxY+l.BoxH, c.h)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			cell := &c.cells[y*c.w+x]
			cell.R = 0
			cell.FG = rgb(0x1f, 0x23, 0x28)
			cell.Attrs = 0
			cell.Flags &^= protocol.CellCont | protocol.CellSkip
			cell.Run = 0
		}
	}
	c.fill(l.BoxX, l.BoxY, l.BoxW, 1, border)
	c.fill(l.BoxX, l.BoxY+l.BoxH-1, l.BoxW, 1, border)
	c.fill(l.BoxX, l.BoxY, 1, l.BoxH, border)
	c.fill(l.BoxX+l.BoxW-1, l.BoxY, 1, l.BoxH, border)
	c.fill(l.BoxX+1, l.BoxY+1, l.BoxW-2, 1, border)
	c.text(l.BoxX+2, l.BoxY+1, truncateWidth(" "+l.Title+" ", l.BoxW-8), titleFG, protocol.AttrBold)
	c.put(l.BoxX+l.BoxW-2, l.BoxY, '×', titleFG, protocol.AttrBold)
}

// modalDim mutes everything outside the modal box.
func modalDim(c *chromeCanvas, l MenuLayout) {
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			if x >= l.BoxX && x < l.BoxX+l.BoxW && y >= l.BoxY && y < l.BoxY+l.BoxH {
				continue
			}
			cell := &c.cells[y*c.w+x]
			cell.BG = colModalDimBG
			cell.FG = colModalDimFG
			cell.Attrs &^= protocol.AttrUnderline | protocol.AttrStrike
		}
	}
}
