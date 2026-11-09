package browser

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// logJS records a JavaScript console/error message for the console view.
func (b *Browser) logJS(kind, msg string) {
	msg = strings.TrimRight(msg, "\n")
	if msg == "" {
		return
	}
	b.jsLog = append(b.jsLog, "["+kind+"] "+msg)
	if len(b.jsLog) > 200 {
		b.jsLog = b.jsLog[len(b.jsLog)-200:]
	}
	if kind == "error" {
		b.jsErr = true
		if strings.HasPrefix(b.status, "JS:") {
			b.status = ""
		}
	}
}

func setMouseReporting(on bool) {
	if on {
		_, _ = os.Stdout.WriteString("\x1b[?1000h\x1b[?1003h\x1b[?1006h")
	} else {
		_, _ = os.Stdout.WriteString("\x1b[?1003l\x1b[?1000l\x1b[?1006l")
	}
}

func (b *Browser) toggleConsole() {
	b.consoleOpen = !b.consoleOpen
	b.consoleScroll = 0
	b.reqScroll = 0
	b.consolePane = 0
	b.prev = nil
	if b.consoleOpen {
		b.jsErr = false
	}
	// Keep mouse reporting on so the panes can be selected by clicking.
	// Terminal text selection needs the usual Shift-drag override.
	setMouseReporting(true)
	b.draw()
}

// closeConsole closes the console like Escape does, leaving the usual exit hint.
func (b *Browser) closeConsole() {
	if !b.consoleOpen {
		return
	}
	b.status = exitHint
	b.toggleConsole()
}

func (b *Browser) handleConsoleKey(k string) bool {
	scroll := func(delta int) {
		if b.consolePane == 1 {
			b.reqScroll += delta
			if b.reqScroll < 0 {
				b.reqScroll = 0
			}
			return
		}
		b.consoleScroll += delta
		if b.consoleScroll < 0 {
			b.consoleScroll = 0
		}
	}
	switch k {
	case "esc":
		b.toggleConsole()
		b.status = exitHint
	case "!":
		b.toggleConsole()
	case "q", "ctrl-c":
		return true
	case "tab":
		if b.consolePane == 0 {
			b.consolePane = 1
		} else {
			b.consolePane = 0
		}
	case "k", "up":
		scroll(1)
	case "j", "down":
		scroll(-1)
	case "g":
		if b.consolePane == 1 {
			b.reqScroll = len(b.requestLines())
		} else {
			b.consoleScroll = len(b.consoleLines(b.width - 2))
		}
	case "G":
		if b.consolePane == 1 {
			b.reqScroll = 0
		} else {
			b.consoleScroll = 0
		}
	}
	return false
}

func (b *Browser) consoleLines(width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, entry := range b.jsLog {
		for _, raw := range strings.Split(entry, "\n") {
			raw = expandTabs(strings.TrimRight(raw, "\r"))
			if raw == "" {
				out = append(out, "")
				continue
			}
			out = append(out, wrapCells(raw, width)...)
		}
	}
	if len(out) == 0 {
		out = append(out, "(no JavaScript messages)")
	}
	return out
}

// wrapCells hard-wraps a string to width cells.
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

func (b *Browser) requestLines() []string {
	if b.loader == nil {
		return []string{"(requests unavailable)"}
	}
	records := b.loader.requestSnapshot()
	out := make([]string, 0, len(records))
	for _, r := range records {
		status := "..."
		size := "-"
		if r.Done {
			status = fmt.Sprintf("%d", r.Status)
			size = humanSize(r.Size)
		}
		if r.Err != "" {
			status = "ERR"
		}
		kind := r.Kind
		if kind == "" {
			kind = "req"
		}
		line := fmt.Sprintf("%3s %-5s %6s %-5s %s", status, r.Method, size, kind, r.URL)
		if r.Err != "" {
			line += "  " + r.Err
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		out = append(out, "(no requests for this page)")
	}
	return out
}

func humanSize(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fK", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/(1024*1024))
	}
}

// consoleLayout describes the console's pane geometry for the current height.
type consoleLayout struct {
	logRows int
	sepY    int // -1 when there is no Requests pane
	reqRows int
	footerY int // -1 when the footer row does not fit
}

func newConsoleLayout(h int) consoleLayout {
	avail := h - 2
	if avail < 0 {
		avail = 0
	}
	l := consoleLayout{logRows: avail, sepY: -1, footerY: -1}
	if h >= 2 {
		l.footerY = h - 1
	}
	if avail >= 5 {
		reqRows := avail / 3
		if reqRows < 1 {
			reqRows = 1
		}
		logRows := avail - reqRows - 1
		if logRows < 1 {
			logRows = 1
			reqRows = avail - logRows - 1
		}
		if reqRows > 0 {
			l.logRows = logRows
			l.sepY = 1 + logRows
			l.reqRows = reqRows
		}
	}
	return l
}

// paneAt returns the pane whose heading or body sits on row y.
func (l consoleLayout) paneAt(y int) int {
	if l.sepY >= 0 && y >= l.sepY {
		return 1
	}
	return 0
}

// handleConsoleMouse switches the active pane on click and scrolls the pane
// under the wheel. Plain left clicks only; dragging is left to the terminal.
func (b *Browser) handleConsoleMouse(ev MouseEvent) {
	if b.width < 1 || b.height < 1 {
		return
	}
	x := ev.X - 1
	y := ev.Y - 1
	if x < 0 || y < 0 || x >= b.width || y >= b.height {
		return
	}
	l := newConsoleLayout(b.height)
	if ev.Wheel != 0 {
		if y == l.footerY {
			return
		}
		step := -3
		if ev.Wheel < 0 {
			step = 3
		}
		if l.paneAt(y) == 1 {
			b.reqScroll += step
			if b.reqScroll < 0 {
				b.reqScroll = 0
			}
		} else {
			b.consoleScroll += step
			if b.consoleScroll < 0 {
				b.consoleScroll = 0
			}
		}
		return
	}
	if ev.Motion || !ev.Press || ev.Button != 0 {
		return
	}
	if y == l.footerY {
		for _, hit := range b.footerHits {
			if x < hit.x || x >= hit.x+hit.w || y < hit.y || y >= hit.y+hit.h {
				continue
			}
			if hit.kind == headerConsole {
				b.closeConsole()
				return
			}
		}
		return
	}
	b.consolePane = l.paneAt(y)
}

func (b *Browser) drawConsole(w, h int) {
	fg := RGB(0xdd, 0xe6, 0xee)
	bg := RGB(0x0f, 0x14, 0x1a)
	g := NewGrid(w, h, fg, bg)
	hdrBG := RGB(0x1f, 0x29, 0x37)
	ftrBG := RGB(0x1b, 0x24, 0x30)
	activeBG := RGB(0x2f, 0x6f, 0xbf)
	headerBG := hdrBG
	if b.consolePane == 0 {
		headerBG = activeBG
	}
	g.Fill(0, 0, w, 1, headerBG)
	b.footerHits = b.footerHits[:0]
	if h >= 2 {
		g.Fill(0, h-1, w, 1, ftrBG)
	}
	title := fmt.Sprintf(" JavaScript logs (%d) — Tab/click: switch · Esc/!: close ", len(b.jsLog))
	g.Text(0, 0, padWidth(truncateWidth(title, w), w),
		&Style{Bold: true, Color: fg, FontSize: 16, LineHeight: 1}, fg)

	lines := b.consoleLines(w - 2)
	reqs := b.requestLines()
	l := newConsoleLayout(h)
	style := &Style{Color: fg, FontSize: 16, LineHeight: 1}
	drawPane := func(items []string, scroll, y, rows int) {
		if rows < 1 {
			return
		}
		start := len(items) - rows - scroll
		if start < 0 {
			start = 0
		}
		if start > len(items)-rows {
			start = len(items) - rows
		}
		if start < 0 {
			start = 0
		}
		for i := 0; i < rows && start+i < len(items); i++ {
			g.Text(1, y+i, truncateWidth(items[start+i], w-2), style, fg)
		}
	}
	drawPane(lines, b.consoleScroll, 1, l.logRows)
	if l.reqRows > 0 {
		sepBG := ftrBG
		if b.consolePane == 1 {
			sepBG = activeBG
		}
		g.Fill(0, l.sepY, w, 1, sepBG)
		sep := fmt.Sprintf(" Requests (%d) — Tab/click: switch pane ", len(reqs))
		g.Text(0, l.sepY, padWidth(truncateWidth(sep, w), w),
			&Style{Bold: true, Color: fg, FontSize: 16, LineHeight: 1}, fg)
		drawPane(reqs, b.reqScroll, l.sepY+1, l.reqRows)
	}
	if h >= 2 {
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
		hint := " Tab/click: switch · j/k: scroll · g/G: ends · Shift+drag: select · q: quit · Esc: close "
		g.Text(0, h-1, padWidth(truncateWidth(hint, hintW), hintW),
			&Style{Color: fg, FontSize: 16, LineHeight: 1}, fg)
		if reserve > 0 {
			iconStyle := &Style{Bold: true, Color: RGB(0x8a, 0xc4, 0xff), FontSize: 16, LineHeight: 1}
			g.Text(w-iconW, h-1, icon, iconStyle, iconStyle.Color)
			b.footerHits = append(b.footerHits, headerHit{kind: headerConsole, x: w - iconW, y: h - 1, w: iconW, h: 1})
		}
	}
	b.flushGrid(g, nil)
}

// jsDialog is a modal JavaScript alert/confirm/prompt.
type jsDialog struct {
	kind      string // alert, confirm, prompt
	message   string
	text      string
	cursor    int
	selectAll bool
}

// showAlert displays an alert dialog. Non-interactive sessions only log it.
func (b *Browser) showAlert(msg string) {
	d := &jsDialog{kind: "alert", message: msg}
	b.runDialog(d)
}

// showConfirm returns the confirm result; the default is false when no
// interactive input is available.
func (b *Browser) showConfirm(msg string) bool {
	d := &jsDialog{kind: "confirm", message: msg}
	_, ok := b.runDialog(d)
	return ok
}

// showPrompt returns the entered text and whether OK was chosen. Cancel
// returns false (JS null).
func (b *Browser) showPrompt(msg, def string) (string, bool) {
	d := &jsDialog{kind: "prompt", message: msg, text: def, cursor: len(def), selectAll: true}
	return b.runDialog(d)
}

// runDialog blocks the JS caller while the modal owns the keyboard.
func (b *Browser) runDialog(d *jsDialog) (string, bool) {
	if b.keys == nil {
		return d.text, d.kind == "alert"
	}
	b.dialog = d
	defer func() {
		b.dialog = nil
		b.draw()
	}()
	for {
		b.draw()
		k, ok := <-b.keys
		if !ok || k == "eof" {
			return "", false
		}
		switch d.kind {
		case "alert":
			switch k {
			case "enter", " ", "esc":
				return "", true
			}
		case "confirm":
			switch k {
			case "enter", "y", "Y":
				return "", true
			case "esc", "n", "N", "ctrl-c":
				return "", false
			}
		case "prompt":
			text, cur, done, ok := dialogPromptKey(d, k)
			d.text, d.cursor = text, cur
			if done {
				return d.text, ok
			}
		}
	}
}

func dialogPromptKey(d *jsDialog, k string) (text string, cursor int, done, ok bool) {
	text = d.text
	cursor = d.cursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(text) {
		cursor = len(text)
	}
	switch k {
	case "enter":
		return text, cursor, true, true
	case "esc", "ctrl-c":
		return text, cursor, true, false
	case "left":
		if d.selectAll {
			cursor = 0
			d.selectAll = false
		} else {
			cursor = prevRuneBoundary(text, cursor)
		}
	case "right":
		if d.selectAll {
			cursor = len(text)
			d.selectAll = false
		} else {
			cursor = nextRuneBoundary(text, cursor)
		}
	case "home":
		cursor = 0
		d.selectAll = false
	case "end":
		cursor = len(text)
		d.selectAll = false
	case "ctrl-a":
		d.selectAll = true
		cursor = len(text)
	case "backspace":
		if d.selectAll {
			text = ""
			cursor = 0
			d.selectAll = false
		} else if cursor > 0 {
			prev := prevRuneBoundary(text, cursor)
			text = text[:prev] + text[cursor:]
			cursor = prev
		}
	case "delete":
		if d.selectAll {
			text = ""
			cursor = 0
			d.selectAll = false
		} else if cursor < len(text) {
			next := nextRuneBoundary(text, cursor)
			text = text[:cursor] + text[next:]
		}
	case "ctrl-u":
		text = ""
		cursor = 0
		d.selectAll = false
	case "ctrl-w":
		if d.selectAll {
			text = ""
			cursor = 0
			d.selectAll = false
			break
		}
		before := strings.TrimRight(text[:cursor], " ")
		cut := 0
		if i := strings.LastIndexAny(before, " /"); i >= 0 {
			cut = i + 1
		}
		text = text[:cut] + text[cursor:]
		cursor = cut
	default:
		if utf8.RuneCountInString(k) == 1 {
			r, _ := utf8.DecodeRuneInString(k)
			if r >= 32 && r != 127 {
				if d.selectAll {
					text = k
					cursor = len(k)
					d.selectAll = false
				} else {
					text = text[:cursor] + k + text[cursor:]
					cursor += len(k)
				}
			}
		}
	}
	return text, cursor, false, false
}

// paintDialog overlays the active modal on the current grid.
func (b *Browser) paintDialog(g *Grid) {
	d := b.dialog
	if d == nil {
		return
	}
	boxW := minInt(g.W-4, 68)
	if boxW < 20 {
		boxW = g.W
	}
	if boxW < 4 {
		return
	}
	inner := boxW - 4
	promptRow := 0
	if d.kind == "prompt" {
		promptRow = 1
	}
	msg := wrapCells(d.message, inner)
	maxLines := g.H - 4 - promptRow
	if maxLines < 1 {
		maxLines = 1
	}
	if len(msg) > maxLines {
		msg = msg[:maxLines]
	}
	boxH := 4 + len(msg) + promptRow
	if boxH > g.H {
		boxH = g.H
	}
	x := (g.W - boxW) / 2
	y := (g.H - boxH) / 2
	bg := RGB(0xf6, 0xf8, 0xfa)
	fg := RGB(0x1f, 0x23, 0x28)
	border := RGB(0x2f, 0x6f, 0xbf)
	titleBG := RGB(0x2f, 0x6f, 0xbf)
	titleFG := RGB(0xff, 0xff, 0xff)
	g.Fill(x, y, boxW, boxH, bg)
	g.Fill(x, y, boxW, 1, border)
	g.Fill(x, y+boxH-1, boxW, 1, border)
	g.Fill(x, y, 1, boxH, border)
	g.Fill(x+boxW-1, y, 1, boxH, border)
	g.Fill(x+1, y+1, boxW-2, 1, titleBG)
	title := " JavaScript " + d.kind + " "
	style := &Style{Bold: true, Color: fg, FontSize: 16, LineHeight: 1}
	titleStyle := &Style{Bold: true, Color: titleFG, FontSize: 16, LineHeight: 1}
	g.Text(x+2, y+1, truncateWidth(title, boxW-4), titleStyle, titleFG)
	for i, line := range msg {
		g.Text(x+2, y+2+i, padWidth(truncateWidth(line, inner), inner), style, fg)
	}
	row := y + 2 + len(msg)
	if d.kind == "prompt" {
		g.Fill(x+2, row, inner, 1, RGB(0xe8, 0xf0, 0xfe))
		head, at, tail, show := promptParts("", d.text, d.cursor, d.selectAll, inner)
		g.Text(x+2, row, head, style, fg)
		cx := x + 2 + stringWidth(head)
		if show {
			g.Put(cx, row, at, &Style{Bold: true}, titleFG, border)
			cx++
		}
		g.Text(cx, row, tail, style, fg)
	}
	hint := " Enter: OK · Esc: Cancel "
	if d.kind == "alert" {
		hint = " Enter / Space / Esc: OK "
	} else if d.kind == "confirm" {
		hint = " Enter/Y: Yes · N/Esc: No "
	}
	g.Text(x+2, y+boxH-2, padWidth(truncateWidth(hint, boxW-4), boxW-4), style, fg)

	// Shadow-free simple modal: dim the outermost columns to make the box pop.
	for yy := 0; yy < g.H; yy++ {
		if yy >= y && yy < y+boxH {
			continue
		}
		for xx := 0; xx < g.W; xx++ {
			c := &g.Cells[yy*g.W+xx]
			c.BG = RGB(0xdf, 0xe4, 0xea)
		}
	}
}
