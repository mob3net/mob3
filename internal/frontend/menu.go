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

// ChoiceView is the common data for a centered choice window.
type ChoiceView struct {
	Title, Hint string
	Rows        []MenuRow
	Active      int
	MinW        int
}

// LayoutMenu computes the view/zoom window geometry without drawing it.
func LayoutMenu(st protocol.State) MenuLayout {
	var view ChoiceView
	switch st.Menu {
	case protocol.MenuView:
		view = ChoiceView{
			Title: "View options",
			Hint:  " ↑↓ choose · Space toggle · Esc close ",
			Rows: []MenuRow{
				{Label: "Fixed elements", Marker: marker(st.ViewFlags&protocol.ViewFlagFixed != 0), On: st.ViewFlags&protocol.ViewFlagFixed != 0},
				{Label: "Sticky elements", Marker: marker(st.ViewFlags&protocol.ViewFlagSticky != 0), On: st.ViewFlags&protocol.ViewFlagSticky != 0},
				{Label: "Background images", Marker: marker(st.ViewFlags&protocol.ViewFlagBackgrounds != 0), On: st.ViewFlags&protocol.ViewFlagBackgrounds != 0},
			},
			Active: st.MenuIndex,
			MinW:   40,
		}
	case protocol.MenuZoom:
		view = ChoiceView{
			Title:  "Zoom",
			Hint:   " ↑↓ choose · Enter apply · Esc close ",
			Active: st.MenuIndex,
			MinW:   32,
		}
		for _, pct := range zoomPresets {
			m := "  "
			if pct == st.ZoomPercent {
				m = "● "
			}
			view.Rows = append(view.Rows, MenuRow{Label: itoa(pct) + "%", Marker: m, On: pct == st.ZoomPercent})
		}
	default:
		return MenuLayout{}
	}
	return layoutChoice(st, view)
}

func layoutChoice(st protocol.State, view ChoiceView) MenuLayout {
	w, h := st.Width, st.Height
	if w < 8 || h < 4 || len(view.Rows) == 0 {
		return MenuLayout{}
	}
	maxLabel := 0
	for _, row := range view.Rows {
		if n := stringWidth(row.Marker + row.Label); n > maxLabel {
			maxLabel = n
		}
	}
	boxW := maxLabel + 6
	if n := stringWidth(view.Hint) + 4; n > boxW {
		boxW = n
	}
	if boxW < view.MinW {
		boxW = view.MinW
	}
	if boxW > w {
		boxW = w
	}
	if boxW < 8 {
		return MenuLayout{}
	}
	maxRows := h - 4
	if maxRows < 1 {
		maxRows = 1
	}
	visible := len(view.Rows)
	if visible > maxRows {
		visible = maxRows
	}
	top := view.Active
	if top < 0 {
		top = 0
	}
	if top+visible > len(view.Rows) {
		top = len(view.Rows) - visible
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
	return MenuLayout{
		Title: view.Title, Hint: view.Hint, Rows: view.Rows, Active: view.Active, Top: top,
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
	return ComposeOverlayFrame(st, content, func(c *chromeCanvas) { DrawMenu(c, st) })
}

// ComposeOverlayFrame stacks the normal header, content and footer into one
// full frame and lets draw paint a modal layer over it.
func ComposeOverlayFrame(st protocol.State, content protocol.Frame, draw func(*chromeCanvas)) (protocol.Frame, bool) {
	if !st.FrontendChrome || st.Width != content.W || st.HeaderRows <= 0 ||
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
	draw(c)
	return full, true
}

// DrawMenu paints the view/zoom window on a full-screen canvas.
func DrawMenu(c *chromeCanvas, st protocol.State) MenuLayout {
	layout := LayoutMenu(st)
	if layout.BoxW <= 0 {
		return layout
	}
	drawChoiceLayout(c, layout)
	return layout
}

// choiceRowAt maps a pointer report to the visible choice row under it.
func choiceRowAt(layout MenuLayout, ev MouseEvent) (int, bool) {
	x, y := ev.X-1, ev.Y-1
	for i, r := range layout.Rects {
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			return layout.Top + i, true
		}
	}
	return 0, false
}

// moveChoice shifts a selection by delta and clamps it to the list.
func moveChoice(index, delta, count int) int {
	if count < 1 {
		return 0
	}
	index += delta
	if index < 0 {
		return 0
	}
	if index >= count {
		return count - 1
	}
	return index
}

// DrawSelect paints the page select popup on a full-screen canvas.
func DrawSelect(c *chromeCanvas, st protocol.State, view *SelectView) MenuLayout {
	layout := LayoutSelect(st, view)
	if layout.BoxW <= 0 {
		return layout
	}
	drawChoiceLayout(c, layout)
	return layout
}

func drawChoiceLayout(c *chromeCanvas, layout MenuLayout) {
	if layout.BoxW <= 0 || layout.BoxH <= 0 {
		return
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
