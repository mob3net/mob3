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

func TestFormStateFocusInputTransitions(t *testing.T) {
	a := &Node{Tag: "input", Value: "abc", ValueSet: true}
	b := &Node{Tag: "input"}
	s := FormState{}
	if old := s.FocusInput(a); old != nil {
		t.Fatal("first focus returned an old node")
	}
	if !a.InputFocused || a.InputCursor != 3 || s.InputFocus != a {
		t.Fatalf("focus state = %+v cursor=%d", s, a.InputCursor)
	}
	if old := s.FocusInput(b); old != a || a.InputFocused || a.InputCursor != 0 || !b.InputFocused {
		t.Fatalf("transition = %v a=%+v b=%+v", old, a, b)
	}
	if old := s.ClearInputFocus(); old != b || b.InputFocused || s.InputFocus != nil {
		t.Fatalf("clear = %v b=%+v", old, b)
	}
}
