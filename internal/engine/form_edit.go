package engine

import (
	"strings"
	"unicode/utf8"
)

// IsTabControl reports whether n participates in the standard Tab order.
func IsTabControl(n *Node) bool {
	if n == nil || n.Type != ElementNode || IsDisabledControl(n) {
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

func TabControlStatus(n *Node) string {
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

// ControlUsesArrows reports whether the focused control reacts to arrow keys,
// so h/j/k/l can stand in for them without stealing page navigation keys.
func ControlUsesArrows(n *Node) bool {
	if n == nil {
		return false
	}
	if IsRangeInput(n) || n.Tag == "select" {
		return true
	}
	return n.Tag == "input" && strings.EqualFold(strings.TrimSpace(n.Attr("type")), "radio")
}

func RadioGroupNodes(n *Node) []*Node {
	if n == nil || n.Tag != "input" || n.Attr("name") == "" {
		return []*Node{n}
	}
	scope := NearestForm(n)
	if scope == nil {
		for scope = n; scope.Parent != nil; scope = scope.Parent {
		}
	}
	name := n.Attr("name")
	var out []*Node
	scope.FindAll(func(x *Node) bool {
		if x.Tag == "input" && strings.EqualFold(strings.TrimSpace(x.Attr("type")), "radio") &&
			x.Attr("name") == name && !IsDisabledControl(x) && x.Style != nil && x.Style.Display != "none" {
			out = append(out, x)
		}
		return false
	})
	if len(out) == 0 {
		return []*Node{n}
	}
	return out
}

func FirstSelectOption(sel *Node) *Node {
	opts := SelectOptions(sel)
	for _, opt := range opts {
		if opt.HasAttr("selected") && !IsDisabledControl(opt) && opt.Style != nil && opt.Style.Display != "none" {
			return opt
		}
	}
	for _, opt := range opts {
		if !IsDisabledControl(opt) && opt.Style != nil && opt.Style.Display != "none" {
			return opt
		}
	}
	return nil
}

func IsSubmitControl(n *Node) bool {
	if n == nil || n.Type != ElementNode || IsDisabledControl(n) {
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

func IsToggleControl(n *Node) bool {
	if n == nil || n.Type != ElementNode || IsDisabledControl(n) {
		return false
	}
	if n.Tag == "option" {
		sel := NearestSelect(n)
		return sel != nil && sel.HasAttr("multiple") && !IsDisabledControl(sel)
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

func NearestSelect(n *Node) *Node {
	for p := n; p != nil; p = p.Parent {
		if p.Type == ElementNode && p.Tag == "select" {
			return p
		}
	}
	return nil
}

// IsLabelable reports controls that a <label> can activate.
func IsLabelable(n *Node) bool {
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

// FindElementByID returns the first element with the given id below root.
func FindElementByID(root *Node, id string) *Node {
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

// LabelControl returns the labelable control associated with a label: its for
// target, or the first labelable descendant for a wrapping label.
func LabelControl(root, label *Node) *Node {
	if label == nil || label.Tag != "label" {
		return nil
	}
	if id := strings.TrimSpace(label.Attr("for")); id != "" {
		if c := FindElementByID(root, id); IsLabelable(c) {
			return c
		}
		return nil
	}
	var found *Node
	label.FindAll(func(n *Node) bool {
		if found == nil && IsLabelable(n) {
			found = n
		}
		return false
	})
	return found
}

// InputAtMaxLength reports whether the control has reached its maxlength.
func InputAtMaxLength(n *Node, value string) bool {
	limit := MaxLength(n)
	return limit > 0 && utf8.RuneCountInString(value) >= limit
}

// InputLineStart returns the byte offset of the start of the caret's line.
func InputLineStart(s string, cur int) int {
	if cur > len(s) {
		cur = len(s)
	}
	if i := strings.LastIndexByte(s[:cur], '\n'); i >= 0 {
		return i + 1
	}
	return 0
}

// InputLineEnd returns the byte offset of the end of the caret's line.
func InputLineEnd(s string, cur int) int {
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

// TextareaVerticalOffset moves the caret one logical line up or down, keeping
// its character column where the target line is long enough.
func TextareaVerticalOffset(s string, cur, delta int) int {
	if cur < 0 {
		cur = 0
	}
	if cur > len(s) {
		cur = len(s)
	}
	start := InputLineStart(s, cur)
	col := utf8.RuneCountInString(s[start:cur])
	line := start
	if delta < 0 {
		if start == 0 {
			return cur
		}
		line = InputLineStart(s, start-1)
	} else {
		end := InputLineEnd(s, cur)
		if end == len(s) {
			return cur
		}
		line = end + 1
	}
	end := InputLineEnd(s, line)
	runes := []rune(s[line:end])
	if col > len(runes) {
		col = len(runes)
	}
	return line + len(string(runes[:col]))
}
