package browser

import (
	"os"
	"strings"
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
}

func setMouseReporting(on bool) {
	if on {
		_, _ = os.Stdout.WriteString("\x1b[?1000h\x1b[?1002h\x1b[?1006h")
	} else {
		_, _ = os.Stdout.WriteString("\x1b[?1002l\x1b[?1000l\x1b[?1006l")
	}
}

func (b *Browser) toggleConsole() {
	b.consoleOpen = !b.consoleOpen
	b.consoleScroll = 0
	b.prev = nil
	// With reporting off the terminal's own selection/copy works, so errors
	// and logs can be copied normally.
	setMouseReporting(!b.consoleOpen)
	b.draw()
}

func (b *Browser) handleConsoleKey(k string) bool {
	switch k {
	case "esc", "!":
		b.toggleConsole()
	case "q", "ctrl-c":
		return true
	case "k", "up":
		b.consoleScroll++
	case "j", "down":
		if b.consoleScroll > 0 {
			b.consoleScroll--
		}
	case "g":
		b.consoleScroll = len(b.consoleLines(1 << 20))
	case "G":
		b.consoleScroll = 0
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

func (b *Browser) drawConsole(w, h int) {
	fg := RGB(0xdd, 0xe6, 0xee)
	bg := RGB(0x0f, 0x14, 0x1a)
	g := NewGrid(w, h, fg, bg)
	hdrBG := RGB(0x1f, 0x29, 0x37)
	ftrBG := RGB(0x1b, 0x24, 0x30)
	g.Fill(0, 0, w, 1, hdrBG)
	if h >= 2 {
		g.Fill(0, h-1, w, 1, ftrBG)
	}
	title := " JS console — Esc/! closes · terminal mouse selection is enabled "
	g.Text(0, 0, padWidth(truncateWidth(title, w), w),
		&Style{Bold: true, Color: fg, FontSize: 16, LineHeight: 1}, fg)

	avail := h - 2
	if avail < 1 {
		avail = 1
	}
	lines := b.consoleLines(w - 2)
	start := len(lines) - avail - b.consoleScroll
	if start < 0 {
		start = 0
	}
	if start > len(lines)-avail {
		start = len(lines) - avail
	}
	if start < 0 {
		start = 0
	}
	style := &Style{Color: fg, FontSize: 16, LineHeight: 1}
	for i := 0; i < avail && start+i < len(lines); i++ {
		g.Text(1, 1+i, truncateWidth(lines[start+i], w-2), style, fg)
	}
	if h >= 2 {
		hint := " j/k: scroll · q: quit · Esc: close "
		g.Text(0, h-1, padWidth(truncateWidth(hint, w), w),
			&Style{Color: fg, FontSize: 16, LineHeight: 1}, fg)
	}
	b.flushGrid(g, nil)
}
