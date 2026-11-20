package browser

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"unicode"
)

// scrollBarInfo records the page scrollbar track/thumb for mouse hit testing.
type scrollBarInfo struct {
	trackTop, trackH int
	thumbTop, thumbH int
}

type headerKind int

const (
	headerBack headerKind = iota
	headerForward
	headerReload
	headerStop
	headerHome
	headerBookmarks
	headerHistory
	headerURL
	headerHelp
	headerConfig
	headerConsole
	headerJS
	headerReader
	headerDark
	headerGraphics
	headerDownload
	headerStatus
)

// headerHit is a clickable region in the top bar, in zero-based screen cells.
type headerHit struct {
	kind headerKind
	x, y int
	w, h int
}

// consoleTabHit is a clickable console menu tab.
type consoleTabHit struct {
	pane int
	x, w int
}

// drawScrollBar paints the page scrollbar on the rightmost content column.
func (b *Browser) drawScrollBar(g *Grid, top, bottom int) {
	b.scrollBar = nil
	max := b.maxScroll()
	vh := bottom - top
	if max <= 0 || vh < 2 || g.W < 2 {
		return
	}
	thumbH := vh * vh / (vh + max)
	if thumbH < 1 {
		thumbH = 1
	}
	if thumbH > vh {
		thumbH = vh
	}
	thumbTop := top
	if vh > thumbH {
		thumbTop = top + (vh-thumbH)*b.scroll/max
	}
	g.Fill(g.W-1, top, 1, vh, RGB(0xdd, 0xe3, 0xea))
	g.Fill(g.W-1, thumbTop, 1, thumbH, RGB(0x7a, 0x8a, 0x9a))
	b.scrollBar = &scrollBarInfo{trackTop: top, trackH: vh, thumbTop: thumbTop, thumbH: thumbH}
}

// scrollBarTo maps a track row to a scroll position, centering the thumb.
func (b *Browser) scrollBarTo(y int) {
	info := b.scrollBar
	if info == nil {
		return
	}
	max := b.maxScroll()
	rel := y - info.trackTop - info.thumbH/2
	span := info.trackH - info.thumbH
	if span <= 0 || max <= 0 {
		b.scrollBy(-b.scroll)
		return
	}
	if rel < 0 {
		rel = 0
	}
	if rel > span {
		rel = span
	}
	target := rel * max / span
	b.scrollBy(target - b.scroll)
}

func (b *Browser) pageFG() Color {
	if b.layout != nil && b.layout.Body != nil && b.layout.Body.Style != nil && b.layout.Body.Style.Color.Set {
		return b.layout.Body.Style.Color
	}
	return RGB(0x1f, 0x23, 0x28)
}

func (b *Browser) pageBG() Color {
	if b.layout != nil && b.layout.PageBG.Set {
		return b.layout.PageBG
	}
	return RGB(255, 255, 255)
}

// drawShortcutLabel draws a label with its first letter underlined to suggest
// the keyboard shortcut. Leading punctuation or symbols such as '≡' are left
// untouched, so "Dark:off" underlines the D and "≡ History" the H.
func drawShortcutLabel(g *Grid, x, y int, label string, st *Style, fg Color) {
	if label == "" {
		return
	}
	idx := -1
	var shortcut rune
	for i, r := range label {
		if unicode.IsLetter(r) {
			idx, shortcut = i, r
			break
		}
	}
	if idx < 0 {
		g.Text(x, y, label, st, fg)
		return
	}
	if prefix := label[:idx]; prefix != "" {
		g.Text(x, y, prefix, st, fg)
		x += stringWidth(prefix)
	}
	under := *st
	under.Underline = true
	g.Put(x, y, shortcut, &under, fg, Color{})
	x += runeWidth(shortcut)
	if rest := label[idx+len(string(shortcut)):]; rest != "" {
		g.Text(x, y, rest, st, fg)
	}
}

// drawUnderlinedRune draws label with the first occurrence of target
// underlined, used when a footer label's shortcut is not its first letter.
func drawUnderlinedRune(g *Grid, x, y int, label string, target rune, st *Style, fg Color) {
	idx := strings.IndexRune(label, target)
	if idx < 0 {
		g.Text(x, y, label, st, fg)
		return
	}
	if prefix := label[:idx]; prefix != "" {
		g.Text(x, y, prefix, st, fg)
		x += stringWidth(prefix)
	}
	under := *st
	under.Underline = true
	g.Put(x, y, target, &under, fg, Color{})
	x += runeWidth(target)
	if rest := label[idx+len(string(target)):]; rest != "" {
		g.Text(x, y, rest, st, fg)
	}
}

// drawHeaderButton paints one top-bar button and records its click region.
func (b *Browser) drawHeaderButton(g *Grid, x, y int, label string, kind headerKind, bg, fg Color, st *Style) int {
	bw := stringWidth(label)
	if x+bw > g.W || y < 0 || y >= g.H {
		return x
	}
	g.Fill(x, y, bw, 1, bg)
	if kind == headerHistory {
		drawShortcutLabel(g, x, y, label, st, fg)
	} else {
		g.Text(x, y, label, st, fg)
	}
	b.headerHits = append(b.headerHits, headerHit{kind: kind, x: x, y: y, w: bw, h: 1})
	return x + bw
}

// draw renders the UI and writes changed lines to the terminal.
// paintInputSelection paints the selected value area of the focused field after
// Ctrl-A. The brackets and any padding outside the value stay in the field's
// normal focused colors.
func (b *Browser) paintInputSelection(g *Grid, top, bottom int) {
	n := b.inputFocus
	if !b.inputSelectAll || n == nil || b.layout == nil {
		return
	}
	selBG := RGB(0x1f, 0x4f, 0x9f)
	selFG := RGB(0xf8, 0xfa, 0xfc)
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Kind != fragText || f.Node != n || f.W < 4 {
			continue
		}
		y := f.Y - b.scroll + top
		if y < top || y >= bottom || y >= g.H {
			continue
		}
		for x := f.X + 2; x < f.X+f.W-2; x++ {
			if x < 0 || x >= g.W {
				continue
			}
			cell := &g.Cells[y*g.W+x]
			if cell.Img {
				continue
			}
			cell.BG = selBG
			if cell.R != ' ' && cell.R != 0 {
				cell.FG = selFG
			}
		}
	}
}

// paintInputCursor draws the caret over a focused input or textarea.
func (b *Browser) paintInputCursor(g *Grid, top, bottom int) {
	if b.layout == nil || !b.layout.HasCursor || b.inputSelectAll {
		return
	}
	r := b.layout.Cursor
	x, y := r.X, r.Y-b.scroll+top
	if x < 0 || x >= g.W || y < top || y >= bottom {
		return
	}
	ch := g.Cells[y*g.W+x].R
	if ch == 0 || runeWidth(ch) != 1 {
		ch = ' '
	}
	g.Put(x, y, ch, &Style{Bold: true}, RGB(0x2f, 0x6f, 0xbf), RGB(0xff, 0xff, 0xff))
}

func (b *Browser) draw() {
	if b.noDraw {
		return
	}
	w, h := b.width, b.height
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if b.consoleOpen {
		b.drawConsole(w, h)
		return
	}
	fg, bg := b.pageFG(), b.pageBG()
	g := NewGrid(w, h, fg, bg)
	top, bottom := b.viewport()
	var placements []ImagePlacement
	var collect *[]ImagePlacement
	// While hints are shown, draw images as half-block art instead of native
	// inline graphics so the hint letters are painted above them. Inline image
	// protocols are composited by the terminal after the text grid.
	if b.inlineImagesEnabled() {
		collect = &placements
	}
	if b.layout != nil {
		b.layout.SetFixedScroll(b.scroll)
	}
	PaintLayoutBig(g, b.layout, b.scroll, top, bottom, b.focusedLink(), fg, collect, b.bigText)
	b.paintControlFocus(g, top, bottom)
	b.paintInputSelection(g, top, bottom)
	b.paintInputCursor(g, top, bottom)
	b.paintSelection(g, top, bottom)
	b.paintFindPage(g, top, bottom)
	b.paintLinkHints(g, top, bottom)
	if !b.noDraw && b.width >= 2 {
		// Overflowing content (wide nowrap blocks, absolute boxes) may still
		// reach the terminal edge. Keep the reserved scrollbar column blank.
		g.ClearGlyphs(b.width-1, top, 1, bottom-top)
	}
	b.drawScrollBar(g, top, bottom)

	headerBG := RGB(0x1f, 0x29, 0x37)
	footerBG := RGB(0xea, 0xee, 0xf2)
	headerFG := RGB(0xf6, 0xf8, 0xfa)
	footerFG := RGB(0x24, 0x29, 0x2f)

	if h >= 2 {
		// Header: a two-row toolbar on normal terminals, one row when short.
		rows := b.headerRows()
		if rows < 1 {
			rows = 1
		}
		b.headerHits = b.headerHits[:0]
		b.footerHits = b.footerHits[:0]
		hs := &Style{Bold: true, Color: headerFG, FontSize: 16, LineHeight: 1}
		us := &Style{Color: headerFG, FontSize: 16, LineHeight: 1}
		btnBG := RGB(0x2b, 0x3a, 0x4c)
		urlBG := RGB(0x13, 0x1d, 0x29)
		promptBG := RGB(0x27, 0x44, 0x63)
		for ry := 0; ry < rows; ry++ {
			g.Fill(0, ry, w, 1, headerBG)
		}
		if rows >= 2 {
			g.FillHGradient(0, 0, w, 1, RGB(0x1f, 0x29, 0x37), RGB(0x31, 0x43, 0x5c))
		}

		backLabel, helpLabel := " ← Back ", " ? Help "
		forwardLabel := " → "
		reloadLabel, reloadKind := " ⟳ ", headerReload
		if b.loading {
			reloadLabel, reloadKind = " × ", headerStop
		}
		homeLabel := " ⌂ "
		historyLabel := " ≡ History "
		bookmarksLabel := " ★ Bookmarks "
		configLabel := " ⚙ "
		if w < 80 {
			backLabel = " ← "
			helpLabel, historyLabel, bookmarksLabel = " ? ", " ≡ ", " ★ "
		}
		fieldBG := urlBG
		if b.prompt {
			fieldBG = promptBG
			if b.promptSelectAll {
				fieldBG = RGB(0x2f, 0x6f, 0xbf)
			}
		}

		drawField := func(x, y, width int) {
			if width < 1 {
				return
			}
			g.Fill(x, y, width, 1, fieldBG)
			if b.progress > 0 {
				filled := int(b.progress*float64(width) + 0.5)
				if filled < 1 {
					filled = 1
				}
				if filled > width {
					filled = width
				}
				g.Fill(x, y, filled, 1, RGB(0x1f, 0x4f, 0x8a))
			}
			if b.prompt {
				head, at, tail, show := promptParts(" Open: ", b.promptText, b.promptCursor, b.promptSelectAll, width)
				g.Text(x, y, head, hs, headerFG)
				cx := x + stringWidth(head)
				if show {
					cursorFG := RGB(0xff, 0xff, 0xff)
					cursorBG := RGB(0x2f, 0x6f, 0xbf)
					g.Put(cx, y, at, &Style{Bold: true}, cursorFG, cursorBG)
					cx++
				}
				g.Text(cx, y, tail, hs, headerFG)
			} else {
				u := ""
				if b.url != nil {
					u = b.url.String()
				}
				g.Text(x, y, fitWidth(" "+u+" ", width), us, headerFG)
			}
			b.headerHits = append(b.headerHits, headerHit{kind: headerURL, x: x, y: y, w: width, h: 1})
		}

		if rows >= 2 {
			// Toolbar row: buttons, page title, help.
			x := 0
			if w >= 24 {
				x = b.drawHeaderButton(g, x, 0, backLabel, headerBack, btnBG, headerFG, hs)
				x = b.drawHeaderButton(g, x, 0, forwardLabel, headerForward, btnBG, headerFG, hs)
				x = b.drawHeaderButton(g, x, 0, reloadLabel, reloadKind, btnBG, headerFG, hs)
				x = b.drawHeaderButton(g, x, 0, homeLabel, headerHome, btnBG, headerFG, hs)
			} else if w >= 12 {
				x = b.drawHeaderButton(g, x, 0, homeLabel, headerHome, btnBG, headerFG, hs)
			}
			helpW, historyW, bookmarksW, configW := 0, 0, 0, 0
			if w >= 6 {
				helpW = stringWidth(helpLabel)
				configW = stringWidth(configLabel)
			}
			if w >= 12 {
				historyW = stringWidth(historyLabel)
			}
			if w >= 18 && b.hasBookmarks() {
				bookmarksW = stringWidth(bookmarksLabel)
			}
			for x+helpW+historyW+bookmarksW+configW > w {
				switch {
				case bookmarksW > 0:
					bookmarksW = 0
				case historyW > 0:
					historyW = 0
				case helpW > 0:
					helpW = 0
				default:
					configW = 0
				}
			}
			configX := w - configW
			helpX := configX - helpW
			histX := helpX - historyW
			bookX := histX - bookmarksW
			title := "MOB³"
			if b.page != nil && b.page.Title != "" {
				title = b.page.Title
			}
			if room := bookX - x; room > 0 {
				titleFG := RGB(0xff, 0xd7, 0x66)
				titleStyle := &Style{Bold: true, Color: titleFG, FontSize: 16, LineHeight: 1}
				tx, label := centeredLabel(x, room, title)
				g.Text(tx, 0, label, titleStyle, titleFG)
			}
			if bookmarksW > 0 {
				b.drawHeaderButton(g, bookX, 0, bookmarksLabel, headerBookmarks, btnBG, headerFG, hs)
			}
			if historyW > 0 {
				b.drawHeaderButton(g, histX, 0, historyLabel, headerHistory, btnBG, headerFG, hs)
			}
			if helpW > 0 {
				b.drawHeaderButton(g, helpX, 0, helpLabel, headerHelp, btnBG, headerFG, hs)
			}
			if configW > 0 {
				b.drawHeaderButton(g, configX, 0, configLabel, headerConfig, btnBG, headerFG, hs)
			}
			// URL row.
			drawField(0, 1, w)
		} else {
			x := 0
			if w >= 24 {
				x = b.drawHeaderButton(g, x, 0, backLabel, headerBack, btnBG, headerFG, hs)
				x = b.drawHeaderButton(g, x, 0, forwardLabel, headerForward, btnBG, headerFG, hs)
				x = b.drawHeaderButton(g, x, 0, reloadLabel, reloadKind, btnBG, headerFG, hs)
				x = b.drawHeaderButton(g, x, 0, homeLabel, headerHome, btnBG, headerFG, hs)
			} else if w >= 12 {
				x = b.drawHeaderButton(g, x, 0, homeLabel, headerHome, btnBG, headerFG, hs)
			}
			helpW, historyW, bookmarksW, configW := 0, 0, 0, 0
			if w >= 6 {
				helpW = stringWidth(helpLabel)
				configW = stringWidth(configLabel)
			}
			if w >= 12 {
				historyW = stringWidth(historyLabel)
			}
			if w >= 18 && b.hasBookmarks() {
				bookmarksW = stringWidth(bookmarksLabel)
			}
			for x+helpW+historyW+bookmarksW+configW > w {
				switch {
				case bookmarksW > 0:
					bookmarksW = 0
				case historyW > 0:
					historyW = 0
				case helpW > 0:
					helpW = 0
				default:
					configW = 0
				}
			}
			urlW := w - x - helpW - historyW - bookmarksW - configW
			if urlW < 1 {
				bookmarksW = 0
				urlW = w - x - helpW - historyW - configW
			}
			if urlW < 1 {
				historyW = 0
				urlW = w - x - helpW - configW
			}
			if urlW < 1 {
				helpW = 0
				urlW = w - x - configW
			}
			if urlW < 1 {
				configW = 0
				urlW = w - x
			}
			drawField(x, 0, urlW)
			if bookmarksW > 0 {
				b.drawHeaderButton(g, w-configW-helpW-historyW-bookmarksW, 0, bookmarksLabel, headerBookmarks, btnBG, headerFG, hs)
			}
			if historyW > 0 {
				b.drawHeaderButton(g, w-configW-helpW-historyW, 0, historyLabel, headerHistory, btnBG, headerFG, hs)
			}
			if helpW > 0 {
				b.drawHeaderButton(g, w-configW-helpW, 0, helpLabel, headerHelp, btnBG, headerFG, hs)
			}
			if configW > 0 {
				b.drawHeaderButton(g, w-configW, 0, configLabel, headerConfig, btnBG, headerFG, hs)
			}
		}

		// Footer.
		fy := h - 1
		g.Fill(0, fy, w, 1, footerBG)
		if b.findActive {
			b.drawFindBar(g, fy, w)
		} else {
			fs := &Style{Color: footerFG, FontSize: 16, LineHeight: 1}
			consoleIcon := ">_"
			consoleW := stringWidth(consoleIcon)
			showConsole := w >= 6
			reserve := 0
			consoleX := w - consoleW
			if showConsole {
				reserve += consoleW + 2
			}
			rightBudget := w/2 - reserve - footerGap
			if rightBudget < 1 {
				rightBudget = 1
			}
			// The right-side state collapses according to terminal width only.
			// Reserve at least half the footer for the left status/URL so a long
			// message never costs more right-side labels than a short one.
			rightText := b.footerRight(rightBudget)
			avail := w - stringWidth(rightText) - reserve - footerGap
			if avail < 0 {
				avail = 0
			}
			leftText := b.footerLeft()
			pdfHint, pdfHintX := "", -1
			if leftText == " Ready" && b.page != nil && b.page.PDF != nil {
				if text, hint, cell, ok := b.pdfFooterLine(avail); ok {
					leftText, pdfHint, pdfHintX = text, hint, cell
				}
			}
			leftStyle := fs
			leftColor := footerFG
			if b.hintConfirm {
				leftColor = RGB(0x9a, 0x5b, 0x00)
				leftStyle = &Style{Bold: true, Color: leftColor, FontSize: 16, LineHeight: 1}
			} else if b.status != "" && (b.quitPending || b.hoveredLink() == nil) {
				switch statusLevelFor(b.status) {
				case statusWarning:
					leftColor = RGB(0x9a, 0x5b, 0x00)
					leftStyle = &Style{Bold: true, Color: leftColor, FontSize: 16, LineHeight: 1}
				case statusError:
					leftColor = RGB(0xb3, 0x1d, 0x1d)
					leftStyle = &Style{Bold: true, Color: leftColor, FontSize: 16, LineHeight: 1}
				case statusSuccess:
					leftColor = RGB(0x17, 0x6b, 0x35)
					leftStyle = &Style{Color: leftColor, FontSize: 16, LineHeight: 1}
				}
			}
			g.Text(0, fy, padWidth(truncateWidth(leftText, avail), avail), leftStyle, leftColor)
			if b.status != "" && !b.hintConfirm && !b.quitPending && b.hoveredLink() == nil {
				if lvl := statusLevelFor(b.status); lvl == statusWarning || lvl == statusError {
					sw := stringWidth(leftText)
					if sw > avail {
						sw = avail
					}
					if sw > 0 {
						b.footerHits = append(b.footerHits, headerHit{kind: headerStatus, x: 0, y: fy, w: sw, h: 1})
					}
					if lvl == statusError {
						drawStatusShortcut(g, 0, fy, avail, leftText, leftStyle, leftColor)
					}
				}
			}
			if pdfHint != "" && pdfHintX >= 0 {
				hintStyle := &Style{Bold: true, Color: RGB(0x1f, 0x4f, 0x9f), FontSize: 16, LineHeight: 1}
				g.Text(pdfHintX, fy, pdfHint, hintStyle, hintStyle.Color)
				b.footerHits = append(b.footerHits, headerHit{kind: headerDownload, x: pdfHintX, y: fy, w: stringWidth(pdfHint), h: 1})
			}
			if rightText != "" {
				rightX := w - reserve - stringWidth(rightText)
				g.Text(rightX, fy, rightText, fs, footerFG)
				labels := []struct {
					text string
					kind headerKind
					on   bool
					err  bool
				}{
					{b.graphicsStatusText(), headerGraphics, false, false},
					{b.jsStatusText(), headerJS, !b.noJS, b.jsErr},
					{b.darkStatusText(), headerDark, b.dark, false},
					{b.readerStatusText(), headerReader, b.reader, false},
				}
				for _, label := range labels {
					i := strings.Index(rightText, label.text)
					if i < 0 {
						continue
					}
					lx := rightX + stringWidth(rightText[:i])
					lw := stringWidth(label.text)
					switch {
					case label.err:
						errStyle := &Style{Bold: true, Color: RGB(0xb3, 0x1d, 0x1d), FontSize: 16, LineHeight: 1}
						drawShortcutLabel(g, lx, fy, label.text, errStyle, errStyle.Color)
					case label.kind == headerGraphics:
						// Underline the i in "Graphics" to advertise the i key.
						drawUnderlinedRune(g, lx, fy, label.text, 'i', fs, fs.Color)
					default:
						labelStyle := fs
						if label.on {
							labelStyle = &Style{Bold: true, Color: RGB(0x1f, 0x4f, 0x9f), FontSize: 16, LineHeight: 1}
						}
						drawShortcutLabel(g, lx, fy, label.text, labelStyle, labelStyle.Color)
					}
					b.footerHits = append(b.footerHits, headerHit{kind: label.kind, x: lx, y: fy, w: lw, h: 1})
				}
			}
			if showConsole {
				iconStyle := &Style{Bold: true, Color: RGB(0x1f, 0x4f, 0x9f), FontSize: 16, LineHeight: 1}
				g.Text(consoleX, fy, consoleIcon, iconStyle, iconStyle.Color)
				b.footerHits = append(b.footerHits, headerHit{kind: headerConsole, x: consoleX, y: fy, w: consoleW, h: 1})
			}
		}
	}

	b.flushGrid(g, placements)
}

// flushGrid writes changed cells to the terminal.
func (b *Browser) flushGrid(g *Grid, placements []ImagePlacement) {
	b.modalClose = Rect{}
	b.modalBox = Rect{}
	switch {
	case b.dialog != nil:
		b.paintDialog(g)
	case b.download != nil:
		b.paintDownload(g)
	case b.notice != nil:
		b.paintNotice(g)
	case b.selectMenu != nil:
		b.paintSelectMenu(g)
	case b.viewMenu != nil:
		b.paintViewMenu(g)
	}
	var lines []string
	if b.color {
		lines = g.Lines(true)
	} else {
		lines = g.LinesMono()
	}
	// Scrolling can leave the terminal in a shifted state (for example after a
	// stray wrap), and cells covered by scaled text are skipped when painting.
	// Clear the whole screen and repaint on any scroll change so stale content
	// can never show through the skipped cells.
	if b.scroll != b.lastScroll {
		b.lastScroll = b.scroll
		b.prev = nil
	}
	// Entering or leaving hint/modal mode switches between native inline
	// images and half-block art. Repaint everything so no stale terminal
	// image survives.
	if native := b.inlineImagesEnabled(); native != b.lastNativeImages {
		b.lastNativeImages = native
		b.prev = nil
	}
	needClear := b.prev == nil || len(b.prev) != len(lines)
	if needClear {
		b.prev = make([]string, len(lines))
	}
	var sb strings.Builder
	if b.gfx != nil {
		b.gfx.BeginFrame(&sb)
	}
	if needClear {
		if b.gfx != nil {
			b.gfx.ClearScreen(&sb)
		} else {
			sb.WriteString("\x1b[2J")
		}
	}
	chrome := func(i int) bool {
		if b.height >= 2 && i == b.height-1 {
			return true
		}
		rows := b.headerRows()
		if i < 0 || i >= rows {
			return false
		}
		// Leave the URL row to normal line diffing once it has been drawn.
		// Periodic memory/JS redraws must not overwrite it, otherwise a
		// terminal-native selection over the URL would be cleared.
		if rows >= 2 && i == 1 {
			return false
		}
		return true
	}
	for i := range lines {
		if !chrome(i) && b.prev[i] == lines[i] {
			continue
		}
		sb.WriteString(fmt.Sprintf("\x1b[%d;1H", i+1))
		sb.WriteString(lines[i])
		if !b.color {
			// No-color lines are trimmed, so clear any longer previous cell
			// run left on the row (for example an old URL in the prompt).
			sb.WriteString("\x1b[K")
		}
		b.prev[i] = lines[i]
	}
	if b.gfx != nil {
		b.gfx.PlaceAll(&sb, placements)
	}
	if sb.Len() > 0 {
		_, _ = os.Stdout.WriteString(sb.String())
	}
}

// tailWidth returns the trailing part of s that fits in max cells.
func tailWidth(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	w := 0
	start := len(runes)
	for start > 0 {
		rw := runeWidth(runes[start-1])
		if w+rw > max {
			break
		}
		w += rw
		start--
	}
	return string(runes[start:])
}

func (b *Browser) scrollPercent() int {
	if b.layout == nil || b.layout.Height <= 0 {
		return 0
	}
	view := b.layout.Height - b.viewportHeight()
	if view <= 0 {
		return 0
	}
	pct := b.scroll * 100 / view
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}

type statusLevel int

const (
	statusNormal statusLevel = iota
	statusWarning
	statusError
	statusSuccess
)

// statusLevelFor classifies footer status text so warnings and errors stand
// out without every caller having to tag its message.
func statusLevelFor(s string) statusLevel {
	switch {
	case strings.HasPrefix(s, "Error:"),
		strings.HasPrefix(s, "Invalid URL:"),
		s == "Invalid form action",
		s == "Copy failed":
		return statusError
	case strings.HasPrefix(s, "Copied "):
		return statusSuccess
	case s == moduleFailedStatus,
		s == layoutTruncatedStatus,
		s == quitConfirmText,
		strings.HasPrefix(s, "Form: "),
		strings.HasPrefix(s, "Site blocked"),
		strings.HasPrefix(s, "HTTP "),
		strings.HasPrefix(s, "Redirect loop stopped"),
		strings.HasPrefix(s, "Blocked local file navigation"),
		strings.HasPrefix(s, "Config is not available in reader mode"),
		strings.HasPrefix(s, "Reader mode:"),
		s == "Reader mode is unavailable on the config page",
		strings.HasPrefix(s, "External protocol:"),
		s == "JavaScript links are not supported",
		s == "Invalid JavaScript URL",
		s == "JavaScript history navigation is not supported":
		return statusWarning
	}
	return statusNormal
}

// drawStatusShortcut underlines the E in an "Error:" footer status, so the
// status advertises the E shortcut like the mode labels advertise J/D/R.
func drawStatusShortcut(g *Grid, x, y, avail int, text string, st *Style, fg Color) {
	if avail < 2 || st == nil || !strings.HasPrefix(text, " Error:") {
		return
	}
	under := *st
	under.Underline = true
	g.Text(x+1, y, "E", &under, fg)
}

func (b *Browser) footerLeft() string {
	if (b.hintConfirm || b.quitPending) && b.status != "" {
		return " " + b.status
	}
	if li := b.hoveredLink(); li != nil {
		return " → " + b.resolveHref(li.Href)
	}
	if b.status != "" {
		return " " + b.status
	}
	if li := b.focusedLink(); li != nil {
		return " → " + b.resolveHref(li.Href)
	}
	if b.deferredImages && b.imageTotal > 0 {
		return fmt.Sprintf(" Loading images %d/%d", b.imageDone, b.imageTotal)
	}
	return " Ready"
}

const (
	// footerGap keeps blank cells between the left status/link text and the
	// right-aligned footer state.
	footerGap = 3
)

func (b *Browser) readerStatusText() string {
	if b.reader {
		return "Reader:on"
	}
	return "Reader:off"
}

func (b *Browser) darkStatusText() string {
	if b.dark {
		return "Dark:on"
	}
	return "Dark:off"
}

func (b *Browser) jsStatusText() string {
	if b.jsErr {
		return "JavaScript:Error"
	}
	if b.noJS {
		return "JavaScript:off"
	}
	return "JavaScript:on"
}

// initGraphicsModes records the cycle order: native inline graphics when
// available, then blocks, then none. The requested mode becomes the starting
// point, so `-graphics=none` can still be cycled up to full graphics.
func (b *Browser) initGraphicsModes(requested GraphicsMode) {
	current := DetectGraphics(requested)
	native := current
	if native != GraphicsKitty && native != GraphicsITerm {
		detected := DetectGraphics(GraphicsAuto)
		if detected == GraphicsKitty || detected == GraphicsITerm {
			native = detected
		} else {
			native = GraphicsNone
		}
	}
	modes := make([]GraphicsMode, 0, 3)
	if native == GraphicsKitty || native == GraphicsITerm {
		modes = append(modes, native)
	}
	modes = append(modes, GraphicsBlocks, GraphicsNone)
	b.graphicsModes = modes
	b.graphicsMode = current
	if current == GraphicsAuto {
		b.graphicsMode = native
	}
}

// cycleGraphics switches full graphics -> blocks -> none -> full, skipping
// modes the terminal does not support.
func (b *Browser) cycleGraphics() {
	if len(b.graphicsModes) == 0 {
		return
	}
	next := b.graphicsMode
	for i, mode := range b.graphicsModes {
		if mode == b.graphicsMode {
			next = b.graphicsModes[(i+1)%len(b.graphicsModes)]
			break
		}
	}
	b.setGraphicsMode(next)
}

func (b *Browser) setGraphicsMode(mode GraphicsMode) {
	if mode == b.graphicsMode &&
		(mode == GraphicsBlocks || mode == GraphicsNone || b.gfx != nil) {
		return
	}
	b.graphicsMode = mode
	b.imagesPaused = false
	b.gfx = NewGFXRenderer(mode, b.cellW, b.cellH)
	hideImages := mode == GraphicsNone
	if b.loader != nil {
		b.loader.noImages = hideImages
	}
	// Rebuild the page in the new mode instead of reloading it. Decoded images
	// stay on the page; layout hides them when images are off, and a page
	// loaded with images disabled fetches them here when they are turned on.
	if b.page != nil && b.page.Root != nil {
		if !hideImages && b.loader != nil {
			b.loader.loadInlineSVGs(b.page.Root)
			if !b.loader.deferImages {
				b.refreshImages()
			}
		}
		b.rebuild()
	}
	b.status = b.graphicsStatusText()
}

// graphicsStatusText reports the resolved graphics mode: auto is shown as the
// protocol actually in use.
func (b *Browser) graphicsStatusText() string {
	mode := "none"
	switch {
	case b.loader != nil && b.loader.noImages:
		mode = "none"
	case b.gfx != nil:
		switch b.gfx.Mode() {
		case GraphicsKitty:
			mode = "kitty"
		case GraphicsITerm:
			mode = "iterm"
		default:
			mode = "blocks"
		}
	default:
		mode = "blocks"
	}
	return "Graphics:" + mode
}

// footerRight builds the right-aligned footer text: graphics and JS state,
// dark and reader state, resident memory and the scroll percentage. When the
// width is too small, elements disappear from left to right: graphics first,
// then the JS state, dark, reader and memory, leaving the scroll percentage
// last.
func (b *Browser) footerRight(width int) string {
	parts := []string{
		b.graphicsStatusText(),
		b.jsStatusText(),
		b.darkStatusText(),
		b.readerStatusText(),
		b.usage.footer(),
	}
	var base string
	for drop := 0; drop < len(parts); drop++ {
		candidate := strings.Join(parts[drop:], "   ")
		if stringWidth(candidate) <= width {
			base = candidate
			break
		}
	}
	return base
}

// centeredLabel fits label into room cells and returns the starting cell plus
// the fitted text, centered within [x, x+room).
func centeredLabel(x, room int, label string) (int, string) {
	if room < 1 {
		return x, ""
	}
	label = truncateWidth(label, room)
	pad := (room - stringWidth(label)) / 2
	return x + pad, label
}

// procUsage tracks the browser's memory footprint for the footer.
type procUsage struct {
	rss uint64
}

func (u *procUsage) sample() {
	u.rss = processRSS()
}

// footer returns the compact footer readout.
func (u procUsage) footer() string {
	return fmt.Sprintf("MEM %dMB", (u.rss+(1<<19))>>20)
}

// processRSS returns the current resident set size, falling back to Go's total
// OS memory when the platform has no /proc filesystem.
func processRSS() uint64 {
	if data, err := os.ReadFile("/proc/self/statm"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 2 {
			if pages, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				return pages * uint64(os.Getpagesize())
			}
		}
	}
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.Sys
}
