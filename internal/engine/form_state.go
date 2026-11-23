package engine

// FormState is the page's current form focus. The browser still owns editing
// and submission; keeping the focus pointers in the engine makes them part of
// the page model rather than shell state.
type FormState struct {
	// InputFocus is the focused text, textarea, range or file control.
	InputFocus *Node
	// InputSelectAll marks the whole value as selected by Ctrl-A.
	InputSelectAll bool
	// ControlFocus is the focused select, checkbox, radio or button.
	ControlFocus *Node
	// ControlOption is the active option inside ControlFocus, when any.
	ControlOption *Node
}

// Current returns the focused node in input-then-control order.
func (s FormState) Current() *Node {
	if s.InputFocus != nil {
		return s.InputFocus
	}
	return s.ControlFocus
}

// ClearInput drops text-input focus and any selection.
func (s *FormState) ClearInput() {
	s.InputFocus = nil
	s.InputSelectAll = false
}

// ClearInputFocus clears the focused text control, resetting its cursor and
// focus marker, and returns the previous node.
func (s *FormState) ClearInputFocus() *Node {
	s.InputSelectAll = false
	old := s.InputFocus
	if old != nil {
		old.InputFocused = false
		old.InputCursor = 0
		s.InputFocus = nil
	}
	return old
}

// FocusInput makes n the focused text control, resetting selection and cursor.
// It returns the previous focused node, if any.
func (s *FormState) FocusInput(n *Node) *Node {
	if s.InputFocus == n {
		return nil
	}
	s.InputSelectAll = false
	old := s.InputFocus
	if old != nil {
		old.InputFocused = false
		old.InputCursor = 0
	}
	s.InputFocus = n
	if n != nil {
		n.InputFocused = true
		n.InputCursor = len(InputValue(n))
	}
	return old
}

// ClearControl drops non-text control focus.
func (s *FormState) ClearControl() {
	s.ControlFocus = nil
	s.ControlOption = nil
}
