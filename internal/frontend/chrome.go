package frontend

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"browser/internal/protocol"
)

// ChromeHitKind identifies a clickable header or footer region. Coordinates
// are zero-based terminal cells; the session loop translates the mouse report.
type ChromeHitKind uint8

const (
	ChromeHitNone ChromeHitKind = iota
	ChromeHitBack
	ChromeHitForward
	ChromeHitReload
	ChromeHitStop
	ChromeHitHome
	ChromeHitHistory
	ChromeHitURL
	ChromeHitHelp
	ChromeHitConfig
	ChromeHitZoom
	ChromeHitConsole
	ChromeHitJS
	ChromeHitReader
	ChromeHitDark
	ChromeHitGraphics
	ChromeHitDownload
	ChromeHitStatus
	ChromeHitTab
	ChromeHitTabClose
	ChromeHitNewTab
	ChromeHitTabPrev
	ChromeHitTabNext
)

// ChromeHit is one clickable chrome region. Index identifies the tab for
// ChromeHitTab and ChromeHitTabClose; it is zero for every other kind.
type ChromeHit struct {
	Kind       ChromeHitKind
	Index      int
	X, Y, W, H int
}

// TabView is the tab-bar label for one tab. It is a plain value so the shared
// presenter and the per-tab mouse handler can draw the same bar.
type TabView struct {
	Title  string
	Active bool
}

// ChromeLayout is the header and footer the front end draws for a normal
// content frame. Overlays and the console keep the engine's full-frame
// fallback and do not use this layout.
type ChromeLayout struct {
	Width, Height, HeaderRows int
	Header, Footer            []protocol.Cell
	Hits                      []ChromeHit
}

const footerGap = 3

func rgb(r, g, b uint8) protocol.Color {
	return protocol.Color{R: r, G: g, B: b, A: 255, Set: true}
}

var (
	colHeaderBG   = rgb(0x1f, 0x29, 0x37)
	colFooterBG   = rgb(0xea, 0xee, 0xf2)
	colHeaderFG   = rgb(0xf6, 0xf8, 0xfa)
	colFooterFG   = rgb(0x24, 0x29, 0x2f)
	colButtonBG   = rgb(0x2b, 0x3a, 0x4c)
	colTabFrom    = rgb(0x3a, 0x45, 0x52)
	colTabTo      = rgb(0x24, 0x2c, 0x36)
	colTabDimFG   = rgb(0x8b, 0x98, 0xa8)
	colURLBG      = rgb(0x13, 0x1d, 0x29)
	colPromptBG   = rgb(0x27, 0x44, 0x63)
	colSelectedBG = rgb(0x2f, 0x6f, 0xbf)
	colGradientBG = rgb(0x31, 0x43, 0x5c)
	colLinkFG     = rgb(0x1f, 0x4f, 0x9f)
	colTitleFG    = rgb(0xff, 0xd7, 0x66)
	colWarnFG     = rgb(0x9a, 0x5b, 0x00)
	colErrorFG    = rgb(0xb3, 0x1d, 0x1d)
	colSuccessFG  = rgb(0x17, 0x6b, 0x35)
	colWhite      = rgb(0xff, 0xff, 0xff)
)

// LayoutChrome builds the normal-page header and footer from the state
// snapshot. The engine still composes the same cells for overlays and tests;
// this function is the front-end-owned path.
func LayoutChrome(st protocol.State) ChromeLayout {
	return LayoutChromeWithTabs(st, nil, 0)
}

// LayoutChromeWithTabs is LayoutChrome with tab labels sharing the toolbar
// row. Any non-empty tab list occupies the title slot, including a single tab;
// offset selects the first visible tab. The centered page title is used only
// while no tab views are available.
func LayoutChromeWithTabs(st protocol.State, tabs []TabView, offset int) ChromeLayout {
	w, h, rows := st.Width, st.Height, st.HeaderRows
	if w <= 0 || h <= 0 || rows <= 0 || rows >= h {
		return ChromeLayout{}
	}
	// A hostile state must not size an unbounded paint buffer.
	if w > protocol.DefaultLimits.MaxFrameWidth {
		w = protocol.DefaultLimits.MaxFrameWidth
	}
	if h > protocol.DefaultLimits.MaxFrameHeight {
		h = protocol.DefaultLimits.MaxFrameHeight
	}
	if rows >= h {
		rows = h - 1
	}

	url := protocol.StripControls(st.URL)
	title := protocol.StripControls(st.Title)
	if title == "" {
		title = "MOB³"
	}
	zoom := protocol.StripControls(st.ZoomLabel)
	prompt := protocol.StripControls(st.PromptText)
	footerLeft := protocol.StripControls(st.FooterLeft)
	footerRight := protocol.StripControls(st.FooterRight)
	pdfHint := protocol.StripControls(st.PDFHint)

	out := ChromeLayout{Width: w, Height: h, HeaderRows: rows}
	out.Header = newCanvas(w, rows).cells
	g := &chromeCanvas{w: w, h: rows, cells: out.Header}

	// Header background and top-row gradient.
	g.fill(0, 0, w, rows, colHeaderBG)
	if rows >= 2 {
		g.hgradient(0, 0, w, 1, colHeaderBG, colGradientBG)
	}

	backLabel, helpLabel := " ← Back ", " ? Help "
	forwardLabel := " → "
	reloadLabel := " ⟳ "
	if st.Loading {
		reloadLabel = " × "
	}
	homeLabel := " ⌂ "
	historyLabel := " ≡ History "
	configLabel := " ⚙ "
	if w < 80 {
		backLabel = " ← "
		helpLabel, historyLabel = " ? ", " ≡ "
	}
	fieldBG := colURLBG
	if st.PromptActive {
		fieldBG = colPromptBG
		if st.PromptSelectAll {
			fieldBG = colSelectedBG
		}
	}

	drawField := func(x, y, width int) {
		if width < 1 {
			return
		}
		g.fill(x, y, width, 1, fieldBG)
		if st.Progress > 0 {
			filled := int(float64(st.Progress)/100*float64(width) + 0.5)
			if filled < 1 {
				filled = 1
			}
			if filled > width {
				filled = width
			}
			g.fill(x, y, filled, 1, rgb(0x1f, 0x4f, 0x8a))
		}
		if st.PromptActive {
			head, at, tail, show := promptParts(" Open: ", prompt, st.PromptCursor, st.PromptSelectAll, width)
			g.text(x, y, head, colHeaderFG, protocol.AttrBold)
			cx := x + stringWidth(head)
			if show {
				g.put(cx, y, at, colWhite, protocol.AttrBold)
				g.cells[y*w+cx].BG = colSelectedBG
				cx++
			}
			g.text(cx, y, tail, colHeaderFG, protocol.AttrBold)
		} else {
			g.text(x, y, fitWidth(" "+url+" ", width), colHeaderFG, 0)
		}
		out.Hits = append(out.Hits, ChromeHit{Kind: ChromeHitURL, X: x, Y: y, W: width, H: 1})
	}

	drawButton := func(x, y int, label string, kind ChromeHitKind, bg, fg protocol.Color) int {
		bw := stringWidth(label)
		if bw < 1 || x+bw > w || y < 0 || y >= rows {
			return x
		}
		g.fill(x, y, bw, 1, bg)
		if kind == ChromeHitHistory {
			g.shortcut(x, y, label, fg, protocol.AttrBold)
		} else {
			g.text(x, y, label, fg, protocol.AttrBold)
		}
		out.Hits = append(out.Hits, ChromeHit{Kind: kind, X: x, Y: y, W: bw, H: 1})
		return x + bw
	}

	if rows >= 2 {
		x := 0
		if w >= 24 {
			x = drawButton(x, 0, backLabel, ChromeHitBack, colButtonBG, colHeaderFG)
			x = drawButton(x, 0, forwardLabel, ChromeHitForward, colButtonBG, colHeaderFG)
			if st.Loading {
				x = drawButton(x, 0, reloadLabel, ChromeHitStop, colButtonBG, colHeaderFG)
			} else {
				x = drawButton(x, 0, reloadLabel, ChromeHitReload, colButtonBG, colHeaderFG)
			}
			x = drawButton(x, 0, homeLabel, ChromeHitHome, colButtonBG, colHeaderFG)
		} else if w >= 12 {
			x = drawButton(x, 0, homeLabel, ChromeHitHome, colButtonBG, colHeaderFG)
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
		if room := histX - x; room > 0 {
			drawTabStrip(g, &out, x, 0, room, tabs, offset, title)
		}
		if historyW > 0 {
			drawButton(histX, 0, historyLabel, ChromeHitHistory, colButtonBG, colHeaderFG)
		}
		if helpW > 0 {
			drawButton(helpX, 0, helpLabel, ChromeHitHelp, colButtonBG, colHeaderFG)
		}
		if configW > 0 {
			drawButton(configX, 0, configLabel, ChromeHitConfig, colButtonBG, colHeaderFG)
		}
		fieldW := w
		if zoom != "" {
			if zw := stringWidth(zoom); zw < w {
				fieldW = w - zw
			} else {
				zoom = ""
			}
		}
		drawField(0, 1, fieldW)
		if zoom != "" {
			drawButton(fieldW, 1, zoom, ChromeHitZoom, colButtonBG, colHeaderFG)
		}
	} else {
		x := 0
		if w >= 24 {
			x = drawButton(x, 0, backLabel, ChromeHitBack, colButtonBG, colHeaderFG)
			x = drawButton(x, 0, forwardLabel, ChromeHitForward, colButtonBG, colHeaderFG)
			if st.Loading {
				x = drawButton(x, 0, reloadLabel, ChromeHitStop, colButtonBG, colHeaderFG)
			} else {
				x = drawButton(x, 0, reloadLabel, ChromeHitReload, colButtonBG, colHeaderFG)
			}
			x = drawButton(x, 0, homeLabel, ChromeHitHome, colButtonBG, colHeaderFG)
		} else if w >= 12 {
			x = drawButton(x, 0, homeLabel, ChromeHitHome, colButtonBG, colHeaderFG)
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
		if zoom != "" {
			if zw := stringWidth(zoom); zw < urlW {
				urlW -= zw
			} else {
				zoom = ""
			}
		}
		drawField(x, 0, urlW)
		if zoom != "" {
			drawButton(x+urlW, 0, zoom, ChromeHitZoom, colButtonBG, colHeaderFG)
		}
		if historyW > 0 {
			drawButton(w-configW-helpW-historyW, 0, historyLabel, ChromeHitHistory, colButtonBG, colHeaderFG)
		}
		if helpW > 0 {
			drawButton(w-configW-helpW, 0, helpLabel, ChromeHitHelp, colButtonBG, colHeaderFG)
		}
		if configW > 0 {
			drawButton(w-configW, 0, configLabel, ChromeHitConfig, colButtonBG, colHeaderFG)
		}
	}

	// Footer.
	f := newCanvas(w, 1)
	f.fill(0, 0, w, 1, colFooterBG)
	if st.FindActive {
		drawFindBar(f, st)
		out.Footer = f.cells
		return out
	}
	consoleIcon := ">_"
	consoleW := stringWidth(consoleIcon)
	showConsole := w >= 6
	reserve := 0
	consoleX := w - consoleW
	if showConsole {
		reserve = consoleW + 2
	}
	rightText := footerRight
	avail := w - stringWidth(rightText) - reserve - footerGap
	if avail < 0 {
		avail = 0
	}
	leftText := footerLeft
	leftFG, leftAttrs := footerStyle(st.FooterLevel)
	f.text(0, 0, padWidth(truncateWidth(leftText, avail), avail), leftFG, leftAttrs)
	if st.FooterStatusHit {
		sw := stringWidth(leftText)
		if sw > avail {
			sw = avail
		}
		if sw > 0 {
			out.Hits = append(out.Hits, ChromeHit{Kind: ChromeHitStatus, X: 0, Y: h - 1, W: sw, H: 1})
		}
		if st.FooterLevel == protocol.FooterError && strings.HasPrefix(leftText, " Error:") && avail >= 2 {
			under := leftAttrs | protocol.AttrUnderline
			f.put(1, 0, 'E', leftFG, under)
		}
	}
	if pdfHint != "" && st.PDFHintX >= 0 && st.PDFHintX < w {
		f.text(st.PDFHintX, 0, pdfHint, colLinkFG, protocol.AttrBold)
		out.Hits = append(out.Hits, ChromeHit{Kind: ChromeHitDownload, X: st.PDFHintX, Y: h - 1, W: stringWidth(pdfHint), H: 1})
	}
	if rightText != "" {
		rightX := w - reserve - stringWidth(rightText)
		f.text(rightX, 0, rightText, colFooterFG, 0)
		parts := strings.Split(rightText, "   ")
		offset := 0
		for _, part := range parts {
			idx := strings.Index(rightText[offset:], part)
			if idx < 0 {
				continue
			}
			start := offset + idx
			offset = start + len(part)
			x := rightX + stringWidth(rightText[:start])
			switch {
			case strings.HasPrefix(part, "Graphics:"):
				f.underlinedRune(x, 0, part, 'i', colFooterFG, 0)
				out.Hits = append(out.Hits, ChromeHit{Kind: ChromeHitGraphics, X: x, Y: h - 1, W: stringWidth(part), H: 1})
			case strings.HasPrefix(part, "JavaScript:"):
				fg, attrs := colFooterFG, uint8(0)
				if part == "JavaScript:Error" {
					fg, attrs = colErrorFG, protocol.AttrBold
				} else if part == "JavaScript:on" {
					fg, attrs = colLinkFG, protocol.AttrBold
				}
				f.shortcut(x, 0, part, fg, attrs)
				out.Hits = append(out.Hits, ChromeHit{Kind: ChromeHitJS, X: x, Y: h - 1, W: stringWidth(part), H: 1})
			case strings.HasPrefix(part, "Dark:"):
				fg, attrs := colFooterFG, uint8(0)
				if part == "Dark:on" {
					fg, attrs = colLinkFG, protocol.AttrBold
				}
				f.shortcut(x, 0, part, fg, attrs)
				out.Hits = append(out.Hits, ChromeHit{Kind: ChromeHitDark, X: x, Y: h - 1, W: stringWidth(part), H: 1})
			case strings.HasPrefix(part, "Reader:"):
				fg, attrs := colFooterFG, uint8(0)
				if part == "Reader:on" {
					fg, attrs = colLinkFG, protocol.AttrBold
				}
				f.shortcut(x, 0, part, fg, attrs)
				out.Hits = append(out.Hits, ChromeHit{Kind: ChromeHitReader, X: x, Y: h - 1, W: stringWidth(part), H: 1})
			}
		}
	}
	if showConsole {
		f.text(consoleX, 0, consoleIcon, colLinkFG, protocol.AttrBold)
		out.Hits = append(out.Hits, ChromeHit{Kind: ChromeHitConsole, X: consoleX, Y: h - 1, W: consoleW, H: 1})
	}
	out.Footer = f.cells
	return out
}

// drawTabStrip paints tab labels in the toolbar title slot.
func drawTabStrip(g *chromeCanvas, out *ChromeLayout, x, y, room int, tabs []TabView, offset int, title string) {
	if room < 1 {
		return
	}
	const tabW = 20
	newW := 0
	if room >= 4 {
		newW = 3
	}
	titleRoom := room - newW

	if len(tabs) == 0 {
		if titleRoom > 0 {
			tx, label := centeredLabel(x, titleRoom, title)
			g.text(tx, y, label, colTitleFG, protocol.AttrBold)
		}
		if newW > 0 {
			drawNewTabButton(g, out, x+room-newW, y, newW)
		}
		return
	}

	avail := titleRoom
	if avail < 1 {
		if newW > 0 {
			drawNewTabButton(g, out, x+room-newW, y, newW)
		}
		return
	}

	if len(tabs)*tabW <= avail {
		cx := x
		for i, tab := range tabs {
			drawTab(g, out, cx, y, tabW, i, tab)
			cx += tabW
		}
		if newW > 0 {
			drawNewTabButton(g, out, x+room-newW, y, newW)
		}
		return
	}

	arrowsW := 2
	viewport := avail - arrowsW
	if viewport < 1 {
		if newW > 0 {
			drawNewTabButton(g, out, x+room-newW, y, newW)
		}
		return
	}
	visible := viewport / tabW
	if visible < 1 {
		visible = 1
	}
	if visible > len(tabs) {
		visible = len(tabs)
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(tabs)-visible {
		offset = len(tabs) - visible
	}
	cx := x
	drawW := tabW
	if drawW > viewport {
		drawW = viewport
	}
	for i := offset; i < offset+visible; i++ {
		drawTab(g, out, cx, y, drawW, i, tabs[i])
		cx += drawW
	}
	prevX := x + room - newW - arrowsW
	g.fill(prevX, y, arrowsW, 1, colButtonBG)
	g.text(prevX, y, "<", colHeaderFG, protocol.AttrBold)
	g.text(prevX+1, y, ">", colHeaderFG, protocol.AttrBold)
	out.Hits = append(out.Hits,
		ChromeHit{Kind: ChromeHitTabPrev, X: prevX, Y: y, W: 1, H: 1},
		ChromeHit{Kind: ChromeHitTabNext, X: prevX + 1, Y: y, W: 1, H: 1},
	)
	if newW > 0 {
		drawNewTabButton(g, out, x+room-newW, y, newW)
	}
}

// drawTab paints one numbered tab with a close region on the right.
func drawTab(g *chromeCanvas, out *ChromeLayout, x, y, w, index int, tab TabView) {
	if w < 1 {
		return
	}
	accentFG, titleFG := colHeaderFG, colTabDimFG
	if tab.Active {
		accentFG, titleFG = colWhite, colWhite
		g.fill(x, y, w, 1, colSelectedBG)
	} else {
		g.hgradient(x, y, w, 1, colTabFrom, colTabTo)
	}
	title := protocol.StripControls(tab.Title)
	if title == "" {
		title = "New Tab"
	}

	lead := fmt.Sprintf(" %d:", index+1)
	tail := " x "
	tailW := stringWidth(tail)
	if tailW > w-1 {
		tail, tailW = "", 0
	}
	leadW := stringWidth(lead)
	if leadW+tailW > w {
		lead = truncateWidth(lead, w-tailW)
		leadW = stringWidth(lead)
	}
	titleW := w - leadW - tailW
	if titleW < 0 {
		titleW = 0
	}
	g.text(x, y, lead, accentFG, protocol.AttrBold)
	if titleW > 0 {
		titleAttrs := uint8(0)
		if tab.Active {
			titleAttrs = protocol.AttrBold
		}
		g.text(x+leadW, y, fitWidth(title, titleW), titleFG, titleAttrs)
	}
	if tailW > 0 {
		closeX := x + w - tailW
		g.text(closeX, y, tail, accentFG, protocol.AttrBold)
		hitW := tailW
		if hitW > 2 {
			hitW = 2
		}
		out.Hits = append(out.Hits, ChromeHit{
			Kind: ChromeHitTabClose, Index: index, X: closeX, Y: y, W: hitW, H: 1,
		})
	}
	tabW := w - tailW
	out.Hits = append(out.Hits, ChromeHit{
		Kind: ChromeHitTab, Index: index, X: x, Y: y, W: tabW, H: 1,
	})
}

func drawNewTabButton(g *chromeCanvas, out *ChromeLayout, x, y, w int) {
	g.fill(x, y, w, 1, colButtonBG)
	g.text(x, y, " + ", colHeaderFG, protocol.AttrBold)
	out.Hits = append(out.Hits, ChromeHit{
		Kind: ChromeHitNewTab, X: x, Y: y, W: w, H: 1,
	})
}

func drawFindBar(f *chromeCanvas, st protocol.State) { drawFindBarAt(f, 0, st) }

func drawFindBarAt(f *chromeCanvas, y int, st protocol.State) {
	bg := rgb(0x27, 0x44, 0x63)
	fg := rgb(0xff, 0xff, 0xff)
	f.fill(0, y, f.w, 1, bg)
	prefix := " Find: "
	f.text(0, y, prefix, fg, protocol.AttrBold)
	x := stringWidth(prefix)
	count := " 0/0 "
	if st.FindText != "" && st.FindTotal > 0 {
		cur := st.FindCurrent
		if cur < 1 {
			cur = 1
		}
		count = fmt.Sprintf(" %d/%d ", cur, st.FindTotal)
	}
	countW := stringWidth(count)
	contentW := f.w - x - countW
	if contentW < 1 {
		count = ""
		countW = 0
		contentW = f.w - x
	}
	if contentW < 1 {
		return
	}
	runes, start, end, cur := promptWindow(st.FindText, st.FindCursor, contentW, false)
	xx := x
	for i := start; i < end; i++ {
		if i == cur {
			if xx < f.w {
				f.put(xx, y, ' ', rgb(0x0f, 0x14, 0x1a), protocol.AttrBold)
				f.cells[y*f.w+xx].BG = rgb(0xf8, 0xfa, 0xfc)
			}
			xx++
			continue
		}
		f.put(xx, y, runes[i], fg, protocol.AttrBold)
		xx += runeWidth(runes[i])
	}
	if cur == len(runes) && cur >= start && xx < f.w {
		f.put(xx, y, ' ', rgb(0x0f, 0x14, 0x1a), protocol.AttrBold)
		f.cells[y*f.w+xx].BG = rgb(0xf8, 0xfa, 0xfc)
	}
	if countW > 0 {
		f.text(f.w-countW, y, count, fg, protocol.AttrBold)
	}
}

func footerStyle(level protocol.FooterLevel) (protocol.Color, uint8) {
	switch level {
	case protocol.FooterWarning:
		return colWarnFG, protocol.AttrBold
	case protocol.FooterError:
		return colErrorFG, protocol.AttrBold
	case protocol.FooterSuccess:
		return colSuccessFG, 0
	default:
		return colFooterFG, 0
	}
}

// chromeCanvas is a small cell buffer for the header and footer. It mirrors the
// engine Grid operations the chrome needs without importing engine types.
type chromeCanvas struct {
	w, h  int
	cells []protocol.Cell
}

func newCanvas(w, h int) *chromeCanvas {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	c := &chromeCanvas{w: w, h: h, cells: make([]protocol.Cell, w*h)}
	for i := range c.cells {
		c.cells[i].R = ' '
	}
	return c
}

func (c *chromeCanvas) fill(x, y, w, h int, bg protocol.Color) {
	if !bg.Set || bg.A == 0 {
		return
	}
	x0, y0 := maxInt(x, 0), maxInt(y, 0)
	x1, y1 := minInt(x+w, c.w), minInt(y+h, c.h)
	for yy := y0; yy < y1; yy++ {
		row := yy * c.w
		for xx := x0; xx < x1; xx++ {
			c.cells[row+xx].BG = bg
		}
	}
}

func (c *chromeCanvas) hgradient(x, y, w, h int, from, to protocol.Color) {
	if !from.Set || !to.Set || w <= 0 || h <= 0 {
		return
	}
	x0, x1 := maxInt(x, 0), minInt(x+w, c.w)
	y0, y1 := maxInt(y, 0), minInt(y+h, c.h)
	mix := func(a, b uint8, t int) uint8 {
		return uint8((int(a)*(255-t) + int(b)*t) / 255)
	}
	for xx := x0; xx < x1; xx++ {
		t := 0
		if w > 1 {
			t = (xx - x) * 255 / (w - 1)
		}
		col := protocol.Color{
			R: mix(from.R, to.R, t), G: mix(from.G, to.G, t),
			B: mix(from.B, to.B, t), A: 255, Set: true,
		}
		for yy := y0; yy < y1; yy++ {
			c.cells[yy*c.w+xx].BG = col
		}
	}
}

func (c *chromeCanvas) put(x, y int, r rune, fg protocol.Color, attrs uint8) {
	if y < 0 || y >= c.h {
		return
	}
	rw := runeWidth(r)
	if rw == 0 || x < 0 || x >= c.w || x+rw > c.w {
		return
	}
	cell := &c.cells[y*c.w+x]
	cell.R = r
	cell.FG = fg
	cell.Attrs = attrs
	cell.Flags &^= protocol.CellCont
	if rw == 2 {
		next := &c.cells[y*c.w+x+1]
		next.R = 0
		next.FG = fg
		next.BG = cell.BG
		next.Flags |= protocol.CellCont
	}
}

func (c *chromeCanvas) text(x, y int, s string, fg protocol.Color, attrs uint8) {
	for _, r := range s {
		rw := runeWidth(r)
		if rw == 0 {
			continue
		}
		if x >= c.w {
			break
		}
		if x+rw > 0 {
			c.put(x, y, r, fg, attrs)
		}
		x += rw
	}
}

// shortcut draws label with its first letter underlined.
func (c *chromeCanvas) shortcut(x, y int, label string, fg protocol.Color, attrs uint8) {
	for i, r := range label {
		if runeWidth(r) == 0 {
			continue
		}
		a := attrs
		if isLetter(r) {
			a |= protocol.AttrUnderline
			c.put(x, y, r, fg, a)
			x += runeWidth(r)
			c.text(x, y, label[i+len(string(r)):], fg, attrs)
			return
		}
		c.put(x, y, r, fg, a)
		x += runeWidth(r)
	}
}

func (c *chromeCanvas) underlinedRune(x, y int, label string, target rune, fg protocol.Color, attrs uint8) {
	idx := strings.IndexRune(label, target)
	if idx < 0 {
		c.text(x, y, label, fg, attrs)
		return
	}
	c.text(x, y, label[:idx], fg, attrs)
	x += stringWidth(label[:idx])
	a := attrs | protocol.AttrUnderline
	c.put(x, y, target, fg, a)
	x += runeWidth(target)
	c.text(x, y, label[idx+len(string(target)):], fg, attrs)
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func promptParts(prefix, text string, cursor int, selectAll bool, width int) (head string, at rune, tail string, show bool) {
	prefixW := stringWidth(prefix)
	if width <= prefixW {
		return truncateWidth(prefix, width), 0, "", false
	}
	contentW := width - prefixW
	runes := []rune(text)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(text) {
		cursor = len(text)
	}
	cur := utf8.RuneCountInString(text[:cursor])
	if cur < 0 {
		cur = 0
	}
	if cur > len(runes) {
		cur = len(runes)
	}
	if selectAll {
		visible := text
		if stringWidth(visible) > contentW {
			visible = tailWidth(visible, contentW)
		}
		return prefix + visible, 0, "", false
	}
	start := cur
	used := 0
	for start > 0 {
		rw := runeWidth(runes[start-1])
		if used+rw > contentW-1 {
			break
		}
		used += rw
		start--
	}
	at = ' '
	after := cur
	if cur < len(runes) {
		at = runes[cur]
		if runeWidth(at) != 1 {
			at = ' '
		}
		after = cur + 1
	}
	used++
	end := after
	for end < len(runes) {
		rw := runeWidth(runes[end])
		if used+rw > contentW {
			break
		}
		used += rw
		end++
	}
	return prefix + string(runes[start:cur]), at, string(runes[after:end]), true
}

func centeredLabel(x, room int, label string) (int, string) {
	if room < 1 {
		return x, ""
	}
	label = truncateWidth(label, room)
	pad := (room - stringWidth(label)) / 2
	return x + pad, label
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ContentBounds returns the page rectangle for a terminal height: the first
// content row and its height. The front end owns the chrome reservation and
// sends this rectangle to the engine.
func ContentBounds(height int) (top, contentHeight int) {
	switch {
	case height < 3:
		return 0, height
	case height < 5:
		return 1, height - 2
	default:
		return 2, height - 3
	}
}
