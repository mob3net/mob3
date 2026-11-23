package engine

// Form editing lives on Session; these wrappers keep the browser call
// sites and tests stable during the migration.

func (b *Session) currentTabNode() *Node { return b.CurrentTabNode() }
func (b *Session) tabStep(delta int)     { b.TabStep(delta, b) }
func (b *Session) setTabFocus(t TabTarget, index, total int) {
	b.SetTabFocus(t, index, total, b)
}
func (b *Session) clearControlFocus() { b.Forms.ClearControl() }
func (b *Session) handleControlKey(k string) bool {
	return b.HandleControlKey(k, b)
}
func (b *Session) activateControlFocus() { b.ActivateControlFocus(b) }
func (b *Session) controlStep(dir int)   { b.ControlStep(dir, b) }
func (b *Session) stepRadio(n *Node, dir int) {
	b.StepRadio(n, dir, b)
}
func (b *Session) stepMultiSelect(sel *Node, dir int) {
	b.StepMultiSelect(sel, dir, b)
}
func (b *Session) stepSingleSelect(sel *Node, dir int) {
	b.StepSingleSelect(sel, dir, b)
}
func (b *Session) ensureFocusVisible() { b.EnsureFocusVisible(b) }
func (b *Session) toggleControl(n *Node) {
	b.ToggleControl(n, b)
}
func (b *Session) fireSelectChange(sel *Node) { b.FireSelectChange(sel, b) }
func (b *Session) clearInputFocus()           { b.Forms.ClearInputFocus() }
func (b *Session) setInputFocus(n *Node)      { b.SetInputFocus(n, b) }
func (b *Session) ensureInputVisible(n *Node) { b.EnsureInputVisible(n, b) }
func (b *Session) setInputValue(n *Node, v string) {
	b.SetInputValue(n, v, b)
}
func (b *Session) submitNode(n *Node) { b.SubmitNode(n, b) }
func (b *Session) validateForm(form *Node) (*Node, string) {
	return ValidateForm(form)
}
func (b *Session) submitForm(form *Node) (bool, string) { return b.SubmitForm(form, b) }
func (b *Session) queueFormSubmit(form *Node)           { b.QueueFormSubmit(form, b) }
func (b *Session) resolveLabelTarget(n *Node) *Node     { return b.ResolveLabelTarget(n) }
func (b *Session) handleInputKey(k string) bool         { return b.HandleInputKey(k, b) }
func (b *Session) handleRangeKey(n *Node, k string) bool {
	return b.HandleRangeKey(n, k, b)
}
func (b *Session) adjustRange(n *Node, direction float64) {
	b.AdjustRange(n, direction, b)
}
func (b *Session) setRangeValue(n *Node, value float64) {
	b.SetRangeValue(n, value, b)
}
func (b *Session) refreshInputCursor(n *Node) { b.RefreshInputCursor(n, b) }
func (b *Session) updateInputFragments(n *Node) bool {
	return b.UpdateInputFragments(n)
}
func (b *Session) rangeSideAt(n *Node, x, y int) int { return b.RangeSideAt(n, x, y) }
func (b *Session) formRequest(form *Node) (string, string, []byte, string, bool) {
	return b.FormRequest(form, b)
}

// paintControlFocus highlights the focused non-text control, or the active
// option inside a multi-select list box.
func (b *Session) paintControlFocus(g *Grid, top, bottom int) {
	if b.Layout == nil || b.Forms.ControlFocus == nil {
		return
	}
	target := b.Forms.ControlFocus
	if b.Forms.ControlOption != nil {
		target = b.Forms.ControlOption
	}
	bg := RGB(0xff, 0xd7, 0x00)
	fg := RGB(0x1f, 0x23, 0x28)
	for i := range b.Layout.Frags {
		f := &b.Layout.Frags[i]
		if f.Node != target || f.Kind != fragText && f.Kind != fragImage {
			continue
		}
		y := f.Y - b.Scroll + top
		if y < top || y >= bottom {
			continue
		}
		g.Highlight(f.X, y, f.W, 1, bg, fg)
	}
}

func isTabControl(n *Node) bool                   { return IsTabControl(n) }
func tabControlStatus(n *Node) string             { return TabControlStatus(n) }
func controlUsesArrows(n *Node) bool              { return ControlUsesArrows(n) }
func radioGroupNodes(n *Node) []*Node             { return RadioGroupNodes(n) }
func firstSelectOption(n *Node) *Node             { return FirstSelectOption(n) }
func isSubmitControl(n *Node) bool                { return IsSubmitControl(n) }
func isToggleControl(n *Node) bool                { return IsToggleControl(n) }
func nearestSelect(n *Node) *Node                 { return NearestSelect(n) }
func isLabelable(n *Node) bool                    { return IsLabelable(n) }
func findElementByID(root *Node, id string) *Node { return FindElementByID(root, id) }
func labelControl(root, label *Node) *Node        { return LabelControl(root, label) }
func inputAtMaxLength(n *Node, value string) bool { return InputAtMaxLength(n, value) }
func inputLineStart(s string, cur int) int        { return InputLineStart(s, cur) }
func inputLineEnd(s string, cur int) int          { return InputLineEnd(s, cur) }
func textareaVerticalOffset(s string, cur, delta int) int {
	return TextareaVerticalOffset(s, cur, delta)
}
