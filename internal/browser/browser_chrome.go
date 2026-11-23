package browser

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"unicode"

	"browser/internal/protocol"
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
	headerZoom
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
		thumbTop = top + (vh-thumbH)*b.Scroll/max
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
		b.scrollBy(-b.Scroll)
		return
	}
	if rel < 0 {
		rel = 0
	}
	if rel > span {
		rel = span
	}
	target := rel * max / span
	b.scrollBy(target - b.Scroll)
}

func (b *Browser) pageFG() Color { return b.Session.PageFG() }
func (b *Browser) pageBG() Color { return b.Session.PageBG() }

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

// zoomBadge returns the URL-row zoom label with one space on each side. The
// badge is always shown and clicking it opens the zoom preset window.
func (b *Browser) zoomBadge() string {
	return " " + zoomPercentLabel(b.Zoom) + " "
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
	n := b.Forms.InputFocus
	if !b.Forms.InputSelectAll || n == nil || b.Layout == nil {
		return
	}
	selBG := RGB(0x1f, 0x4f, 0x9f)
	selFG := RGB(0xf8, 0xfa, 0xfc)
	for i := range b.Layout.Frags {
		f := &b.Layout.Frags[i]
		if f.Kind != fragText || f.Node != n || f.W < 4 {
			continue
		}
		y := f.Y - b.Scroll + top
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
	if b.Layout == nil || !b.Layout.HasCursor || b.Forms.InputSelectAll {
		return
	}
	r := b.Layout.Cursor
	x, y := r.X, r.Y-b.Scroll+top
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
	if b.NoDraw {
		return
	}
	w, h := b.Width, b.Height
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if b.consoleOpen {
		if b.sessionOut != nil && !b.consoleEngineFallback() {
			b.emitConsoleView()
			return
		}
		b.drawConsole(w, h)
		return
	}
	// Kitty scaled text is composited by the terminal above the cell grid, so
	// a modal cannot hide a large heading that runs across it. Rebuild the page
	// without scaled text while a modal or popup is open, and restore it when
	// the overlay closes.
	if b.Layout != nil && b.scaledTextWanted() != b.ScaledLayout {
		b.rebuild()
	}
	// Deferred image batches keep their layout until the viewport can see them.
	b.applyPendingImageLayout()
	fg, bg := b.pageFG(), b.pageBG()
	cr := b.contentRect()
	top := cr.Top
	contentH := cr.Height
	if contentH < 1 {
		contentH = 1
	}
	// The engine paints only the content rectangle. Chrome is composed on the
	// screen grid afterwards, so page content has no way to reach the toolbar,
	// footer or dialogs.
	cg := reuseGrid(b.contentGrid, w, contentH, fg, bg)
	b.contentGrid = cg
	var placements []ImagePlacement
	var collect *[]ImagePlacement
	// While hints are shown, draw images as half-block art instead of native
	// inline graphics so the hint letters are painted above them. Inline image
	// protocols are composited by the terminal after the text grid.
	if b.inlineImagesEnabled() {
		collect = &placements
	}
	if b.Layout != nil {
		b.Layout.SetFixedScroll(b.Scroll)
	}
	PaintLayoutBig(cg, b.Layout, b.Scroll, 0, contentH, b.focusedLink(), fg, collect, b.BigText)
	b.paintControlFocus(cg, 0, contentH)
	b.paintInputSelection(cg, 0, contentH)
	b.paintInputCursor(cg, 0, contentH)
	b.paintSelection(cg, 0, contentH)
	b.paintFindPage(cg, 0, contentH)
	b.paintLinkHints(cg, 0, contentH)
	if !b.NoDraw && b.Width >= 2 {
		// Overflowing content (wide nowrap blocks, absolute boxes) may still
		// reach the terminal edge. Keep the reserved scrollbar column blank.
		cg.ClearGlyphs(w-1, 0, 1, contentH)
	}
	if b.frontendChromeActive() {
		// The front end paints and hit-tests the scrollbar for normal frames.
		b.scrollBar = nil
	} else {
		b.drawScrollBar(cg, 0, contentH)
		if b.scrollBar != nil {
			// Mouse hit testing works in terminal coordinates; the track was
			// computed in content coordinates.
			b.scrollBar.trackTop += top
			b.scrollBar.thumbTop += top
		}
	}

	g := reuseGrid(b.screenGrid, w, h, fg, bg)
	b.screenGrid = g
	blitGrid(g, cg, top)
	for _, r := range cg.Runs {
		r.Y += top
		g.Runs = append(g.Runs, r)
	}
	b.drawChrome(g)
	if b.frontendChromePath() {
		b.presentComposed(cg, g, top, contentH, placements)
		return
	}
	b.presentGrid(g, placements)
}

// presentGrid hands one full screen to the front end, or writes it directly
// when no presentation session is active (tests and automation). Every screen
// path, including the console, must use this so the front-end row diff never
// goes stale.
func (b *Browser) presentGrid(g *Grid, placements []ImagePlacement) {
	top := b.contentRect().Top
	for i := range placements {
		placements[i].Y += top
	}
	if b.emitFrames {
		// The frame buffer is reused; the transport copies it before the next
		// draw.
		b.chromeBuf = protocol.Chrome{Show: false}
		b.chromeReady = true
		b.frameBuf = toProtocolFrameInto(&b.frameBuf, g, 0)
		b.frameBuf.Placements = b.protocolPlacements(b.frameBuf.Placements[:0], placements, g.W, g.H)
		b.frameReady = true
		return
	}
	b.flushGrid(g, placements)
}

// presentComposed sends a content-only frame plus the chrome rows the shell
// composed above and below it. The front end stacks them, so a normal page
// frame no longer carries chrome cells.
func (b *Browser) presentComposed(content, screen *Grid, top, contentH int, placements []ImagePlacement) {
	if top < 0 || contentH < 1 || screen.H < top+contentH+1 {
		b.presentGrid(screen, placements)
		return
	}
	header := b.chromeBuf.Header[:0]
	for _, c := range screen.Cells[:top*screen.W] {
		header = append(header, protoCell(c))
	}
	footer := b.chromeBuf.Footer[:0]
	for _, c := range screen.Cells[(screen.H-1)*screen.W:] {
		footer = append(footer, protoCell(c))
	}
	b.chromeBuf = protocol.Chrome{
		Show: true, Width: screen.W, HeaderRows: top,
		Header: header, Footer: footer,
	}
	b.chromeReady = true
	b.frameBuf = toProtocolFrameInto(&b.frameBuf, content, 0)
	b.frameBuf.Placements = b.protocolPlacements(b.frameBuf.Placements[:0], placements, content.W, content.H)
	b.frameReady = true
}

// drawChrome composes the trusted shell: toolbar, footer and app overlays. It
// runs after the content frame is blitted, so page content cannot reach these
// rows and dialogs always paint above it.
// footerModel computes the footer text, level and hit width once for both the
// engine fallback and the protocol.State snapshot the front end draws from.
type footerModel struct {
	left, right, pdfHint string
	pdfHintX             int
	avail                int
	level                statusLevel
	statusHit            bool
}

func (b *Browser) footerModel() footerModel {
	w := b.Width
	consoleW := 2
	reserve := 0
	if w >= 6 {
		reserve = consoleW + 2
	}
	rightBudget := w/2 - reserve - footerGap
	if rightBudget < 1 {
		rightBudget = 1
	}
	rightText := b.footerRight(rightBudget)
	avail := w - stringWidth(rightText) - reserve - footerGap
	if avail < 0 {
		avail = 0
	}
	leftText := b.footerLeft()
	pdfHint, pdfHintX := "", -1
	if leftText == " Ready" && b.Page != nil && b.Page.PDF != nil {
		if text, hint, cell, ok := b.pdfFooterLine(avail); ok {
			leftText, pdfHint, pdfHintX = text, hint, cell
		}
	}
	level := statusNormal
	statusHit := false
	if b.HintConfirm {
		level = statusWarning
	} else if b.status != "" && (b.quitPending || b.hoveredLink() == nil) {
		level = statusLevelFor(b.status)
	}
	if b.status != "" && !b.HintConfirm && !b.quitPending && b.hoveredLink() == nil {
		if level == statusWarning || level == statusError {
			statusHit = true
		}
	}
	return footerModel{
		left: leftText, right: rightText, pdfHint: pdfHint, pdfHintX: pdfHintX,
		avail: avail, level: level, statusHit: statusHit,
	}
}

func (b *Browser) drawChrome(g *Grid) {
	w, h := g.W, g.H
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
		if b.Loading {
			reloadLabel, reloadKind = " × ", headerStop
		}
		homeLabel := " ⌂ "
		historyLabel := " ≡ History "
		configLabel := " ⚙ "
		if w < 80 {
			backLabel = " ← "
			helpLabel, historyLabel = " ? ", " ≡ "
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
			if b.Progress > 0 {
				filled := int(b.Progress*float64(width) + 0.5)
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
				if b.URL != nil {
					u = b.URL.String()
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
			helpW, historyW, configW := 0, 0, 0
			if w >= 6 {
				helpW = stringWidth(helpLabel)
				configW = stringWidth(configLabel)
			}
			if w >= 12 {
				historyW = stringWidth(historyLabel)
			}
			for x+helpW+historyW+configW > w {
				switch {
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
			title := "MOB³"
			if b.Page != nil && b.Page.Title != "" {
				title = b.Page.Title
			}
			if room := histX - x; room > 0 {
				titleFG := RGB(0xff, 0xd7, 0x66)
				titleStyle := &Style{Bold: true, Color: titleFG, FontSize: 16, LineHeight: 1}
				tx, label := centeredLabel(x, room, title)
				g.Text(tx, 0, label, titleStyle, titleFG)
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
			// URL row. The zoom badge keeps the far-right cells, so the URL
			// and the prompt both truncate before it instead of running under.
			zoomLabel := b.zoomBadge()
			fieldW := w
			if zoomLabel != "" {
				if zw := stringWidth(zoomLabel); zw < w {
					fieldW = w - zw
				} else {
					zoomLabel = ""
				}
			}
			drawField(0, 1, fieldW)
			if zoomLabel != "" {
				b.drawHeaderButton(g, fieldW, 1, zoomLabel, headerZoom, btnBG, headerFG, hs)
			}
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
			helpW, historyW, configW := 0, 0, 0
			if w >= 6 {
				helpW = stringWidth(helpLabel)
				configW = stringWidth(configLabel)
			}
			if w >= 12 {
				historyW = stringWidth(historyLabel)
			}
			for x+helpW+historyW+configW > w {
				switch {
				case historyW > 0:
					historyW = 0
				case helpW > 0:
					helpW = 0
				default:
					configW = 0
				}
			}
			urlW := w - x - helpW - historyW - configW
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
			zoomLabel := b.zoomBadge()
			if zoomLabel != "" {
				if zw := stringWidth(zoomLabel); zw < urlW {
					urlW -= zw
				} else {
					zoomLabel = ""
				}
			}
			drawField(x, 0, urlW)
			if zoomLabel != "" {
				b.drawHeaderButton(g, x+urlW, 0, zoomLabel, headerZoom, btnBG, headerFG, hs)
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
		if b.FindActive {
			b.drawFindBar(g, fy, w)
		} else {
			fm := b.footerModel()
			fs := &Style{Color: footerFG, FontSize: 16, LineHeight: 1}
			leftStyle := fs
			leftColor := footerFG
			switch fm.level {
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
			g.Text(0, fy, padWidth(truncateWidth(fm.left, fm.avail), fm.avail), leftStyle, leftColor)
			if fm.statusHit {
				sw := stringWidth(fm.left)
				if sw > fm.avail {
					sw = fm.avail
				}
				if sw > 0 {
					b.footerHits = append(b.footerHits, headerHit{kind: headerStatus, x: 0, y: fy, w: sw, h: 1})
				}
				if fm.level == statusError {
					drawStatusShortcut(g, 0, fy, fm.avail, fm.left, leftStyle, leftColor)
				}
			}
			if fm.pdfHint != "" && fm.pdfHintX >= 0 {
				hintStyle := &Style{Bold: true, Color: RGB(0x1f, 0x4f, 0x9f), FontSize: 16, LineHeight: 1}
				g.Text(fm.pdfHintX, fy, fm.pdfHint, hintStyle, hintStyle.Color)
				b.footerHits = append(b.footerHits, headerHit{kind: headerDownload, x: fm.pdfHintX, y: fy, w: stringWidth(fm.pdfHint), h: 1})
			}
			if fm.right != "" {
				consoleW := stringWidth(">_")
				reserve := 0
				if w >= 6 {
					reserve = consoleW + 2
				}
				rightX := w - reserve - stringWidth(fm.right)
				g.Text(rightX, fy, fm.right, fs, footerFG)
				labels := []struct {
					text string
					kind headerKind
					on   bool
					err  bool
				}{
					{b.graphicsStatusText(), headerGraphics, false, false},
					{b.jsStatusText(), headerJS, !b.NoJS, b.jsErr},
					{b.darkStatusText(), headerDark, b.Dark, false},
					{b.readerStatusText(), headerReader, b.Reader, false},
				}
				for _, label := range labels {
					i := strings.Index(fm.right, label.text)
					if i < 0 {
						continue
					}
					lx := rightX + stringWidth(fm.right[:i])
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
			if w >= 6 {
				consoleIcon := ">_"
				consoleX := w - stringWidth(consoleIcon)
				iconStyle := &Style{Bold: true, Color: RGB(0x1f, 0x4f, 0x9f), FontSize: 16, LineHeight: 1}
				g.Text(consoleX, fy, consoleIcon, iconStyle, iconStyle.Color)
				b.footerHits = append(b.footerHits, headerHit{kind: headerConsole, x: consoleX, y: fy, w: stringWidth(consoleIcon), h: 1})
			}
		}
	}
	b.paintShellOverlays(g)
}

// paintShellOverlays draws the trusted shell's modal layer above both page
// content and the console.
func (b *Browser) paintShellOverlays(g *Grid) {
	b.modalClose = Rect{}
	b.modalBox = Rect{}
	switch {
	case b.dialog != nil:
		b.paintDialog(g)
	case b.download != nil && !b.downloadFrontend:
		b.paintDownload(g)
	case b.notice != nil && !b.noticeFrontend:
		b.paintNotice(g)
	case b.selectMenu != nil && !b.selectFrontend:
		b.paintSelectMenu(g)
	case b.viewMenu != nil:
		b.paintViewMenu(g)
	case b.zoomMenu != nil:
		b.paintZoomMenu(g)
	}
}

// flushGrid writes changed cells to the terminal.
func (b *Browser) flushGrid(g *Grid, placements []ImagePlacement) {
	enc := b.frameEncoder
	if enc == nil {
		enc = defaultFrameEncoder
	}
	if enc == nil {
		return
	}
	lines := enc(toProtocolFrameInto(&b.frameBuf, g, 0), b.color, !b.color)
	// Scrolling can leave the terminal in a shifted state (for example after a
	// stray wrap), and cells covered by scaled text are skipped when painting.
	// Clear the whole screen and repaint on any scroll change so stale content
	// can never show through the skipped cells.
	if b.Scroll != b.lastScroll {
		b.lastScroll = b.Scroll
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
		if b.Height >= 2 && i == b.Height-1 {
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
		b.gfx.PlaceAll(&sb, b.protocolPlacements(nil, placements, g.W, g.H))
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

func (b *Browser) scrollPercent() int { return b.Session.ScrollPercent(b.viewportHeight()) }

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
	if (b.HintConfirm || b.quitPending) && b.status != "" {
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
	if b.DeferredImages && b.ImageTotal > 0 {
		return fmt.Sprintf(" Loading images %d/%d", b.ImageDone, b.ImageTotal)
	}
	return " Ready"
}

const (
	// footerGap keeps blank cells between the left status/link text and the
	// right-aligned footer state.
	footerGap = 3
)

func (b *Browser) readerStatusText() string {
	if b.Reader {
		return "Reader:on"
	}
	return "Reader:off"
}

func (b *Browser) darkStatusText() string {
	if b.Dark {
		return "Dark:on"
	}
	return "Dark:off"
}

func (b *Browser) jsStatusText() string {
	if b.jsErr {
		return "JavaScript:Error"
	}
	if b.NoJS {
		return "JavaScript:off"
	}
	return "JavaScript:on"
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
	b.ImagesPaused = false
	b.gfx = nil
	if b.newGraphics != nil {
		b.gfx = b.newGraphics(mode, b.CellW, b.CellH)
	}
	hideImages := mode == GraphicsNone
	if b.loader != nil {
		b.loader.noImages = hideImages
	}
	// Rebuild the page in the new mode instead of reloading it. Decoded images
	// stay on the page; layout hides them when images are off, and a page
	// loaded with images disabled fetches them here when they are turned on.
	if b.Page != nil && b.Page.Root != nil {
		if !hideImages && b.loader != nil {
			b.loader.loadInlineSVGs(b.Page.Root)
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

// processRSS returns Go's total OS memory. MOB³ reads no proc files, so this
// is the closest process-wide figure available without a filesystem read.
func processRSS() uint64 {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.Sys
}
