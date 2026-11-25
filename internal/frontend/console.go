package frontend

import (
	"fmt"
	"io"
	"strings"

	"browser/internal/linehistory"
	"browser/internal/protocol"
	"browser/internal/textedit"
	"browser/internal/textwidth"
)

// consoleOverlay is the full-screen JavaScript console and tools pane.
type consoleOverlay struct {
	consoleActive     bool
	consoleView       protocol.ConsoleView
	consolePane       int
	consoleEdit       bool
	consoleText       string
	consoleCursor     int
	consoleHist       [5]linehistory.History
	consoleFindActive bool
	consoleFindText   string
	consoleFindCursor int
}

func (w *consoleOverlay) isOpen() bool { return w.consoleActive }

// dismissOnModal is false: the console owns the whole screen and a page modal
// is drawn over it instead of replacing it.
func (w *consoleOverlay) dismissOnModal() bool { return false }

func (w *consoleOverlay) reset(l *Tab) {
	w.consoleActive = false
	w.consoleFindActive = false
}

// install reports that the console owns the whole screen; the caller presents
// it after every other window has republished its view.
func (w *consoleOverlay) install(l *Tab) bool { return true }

func (w *consoleOverlay) close(l *Tab) {
	w.consoleActive = false
	_ = l.Session.Send(protocol.Command{Body: protocol.ConsoleAction{Op: protocol.ConsoleClose}})
}

// applyState reports false: console state arrives as its own event, so page
// state updates keep flowing to the page underneath.
func (w *consoleOverlay) applyState(l *Tab, st protocol.State) bool { return false }

func (w *consoleOverlay) applyView(l *Tab, v protocol.ConsoleView) {
	if !w.consoleActive {
		w.consolePane = v.Pane
		w.consoleEdit = false
		w.consoleText = ""
		w.consoleCursor = 0
	}
	w.consoleActive = true
	w.consolePane = v.Pane
	w.consoleView = v
	if w.consoleFindActive {
		w.consoleView.FindActive = true
		w.consoleView.FindText = w.consoleFindText
		w.consoleView.FindCursor = w.consoleFindCursor
	}
	if w.consoleEdit {
		w.consoleView.Input = w.consoleText
		w.consoleView.Cursor = w.consoleCursor
		w.consoleView.Edit = true
		w.consoleView.Hint = ""
	}
	w.present(l)
}

func (w *consoleOverlay) present(l *Tab) {
	if l.Presenter == nil {
		return
	}
	w.consoleView.Input = w.consoleText
	w.consoleView.Cursor = w.consoleCursor
	w.consoleView.Edit = w.consoleEdit
	if w.consoleEdit {
		w.consoleView.Hint = ""
	}
	w.consoleView.Pane = w.consolePane
	if w.consoleFindActive {
		w.consoleView.FindActive = true
		w.consoleView.FindText = w.consoleFindText
		w.consoleView.FindCursor = w.consoleFindCursor
	} else {
		w.consoleView.FindActive = false
	}
	_ = l.Presenter.PresentConsole(&w.consoleView)
}

func (w *consoleOverlay) action(l *Tab, a protocol.ConsoleAction) {
	_ = l.Session.Send(protocol.Command{Body: a})
}

func (w *consoleOverlay) handleKey(l *Tab, k string) (handled, quit bool) {
	if w.consoleFindActive {
		return w.handleFindKey(l, k)
	}
	if w.consoleView.Quit {
		switch k {
		case "y", "Y", "enter":
			return true, true
		default:
			w.action(l, protocol.ConsoleAction{Op: protocol.ConsoleCancelQuit})
			return true, false
		}
	}
	if w.consoleEdit {
		switch k {
		case "esc":
			w.consoleEdit = false
			w.action(l, protocol.ConsoleAction{Op: protocol.ConsoleCancelEdit})
			w.present(l)
			return true, false
		case "ctrl-c":
			return true, false
		case "tab":
			w.consoleEdit = false
			w.action(l, protocol.ConsoleAction{Op: protocol.ConsoleCycle, Delta: 1})
			w.present(l)
			return true, false
		case "backtab":
			w.consoleEdit = false
			w.action(l, protocol.ConsoleAction{Op: protocol.ConsoleCycle, Delta: -1})
			w.present(l)
			return true, false
		case "enter":
			text := strings.TrimSpace(w.consoleText)
			if text != "" {
				w.consoleHist[w.consolePane].Record(text)
				w.action(l, protocol.ConsoleAction{Op: protocol.ConsoleInput, Text: text})
			}
			w.consoleText = ""
			w.consoleCursor = 0
			w.present(l)
			return true, false
		case "up", "ctrl-p":
			if prev, ok := w.consoleHist[w.consolePane].Up(w.consoleText); ok {
				w.consoleText = prev
				w.consoleCursor = len(prev)
			}
			w.present(l)
			return true, false
		case "down", "ctrl-n":
			if next, ok := w.consoleHist[w.consolePane].Down(w.consoleText); ok {
				w.consoleText = next
				w.consoleCursor = len(next)
			}
			w.present(l)
			return true, false
		}
		if k == "ctrl-a" {
			k = "home"
		}
		text, cur, _ := textedit.EditLineKey(w.consoleText, w.consoleCursor, k, textedit.EditConfig{MaxRunes: 8192})
		w.consoleText = text
		if cur > len(text) {
			cur = len(text)
		}
		w.consoleCursor = cur
		w.present(l)
		return true, false
	}
	switch k {
	case "esc", "!":
		w.consoleActive = false
		w.action(l, protocol.ConsoleAction{Op: protocol.ConsoleClose})
	case "q", "ctrl-c":
		w.action(l, protocol.ConsoleAction{Op: protocol.ConsoleQuit})
	case "tab", "l", "right":
		w.action(l, protocol.ConsoleAction{Op: protocol.ConsoleCycle, Delta: 1})
	case "backtab", "h", "left":
		w.action(l, protocol.ConsoleAction{Op: protocol.ConsoleCycle, Delta: -1})
	case ":", "enter":
		if w.consolePane != 4 {
			w.consolePane = 0
		}
		w.consoleEdit = true
		w.consoleText = ""
		w.consoleCursor = 0
		w.action(l, protocol.ConsoleAction{Op: protocol.ConsoleFocus})
		w.present(l)
	case "k", "up":
		w.action(l, protocol.ConsoleAction{Op: protocol.ConsoleScroll, Delta: 1})
	case "j", "down":
		w.action(l, protocol.ConsoleAction{Op: protocol.ConsoleScroll, Delta: -1})
	case "g":
		w.action(l, protocol.ConsoleAction{Op: protocol.ConsoleTop})
	case "G":
		w.action(l, protocol.ConsoleAction{Op: protocol.ConsoleBottom})
	case "y":
		l.expectClipboard()
		w.action(l, protocol.ConsoleAction{Op: protocol.ConsoleCopy})
	case "/":
		if w.consolePane == 3 {
			w.consoleFindActive = true
			w.consoleFindText = ""
			w.consoleFindCursor = 0
			_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Query: ""}})
			w.present(l)
		}
	case "n":
		if w.consoleView.FindText != "" {
			_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Query: w.consoleView.FindText, Next: true}})
		}
	case "N":
		if w.consoleView.FindText != "" {
			_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Query: w.consoleView.FindText, Prev: true}})
		}
	}
	return true, false
}

// handleMouse owns pointer reports on the console screen. Tabs switch sections,
// the input row focuses the JS/Tools prompt, the footer >_ icon closes the
// console, and the wheel scrolls the active section. Every report is consumed
// so a click cannot reach the page underneath.
func (w *consoleOverlay) handleMouse(l *Tab, ev MouseEvent) bool {
	v := w.consoleView
	if v.Width <= 0 || v.Height <= 0 || v.Quit || w.consoleFindActive {
		return true
	}
	if ev.Wheel != 0 {
		delta := 3
		if ev.Wheel > 0 {
			delta = -3
		}
		w.action(l, protocol.ConsoleAction{Op: protocol.ConsoleScroll, Delta: delta})
		return true
	}
	if ev.Motion || !ev.Press || ev.Button != MouseButtonLeft {
		return true
	}
	x, y := ev.X-1, ev.Y-1
	if x < 0 || y < 0 || x >= v.Width || y >= v.Height {
		return true
	}
	layout := consoleLayoutFor(v.Height, w.consolePane)
	switch {
	case y == layout.tabsY:
		if pane := consoleTabAt(v.Tabs, v.Width, x); pane >= 0 {
			w.consolePane = pane
			w.consoleEdit = false
			w.action(l, protocol.ConsoleAction{Op: protocol.ConsolePane, Pane: pane})
			w.present(l)
		}
	case layout.inputY >= 0 && y == layout.inputY:
		if w.consolePane != 4 {
			w.consolePane = 0
		}
		w.consoleEdit = true
		w.consoleCursor = len(w.consoleText)
		w.action(l, protocol.ConsoleAction{Op: protocol.ConsoleFocus})
		w.present(l)
	case y == layout.footerY && v.Width >= 6 && x >= v.Width-textwidth.StringWidth(">_"):
		w.consoleActive = false
		w.action(l, protocol.ConsoleAction{Op: protocol.ConsoleClose})
	}
	return true
}

func (w *consoleOverlay) handleFindKey(l *Tab, k string) (handled, quit bool) {
	switch k {
	case "esc", "enter", "tab", "backtab":
		w.consoleFindActive = false
		w.present(l)
		return true, false
	case "down":
		_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Query: w.consoleFindText, Next: true}})
		return true, false
	case "up":
		_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Query: w.consoleFindText, Prev: true}})
		return true, false
	}
	if k == "ctrl-a" {
		k = "home"
	}
	text, cur, _ := textedit.EditLineKey(w.consoleFindText, w.consoleFindCursor, k, textedit.EditConfig{MaxRunes: 512})
	changed := text != w.consoleFindText
	w.consoleFindText = text
	if cur > len(text) {
		cur = len(text)
	}
	w.consoleFindCursor = cur
	if changed {
		_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Query: text}})
	}
	w.present(l)
	return true, false
}

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

// consoleTabAt returns the tab under console-menu column x, or -1. It uses the
// same width walk as BuildConsoleFrame so clicks land on the drawn labels.
func consoleTabAt(tabs []string, w, x int) int {
	pos := 0
	for i, title := range tabs {
		if pos >= w {
			break
		}
		title = protocol.StripControls(title)
		tw := textwidth.StringWidth(title)
		if tw > w-pos {
			tw = w - pos
		}
		if x >= pos && x < pos+tw {
			return i
		}
		pos += tw
	}
	return -1
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
			tw := textwidth.StringWidth(title)
			if tw > w-x {
				tw = w - x
			}
			tabBG := hdrBG
			if pane == v.Pane {
				tabBG = activeBG
			}
			c.fill(x, l.tabsY, tw, 1, tabBG)
			c.text(x, l.tabsY, textwidth.Truncate(title, tw), fg, protocol.AttrBold)
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
			c.text(1, l.bodyY+i, textwidth.Truncate(protocol.StripControls(line), w-2), fg, 0)
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
			c.text(1, y, textwidth.Truncate(protocol.StripControls(v.Summary), w-2), rgb(0x8a, 0x9a, 0xaa), 0)
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
						xx += textwidth.RuneWidth(runes[i])
					}
					if cur == len(runes) && cur >= start && xx < w {
						c.put(xx, y, ' ', rgb(0x0f, 0x14, 0x1a), protocol.AttrBold)
						c.cells[y*w+xx].BG = rgb(0xf8, 0xfa, 0xfc)
					}
				} else if input == "" {
					c.text(2, y, textwidth.Truncate(hint, contentW), rgb(0x8a, 0x9a, 0xaa), 0)
				} else {
					c.text(2, y, textwidth.Truncate(input, contentW), fg, 0)
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
		iconW := textwidth.StringWidth(icon)
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
		c.text(0, l.footerY, textwidth.Pad(textwidth.Truncate(footer, hintW), hintW), footerFG, 0)
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
	if p.gfx != nil {
		// Delete visible page images so they cannot composite above the console.
		// This keeps the transmitted kitty data, and ClearScreen below also
		// preserves it; ED 2 would free it and leave the page images missing
		// when the console closes.
		p.gfx.BeginFrame(&sb)
	}
	if needClear {
		if p.gfx != nil {
			p.gfx.ClearScreen(&sb)
		} else {
			sb.WriteString("\x1b[2J")
		}
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
