package engine

import (
	"fmt"
	"runtime"
	"strings"
)

func (b *Session) zoomBadge() string {
	return " " + zoomPercentLabel(b.Zoom) + " "
}

func (b *Session) pageFG() Color { return b.PageFG() }
func (b *Session) pageBG() Color { return b.PageBG() }

func (b *Session) paintInputSelection(g *Grid, top, bottom int) {
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
func (b *Session) paintInputCursor(g *Grid, top, bottom int) {
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

func (b *Session) draw() {
	if b.NoDraw {
		return
	}
	w := b.Width
	if w < 1 {
		w = 1
	}
	if b.consoleOpen {
		if b.sessionOut != nil {
			b.emitConsoleView()
		}
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
	contentH := b.contentRect().Height
	if contentH < 1 {
		contentH = 1
	}
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
	if w >= 2 {
		// Overflowing content (wide nowrap blocks, absolute boxes) may still
		// reach the terminal edge. Keep the reserved scrollbar column blank.
		cg.ClearGlyphs(w-1, 0, 1, contentH)
	}
	b.presentContent(cg, placements)
}

// presentContent hands the content rectangle to the front end, or writes it
// directly when no presentation session is active (tests and automation).
func (b *Session) presentContent(content *Grid, placements []ImagePlacement) {
	if b.emitFrames {
		b.frameBuf = toProtocolFrameInto(&b.frameBuf, content, 0)
		b.frameBuf.Placements = b.ProtocolPlacements(b.frameBuf.Placements[:0], placements, content.W, content.H)
		b.frameReady = true
		return
	}
	b.flushGrid(content, placements)
}

// footerModel computes the footer text, level and width once for the
// protocol.State snapshot the front end draws from.
type footerModel struct {
	left, right, pdfHint string
	pdfHintX             int
	avail                int
	level                statusLevel
	statusHit            bool
}

func (b *Session) footerModel() footerModel {
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

// flushGrid writes changed cells to the terminal.
func (b *Session) flushGrid(g *Grid, placements []ImagePlacement) {
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
	for i := range lines {
		if b.prev[i] == lines[i] {
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
		b.gfx.PlaceAll(&sb, b.ProtocolPlacements(nil, placements, g.W, g.H))
	}
	if sb.Len() > 0 {
		_, _ = b.writer().Write([]byte(sb.String()))
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

func (b *Session) scrollPercent() int { return b.ScrollPercent(b.viewportHeight()) }

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

func (b *Session) footerLeft() string {
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

func (b *Session) readerStatusText() string {
	if b.Reader {
		return "Reader:on"
	}
	return "Reader:off"
}

func (b *Session) darkStatusText() string {
	if b.Dark {
		return "Dark:on"
	}
	return "Dark:off"
}

func (b *Session) jsStatusText() string {
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
func (b *Session) cycleGraphics() {
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

func (b *Session) setGraphicsMode(mode GraphicsMode) {
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
		b.loader.NoImages = hideImages
	}
	// Rebuild the page in the new mode instead of reloading it. Decoded images
	// stay on the page; layout hides them when images are off, and a page
	// loaded with images disabled fetches them here when they are turned on.
	if b.Page != nil && b.Page.Root != nil {
		if !hideImages && b.loader != nil {
			b.loader.LoadInlineSVGs(b.Page.Root)
			if !b.loader.DeferImageLoad {
				b.refreshImages()
			}
		}
		b.rebuild()
	}
	b.status = b.graphicsStatusText()
}

// graphicsStatusText reports the resolved graphics mode: auto is shown as the
// protocol actually in use.
func (b *Session) graphicsStatusText() string {
	mode := "none"
	switch {
	case b.loader != nil && b.loader.NoImages:
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
func (b *Session) footerRight(width int) string {
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
