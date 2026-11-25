package engine

import (
	"strconv"
	"strings"

	"github.com/grafana/sobek"
)

// nodeStyle returns a node's computed style, computing it on demand when the
// node was created after the layout that filled the style map. Without this a
// script-created menu has no style, so getComputedStyle reports "static" and
// offsetWidth falls back to a full-width block guess.
func (j *jsRuntime) nodeStyle(n *Node) *Style {
	if n == nil {
		return nil
	}
	if n.Style != nil {
		return n.Style
	}
	if j.host != nil && j.host.Layout() != nil {
		return j.host.Layout().ComputeNode(n)
	}
	return nil
}

// computedStyleObject returns a getComputedStyle-like snapshot. Values come
// from the resolved style when layout has run; detached or not-yet-styled
// elements get sensible defaults.
func (j *jsRuntime) computedStyleObject(n *Node) *sobek.Object {
	o := j.vm.NewObject()
	if j.cssStyleProto != nil {
		_ = o.SetPrototype(j.cssStyleProto)
	}
	st := j.nodeStyle(n)
	if st == nil {
		st = &Style{}
	}
	font := st.FontSize
	if font <= 0 {
		font = 16
	}
	line := st.LineHeight
	if line <= 0 {
		line = 1
	}
	display := st.Display
	if display == "" {
		display = "block"
	}
	weights := "400"
	if st.Bold {
		weights = "700"
	}
	style := "normal"
	if st.Italic {
		style = "italic"
	}
	decoration := "none"
	switch {
	case st.Underline && st.Strike:
		decoration = "underline line-through"
	case st.Underline:
		decoration = "underline"
	case st.Strike:
		decoration = "line-through"
	}
	props := map[string]string{
		"display":          display,
		"visibility":       "visible",
		"position":         styleDefault(st.Position, "static"),
		"float":            "none",
		"css-float":        "none",
		"box-sizing":       "content-box",
		"opacity":          "1",
		"color":            computedColor(st.Color, "rgb(0, 0, 0)"),
		"background-color": computedColor(st.Background, "rgba(0, 0, 0, 0)"),
		"font-size":        cssPxString(font),
		"font-weight":      weights,
		"font-style":       style,
		"line-height":      cssPxString(line * font),
		"text-align":       styleDefault(st.TextAlign, "start"),
		"text-transform":   styleDefault(st.TextTransform, "none"),
		"text-indent":      lengthCSS(st.TextIndent, "0px"),
		"text-decoration":  decoration,
		"white-space":      styleDefault(st.WhiteSpace, "normal"),
		"overflow":         styleDefault(st.Overflow, "visible"),
		"overflow-x":       styleDefault(st.Overflow, "visible"),
		"overflow-y":       styleDefault(st.Overflow, "visible"),
		"width":            lengthCSS(st.Width, "auto"),
		"height":           lengthCSS(st.Height, "auto"),
		"top":              lengthCSS(st.Top, "auto"),
		"right":            lengthCSS(st.Right, "auto"),
		"bottom":           lengthCSS(st.Bottom, "auto"),
		"left":             lengthCSS(st.Left, "auto"),
		"z-index":          zIndexCSS(st),
		"max-width":        lengthCSS(st.MaxWidth, "none"),
		"list-style-type":  styleDefault(st.ListStyle, "disc"),
		"flex-direction":   styleDefault(st.FlexDir, "row"),
		"flex-grow":        strconv.FormatFloat(st.FlexGrow, 'f', -1, 64),
		"gap":              lengthCSS(st.Gap, "normal"),
		"border-color":     computedColor(st.BorderColor, "rgb(0, 0, 0)"),
	}
	sides := []string{"top", "right", "bottom", "left"}
	for i, side := range sides {
		props["margin-"+side] = lengthCSS(st.Margin[i], "0px")
		props["padding-"+side] = lengthCSS(st.Padding[i], "0px")
		props["border-"+side+"-width"] = lengthCSS(st.Border[i], "0px")
	}
	for k, v := range props {
		_ = o.Set(k, v)
		if camel := kebabToCamel(k); camel != k {
			_ = o.Set(camel, v)
		}
	}
	j.method(o, "getPropertyValue", func(call sobek.FunctionCall) sobek.Value {
		name := strings.ToLower(strings.TrimSpace(call.Argument(0).String()))
		if v, ok := props[name]; ok {
			return j.vm.ToValue(v)
		}
		if v, ok := props[camelToKebab(name)]; ok {
			return j.vm.ToValue(v)
		}
		return j.vm.ToValue("")
	})
	j.method(o, "getPropertyPriority", func(sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue("")
	})
	j.method(o, "item", func(call sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue("")
	})
	_ = o.Set("length", len(props))
	return o
}

func styleDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func computedColor(c Color, def string) string {
	if !c.Set {
		return def
	}
	if c.A >= 255 {
		return "rgb(" + strconv.Itoa(int(c.R)) + ", " + strconv.Itoa(int(c.G)) + ", " + strconv.Itoa(int(c.B)) + ")"
	}
	return "rgba(" + strconv.Itoa(int(c.R)) + ", " + strconv.Itoa(int(c.G)) + ", " +
		strconv.Itoa(int(c.B)) + ", " + strconv.FormatFloat(float64(c.A)/255, 'f', 3, 64) + ")"
}

func cssPxString(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64) + "px"
}

func lengthCSS(l Length, def string) string {
	if !l.Set || l.Auto {
		return def
	}
	switch l.U {
	case UnitPercent:
		return strconv.FormatFloat(l.V, 'f', -1, 64) + "%"
	case UnitEM:
		return strconv.FormatFloat(l.V, 'f', -1, 64) + "em"
	case UnitREM:
		return strconv.FormatFloat(l.V, 'f', -1, 64) + "rem"
	case UnitPT:
		return strconv.FormatFloat(l.V, 'f', -1, 64) + "pt"
	case UnitCH:
		return strconv.FormatFloat(l.V, 'f', -1, 64) + "ch"
	default:
		return cssPxString(l.V)
	}
}

func kebabToCamel(s string) string {
	parts := strings.Split(s, "-")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

func camelToKebab(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('-')
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
