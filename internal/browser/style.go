package browser

import (
	"math"
	"strconv"
	"strings"
)

const (
	cellW = 8.0  // CSS pixels per terminal column
	cellH = 16.0 // CSS pixels per terminal row
)

const (
	UnitNone Unit = iota
	UnitPX
	UnitEM
	UnitREM
	UnitPercent
	UnitPT
	UnitCH
	UnitLH
	UnitVW
	UnitVH
	UnitVMin
	UnitVMax
)

// Style is a computed style. Lengths remain unresolved so percentages can be
// resolved against the containing block during layout.
type Style struct {
	// Vars carries inherited CSS custom properties used by var().
	Vars           map[string]string
	Display        string
	Hidden         bool
	HiddenExplicit bool
	Color          Color
	Background     Color
	FontSize       float64
	Bold           bool
	Italic         bool
	Underline      bool
	Strike         bool
	TextTransform  string
	TextAlign      string
	VerticalAlign  string
	TextIndent     Length
	WhiteSpace     string
	Content        string
	ContentSet     bool
	LineClamp      int
	LineHeight     float64 // multiplier
	Margin         [4]Length
	Padding        [4]Length
	Border         [4]Length
	BorderStyle    [4]bool
	BorderColor    Color
	Width          Length
	Height         Length
	MinWidth       Length
	MaxWidth       Length
	MinHeight      Length
	MaxHeight      Length
	BoxSizing      string
	ListStyle      string
	Overflow       string
	Position       string
	Float          string
	Clear          string
	Top            Length
	Right          Length
	Bottom         Length
	Left           Length
	Clip           string
	FlexDir        string
	FlexWrap       string
	JustifyContent string
	AlignItems     string
	Order          int
	FlexGrow       float64
	FlexShrink     float64
	FlexBasis      Length
	FlexBasisSet   bool
	Gap            Length
	RowGap         Length
	GridCols       string
	GridAreas      string
	GridArea       string
	GridColumn     string
	GridRow        string
	GridAutoFlow   string
	ObjectFit      string
	ObjectPosX     float64
	ObjectPosY     float64
	AspectRatio    float64
}

func parseObjectPosition(v string, st *Style) {
	fields := splitFields(strings.ToLower(strings.TrimSpace(v)))
	if len(fields) == 0 {
		return
	}
	parse := func(s string) (float64, bool) {
		switch s {
		case "left", "top":
			return 0, true
		case "center":
			return 0.5, true
		case "right", "bottom":
			return 1, true
		}
		if strings.HasSuffix(s, "%") {
			if f, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64); err == nil {
				if f < 0 {
					f = 0
				}
				if f > 100 {
					f = 100
				}
				return f / 100, true
			}
		}
		return 0, false
	}
	x, xok := parse(fields[0])
	if len(fields) == 1 {
		if fields[0] == "top" || fields[0] == "bottom" {
			st.ObjectPosX, st.ObjectPosY = 0.5, x
			return
		}
		if xok {
			st.ObjectPosX = x
		}
		return
	}
	y, yok := parse(fields[1])
	if xok {
		st.ObjectPosX = x
	}
	if yok {
		st.ObjectPosY = y
	}
}

func parseAspectRatio(v string, st *Style) {
	v = strings.ToLower(strings.TrimSpace(v))
	if strings.HasPrefix(v, "auto") {
		v = strings.TrimSpace(strings.TrimPrefix(v, "auto"))
	}
	if v == "" {
		return
	}
	if i := strings.IndexByte(v, '/'); i >= 0 {
		w, err1 := strconv.ParseFloat(strings.TrimSpace(v[:i]), 64)
		h, err2 := strconv.ParseFloat(strings.TrimSpace(v[i+1:]), 64)
		if err1 == nil && err2 == nil && w > 0 && h > 0 {
			st.AspectRatio = w / h
		}
		return
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
		st.AspectRatio = f
	}
}

// TextScale is the visual size multiplier used by terminal text sizing. Runs
// at the base 16px font have scale 1; 24px rounds to 2, 40px to 3, and so on.
func (s *Style) TextScale() int {
	if s == nil || s.FontSize < 20 {
		return 1
	}
	n := int(math.Round(s.FontSize / cellH))
	if n < 1 {
		n = 1
	}
	if n > 4 {
		n = 4
	}
	return n
}

// LineHeightCells returns the height of a line box in terminal rows.
func (s *Style) LineHeightCells() int {
	lh := s.LineHeight
	if lh <= 0 {
		lh = 1
	}
	h := int(math.Floor(s.FontSize * lh / cellH))
	if h < 1 {
		h = 1
	}
	return h
}

func defaultStyle() *Style {
	return &Style{
		Display:     "inline",
		Color:       RGB(0x1f, 0x23, 0x28),
		FontSize:    16,
		TextAlign:   "left",
		LineHeight:  1,
		WhiteSpace:  "normal",
		BoxSizing:   "content-box",
		ListStyle:   "disc",
		BorderColor: RGB(0xd0, 0xd7, 0xde),
		ObjectPosX:  0.5,
		ObjectPosY:  0.5,
	}
}

func inheritStyle(parent *Style) *Style {
	st := defaultStyle()
	if parent != nil {
		st.Color = parent.Color
		st.FontSize = parent.FontSize
		st.Bold = parent.Bold
		st.Italic = parent.Italic
		st.Underline = parent.Underline
		st.Strike = parent.Strike
		st.TextTransform = parent.TextTransform
		st.TextAlign = parent.TextAlign
		st.WhiteSpace = parent.WhiteSpace
		st.LineHeight = parent.LineHeight
		st.ListStyle = parent.ListStyle
		st.Hidden = parent.Hidden
		st.BorderColor = parent.BorderColor
		if len(parent.Vars) > 0 {
			st.Vars = make(map[string]string, len(parent.Vars))
			for k, v := range parent.Vars {
				st.Vars[k] = v
			}
		}
	}
	return st
}

// setPair applies a logical start/end shorthand to two physical sides. MOB³
// only lays out horizontal-tb text, so inline maps to left/right and block to
// top/bottom.
func setPair(dst *[4]Length, start, end int, fields []string, fontPx float64) {
	if len(fields) == 0 {
		return
	}
	first := parseLength(fields[0], fontPx)
	second := first
	if len(fields) > 1 {
		second = parseLength(fields[1], fontPx)
	}
	dst[start], dst[end] = first, second
}

func setSides(dst *[4]Length, fields []string, fontPx float64) {
	if len(fields) == 0 {
		return
	}
	ls := make([]Length, len(fields))
	for i, f := range fields {
		ls[i] = parseLength(f, fontPx)
	}
	switch len(ls) {
	case 1:
		dst[0], dst[1], dst[2], dst[3] = ls[0], ls[0], ls[0], ls[0]
	case 2:
		dst[0], dst[2] = ls[0], ls[0]
		dst[1], dst[3] = ls[1], ls[1]
	case 3:
		dst[0] = ls[0]
		dst[1], dst[3] = ls[1], ls[1]
		dst[2] = ls[2]
	default:
		dst[0], dst[1], dst[2], dst[3] = ls[0], ls[1], ls[2], ls[3]
	}
}

func isBorderStyle(s string) bool {
	switch strings.ToLower(s) {
	case "none", "hidden", "solid", "dashed", "dotted", "double",
		"groove", "ridge", "inset", "outset":
		return true
	}
	return false
}

func applyDecls(st *Style, decls []Declaration, parent *Style, important bool, fontBase float64) {
	// Custom properties must be available before declarations that use them,
	// even when the var() reference appears earlier in the same block.
	for _, d := range decls {
		if d.Important != important || !strings.HasPrefix(d.Prop, "--") {
			continue
		}
		applyDecl(st, d.Prop, d.Value, parent)
	}
	for _, d := range decls {
		if d.Important != important || strings.HasPrefix(d.Prop, "--") {
			continue
		}
		applyDecl(st, d.Prop, d.Value, parent)
	}
}

func applyDecl(st *Style, prop, val string, parent *Style) {
	prop = strings.TrimSpace(prop)
	if strings.HasPrefix(prop, "--") {
		if st.Vars == nil {
			st.Vars = map[string]string{}
		}
		st.Vars[prop] = strings.TrimSpace(val)
		return
	}
	prop = strings.ToLower(prop)
	val = strings.TrimSpace(val)
	if val == "" {
		return
	}
	if strings.Contains(strings.ToLower(val), "var(") {
		expanded, ok := expandVars(val, st.Vars, 0)
		if !ok {
			return
		}
		val = expanded
		if val == "" {
			return
		}
	}
	lower := strings.ToLower(val)
	switch prop {
	case "display":
		switch lower {
		case "none":
			st.Display = "none"
		case "block", "flow-root", "-webkit-box", "flex", "grid", "list-item", "table",
			"table-row", "table-cell", "table-caption", "table-row-group",
			"table-header-group", "table-footer-group", "inline-block",
			"inline-flex", "inline-grid", "inline-table", "contents":
			switch lower {
			case "flow-root", "-webkit-box":
				st.Display = "block"
			case "grid":
				st.Display = "grid"
			case "flex":
				st.Display = "flex"
			case "inline-flex", "inline-grid":
				// Inline-level flex/grid boxes stay in the surrounding line;
				// treating them as atomic inline-block boxes keeps their
				// padding and prevents buttons from becoming full-width rows.
				st.Display = "inline-block"
			case "contents":
				st.Display = "contents"
			case "inline-table":
				st.Display = "table"
			default:
				st.Display = lower
			}
		case "inline":
			st.Display = "inline"
		default:
			st.Display = "inline"
		}
	case "visibility":
		st.Hidden = lower == "hidden" || lower == "collapse"
	case "color":
		if c, ok := parseColor(val, st.Color); ok {
			st.Color = c
		}
	case "background-color":
		if c, ok := parseColor(val, st.Background); ok {
			st.Background = c
		}
	case "background":
		if f := splitFields(val); len(f) > 0 {
			if lower == "none" {
				st.Background = Color{}
				return
			}
			for i := len(f) - 1; i >= 0; i-- {
				if c, ok := parseColor(f[i], st.Color); ok && !strings.HasPrefix(f[i], "url(") {
					st.Background = c
					break
				}
			}
		}
	case "font-size":
		if sz, ok := parseFontSize(val, parentSize(parent)); ok {
			if sz < 1 {
				sz = 1
			}
			st.FontSize = sz
		}
	case "font-weight":
		switch lower {
		case "bold", "bolder":
			st.Bold = true
		case "normal", "lighter":
			st.Bold = false
		default:
			if n, err := strconv.Atoi(lower); err == nil {
				st.Bold = n >= 600
			}
		}
	case "font-style":
		st.Italic = lower == "italic" || lower == "oblique"
	case "font":
		fields := splitFields(val)
		for _, f := range fields {
			fl := strings.ToLower(f)
			switch {
			case fl == "bold" || fl == "bolder":
				st.Bold = true
				continue
			case fl == "italic" || fl == "oblique":
				st.Italic = true
				continue
			case fl == "normal" || fl == "small-caps" || fl == "lighter":
				continue
			}
			// A bare 100..900 token is a font-weight, not a pixel size.
			if n, err := strconv.Atoi(fl); err == nil && n >= 100 && n <= 900 && n%100 == 0 {
				continue
			}
			sizePart := f
			lh := ""
			if j := strings.IndexByte(sizePart, '/'); j >= 0 {
				lh = sizePart[j+1:]
				sizePart = sizePart[:j]
			}
			if sz, ok := parseFontSize(sizePart, parentSize(parent)); ok {
				st.FontSize = sz
				if lh != "" {
					if f, err := strconv.ParseFloat(strings.TrimSpace(lh), 64); err == nil {
						st.LineHeight = math.Max(0.5, f)
					} else if l := parseLength(lh, st.FontSize); l.Set {
						st.LineHeight = math.Max(0.5, l.cssPx(st.FontSize)/st.FontSize)
					}
				}
				break
			}
		}
	case "line-height":
		if lower == "normal" {
			st.LineHeight = 1.2
			break
		}
		if f, err := strconv.ParseFloat(lower, 64); err == nil {
			st.LineHeight = math.Max(0.5, f)
			return
		}
		if l := parseLength(val, st.FontSize); l.Set {
			if l.U == UnitPercent {
				st.LineHeight = l.V / 100
			} else {
				st.LineHeight = l.cssPx(st.FontSize) / st.FontSize
			}
			if st.LineHeight < 0.5 {
				st.LineHeight = 0.5
			}
		}
	case "text-align":
		switch lower {
		case "left", "right", "center", "justify":
			st.TextAlign = lower
		}
	case "vertical-align":
		switch lower {
		case "top", "middle", "bottom", "baseline", "sub", "super", "text-top", "text-bottom":
			st.VerticalAlign = lower
		}
	case "text-indent":
		st.TextIndent = parseLength(val, st.FontSize)
	case "text-transform":
		switch lower {
		case "uppercase", "lowercase", "capitalize", "none":
			st.TextTransform = lower
		}
	case "content":
		switch lower {
		case "none", "normal":
			st.Content = ""
			st.ContentSet = false
		default:
			st.Content = val
			if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
				st.Content = val[1 : len(val)-1]
			}
			st.ContentSet = true
		}
	case "text-decoration", "text-decoration-line":
		f := splitFields(lower)
		none := false
		underline, strike := false, false
		for _, x := range f {
			switch x {
			case "none":
				none = true
			case "underline":
				underline = true
			case "line-through":
				strike = true
			}
		}
		if none {
			st.Underline, st.Strike = false, false
		} else {
			st.Underline, st.Strike = underline, strike
		}
	case "white-space":
		switch lower {
		case "pre", "pre-wrap", "pre-line":
			st.WhiteSpace = "pre"
		case "nowrap":
			st.WhiteSpace = "nowrap"
		default:
			st.WhiteSpace = "normal"
		}
	case "margin":
		setSides(&st.Margin, splitFields(val), st.FontSize)
	case "margin-top":
		st.Margin[0] = parseLength(val, st.FontSize)
	case "margin-right":
		st.Margin[1] = parseLength(val, st.FontSize)
	case "margin-bottom":
		st.Margin[2] = parseLength(val, st.FontSize)
	case "margin-left":
		st.Margin[3] = parseLength(val, st.FontSize)
	case "margin-inline":
		setPair(&st.Margin, 3, 1, splitFields(val), st.FontSize)
	case "margin-block":
		setPair(&st.Margin, 0, 2, splitFields(val), st.FontSize)
	case "margin-inline-start":
		st.Margin[3] = parseLength(val, st.FontSize)
	case "margin-inline-end":
		st.Margin[1] = parseLength(val, st.FontSize)
	case "margin-block-start":
		st.Margin[0] = parseLength(val, st.FontSize)
	case "margin-block-end":
		st.Margin[2] = parseLength(val, st.FontSize)
	case "padding":
		setSides(&st.Padding, splitFields(val), st.FontSize)
	case "padding-top":
		st.Padding[0] = parseLength(val, st.FontSize)
	case "padding-right":
		st.Padding[1] = parseLength(val, st.FontSize)
	case "padding-bottom":
		st.Padding[2] = parseLength(val, st.FontSize)
	case "padding-left":
		st.Padding[3] = parseLength(val, st.FontSize)
	case "padding-inline":
		setPair(&st.Padding, 3, 1, splitFields(val), st.FontSize)
	case "padding-block":
		setPair(&st.Padding, 0, 2, splitFields(val), st.FontSize)
	case "padding-inline-start":
		st.Padding[3] = parseLength(val, st.FontSize)
	case "padding-inline-end":
		st.Padding[1] = parseLength(val, st.FontSize)
	case "padding-block-start":
		st.Padding[0] = parseLength(val, st.FontSize)
	case "padding-block-end":
		st.Padding[2] = parseLength(val, st.FontSize)
	case "border":
		applyBorderShorthand(st, val, []int{0, 1, 2, 3})
	case "border-top":
		applyBorderShorthand(st, val, []int{0})
	case "border-right":
		applyBorderShorthand(st, val, []int{1})
	case "border-bottom":
		applyBorderShorthand(st, val, []int{2})
	case "border-left":
		applyBorderShorthand(st, val, []int{3})
	case "border-width":
		applyBorderWidth(st, splitFields(val), []int{0, 1, 2, 3})
	case "border-top-width":
		applyBorderWidth(st, []string{val}, []int{0})
	case "border-right-width":
		applyBorderWidth(st, []string{val}, []int{1})
	case "border-bottom-width":
		applyBorderWidth(st, []string{val}, []int{2})
	case "border-left-width":
		applyBorderWidth(st, []string{val}, []int{3})
	case "border-color":
		applyBorderColor(st, splitFields(val), []int{0, 1, 2, 3})
	case "border-top-color":
		applyBorderColor(st, []string{val}, []int{0})
	case "border-right-color":
		applyBorderColor(st, []string{val}, []int{1})
	case "border-bottom-color":
		applyBorderColor(st, []string{val}, []int{2})
	case "border-left-color":
		applyBorderColor(st, []string{val}, []int{3})
	case "border-style":
		applyBorderStyleFields(st, splitFields(lower), []int{0, 1, 2, 3})
	case "border-top-style":
		applyBorderStyleFields(st, []string{lower}, []int{0})
	case "border-right-style":
		applyBorderStyleFields(st, []string{lower}, []int{1})
	case "border-bottom-style":
		applyBorderStyleFields(st, []string{lower}, []int{2})
	case "border-left-style":
		applyBorderStyleFields(st, []string{lower}, []int{3})
	case "width":
		if lower == "auto" || lower == "fit-content" || lower == "max-content" || lower == "min-content" {
			st.Width = Length{}
		} else {
			st.Width = parseLength(val, st.FontSize)
		}
	case "height":
		if lower == "auto" {
			st.Height = Length{}
		} else {
			st.Height = parseLength(val, st.FontSize)
		}
	case "min-width":
		if lower == "auto" {
			st.MinWidth = Length{}
		} else {
			st.MinWidth = parseLength(val, st.FontSize)
		}
	case "max-width":
		if lower == "none" || lower == "auto" {
			st.MaxWidth = Length{}
		} else {
			st.MaxWidth = parseLength(val, st.FontSize)
		}
	case "min-height":
		if lower == "auto" {
			st.MinHeight = Length{}
		} else {
			st.MinHeight = parseLength(val, st.FontSize)
		}
	case "max-height":
		if lower == "none" || lower == "auto" {
			st.MaxHeight = Length{}
		} else {
			st.MaxHeight = parseLength(val, st.FontSize)
		}
	case "box-sizing":
		switch lower {
		case "border-box":
			st.BoxSizing = "border-box"
		case "content-box":
			st.BoxSizing = "content-box"
		}
	case "list-style", "list-style-type":
		for _, f := range splitFields(lower) {
			switch f {
			case "disc", "circle", "square", "decimal", "decimal-leading-zero",
				"lower-alpha", "upper-alpha", "lower-latin", "upper-latin",
				"lower-roman", "upper-roman", "none":
				st.ListStyle = f
			}
		}
	case "flex-direction":
		switch lower {
		case "column", "column-reverse":
			st.FlexDir = "column"
		default:
			st.FlexDir = "row"
		}
	case "flex-wrap":
		switch lower {
		case "nowrap", "no-wrap":
			st.FlexWrap = "nowrap"
		case "wrap", "wrap-reverse":
			st.FlexWrap = "wrap"
		}
	case "justify-content":
		switch lower {
		case "flex-end", "end", "right":
			st.JustifyContent = "end"
		case "center":
			st.JustifyContent = "center"
		case "space-between":
			st.JustifyContent = "between"
		case "space-around":
			st.JustifyContent = "around"
		case "space-evenly":
			st.JustifyContent = "evenly"
		default:
			st.JustifyContent = "start"
		}
	case "align-items":
		switch lower {
		case "center":
			st.AlignItems = "center"
		case "flex-end", "end", "bottom":
			st.AlignItems = "end"
		default:
			st.AlignItems = "start"
		}
	case "order":
		if n, err := strconv.Atoi(lower); err == nil {
			st.Order = n
		}
	case "flex-flow":
		for _, f := range splitFields(lower) {
			switch f {
			case "column", "column-reverse":
				st.FlexDir = "column"
			case "row", "row-reverse":
				st.FlexDir = "row"
			case "wrap", "wrap-reverse":
				st.FlexWrap = "wrap"
			}
		}
	case "flex-grow":
		if f, err := strconv.ParseFloat(lower, 64); err == nil && f > 0 {
			st.FlexGrow = f
		}
	case "flex":
		fields := splitFields(lower)
		if len(fields) > 0 {
			if f, err := strconv.ParseFloat(fields[0], 64); err == nil {
				if f > 0 {
					st.FlexGrow = f
				}
				if len(fields) >= 3 {
					if s, err := strconv.ParseFloat(fields[1], 64); err == nil {
						st.FlexShrink = s
					}
					if l := parseLength(fields[2], st.FontSize); l.Set {
						st.FlexBasis = l
						st.FlexBasisSet = true
					}
				}
			} else if l := parseLength(fields[0], st.FontSize); l.Set {
				st.FlexBasis = l
				st.FlexBasisSet = true
			}
		}
	case "flex-basis":
		if l := parseLength(val, st.FontSize); l.Set {
			st.FlexBasis = l
			st.FlexBasisSet = true
		}
	case "flex-shrink":
		if f, err := strconv.ParseFloat(lower, 64); err == nil && f >= 0 {
			st.FlexShrink = f
		}
	case "gap", "column-gap":
		if l := parseLength(val, st.FontSize); l.Set {
			st.Gap = l
		}
	case "row-gap", "grid-row-gap":
		if l := parseLength(val, st.FontSize); l.Set {
			st.RowGap = l
		}
	case "grid-template-columns":
		st.GridCols = val
	case "grid-template-areas":
		st.GridAreas = val
	case "grid-area":
		if fields := splitFields(val); len(fields) > 0 {
			name := fields[0]
			if name != "auto" && name != "span" {
				if _, err := strconv.Atoi(name); err != nil {
					st.GridArea = name
				}
			}
		}
	case "grid-column", "grid-column-start", "grid-column-end":
		if prop == "grid-column" {
			st.GridColumn = val
		} else if prop == "grid-column-start" {
			st.GridColumn = val + " / auto"
		}
	case "grid-row", "grid-row-start", "grid-row-end":
		if prop == "grid-row" {
			st.GridRow = val
		}
	case "grid-auto-flow":
		st.GridAutoFlow = lower
	case "transform":
		// Content moved fully off-screen (common for cloned fixed navs and
		// closed drawers) should not take part in layout.
		if strings.Contains(lower, "translatey(-") || strings.Contains(lower, "translatex(-") ||
			strings.Contains(lower, "scale(0") {
			st.Hidden = true
		}
	case "opacity":
		if f, err := strconv.ParseFloat(lower, 64); err == nil && f <= 0.05 {
			st.Hidden = true
		}
	case "overflow", "overflow-x", "overflow-y":
		// A shorthand such as "hidden auto" still clips both axes in a
		// terminal; only a fully visible value leaves descendants unclipped.
		if lower != "" && lower != "visible" {
			st.Overflow = "hidden"
		}
	case "line-clamp", "-webkit-line-clamp":
		if n, err := strconv.Atoi(lower); err == nil && n > 0 {
			st.LineClamp = n
		}
	case "float":
		switch lower {
		case "left", "right", "inline-start", "inline-end":
			st.Float = lower
		default:
			st.Float = ""
		}
	case "clear":
		switch lower {
		case "left", "right", "both", "inline-start", "inline-end":
			st.Clear = lower
		default:
			st.Clear = ""
		}
	case "position":
		st.Position = lower
	case "object-fit":
		switch lower {
		case "fill", "contain", "cover", "none", "scale-down":
			st.ObjectFit = lower
		}
	case "object-position":
		parseObjectPosition(val, st)
	case "aspect-ratio":
		parseAspectRatio(val, st)
	case "top", "right", "bottom", "left":
		l := parseLength(val, st.FontSize)
		switch prop {
		case "top":
			st.Top = l
		case "right":
			st.Right = l
		case "bottom":
			st.Bottom = l
		case "left":
			st.Left = l
		}
	case "clip", "clip-path":
		st.Clip = lower
	}
}

func parentSize(parent *Style) float64 {
	if parent != nil {
		return parent.FontSize
	}
	return 16
}

func applyBorderShorthand(st *Style, val string, sides []int) {
	w := Length{}
	color := Color{}
	none := false
	styled := false
	for _, f := range splitFields(val) {
		fl := strings.ToLower(f)
		if isBorderStyle(fl) {
			styled = true
			if fl == "none" || fl == "hidden" {
				none = true
			}
			continue
		}
		if c, ok := parseColor(f, st.BorderColor); ok {
			color = c
			continue
		}
		switch fl {
		case "thin":
			w = px(1)
		case "medium":
			w = px(3)
		case "thick":
			w = px(5)
		default:
			if l := parseLength(f, st.FontSize); l.Set {
				w = l
			}
		}
	}
	for _, i := range sides {
		if none {
			st.Border[i] = Length{}
			st.BorderStyle[i] = false
		} else {
			st.Border[i] = w
			st.BorderStyle[i] = styled
		}
	}
	if color.Set {
		st.BorderColor = color
	}
}

func applyBorderWidth(st *Style, fields []string, sides []int) {
	var widths [4]Length
	setSides(&widths, fields, st.FontSize)
	for _, i := range sides {
		st.Border[i] = widths[i]
		if len(fields) == 1 {
			st.Border[i] = widths[0]
		}
	}
}

// applyBorderStyleFields maps the 1-4 values of a border-style declaration
// onto the selected sides. Used border widths are zero when a side is
// none/hidden, even if border-width is set later.
func applyBorderStyleFields(st *Style, fields []string, sides []int) {
	var vis [4]bool
	for i, f := range fields {
		if i >= 4 {
			break
		}
		s := strings.ToLower(f)
		vis[i] = isBorderStyle(s) && s != "none" && s != "hidden"
	}
	switch len(fields) {
	case 0:
		return
	case 1:
		vis[1], vis[2], vis[3] = vis[0], vis[0], vis[0]
	case 2:
		vis[2], vis[3] = vis[0], vis[1]
	case 3:
		vis[3] = vis[1]
	}
	for _, i := range sides {
		st.BorderStyle[i] = vis[i]
	}
}

func applyBorderColor(st *Style, fields []string, sides []int) {
	var colors [4]Color
	for i, f := range fields {
		if c, ok := parseColor(f, st.BorderColor); ok {
			if i < 4 {
				colors[i] = c
			}
		}
	}
	switch len(fields) {
	case 1:
		if colors[0].Set {
			st.BorderColor = colors[0]
		}
	default:
		if colors[0].Set {
			st.BorderColor = colors[0]
		}
	}
	_ = sides
}

// ApplyPresentationAttrs maps legacy HTML attributes onto the style, before
// author stylesheets so CSS can override them.
func ApplyPresentationAttrs(n *Node, st *Style) {
	if n == nil || n.Type != ElementNode {
		return
	}
	if v := n.Attr("bgcolor"); v != "" {
		if c, ok := parseColor(v, st.Background); ok {
			st.Background = c
		}
	}
	if v := n.Attr("color"); v != "" && (n.Tag == "font" || n.Tag == "body" || n.Tag == "table" || n.Tag == "tr" || n.Tag == "td" || n.Tag == "th") {
		if c, ok := parseColor(v, st.Color); ok {
			st.Color = c
		}
	}
	if v := n.Attr("text"); v != "" && n.Tag == "body" {
		if c, ok := parseColor(v, st.Color); ok {
			st.Color = c
		}
	}
	if v := n.Attr("align"); v != "" && (n.Tag == "td" || n.Tag == "th" || n.Tag == "div" || n.Tag == "p" || n.Tag == "table" || n.Tag == "h1" || n.Tag == "h2" || n.Tag == "h3" || n.Tag == "h4" || n.Tag == "h5" || n.Tag == "h6") {
		switch strings.ToLower(v) {
		case "left", "right", "center":
			st.TextAlign = strings.ToLower(v)
		}
	}
	if n.HasAttr("nowrap") {
		st.WhiteSpace = "nowrap"
	}
	if v := n.Attr("width"); v != "" {
		switch n.Tag {
		case "img", "table", "td", "th", "hr":
			if l := parseLength(v, st.FontSize); l.Set {
				st.Width = l
			}
		}
	}
	if v := n.Attr("height"); v != "" {
		switch n.Tag {
		case "img", "table", "td", "th":
			if l := parseLength(v, st.FontSize); l.Set {
				st.Height = l
			}
		}
	}
	if n.Tag == "font" {
		if v := n.Attr("face"); v != "" {
			_ = v
		}
	}
}

// expandVars substitutes var(--name[, fallback]) references using the
// inherited custom property map. It returns false for an unresolved reference
// without a fallback, which drops the declaration like an invalid CSS value.
func expandVars(value string, vars map[string]string, depth int) (string, bool) {
	if depth > 16 {
		return "", false
	}
	for {
		lower := strings.ToLower(value)
		idx := strings.Index(lower, "var(")
		if idx < 0 {
			return value, true
		}
		end := matchingParen(value, idx+3)
		if end < 0 {
			return "", false
		}
		inner := value[idx+4 : end]
		parts := splitTopLevel(inner, ',')
		name := strings.TrimSpace(parts[0])
		repl, ok := vars[name]
		if !ok {
			if len(parts) < 2 {
				return "", false
			}
			repl = strings.TrimSpace(strings.Join(parts[1:], ","))
		}
		expanded, ok := expandVars(repl, vars, depth+1)
		if !ok {
			return "", false
		}
		value = value[:idx] + expanded + value[end+1:]
	}
}

// matchingParen returns the index of the ')' matching the '(' at open.
func matchingParen(s string, open int) int {
	if open < 0 || open >= len(s) || s[open] != '(' {
		return -1
	}
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '\'':
			i = skipString(s, i)
			if i >= len(s) {
				return -1
			}
		case '"':
			i = skipString(s, i)
			if i >= len(s) {
				return -1
			}
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func clipHidesContent(clip string) bool {
	clip = strings.TrimSpace(strings.ToLower(clip))
	if clip == "" {
		return false
	}
	if strings.Contains(clip, "inset(50%)") || strings.Contains(clip, "inset(100%)") {
		return true
	}
	if !strings.HasPrefix(clip, "rect(") || !strings.HasSuffix(clip, ")") {
		return false
	}
	inner := strings.TrimSpace(clip[5 : len(clip)-1])
	parts := splitTopLevel(inner, ',')
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "auto" {
			return false
		}
		l := parseLength(part, 16)
		if !l.Set || l.cssPx(16) > 1 {
			return false
		}
	}
	return true
}
