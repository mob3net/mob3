package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// FormFields lists every form and form control in the document, including
// off-screen ones. Each control carries a stable CSS selector and its current
// value or state so fill, select, toggle and submit can target it directly.
func (a *Automation) FormFields(selector string) (string, error) {
	return a.formFieldsPage(selector, 0, 0)
}

type formControlEntry struct {
	form      *Node
	formIndex int
	node      *Node
}

// formFieldsPage returns one page of form controls. offset is a zero-based
// control index; limit <= 0 fills the response budget.
func (a *Automation) formFieldsPage(selector string, offset, limit int) (string, error) {
	if a == nil || a.b == nil {
		return "", errors.New("automation is closed")
	}
	if offset < 0 {
		return "", errors.New("offset must be zero or positive")
	}
	if limit < 0 {
		return "", errors.New("limit must be zero or positive")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.Page == nil || b.Page.Root == nil {
		return "No page loaded", nil
	}
	root := b.Page.Root
	var forms []*Node
	if selector != "" {
		n, err := findSelectorNode(root, selector)
		if err != nil {
			return "", err
		}
		orig := n
		if n.Tag != "form" {
			n = NearestForm(n)
		}
		if n == nil {
			return "", fmt.Errorf("%s is not inside a form", describeNode(orig))
		}
		forms = []*Node{n}
	} else {
		forms = root.FindAll(func(n *Node) bool { return n.Tag == "form" })
	}

	var entries []formControlEntry
	for i, f := range forms {
		for _, n := range f.FindAll(func(n *Node) bool { return IsFormControl(n) }) {
			if isHiddenInput(n) {
				continue
			}
			entries = append(entries, formControlEntry{form: f, formIndex: i + 1, node: n})
		}
	}
	looseTotal := 0
	if selector == "" {
		for _, n := range root.FindAll(func(n *Node) bool { return IsFormControl(n) }) {
			if isHiddenInput(n) || NearestForm(n) != nil {
				continue
			}
			entries = append(entries, formControlEntry{node: n})
			looseTotal++
		}
	}
	total := len(entries)
	if len(forms) == 0 && total == 0 {
		return "No forms or form controls found", nil
	}
	if total == 0 {
		return fmt.Sprintf("Forms: %d", len(forms)), nil
	}
	if offset >= total {
		return "", fmt.Errorf("offset %d is past the last control (%d controls)", offset, total)
	}
	maxItems := automationListCap
	if limit > 0 && limit < maxItems {
		maxItems = limit
	}
	var lines []string
	chars := 0
	var prevForm *Node
	looseHeader := false
	for i := offset; i < total && len(lines) < maxItems; i++ {
		e := entries[i]
		var line strings.Builder
		if e.form != nil && e.form != prevForm {
			fmt.Fprintf(&line, "[%d] form method=%q action=%q selector=%s\n",
				e.formIndex, formMethod(e.form), e.form.Attr("action"), strconv.Quote(selectorForNode(root, e.form)))
			prevForm = e.form
		} else if e.form == nil && !looseHeader {
			if len(forms) > 0 {
				line.WriteString("\n")
			}
			fmt.Fprintf(&line, "Controls outside any form: %d\n", looseTotal)
			looseHeader = true
		}
		line.WriteString("  ")
		line.WriteString(controlLine(root, e.node))
		text := line.String()
		if len(lines) > 0 && chars+len(text)+1 > automationListChars {
			break
		}
		lines = append(lines, text)
		chars += len(text) + 1
	}
	end := offset + len(lines)
	var sb strings.Builder
	if len(forms) > 0 {
		fmt.Fprintf(&sb, "Forms: %d\n", len(forms))
	}
	fmt.Fprintf(&sb, "Controls: %d  Showing: %d-%d of %d", total, offset+1, end, total)
	if end < total {
		fmt.Fprintf(&sb, "  Next: %d", end)
	}
	sb.WriteString("\n")
	sb.WriteString(strings.Join(lines, "\n"))
	return sb.String(), nil
}

func formMethod(form *Node) string {
	m := strings.ToUpper(strings.TrimSpace(form.Attr("method")))
	if m == "" {
		return "GET"
	}
	return m
}

func isHiddenInput(n *Node) bool {
	return n.Tag == "input" && strings.EqualFold(strings.TrimSpace(n.Attr("type")), "hidden")
}

func controlLine(root, n *Node) string {
	typ := ""
	if n.Tag == "input" {
		typ = strings.ToLower(strings.TrimSpace(n.Attr("type")))
		if typ == "" {
			typ = "text"
		}
	}
	parts := []string{n.Tag}
	if name := n.Attr("name"); name != "" {
		parts = append(parts, "name="+strconv.Quote(name))
	}
	switch {
	case typ != "":
		parts = append(parts, "type="+strconv.Quote(typ))
	case n.Tag == "button":
		parts = append(parts, "type="+strconv.Quote("submit"))
	}
	switch {
	case typ == "checkbox" || typ == "radio":
		state := "unchecked"
		if n.HasAttr("checked") {
			state = "checked"
		}
		value := n.Attr("value")
		if value == "" {
			value = "on"
		}
		parts = append(parts, "state="+state, "value="+strconv.Quote(value))
	case n.Tag == "select":
		opts := SelectOptions(n)
		var values []string
		for _, o := range opts {
			values = append(values, OptionValue(o))
		}
		if n.HasAttr("multiple") {
			parts = append(parts, "multiple", "values="+quotedList(SelectedOptionValues(n)))
		} else {
			parts = append(parts, "value="+strconv.Quote(InputValue(n)))
		}
		parts = append(parts, "options="+quotedList(values))
	case n.Tag == "textarea":
		parts = append(parts, "value="+strconv.Quote(clipText(collapseWhitespace(InputValue(n)), 80)))
	case typ == "password":
		// Never expose a password through the automation surface. Presence is
		// still useful, so an empty field stays visibly empty.
		value := `""`
		if InputValue(n) != "" {
			value = `"[redacted]"`
		}
		parts = append(parts, "value="+value)
	case n.Tag == "button":
		label := collapseWhitespace(n.TextContent())
		if label == "" {
			label = n.Attr("value")
		}
		parts = append(parts, "text="+strconv.Quote(clipText(label, 80)))
	default:
		parts = append(parts, "value="+strconv.Quote(clipText(collapseWhitespace(InputValue(n)), 80)))
	}
	if n.HasAttr("required") {
		parts = append(parts, "required")
	}
	if IsReadOnlyControl(n) {
		parts = append(parts, "readonly")
	}
	if IsDisabledControl(n) {
		parts = append(parts, "disabled")
	}
	parts = append(parts, "selector="+strconv.Quote(selectorForNode(root, n)))
	return "- " + strings.Join(parts, " ")
}

// Fill sets text-like controls. Keys are CSS selectors; each value replaces the
// control's current value. It does not submit anything.
func (a *Automation) Fill(fields map[string]string) (int, error) {
	if a == nil || a.b == nil {
		return 0, errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.Page == nil || b.Page.Root == nil {
		return 0, errors.New("no page loaded")
	}
	if len(fields) == 0 {
		return 0, errors.New("fill needs at least one selector")
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	filled := 0
	for _, sel := range keys {
		n, err := findSelectorNode(b.Page.Root, sel)
		if err != nil {
			return filled, err
		}
		if err := a.fillControlLocked(n, fields[sel]); err != nil {
			return filled, fmt.Errorf("%s: %w", sel, err)
		}
		filled++
	}
	return filled, nil
}

func (a *Automation) fillControlLocked(n *Node, value string) error {
	b := a.b
	if n == nil || n.Type != ElementNode {
		return errors.New("not an element")
	}
	if IsDisabledControl(n) {
		return fmt.Errorf("%s is disabled", describeNode(n))
	}
	if n.Tag == "textarea" {
		if IsReadOnlyControl(n) {
			return fmt.Errorf("%s is read-only", describeNode(n))
		}
		b.setInputValue(n, truncateRunes(value, MaxLength(n)))
		a.fireChangeLocked(n)
		return nil
	}
	if n.Tag != "input" {
		return fmt.Errorf("%s is not an input or textarea; use select or toggle", describeNode(n))
	}
	typ := strings.ToLower(strings.TrimSpace(n.Attr("type")))
	switch typ {
	case "checkbox", "radio":
		return errors.New("use toggle for checkboxes and radios")
	case "file":
		return errors.New("file uploads are not supported")
	case "submit", "button", "reset", "image", "hidden":
		return fmt.Errorf("%s is not an editable field", describeNode(n))
	case "range":
		f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return errors.New("range value must be a number")
		}
		b.setRangeValue(n, f)
		return nil
	}
	if IsReadOnlyControl(n) {
		return fmt.Errorf("%s is read-only", describeNode(n))
	}
	if !IsTextInput(n) {
		return fmt.Errorf("%s is not an editable text field", describeNode(n))
	}
	b.setInputValue(n, truncateRunes(value, MaxLength(n)))
	a.fireChangeLocked(n)
	return nil
}

// fireChangeLocked fires the change event a browser sends when a value is
// committed and applies any DOM mutations the handler made.
func (a *Automation) fireChangeLocked(n *Node) {
	b := a.b
	if b.JS == nil {
		return
	}
	b.JS.FireNodeEvent(n, "change")
	if b.JS.ConsumeDirty() {
		b.rebuild()
	}
}

func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit])
}

// Select chooses one or more options in a select by value, label, or zero-based
// index. For a multiple select, values selects the whole set.
func (a *Automation) Select(selector string, value *string, values []string, label string, index *int) (string, error) {
	if a == nil || a.b == nil {
		return "", errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.Page == nil || b.Page.Root == nil {
		return "", errors.New("no page loaded")
	}
	n, err := findSelectorNode(b.Page.Root, selector)
	if err != nil {
		return "", err
	}
	if n.Tag != "select" {
		return "", fmt.Errorf("%s is not a <select>", describeNode(n))
	}
	if IsDisabledControl(n) {
		return "", fmt.Errorf("%s is disabled", describeNode(n))
	}
	opts := SelectOptions(n)
	var enabled []*Node
	for _, o := range opts {
		if !IsDisabledControl(o) {
			enabled = append(enabled, o)
		}
	}
	if len(enabled) == 0 {
		return "", errors.New("select has no enabled options")
	}
	multi := n.HasAttr("multiple")
	var chosen []*Node
	switch {
	case len(values) > 0:
		if !multi && len(values) != 1 {
			return "", errors.New("values needs a multiple select; use value for a single select")
		}
		seen := map[*Node]bool{}
		for _, v := range values {
			o := optionByValue(enabled, v)
			if o == nil {
				return "", fmt.Errorf("no option with value %q (options: %s)", v, optionValueList(enabled))
			}
			if !seen[o] {
				seen[o] = true
				chosen = append(chosen, o)
			}
		}
	case index != nil:
		if *index < 0 || *index >= len(opts) {
			return "", fmt.Errorf("option index %d out of range (0-%d)", *index, len(opts)-1)
		}
		o := opts[*index]
		if IsDisabledControl(o) {
			return "", errors.New("option is disabled")
		}
		chosen = []*Node{o}
	case label != "":
		o := optionByLabel(enabled, label)
		if o == nil {
			return "", fmt.Errorf("no option for %q (options: %s)", label, optionValueList(enabled))
		}
		chosen = []*Node{o}
	case value != nil:
		o := optionByValue(enabled, *value)
		if o == nil {
			return "", fmt.Errorf("no option with value %q (options: %s)", *value, optionValueList(enabled))
		}
		chosen = []*Node{o}
	default:
		return "", errors.New("select needs value, values, text or index")
	}
	for _, o := range opts {
		RemoveAttr(o, "selected")
	}
	var picked []string
	for _, o := range chosen {
		SetAttr(o, "selected", "")
		picked = append(picked, OptionValue(o))
	}
	// A select's value is derived from its selected option attributes; drop any
	// stale direct value a script may have set.
	n.ValueSet = false
	n.Value = ""
	b.fireSelectChange(n)
	return fmt.Sprintf("selected %s = %s", describeNode(n), quotedList(picked)), nil
}

func optionByValue(opts []*Node, value string) *Node {
	for _, o := range opts {
		if OptionValue(o) == value {
			return o
		}
	}
	return nil
}

func optionByLabel(opts []*Node, label string) *Node {
	for _, o := range opts {
		if OptionLabel(o) == label {
			return o
		}
	}
	var match *Node
	for _, o := range opts {
		if strings.EqualFold(OptionLabel(o), label) {
			if match != nil {
				return match // ambiguous case-only match; take the first
			}
			match = o
		}
	}
	return match
}

func optionValueList(opts []*Node) string {
	var values []string
	for _, o := range opts {
		values = append(values, OptionValue(o))
	}
	return clipText(quotedList(values), 160)
}

// Toggle checks or unchecks a checkbox, radio, or multi-select option. Without
// an explicit state it flips the current one.
func (a *Automation) Toggle(selector string, checked *bool) (string, error) {
	if a == nil || a.b == nil {
		return "", errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.Page == nil || b.Page.Root == nil {
		return "", errors.New("no page loaded")
	}
	n, err := findSelectorNode(b.Page.Root, selector)
	if err != nil {
		return "", err
	}
	if IsDisabledControl(n) {
		return "", fmt.Errorf("%s is disabled", describeNode(n))
	}
	if n.Tag == "option" {
		sel := nearestSelect(n)
		if sel == nil || !sel.HasAttr("multiple") {
			return "", errors.New("use select for <select> options")
		}
		current := n.HasAttr("selected")
		want := !current
		if checked != nil {
			want = *checked
		}
		if current == want {
			return toggleState(n, want), nil
		}
		if want {
			SetAttr(n, "selected", "")
		} else {
			RemoveAttr(n, "selected")
		}
		b.fireSelectChange(sel)
		return toggleState(n, want), nil
	}
	if n.Tag != "input" {
		return "", fmt.Errorf("%s is not a checkbox, radio or option", describeNode(n))
	}
	typ := strings.ToLower(strings.TrimSpace(n.Attr("type")))
	if typ != "checkbox" && typ != "radio" {
		return "", fmt.Errorf("%s is not a checkbox or radio; use select for <select>", describeNode(n))
	}
	current := n.HasAttr("checked")
	want := !current
	if checked != nil {
		want = *checked
	}
	if typ == "radio" && !want {
		if current {
			return "", errors.New("a radio cannot be unchecked; choose another radio instead")
		}
		return toggleState(n, false), nil
	}
	if current != want {
		b.toggleControl(n)
	}
	return toggleState(n, want), nil
}

func toggleState(n *Node, checked bool) string {
	state := "unchecked"
	if checked {
		state = "checked"
	}
	return fmt.Sprintf("%s is %s", describeNode(n), state)
}

// Submit resolves a form, runs constraint validation and submit handlers, and
// waits for the navigation the submission starts. Without a selector it uses
// the focused control's form or the page's only form.
func (a *Automation) Submit(ctx context.Context, selector string) error {
	if a == nil || a.b == nil {
		return errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.Page == nil || b.Page.Root == nil {
		return errors.New("no page loaded")
	}
	var form *Node
	if selector == "" {
		if b.Forms.InputFocus != nil {
			form = NearestForm(b.Forms.InputFocus)
		}
		if form == nil {
			forms := b.Page.Root.FindAll(func(n *Node) bool { return n.Tag == "form" })
			switch len(forms) {
			case 0:
				return errors.New("no form on the page")
			case 1:
				form = forms[0]
			default:
				return fmt.Errorf("submit needs a selector (%d forms on the page)", len(forms))
			}
		}
	} else {
		n, err := findSelectorNode(b.Page.Root, selector)
		if err != nil {
			return err
		}
		if n.Tag == "form" {
			form = n
		} else {
			form = NearestForm(n)
		}
		if form == nil {
			return fmt.Errorf("%s is not inside a form", describeNode(n))
		}
	}
	started, reason := b.submitForm(form)
	if !started {
		if reason == "" {
			reason = "form was not submitted"
		}
		return errors.New(reason)
	}
	return a.waitPendingLocked(ctx)
}
