package browser

import (
	"fmt"
	"strings"

	"browser/internal/engine"
	"browser/internal/protocol"
)

// Hint assignment lives in the engine; these aliases keep the session-side
// target collection, painting and activation readable during the migration.
type hintGlyph = engine.HintGlyph
type hintChar = engine.HintChar
type linkHint = engine.LinkHint

func hintLetter(r rune) (byte, bool) { return engine.HintLetter(r) }
func buildLinkHints(links []int, glyphs map[int][]hintGlyph) []linkHint {
	return engine.AssignLinkHints(links, glyphs)
}
func encodeLinkHint(i, length int) string { return engine.EncodeLinkHint(i, length) }

// hintTarget is a link or editable field reachable through link hints.
type hintTarget = engine.HintTarget

// openLinkHints starts hint mode and labels every link and editable field that
// intersects the current viewport. Each label is picked from the target's own
// text: the first unused letter for up to 26 targets, then two letters for
// target-heavy pages. With ask set (the F key), choosing a link shows its
// destination and waits for confirmation; fields are focused directly.
func (b *Browser) openLinkHints(ask bool) {
	if b.Layout == nil {
		b.status = "No links or fields on this page"
		return
	}
	// Fixed bars keep viewport coordinates in FixedY; re-anchor them for the
	// current scroll so their controls pass the visibility test.
	b.Layout.SetFixedScroll(b.Scroll)
	top, bottom := b.viewport()
	var root *Node
	if b.Page != nil {
		root = b.Page.Root
	}
	targets := engine.BuildHintTargets(b.Layout, root, b.Scroll, top, bottom)
	if len(targets) == 0 {
		b.status = "No links or controls in view"
		return
	}
	ids := make([]int, len(targets))
	for i := range targets {
		ids[i] = i
	}
	b.HintTargets = targets
	b.HintItems = b.HintItems[:0]
	b.HintItems = append(b.HintItems, buildLinkHints(ids, engine.CollectHintGlyphs(b.Layout, targets, b.Scroll, top, bottom))...)
	b.LinkHints = true
	b.HintPrefix = ""
	b.HintAsk = ask
	b.HintConfirm = false
	b.HintConfirmLink = -1
	b.status = "Choose link or control (Esc cancels)"
	if b.sessionOut != nil {
		if err := b.sendHints(ask, top, bottom); err != nil {
			b.clearLinkHints()
			b.status = "No links or controls in view"
			return
		}
		b.HintsFrontend = true
	}
}

func (b *Browser) sendHints(ask bool, top, bottom int) error {
	items := make([]protocol.HintItem, 0, len(b.HintItems))
	for _, h := range b.HintItems {
		item := protocol.HintItem{Target: h.Target, Label: h.Label}
		if r, ok := b.hintTargetRect(h.Target, top, bottom); ok {
			item.X, item.Y = r.X, r.Y-b.Scroll
			item.W, item.H = r.W, r.H
		}
		item.Chars = make([]protocol.HintChar, len(h.Chars))
		for i, c := range h.Chars {
			item.Chars[i] = protocol.HintChar{R: c.R, X: c.X, Y: c.Y - b.Scroll}
		}
		t := b.hintTargetAt(h.Target)
		if t.Node == nil && t.Link >= 0 && b.Layout != nil && t.Link < len(b.Layout.Links) {
			item.Link = true
			item.Href = b.resolveHref(b.Layout.Links[t.Link].Href)
		}
		items = append(items, item)
	}
	return b.sessionOut.Send(protocol.Event{Body: protocol.Hints{
		Ask: ask, Prefix: b.HintPrefix, Items: items,
	}})
}

// reopenLinkHints restores an active hint session after a layout rebuild. The
// typed prefix is kept only when at least two hints still match, which is the
// only state the interactive key handler leaves active.
func (b *Browser) reopenLinkHints(ask bool, prefix string) {
	b.openLinkHints(ask)
	if !b.LinkHints || prefix == "" {
		return
	}
	matches := 0
	for _, h := range b.HintItems {
		if strings.HasPrefix(h.Label, prefix) {
			matches++
		}
	}
	if matches < 2 {
		return
	}
	b.HintPrefix = prefix
	b.status = fmt.Sprintf("Choose hint: %s (Esc cancels)", prefix)
}

// hintTargetRect returns the visible box of a hint target.
func (b *Browser) hintTargetRect(target int, top, bottom int) (Rect, bool) {
	return engine.HintTargetRect(b.Layout, b.HintTargets, target, b.Scroll, top, bottom)
}

// clearLinkHints leaves hint mode without navigating. It is safe to call when
// hints are not active.
func (b *Browser) clearLinkHints() {
	if b.Session.ClearHints() {
		b.status = ""
	}
}

// handleLinkHintKey consumes keys while hints are shown. Letters extend or
// follow a hint; Escape cancels. Any other key dismisses hint mode and is then
// handled normally, so scrolling always makes the hints disappear.
func (b *Browser) handleLinkHintKey(k string) bool {
	if k == "esc" || k == "ctrl-c" {
		b.clearLinkHints()
		b.status = exitHint
		return false
	}
	if len(k) == 1 {
		c := k[0]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c >= 'a' && c <= 'z' {
			b.typeLinkHint(string(c))
			return false
		}
	}
	b.clearLinkHints()
	if k == "f" {
		return false
	}
	return b.HandleKey(k)
}

// typeLinkHint narrows the visible hints by one letter and activates the
// target when only one hint still matches.
func (b *Browser) typeLinkHint(letter string) {
	next := b.HintPrefix + letter
	matches := 0
	match := -1
	for _, h := range b.HintItems {
		if strings.HasPrefix(h.Label, next) {
			matches++
			match = h.Target
		}
	}
	if matches == 0 {
		return
	}
	b.HintPrefix = next
	if matches == 1 {
		b.chooseHint(match)
		return
	}
	b.status = fmt.Sprintf("Choose hint: %s (Esc cancels)", next)
}

// chooseHint follows a chosen link or activates a chosen form control. With F,
// links wait for destination confirmation; controls act immediately.
func (b *Browser) chooseHint(target int) {
	t := b.hintTargetAt(target)
	ask := b.HintAsk
	top, bottom := b.viewport()
	r, _ := b.hintTargetRect(target, top, bottom)
	b.clearLinkHints()
	if t.Node != nil {
		b.activateHintControl(t.Node, r)
		return
	}
	if t.Link < 0 {
		return
	}
	if !ask {
		b.Focus = t.Link
		b.followFocused()
		return
	}
	b.beginHintConfirm(t.Link)
}

// activateHintControl applies the same action as clicking a form control and
// leaves that control focused for follow-up keyboard use.
func (b *Browser) activateHintControl(n *Node, r Rect) {
	if n == nil {
		return
	}
	switch {
	case isFileInput(n):
		b.focusControlTarget(n, nil)
		b.showUploadNotice()
	case n.Tag == "option":
		sel := nearestSelect(n)
		if sel == nil {
			return
		}
		b.focusControlTarget(sel, n)
		b.toggleControl(n)
	case isToggleControl(n):
		b.focusControlTarget(n, nil)
		b.toggleControl(n)
	case isTextInput(n), isRangeInput(n):
		b.setInputFocus(n)
		b.ensureInputVisible(n)
	case isButtonControl(n):
		b.focusControlTarget(n, nil)
		x, y := r.X, r.Y
		if r.W <= 0 || r.H <= 0 {
			x, y = r.X+1, r.Y
		}
		b.clickContent(x, y)
		if b.liveNode(n) {
			b.focusControlTarget(n, nil)
		}
	}
}

func (b *Browser) focusControlTarget(n *Node, option *Node) {
	if n == nil {
		return
	}
	b.Focus = -1
	b.Forms.ControlFocus = n
	b.Forms.ControlOption = option
	if n.Tag == "select" && n.HasAttr("multiple") && option == nil {
		b.Forms.ControlOption = firstSelectOption(n)
	}
	b.status = tabControlStatus(n)
}

func isButtonControl(n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	if n.Tag == "button" {
		return true
	}
	if n.Tag != "input" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(n.Attr("type"))) {
	case "submit", "button", "reset", "image":
		return true
	}
	return false
}

// hintTargetAt resolves a hint target index. Tests and older hint-only pages
// without hintTargets fall back to treating the index as a link index.
func (b *Browser) hintTargetAt(i int) hintTarget { return b.Session.HintTargetAt(i) }

// beginHintConfirm shows a chosen F link and waits for confirmation.
func (b *Browser) beginHintConfirm(link int) {
	b.HintConfirm = true
	b.HintConfirmLink = link
	target := ""
	if b.Layout != nil && link >= 0 && link < len(b.Layout.Links) {
		target = b.resolveHref(b.Layout.Links[link].Href)
	}
	b.status = target + "  (Enter/y follows · Esc cancels)"
}

// findHintLink locates an equivalent link after a layout rebuild, preferring
// the same DOM node and falling back to the same href.
func (b *Browser) findHintLink(node *Node, href string) int {
	if b.Layout == nil {
		return -1
	}
	if node != nil {
		for i, li := range b.Layout.Links {
			if li.Node == node {
				return i
			}
		}
	}
	for i, li := range b.Layout.Links {
		if li.Href == href {
			return i
		}
	}
	return -1
}

// clearHintConfirm cancels a pending F confirmation without navigating.
func (b *Browser) clearHintConfirm() {
	if !b.HintConfirm {
		return
	}
	b.HintConfirm = false
	b.HintConfirmLink = -1
	b.status = ""
}

// handleHintConfirmKey accepts Enter/y to follow the chosen link. Escape, n or
// any other key cancels the confirmation; the key is then processed normally.
func (b *Browser) handleHintConfirmKey(k string) bool {
	link := b.HintConfirmLink
	switch k {
	case "enter", "y", "Y":
		b.clearHintConfirm()
		if link >= 0 {
			b.Focus = link
			b.followFocused()
		}
		return false
	case "esc", "n", "N", "ctrl-c":
		b.clearHintConfirm()
		b.status = exitHint
		return false
	}
	b.clearHintConfirm()
	if k == "F" {
		return false
	}
	return b.HandleKey(k)
}

// inlineImagesEnabled reports whether the current frame should use native
// terminal image protocols. Hint mode and any open modal fall back to
// half-block art so text painted above the images cannot be covered by them.
func (b *Browser) inlineImagesEnabled() bool {
	if b.gfx == nil || b.LinkHints {
		return false
	}
	return b.dialog == nil && b.download == nil && b.notice == nil
}

// paintLinkHints draws the chosen letters of each matching link with a yellow
// background and underline. The page text itself is never replaced unless a
// link has no text letters at all.
func (b *Browser) paintLinkHints(g *Grid, top, bottom int) {
	if !b.LinkHints || b.HintsFrontend {
		return
	}
	engine.PaintHintLabels(g, b.Layout, b.HintTargets, b.HintItems, b.HintPrefix, b.Scroll, top, bottom, b.color)
}
