package engine

import "strings"

// ToggleAction tells the session what a control activation needs.
type ToggleAction uint8

const (
	ToggleNone ToggleAction = iota
	ToggleValue
	ToggleOpenSelect
)

// ToggleResult is the state change produced by activating a control.
type ToggleResult struct {
	Action ToggleAction
	// Select is set for an option toggle so the caller can fire input/change
	// on its select element.
	Select *Node
}

// ToggleControl applies the state change for a checkbox, radio, option or
// select. The caller owns menus, script events and repainting.
func ToggleControl(n *Node) ToggleResult {
	if n == nil || IsDisabledControl(n) {
		return ToggleResult{}
	}
	if n.Tag == "option" {
		sel := NearestSelect(n)
		if sel == nil || !sel.HasAttr("multiple") || n.HasAttr("disabled") {
			return ToggleResult{}
		}
		if n.HasAttr("selected") {
			RemoveAttr(n, "selected")
		} else {
			SetAttr(n, "selected", "")
		}
		return ToggleResult{Action: ToggleValue, Select: sel}
	}
	if n.Tag == "select" {
		if n.HasAttr("multiple") {
			return ToggleResult{}
		}
		return ToggleResult{Action: ToggleOpenSelect}
	}
	typ := strings.ToLower(n.Attr("type"))
	if typ == "radio" {
		name := n.Attr("name")
		// Radio groups are scoped to their form; controls outside any form share
		// the whole document as their group.
		scope := NearestForm(n)
		if scope == nil {
			for scope = n; scope.Parent != nil; scope = scope.Parent {
			}
		}
		if name != "" {
			scope.FindAll(func(x *Node) bool {
				if x != n && x.Tag == "input" && strings.EqualFold(x.Attr("type"), "radio") && x.Attr("name") == name {
					RemoveAttr(x, "checked")
				}
				return false
			})
		}
		SetAttr(n, "checked", "")
	} else if n.HasAttr("checked") {
		RemoveAttr(n, "checked")
	} else {
		SetAttr(n, "checked", "")
	}
	return ToggleResult{Action: ToggleValue}
}
