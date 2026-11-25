package engine

import (
	"net/url"
	"strings"
	"unicode/utf8"

	"browser/internal/textwidth"
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

func UpdateInputFragments(layout *Layout, n *Node) bool {
	if layout == nil || n == nil || !IsTextInput(n) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(n.Attr("type")), "color") {
		// The color field's own background is the live swatch, so it needs a
		// full paint whenever the value changes.
		return false
	}
	var frags []*Frag
	for i := range layout.Frags {
		f := &layout.Frags[i]
		if f.Kind == FragText && f.Node == n {
			frags = append(frags, f)
		}
	}
	if len(frags) == 0 {
		return false
	}
	if n.Tag == "textarea" {
		boxW := frags[0].W
		rows, cursorRow, cursorCol, hasCursor := TextareaLayoutFor(n, boxW)
		if len(rows) != len(frags) {
			return false
		}
		for i := range rows {
			if textwidth.StringWidth(rows[i]) != frags[i].W {
				return false
			}
			frags[i].Text = rows[i]
		}
		if hasCursor {
			layout.Cursor = Rect{X: frags[cursorRow].X + 2 + cursorCol, Y: frags[cursorRow].Y, W: 1, H: 1}
			layout.HasCursor = true
		} else {
			layout.HasCursor = false
		}
		return true
	}
	label := FormLabel(n)
	if label == "" || len(frags) != 1 {
		return false
	}
	w := textwidth.StringWidth(label)
	boxW := frags[0].W
	if w != boxW {
		// A fixed-width or flex-stretched control keeps the same box while the
		// value changes; rewrite the label inside it instead of rebuilding the
		// whole page. Auto-width controls must relayout when their box grows or
		// shrinks.
		fixed := false
		if st := n.Style; st != nil {
			fixed = st.FlexGrow > 0 || (st.Width.Set && !st.Width.IsAuto())
		}
		if !fixed {
			return false
		}
		if w > boxW {
			label = textwidth.Truncate(label, boxW)
		} else if pad := boxW - w; pad > 0 {
			if strings.HasSuffix(label, "]") {
				label = strings.TrimSuffix(label, "]") + strings.Repeat(" ", pad) + "]"
			} else {
				label += strings.Repeat(" ", pad)
			}
		}
	}
	frags[0].Text = label
	if col, ok := InputCursorOffset(n, label); ok {
		layout.Cursor = Rect{X: frags[0].X + 2 + col, Y: frags[0].Y, W: 1, H: 1}
		layout.HasCursor = true
	} else {
		layout.HasCursor = false
	}
	return true
}

func RangeSideAt(layout *Layout, n *Node, x, y int) int {
	if layout == nil || n == nil {
		return 0
	}
	for i := range layout.Frags {
		f := &layout.Frags[i]
		if f.Kind != FragText || f.Node != n || y != f.Y {
			continue
		}
		rel := x - f.X
		if i := strings.Index(f.Text, "-"); i >= 0 && rel == textwidth.StringWidth(f.Text[:i]) {
			return -1
		}
		if i := strings.LastIndex(f.Text, "+"); i >= 0 && rel == textwidth.StringWidth(f.Text[:i]) {
			return 1
		}
	}
	return 0
}

// FormRequest resolves a form's action and collects its successful controls.
func FormRequest(form *Node, base *url.URL) (method, target string, body []byte, contentType string, ok bool) {
	method = strings.ToUpper(strings.TrimSpace(form.Attr("method")))
	if method == "" {
		method = "GET"
	}
	action := strings.TrimSpace(form.Attr("action"))
	var u *url.URL
	var err error
	if action == "" {
		u = base
	} else if base != nil {
		u, err = base.Parse(action)
	}
	if err != nil || u == nil {
		return "", "", nil, "", false
	}
	vals := url.Values{}
	form.FindAll(func(x *Node) bool {
		if x.Tag != "input" && x.Tag != "textarea" && x.Tag != "select" {
			return false
		}
		if IsDisabledControl(x) {
			return false
		}
		name := x.Attr("name")
		if name == "" {
			return false
		}
		if x.Tag == "select" {
			if x.HasAttr("multiple") {
				for _, v := range SelectedOptionValues(x) {
					vals.Add(name, v)
				}
			} else {
				vals.Add(name, InputValue(x))
			}
			return false
		}
		typ := strings.ToLower(x.Attr("type"))
		switch typ {
		case "submit", "button", "reset", "image", "file":
			return false
		case "checkbox", "radio":
			if x.HasAttr("checked") {
				v := x.Attr("value")
				if v == "" {
					v = "on"
				}
				vals.Add(name, v)
			}
		default:
			vals.Add(name, InputValue(x))
		}
		return false
	})
	if method == "POST" {
		return method, u.String(), []byte(vals.Encode()), "application/x-www-form-urlencoded", true
	}
	q := u.Query()
	for k, values := range vals {
		q[k] = values
	}
	u.RawQuery = q.Encode()
	return "GET", u.String(), nil, "", true
}
