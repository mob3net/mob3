package browser

import "math"

// applyDarkLayout remaps the computed palette after layout. Light backgrounds
// become dark, dark foregrounds become light, and hues are preserved so links
// and status colors remain recognisable.
func applyDarkLayout(ly *Layout) {
	if ly == nil {
		return
	}
	fgCache := make(map[Color]Color)
	bgCache := make(map[Color]Color)
	seen := make(map[*Style]bool)
	darkStyle := func(st *Style) {
		if st == nil || seen[st] {
			return
		}
		seen[st] = true
		st.Color = darkForeground(st.Color, fgCache)
		st.Background = darkBackground(st.Background, bgCache)
		st.BorderColor = darkBackground(st.BorderColor, bgCache)
	}
	for _, st := range ly.Styles {
		darkStyle(st)
	}
	for i := range ly.Frags {
		f := &ly.Frags[i]
		switch f.Kind {
		case fragText:
			darkStyle(f.Style)
		case fragFill:
			f.Color = darkBackground(f.Color, bgCache)
		}
	}
	ly.PageBG = darkBackground(ly.PageBG, bgCache)
}

// isDarkColor reports whether a resolved page background already belongs to a
// dark palette. It keeps author-supplied dark themes intact when dark mode is
// enabled.
func isDarkColor(c Color) bool {
	if !c.Set || c.A == 0 {
		return false
	}
	_, _, l := colorHSL(c)
	return l < 0.5
}

func darkForeground(c Color, cache map[Color]Color) Color {
	if !c.Set || c.A == 0 {
		return c
	}
	if v, ok := cache[c]; ok {
		return v
	}
	h, s, l := colorHSL(c)
	out := hslColor(h, s, 0.70+0.30*l, c.A)
	cache[c] = out
	return out
}

func darkBackground(c Color, cache map[Color]Color) Color {
	if !c.Set || c.A == 0 {
		return c
	}
	if v, ok := cache[c]; ok {
		return v
	}
	h, s, l := colorHSL(c)
	if l > 0.5 {
		l = 0.16 * l
	} else {
		l *= 0.70
	}
	out := hslColor(h, s, l, c.A)
	cache[c] = out
	return out
}

func colorHSL(c Color) (h, s, l float64) {
	r := float64(c.R) / 255
	g := float64(c.G) / 255
	b := float64(c.B) / 255
	max := math.Max(r, math.Max(g, b))
	min := math.Min(r, math.Min(g, b))
	l = (max + min) / 2
	if max == min {
		return 0, 0, l
	}
	d := max - min
	if l > 0.5 {
		s = d / (2 - max - min)
	} else {
		s = d / (max + min)
	}
	switch max {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h /= 6
	return h, s, l
}

func hslColor(h, s, l float64, a uint8) Color {
	if s == 0 {
		v := clampByte(l * 255)
		return Color{R: v, G: v, B: v, A: a, Set: true}
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	r := hueToRGB(p, q, h+1.0/3.0)
	g := hueToRGB(p, q, h)
	b := hueToRGB(p, q, h-1.0/3.0)
	return Color{R: clampByte(r * 255), G: clampByte(g * 255), B: clampByte(b * 255), A: a, Set: true}
}

func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	switch {
	case t < 1.0/6.0:
		return p + (q-p)*6*t
	case t < 1.0/2.0:
		return q
	case t < 2.0/3.0:
		return p + (q-p)*(2.0/3.0-t)*6
	default:
		return p
	}
}

func clampByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(math.Round(v))
}
