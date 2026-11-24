package frontend

import (
	"strings"

	"browser/internal/protocol"
)

// HintView is the front-end state of one link-hint session.
type HintView struct {
	Ask    bool
	Prefix string
	Items  []protocol.HintItem
}

// ApplyHints paints the matching hint letters and fallback labels on a content
// frame. Coordinates are content-viewport cells.
func ApplyHints(f *protocol.Frame, v *HintView, color bool) {
	if v == nil || f.W <= 0 || f.H <= 0 || len(f.Cells) < f.W*f.H {
		return
	}
	bg := rgb(0xff, 0xd7, 0x00)
	fg := rgb(0x1f, 0x23, 0x28)
	attrs := uint8(protocol.AttrBold | protocol.AttrUnderline)
	if !color {
		attrs |= protocol.AttrReverse
	}
	for _, item := range v.Items {
		if !strings.HasPrefix(item.Label, v.Prefix) {
			continue
		}
		if len(item.Chars) > 0 {
			chars := item.Chars
			if len(v.Prefix) < len(chars) {
				chars = chars[len(v.Prefix):]
			} else {
				chars = nil
			}
			for _, c := range chars {
				paintHintCell(f, c.X, c.Y, c.R, fg, bg, attrs)
			}
			continue
		}
		label := item.Label
		if len(v.Prefix) < len(label) {
			label = label[len(v.Prefix):]
		} else {
			continue
		}
		x, y := item.X, item.Y
		if y < 0 || y >= f.H {
			continue
		}
		if x+len(label) > f.W {
			x = f.W - len(label)
		}
		if x < 0 {
			continue
		}
		for _, r := range label {
			paintHintCell(f, x, y, r, fg, bg, attrs)
			x += runeWidth(r)
		}
	}
}

func paintHintCell(f *protocol.Frame, x, y int, r rune, fg, bg protocol.Color, attrs uint8) {
	if x < 0 || x >= f.W || y < 0 || y >= f.H || runeWidth(r) == 0 {
		return
	}
	c := &f.Cells[y*f.W+x]
	c.R = r
	c.FG = fg
	c.BG = bg
	c.Attrs = attrs
	c.Flags &^= protocol.CellCont | protocol.CellSkip
	c.Run = 0
}
