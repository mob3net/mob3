package browser

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// tabTarget is one stop in the standard Tab order: a link or a form control.
type tabTarget struct {
	link int
	node *Node
}

type radioKey struct {
	form *Node
	name string
}

// tabTargets returns links and form controls in document order. Radio groups
// take one stop at their first button; arrows move within the group.
func (b *Browser) tabTargets() []tabTarget {
	if b.page == nil || b.page.Root == nil || b.layout == nil {
		return nil
	}
	linkByNode := make(map[*Node]int, len(b.layout.Links))
	for i, li := range b.layout.Links {
		if li.Node == nil {
			continue
		}
		if _, ok := linkByNode[li.Node]; !ok {
			linkByNode[li.Node] = i
		}
	}
	var out []tabTarget
	seenRadio := make(map[radioKey]bool)
	b.page.Root.FindAll(func(n *Node) bool {
		if link, ok := linkByNode[n]; ok {
			out = append(out, tabTarget{link: link, node: n})
			return false
		}
		if !isTabControl(n) {
			return false
		}
		if strings.EqualFold(strings.TrimSpace(n.Attr("type")), "radio") && n.Tag == "input" {
			name := n.Attr("name")
			if name != "" {
				key := radioKey{form: nearestForm(n), name: name}
				if seenRadio[key] {
					return false
				}
				seenRadio[key] = true
			}
		}
		out = append(out, tabTarget{link: -1, node: n})
		return false
	})
	// A radio group's Tab stop is its checked button, or the first one.
	for i := range out {
		n := out[i].node
		if n == nil || n.Tag != "input" || !strings.EqualFold(strings.TrimSpace(n.Attr("type")), "radio") {
			continue
		}
		for _, r := range radioGroupNodes(n) {
			if r.HasAttr("checked") {
				out[i].node = r
				break
			}
		}
	}
	return out
}

// isTabControl reports whether n participates in the standard Tab order.
func isTabControl(n *Node) bool {
	if n == nil || n.Type != ElementNode || isDisabledControl(n) {
		return false
	}
	for p := n; p != nil; p = p.Parent {
		if p.Type != ElementNode || p.Style == nil {
			continue
		}
		if p.Style.Display == "none" || p.Style.Hidden {
			return false
		}
	}
	switch n.Tag {
	case "input":
		return !strings.EqualFold(strings.TrimSpace(n.Attr("type")), "hidden")
	case "select", "textarea", "button":
		return true
	}
	return false
}

func (b *Browser) currentTabNode() *Node {
	if b.inputFocus != nil {
		return b.inputFocus
	}
	if b.controlFocus != nil {
		return b.controlFocus
	}
	if b.layout != nil && b.focus >= 0 && b.focus < len(b.layout.Links) {
		return b.layout.Links[b.focus].Node
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
	cur := b.currentTabNode()
	idx := -1
	for i, t := range targets {
		if t.node == cur {
			idx = i
			break
		}
	}
	if idx < 0 && cur != nil {
		// Arrow movement can leave focus on a later radio in a group; Tab
		// continues from the group's single stop.
		if cur.Tag == "input" && strings.EqualFold(strings.TrimSpace(cur.Attr("type")), "radio") && cur.Attr("name") != "" {
			key := radioKey{form: nearestForm(cur), name: cur.Attr("name")}
			for i, t := range targets {
				n := t.node
				if n != nil && n.Tag == "input" && strings.EqualFold(strings.TrimSpace(n.Attr("type")), "radio") &&
					nearestForm(n) == key.form && n.Attr("name") == key.name {
					idx = i
					break
				}
			}
		}
	}
	next := 0
	if idx < 0 {
		if delta < 0 {
			next = len(targets) - 1
		}
	} else {
		next = (idx + delta + len(targets)) % len(targets)
	}
	b.setTabFocus(targets[next], next+1, len(targets))
}

// setTabFocus makes one Tab target current: a link is focused visually, a text
// field starts editing, and other controls get a highlight and key handling.
func (b *Browser) setTabFocus(t tabTarget, index, total int) {
	if t.link < 0 && isTextInput(t.node) {
		b.focus = -1
		b.setInputFocus(t.node)
		b.ensureInputVisible(t.node)
		return
	}
	if b.inputFocus != nil {
		// Leave the field through the normal blur path so change handlers run.
		b.setInputFocus(nil)
	} else {
		b.clearControlFocus()
	}
	if t.link >= 0 {
		b.focus = t.link
		b.ensureFocusVisible()
		li := b.layout.Links[t.link]
		b.status = fmt.Sprintf("%d/%d  %s", index, total, b.resolveHref(li.Href))
		return
	}
	b.focus = -1
	b.controlFocus = t.node
	if t.node.Tag == "select" && t.node.HasAttr("multiple") {
		b.controlOption = firstSelectOption(t.node)
	}
	if b.controlOption != nil {
		b.ensureInputVisible(b.controlOption)
	} else {
		b.ensureInputVisible(b.controlFocus)
	}
	b.status = tabControlStatus(t.node)
}

func tabControlStatus(n *Node) string {
	if n == nil {
		return ""
	}
	if n.Tag == "input" {
		switch strings.ToLower(strings.TrimSpace(n.Attr("type"))) {
		case "checkbox":
			return "Checkbox: Space toggles · Tab moves · Esc clears"
		case "radio":
			return "Radio: arrows choose · Space selects"
		case "range":
			return "Range: arrows adjust · Tab moves"
		case "file":
			return "File input (uploads unsupported)"
		}
	}
	switch n.Tag {
	case "select":
		if n.HasAttr("multiple") {
			return "Multi-select: arrows move · Space toggles"
		}
		return "Select: arrows change · Enter opens"
	case "button":
		return "Button: Enter activates"
	}
	return "Tab moves · Esc clears"
}

func (b *Browser) clearControlFocus() {
	b.controlFocus = nil
	b.controlOption = nil
}

// paintControlFocus highlights the focused non-text control, or the active
// option inside a multi-select list box.
func (b *Browser) paintControlFocus(g *Grid, top, bottom int) {
	if b.layout == nil || b.controlFocus == nil {
		return
	}
	target := b.controlFocus
	if b.controlOption != nil {
		target = b.controlOption
	}
	bg := RGB(0xff, 0xd7, 0x00)
	fg := RGB(0x1f, 0x23, 0x28)
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Node != target || f.Kind != fragText && f.Kind != fragImage {
			continue
		}
		y := f.Y - b.scroll + top
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
		if controlUsesArrows(b.controlFocus) {
			b.controlStep(-1)
			return false
		}
	case "k":
		if controlUsesArrows(b.controlFocus) {
			b.controlStep(-1)
			return false
		}
	case "l":
		if controlUsesArrows(b.controlFocus) {
			b.controlStep(1)
			return false
		}
	case "j":
		if controlUsesArrows(b.controlFocus) {
			b.controlStep(1)
			return false
		}
	}
	b.clearControlFocus()
	return b.HandleKey(k)
}

// controlUsesArrows reports whether the focused control reacts to arrow keys,
// so h/j/k/l can stand in for them without stealing page navigation keys.
func controlUsesArrows(n *Node) bool {
	if n == nil {
		return false
	}
	if isRangeInput(n) || n.Tag == "select" {
		return true
	}
	return n.Tag == "input" && strings.EqualFold(strings.TrimSpace(n.Attr("type")), "radio")
}

func (b *Browser) activateControlFocus() {
	n := b.controlFocus
	if n == nil {
		return
	}
	top, bottom := b.viewport()
	r, _ := b.visibleInputRect(n, top, bottom)
	if n.Tag == "select" && n.HasAttr("multiple") && b.controlOption != nil {
		b.toggleControl(b.controlOption)
		return
	}
	b.activateHintControl(n, r)
}

func (b *Browser) controlStep(dir int) {
	n := b.controlFocus
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
	b.controlFocus = next
	b.toggleControl(next)
	b.ensureInputVisible(next)
	b.status = tabControlStatus(next)
}

func radioGroupNodes(n *Node) []*Node {
	if n == nil || n.Tag != "input" || n.Attr("name") == "" {
		return []*Node{n}
	}
	scope := nearestForm(n)
	if scope == nil {
		for scope = n; scope.Parent != nil; scope = scope.Parent {
		}
	}
	name := n.Attr("name")
	var out []*Node
	scope.FindAll(func(x *Node) bool {
		if x.Tag == "input" && strings.EqualFold(strings.TrimSpace(x.Attr("type")), "radio") &&
			x.Attr("name") == name && !isDisabledControl(x) && x.Style != nil && x.Style.Display != "none" {
			out = append(out, x)
		}
		return false
	})
	if len(out) == 0 {
		return []*Node{n}
	}
	return out
}

func (b *Browser) stepMultiSelect(sel *Node, dir int) {
	opts := selectOptions(sel)
	if len(opts) == 0 {
		return
	}
	idx := 0
	for i, opt := range opts {
		if opt == b.controlOption {
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
	b.controlOption = opts[idx]
	b.ensureInputVisible(b.controlOption)
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

func firstSelectOption(sel *Node) *Node {
	opts := selectOptions(sel)
	for _, opt := range opts {
		if opt.HasAttr("selected") && !isDisabledControl(opt) && opt.Style != nil && opt.Style.Display != "none" {
			return opt
		}
	}
	for _, opt := range opts {
		if !isDisabledControl(opt) && opt.Style != nil && opt.Style.Display != "none" {
			return opt
		}
	}
	return nil
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
	if r.Y < b.scroll {
		b.scroll = r.Y - 1
	} else if r.Y+r.H >= b.scroll+vh {
		b.scroll = r.Y + r.H - vh + 1
	}
	b.clampScroll()
}

func isTextInput(n *Node) bool {
	if n == nil || n.Type != ElementNode || isDisabledControl(n) || isReadOnlyControl(n) {
		return false
	}
	if n.Tag == "textarea" {
		return true
	}
	if n.Tag != "input" {
		return false
	}
	switch strings.ToLower(n.Attr("type")) {
	case "", "text", "search", "email", "url", "tel", "number", "password",
		"date", "datetime-local", "month", "week", "time", "color":
		return true
	}
	return false
}

// isRangeInput reports an interactive range slider.
func isRangeInput(n *Node) bool {
	return n != nil && n.Type == ElementNode && n.Tag == "input" &&
		strings.EqualFold(strings.TrimSpace(n.Attr("type")), "range") &&
		!isDisabledControl(n)
}

func isFileInput(n *Node) bool {
	if n == nil || n.Type != ElementNode || n.Tag != "input" || isDisabledControl(n) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(n.Attr("type")), "file")
}

func isSubmitControl(n *Node) bool {
	if n == nil || n.Type != ElementNode || isDisabledControl(n) {
		return false
	}
	if n.Tag == "button" {
		switch strings.ToLower(strings.TrimSpace(n.Attr("type"))) {
		case "", "submit":
			return true
		}
		return false
	}
	if n.Tag != "input" {
		return false
	}
	switch strings.ToLower(n.Attr("type")) {
	case "submit", "image":
		return true
	}
	return false
}

func isToggleControl(n *Node) bool {
	if n == nil || n.Type != ElementNode || isDisabledControl(n) {
		return false
	}
	if n.Tag == "option" {
		sel := nearestSelect(n)
		return sel != nil && sel.HasAttr("multiple") && !isDisabledControl(sel)
	}
	if n.Tag == "select" {
		return true
	}
	if n.Tag != "input" {
		return false
	}
	switch strings.ToLower(n.Attr("type")) {
	case "checkbox", "radio":
		return true
	}
	return false
}

func nearestSelect(n *Node) *Node {
	for p := n; p != nil; p = p.Parent {
		if p.Type == ElementNode && p.Tag == "select" {
			return p
		}
	}
	return nil
}

// toggleControl flips a checkbox, radio or multi-select option. A single select
// opens its option popup instead of cycling through values blind.
func (b *Browser) toggleControl(n *Node) {
	if n == nil || isDisabledControl(n) {
		return
	}
	if n.Tag == "option" {
		sel := nearestSelect(n)
		if sel == nil || !sel.HasAttr("multiple") || n.HasAttr("disabled") {
			return
		}
		if n.HasAttr("selected") {
			removeAttr(n, "selected")
		} else {
			setAttr(n, "selected", "")
		}
		b.fireSelectChange(sel)
		return
	}
	if n.Tag == "select" {
		if n.HasAttr("multiple") {
			return
		}
		b.openSelectMenu(n)
		return
	}
	typ := strings.ToLower(n.Attr("type"))
	if typ == "radio" {
		name := n.Attr("name")
		// Radio groups are scoped to their form; controls outside any form share
		// the whole document as their group.
		scope := nearestForm(n)
		if scope == nil {
			for scope = n; scope.Parent != nil; scope = scope.Parent {
			}
		}
		if name != "" {
			scope.FindAll(func(x *Node) bool {
				if x != n && x.Tag == "input" && strings.EqualFold(x.Attr("type"), "radio") && x.Attr("name") == name {
					removeAttr(x, "checked")
				}
				return false
			})
		}
		setAttr(n, "checked", "")
	} else if n.HasAttr("checked") {
		removeAttr(n, "checked")
	} else {
		setAttr(n, "checked", "")
	}
	if b.js != nil {
		b.js.fireNodeEvent(n, "input")
		b.js.fireNodeEvent(n, "change")
	}
	b.rebuild()
}

// fireSelectChange notifies scripts and repaints after a select value changes.
func (b *Browser) fireSelectChange(sel *Node) {
	if b.js != nil && sel != nil {
		b.js.fireNodeEvent(sel, "input")
		b.js.fireNodeEvent(sel, "change")
	}
	b.rebuild()
}

func nearestForm(n *Node) *Node {
	for p := n; p != nil; p = p.Parent {
		if p.Tag == "form" {
			return p
		}
	}
	return nil
}

func (b *Browser) clearInputFocus() {
	b.inputSelectAll = false
	if b.inputFocus != nil {
		b.inputFocus.InputFocused = false
		b.inputFocus = nil
	}
}

func (b *Browser) setInputFocus(n *Node) {
	if b.inputFocus == n {
		return
	}
	b.inputSelectAll = false
	b.clearControlFocus()
	if old := b.inputFocus; old != nil {
		old.InputFocused = false
		old.InputCursor = 0
		if b.js != nil {
			b.js.fireNodeEvent(old, "change")
		}
	}
	b.inputFocus = n
	if n != nil {
		n.InputFocused = true
		if isRangeInput(n) {
			n.InputCursor = 0
			b.status = "+/− or arrows adjust · Esc finishes"
		} else {
			n.InputCursor = len(inputValue(n))
			b.status = "Typing · Enter submits · Esc finishes"
		}
	} else {
		b.status = ""
	}
	b.rebuild()
}

// ensureInputVisible scrolls a focused control into the viewport.
func (b *Browser) ensureInputVisible(n *Node) {
	if b.layout == nil || n == nil {
		return
	}
	top, bottom := b.viewport()
	vh := bottom - top
	if vh < 1 {
		vh = 1
	}
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Node != n || f.Fixed {
			continue
		}
		if f.Y < b.scroll {
			b.scroll = f.Y - 1
		} else if f.Y+f.H >= b.scroll+vh {
			b.scroll = f.Y + f.H - vh + 1
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
		b.js.fireNodeEvent(n, "input")
		// The rebuild below uses any DOM mutations the handler made, so do
		// not schedule a second timer-driven rebuild for the same keystroke.
		dirty = b.js.consumeDirty()
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
func (b *Browser) updateInputFragments(n *Node) bool {
	if b.layout == nil || n == nil || !isTextInput(n) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(n.Attr("type")), "color") {
		// The color field's own background is the live swatch, so it needs a
		// full paint whenever the value changes.
		return false
	}
	var frags []*Frag
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Kind == fragText && f.Node == n {
			frags = append(frags, f)
		}
	}
	if len(frags) == 0 {
		return false
	}
	if n.Tag == "textarea" {
		boxW := frags[0].W
		rows, cursorRow, cursorCol, hasCursor := textareaLayoutFor(n, boxW)
		if len(rows) != len(frags) {
			return false
		}
		for i := range rows {
			if stringWidth(rows[i]) != frags[i].W {
				return false
			}
			frags[i].Text = rows[i]
		}
		if hasCursor {
			b.layout.Cursor = Rect{X: frags[cursorRow].X + 2 + cursorCol, Y: frags[cursorRow].Y, W: 1, H: 1}
			b.layout.HasCursor = true
		} else {
			b.layout.HasCursor = false
		}
		return true
	}
	label := formLabel(n)
	if label == "" || len(frags) != 1 {
		return false
	}
	w := stringWidth(label)
	boxW := frags[0].W
	if w != boxW {
		// A fixed-width or flex-stretched control keeps the same box while the
		// value changes; rewrite the label inside it instead of rebuilding the
		// whole page. Auto-width controls must relayout when their box grows or
		// shrinks.
		fixed := false
		if st := n.Style; st != nil {
			fixed = st.FlexGrow > 0 || (st.Width.Set && !st.Width.IsAuto())
		}
		if !fixed {
			return false
		}
		if w > boxW {
			label = truncateWidth(label, boxW)
		} else if pad := boxW - w; pad > 0 {
			if strings.HasSuffix(label, "]") {
				label = strings.TrimSuffix(label, "]") + strings.Repeat(" ", pad) + "]"
			} else {
				label += strings.Repeat(" ", pad)
			}
		}
	}
	frags[0].Text = label
	if col, ok := inputCursorOffset(n, label); ok {
		b.layout.Cursor = Rect{X: frags[0].X + 2 + col, Y: frags[0].Y, W: 1, H: 1}
		b.layout.HasCursor = true
	} else {
		b.layout.HasCursor = false
	}
	return true
}

func (b *Browser) submitNode(n *Node) {
	b.clearInputFocus()
	if form := nearestForm(n); form != nil {
		b.submitForm(form)
	}
}

// validateForm applies the supported HTML constraint checks in document order.
// It returns the first invalid control and a warning for the footer.
func (b *Browser) validateForm(form *Node) (*Node, string) {
	var invalid *Node
	var reason string
	fail := func(x *Node, msg string) {
		if invalid == nil {
			invalid, reason = x, msg
		}
	}
	form.FindAll(func(x *Node) bool {
		if x.Tag != "input" && x.Tag != "textarea" && x.Tag != "select" {
			return false
		}
		if isDisabledControl(x) {
			return false
		}
		typ := strings.ToLower(strings.TrimSpace(x.Attr("type")))
		required := x.HasAttr("required")
		switch typ {
		case "submit", "button", "reset", "image", "file", "hidden":
			return false
		case "checkbox":
			if required && !x.HasAttr("checked") {
				fail(x, "Form: "+formFieldName(x)+" is required")
			}
			return false
		case "radio":
			if required && !radioGroupChecked(form, x) {
				fail(x, "Form: "+formFieldName(x)+" is required")
			}
			return false
		}
		var value string
		if x.Tag == "select" {
			if x.HasAttr("multiple") {
				if required && len(selectedOptionValues(x)) == 0 {
					fail(x, "Form: "+formFieldName(x)+" is required")
				}
				return false
			}
			value = inputValue(x)
		} else {
			value = inputValue(x)
		}
		if required && value == "" {
			fail(x, "Form: "+formFieldName(x)+" is required")
			return false
		}
		if value == "" {
			return false
		}
		if msg := invalidControlValue(x, typ, value); msg != "" {
			fail(x, "Form: "+formFieldName(x)+" "+msg)
			return false
		}
		if patternApplies(typ) {
			if pattern := strings.TrimSpace(x.Attr("pattern")); pattern != "" {
				re, err := regexp.Compile("^(?:" + pattern + ")$")
				if err == nil && !re.MatchString(value) {
					fail(x, "Form: "+formFieldName(x)+" does not match the pattern")
				}
			}
		}
		return false
	})
	return invalid, reason
}

func formFieldName(n *Node) string {
	for _, key := range []string{"name", "id", "placeholder"} {
		if v := strings.TrimSpace(n.Attr(key)); v != "" {
			return v
		}
	}
	if n != nil && n.Tag != "" {
		return n.Tag
	}
	return "field"
}

func radioGroupChecked(form *Node, x *Node) bool {
	if x == nil {
		return false
	}
	name := x.Attr("name")
	if name == "" {
		return x.HasAttr("checked")
	}
	checked := false
	form.FindAll(func(n *Node) bool {
		if n.Tag == "input" && strings.EqualFold(n.Attr("type"), "radio") &&
			n.Attr("name") == name && n.HasAttr("checked") && !isDisabledControl(n) {
			checked = true
		}
		return false
	})
	return checked
}

func patternApplies(typ string) bool {
	switch typ {
	case "", "text", "search", "email", "url", "tel", "password":
		return true
	}
	return false
}

var (
	inputDateRE     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	inputMonthRE    = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)
	inputWeekRE     = regexp.MustCompile(`^\d{4}-W(0[1-9]|[1-4]\d|5[0-3])$`)
	inputTimeRE     = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d(:[0-5]\d(\.\d{1,3})?)?$`)
	inputDateTimeRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T([01]\d|2[0-3]):[0-5]\d(:[0-5]\d(\.\d{1,3})?)?$`)
	inputColorRE    = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// invalidControlValue reports a format error for typed value controls.
func invalidControlValue(n *Node, typ, value string) string {
	switch typ {
	case "date":
		if !inputDateRE.MatchString(value) {
			return "needs a date like YYYY-MM-DD"
		}
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return "has an invalid date"
		}
	case "month":
		if !inputMonthRE.MatchString(value) {
			return "needs a month like YYYY-MM"
		}
	case "week":
		if !inputWeekRE.MatchString(value) {
			return "needs a week like YYYY-W01"
		}
	case "time":
		if !inputTimeRE.MatchString(value) {
			return "needs a time like HH:MM"
		}
	case "datetime-local":
		if !inputDateTimeRE.MatchString(value) {
			return "needs a date and time like YYYY-MM-DDTHH:MM"
		}
		datePart := value[:10]
		if _, err := time.Parse("2006-01-02", datePart); err != nil {
			return "has an invalid date"
		}
	case "color":
		if !inputColorRE.MatchString(value) {
			return "needs a color like #ff0000"
		}
	}
	return ""
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
		prevented = b.js.fireNodeEvent(form, "submit")
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

// formRequest resolves a form's action and collects its successful controls.
func (b *Browser) formRequest(form *Node) (method, target string, body []byte, contentType string, ok bool) {
	method = strings.ToUpper(strings.TrimSpace(form.Attr("method")))
	if method == "" {
		method = http.MethodGet
	}
	action := strings.TrimSpace(form.Attr("action"))
	var u *url.URL
	var err error
	if action == "" {
		u = b.url
	} else if b.page != nil {
		u, err = ResolveURL(action, b.page.Base)
	}
	if err != nil || u == nil {
		b.status = "Invalid form action"
		return "", "", nil, "", false
	}
	vals := url.Values{}
	form.FindAll(func(x *Node) bool {
		if x.Tag != "input" && x.Tag != "textarea" && x.Tag != "select" {
			return false
		}
		if isDisabledControl(x) {
			return false
		}
		name := x.Attr("name")
		if name == "" {
			return false
		}
		if x.Tag == "select" {
			if x.HasAttr("multiple") {
				for _, v := range selectedOptionValues(x) {
					vals.Add(name, v)
				}
			} else {
				vals.Add(name, inputValue(x))
			}
			return false
		}
		typ := strings.ToLower(x.Attr("type"))
		switch typ {
		case "submit", "button", "reset", "image", "file":
			return false
		case "checkbox", "radio":
			if x.HasAttr("checked") {
				v := x.Attr("value")
				if v == "" {
					v = "on"
				}
				vals.Add(name, v)
			}
		default:
			vals.Add(name, inputValue(x))
		}
		return false
	})
	if method == http.MethodPost {
		return method, u.String(), []byte(vals.Encode()), "application/x-www-form-urlencoded", true
	}
	q := u.Query()
	for k, values := range vals {
		q[k] = values
	}
	u.RawQuery = q.Encode()
	return http.MethodGet, u.String(), nil, "", true
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
	b.js.pendingForm = &jsFormSubmission{method: method, target: target, body: body, contentType: contentType}
}

// isLabelable reports controls that a <label> can activate.
func isLabelable(n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	switch n.Tag {
	case "input":
		return !strings.EqualFold(strings.TrimSpace(n.Attr("type")), "hidden")
	case "button", "select", "textarea":
		return true
	}
	return false
}

// findElementByID returns the first element with the given id below root.
func findElementByID(root *Node, id string) *Node {
	if root == nil || id == "" {
		return nil
	}
	var found *Node
	root.FindAll(func(n *Node) bool {
		if found == nil && n.Attr("id") == id {
			found = n
		}
		return false
	})
	return found
}

// labelControl returns the labelable control associated with a label: its for
// target, or the first labelable descendant for a wrapping label.
func labelControl(root, label *Node) *Node {
	if label == nil || label.Tag != "label" {
		return nil
	}
	if id := strings.TrimSpace(label.Attr("for")); id != "" {
		if c := findElementByID(root, id); isLabelable(c) {
			return c
		}
		return nil
	}
	var found *Node
	label.FindAll(func(n *Node) bool {
		if found == nil && isLabelable(n) {
			found = n
		}
		return false
	})
	return found
}

// resolveLabelTarget maps a click on label text to its control. Clicks on the
// control itself return nil so normal control handling is not duplicated.
func (b *Browser) resolveLabelTarget(n *Node) *Node {
	if n == nil || b.page == nil || b.page.Root == nil {
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
	target := labelControl(b.page.Root, label)
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
func (b *Browser) rangeSideAt(n *Node, x, y int) int {
	if b.layout == nil || n == nil {
		return 0
	}
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Kind != fragText || f.Node != n || y != f.Y {
			continue
		}
		rel := x - f.X
		if i := strings.Index(f.Text, "-"); i >= 0 && rel == stringWidth(f.Text[:i]) {
			return -1
		}
		if i := strings.LastIndex(f.Text, "+"); i >= 0 && rel == stringWidth(f.Text[:i]) {
			return 1
		}
	}
	return 0
}

func (b *Browser) handleInputKey(k string) bool {
	n := b.inputFocus
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
		b.inputSelectAll = true
		return false
	case "esc":
		b.inputSelectAll = false
		b.setInputFocus(nil)
		b.status = exitHint
		return false
	case "enter":
		if n.Tag == "textarea" {
			all := b.inputSelectAll
			b.inputSelectAll = false
			if all {
				value, cur = "", 0
			}
			if utf8.RuneCountInString(value) < 2048 && !inputAtMaxLength(n, value) {
				n.InputCursor = cur + 1
				b.setInputValue(n, value[:cur]+"\n"+value[cur:])
			}
			return false
		}
		b.inputSelectAll = false
		b.submitInput()
		return false
	case "tab":
		b.inputSelectAll = false
		b.tabStep(1)
		return false
	case "backtab":
		b.inputSelectAll = false
		b.tabStep(-1)
		return false
	case "ctrl-c":
		b.inputSelectAll = false
		return true
	case "home":
		if n.Tag == "textarea" {
			b.inputSelectAll = false
			n.InputCursor = inputLineStart(value, cur)
			b.refreshInputCursor(n)
			return false
		}
	case "end":
		if n.Tag == "textarea" {
			b.inputSelectAll = false
			n.InputCursor = inputLineEnd(value, cur)
			b.refreshInputCursor(n)
			return false
		}
	case "up", "down":
		b.inputSelectAll = false
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
		selAll:   &b.inputSelectAll,
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

// inputAtMaxLength reports whether the control has reached its maxlength.
func inputAtMaxLength(n *Node, value string) bool {
	limit := maxLength(n)
	return limit > 0 && utf8.RuneCountInString(value) >= limit
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
		b.js.fireNodeEvent(n, "input")
		b.js.fireNodeEvent(n, "change")
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

// inputLineStart returns the byte offset of the start of the caret's line.
func inputLineStart(s string, cur int) int {
	if cur > len(s) {
		cur = len(s)
	}
	if i := strings.LastIndexByte(s[:cur], '\n'); i >= 0 {
		return i + 1
	}
	return 0
}

// inputLineEnd returns the byte offset of the end of the caret's line.
func inputLineEnd(s string, cur int) int {
	if cur < 0 {
		cur = 0
	}
	if cur > len(s) {
		cur = len(s)
	}
	if i := strings.IndexByte(s[cur:], '\n'); i >= 0 {
		return cur + i
	}
	return len(s)
}

// textareaVerticalOffset moves the caret one logical line up or down, keeping
// its character column where the target line is long enough.
func textareaVerticalOffset(s string, cur, delta int) int {
	if cur < 0 {
		cur = 0
	}
	if cur > len(s) {
		cur = len(s)
	}
	start := inputLineStart(s, cur)
	col := utf8.RuneCountInString(s[start:cur])
	line := start
	if delta < 0 {
		if start == 0 {
			return cur
		}
		line = inputLineStart(s, start-1)
	} else {
		end := inputLineEnd(s, cur)
		if end == len(s) {
			return cur
		}
		line = end + 1
	}
	end := inputLineEnd(s, line)
	runes := []rune(s[line:end])
	if col > len(runes) {
		col = len(runes)
	}
	return line + len(string(runes[:col]))
}
