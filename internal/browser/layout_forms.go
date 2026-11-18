package browser

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Form controls use single-cell Unicode glyphs so a checkbox plus its label
// reads like a form rather than a run of ASCII brackets.
const (
	glyphCheckboxOn  = "☑"
	glyphCheckboxOff = "☐"
	glyphRadioOn     = "◉"
	glyphRadioOff    = "○"
)

func isFormControl(n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	switch n.Tag {
	case "input", "select", "textarea", "button":
		return true
	}
	return false
}

// isDisabledControl reports a control that is disabled itself or belongs to a
// disabled fieldset.
func isDisabledControl(n *Node) bool {
	if n == nil {
		return false
	}
	for p := n; p != nil; p = p.Parent {
		if p.Type != ElementNode {
			continue
		}
		if (p.Tag == "input" || p.Tag == "select" || p.Tag == "textarea" || p.Tag == "button" || p.Tag == "option") && p.HasAttr("disabled") {
			return true
		}
		if p.Tag == "fieldset" && p.HasAttr("disabled") {
			return true
		}
	}
	return false
}

// isReadOnlyControl reports a text-like control that is not editable.
func isReadOnlyControl(n *Node) bool {
	if n == nil {
		return false
	}
	if n.Tag == "textarea" {
		return n.HasAttr("readonly")
	}
	if n.Tag != "input" {
		return false
	}
	switch strings.ToLower(n.Attr("type")) {
	case "", "text", "search", "email", "url", "tel", "number", "password",
		"date", "datetime-local", "month", "week", "time", "color":
		return n.HasAttr("readonly")
	}
	return false
}

// maxLength returns the maxlength limit in runes, or 0 when unrestricted.
func maxLength(n *Node) int {
	if n == nil {
		return 0
	}
	limit, err := strconv.Atoi(strings.TrimSpace(n.Attr("maxlength")))
	if err != nil || limit < 0 {
		return 0
	}
	return limit
}

func (c *inlineCtx) addLabel(label string, st *Style, node *Node) {
	if label == "" {
		return
	}
	maxW := c.lineMaxW()
	if w := stringWidth(label); w > maxW && maxW > 0 {
		label = truncateWidth(label, maxW)
	}
	w := stringWidth(label)
	if w < 1 {
		w = 1
	}
	c.addAtomicBox(inlineItem{
		kind: itemAtomic, w: w, h: 1, style: st, node: node,
		paint: func(x, y int) {
			c.l.addText(x, y, label, st, node)
		},
	})
}

// addLabelBox reserves the image's layout box and paints the label on its
// first row, so a disabled or broken image keeps the space it would occupy.
func (c *inlineCtx) addLabelBox(label string, st *Style, node *Node, w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	c.addAtomicBox(inlineItem{
		kind: itemAtomic, w: w, h: h, style: st, node: node,
		paint: func(x, y int) {
			c.l.addText(x, y, label, st, node)
		},
	})
}

func (c *inlineCtx) addForm(n *Node, st *Style) {
	if n.Tag == "textarea" {
		c.addTextarea(n, st)
		return
	}
	if n.Tag == "select" && n.HasAttr("multiple") {
		c.addMultiSelect(n, st)
		return
	}
	label := formLabel(n)
	if label == "" {
		return
	}
	maxW := c.lineMaxW()
	if w := stringWidth(label); w > maxW && maxW > 0 {
		label = truncateWidth(label, maxW)
	}
	w := stringWidth(label)
	if w < 1 {
		w = 1
	}
	editable := isTextInput(n)
	// A text-like control stretches its widget to an explicit CSS width, or to
	// the width allocated by a flex row (used by the home search box).
	if editable {
		target := 0
		switch {
		case st.FlexGrow > 0:
			target = maxW
		case st.Width.Set && !st.Width.IsAuto():
			target = st.Width.resolveH(maxW, st.FontSize)
		}
		if target > w {
			w = minInt(target, maxW)
			if strings.HasSuffix(label, "]") {
				pad := w - stringWidth(label)
				label = label[:len(label)-1] + strings.Repeat(" ", pad) + "]"
			}
		}
	}
	disabled := isDisabledControl(n)
	readOnly := isReadOnlyControl(n)
	c.addAtomicBox(inlineItem{
		kind: itemAtomic, w: w, h: 1, style: st, node: n,
		paint: func(x, y int) {
			bg := RGB(0xea, 0xee, 0xf2)
			fg := RGB(0x1f, 0x23, 0x28)
			paintStyle := st
			cl := *st
			cl.Bold = true
			switch {
			case disabled:
				bg = RGB(0xdc, 0xe0, 0xe4)
				fg = RGB(0x8a, 0x93, 0x9d)
			case readOnly:
				bg = RGB(0xf1, 0xf3, 0xf5)
				fg = RGB(0x57, 0x60, 0x6a)
			case n.InputFocused && isRangeInput(n):
				bg = RGB(0x2f, 0x6f, 0xbf)
				fg = RGB(0xff, 0xff, 0xff)
			case editable:
				if n.InputFocused {
					bg = RGB(0x2f, 0x6f, 0xbf)
					fg = RGB(0xff, 0xff, 0xff)
				} else {
					bg = RGB(0xd8, 0xe7, 0xf8)
					fg = RGB(0x1f, 0x23, 0x28)
				}
			}
			if !disabled && !readOnly && strings.EqualFold(n.Attr("type"), "color") {
				if swatch, ok := parseColor(inputValue(n), Color{}); ok && swatch.Set && swatch.A > 0 {
					bg = swatch
					fg = readableFG(RGB(0x1f, 0x23, 0x28), bg)
					if n.InputFocused {
						cl.Bold = true
					}
				}
			}
			cl.Color = fg
			cl.Background = bg
			paintStyle = &cl
			c.l.addFill(x, y, w, 1, bg)
			c.l.addText(x, y, label, paintStyle, n)
			if col, ok := inputCursorOffset(n, label); ok {
				c.l.setInputCursor(x+2+col, y)
			}
		},
	})
}

// addMultiSelect renders a <select multiple> as an always-visible list box.
// Every option occupies its own row and keeps the option node, so a click can
// toggle precisely that option rather than cycling the whole control.
func (c *inlineCtx) addMultiSelect(n *Node, st *Style) {
	opts := selectOptions(n)
	if len(opts) == 0 {
		c.addLabel("[ multi-select ]", st, n)
		return
	}
	labels := make([]string, len(opts))
	boxW := 0
	for i, opt := range opts {
		mark := glyphCheckboxOff + " "
		if opt.HasAttr("selected") {
			mark = glyphCheckboxOn + " "
		}
		labels[i] = mark + optionLabel(opt)
		if w := stringWidth(labels[i]); w > boxW {
			boxW = w
		}
	}
	if c.maxW > 0 && boxW > c.maxW {
		boxW = c.maxW
	}
	if boxW < 10 {
		boxW = 10
		if c.maxW > 0 && boxW > c.maxW {
			boxW = c.maxW
		}
	}
	if boxW < 1 {
		boxW = 1
	}
	for i := range labels {
		labels[i] = truncateWidth(labels[i], boxW)
	}
	c.addAtomicBox(inlineItem{
		kind: itemAtomic, w: boxW, h: len(opts), style: st, node: n,
		paint: func(x, y int) {
			for i, opt := range opts {
				bg := RGB(0xea, 0xee, 0xf2)
				fg := RGB(0x1f, 0x23, 0x28)
				rowStyle := st
				if opt.HasAttr("selected") {
					bg = RGB(0xd8, 0xe7, 0xf8)
					cl := *st
					cl.Color = fg
					rowStyle = &cl
				}
				c.l.addFill(x, y+i, boxW, 1, bg)
				c.l.addText(x, y+i, labels[i], rowStyle, opt)
			}
		},
	})
}

// addTextarea renders a textarea as a multi-row editable widget. Its first row
// uses the same bracketed style as a text input; the extra rows make the
// control visibly taller than a single-line field.
func (c *inlineCtx) addTextarea(n *Node, st *Style) {
	label := formLabel(n)
	if label == "" {
		return
	}
	boxW := stringWidth(label)
	if c.maxW > 0 && boxW > c.maxW {
		boxW = c.maxW
	}
	if boxW < 8 {
		boxW = 8
	}
	rows, cursorRow, cursorCol, hasCursor := textareaLayoutFor(n, boxW)
	editable := isTextInput(n)
	c.addAtomicBox(inlineItem{
		kind: itemAtomic, w: boxW, h: len(rows), style: st, node: n,
		paint: func(x, y int) {
			bg := RGB(0xea, 0xee, 0xf2)
			paintStyle := st
			if editable {
				cl := *st
				cl.Bold = true
				if n.InputFocused {
					bg = RGB(0x2f, 0x6f, 0xbf)
					cl.Color = RGB(0xff, 0xff, 0xff)
				} else {
					bg = RGB(0xd8, 0xe7, 0xf8)
					cl.Color = RGB(0x1f, 0x23, 0x28)
				}
				cl.Background = bg
				paintStyle = &cl
			}
			for i, row := range rows {
				c.l.addFill(x, y+i, boxW, 1, bg)
				c.l.addText(x, y+i, row, paintStyle, n)
			}
			if hasCursor && n.InputFocused {
				c.l.setInputCursor(x+2+cursorCol, y+cursorRow)
			}
		},
	})
}

// textareaLayoutFor formats the visible rows of a textarea and the focused
// caret cell within the box.
func textareaLayoutFor(n *Node, boxW int) (rows []string, cursorRow, cursorCol int, hasCursor bool) {
	inner := boxW - 3
	if inner < 1 {
		inner = 1
	}
	lines, cursorRow, cursorCol, hasCursor := textareaLinesCursor(n, inner)
	h := len(lines)
	if rows := textareaRows(n); rows > h {
		h = rows
	}
	if h < 2 {
		h = 2
	}
	if h > 12 {
		h = 12
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	if cursorRow >= h {
		hasCursor = false
	}
	out := make([]string, h)
	for i, line := range lines {
		out[i] = "[ " + padWidth(truncateWidth(line, inner), inner) + "]"
	}
	if cursorCol >= inner {
		cursorCol = inner - 1
	}
	if cursorCol < 0 {
		cursorCol = 0
	}
	return out, cursorRow, cursorCol, hasCursor
}

// textareaLines splits a textarea value into display rows, wrapping long lines
// to width. Only the rows that can be displayed are produced, so typing into a
// large textarea stays cheap.
func textareaLines(n *Node, width int) []string {
	lines, _, _, _ := textareaLinesCursor(n, width)
	return lines
}

// textareaLinesCursor is textareaLines plus the caret's row and cell column.
func textareaLinesCursor(n *Node, width int) (out []string, cursorRow, cursorCol int, hasCursor bool) {
	txt := inputValue(n)
	cur := n.InputCursor
	hasCursor = n.InputFocused
	if txt == "" && !n.InputFocused {
		txt = n.Attr("placeholder")
		hasCursor = false
	}
	txt, cur = normalizeTextCursor(txt, cur)
	if width < 1 {
		width = 1
	}
	// The widget never shows more than 12 rows; stop scanning beyond that.
	txt = truncateWidth(txt, width*12+12)
	if hasCursor {
		if cur > len(txt) {
			hasCursor = false
		} else {
			cur = len(expandTabs(txt[:cur]))
		}
	}
	txt = expandTabs(txt)
	var b strings.Builder
	w := 0
	row := 0
	found := false
	flush := func() {
		out = append(out, b.String())
		b.Reset()
		w = 0
		row = len(out)
	}
	for i, r := range txt {
		if len(out) >= 12 {
			break
		}
		atCursor := hasCursor && !found && i == cur
		if atCursor {
			cursorRow, cursorCol = row, w
			found = true
		}
		if r == '\n' {
			flush()
			continue
		}
		rw := runeWidth(r)
		if rw == 0 {
			b.WriteRune(r)
			continue
		}
		if w+rw > width && w > 0 {
			flush()
			if atCursor {
				cursorRow, cursorCol = row, w
			}
		}
		b.WriteRune(r)
		w += rw
	}
	if len(out) < 12 {
		out = append(out, b.String())
	}
	if hasCursor && !found {
		if cur <= len(txt) && row < len(out) {
			cursorRow, cursorCol = row, w
			found = true
		}
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out, cursorRow, cursorCol, hasCursor && found
}

// normalizeTextCursor normalises CRLF/CR newlines while keeping the caret at
// the same logical position.
func normalizeTextCursor(s string, cur int) (string, int) {
	if cur < 0 {
		cur = 0
	}
	if cur > len(s) {
		cur = len(s)
	}
	before := strings.ReplaceAll(strings.ReplaceAll(s[:cur], "\r\n", "\n"), "\r", "\n")
	full := strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	return full, len(before)
}

// textareaRows reads the HTML rows attribute, clamped to the supported range.
func textareaRows(n *Node) int {
	rows, err := strconv.Atoi(strings.TrimSpace(n.Attr("rows")))
	if err != nil || rows < 2 {
		return 0
	}
	if rows > 12 {
		rows = 12
	}
	return rows
}

// selectOptions returns every option below a select, including those nested in
// optgroup elements.
func selectOptions(n *Node) []*Node {
	if n == nil {
		return nil
	}
	return n.FindAll(func(x *Node) bool { return x.Tag == "option" })
}

// optionLabel is the visible text of an option, falling back to its value.
func optionLabel(opt *Node) string {
	if opt == nil {
		return ""
	}
	label := strings.TrimSpace(opt.TextContent())
	if label == "" {
		label = strings.TrimSpace(opt.Attr("value"))
	}
	return label
}

// optionValue is the value submitted for an option. Options without a value
// use their visible text, matching HTML.
func optionValue(opt *Node) string {
	if opt == nil {
		return ""
	}
	if v := opt.Attr("value"); v != "" {
		return v
	}
	return strings.TrimSpace(opt.TextContent())
}

// selectedOptionValues returns the submitted values of a multi-select in
// document order.
func selectedOptionValues(n *Node) []string {
	var values []string
	for _, opt := range selectOptions(n) {
		if opt.HasAttr("selected") && !isDisabledControl(opt) {
			values = append(values, optionValue(opt))
		}
	}
	return values
}

// rangeParams resolves the min/max/step/value of a range input using the HTML
// defaults.
func rangeParams(n *Node) (min, max, step, value float64) {
	min, max, step, value = 0, 100, 1, 0
	if v, err := strconv.ParseFloat(strings.TrimSpace(n.Attr("min")), 64); err == nil {
		min = v
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(n.Attr("max")), 64); err == nil {
		max = v
	}
	if max < min {
		max = min
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(n.Attr("step")), 64); err == nil && v > 0 {
		step = v
	}
	value = min + (max-min)/2
	if n.ValueSet {
		if v, err := strconv.ParseFloat(strings.TrimSpace(n.Value), 64); err == nil {
			value = v
		}
	} else if v, err := strconv.ParseFloat(strings.TrimSpace(n.Attr("value")), 64); err == nil {
		value = v
	}
	if value < min {
		value = min
	}
	if value > max {
		value = max
	}
	return min, max, step, value
}

func formatRangeNumber(v float64) string {
	if math.Abs(v-math.Round(v)) < 1e-9 {
		return strconv.FormatInt(int64(math.Round(v)), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func rangeValueString(n *Node) string {
	_, _, _, value := rangeParams(n)
	return formatRangeNumber(value)
}

// rangeLabel draws a compact slider with the current value.
func rangeLabel(n *Node) string {
	min, max, _, value := rangeParams(n)
	const barW = 16
	pos := 0
	if max > min {
		pos = int(math.Round((value - min) / (max - min) * float64(barW-1)))
	}
	if pos < 0 {
		pos = 0
	}
	if pos >= barW {
		pos = barW - 1
	}
	var bar strings.Builder
	for i := 0; i < barW; i++ {
		if i <= pos {
			bar.WriteRune('█')
		} else {
			bar.WriteRune('░')
		}
	}
	return "[ - " + bar.String() + " " + rangeValueString(n) + " + ]"
}

// inputValue returns the current editable value of a form control.
func inputValue(n *Node) string {
	if n == nil {
		return ""
	}
	if n.ValueSet {
		return n.Value
	}
	if n.Tag == "select" {
		var first string
		for _, opt := range selectOptions(n) {
			if isDisabledControl(opt) {
				continue
			}
			val := optionValue(opt)
			if first == "" {
				first = val
			}
			if opt.HasAttr("selected") {
				return val
			}
		}
		return first
	}
	if n.Tag == "textarea" {
		return strings.TrimSpace(n.TextContent())
	}
	if n.Tag == "input" {
		switch strings.ToLower(n.Attr("type")) {
		case "range":
			return rangeValueString(n)
		case "color":
			if v := n.Attr("value"); v != "" {
				return v
			}
			return "#000000"
		}
	}
	return n.Attr("value")
}

// inputDisplayCells is the widest text window shown inside a focused input.
const inputDisplayCells = 40

// inputTextDisplay returns the text shown inside a text-like input and the
// cell column of the caret within that text. A focused value scrolls so the
// caret stays visible; unfocused values simply truncate at the right edge.
func inputTextDisplay(n *Node, typ string) (string, int, bool) {
	val := inputValue(n)
	if !n.InputFocused {
		if val == "" {
			val = n.Attr("placeholder")
		}
		if typ == "password" {
			val = strings.Repeat("•", utf8.RuneCountInString(val))
		}
		if val == "" {
			val = strings.Repeat(" ", 10)
		}
		if w := stringWidth(val); w > inputDisplayCells {
			val = truncateWidth(val, inputDisplayCells)
		}
		return val, 0, false
	}
	cur := n.InputCursor
	if cur < 0 {
		cur = 0
	}
	if cur > len(val) {
		cur = len(val)
	}
	if typ == "password" {
		before := utf8.RuneCountInString(val[:cur])
		val = strings.Repeat("•", utf8.RuneCountInString(val))
		cur = len(strings.Repeat("•", before))
	}
	runes := []rune(val)
	curRune := utf8.RuneCountInString(val[:cur])
	if curRune > len(runes) {
		curRune = len(runes)
	}
	// Leave one cell for the caret itself and one for a leading ellipsis.
	limit := inputDisplayCells - 1
	start := curRune
	used := 0
	for start > 0 {
		rw := runeWidth(runes[start-1])
		if used+rw > limit {
			break
		}
		used += rw
		start--
	}
	used = 0
	for i := start; i < curRune; i++ {
		used += runeWidth(runes[i])
	}
	end := curRune
	for end < len(runes) {
		rw := runeWidth(runes[end])
		if used+rw > limit {
			break
		}
		used += rw
		end++
	}
	display := string(runes[start:end])
	cursorCol := stringWidth(string(runes[start:curRune]))
	if start > 0 {
		display = "…" + display
		cursorCol++
	}
	return display, cursorCol, true
}

// inputCursorOffset returns the caret column within a formatted input label.
func inputCursorOffset(n *Node, label string) (int, bool) {
	if n == nil || n.Tag != "input" || !n.InputFocused {
		return 0, false
	}
	typ := strings.ToLower(n.Attr("type"))
	switch typ {
	case "hidden", "checkbox", "radio", "submit", "button", "reset", "file", "image", "range":
		return 0, false
	}
	_, col, ok := inputTextDisplay(n, typ)
	if !ok {
		return 0, false
	}
	if max := stringWidth(label) - 4; col > max {
		col = max
	}
	if col < 0 {
		col = 0
	}
	return col, true
}

func formLabel(n *Node) string {
	switch n.Tag {
	case "input":
		typ := strings.ToLower(n.Attr("type"))
		val := inputValue(n)
		switch typ {
		case "hidden":
			return ""
		case "checkbox":
			if n.HasAttr("checked") {
				return glyphCheckboxOn
			}
			return glyphCheckboxOff
		case "radio":
			if n.HasAttr("checked") {
				return glyphRadioOn
			}
			return glyphRadioOff
		case "submit", "button", "reset":
			if val == "" {
				if typ == "reset" {
					val = "Reset"
				} else {
					val = "Submit"
				}
			}
			return "[ " + val + " ]"
		case "file":
			return "[ Choose File ]"
		case "image":
			return "[ image button ]"
		case "range":
			return rangeLabel(n)
		case "password":
			display, _, _ := inputTextDisplay(n, typ)
			minW := 16
			if w := stringWidth(display); w < minW {
				display += strings.Repeat(" ", minW-w)
			}
			return "[ " + display + " ]"
		default:
			display, _, _ := inputTextDisplay(n, typ)
			minW := 16
			if typ == "search" {
				minW = 30
			}
			if w := stringWidth(display); w < minW {
				display += strings.Repeat(" ", minW-w)
			}
			return "[ " + display + " ]"
		}
	case "select":
		var chosen string
		for _, opt := range selectOptions(n) {
			if isDisabledControl(opt) {
				continue
			}
			if chosen == "" {
				chosen = strings.TrimSpace(opt.TextContent())
			}
			if opt.HasAttr("selected") {
				chosen = strings.TrimSpace(opt.TextContent())
				break
			}
		}
		if chosen == "" {
			chosen = "select"
		}
		return "[ " + chosen + " ▾ ]"
	case "textarea":
		txt := truncateWidth(inputValue(n), 80)
		if txt == "" && !n.InputFocused {
			txt = n.Attr("placeholder")
		}
		if i := strings.IndexByte(txt, '\n'); i >= 0 {
			txt = txt[:i]
		}
		if txt == "" {
			txt = strings.Repeat(" ", 10)
		}
		if w := stringWidth(txt); w < 30 {
			txt += strings.Repeat(" ", 30-w)
		}
		if w := stringWidth(txt); w > 40 {
			txt = truncateWidth(txt, 40)
		}
		return "[ " + txt + " ]"
	case "button":
		txt := strings.TrimSpace(visibleText(n))
		if txt == "" {
			txt = "Button"
		}
		return "[ " + txt + " ]"
	}
	return ""
}
