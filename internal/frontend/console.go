package frontend

import (
	"fmt"
	"io"
	"strings"

	"browser/internal/movekeys"
	"browser/internal/protocol"
	"browser/internal/textedit"
	"browser/internal/textwidth"
)

// Console sections, ordered as they are stacked in the console view. The
// values match the engine's section order.
const (
	consolePaneLogs = iota
	consolePaneRequests
	consolePaneStorage
	consolePaneHTML
	consolePaneTools
	consolePaneCount
)

const (
	consoleFooter = " Click/Tab/h/l: section · : input · j/k d/u: scroll · Space/b: page · g/G: ends · y: copy all · q: quit · Esc: close "
	consoleHint   = "JavaScript input"
	toolsHint     = "Tools: help · form · links · requests · logs · source · snapshot"
)

// consoleOverlay is the full-screen JavaScript console and tools pane. It owns
// the pane, scroll, input, history and quit state; the engine only supplies the
// requested section window.
type consoleOverlay struct {
	consoleActive     bool
	consoleView       protocol.ConsoleView
	consolePane       int
	consoleStart      [consolePaneCount]int
	consoleTotal      [consolePaneCount]int
	consoleFollow     [consolePaneCount]bool
	consoleEdit       bool
	consoleText       string
	consoleCursor     int
	consoleHist       [consolePaneCount]lineHistory
	consoleQuit       bool
	consoleFindActive bool
	consoleFindText   string
	consoleFindCursor int
	consoleFindLine   int
	consoleFindSet    bool
}

func (w *consoleOverlay) isOpen() bool { return w.consoleActive }

// dismissOnModal is false: the console owns the whole screen and a page modal
// is drawn over it instead of replacing it.
func (w *consoleOverlay) dismissOnModal() bool { return false }

func (w *consoleOverlay) reset(l *Tab) {
	w.consoleActive = false
	w.consoleEdit = false
	w.consoleQuit = false
	w.consoleFindActive = false
	w.consoleFindSet = false
}

// install reports that the console owns the whole screen; the caller presents
// it after every other window has republished its view.
func (w *consoleOverlay) install(l *Tab) bool {
	return true
}

func (w *consoleOverlay) close(l *Tab) {
	if !w.consoleActive {
		return
	}
	w.deactivate()
	_ = l.Session.Send(protocol.Command{Body: protocol.ConsoleAction{Op: protocol.ConsoleClose}})
}

// deactivate drops the local window state without sending a command.
func (w *consoleOverlay) deactivate() {
	w.consoleActive = false
	w.consoleEdit = false
	w.consoleQuit = false
	w.consoleFindActive = false
	w.consoleFindSet = false
	w.consoleFindText = ""
	w.consoleFindCursor = 0
}

// toggle opens or closes the console from the shell key or the footer icon.
func (w *consoleOverlay) toggle(l *Tab) {
	if w.consoleActive {
		w.close(l)
		return
	}
	w.open(l)
}

func (w *consoleOverlay) open(l *Tab) {
	if w.consoleActive {
		return
	}
	w.consoleActive = true
	w.consolePane = consolePaneLogs
	for i := range w.consoleStart {
		w.consoleStart[i] = -1
		w.consoleFollow[i] = true
	}
	w.consoleEdit = false
	w.consoleText = ""
	w.consoleCursor = 0
	w.consoleQuit = false
	w.consoleFindActive = false
	w.consoleFindSet = false
	w.consoleFindText = ""
	w.consoleFindCursor = 0
	_ = l.Session.Send(protocol.Command{Body: protocol.ConsoleAction{Op: protocol.ConsoleOpen}})
	w.request(l, true)
}

// applyState reports false: console state arrives as its own event, so page
// state updates keep flowing to the page underneath.
func (w *consoleOverlay) applyState(l *Tab, st protocol.State) bool { return false }

// applyView stores a content window and scrolls to a new find match when one
// arrives. It ignores a window for a section the front end has already left.
func (w *consoleOverlay) applyView(l *Tab, v protocol.ConsoleView) {
	if !w.consoleActive || v.Pane != w.consolePane {
		return
	}
	w.consoleView = v
	w.consoleTotal[w.consolePane] = v.Total
	if w.consoleFollow[w.consolePane] && v.Total > 0 {
		want := w.maxStart()
		w.consoleStart[w.consolePane] = want
		if v.Start != want {
			w.present(l)
			w.request(l, false)
			return
		}
	} else {
		w.consoleStart[w.consolePane] = v.Start
	}
	if w.consoleFindText != "" && v.FindLineValid && (!w.consoleFindSet || v.FindLine != w.consoleFindLine) {
		w.consoleFindLine = v.FindLine
		w.consoleFindSet = true
		if !w.lineVisible(v.FindLine) {
			start := v.FindLine
			if max := w.maxStart(); start > max {
				start = max
			}
			if start < 0 {
				start = 0
			}
			w.consoleStart[w.consolePane] = start
			w.consoleFollow[w.consolePane] = false
			w.present(l)
			w.request(l, false)
			return
		}
	}
	w.present(l)
}

func (w *consoleOverlay) lineVisible(line int) bool {
	rows := w.contentRows(w.consoleView.Height)
	start := w.consoleStart[w.consolePane]
	return rows > 0 && line >= start && line < start+rows
}

func (w *consoleOverlay) present(l *Tab) {
	if l.Presenter == nil {
		return
	}
	w.consoleView.Pane = w.consolePane
	_ = l.Presenter.PresentConsole(&w.consoleView, w.ui())
}

// ui snapshots the front-end-owned console chrome for the frame renderer.
func (w *consoleOverlay) ui() ConsoleUI {
	hint := consoleHint
	if w.consolePane == consolePaneTools {
		hint = toolsHint
	}
	footer := consoleFooter
	if w.consoleQuit {
		footer = " " + quitPrompt
	}
	if w.consoleFindActive {
		hint = ""
	}
	return ConsoleUI{
		Pane: w.consolePane, Input: w.consoleText, Cursor: w.consoleCursor,
		Edit: w.consoleEdit, Hint: hint, Footer: footer, Quit: w.consoleQuit,
		FindActive: w.consoleFindActive, FindText: w.consoleFindText,
		FindCursor: w.consoleFindCursor,
	}
}

// size returns the terminal dimensions the console fills.
func (w *consoleOverlay) size(l *Tab) (int, int) {
	width, height := w.consoleView.Width, w.consoleView.Height
	if width <= 0 {
		width = l.State.Page.Width
	}
	if height <= 0 {
		height = l.State.Page.Height
	}
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	return width, height
}

// contentRows returns the section body rows requested from the engine,
// excluding the input row on logs/tools and the summary row on requests.
func (w *consoleOverlay) contentRows(height int) int {
	return w.contentRowsFor(consoleLayoutFor(height, w.consolePane))
}

func (w *consoleOverlay) contentRowsFor(l consoleLayout) int {
	rows := l.bodyRows
	if rows <= 0 {
		return 0
	}
	switch w.consolePane {
	case consolePaneLogs, consolePaneTools:
		if l.inputY >= 0 {
			rows = l.inputY - l.bodyY
		}
	case consolePaneRequests:
		if rows > 1 {
			rows--
		}
	}
	if rows < 0 {
		return 0
	}
	return rows
}

func (w *consoleOverlay) request(l *Tab, enter bool) {
	if !w.consoleActive {
		return
	}
	width, height := w.size(l)
	start := w.consoleStart[w.consolePane]
	if w.consoleFollow[w.consolePane] {
		start = -1
	}
	_ = l.Session.Send(protocol.Command{Body: protocol.ConsoleAction{
		Op: protocol.ConsoleWindow, Pane: w.consolePane, Start: start,
		Rows: w.contentRows(height), Width: width, Enter: enter,
	}})
}

// resize updates the console dimensions and requests a fresh window. The
// engine invalidates its cached request on resize, so this request is required
// to repaint the open console.
func (w *consoleOverlay) resize(l *Tab, width, height int) {
	if !w.consoleActive {
		return
	}
	w.consoleView.Width = width
	w.consoleView.Height = height
	if start := w.consoleStart[w.consolePane]; start >= 0 && !w.consoleFollow[w.consolePane] {
		if max := w.maxStart(); start > max {
			w.consoleStart[w.consolePane] = max
		}
	}
	w.request(l, false)
}

// maxStart is the largest first-line index that still fills the body.
func (w *consoleOverlay) maxStart() int {
	rows := w.contentRows(w.consoleView.Height)
	total := w.consoleTotal[w.consolePane]
	if rows < 1 || total <= rows {
		return 0
	}
	return total - rows
}

// scroll moves the active section by delta lines. Following the bottom keeps
// new output visible; scrolling up leaves follow mode.
func (w *consoleOverlay) scroll(l *Tab, delta int) {
	if delta == 0 {
		return
	}
	pane := w.consolePane
	if w.consoleFollow[pane] {
		if delta > 0 {
			return
		}
		w.consoleStart[pane] = w.maxStart()
		w.consoleFollow[pane] = false
	}
	start := w.consoleStart[pane]
	if start < 0 {
		start = w.maxStart()
	}
	start += delta
	if start < 0 {
		start = 0
	}
	if max := w.maxStart(); start >= max {
		start = max
		w.consoleFollow[pane] = true
	}
	w.consoleStart[pane] = start
	w.present(l)
	w.request(l, false)
}

func (w *consoleOverlay) jump(l *Tab, top bool) {
	w.consoleFollow[w.consolePane] = !top
	if top {
		w.consoleStart[w.consolePane] = 0
	} else {
		w.consoleStart[w.consolePane] = -1
	}
	w.present(l)
	w.request(l, false)
}

// move applies one shared viewport movement command to the active section.
func (w *consoleOverlay) move(l *Tab, m movekeys.Move) {
	switch m {
	case movekeys.Top:
		w.jump(l, true)
	case movekeys.Bottom:
		w.jump(l, false)
	default:
		_, height := w.size(l)
		if delta, ok := m.Delta(w.contentRows(height)); ok {
			w.scroll(l, delta)
		}
	}
}

// selectPane switches the active section. HTML enters at its first line; the
// other sections keep their own position. Leaving HTML closes its find bar.
func (w *consoleOverlay) selectPane(l *Tab, pane int, enter bool) {
	if pane < 0 || pane >= consolePaneCount {
		pane = consolePaneLogs
	}
	if pane != w.consolePane {
		if pane == consolePaneHTML {
			w.consoleStart[pane] = 0
			w.consoleFollow[pane] = false
		}
		if pane != consolePaneHTML {
			w.closeFind(l)
		}
		w.consolePane = pane
		w.consoleEdit = false
		enter = true
	}
	w.present(l)
	w.request(l, enter)
}

func (w *consoleOverlay) cycle(l *Tab, dir int) {
	pane := (w.consolePane + dir + consolePaneCount) % consolePaneCount
	w.selectPane(l, pane, true)
}

func (w *consoleOverlay) focusInput(l *Tab) {
	if w.consolePane != consolePaneTools && w.consolePane != consolePaneLogs {
		w.selectPane(l, consolePaneLogs, true)
	}
	w.consoleEdit = true
	w.consoleCursor = len(w.consoleText)
	w.present(l)
}

// hideFind hides the find bar but keeps the query, match state and last line so
// n/N can continue after Esc without yanking the section.
func (w *consoleOverlay) hideFind() {
	w.consoleFindActive = false
}

// closeFind hides the find bar and clears the engine's HTML find, used when the
// user leaves the HTML section.
func (w *consoleOverlay) closeFind(l *Tab) {
	if !w.consoleFindActive && w.consoleFindText == "" {
		return
	}
	w.hideFind()
	w.consoleFindSet = false
	w.consoleFindText = ""
	w.consoleFindCursor = 0
	_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Close: true}})
}

func (w *consoleOverlay) handleKey(l *Tab, k string) (handled, quit bool) {
	if w.consoleFindActive {
		return w.handleFindKey(l, k)
	}
	if w.consoleQuit {
		switch k {
		case "y", "Y", "enter":
			return true, true
		default:
			w.consoleQuit = false
			w.present(l)
			return true, false
		}
	}
	if w.consoleEdit {
		switch k {
		case "esc":
			w.consoleEdit = false
			w.present(l)
			return true, false
		case "ctrl-c":
			return true, false
		case "tab":
			w.consoleEdit = false
			w.cycle(l, 1)
			return true, false
		case "backtab":
			w.consoleEdit = false
			w.cycle(l, -1)
			return true, false
		case "enter":
			text := strings.TrimSpace(w.consoleText)
			if text != "" {
				w.consoleHist[w.consolePane].record(text)
				_ = l.Session.Send(protocol.Command{Body: protocol.ConsoleAction{
					Op: protocol.ConsoleInput, Pane: w.consolePane, Text: text,
				}})
			}
			w.consoleText = ""
			w.consoleCursor = 0
			w.consoleStart[w.consolePane] = -1
			w.consoleFollow[w.consolePane] = true
			w.present(l)
			w.request(l, false)
			return true, false
		case "up", "ctrl-p":
			if prev, ok := w.consoleHist[w.consolePane].up(w.consoleText); ok {
				w.consoleText = prev
				w.consoleCursor = len(prev)
			}
			w.present(l)
			return true, false
		case "down", "ctrl-n":
			if next, ok := w.consoleHist[w.consolePane].down(w.consoleText); ok {
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
	if m, ok := movekeys.Lookup(k); ok {
		w.move(l, m)
		return true, false
	}
	switch k {
	case "esc", "!":
		w.close(l)
	case "q", "ctrl-c":
		w.consoleQuit = true
		w.present(l)
	case "tab", "l", "right":
		w.cycle(l, 1)
	case "backtab", "h", "left":
		w.cycle(l, -1)
	case ":", "enter":
		w.focusInput(l)
	case "y":
		l.expectClipboard()
		_ = l.Session.Send(protocol.Command{Body: protocol.ConsoleAction{Op: protocol.ConsoleCopy, Pane: w.consolePane}})
	case "/":
		if w.consolePane == consolePaneHTML {
			w.consoleFindActive = true
			w.consoleFindSet = false
			w.consoleFindText = ""
			w.consoleFindCursor = 0
			_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Query: ""}})
			w.present(l)
		}
	case "n":
		if w.consolePane == consolePaneHTML && w.consoleFindText != "" {
			_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Query: w.consoleFindText, Next: true}})
		}
	case "N":
		if w.consolePane == consolePaneHTML && w.consoleFindText != "" {
			_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: protocol.FindHTML, Query: w.consoleFindText, Prev: true}})
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
	if v.Width <= 0 || v.Height <= 0 || w.consoleQuit || w.consoleFindActive {
		return true
	}
	if ev.Wheel != 0 {
		delta := 3
		if ev.Wheel > 0 {
			delta = -3
		}
		w.scroll(l, delta)
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
			w.consoleEdit = false
			w.selectPane(l, pane, true)
		}
	case layout.inputY >= 0 && y == layout.inputY:
		w.focusInput(l)
	case y == layout.footerY && v.Width >= 6 && x >= v.Width-textwidth.StringWidth(">_"):
		w.close(l)
	}
	return true
}

func (w *consoleOverlay) handleFindKey(l *Tab, k string) (handled, quit bool) {
	switch k {
	case "esc", "enter", "tab", "backtab":
		w.hideFind()
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
		w.consoleFindSet = false
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
	if pane < 0 || pane >= consolePaneCount {
		pane = consolePaneLogs
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
	if (pane == consolePaneLogs || pane == consolePaneTools) && l.bodyRows > 0 {
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

// ConsoleUI is the front-end-owned console chrome drawn around a section
// window: pane, input row, footer and find bar.
type ConsoleUI struct {
	Pane       int
	Input      string
	Cursor     int
	Edit       bool
	Hint       string
	Footer     string
	Quit       bool
	FindActive bool
	FindText   string
	FindCursor int
}

// BuildConsoleFrame renders one console window and its local chrome into a
// full-screen frame.
func BuildConsoleFrame(v *protocol.ConsoleView, ui ConsoleUI) protocol.Frame {
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
	l := consoleLayoutFor(h, ui.Pane)
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
			if pane == ui.Pane {
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
				input := protocol.StripControls(ui.Input)
				hint := protocol.StripControls(ui.Hint)
				if ui.Edit {
					runes, start, end, cur := promptWindow(input, ui.Cursor, contentW, false)
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
		if ui.FindActive {
			drawFindBarAt(c, l.footerY, protocol.State{
				FindActive: true, FindText: ui.FindText, FindCursor: ui.FindCursor,
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
		footer := protocol.StripControls(ui.Footer)
		footerFG := fg
		if ui.Quit {
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
func (p *Presenter) PresentConsole(v *protocol.ConsoleView, ui ConsoleUI) error {
	if p == nil || p.out == nil || v == nil {
		return nil
	}
	if !p.consoleActive {
		p.prev = nil
	}
	p.console = v
	p.consoleUI = ui
	p.consoleActive = true
	f := BuildConsoleFrame(v, ui)
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
