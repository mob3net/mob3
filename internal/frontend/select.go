package frontend

import "browser/internal/protocol"

// SelectView is the front-end state of one page select popup.
type SelectView struct {
	ID       uint32
	Options  []string
	Selected int
	Index    int
}

// LayoutSelect computes the select popup geometry without drawing it.
func LayoutSelect(st protocol.State, view *SelectView) MenuLayout {
	if view == nil || len(view.Options) == 0 {
		return MenuLayout{}
	}
	rows := make([]MenuRow, len(view.Options))
	for i, opt := range view.Options {
		marker := "  "
		if i == view.Selected {
			marker = "✓ "
		}
		rows[i] = MenuRow{Label: protocol.StripControls(opt), Marker: marker, On: i == view.Selected}
	}
	active := view.Index
	if active < 0 || active >= len(rows) {
		active = 0
	}
	return layoutChoice(st, ChoiceView{
		Title:  "Select an option",
		Hint:   " ↑↓ choose · Enter select · Esc close ",
		Rows:   rows,
		Active: active,
		MinW:   40,
	})
}

// ComposeSelectFrame stacks the normal screen and draws the select popup.
func ComposeSelectFrame(st protocol.State, content protocol.Frame, view *SelectView) (protocol.Frame, bool) {
	return ComposeOverlayFrame(st, content, func(c *chromeCanvas) { DrawSelect(c, st, view) })
}
