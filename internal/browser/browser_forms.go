package browser

import "browser/internal/engine"

// Form editing lives on engine.Session; these wrappers keep the browser call
// sites and tests stable during the migration.

type tabTarget = engine.TabTarget

func (b *Browser) tabTargets() []tabTarget { return b.Session.TabTargets() }
func (b *Browser) currentTabNode() *Node   { return b.Session.CurrentTabNode() }
func (b *Browser) tabStep(delta int)       { b.Session.TabStep(delta, b) }
func (b *Browser) setTabFocus(t tabTarget, index, total int) {
	b.Session.SetTabFocus(t, index, total, b)
}
func (b *Browser) clearControlFocus() { b.Session.Forms.ClearControl() }
func (b *Browser) handleControlKey(k string) bool {
	return b.Session.HandleControlKey(k, b)
}
func (b *Browser) activateControlFocus() { b.Session.ActivateControlFocus(b) }
func (b *Browser) controlStep(dir int)   { b.Session.ControlStep(dir, b) }
func (b *Browser) stepRadio(n *Node, dir int) {
	b.Session.StepRadio(n, dir, b)
}
func (b *Browser) stepMultiSelect(sel *Node, dir int) {
	b.Session.StepMultiSelect(sel, dir, b)
}
func (b *Browser) stepSingleSelect(sel *Node, dir int) {
	b.Session.StepSingleSelect(sel, dir, b)
}
func (b *Browser) ensureFocusVisible() { b.Session.EnsureFocusVisible(b) }
func (b *Browser) toggleControl(n *Node) {
	b.Session.ToggleControl(n, b)
}
func (b *Browser) fireSelectChange(sel *Node) { b.Session.FireSelectChange(sel, b) }
func (b *Browser) clearInputFocus()           { b.Session.Forms.ClearInputFocus() }
func (b *Browser) setInputFocus(n *Node)      { b.Session.SetInputFocus(n, b) }
func (b *Browser) ensureInputVisible(n *Node) { b.Session.EnsureInputVisible(n, b) }
func (b *Browser) setInputValue(n *Node, v string) {
	b.Session.SetInputValue(n, v, b)
}
func (b *Browser) submitNode(n *Node) { b.Session.SubmitNode(n, b) }
func (b *Browser) validateForm(form *Node) (*Node, string) {
	return engine.ValidateForm(form)
}
func (b *Browser) submitForm(form *Node) (bool, string) { return b.Session.SubmitForm(form, b) }
func (b *Browser) queueFormSubmit(form *Node)           { b.Session.QueueFormSubmit(form, b) }
func (b *Browser) resolveLabelTarget(n *Node) *Node     { return b.Session.ResolveLabelTarget(n) }
func (b *Browser) handleInputKey(k string) bool         { return b.Session.HandleInputKey(k, b) }
func (b *Browser) handleRangeKey(n *Node, k string) bool {
	return b.Session.HandleRangeKey(n, k, b)
}
func (b *Browser) adjustRange(n *Node, direction float64) {
	b.Session.AdjustRange(n, direction, b)
}
func (b *Browser) setRangeValue(n *Node, value float64) {
	b.Session.SetRangeValue(n, value, b)
}
func (b *Browser) refreshInputCursor(n *Node) { b.Session.RefreshInputCursor(n, b) }
func (b *Browser) updateInputFragments(n *Node) bool {
	return b.Session.UpdateInputFragments(n)
}
func (b *Browser) rangeSideAt(n *Node, x, y int) int { return b.Session.RangeSideAt(n, x, y) }
func (b *Browser) formRequest(form *Node) (string, string, []byte, string, bool) {
	return b.Session.FormRequest(form, b)
}

// paintControlFocus highlights the focused non-text control, or the active
// option inside a multi-select list box.
func (b *Browser) paintControlFocus(g *Grid, top, bottom int) {
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

func isTabControl(n *Node) bool                   { return engine.IsTabControl(n) }
func tabControlStatus(n *Node) string             { return engine.TabControlStatus(n) }
func controlUsesArrows(n *Node) bool              { return engine.ControlUsesArrows(n) }
func radioGroupNodes(n *Node) []*Node             { return engine.RadioGroupNodes(n) }
func firstSelectOption(n *Node) *Node             { return engine.FirstSelectOption(n) }
func isSubmitControl(n *Node) bool                { return engine.IsSubmitControl(n) }
func isToggleControl(n *Node) bool                { return engine.IsToggleControl(n) }
func nearestSelect(n *Node) *Node                 { return engine.NearestSelect(n) }
func isLabelable(n *Node) bool                    { return engine.IsLabelable(n) }
func findElementByID(root *Node, id string) *Node { return engine.FindElementByID(root, id) }
func labelControl(root, label *Node) *Node        { return engine.LabelControl(root, label) }
func inputAtMaxLength(n *Node, value string) bool { return engine.InputAtMaxLength(n, value) }
func inputLineStart(s string, cur int) int        { return engine.InputLineStart(s, cur) }
func inputLineEnd(s string, cur int) int          { return engine.InputLineEnd(s, cur) }
func textareaVerticalOffset(s string, cur, delta int) int {
	return engine.TextareaVerticalOffset(s, cur, delta)
}
