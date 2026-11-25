package engine

import (
	"browser/internal/protocol"
	"strings"
)

// linkAt returns the index of the link covering document cell (x, y), or -1.
func (b *Session) linkAt(x, y int) int { return b.LinkAt(x, y) }

func (b *Session) submitInput() {
	n := b.Forms.InputFocus
	if n == nil {
		return
	}
	form := NearestForm(n)
	b.setInputFocus(nil)
	if form != nil {
		b.submitForm(form)
	}
}

// nodeAt returns the nearest element under a document cell, or nil.
func (b *Session) nodeAt(x, y int) *Node { return b.NodeAt(x, y) }

// liveNode reports whether n belongs to the current page document. Reader mode
// lays out a detached extraction, so synthetic events must not be dispatched on
// those clones.
func (b *Session) liveNode(n *Node) bool {
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

// emitOpenTab asks the front end to open a link in a background tab.
func (b *Session) emitOpenTab(url string) {
	if b == nil || b.sessionOut == nil || url == "" {
		return
	}
	_ = b.sessionOut.Send(protocol.Event{Body: protocol.OpenTab{URL: url}})
}

// emitLinkPreview asks the front end to show a link destination.
func (b *Session) emitLinkPreview(url string, target int) {
	if b == nil || b.sessionOut == nil || url == "" {
		return
	}
	_ = b.sessionOut.Send(protocol.Event{Body: protocol.LinkPreview{URL: url, Target: target}})
}

// clickContent follows a link under the pointer, or clears link focus.
// JavaScript click handlers run first and may prevent navigation.
func (b *Session) clickContent(x, docY int) {
	b.clearControlFocus()
	if idx := b.linkAt(x, docY); idx >= 0 {
		li := b.Layout.Links[idx]
		if b.JS != nil && b.liveNode(li.Node) {
			if b.JS.DispatchClick(li.Node) {
				if b.JS.EventErr() && linkFallbackOK(li.Href) {
					b.Focus = idx
					b.followLink(li)
					return
				}
				if b.JS.ConsumeDirty() {
					b.rebuild()
				}
				b.consumePendingJS()
				return
			}
		}
		if b.consumePendingJS() {
			return
		}
		b.Focus = idx
		b.followFocused()
		return
	}
	n := b.nodeAt(x, docY)
	if target := b.resolveLabelTarget(n); target != nil {
		n = target
	}
	if n != nil && IsDisabledControl(n) {
		b.Focus = -1
		b.status = ""
		return
	}
	prevented := false
	if b.JS != nil && b.liveNode(n) {
		prevented = b.JS.DispatchClick(n)
		if b.JS.ConsumeDirty() {
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
	b.Focus = -1
	b.status = ""
}

// HandleMouse processes a decoded mouse event. It returns true when the
// browser should quit.
func (b *Session) HandleMouse(ev MouseEvent) bool {
	if ev.Shift {
		// Most terminals handle Shift+drag themselves and never send this. If
		// one does forward it, leave the event alone and write nothing so a
		// terminal-native selection is not cleared by a redraw.
		return false
	}
	// The copy confirmation holds the footer line until the pointer moves or
	// the page scrolls, then normal hover/status reporting resumes.
	if (ev.Motion || ev.Wheel != 0) && b.status == "Copied URL" {
		b.status = ""
	}
	if b.LinkHints && (ev.Press || ev.Wheel != 0) {
		b.clearLinkHints()
	}
	if b.HintConfirm && (ev.Press || ev.Wheel != 0) {
		b.clearHintConfirm()
	}
	x := ev.X - 1
	y := ev.Y - 1
	if x < 0 || y < 0 || x >= b.Width || y >= b.Height {
		b.setPageHover(nil)
		return false
	}
	if !ev.Press && b.Selecting {
		top, bottom := b.viewport()
		if y < top || y >= bottom {
			b.Selecting = false
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
	top, bottom := b.viewport()
	if y < top || y >= bottom {
		if ev.Motion {
			b.Hover = -1
			b.setPageHover(nil)
		}
		return false
	}
	if ev.Motion && !b.Selecting {
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
		case MouseButtonMiddle:
			docY := b.Scroll + (y - top)
			if n := b.nodeAt(x, docY); n != nil {
				if a := NearestAnchor(n); a != nil {
					if href := strings.TrimSpace(a.Attr("href")); href != "" {
						b.emitOpenTab(b.resolveHref(href))
					}
				}
			}
			return false
		case MouseButtonRight:
			// Show the link destination before following or copying it.
			docY := b.Scroll + (y - top)
			if idx := b.linkAt(x, docY); idx >= 0 && b.Layout != nil && idx < len(b.Layout.Links) {
				b.emitLinkPreview(b.resolveHref(b.Layout.Links[idx].Href), idx)
			}
			return false
		case MouseButtonLeft:
			docY := b.Scroll + (y - top)
			if n := b.nodeAt(x, docY); n != nil {
				if target := b.resolveLabelTarget(n); target != nil {
					n = target
				}
				if IsFileInput(n) {
					b.Selecting = false
					b.showUploadNotice()
					return false
				}
				if isToggleControl(n) {
					b.Selecting = false
					b.toggleControl(n)
					return false
				}
				if IsTextInput(n) {
					b.Selecting = false
					cursor, ok := formClickOffset(b.Layout, n, x, docY)
					b.setInputFocus(n)
					if ok {
						n.InputCursor = cursor
						b.Forms.InputSelectAll = false
						b.refreshInputCursor(n)
					}
					return false
				}
				if IsRangeInput(n) {
					b.Selecting = false
					if dir := b.rangeSideAt(n, x, docY); dir != 0 {
						if b.Forms.InputFocus != n {
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
			b.startSelection(CellPos{X: x, Y: docY})
			b.status = ""
		}
		return false
	}
	// Button release: a drag keeps the selection, a plain click acts.
	if b.Selecting {
		moved := b.hasSelection()
		b.Selecting = false
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

// HandleKey processes one content key. Shell keys are front-end-owned; this
// path keeps page movement, forms, find navigation, hints and selection.
func (b *Session) HandleKey(k string) bool {
	if ev, ok := ParseMouseToken(k); ok {
		return b.HandleMouse(ev)
	}
	return b.handlePageKey(k)
}

// handlePageKey routes keys that belong to page content, forms, links, find
// navigation and viewport movement.
func (b *Session) handlePageKey(k string) bool {
	if b.Forms.InputFocus != nil {
		return b.handleInputKey(k)
	}
	if b.Forms.ControlFocus != nil {
		return b.handleControlKey(k)
	}
	if b.LinkHints {
		return b.handleLinkHintKey(k)
	}
	if b.HintConfirm {
		return b.handleHintConfirmKey(k)
	}
	if k == "esc" && (b.Loading || b.DeferredImages) {
		b.stopLoading()
		return false
	}

	switch k {
	case "j", "down", "ctrl-n":
		b.scrollBy(1)
	case "k", "up", "ctrl-p":
		b.scrollBy(-1)
	case " ", "space", "pgdn", "ctrl-v":
		b.scrollBy(b.viewportHeight() - 1)
	case "b", "pgup":
		b.scrollBy(-(b.viewportHeight() - 1))
	case "d", "ctrl-d":
		b.scrollBy(b.viewportHeight() / 2)
	case "u", "ctrl-u":
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
	case "f":
		if li := b.currentFindLink(); li != nil {
			b.Focus = li.Index
			b.followFocused()
			return false
		}
		b.openLinkHints(protocol.HintFollow)
	case "F":
		b.openLinkHints(protocol.HintNewTab)
	case "p":
		b.openLinkHints(protocol.HintPreview)
	case "n":
		if b.FindText != "" && b.FindScope == findScopePage {
			b.findNext(1)
		}
	case "N":
		if b.FindText != "" && b.FindScope == findScopePage {
			b.findNext(-1)
		}
	case "y":
		b.copySelection()
	case "Y":
		b.copyPageText()
	case "ctrl-y":
		b.copyViewportText()
	case "esc":
		if b.FindText != "" {
			b.clearFind()
		}
		b.Focus = -1
		b.clearControlFocus()
		b.clearSelection()
		b.status = exitHint
	}
	return false
}

// Mouse types live in the frontend; these aliases keep session call sites and
// tests on the existing API during the migration.
type MouseEvent = protocol.MouseEvent

const (
	MouseButtonLeft    = protocol.MouseButtonLeft
	MouseButtonMiddle  = protocol.MouseButtonMiddle
	MouseButtonRight   = protocol.MouseButtonRight
	MouseButtonBack    = protocol.MouseButtonBack
	MouseButtonForward = protocol.MouseButtonForward
)

func ParseMouseToken(tok string) (MouseEvent, bool) { return protocol.ParseMouseToken(tok) }

func mouseToken(ev MouseEvent) string { return protocol.MouseToken(ev) }

func parseSGRMouse(params string, release bool) (MouseEvent, bool) {
	return protocol.ParseSGRMouse(params, release)
}

func parseX10Mouse(b, x, y byte) (MouseEvent, bool) {
	return protocol.ParseX10Mouse(b, x, y)
}
