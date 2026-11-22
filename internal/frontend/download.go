package frontend

import "browser/internal/protocol"

// DownloadView is the front-end state of the download curl hint.
type DownloadView struct {
	Name    string
	Command string
}

// NoticeView is the front-end state of a small informational modal.
type NoticeView struct {
	Title   string
	Message string
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

// ComposeDownloadFrame stacks the normal screen and draws the curl hint.
func ComposeDownloadFrame(st protocol.State, content protocol.Frame, v *DownloadView) (protocol.Frame, bool) {
	return ComposeOverlayFrame(st, content, func(c *chromeCanvas) { DrawDownload(c, st, v) })
}

// ComposeNoticeFrame stacks the normal screen and draws the notice.
func ComposeNoticeFrame(st protocol.State, content protocol.Frame, v *NoticeView) (protocol.Frame, bool) {
	return ComposeOverlayFrame(st, content, func(c *chromeCanvas) { DrawNotice(c, st, v) })
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
