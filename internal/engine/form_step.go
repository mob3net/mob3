package engine

import "strings"

// StepRadio moves control focus to the next radio in its group and returns it.
func StepRadio(state *FormState, n *Node, dir int) *Node {
	group := RadioGroupNodes(n)
	if len(group) == 0 {
		return nil
	}
	idx := 0
	for i, r := range group {
		if r == n {
			idx = i
			break
		}
	}
	next := group[(idx+dir+len(group))%len(group)]
	state.ControlFocus = next
	return next
}

// StepMultiSelect moves the active option in a multi-select and returns it.
func StepMultiSelect(state *FormState, sel *Node, dir int) *Node {
	opts := SelectOptions(sel)
	if len(opts) == 0 {
		return nil
	}
	idx := 0
	for i, opt := range opts {
		if opt == state.ControlOption {
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
	state.ControlOption = opts[idx]
	return opts[idx]
}

// StepSingleSelect moves the selected option in a single-select, updates the
// selected attributes and returns the new option.
func StepSingleSelect(sel *Node, dir int) *Node {
	opts := SelectOptions(sel)
	if len(opts) == 0 {
		return nil
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
	return opts[idx]
}

// ControlIsRadio reports whether n is a radio input.
func ControlIsRadio(n *Node) bool {
	return n != nil && n.Tag == "input" && strings.EqualFold(strings.TrimSpace(n.Attr("type")), "radio")
}
