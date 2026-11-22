package engine

import (
	"strings"
)

// mediaMatches reports whether a comma-separated media query list applies to
// a screen viewport of viewportPx CSS pixels. A non-positive width means the
// viewport is unknown; only print-only rules are excluded then.
func mediaMatches(query string, viewportPx int, dark bool) bool {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return true
	}
	for _, part := range SplitTopLevel(query, ',') {
		if mediaQueryMatches(strings.TrimSpace(part), viewportPx, dark) {
			return true
		}
	}
	return false
}

func mediaQueryMatches(q string, vw int, dark bool) bool {
	if q == "" {
		return true
	}
	neg := false
	if strings.HasPrefix(q, "not ") {
		neg = true
		q = strings.TrimSpace(q[4:])
	}
	if strings.HasPrefix(q, "only ") {
		q = strings.TrimSpace(q[5:])
	}
	match := true
	for _, term := range strings.Split(q, " and ") {
		term = strings.TrimSpace(term)
		switch term {
		case "", "all", "screen":
		case "print", "speech":
			match = false
		default:
			if strings.HasPrefix(term, "(") && strings.HasSuffix(term, ")") {
				if !mediaFeatureMatches(term[1:len(term)-1], vw, dark) {
					match = false
				}
			} else {
				match = false
			}
		}
	}
	if neg {
		return !match
	}
	return match
}

func mediaFeatureMatches(f string, vw int, dark bool) bool {
	f = strings.TrimSpace(f)
	if strings.ContainsAny(f, "<>") {
		op := ""
		var left, right string
		if i := strings.IndexAny(f, "<>"); i >= 0 {
			left = strings.TrimSpace(f[:i])
			right = strings.TrimSpace(f[i+1:])
			op = string(f[i])
			if strings.HasPrefix(right, "=") {
				op += "="
				right = strings.TrimSpace(right[1:])
			}
		}
		if vw <= 0 {
			return true
		}
		var width bool
		switch left {
		case "width", "device-width":
			width = true
		case "height", "device-height":
			return true
		}
		if !width {
			return false
		}
		px := mediaPx(right)
		switch op {
		case ">":
			return vw > px
		case ">=":
			return vw >= px
		case "<":
			return vw < px
		case "<=":
			return vw <= px
		}
		return false
	}
	if k, v, ok := strings.Cut(f, ":"); ok {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		// The color scheme is independent of the viewport, so it is checked
		// before the unknown-width shortcut below.
		if k == "prefers-color-scheme" {
			if dark {
				return v == "dark"
			}
			return v == "light" || v == "no-preference"
		}
		if vw <= 0 {
			return true
		}
		switch k {
		case "min-width", "min-device-width":
			return vw >= mediaPx(v)
		case "max-width", "max-device-width":
			return vw <= mediaPx(v)
		case "width", "device-width":
			return vw == mediaPx(v)
		case "orientation", "hover", "any-hover", "pointer", "any-pointer",
			"min-height", "max-height", "height", "min-resolution",
			"max-resolution", "resolution":
			return true
		}
		return false
	}
	switch f {
	case "color", "monochrome", "grid":
		return true
	}
	return false
}

func mediaPx(v string) int {
	if l := ParseLength(v, 16); l.Set {
		return int(l.CSSPx(16))
	}
	return 0
}

// MediaMatches evaluates a media query against the viewport and dark mode.
func MediaMatches(query string, viewportPx int, dark bool) bool {
	return mediaMatches(query, viewportPx, dark)
}

// MediaFeatureMatches evaluates one media feature.
func MediaFeatureMatches(feature string, viewportPx int, dark bool) bool {
	return mediaFeatureMatches(feature, viewportPx, dark)
}
