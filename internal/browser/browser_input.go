package browser

import (
	"browser/internal/engine"
	"browser/internal/frontend"
	"strings"
	"unicode/utf8"
)

// modalMouseAction classifies a press on an open modal: "close" for the ×,
// "outside" for a click on the backdrop, and "" for a click inside the body.
func (b *Browser) modalMouseAction(ev MouseEvent) string {
	if !ev.Press || ev.Motion || ev.Button != MouseButtonLeft {
		return ""
	}
	x, y := ev.X-1, ev.Y-1
	inRect := func(r Rect) bool {
		return r.W > 0 && r.H > 0 && x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
	}
	if inRect(b.modalClose) {
		return "close"
	}
	if !inRect(b.modalBox) {
		return "outside"
	}
	return ""
}

// linkAt returns the index of the link covering document cell (x, y), or -1.
func (b *Browser) linkAt(x, y int) int {
	if b.Layout == nil {
		return -1
	}
	best, bestLayer := -1, -1
	for i, li := range b.Layout.Links {
		for _, r := range li.Rects {
			if x < r.X || x >= r.X+r.W || y < r.Y || y >= r.Y+r.H {
				continue
			}
			// Positioned overlays and sticky bars paint above in-flow links,
			// so a click on their cells must follow the visible link.
			if layer := overlayLayer(li.Node); layer > bestLayer {
				best, bestLayer = i, layer
			}
			break
		}
	}
	return best
}

// openURLPrompt opens the URL field prefilled with the current URL, selected
// so typing replaces it like a normal browser address bar.
func (b *Browser) openURLPrompt() {
	b.openURLPromptMode(false)
}

// openVimPrompt opens the URL prompt from `:` so `q` can quit.
func (b *Browser) openVimPrompt() {
	b.openURLPromptMode(true)
}

func (b *Browser) openURLPromptMode(vim bool) {
	b.prompt = true
	b.promptVim = vim
	b.promptText = ""
	if b.URL != nil {
		b.promptText = b.URL.String()
	}
	b.promptCursor = len(b.promptText)
	b.promptSelectAll = true
	b.promptHist.reset()
	b.status = ""
}

// editConfig selects the per-control differences for editLineKey.
type editConfig struct {
	selAll            *bool
	collapseSelection bool
	maxRunes          int
}

func editLineKey(value string, caret int, k string, cfg editConfig) (string, int, bool) {
	return engine.EditLineKey(value, caret, k, engine.EditConfig{
		SelAll: cfg.selAll, CollapseSelection: cfg.collapseSelection, MaxRunes: cfg.maxRunes,
	})
}

func (b *Browser) handlePromptKey(k string) bool {
	text := b.promptText
	cur := b.promptCursor
	switch k {
	case "enter":
		text = strings.TrimSpace(text)
		quit := b.promptVim && (text == "q" || text == ":q")
		b.prompt = false
		b.promptVim = false
		b.promptText = ""
		b.promptCursor = 0
		b.promptSelectAll = false
		if quit {
			// :q exits immediately, unlike plain q which asks first.
			return true
		}
		if text != "" {
			b.promptHist.record(text)
			b.Navigate(text, true)
		}
		return false
	case "esc":
		b.closeURLPrompt()
		b.status = exitHint
		return false
	case "up", "ctrl-p":
		if prev, ok := b.promptHist.up(text); ok {
			text = prev
			cur = len(text)
		}
		b.promptSelectAll = false
	case "down", "ctrl-n":
		if next, ok := b.promptHist.down(text); ok {
			text = next
			cur = len(text)
		}
		b.promptSelectAll = false
	case "ctrl-c":
		b.closeURLPrompt()
		return false
	}
	text, cur, _ = editLineKey(text, cur, k, editConfig{
		selAll:            &b.promptSelectAll,
		collapseSelection: true,
	})
	b.promptText = text
	if cur > len(text) {
		cur = len(text)
	}
	b.promptCursor = cur
	return false
}

// closeURLPrompt dismisses the URL prompt and clears its editing state.
func (b *Browser) closeURLPrompt() {
	b.prompt = false
	b.promptVim = false
	b.promptText = ""
	b.promptCursor = 0
	b.promptSelectAll = false
}

// promptWindow returns the visible rune range for the URL text and the cursor
// rune position. cur is -1 for the full-selection view, which shows the tail of
// a long URL without a cursor.
func promptWindow(text string, cursor, contentW int, selectAll bool) (runes []rune, start, end, cur int) {
	runes = []rune(text)
	if contentW < 1 {
		return runes, 0, 0, 0
	}
	if selectAll {
		visible := text
		if stringWidth(visible) > contentW {
			visible = tailWidth(visible, contentW)
		}
		start = len(runes) - utf8.RuneCountInString(visible)
		return runes, start, len(runes), -1
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(text) {
		cursor = len(text)
	}
	cur = utf8.RuneCountInString(text[:cursor])
	if cur < 0 {
		cur = 0
	}
	if cur > len(runes) {
		cur = len(runes)
	}
	start = cur
	used := 0
	for start > 0 {
		rw := runeWidth(runes[start-1])
		if used+rw > contentW-1 {
			break
		}
		used += rw
		start--
	}
	after := cur
	if cur < len(runes) {
		after = cur + 1
	}
	used++
	end = after
	for end < len(runes) {
		rw := runeWidth(runes[end])
		if used+rw > contentW {
			break
		}
		used += rw
		end++
	}
	return runes, start, end, cur
}

func runeIndexToByte(runes []rune, i int) int {
	if i <= 0 {
		return 0
	}
	if i > len(runes) {
		i = len(runes)
	}
	return len(string(runes[:i]))
}

// promptOffsetAt maps a cell inside the URL field to a byte offset in the
// prompt text, using the same visible window that is currently drawn.
func (b *Browser) promptOffsetAt(x, width int) int {
	prefixW := stringWidth(" Open: ")
	contentW := width - prefixW
	if contentW < 1 {
		return 0
	}
	rel := x - prefixW
	if rel <= 0 {
		return 0
	}
	runes, start, end, cur := promptWindow(b.promptText, b.promptCursor, contentW, b.promptSelectAll)
	used := 0
	for i := start; i < end; i++ {
		if i == cur {
			if rel < used+1 {
				return runeIndexToByte(runes, i)
			}
			used++
			continue
		}
		rw := runeWidth(runes[i])
		if rel < used+rw {
			return runeIndexToByte(runes, i)
		}
		used += rw
	}
	return runeIndexToByte(runes, end)
}

// promptParts splits the visible URL field around the cursor. The cursor is an
// overlay cell rather than an inserted character, so moving it does not shift
// the text.
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

func (b *Browser) submitInput() {
	n := b.forms.InputFocus
	if n == nil {
		return
	}
	form := nearestForm(n)
	b.setInputFocus(nil)
	if form != nil {
		b.submitForm(form)
	}
}

// nodeAt returns the nearest element under a document cell, or nil.
func (b *Browser) nodeAt(x, y int) *Node {
	if b.Layout == nil {
		return nil
	}
	var best *Node
	bestLayer, bestArea, bestDepth := -1, 0, 0
	consider := func(n *Node, layer, area, depth int) {
		if n == nil {
			return
		}
		if layer > bestLayer || (layer == bestLayer && (area < bestArea || (area == bestArea && depth > bestDepth))) {
			best, bestLayer, bestArea, bestDepth = n, layer, area, depth
		}
	}
	for i := len(b.Layout.Frags) - 1; i >= 0; i-- {
		f := &b.Layout.Frags[i]
		if f.Node == nil || (f.Kind != fragText && f.Kind != fragImage) {
			continue
		}
		if x < f.X || x >= f.X+f.W || y < f.Y || y >= f.Y+f.H {
			continue
		}
		for p := f.Node; p != nil; p = p.Parent {
			if p.Type == ElementNode {
				consider(p, overlayLayer(p), -1, nodeDepth(p))
				break
			}
		}
	}
	// Padding and background gaps have no text fragment. Use laid-out element
	// boxes so hover menus remain reachable while moving between their items.
	for n, r := range b.Layout.Boxes {
		if r.W <= 0 || r.H <= 0 || x < r.X || x >= r.X+r.W || y < r.Y || y >= r.Y+r.H {
			continue
		}
		consider(n, overlayLayer(n), r.W*r.H, nodeDepth(n))
	}
	return best
}

// overlayLayer ranks positioned boxes above normal flow for hit testing.
// A nested positioned box must not mask the high-z overlay that contains it:
// a relative z-index:0 dialog child inside a fixed z-index:9999 modal still
// paints with the modal, above ordinary positioned content.
func overlayLayer(n *Node) int {
	layer := 0
	for p := n; p != nil; p = p.Parent {
		if p.Style == nil || !isPositionedStyle(p.Style) {
			continue
		}
		if l := 2 + p.Style.ZIndex; l > layer {
			layer = l
		}
	}
	return layer
}

func nodeDepth(n *Node) int {
	d := 0
	for p := n; p != nil; p = p.Parent {
		if p.Type == ElementNode {
			d++
		}
	}
	return d
}

// liveNode reports whether n belongs to the current page document. Reader mode
// lays out a detached extraction, so synthetic events must not be dispatched on
// those clones.
func (b *Browser) liveNode(n *Node) bool {
	if b.Page == nil || n == nil {
		return false
	}
	for p := n; p != nil; p = p.Parent {
		if p == b.Page.Root {
			return true
		}
	}
	return false
}

// clickContent follows a link under the pointer, or clears link focus.
// JavaScript click handlers run first and may prevent navigation.
func (b *Browser) clickContent(x, docY int) {
	b.clearControlFocus()
	if idx := b.linkAt(x, docY); idx >= 0 {
		li := b.Layout.Links[idx]
		if b.js != nil && b.liveNode(li.Node) {
			if b.js.DispatchClick(li.Node) {
				if b.js.EventErr() && linkFallbackOK(li.Href) {
					b.focus = idx
					b.followLink(li)
					return
				}
				if b.js.ConsumeDirty() {
					b.rebuild()
				}
				b.consumePendingJS()
				return
			}
		}
		if b.consumePendingJS() {
			return
		}
		b.focus = idx
		b.followFocused()
		return
	}
	n := b.nodeAt(x, docY)
	if target := b.resolveLabelTarget(n); target != nil {
		n = target
	}
	if n != nil && isDisabledControl(n) {
		b.focus = -1
		b.status = ""
		return
	}
	prevented := false
	if b.js != nil && b.liveNode(n) {
		prevented = b.js.DispatchClick(n)
		if b.js.ConsumeDirty() {
			b.rebuild()
		}
	}
	if b.consumePendingJS() {
		return
	}
	// Submit-style controls act on release so click handlers on the button run
	// first, and type=button/reset controls are not mistaken for submits.
	if n != nil && isSubmitControl(n) && !prevented {
		b.submitNode(n)
		return
	}
	b.focus = -1
	b.status = ""
}

// HandleMouse processes a decoded mouse event. It returns true when the
// browser should quit.
func (b *Browser) HandleMouse(ev MouseEvent) bool {
	if ev.Shift {
		// Most terminals handle Shift+drag themselves and never send this. If
		// one does forward it, leave the event alone and write nothing so a
		// terminal-native selection is not cleared by a redraw.
		b.skipDraw = true
		return false
	}
	// The copy confirmation holds the footer line until the pointer moves or
	// the page scrolls, then normal hover/status reporting resumes.
	if (ev.Motion || ev.Wheel != 0) && b.status == "Copied URL" {
		b.status = ""
	}
	// A release always ends a scrollbar drag, even when the report also
	// carries the motion bit or lands on the toolbar, footer or outside the
	// page. Otherwise the next pointer move would keep scrolling.
	if b.scrollDrag && !ev.Press {
		b.scrollDrag = false
		return false
	}
	if b.download != nil {
		switch b.modalMouseAction(ev) {
		case "close", "outside":
			b.download = nil
			b.status = ""
		}
		return false
	}
	if b.notice != nil {
		switch b.modalMouseAction(ev) {
		case "close", "outside":
			b.notice = nil
			b.status = ""
		}
		return false
	}
	if b.selectMenu != nil {
		b.handleSelectMenuMouse(ev)
		return false
	}
	if b.viewMenu != nil {
		b.handleViewMenuMouse(ev)
		return false
	}
	if b.zoomMenu != nil {
		b.handleZoomMenuMouse(ev)
		return false
	}
	if b.prompt {
		if ev.Motion || ev.Wheel != 0 || !ev.Press {
			return false
		}
		x := ev.X - 1
		y := ev.Y - 1
		for _, hit := range b.headerHits {
			if x < hit.x || x >= hit.x+hit.w || y < hit.y || y >= hit.y+hit.h {
				continue
			}
			if hit.kind == headerZoom {
				b.closeURLPrompt()
				b.openZoomMenu()
				return false
			}
			if hit.kind == headerURL {
				b.promptCursor = b.promptOffsetAt(x-hit.x, hit.w)
				b.promptSelectAll = false
				b.status = ""
				return false
			}
		}
		b.closeURLPrompt()
		b.status = exitHint
		return false
	}
	if b.consoleOpen {
		b.handleConsoleMouse(ev)
		return false
	}
	if b.linkHints && (ev.Press || ev.Wheel != 0) {
		b.clearLinkHints()
	}
	if b.hintConfirm && (ev.Press || ev.Wheel != 0) {
		b.clearHintConfirm()
	}
	x := ev.X - 1
	y := ev.Y - 1
	if x < 0 || y < 0 || x >= b.width || y >= b.height {
		b.setPageHover(nil)
		return false
	}
	if !ev.Press && b.selecting {
		top, bottom := b.viewport()
		if y < top || y >= bottom {
			b.selecting = false
			if b.hasSelection() {
				b.status = b.selectionSummary()
			}
		}
	}
	if ev.Wheel != 0 {
		step := 3
		if ev.Wheel < 0 {
			step = -step
		}
		b.scrollBy(step)
		top, bottom := b.viewport()
		b.updateHover(x, y, top, bottom)
		return false
	}
	if y < b.headerRows() && b.height >= 2 {
		if ev.Motion {
			b.hover = -1
			b.setPageHover(nil)
		}
		if !ev.Press || ev.Motion {
			return false
		}
		for _, hit := range b.headerHits {
			if x < hit.x || x >= hit.x+hit.w || y < hit.y || y >= hit.y+hit.h {
				continue
			}
			switch hit.kind {
			case headerBack, headerForward:
				// History is front-end-owned; the direct engine mouse path has
				// no session list to move through.
			case headerReload:
				b.reload()
			case headerStop:
				b.stopLoading()
			case headerHome:
				target := b.home
				if target == "" {
					target = "about:home"
				}
				b.Navigate(target, true)
			case headerHistory:
				b.Navigate("about:history", true)
			case headerHelp:
				b.Navigate("about:help", true)
			case headerConfig:
				b.Navigate("about:config", true)
			case headerURL:
				if b.forms.InputFocus != nil {
					b.setInputFocus(nil)
				}
				b.openURLPrompt()
			case headerZoom:
				b.openZoomMenu()
			}
			return false
		}
		return false
	}
	if y == b.height-1 {
		if ev.Motion {
			b.hover = -1
			b.setPageHover(nil)
			return false
		}
		if ev.Press {
			for _, hit := range b.footerHits {
				if x < hit.x || x >= hit.x+hit.w || y < hit.y || y >= hit.y+hit.h {
					continue
				}
				switch hit.kind {
				case headerConsole:
					b.toggleConsole()
					return false
				case headerJS:
					b.toggleJavaScript()
					return false
				case headerReader:
					b.toggleReader()
					return false
				case headerDark:
					b.toggleDark()
					return false
				case headerGraphics:
					b.cycleGraphics()
					return false
				case headerDownload:
					b.downloadCurrentPDF()
					return false
				case headerStatus:
					b.showStatusDetails()
					return false
				}
			}
		}
		return false
	}
	top, bottom := b.viewport()
	if info := b.scrollBar; info != nil && x == b.width-1 && y >= info.trackTop && y < info.trackTop+info.trackH {
		if ev.Press && ev.Button == 0 {
			b.scrollDrag = true
			b.scrollBarTo(y)
			return false
		}
		if ev.Motion && b.scrollDrag {
			b.scrollBarTo(y)
			return false
		}
	}
	if b.scrollDrag {
		if ev.Motion {
			b.scrollBarTo(y)
			return false
		}
		b.scrollDrag = false
		return false
	}
	if ev.Motion && !b.selecting {
		docY := b.Scroll + (y - top)
		b.setPageHover(b.nodeAt(x, docY))
		b.updateHover(x, y, top, bottom)
		return false
	}
	if ev.Motion && ev.Press {
		b.updateSelection(x, y, top, bottom)
		return false
	}
	if ev.Press {
		switch ev.Button {
		case MouseButtonRight:
			// Copy the link under the pointer instead of navigating; a
			// right-click away from a link leaves the page alone. Back stays
			// on the mouse back button and h.
			if y >= top && y < bottom {
				docY := b.Scroll + (y - top)
				if n := b.nodeAt(x, docY); n != nil {
					if a := nearestAnchor(n); a != nil {
						if href := strings.TrimSpace(a.Attr("href")); href != "" {
							b.clearInputFocus()
							target := b.resolveHref(href)
							if b.copyText(target) {
								b.status = "Copied URL"
								// Drop the hovered-link line so the copy
								// confirmation replaces it in the footer.
								b.hover = -1
								b.setPageHover(nil)
							}
						}
					}
				}
			}
			return false
		case MouseButtonLeft:
			if y >= top && y < bottom {
				docY := b.Scroll + (y - top)
				if n := b.nodeAt(x, docY); n != nil {
					if target := b.resolveLabelTarget(n); target != nil {
						n = target
					}
					if isFileInput(n) {
						b.selecting = false
						b.showUploadNotice()
						return false
					}
					if isToggleControl(n) {
						b.selecting = false
						b.toggleControl(n)
						return false
					}
					if isTextInput(n) {
						b.selecting = false
						b.setInputFocus(n)
						return false
					}
					if isRangeInput(n) {
						b.selecting = false
						if dir := b.rangeSideAt(n, x, docY); dir != 0 {
							if b.forms.InputFocus != n {
								b.setInputFocus(n)
							}
							b.adjustRange(n, float64(dir))
						} else {
							b.setInputFocus(n)
						}
						return false
					}
				}
				b.setInputFocus(nil)
				b.startSelection(engine.CellPos{X: x, Y: docY})
				b.status = ""
			}
		}
		return false
	}
	// Button release: a drag keeps the selection, a plain click acts.
	if b.selecting {
		moved := b.hasSelection()
		b.selecting = false
		if moved {
			b.status = b.selectionSummary()
		} else if y >= top && y < bottom {
			b.clickContent(x, b.Scroll+(y-top))
		}
	}
	// Do not restore the hovered-link line while the copy confirmation is
	// showing: releasing the right button would otherwise hide it at once.
	// Pointer motion has already cleared the message, so hover resumes then.
	if b.status != "Copied URL" {
		b.updateHover(x, y, top, bottom)
	}
	return false
}

// exitHint is shown after Escape so users can find the quit and help keys.
const exitHint = "q Quit / ? Help"

// quitConfirmText is shown before quitting; only y/Y confirms.
const quitConfirmText = "Are you sure you want to quit? (y)"

// HandleKey processes one key. It returns true when the browser should quit.
func (b *Browser) HandleKey(k string) bool {
	if ev, ok := ParseMouseToken(k); ok {
		return b.HandleMouse(ev)
	}
	if handled, quit := b.handleShellKey(k); handled {
		return quit
	}
	return b.handlePageKey(k)
}

// handleShellKey consumes keys owned by the trusted shell: quit confirmation,
// downloads, notices, menus, the console, the prompt and the find bar. It
// returns handled=false for keys that belong to the page.
func (b *Browser) handleShellKey(k string) (handled, quit bool) {
	if b.quitPending {
		b.quitPending = false
		b.status = ""
		return true, k == "y" || k == "Y" || k == "enter"
	}
	if b.download != nil {
		switch k {
		case "y", "Y":
			if b.copyText(b.download.command) {
				b.download = nil
			}
		case "esc", "enter", "q", "ctrl-c":
			b.download = nil
			b.status = ""
		}
		return true, false
	}
	if b.notice != nil {
		switch k {
		case "esc", "enter", "q", "ctrl-c":
			b.notice = nil
			b.status = ""
		}
		return true, false
	}
	if b.selectMenu != nil {
		return true, b.handleSelectMenuKey(k)
	}
	if b.viewMenu != nil {
		return true, b.handleViewMenuKey(k)
	}
	if b.zoomMenu != nil {
		return true, b.handleZoomMenuKey(k)
	}
	if b.consoleOpen {
		return true, b.handleConsoleKey(k)
	}
	if b.prompt {
		return true, b.handlePromptKey(k)
	}
	if b.findActive {
		return true, b.handleFindKey(k)
	}
	return false, false
}

// handlePageKey routes keys that belong to page content, forms, links and
// viewport movement.
func (b *Browser) handlePageKey(k string) bool {
	if b.forms.InputFocus != nil {
		return b.handleInputKey(k)
	}
	if b.forms.ControlFocus != nil {
		return b.handleControlKey(k)
	}
	if b.linkHints {
		return b.handleLinkHintKey(k)
	}
	if b.hintConfirm {
		return b.handleHintConfirmKey(k)
	}
	if k == "/" {
		b.openFind(findScopePage)
		return false
	}
	if k == "esc" && (b.loading || b.deferredImages) {
		b.stopLoading()
		return false
	}

	switch k {
	case "q", "ctrl-c":
		b.quitPending = true
		b.status = quitConfirmText
		return false
	case "j", "down", "ctrl-n":
		b.scrollBy(1)
	case "k", "up", "ctrl-p":
		b.scrollBy(-1)
	case " ", "space", "pgdn", "ctrl-v":
		b.scrollBy(b.viewportHeight() - 1)
	case "b", "pgup":
		b.scrollBy(-(b.viewportHeight() - 1))
	case "ctrl-d":
		b.scrollBy(b.viewportHeight() / 2)
	case "ctrl-u":
		b.scrollBy(-b.viewportHeight() / 2)
	case "g", "home":
		b.Scroll = 0
	case "G", "end":
		b.Scroll = b.maxScroll()
	case "tab":
		b.tabStep(1)
	case "backtab":
		b.tabStep(-1)
	case "enter":
		b.followFocused()
	case "r":
		b.reload()
	case "E":
		b.showStatusDetails()
	case "R":
		b.toggleReader()
	case "D":
		b.toggleDark()
	case "+":
		b.zoomBy(1)
	case "-":
		b.zoomBy(-1)
	case "0":
		b.resetZoom()
	case "J":
		b.toggleJavaScript()
	case "o", "ctrl-l":
		b.openURLPrompt()
	case ":":
		b.openVimPrompt()
	case "f":
		if li := b.currentFindLink(); li != nil {
			b.focus = li.Index
			b.followFocused()
			return false
		}
		b.openLinkHints(false)
	case "F":
		b.openLinkHints(true)
	case "i":
		b.cycleGraphics()
	case "X":
		b.openViewMenu()
	case "Z":
		b.openZoomMenu()
	case "u":
		if b.URL != nil {
			b.status = b.URL.String()
		}
	case "n":
		if b.findText != "" && b.findScope == findScopePage {
			b.findNext(1)
		}
	case "N":
		if b.findText != "" && b.findScope == findScopePage {
			b.findNext(-1)
		}
	case "d":
		b.downloadCurrentPDF()
	case "y":
		b.copySelection()
	case "Y":
		b.copyPageText()
	case "ctrl-y":
		b.copyViewportText()
	case "esc":
		if b.findText != "" {
			b.clearFind()
		}
		b.focus = -1
		b.clearControlFocus()
		b.clearSelection()
		b.status = exitHint
	case "!":
		b.toggleConsole()
	case "?":
		b.Navigate("about:help", true)
	case "H":
		b.Navigate("about:history", true)
	}
	return false
}

// Mouse types live in the frontend; these aliases keep session call sites and
// tests on the existing API during the migration.
type MouseEvent = frontend.MouseEvent

const (
	MouseButtonLeft    = frontend.MouseButtonLeft
	MouseButtonMiddle  = frontend.MouseButtonMiddle
	MouseButtonRight   = frontend.MouseButtonRight
	MouseButtonBack    = frontend.MouseButtonBack
	MouseButtonForward = frontend.MouseButtonForward
)

func ParseMouseToken(tok string) (MouseEvent, bool) { return frontend.ParseMouseToken(tok) }

func mouseToken(ev MouseEvent) string { return frontend.MouseToken(ev) }

func parseSGRMouse(params string, release bool) (MouseEvent, bool) {
	return frontend.ParseSGRMouse(params, release)
}

func parseX10Mouse(b, x, y byte) (MouseEvent, bool) {
	return frontend.ParseX10Mouse(b, x, y)
}
