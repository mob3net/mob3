package engine

import (
	"strings"

	"github.com/grafana/sobek"
)

var styleProps = map[string]string{
	"color":                    "color",
	"background":               "background",
	"backgroundColor":          "background-color",
	"display":                  "display",
	"fontSize":                 "font-size",
	"fontWeight":               "font-weight",
	"fontStyle":                "font-style",
	"fontFamily":               "font-family",
	"lineHeight":               "line-height",
	"textAlign":                "text-align",
	"textDecoration":           "text-decoration",
	"textTransform":            "text-transform",
	"margin":                   "margin",
	"padding":                  "padding",
	"border":                   "border",
	"width":                    "width",
	"height":                   "height",
	"maxWidth":                 "max-width",
	"position":                 "position",
	"top":                      "top",
	"right":                    "right",
	"bottom":                   "bottom",
	"left":                     "left",
	"zIndex":                   "z-index",
	"opacity":                  "opacity",
	"whiteSpace":               "white-space",
	"visibility":               "visibility",
	"transition":               "transition",
	"transitionProperty":       "transition-property",
	"transitionDuration":       "transition-duration",
	"transitionTimingFunction": "transition-timing-function",
	"transitionDelay":          "transition-delay",
	"transform":                "transform",
	"transformOrigin":          "transform-origin",
	"transformStyle":           "transform-style",
	"animation":                "animation",
	"animationName":            "animation-name",
	"animationDuration":        "animation-duration",
	"animationTimingFunction":  "animation-timing-function",
	"animationDelay":           "animation-delay",
	"animationIterationCount":  "animation-iteration-count",
	"animationDirection":       "animation-direction",
	"animationFillMode":        "animation-fill-mode",
	"animationPlayState":       "animation-play-state",
	"willChange":               "will-change",
	"scrollBehavior":           "scroll-behavior",
	"WebkitTransition":         "transition",
	"WebkitTransform":          "transform",
	"webkitTransition":         "transition",
	"webkitTransform":          "transform",
	"MozTransition":            "transition",
	"MozTransform":             "transform",
}

// cssLengthPx resolves a CSS length string to whole CSS pixels for the DOM
// measurement APIs. Percentages use the supplied viewport basis.
func cssLengthPx(v string, font, basis float64) (int, bool) {
	if strings.TrimSpace(v) == "" {
		return 0, false
	}
	l := ParseLength(v, font)
	if !l.Set || l.Auto {
		return 0, false
	}
	return lengthPx(l, font, basis)
}

func lengthPx(l Length, font, basis float64) (int, bool) {
	if !l.Set || l.Auto {
		return 0, false
	}
	switch {
	case l.Calc:
		return int(l.CalcPx + l.CalcVW/100*basis + basis*l.CalcPct/100 + 0.5), true
	case l.Func != nil:
		return int(l.Func.PX(basis) + 0.5), true
	case l.U == UnitPercent:
		return int(basis*l.V/100 + 0.5), true
	}
	return int(l.CSSPx(font) + 0.5), true
}

func (j *jsRuntime) elementWidthPx(n *Node, viewport int) int {
	if n == nil {
		return 0
	}
	if n.Tag == "html" || n.Tag == "body" {
		return viewport
	}
	j.nodeStyle(n)
	font := 16.0
	if n.Style != nil && n.Style.FontSize > 0 {
		font = n.Style.FontSize
	}
	if v := styleGet(n, "width"); v != "" {
		if px, ok := cssLengthPx(v, font, float64(viewport)); ok {
			return px
		}
	}
	if n.Style != nil && n.Style.Width.Set && !n.Style.Width.IsAuto() {
		basis := 0.0
		if n.Style.Width.U == UnitPercent {
			basis = float64(viewport)
		}
		if px, ok := lengthPx(n.Style.Width, font, basis); ok {
			return px
		}
	}
	if j.host.Layout() != nil {
		if r, ok := j.host.Layout().Boxes[n]; ok && r.W > 0 {
			return CSSWidthPx(r.W)
		}
	}
	// Before the first layout pass (scripts run ahead of it) a block-level box
	// with auto width still fills its containing block. Carousels read
	// clientWidth to size their slides; reporting 0 made them write
	// `width:0px` into the DOM and collapse the whole slider.
	if blockLevelNode(n) {
		return j.containingWidthPx(n, viewport)
	}
	// A shrink-to-fit box that has not been laid out yet (a dropdown appended
	// by script) is at least its min-width. Menus read offsetWidth right after
	// appending to centre themselves; reporting 0 made them fall back to a
	// guessed width and open at the page corner.
	if n.Style != nil && n.Style.MinWidth.Set && !n.Style.MinWidth.IsAuto() {
		basis := 0.0
		if n.Style.MinWidth.U == UnitPercent {
			basis = float64(viewport)
		}
		if px, ok := lengthPx(n.Style.MinWidth, font, basis); ok && px > 0 {
			return px
		}
	}
	return 0
}

var blockLevelTags = map[string]bool{
	"html": true, "body": true, "div": true, "p": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "li": true, "dl": true, "dt": true, "dd": true,
	"blockquote": true, "pre": true, "table": true, "thead": true,
	"tbody": true, "tfoot": true, "tr": true, "td": true, "th": true,
	"form": true, "fieldset": true, "legend": true, "figure": true,
	"figcaption": true, "header": true, "footer": true, "nav": true,
	"main": true, "section": true, "article": true, "aside": true,
	"address": true, "hr": true, "center": true, "details": true,
	"summary": true, "hgroup": true, "search": true, "dialog": true,
}

// blockLevelNode reports whether a box fills its containing block's width.
func blockLevelNode(n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	if n.Style != nil {
		if n.Style.Float != "" || n.Style.Position == "absolute" || n.Style.Position == "fixed" {
			return false
		}
		switch n.Style.Display {
		case "block", "list-item", "flex", "grid", "table":
			return true
		}
		return false
	}
	return blockLevelTags[strings.ToLower(n.Tag)]
}

// containingWidthPx estimates the width of a block-level box's containing
// block from ancestor widths and max-widths, defaulting to the viewport.
func (j *jsRuntime) containingWidthPx(n *Node, viewport int) int {
	w := viewport
	for p := n.Parent; p != nil && p.Type == ElementNode; p = p.Parent {
		if p.Tag == "html" || p.Tag == "body" {
			break
		}
		font := 16.0
		if p.Style != nil && p.Style.FontSize > 0 {
			font = p.Style.FontSize
		}
		if px, ok := cssLengthPx(styleGet(p, "width"), font, float64(viewport)); ok && px > 0 && px < w {
			w = px
		}
		if px, ok := cssLengthPx(styleGet(p, "max-width"), font, float64(viewport)); ok && px > 0 && px < w {
			w = px
		}
		if p.Style != nil {
			if px, ok := lengthPx(p.Style.Width, font, float64(viewport)); ok && px > 0 && px < w {
				w = px
			}
			if px, ok := lengthPx(p.Style.MaxWidth, font, float64(viewport)); ok && px > 0 && px < w {
				w = px
			}
		}
	}
	return w
}

func (j *jsRuntime) elementHeightPx(n *Node, viewport int) int {
	if n == nil {
		return 0
	}
	if n.Tag == "html" || n.Tag == "body" {
		return viewport
	}
	j.nodeStyle(n)
	font := 16.0
	if n.Style != nil && n.Style.FontSize > 0 {
		font = n.Style.FontSize
	}
	if v := styleGet(n, "height"); v != "" {
		if px, ok := cssLengthPx(v, font, float64(viewport)); ok {
			return px
		}
	}
	if n.Style != nil && n.Style.Height.Set && !n.Style.Height.IsAuto() {
		basis := 0.0
		if n.Style.Height.U == UnitPercent {
			basis = float64(viewport)
		}
		if px, ok := lengthPx(n.Style.Height, font, basis); ok {
			return px
		}
	}
	if j.host.Layout() != nil {
		if r, ok := j.host.Layout().Boxes[n]; ok && r.H > 0 {
			return CSSHeightPx(r.H)
		}
	}
	return 0
}

func styleGet(n *Node, prop string) string {
	for _, d := range ParseDeclarations(AttrValue(n, "style")) {
		if strings.EqualFold(d.Prop, prop) {
			return d.Value
		}
	}
	return ""
}

func styleSet(j *jsRuntime, n *Node, prop, value string) {
	decls := ParseDeclarations(AttrValue(n, "style"))
	found := false
	for i := range decls {
		if strings.EqualFold(decls[i].Prop, prop) {
			found = true
			if value == "" {
				decls = append(decls[:i], decls[i+1:]...)
			} else {
				decls[i].Value = value
			}
			break
		}
	}
	if !found && value != "" {
		decls = append(decls, Declaration{Prop: prop, Value: value})
	}
	parts := make([]string, 0, len(decls))
	for _, d := range decls {
		parts = append(parts, d.Prop+": "+d.Value)
	}
	j.setAttr(n, "style", strings.Join(parts, "; "))
}

func (j *jsRuntime) styleObject(n *Node) *sobek.Object {
	o := j.vm.NewObject()
	if j.cssStyleProto != nil {
		_ = o.SetPrototype(j.cssStyleProto)
	}
	j.accessor(o, "cssText", func() sobek.Value {
		return j.vm.ToValue(AttrValue(n, "style"))
	}, func(v sobek.Value) {
		j.setAttr(n, "style", v.String())
		j.markDirty()
	})
	j.method(o, "setProperty", func(call sobek.FunctionCall) sobek.Value {
		styleSet(j, n, call.Argument(0).String(), call.Argument(1).String())
		j.markDirty()
		return sobek.Undefined()
	})
	j.method(o, "getPropertyValue", func(call sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue(styleGet(n, call.Argument(0).String()))
	})
	j.method(o, "getPropertyPriority", func(sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue("")
	})
	j.method(o, "item", func(call sobek.FunctionCall) sobek.Value {
		idx := int(call.Argument(0).ToInteger())
		decls := ParseDeclarations(AttrValue(n, "style"))
		if idx >= 0 && idx < len(decls) {
			return j.vm.ToValue(decls[idx].Prop)
		}
		return j.vm.ToValue("")
	})
	j.method(o, "removeProperty", func(call sobek.FunctionCall) sobek.Value {
		prop := call.Argument(0).String()
		old := styleGet(n, prop)
		styleSet(j, n, prop, "")
		j.markDirty()
		return j.vm.ToValue(old)
	})
	for jsName, cssName := range styleProps {
		jsName, cssName := jsName, cssName
		j.accessor(o, jsName, func() sobek.Value {
			return j.vm.ToValue(styleGet(n, cssName))
		}, func(v sobek.Value) {
			styleSet(j, n, cssName, v.String())
			j.markDirty()
		})
	}
	return o
}
