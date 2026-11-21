package browser

import (
	"strconv"
	"strings"
	"unicode"
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
	if b.layout == nil {
		return -1
	}
	for i, li := range b.layout.Links {
		for _, r := range li.Rects {
			if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
				return i
			}
		}
	}
	return -1
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
	if b.url != nil {
		b.promptText = b.url.String()
	}
	b.promptCursor = len(b.promptText)
	b.promptSelectAll = true
	b.promptHist.reset()
	b.status = ""
}

func prevRuneBoundary(s string, i int) int {
	if i <= 0 {
		return 0
	}
	if i > len(s) {
		i = len(s)
	}
	_, size := utf8.DecodeLastRuneInString(s[:i])
	return i - size
}

func nextRuneBoundary(s string, i int) int {
	if i >= len(s) {
		return len(s)
	}
	if i < 0 {
		i = 0
	}
	_, size := utf8.DecodeRuneInString(s[i:])
	return i + size
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// prevWordBoundary returns the start of the word before i, skipping any
// separators immediately before the caret. Deleting [start, i) therefore
// removes one URL segment while keeping the separators before it.
func prevWordBoundary(s string, i int) int {
	if i <= 0 {
		return 0
	}
	if i > len(s) {
		i = len(s)
	}
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:i])
		if isWordRune(r) {
			break
		}
		i -= size
	}
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:i])
		if !isWordRune(r) {
			break
		}
		i -= size
	}
	return i
}

// nextWordBoundary returns the end of the word at or after i, skipping leading
// separators first. It is used for forward word motion.
func nextWordBoundary(s string, i int) int {
	if i < 0 {
		i = 0
	}
	if i > len(s) {
		i = len(s)
	}
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if isWordRune(r) {
			break
		}
		i += size
	}
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !isWordRune(r) {
			break
		}
		i += size
	}
	return i
}

// forwardWordBoundary returns the end of the next word including the
// separators that follow it, matching kill-word behavior.
func forwardWordBoundary(s string, i int) int {
	i = nextWordBoundary(s, i)
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if isWordRune(r) {
			break
		}
		i += size
	}
	return i
}

func (b *Browser) handlePromptKey(k string) bool {
	text := b.promptText
	cur := b.promptCursor
	if cur < 0 {
		cur = 0
	}
	if cur > len(text) {
		cur = len(text)
	}
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
	case "left":
		if b.promptSelectAll {
			cur = 0
			b.promptSelectAll = false
		} else {
			cur = prevRuneBoundary(text, cur)
		}
	case "right":
		if b.promptSelectAll {
			cur = len(text)
			b.promptSelectAll = false
		} else {
			cur = nextRuneBoundary(text, cur)
		}
	case "home":
		cur = 0
		b.promptSelectAll = false
	case "end":
		cur = len(text)
		b.promptSelectAll = false
	case "ctrl-a":
		b.promptSelectAll = true
		cur = len(text)
	case "ctrl-e":
		cur = len(text)
		b.promptSelectAll = false
	case "ctrl-b":
		if b.promptSelectAll {
			cur = 0
			b.promptSelectAll = false
		} else {
			cur = prevRuneBoundary(text, cur)
		}
	case "ctrl-f":
		if b.promptSelectAll {
			cur = len(text)
			b.promptSelectAll = false
		} else {
			cur = nextRuneBoundary(text, cur)
		}
	case "alt-b", "ctrl-left", "alt-left":
		if b.promptSelectAll {
			cur = 0
			b.promptSelectAll = false
		} else {
			cur = prevWordBoundary(text, cur)
		}
	case "alt-f", "ctrl-right", "alt-right":
		if b.promptSelectAll {
			cur = len(text)
			b.promptSelectAll = false
		} else {
			cur = nextWordBoundary(text, cur)
		}
	case "backspace":
		if b.promptSelectAll {
			text = ""
			cur = 0
			b.promptSelectAll = false
		} else if cur > 0 {
			prev := prevRuneBoundary(text, cur)
			text = text[:prev] + text[cur:]
			cur = prev
		}
	case "delete":
		if b.promptSelectAll {
			text = ""
			cur = 0
			b.promptSelectAll = false
		} else if cur < len(text) {
			next := nextRuneBoundary(text, cur)
			text = text[:cur] + text[next:]
		}
	case "ctrl-u":
		text = ""
		cur = 0
		b.promptSelectAll = false
	case "ctrl-k":
		if b.promptSelectAll {
			text = ""
			cur = 0
			b.promptSelectAll = false
		} else {
			text = text[:cur]
		}
	case "alt-backspace", "ctrl-backspace", "ctrl-w":
		if b.promptSelectAll {
			text = ""
			cur = 0
			b.promptSelectAll = false
			break
		}
		cut := prevWordBoundary(text, cur)
		if cut < cur {
			text = text[:cut] + text[cur:]
			cur = cut
		}
	case "alt-delete", "ctrl-delete", "alt-d":
		if b.promptSelectAll {
			text = ""
			cur = 0
			b.promptSelectAll = false
			break
		}
		end := forwardWordBoundary(text, cur)
		if end > cur {
			text = text[:cur] + text[end:]
		}
	case "ctrl-c":
		b.closeURLPrompt()
		return false
	default:
		if utf8.RuneCountInString(k) == 1 {
			r, _ := utf8.DecodeRuneInString(k)
			if r >= 32 && r != 127 {
				if b.promptSelectAll {
					text = k
					cur = len(k)
					b.promptSelectAll = false
				} else {
					text = text[:cur] + k + text[cur:]
					cur += len(k)
				}
			}
		}
	}
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
	n := b.inputFocus
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
	if b.layout == nil {
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
	for i := len(b.layout.Frags) - 1; i >= 0; i-- {
		f := &b.layout.Frags[i]
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
	for n, r := range b.layout.Boxes {
		if r.W <= 0 || r.H <= 0 || x < r.X || x >= r.X+r.W || y < r.Y || y >= r.Y+r.H {
			continue
		}
		consider(n, overlayLayer(n), r.W*r.H, nodeDepth(n))
	}
	return best
}

// overlayLayer ranks positioned boxes above normal flow for hit testing.
func overlayLayer(n *Node) int {
	for p := n; p != nil; p = p.Parent {
		if p.Style != nil && (p.Style.Position == "absolute" || p.Style.Position == "fixed") {
			return 2
		}
	}
	return 0
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
	if b.page == nil || n == nil {
		return false
	}
	for p := n; p != nil; p = p.Parent {
		if p == b.page.Root {
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
		li := b.layout.Links[idx]
		if b.js != nil && b.liveNode(li.Node) {
			if b.js.dispatchClick(li.Node) {
				if b.js.eventErr && linkFallbackOK(li.Href) {
					b.focus = idx
					b.followLink(li)
					return
				}
				if b.js.consumeDirty() {
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
		prevented = b.js.dispatchClick(n)
		if b.js.consumeDirty() {
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
	if b.prompt {
		if ev.Motion || ev.Wheel != 0 || !ev.Press {
			return false
		}
		x := ev.X - 1
		y := ev.Y - 1
		for _, hit := range b.headerHits {
			if hit.kind == headerURL && x >= hit.x && x < hit.x+hit.w && y >= hit.y && y < hit.y+hit.h {
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
			case headerBack:
				b.back()
			case headerForward:
				b.forward()
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
			case headerBookmarks:
				b.Navigate("about:bookmarks", true)
			case headerHistory:
				b.Navigate("about:history", true)
			case headerHelp:
				b.Navigate("about:help", true)
			case headerConfig:
				b.Navigate("about:config", true)
			case headerURL:
				if b.inputFocus != nil {
					b.setInputFocus(nil)
				}
				b.openURLPrompt()
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
		docY := b.scroll + (y - top)
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
		case MouseButtonBack:
			b.clearInputFocus()
			b.back()
			return false
		case MouseButtonForward:
			b.clearInputFocus()
			b.forward()
			return false
		case MouseButtonRight:
			// Copy the link under the pointer instead of navigating; a
			// right-click away from a link leaves the page alone. Back stays
			// on the mouse back button and h.
			if y >= top && y < bottom {
				docY := b.scroll + (y - top)
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
				docY := b.scroll + (y - top)
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
							if b.inputFocus != n {
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
				p := cellPos{X: x, Y: docY}
				b.selAnchor = p
				b.selCursor = p
				b.selecting = true
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
			b.clickContent(x, b.scroll+(y-top))
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
	if b.quitPending {
		b.quitPending = false
		b.status = ""
		return k == "y" || k == "Y" || k == "enter"
	}
	if ev, ok := ParseMouseToken(k); ok {
		return b.HandleMouse(ev)
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
		return false
	}
	if b.notice != nil {
		switch k {
		case "esc", "enter", "q", "ctrl-c":
			b.notice = nil
			b.status = ""
		}
		return false
	}
	if b.selectMenu != nil {
		return b.handleSelectMenuKey(k)
	}
	if b.viewMenu != nil {
		return b.handleViewMenuKey(k)
	}
	if b.consoleOpen {
		return b.handleConsoleKey(k)
	}
	if b.prompt {
		return b.handlePromptKey(k)
	}
	if b.findActive {
		return b.handleFindKey(k)
	}

	if b.inputFocus != nil {
		return b.handleInputKey(k)
	}
	if b.controlFocus != nil {
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
		b.scroll = 0
	case "G", "end":
		b.scroll = b.maxScroll()
	case "tab":
		b.tabStep(1)
	case "backtab":
		b.tabStep(-1)
	case "enter":
		b.followFocused()
	case "ctrl-o", "[":
		b.back()
	case "ctrl-i", "]":
		b.forward()
	case "r":
		b.reload()
	case "E":
		b.showStatusDetails()
	case "R":
		b.toggleReader()
	case "D":
		b.toggleDark()
	case "J":
		b.toggleJavaScript()
	case "o":
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
	case "u":
		if b.url != nil {
			b.status = b.url.String()
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

// MouseEvent is a decoded terminal mouse report.
type MouseEvent struct {
	Button int // 0 left, 1 middle, 2 right, 8 back, 9 forward
	X, Y   int // 1-based terminal cell coordinates
	Press  bool
	Motion bool // drag report while a button is held
	Wheel  int  // -1 wheel up, +1 wheel down, 0 otherwise
	Shift  bool // modifier bit; reserved for terminal-native selection
}

const (
	MouseButtonLeft    = 0
	MouseButtonMiddle  = 1
	MouseButtonRight   = 2
	MouseButtonBack    = 8
	MouseButtonForward = 9
)

// decodeMouseButton strips modifier/motion/wheel bits and normalizes xterm's
// 128-131 encoding for buttons 8-11. Some terminals report 8-11 directly.
func decodeMouseButton(code int) int {
	if code&128 != 0 {
		return 8 + (code & 3)
	}
	switch code {
	case 8, 9, 10, 11:
		return code
	}
	return code & 3
}

const (
	mouseWheelUp   = 64
	mouseWheelDown = 65
)

// parseSGRMouse decodes the parameters of an SGR mouse report. params is the
// text between CSI and the final 'M'/'m' (for example "<0;12;5"); release is
// true for the lowercase final byte.
func parseSGRMouse(params string, release bool) (MouseEvent, bool) {
	params = strings.TrimPrefix(params, "<")
	parts := strings.Split(params, ";")
	if len(parts) != 3 {
		return MouseEvent{}, false
	}
	b, err1 := strconv.Atoi(parts[0])
	x, err2 := strconv.Atoi(parts[1])
	y, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return MouseEvent{}, false
	}
	ev := MouseEvent{X: x, Y: y, Press: !release, Motion: b&32 != 0, Shift: b&4 != 0}
	switch {
	case b&64 != 0:
		ev.Button = b & 3
		if ev.Button == 1 {
			ev.Wheel = 1
		} else {
			ev.Wheel = -1
		}
	default:
		ev.Button = decodeMouseButton(b)
	}
	return ev, true
}

// parseX10Mouse decodes a legacy 3-byte X10 mouse report.
func parseX10Mouse(b, x, y byte) (MouseEvent, bool) {
	btn := int(b) - 32
	x -= 32
	y -= 32
	if x < 1 || y < 1 || btn < 0 {
		return MouseEvent{}, false
	}
	ev := MouseEvent{X: int(x), Y: int(y), Motion: btn&32 != 0, Shift: btn&4 != 0}
	if btn&64 != 0 {
		ev.Press = true
		ev.Button = btn & 3
		if ev.Button == 1 {
			ev.Wheel = 1
		} else {
			ev.Wheel = -1
		}
		return ev, true
	}
	button := decodeMouseButton(btn)
	if button == 3 {
		ev.Button = 0
		ev.Press = false
		return ev, true
	}
	ev.Button = button
	ev.Press = true
	return ev, true
}

// mouseToken encodes a mouse event as an input token.
func mouseToken(ev MouseEvent) string {
	mod := ""
	if ev.Shift {
		mod = ":shift"
	}
	if ev.Wheel != 0 {
		dir := "up"
		if ev.Wheel > 0 {
			dir = "down"
		}
		return "mouse:wheel:" + dir + ":" + strconv.Itoa(ev.X) + ":" + strconv.Itoa(ev.Y) + mod
	}
	kind := "release"
	if ev.Press {
		kind = "press"
	}
	if ev.Motion {
		kind = "drag"
	}
	return "mouse:" + kind + ":" + strconv.Itoa(ev.Button) + ":" + strconv.Itoa(ev.X) + ":" + strconv.Itoa(ev.Y) + mod
}

// ParseMouseToken decodes a token produced by mouseToken.
func ParseMouseToken(tok string) (MouseEvent, bool) {
	if !strings.HasPrefix(tok, "mouse:") {
		return MouseEvent{}, false
	}
	parts := strings.Split(strings.TrimPrefix(tok, "mouse:"), ":")
	shift := false
	if len(parts) > 1 && parts[len(parts)-1] == "shift" {
		shift = true
		parts = parts[:len(parts)-1]
	}
	switch {
	case len(parts) == 4 && parts[0] == "wheel":
		x, err1 := strconv.Atoi(parts[2])
		y, err2 := strconv.Atoi(parts[3])
		if err1 != nil || err2 != nil {
			return MouseEvent{}, false
		}
		ev := MouseEvent{X: x, Y: y, Shift: shift}
		if parts[1] == "up" {
			ev.Wheel = -1
		} else {
			ev.Wheel = 1
		}
		return ev, true
	case len(parts) == 4 && (parts[0] == "press" || parts[0] == "release" || parts[0] == "drag"):
		btn, err1 := strconv.Atoi(parts[1])
		x, err2 := strconv.Atoi(parts[2])
		y, err3 := strconv.Atoi(parts[3])
		if err1 != nil || err2 != nil || err3 != nil {
			return MouseEvent{}, false
		}
		return MouseEvent{
			Button: btn, X: x, Y: y, Press: parts[0] != "release",
			Motion: parts[0] == "drag", Shift: shift,
		}, true
	}
	return MouseEvent{}, false
}
