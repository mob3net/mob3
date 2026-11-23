package engine

import "testing"

func TestFormStateCurrentAndClear(t *testing.T) {
	input := &Node{Tag: "input"}
	selectNode := &Node{Tag: "select"}
	opt := &Node{Tag: "option"}
	s := FormState{InputFocus: input, InputSelectAll: true, ControlFocus: selectNode, ControlOption: opt}
	if s.Current() != input {
		t.Fatal("input focus did not win")
	}
	s.ClearInput()
	if s.Current() != selectNode || s.InputSelectAll {
		t.Fatalf("after input clear: %+v", s)
	}
	s.ClearControl()
	if s.Current() != nil || s.ControlOption != nil {
		t.Fatalf("after control clear: %+v", s)
	}
}
