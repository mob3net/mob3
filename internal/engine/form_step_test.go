package engine

import "testing"

func TestFormStepping(t *testing.T) {
	doc := ParseHTML(`<html><body><form>
		<input type="radio" name="r" value="a">
		<input type="radio" name="r" value="b">
		<select name="s"><option value="1">1</option><option value="2" selected>2</option><option value="3">3</option></select>
		<select name="m" multiple><option value="x">x</option><option value="y">y</option></select>
	</form></body></html>`)
	radios := doc.FindAll(func(n *Node) bool { return n.Tag == "input" })
	for _, n := range radios {
		n.Style = &Style{}
	}
	state := FormState{ControlFocus: radios[0]}
	if next := StepRadio(&state, radios[0], 1); next != radios[1] || state.ControlFocus != radios[1] {
		t.Fatalf("radio step = %+v state=%+v", next, state.ControlFocus)
	}

	selects := doc.FindAll(func(n *Node) bool { return n.Tag == "select" })
	for _, n := range selects {
		n.Style = &Style{}
		for _, opt := range SelectOptions(n) {
			opt.Style = &Style{}
		}
	}
	if next := StepSingleSelect(selects[0], 1); next == nil || !next.HasAttr("selected") || next.Attr("value") != "3" {
		t.Fatalf("single select = %+v", next)
	}
	multi := selects[1]
	state.ControlOption = SelectOptions(multi)[0]
	if next := StepMultiSelect(&state, multi, 1); next == nil || state.ControlOption != next || next.Attr("value") != "y" {
		t.Fatalf("multi select = %+v state=%+v", next, state.ControlOption)
	}
}
