package engine

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"browser/internal/protocol"
	"browser/internal/textedit"
)

// TabTargets returns links and form controls in document order.
func (s *Session) TabTargets() []TabTarget { return BuildTabTargets(s.Page, s.Layout) }

// CurrentTabNode returns the current Tab stop.
func (s *Session) CurrentTabNode() *Node {
	if s.Forms.InputFocus != nil {
		return s.Forms.InputFocus
	}
	if s.Forms.ControlFocus != nil {
		return s.Forms.ControlFocus
	}
	if s.Layout != nil && s.Focus >= 0 && s.Focus < len(s.Layout.Links) {
		return s.Layout.Links[s.Focus].Node
	}
	return nil
}

// TabStep moves through the standard Tab order, wrapping at the ends.
func (s *Session) TabStep(delta int, h SessionHooks) {
	targets := s.TabTargets()
	if len(targets) == 0 {
		s.Forms.ClearInputFocus()
		s.Forms.ClearControl()
		s.Focus = -1
		h.SetStatus("No focusable elements")
		return
	}
	next := TabIndex(targets, s.CurrentTabNode(), delta)
	s.SetTabFocus(targets[next], next+1, len(targets), h)
}

// SetTabFocus makes one Tab target current.
func (s *Session) SetTabFocus(t TabTarget, index, total int, h SessionHooks) {
	if t.Link < 0 && IsTextInput(t.Node) {
		s.Focus = -1
		s.SetInputFocus(t.Node, h)
		s.EnsureInputVisible(t.Node, h)
		return
	}
	if s.Forms.InputFocus != nil {
		s.SetInputFocus(nil, h)
	} else {
		s.Forms.ClearControl()
	}
	if t.Link >= 0 {
		s.Focus = t.Link
		s.EnsureFocusVisible(h)
		li := s.Layout.Links[t.Link]
		h.SetStatus(fmt.Sprintf("%d/%d  %s", index, total, s.ResolveHref(li.Href)))
		return
	}
	s.Focus = -1
	s.Forms.ControlFocus = t.Node
	if t.Node.Tag == "select" && t.Node.HasAttr("multiple") {
		s.Forms.ControlOption = FirstSelectOption(t.Node)
	}
	if s.Forms.ControlOption != nil {
		s.EnsureInputVisible(s.Forms.ControlOption, h)
	} else {
		s.EnsureInputVisible(s.Forms.ControlFocus, h)
	}
	h.SetStatus(TabControlStatus(t.Node))
}

// HandleControlKey processes keys while a non-text form control is focused.
func (s *Session) HandleControlKey(k string, h SessionHooks) bool {
	switch k {
	case "esc", "ctrl-c":
		s.Forms.ClearControl()
		if k == "ctrl-c" {
			return h.HandleKey(k)
		}
		h.SetStatus(ExitHint)
		return false
	case "tab":
		s.TabStep(1, h)
		return false
	case "backtab":
		s.TabStep(-1, h)
		return false
	case "enter", " ", "space":
		s.ActivateControlFocus(h)
		return false
	case "left", "up":
		s.ControlStep(-1, h)
		return false
	case "right", "down":
		s.ControlStep(1, h)
		return false
	case "h":
		if ControlUsesArrows(s.Forms.ControlFocus) {
			s.ControlStep(-1, h)
			return false
		}
	case "k":
		if ControlUsesArrows(s.Forms.ControlFocus) {
			s.ControlStep(-1, h)
			return false
		}
	case "l":
		if ControlUsesArrows(s.Forms.ControlFocus) {
			s.ControlStep(1, h)
			return false
		}
	case "j":
		if ControlUsesArrows(s.Forms.ControlFocus) {
			s.ControlStep(1, h)
			return false
		}
	}
	s.Forms.ClearControl()
	return h.HandleKey(k)
}

// ExitHint is shown after Escape.
const ExitHint = "q Quit / ? Help"

// ActivateControlFocus activates the focused control.
func (s *Session) ActivateControlFocus(h SessionHooks) {
	n := s.Forms.ControlFocus
	if n == nil {
		return
	}
	top, bottom := h.Viewport()
	r, _ := VisibleInputRect(s.Layout, n, s.Scroll, top, bottom)
	if n.Tag == "select" && n.HasAttr("multiple") && s.Forms.ControlOption != nil {
		s.ToggleControl(s.Forms.ControlOption, h)
		return
	}
	h.ActivateHintControl(n, r)
}

// ControlStep moves a radio group or select value.
func (s *Session) ControlStep(dir int, h SessionHooks) {
	n := s.Forms.ControlFocus
	if n == nil {
		return
	}
	if IsRangeInput(n) {
		s.AdjustRange(n, float64(dir), h)
		return
	}
	if n.Tag == "select" {
		if n.HasAttr("multiple") {
			s.StepMultiSelect(n, dir, h)
		} else {
			s.StepSingleSelect(n, dir, h)
		}
		return
	}
	if n.Tag == "input" && strings.EqualFold(strings.TrimSpace(n.Attr("type")), "radio") {
		s.StepRadio(n, dir, h)
	}
}

func (s *Session) StepRadio(n *Node, dir int, h SessionHooks) {
	group := RadioGroupNodes(n)
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
	s.Forms.ControlFocus = next
	s.ToggleControl(next, h)
	s.EnsureInputVisible(next, h)
	h.SetStatus(TabControlStatus(next))
}

func (s *Session) StepMultiSelect(sel *Node, dir int, h SessionHooks) {
	opts := SelectOptions(sel)
	if len(opts) == 0 {
		return
	}
	idx := 0
	for i, opt := range opts {
		if opt == s.Forms.ControlOption {
			idx = i
			break
		}
	}
	for tries := 0; tries < len(opts); tries++ {
		idx = (idx + dir + len(opts)) % len(opts)
		if !IsDisabledControl(opts[idx]) && opts[idx].Style != nil && opts[idx].Style.Display != "none" {
			break
		}
	}
	s.Forms.ControlOption = opts[idx]
	s.EnsureInputVisible(s.Forms.ControlOption, h)
	h.SetStatus(TabControlStatus(sel))
}

func (s *Session) StepSingleSelect(sel *Node, dir int, h SessionHooks) {
	opts := SelectOptions(sel)
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
		if !IsDisabledControl(opts[idx]) {
			break
		}
	}
	for _, opt := range opts {
		RemoveAttr(opt, "selected")
	}
	SetAttr(opts[idx], "selected", "")
	s.FireSelectChange(sel, h)
}

// EnsureFocusVisible scrolls the focused link into view.
func (s *Session) EnsureFocusVisible(h SessionHooks) {
	li := s.FocusedLink()
	if li == nil || len(li.Rects) == 0 {
		return
	}
	top, bottom := h.Viewport()
	vh := bottom - top
	if vh < 1 {
		vh = 1
	}
	r := li.Rects[0]
	if r.Y < s.Scroll {
		s.Scroll = r.Y - 1
	} else if r.Y+r.H >= s.Scroll+vh {
		s.Scroll = r.Y + r.H - vh + 1
	}
	s.ClampScroll(vh)
}

// ToggleControl flips a checkbox, radio or multi-select option.
func (s *Session) ToggleControl(n *Node, h SessionHooks) {
	res := ToggleControl(n)
	switch res.Action {
	case ToggleOpenSelect:
		h.OpenSelectMenu(n)
		return
	case ToggleValue:
		if res.Select != nil {
			s.FireSelectChange(res.Select, h)
			return
		}
		if s.JS != nil {
			s.JS.FireNodeEvent(n, "input")
			s.JS.FireNodeEvent(n, "change")
		}
		h.Rebuild()
	}
}

// FireSelectChange notifies scripts and repaints after a select value changes.
func (s *Session) FireSelectChange(sel *Node, h SessionHooks) {
	if s.JS != nil && sel != nil {
		s.JS.FireNodeEvent(sel, "input")
		s.JS.FireNodeEvent(sel, "change")
	}
	h.Rebuild()
}

// SetInputFocus focuses or unfocuses a text control.
func (s *Session) SetInputFocus(n *Node, h SessionHooks) {
	if s.Forms.InputFocus == n {
		return
	}
	s.Forms.ClearControl()
	old := s.Forms.FocusInput(n)
	if old != nil && s.JS != nil {
		s.JS.FireNodeEvent(old, "change")
	}
	if n != nil {
		if IsRangeInput(n) {
			h.SetStatus("+/− or arrows adjust · Esc finishes")
		} else {
			h.SetStatus("Typing · Enter submits · Esc finishes")
		}
	} else {
		h.SetStatus("")
	}
	h.Rebuild()
}

// EnsureInputVisible scrolls a focused control into the viewport.
func (s *Session) EnsureInputVisible(n *Node, h SessionHooks) {
	if s.Layout == nil || n == nil {
		return
	}
	top, bottom := h.Viewport()
	vh := bottom - top
	if vh < 1 {
		vh = 1
	}
	for i := range s.Layout.Frags {
		f := &s.Layout.Frags[i]
		if f.Node != n || f.Fixed {
			continue
		}
		if f.Y < s.Scroll {
			s.Scroll = f.Y - 1
		} else if f.Y+f.H >= s.Scroll+vh {
			s.Scroll = f.Y + f.H - vh + 1
		}
		s.ClampScroll(vh)
		return
	}
}

// SetInputValue updates a text control and notifies scripts.
func (s *Session) SetInputValue(n *Node, v string, h SessionHooks) {
	if n == nil {
		return
	}
	n.Value = v
	n.ValueSet = true
	dirty := false
	if s.JS != nil {
		s.JS.FireNodeEvent(n, "input")
		dirty = s.JS.ConsumeDirty()
	}
	if h.ApplyPendingJS(false, 0, false) {
		return
	}
	if !dirty && !s.FindActive && s.UpdateInputFragments(n) {
		return
	}
	h.Rebuild()
}

// SubmitNode submits the form containing n.
func (s *Session) SubmitNode(n *Node, h SessionHooks) {
	s.Forms.ClearInputFocus()
	if form := NearestForm(n); form != nil {
		s.SubmitForm(form, h)
	}
}

// SubmitForm validates and submits a form.
func (s *Session) SubmitForm(form *Node, h SessionHooks) (started bool, reason string) {
	if form == nil {
		return false, "no form to submit"
	}
	if field, msg := ValidateForm(form); field != nil {
		if IsTextInput(field) || IsRangeInput(field) {
			s.SetInputFocus(field, h)
		}
		h.SetStatus(msg)
		return false, msg
	}
	prevented := false
	if s.JS != nil {
		prevented = s.JS.FireNodeEvent(form, "submit")
	}
	if h.ApplyPendingJS(false, 0, false) {
		return true, ""
	}
	if prevented {
		return false, "submit event was prevented"
	}
	method, target, body, contentType, ok := s.FormRequest(form, h)
	if !ok {
		return false, "Invalid form action"
	}
	if method == protocol.MethodPost {
		h.NavigateRequest(method, target, body, contentType, true)
		return true, ""
	}
	if u, err := url.Parse(target); err == nil && u.Scheme == "about" {
		switch AboutPageName(u) {
		case "home", "start":
			query := strings.TrimSpace(u.Query().Get("q"))
			if query == "" {
				h.SetStatus("Enter a URL or search query")
				return false, "Enter a URL or search query"
			}
			h.Navigate(query, true)
			return true, ""
		}
	}
	h.Navigate(target, true)
	return true, ""
}

// QueueFormSubmit implements HTMLFormElement.submit().
func (s *Session) QueueFormSubmit(form *Node, h SessionHooks) {
	method, target, body, contentType, ok := s.FormRequest(form, h)
	if !ok {
		return
	}
	if s.JS == nil {
		if method == protocol.MethodPost {
			h.NavigateRequest(method, target, body, contentType, true)
		} else {
			h.Navigate(target, true)
		}
		return
	}
	s.JS.SetPendingForm(&FormSubmission{Method: method, Target: target, Body: body, ContentType: contentType})
}

// ResolveLabelTarget maps a click on label text to its control.
func (s *Session) ResolveLabelTarget(n *Node) *Node {
	if n == nil || s.Page == nil || s.Page.Root == nil {
		return nil
	}
	for p := n; p != nil; p = p.Parent {
		if IsLabelable(p) {
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
	target := LabelControl(s.Page.Root, label)
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

// HandleInputKey processes keys while a text control is focused.
func (s *Session) HandleInputKey(k string, h SessionHooks) bool {
	n := s.Forms.InputFocus
	if n == nil {
		return false
	}
	if IsRangeInput(n) {
		return s.HandleRangeKey(n, k, h)
	}
	value := InputValue(n)
	cur := n.InputCursor
	if cur < 0 || cur > len(value) {
		cur = len(value)
	}
	switch k {
	case "ctrl-a":
		s.Forms.InputSelectAll = true
		return false
	case "esc":
		s.Forms.InputSelectAll = false
		s.SetInputFocus(nil, h)
		h.SetStatus(ExitHint)
		return false
	case "enter":
		if n.Tag == "textarea" {
			all := s.Forms.InputSelectAll
			s.Forms.InputSelectAll = false
			if all {
				value, cur = "", 0
			}
			if utf8.RuneCountInString(value) < 2048 && !InputAtMaxLength(n, value) {
				n.InputCursor = cur + 1
				s.SetInputValue(n, value[:cur]+"\n"+value[cur:], h)
			}
			return false
		}
		s.Forms.InputSelectAll = false
		s.SubmitNode(s.Forms.InputFocus, h)
		return false
	case "tab":
		s.Forms.InputSelectAll = false
		s.TabStep(1, h)
		return false
	case "backtab":
		s.Forms.InputSelectAll = false
		s.TabStep(-1, h)
		return false
	case "ctrl-c":
		s.Forms.InputSelectAll = false
		return true
	case "home":
		if n.Tag == "textarea" {
			s.Forms.InputSelectAll = false
			n.InputCursor = InputLineStart(value, cur)
			s.RefreshInputCursor(n, h)
			return false
		}
	case "end":
		if n.Tag == "textarea" {
			s.Forms.InputSelectAll = false
			n.InputCursor = InputLineEnd(value, cur)
			s.RefreshInputCursor(n, h)
			return false
		}
	case "up", "down":
		s.Forms.InputSelectAll = false
		if n.Tag == "textarea" {
			delta := -1
			if k == "down" {
				delta = 1
			}
			n.InputCursor = TextareaVerticalOffset(value, cur, delta)
			s.RefreshInputCursor(n, h)
		}
		return false
	}
	maxRunes := 2048
	if limit := MaxLength(n); limit > 0 && limit < maxRunes {
		maxRunes = limit
	}
	newValue, newCur, handled := textedit.EditLineKey(value, cur, k, textedit.EditConfig{
		SelAll:   &s.Forms.InputSelectAll,
		MaxRunes: maxRunes,
	})
	if handled {
		n.InputCursor = newCur
		if newValue != value {
			s.SetInputValue(n, newValue, h)
		} else {
			s.RefreshInputCursor(n, h)
		}
	}
	return false
}

// HandleRangeKey drives a focused range slider.
func (s *Session) HandleRangeKey(n *Node, k string, h SessionHooks) bool {
	switch k {
	case "esc":
		s.SetInputFocus(nil, h)
		h.SetStatus(ExitHint)
		return false
	case "enter":
		s.SubmitNode(s.Forms.InputFocus, h)
		return false
	case "tab":
		s.TabStep(1, h)
		return false
	case "backtab":
		s.TabStep(-1, h)
		return false
	case "ctrl-c":
		return true
	case "left", "down":
		s.AdjustRange(n, -1, h)
		return false
	case "right", "up":
		s.AdjustRange(n, 1, h)
		return false
	case "home":
		min, _, _, _ := RangeParams(n)
		s.SetRangeValue(n, min, h)
		return false
	case "end":
		_, max, _, _ := RangeParams(n)
		s.SetRangeValue(n, max, h)
		return false
	}
	return false
}

// AdjustRange moves a range value by direction.
func (s *Session) AdjustRange(n *Node, direction float64, h SessionHooks) {
	min, max, step, value := RangeParams(n)
	value += direction * step
	if value < min {
		value = min
	}
	if value > max {
		value = max
	}
	s.SetRangeValue(n, value, h)
}

// SetRangeValue assigns and fires a range value.
func (s *Session) SetRangeValue(n *Node, value float64, h SessionHooks) {
	min, max, _, _ := RangeParams(n)
	if value < min {
		value = min
	}
	if value > max {
		value = max
	}
	n.Value = FormatRangeNumber(value)
	n.ValueSet = true
	if s.JS != nil {
		s.JS.FireNodeEvent(n, "input")
		s.JS.FireNodeEvent(n, "change")
	}
	h.Rebuild()
}

// RefreshInputCursor patches the focused widget after a caret-only move.
func (s *Session) RefreshInputCursor(n *Node, h SessionHooks) {
	if !s.UpdateInputFragments(n) {
		h.Rebuild()
	}
}

// UpdateInputFragments rewrites a control's text fragments when possible.
func (s *Session) UpdateInputFragments(n *Node) bool { return UpdateInputFragments(s.Layout, n) }

// RangeSideAt reports which end of a range slider was clicked.
func (s *Session) RangeSideAt(n *Node, x, y int) int { return RangeSideAt(s.Layout, n, x, y) }

// FormRequest returns the resolved submission request.
func (s *Session) FormRequest(form *Node, h SessionHooks) (method, target string, body []byte, contentType string, ok bool) {
	base := s.URL
	if s.Page != nil && s.Page.Base != nil {
		base = s.Page.Base
	}
	method, target, body, contentType, ok = FormRequest(form, base)
	if !ok {
		h.SetStatus("Invalid form action")
	}
	return
}
