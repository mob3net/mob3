package browser

import (
	"browser/internal/engine"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

// tabTarget is one stop in the standard Tab order.
type tabTarget = engine.TabTarget

// tabTargets returns links and form controls in document order. Radio groups
// take one stop at their first button; arrows move within the group.
func (b *Browser) tabTargets() []tabTarget {
	return engine.BuildTabTargets(b.Page, b.Layout)
}

func (b *Browser) currentTabNode() *Node {
	if b.forms.InputFocus != nil {
		return b.forms.InputFocus
	}
	if b.forms.ControlFocus != nil {
		return b.forms.ControlFocus
	}
	if b.Layout != nil && b.focus >= 0 && b.focus < len(b.Layout.Links) {
		return b.Layout.Links[b.focus].Node
	}
	return nil
}

// tabStep moves through the standard Tab order, wrapping at the ends.
func (b *Browser) tabStep(delta int) {
	targets := b.tabTargets()
	if len(targets) == 0 {
		b.clearInputFocus()
		b.clearControlFocus()
		b.focus = -1
		b.status = "No focusable elements"
		return
	}
	next := engine.TabIndex(targets, b.currentTabNode(), delta)
	b.setTabFocus(targets[next], next+1, len(targets))
}

// setTabFocus makes one Tab target current: a link is focused visually, a text
// field starts editing, and other controls get a highlight and key handling.
func (b *Browser) setTabFocus(t tabTarget, index, total int) {
	if t.Link < 0 && isTextInput(t.Node) {
		b.focus = -1
		b.setInputFocus(t.Node)
		b.ensureInputVisible(t.Node)
		return
	}
	if b.forms.InputFocus != nil {
		// Leave the field through the normal blur path so change handlers run.
		b.setInputFocus(nil)
	} else {
		b.clearControlFocus()
	}
	if t.Link >= 0 {
		b.focus = t.Link
		b.ensureFocusVisible()
		li := b.Layout.Links[t.Link]
		b.status = fmt.Sprintf("%d/%d  %s", index, total, b.resolveHref(li.Href))
		return
	}
	b.focus = -1
	b.forms.ControlFocus = t.Node
	if t.Node.Tag == "select" && t.Node.HasAttr("multiple") {
		b.forms.ControlOption = firstSelectOption(t.Node)
	}
	if b.forms.ControlOption != nil {
		b.ensureInputVisible(b.forms.ControlOption)
	} else {
		b.ensureInputVisible(b.forms.ControlFocus)
	}
	b.status = tabControlStatus(t.Node)
}

func (b *Browser) clearControlFocus() {
	b.forms.ControlFocus = nil
	b.forms.ControlOption = nil
}

// paintControlFocus highlights the focused non-text control, or the active
// option inside a multi-select list box.
func (b *Browser) paintControlFocus(g *Grid, top, bottom int) {
	if b.Layout == nil || b.forms.ControlFocus == nil {
		return
	}
	target := b.forms.ControlFocus
	if b.forms.ControlOption != nil {
		target = b.forms.ControlOption
	}
	bg := RGB(0xff, 0xd7, 0x00)
	fg := RGB(0x1f, 0x23, 0x28)
	for i := range b.Layout.Frags {
		f := &b.Layout.Frags[i]
		if f.Node != target || f.Kind != fragText && f.Kind != fragImage {
			continue
		}
		y := f.Y - b.Scroll + top
		if y < top || y >= bottom {
			continue
		}
		g.Highlight(f.X, y, f.W, 1, bg, fg)
	}
}

// handleControlKey processes keys while a non-text form control is focused.
func (b *Browser) handleControlKey(k string) bool {
	switch k {
	case "esc", "ctrl-c":
		b.clearControlFocus()
		if k == "ctrl-c" {
			return b.HandleKey(k)
		}
		b.status = exitHint
		return false
	case "tab":
		b.tabStep(1)
		return false
	case "backtab":
		b.tabStep(-1)
		return false
	case "enter", " ", "space":
		b.activateControlFocus()
		return false
	case "left", "up":
		b.controlStep(-1)
		return false
	case "right", "down":
		b.controlStep(1)
		return false
	case "h":
		if controlUsesArrows(b.forms.ControlFocus) {
			b.controlStep(-1)
			return false
		}
	case "k":
		if controlUsesArrows(b.forms.ControlFocus) {
			b.controlStep(-1)
			return false
		}
	case "l":
		if controlUsesArrows(b.forms.ControlFocus) {
			b.controlStep(1)
			return false
		}
	case "j":
		if controlUsesArrows(b.forms.ControlFocus) {
			b.controlStep(1)
			return false
		}
	}
	b.clearControlFocus()
	return b.HandleKey(k)
}

func (b *Browser) activateControlFocus() {
	n := b.forms.ControlFocus
	if n == nil {
		return
	}
	top, bottom := b.viewport()
	r, _ := engine.VisibleInputRect(b.Layout, n, b.Scroll, top, bottom)
	if n.Tag == "select" && n.HasAttr("multiple") && b.forms.ControlOption != nil {
		b.toggleControl(b.forms.ControlOption)
		return
	}
	b.activateHintControl(n, r)
}

func (b *Browser) controlStep(dir int) {
	n := b.forms.ControlFocus
	if n == nil {
		return
	}
	if isRangeInput(n) {
		b.adjustRange(n, float64(dir))
		return
	}
	if n.Tag == "select" {
		if n.HasAttr("multiple") {
			b.stepMultiSelect(n, dir)
		} else {
			b.stepSingleSelect(n, dir)
		}
		return
	}
	if n.Tag == "input" && strings.EqualFold(strings.TrimSpace(n.Attr("type")), "radio") {
		b.stepRadio(n, dir)
	}
}

func (b *Browser) stepRadio(n *Node, dir int) {
	group := radioGroupNodes(n)
	if len(group) == 0 {
		return
	}
	idx := 0
	for i, r := range group {
		if r == n {
			idx = i
			break
		}
	}
	next := group[(idx+dir+len(group))%len(group)]
	b.forms.ControlFocus = next
	b.toggleControl(next)
	b.ensureInputVisible(next)
	b.status = tabControlStatus(next)
}

func (b *Browser) stepMultiSelect(sel *Node, dir int) {
	opts := selectOptions(sel)
	if len(opts) == 0 {
		return
	}
	idx := 0
	for i, opt := range opts {
		if opt == b.forms.ControlOption {
			idx = i
			break
		}
	}
	for tries := 0; tries < len(opts); tries++ {
		idx = (idx + dir + len(opts)) % len(opts)
		if !isDisabledControl(opts[idx]) && opts[idx].Style != nil && opts[idx].Style.Display != "none" {
			break
		}
	}
	b.forms.ControlOption = opts[idx]
	b.ensureInputVisible(b.forms.ControlOption)
	b.status = tabControlStatus(sel)
}

func (b *Browser) stepSingleSelect(sel *Node, dir int) {
	opts := selectOptions(sel)
	if len(opts) == 0 {
		return
	}
	idx := 0
	for i, opt := range opts {
		if opt.HasAttr("selected") {
			idx = i
			break
		}
	}
	for tries := 0; tries < len(opts); tries++ {
		idx = (idx + dir + len(opts)) % len(opts)
		if !isDisabledControl(opts[idx]) {
			break
		}
	}
	for _, opt := range opts {
		removeAttr(opt, "selected")
	}
	setAttr(opts[idx], "selected", "")
	b.fireSelectChange(sel)
}

func (b *Browser) ensureFocusVisible() {
	li := b.focusedLink()
	if li == nil || len(li.Rects) == 0 {
		return
	}
	top, bottom := b.viewport()
	vh := bottom - top
	if vh < 1 {
		vh = 1
	}
	r := li.Rects[0]
	if r.Y < b.Scroll {
		b.Scroll = r.Y - 1
	} else if r.Y+r.H >= b.Scroll+vh {
		b.Scroll = r.Y + r.H - vh + 1
	}
	b.clampScroll()
}

// isRangeInput reports an interactive range slider.

// toggleControl flips a checkbox, radio or multi-select option. A single select
// opens its option popup instead of cycling through values blind.
func (b *Browser) toggleControl(n *Node) {
	res := engine.ToggleControl(n)
	switch res.Action {
	case engine.ToggleOpenSelect:
		b.openSelectMenu(n)
		return
	case engine.ToggleValue:
		if res.Select != nil {
			b.fireSelectChange(res.Select)
			return
		}
		if b.js != nil {
			b.js.FireNodeEvent(n, "input")
			b.js.FireNodeEvent(n, "change")
		}
		b.rebuild()
	}
}

// fireSelectChange notifies scripts and repaints after a select value changes.
func (b *Browser) fireSelectChange(sel *Node) {
	if b.js != nil && sel != nil {
		b.js.FireNodeEvent(sel, "input")
		b.js.FireNodeEvent(sel, "change")
	}
	b.rebuild()
}

func (b *Browser) clearInputFocus() {
	b.forms.ClearInputFocus()
}

func (b *Browser) setInputFocus(n *Node) {
	if b.forms.InputFocus == n {
		return
	}
	b.clearControlFocus()
	old := b.forms.FocusInput(n)
	if old != nil && b.js != nil {
		b.js.FireNodeEvent(old, "change")
	}
	if n != nil {
		if isRangeInput(n) {
			b.status = "+/− or arrows adjust · Esc finishes"
		} else {
			b.status = "Typing · Enter submits · Esc finishes"
		}
	} else {
		b.status = ""
	}
	b.rebuild()
}

// ensureInputVisible scrolls a focused control into the viewport.
func (b *Browser) ensureInputVisible(n *Node) {
	if b.Layout == nil || n == nil {
		return
	}
	top, bottom := b.viewport()
	vh := bottom - top
	if vh < 1 {
		vh = 1
	}
	for i := range b.Layout.Frags {
		f := &b.Layout.Frags[i]
		if f.Node != n || f.Fixed {
			continue
		}
		if f.Y < b.Scroll {
			b.Scroll = f.Y - 1
		} else if f.Y+f.H >= b.Scroll+vh {
			b.Scroll = f.Y + f.H - vh + 1
		}
		b.clampScroll()
		return
	}
}

func (b *Browser) setInputValue(n *Node, v string) {
	if n == nil {
		return
	}
	n.Value = v
	n.ValueSet = true
	dirty := false
	if b.js != nil {
		b.js.FireNodeEvent(n, "input")
		// The rebuild below uses any DOM mutations the handler made, so do
		// not schedule a second timer-driven rebuild for the same keystroke.
		dirty = b.js.ConsumeDirty()
	}
	if b.consumePendingJS() {
		return
	}
	// Most keystrokes only change the control's text. Patch its fragments in
	// place when the widget keeps the same box geometry instead of running a
	// full style+layout pass for the whole page.
	if !dirty && !b.findActive && b.updateInputFragments(n) {
		return
	}
	b.rebuild()
}

// updateInputFragments rewrites the focused control's text fragments after a
// value change. It reports false when the widget's width or row count changed
// and a full relayout is required.
func (b *Browser) submitNode(n *Node) {
	b.clearInputFocus()
	if form := nearestForm(n); form != nil {
		b.submitForm(form)
	}
}

// validateForm applies the engine's supported HTML constraint checks.
func (b *Browser) validateForm(form *Node) (*Node, string) {
	return engine.ValidateForm(form)
}

// submitForm validates and submits a form. It reports whether a navigation was
// started and, when not, the reason shown to the user or returned to automation.
func (b *Browser) submitForm(form *Node) (started bool, reason string) {
	if form == nil {
		return false, "no form to submit"
	}
	if field, msg := b.validateForm(form); field != nil {
		if isTextInput(field) || isRangeInput(field) {
			b.setInputFocus(field)
		}
		b.status = msg
		return false, msg
	}
	prevented := false
	if b.js != nil {
		prevented = b.js.FireNodeEvent(form, "submit")
	}
	if b.consumePendingJS() {
		return true, ""
	}
	if prevented {
		return false, "submit event was prevented"
	}
	method, target, body, contentType, ok := b.formRequest(form)
	if !ok {
		return false, nonEmptyStatus(b.status)
	}
	if method == http.MethodPost {
		b.navigateRequest(method, target, body, contentType, true)
		return true, ""
	}
	if u, err := url.Parse(target); err == nil && u.Scheme == "about" {
		switch aboutPageName(u) {
		case "home", "start":
			// The start page's search box acts exactly like the URL prompt:
			// resolve the entered text as a URL, with the -search fallback.
			query := strings.TrimSpace(u.Query().Get("q"))
			if query == "" {
				b.status = "Enter a URL or search query"
				return false, b.status
			}
			b.Navigate(query, true)
			return true, ""
		}
	}
	b.Navigate(target, true)
	return true, ""
}

// queueFormSubmit implements HTMLFormElement.submit(): it queues a navigation
// without firing submit handlers or running constraint validation.
func (b *Browser) queueFormSubmit(form *Node) {
	method, target, body, contentType, ok := b.formRequest(form)
	if !ok {
		return
	}
	if b.js == nil {
		if method == http.MethodPost {
			b.navigateRequest(method, target, body, contentType, true)
		} else {
			b.Navigate(target, true)
		}
		return
	}
	b.js.SetPendingForm(&FormSubmission{Method: method, Target: target, Body: body, ContentType: contentType})
}

// resolveLabelTarget maps a click on label text to its control. Clicks on the
// control itself return nil so normal control handling is not duplicated.
func (b *Browser) resolveLabelTarget(n *Node) *Node {
	if n == nil || b.Page == nil || b.Page.Root == nil {
		return nil
	}
	for p := n; p != nil; p = p.Parent {
		if isLabelable(p) {
			return nil
		}
	}
	label := n
	for label != nil && label.Tag != "label" {
		label = label.Parent
	}
	if label == nil {
		return nil
	}
	target := labelControl(b.Page.Root, label)
	if target == nil {
		return nil
	}
	for p := n; p != nil; p = p.Parent {
		if p == target {
			return nil
		}
	}
	return target
}

// rangeSideAt reports which end of a range slider was clicked: -1 for the
// minus, +1 for the plus, and 0 elsewhere.
func (b *Browser) handleInputKey(k string) bool {
	n := b.forms.InputFocus
	if n == nil {
		return false
	}
	if isRangeInput(n) {
		return b.handleRangeKey(n, k)
	}
	value := inputValue(n)
	cur := n.InputCursor
	if cur < 0 || cur > len(value) {
		cur = len(value)
	}
	switch k {
	case "ctrl-a":
		b.forms.InputSelectAll = true
		return false
	case "esc":
		b.forms.InputSelectAll = false
		b.setInputFocus(nil)
		b.status = exitHint
		return false
	case "enter":
		if n.Tag == "textarea" {
			all := b.forms.InputSelectAll
			b.forms.InputSelectAll = false
			if all {
				value, cur = "", 0
			}
			if utf8.RuneCountInString(value) < 2048 && !inputAtMaxLength(n, value) {
				n.InputCursor = cur + 1
				b.setInputValue(n, value[:cur]+"\n"+value[cur:])
			}
			return false
		}
		b.forms.InputSelectAll = false
		b.submitInput()
		return false
	case "tab":
		b.forms.InputSelectAll = false
		b.tabStep(1)
		return false
	case "backtab":
		b.forms.InputSelectAll = false
		b.tabStep(-1)
		return false
	case "ctrl-c":
		b.forms.InputSelectAll = false
		return true
	case "home":
		if n.Tag == "textarea" {
			b.forms.InputSelectAll = false
			n.InputCursor = inputLineStart(value, cur)
			b.refreshInputCursor(n)
			return false
		}
	case "end":
		if n.Tag == "textarea" {
			b.forms.InputSelectAll = false
			n.InputCursor = inputLineEnd(value, cur)
			b.refreshInputCursor(n)
			return false
		}
	case "up", "down":
		b.forms.InputSelectAll = false
		if n.Tag == "textarea" {
			delta := -1
			if k == "down" {
				delta = 1
			}
			n.InputCursor = textareaVerticalOffset(value, cur, delta)
			b.refreshInputCursor(n)
		}
		return false
	}
	maxRunes := 2048
	if limit := maxLength(n); limit > 0 && limit < maxRunes {
		maxRunes = limit
	}
	newValue, newCur, handled := editLineKey(value, cur, k, editConfig{
		selAll:   &b.forms.InputSelectAll,
		maxRunes: maxRunes,
	})
	if handled {
		n.InputCursor = newCur
		if newValue != value {
			b.setInputValue(n, newValue)
		} else {
			b.refreshInputCursor(n)
		}
	}
	return false
}

// handleRangeKey drives a focused range slider.
func (b *Browser) handleRangeKey(n *Node, k string) bool {
	switch k {
	case "esc":
		b.setInputFocus(nil)
		b.status = exitHint
		return false
	case "enter":
		b.submitInput()
		return false
	case "tab":
		b.tabStep(1)
		return false
	case "backtab":
		b.tabStep(-1)
		return false
	case "ctrl-c":
		return true
	case "left", "down":
		b.adjustRange(n, -1)
		return false
	case "right", "up":
		b.adjustRange(n, 1)
		return false
	case "home":
		min, _, _, _ := rangeParams(n)
		b.setRangeValue(n, min)
		return false
	case "end":
		_, max, _, _ := rangeParams(n)
		b.setRangeValue(n, max)
		return false
	}
	return false
}

func (b *Browser) adjustRange(n *Node, direction float64) {
	min, max, step, value := rangeParams(n)
	value += direction * step
	if value < min {
		value = min
	}
	if value > max {
		value = max
	}
	b.setRangeValue(n, value)
}

func (b *Browser) setRangeValue(n *Node, value float64) {
	min, max, _, _ := rangeParams(n)
	if value < min {
		value = min
	}
	if value > max {
		value = max
	}
	n.Value = formatRangeNumber(value)
	n.ValueSet = true
	if b.js != nil {
		b.js.FireNodeEvent(n, "input")
		b.js.FireNodeEvent(n, "change")
	}
	b.rebuild()
}

// refreshInputCursor patches the focused widget after a caret-only move and
// falls back to a full rebuild when its display width changed.
func (b *Browser) refreshInputCursor(n *Node) {
	if !b.updateInputFragments(n) {
		b.rebuild()
	}
}

// Form helpers live in the engine; these wrappers keep the session call sites
func (b *Browser) updateInputFragments(n *Node) bool {
	return engine.UpdateInputFragments(b.Layout, n)
}

func (b *Browser) rangeSideAt(n *Node, x, y int) int {
	return engine.RangeSideAt(b.Layout, n, x, y)
}

func (b *Browser) formRequest(form *Node) (method, target string, body []byte, contentType string, ok bool) {
	base := b.URL
	if b.Page != nil && b.Page.Base != nil {
		base = b.Page.Base
	}
	method, target, body, contentType, ok = engine.FormRequest(form, base)
	if !ok {
		b.status = "Invalid form action"
	}
	return
}

// Form helpers live in the engine; these wrappers keep the session call sites
// readable during the migration.
func isTabControl(n *Node) bool                   { return engine.IsTabControl(n) }
func tabControlStatus(n *Node) string             { return engine.TabControlStatus(n) }
func controlUsesArrows(n *Node) bool              { return engine.ControlUsesArrows(n) }
func radioGroupNodes(n *Node) []*Node             { return engine.RadioGroupNodes(n) }
func firstSelectOption(n *Node) *Node             { return engine.FirstSelectOption(n) }
func isSubmitControl(n *Node) bool                { return engine.IsSubmitControl(n) }
func isToggleControl(n *Node) bool                { return engine.IsToggleControl(n) }
func nearestSelect(n *Node) *Node                 { return engine.NearestSelect(n) }
func isLabelable(n *Node) bool                    { return engine.IsLabelable(n) }
func findElementByID(root *Node, id string) *Node { return engine.FindElementByID(root, id) }
func labelControl(root, label *Node) *Node        { return engine.LabelControl(root, label) }
func inputAtMaxLength(n *Node, value string) bool { return engine.InputAtMaxLength(n, value) }
func inputLineStart(s string, cur int) int        { return engine.InputLineStart(s, cur) }
func inputLineEnd(s string, cur int) int          { return engine.InputLineEnd(s, cur) }
func textareaVerticalOffset(s string, cur, delta int) int {
	return engine.TextareaVerticalOffset(s, cur, delta)
}
