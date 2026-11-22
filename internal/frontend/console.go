package frontend

import (
	"fmt"
	"io"
	"strings"

	"browser/internal/protocol"
)

type consoleLayout struct {
	tabsY, bodyY, bodyRows, inputY, footerY int
}

func consoleLayoutFor(h, pane int) consoleLayout {
	l := consoleLayout{tabsY: -1, bodyY: -1, inputY: -1, footerY: -1}
	if pane < 0 || pane > 4 {
		pane = 0
	}
	if h <= 0 {
		return l
	}
	if h >= 2 {
		l.footerY = h - 1
	}
	avail := h
	if l.footerY >= 0 {
		avail = h - 1
	}
	if avail <= 0 {
		return l
	}
	l.tabsY = 0
	l.bodyY = 1
	l.bodyRows = avail - 1
	if l.bodyRows < 0 {
		l.bodyRows = 0
	}
	if (pane == 0 || pane == 4) && l.bodyRows > 0 {
		l.inputY = l.bodyY + l.bodyRows - 1
	}
	return l
}

// BuildConsoleFrame renders one console snapshot into a full-screen frame.
func BuildConsoleFrame(v *protocol.ConsoleView) protocol.Frame {
	w, h := v.Width, v.Height
	if w < 1 || h < 1 {
		return protocol.Frame{}
	}
	fg := rgb(0xdd, 0xe6, 0xee)
	bg := rgb(0x0f, 0x14, 0x1a)
	f := protocol.Frame{W: w, H: h, Cells: make([]protocol.Cell, w*h)}
	for i := range f.Cells {
		f.Cells[i] = protocol.Cell{R: ' ', FG: fg, BG: bg}
	}
	c := &chromeCanvas{w: w, h: h, cells: f.Cells}
	hdrBG := rgb(0x1f, 0x29, 0x37)
	ftrBG := rgb(0x1b, 0x24, 0x30)
	activeBG := rgb(0x2f, 0x6f, 0xbf)
	l := consoleLayoutFor(h, v.Pane)
	if l.footerY >= 0 {
		c.fill(0, l.footerY, w, 1, ftrBG)
	}
	if l.tabsY >= 0 {
		x := 0
		for pane, title := range v.Tabs {
			if x >= w {
				break
			}
			title = protocol.StripControls(title)
			tw := stringWidth(title)
			if tw > w-x {
				tw = w - x
			}
			tabBG := hdrBG
			if pane == v.Pane {
				tabBG = activeBG
			}
			c.fill(x, l.tabsY, tw, 1, tabBG)
			c.text(x, l.tabsY, truncateWidth(title, tw), fg, protocol.AttrBold)
			x += tw
		}
		if x < w {
			c.fill(x, l.tabsY, w-x, 1, hdrBG)
		}
	}
	if l.bodyY >= 0 && l.bodyRows > 0 {
		for i, line := range v.Lines {
			if i >= l.bodyRows {
				break
			}
			c.text(1, l.bodyY+i, truncateWidth(protocol.StripControls(line), w-2), fg, 0)
		}
		matchBG, currentBG := rgb(0xff, 0xe0, 0x66), rgb(0xff, 0x9d, 0x2a)
		matchFG := rgb(0x1f, 0x23, 0x28)
		for _, h := range v.Highlights {
			y := l.bodyY + h.Line
			if y < l.bodyY || y >= l.bodyY+l.bodyRows {
				continue
			}
			bg := matchBG
			if h.Current {
				bg = currentBG
			}
			for x := h.X; x < h.X+h.W && x < w; x++ {
				if x < 0 {
					continue
				}
				cell := &c.cells[y*w+x]
				cell.BG = bg
				cell.FG = matchFG
			}
		}
		if v.Summary != "" {
			y := l.bodyY + l.bodyRows - 1
			c.fill(1, y, maxInt(0, w-2), 1, ftrBG)
			c.text(1, y, truncateWidth(protocol.StripControls(v.Summary), w-2), rgb(0x8a, 0x9a, 0xaa), 0)
		}
		if l.inputY >= 0 {
			y := l.inputY
			c.fill(0, y, w, 1, ftrBG)
			c.text(0, y, "> ", fg, 0)
			contentW := w - 2
			if contentW > 0 {
				input := protocol.StripControls(v.Input)
				hint := protocol.StripControls(v.Hint)
				if v.Edit {
					runes, start, end, cur := promptWindow(input, v.Cursor, contentW, false)
					xx := 2
					for i := start; i < end; i++ {
						if i == cur {
							if xx < w {
								c.put(xx, y, ' ', rgb(0x0f, 0x14, 0x1a), protocol.AttrBold)
								c.cells[y*w+xx].BG = rgb(0xf8, 0xfa, 0xfc)
							}
							xx++
							continue
						}
						c.put(xx, y, runes[i], fg, 0)
						xx += runeWidth(runes[i])
					}
					if cur == len(runes) && cur >= start && xx < w {
						c.put(xx, y, ' ', rgb(0x0f, 0x14, 0x1a), protocol.AttrBold)
						c.cells[y*w+xx].BG = rgb(0xf8, 0xfa, 0xfc)
					}
				} else if input == "" {
					c.text(2, y, truncateWidth(hint, contentW), rgb(0x8a, 0x9a, 0xaa), 0)
				} else {
					c.text(2, y, truncateWidth(input, contentW), fg, 0)
				}
			}
		}
	}
	if l.footerY >= 0 {
		if v.FindActive {
			drawFindBarAt(c, l.footerY, protocol.State{
				FindActive: true, FindText: v.FindText, FindCursor: v.FindCursor,
				FindCurrent: v.FindCurrent, FindTotal: v.FindTotal,
			})
			return f
		}
		icon := ">_"
		iconW := stringWidth(icon)
		reserve := 0
		if w >= 6 {
			reserve = iconW + 2
		}
		hintW := w - reserve
		if hintW < 1 {
			hintW = 1
		}
		footer := protocol.StripControls(v.Footer)
		footerFG := fg
		if v.Quit {
			footerFG = rgb(0x9a, 0x5b, 0x00)
		}
		c.text(0, l.footerY, padWidth(truncateWidth(footer, hintW), hintW), footerFG, 0)
		if reserve > 0 {
			c.text(w-iconW, l.footerY, icon, rgb(0x8a, 0xc4, 0xff), protocol.AttrBold)
		}
	}
	return f
}

// PresentConsole draws a console snapshot and stores it for chrome-only
// refreshes (dialogs/select/download/notice over the console).
func (p *Presenter) PresentConsole(v *protocol.ConsoleView) error {
	if p == nil || p.out == nil || v == nil {
		return nil
	}
	if !p.consoleActive {
		p.prev = nil
	}
	p.console = v
	p.consoleActive = true
	f := BuildConsoleFrame(v)
	p.applyConsoleOverlays(&f)
	return p.presentFull(f)
}

func (p *Presenter) applyConsoleOverlays(f *protocol.Frame) {
	if f.W <= 0 || f.H <= 0 {
		return
	}
	st := p.state
	st.Width, st.Height, st.HeaderRows = f.W, f.H, 0
	c := &chromeCanvas{w: f.W, h: f.H, cells: f.Cells}
	switch {
	case p.dialog != nil:
		DrawDialog(c, st, p.dialog)
	case p.selectView != nil:
		DrawSelect(c, st, p.selectView)
	case p.download != nil:
		DrawDownload(c, st, p.download)
	case p.notice != nil:
		DrawNotice(c, st, p.notice)
	}
}

func (p *Presenter) presentFull(f protocol.Frame) error {
	lines := EncodeFrame(f, p.color, !p.color)
	if len(lines) != f.H {
		return nil
	}
	needClear := p.prev == nil || len(p.prev) != len(lines)
	if needClear {
		p.prev = make([]string, len(lines))
	}
	var sb strings.Builder
	if needClear {
		sb.WriteString("\x1b[2J")
	}
	for i := range lines {
		if p.prev[i] == lines[i] {
			continue
		}
		fmt.Fprintf(&sb, "\x1b[%d;1H", i+1)
		sb.WriteString(lines[i])
		if !p.color {
			sb.WriteString("\x1b[K")
		}
		p.prev[i] = lines[i]
	}
	if sb.Len() == 0 {
		return nil
	}
	_, err := io.WriteString(p.out, sb.String())
	return err
}
