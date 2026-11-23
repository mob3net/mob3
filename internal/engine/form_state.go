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

// ClearControl drops non-text control focus.
func (s *FormState) ClearControl() {
	s.ControlFocus = nil
	s.ControlOption = nil
}
