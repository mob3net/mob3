package browser

import (
	"strings"
	"unicode/utf8"
)

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
