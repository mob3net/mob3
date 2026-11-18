package browser

import (
	"math"
	"strconv"
	"strings"
)

// Color is a straight 8-bit RGBA color. Set=false means "use the default".
type Color struct {
	R, G, B, A uint8
	Set        bool
}

// RGB builds an opaque color.
func RGB(r, g, b uint8) Color { return Color{R: r, G: g, B: b, A: 255, Set: true} }

// Transparent is a set color with zero alpha.
func Transparent() Color { return Color{Set: true} }

func (c Color) opaque() bool { return c.Set && c.A >= 128 }

var colorNames = map[string]Color{
	"aliceblue": RGB(0xf0, 0xf8, 0xff), "antiquewhite": RGB(0xfa, 0xeb, 0xd7),
	"aqua": RGB(0x00, 0xff, 0xff), "aquamarine": RGB(0x7f, 0xff, 0xd4),
	"azure": RGB(0xf0, 0xff, 0xff), "beige": RGB(0xf5, 0xf5, 0xdc),
	"bisque": RGB(0xff, 0xe4, 0xc4), "black": RGB(0, 0, 0),
	"blanchedalmond": RGB(0xff, 0xeb, 0xcd), "blue": RGB(0, 0, 255),
	"blueviolet": RGB(0x8a, 0x2b, 0xe2), "brown": RGB(0xa5, 0x2a, 0x2a),
	"burlywood": RGB(0xde, 0xb8, 0x87), "cadetblue": RGB(0x5f, 0x9e, 0xa0),
	"chartreuse": RGB(0x7f, 0xff, 0x00), "chocolate": RGB(0xd2, 0x69, 0x1e),
	"coral": RGB(0xff, 0x7f, 0x50), "cornflowerblue": RGB(0x64, 0x95, 0xed),
	"cornsilk": RGB(0xff, 0xf8, 0xdc), "crimson": RGB(0xdc, 0x14, 0x3c),
	"cyan": RGB(0x00, 0xff, 0xff), "darkblue": RGB(0x00, 0x00, 0x8b),
	"darkcyan": RGB(0x00, 0x8b, 0x8b), "darkgoldenrod": RGB(0xb8, 0x86, 0x0b),
	"darkgray": RGB(0xa9, 0xa9, 0xa9), "darkgreen": RGB(0x00, 0x64, 0x00),
	"darkgrey": RGB(0xa9, 0xa9, 0xa9), "darkkhaki": RGB(0xbd, 0xb7, 0x6b),
	"darkmagenta": RGB(0x8b, 0x00, 0x8b), "darkolivegreen": RGB(0x55, 0x6b, 0x2f),
	"darkorange": RGB(0xff, 0x8c, 0x00), "darkorchid": RGB(0x99, 0x32, 0xcc),
	"darkred": RGB(0x8b, 0x00, 0x00), "darksalmon": RGB(0xe9, 0x96, 0x7a),
	"darkseagreen": RGB(0x8f, 0xbc, 0x8f), "darkslateblue": RGB(0x48, 0x3d, 0x8b),
	"darkslategray": RGB(0x2f, 0x4f, 0x4f), "darkturquoise": RGB(0x00, 0xce, 0xd1),
	"darkviolet": RGB(0x94, 0x00, 0xd3), "deeppink": RGB(0xff, 0x14, 0x93),
	"deepskyblue": RGB(0x00, 0xbf, 0xff), "dimgray": RGB(0x69, 0x69, 0x69),
	"dodgerblue": RGB(0x1e, 0x90, 0xff), "firebrick": RGB(0xb2, 0x22, 0x22),
	"floralwhite": RGB(0xff, 0xfa, 0xf0), "forestgreen": RGB(0x22, 0x8b, 0x22),
	"fuchsia": RGB(0xff, 0x00, 0xff), "gainsboro": RGB(0xdc, 0xdc, 0xdc),
	"ghostwhite": RGB(0xf8, 0xf8, 0xff), "gold": RGB(0xff, 0xd7, 0x00),
	"goldenrod": RGB(0xda, 0xa5, 0x20), "gray": RGB(0x80, 0x80, 0x80),
	"green": RGB(0x00, 0x80, 0x00), "greenyellow": RGB(0xad, 0xff, 0x2f),
	"grey": RGB(0x80, 0x80, 0x80), "honeydew": RGB(0xf0, 0xff, 0xf0),
	"hotpink": RGB(0xff, 0x69, 0xb4), "indianred": RGB(0xcd, 0x5c, 0x5c),
	"indigo": RGB(0x4b, 0x00, 0x82), "ivory": RGB(0xff, 0xff, 0xf0),
	"khaki": RGB(0xf0, 0xe6, 0x8c), "lavender": RGB(0xe6, 0xe6, 0xfa),
	"lavenderblush": RGB(0xff, 0xf0, 0xf5), "lawngreen": RGB(0x7c, 0xfc, 0x00),
	"lemonchiffon": RGB(0xff, 0xfa, 0xcd), "lightblue": RGB(0xad, 0xd8, 0xe6),
	"lightcoral": RGB(0xf0, 0x80, 0x80), "lightcyan": RGB(0xe0, 0xff, 0xff),
	"lightgoldenrodyellow": RGB(0xfa, 0xfa, 0xd2), "lightgray": RGB(0xd3, 0xd3, 0xd3),
	"lightgreen": RGB(0x90, 0xee, 0x90), "lightgrey": RGB(0xd3, 0xd3, 0xd3),
	"lightpink": RGB(0xff, 0xb6, 0xc1), "lightsalmon": RGB(0xff, 0xa0, 0x7a),
	"lightseagreen": RGB(0x20, 0xb2, 0xaa), "lightskyblue": RGB(0x87, 0xce, 0xfa),
	"lightslategray": RGB(0x77, 0x88, 0x99), "lightsteelblue": RGB(0xb0, 0xc4, 0xde),
	"lightyellow": RGB(0xff, 0xff, 0xe0), "lime": RGB(0x00, 0xff, 0x00),
	"limegreen": RGB(0x32, 0xcd, 0x32), "linen": RGB(0xfa, 0xf0, 0xe6),
	"magenta": RGB(0xff, 0x00, 0xff), "maroon": RGB(0x80, 0x00, 0x00),
	"mediumaquamarine": RGB(0x66, 0xcd, 0xaa), "mediumblue": RGB(0x00, 0x00, 0xcd),
	"mediumorchid": RGB(0xba, 0x55, 0xd3), "mediumpurple": RGB(0x93, 0x70, 0xdb),
	"mediumseagreen": RGB(0x3c, 0xb3, 0x71), "mediumslateblue": RGB(0x7b, 0x68, 0xee),
	"mediumspringgreen": RGB(0x00, 0xfa, 0x9a), "mediumturquoise": RGB(0x48, 0xd1, 0xcc),
	"mediumvioletred": RGB(0xc7, 0x15, 0x85), "midnightblue": RGB(0x19, 0x19, 0x70),
	"mintcream": RGB(0xf5, 0xff, 0xfa), "mistyrose": RGB(0xff, 0xe4, 0xe1),
	"moccasin": RGB(0xff, 0xe4, 0xb5), "navajowhite": RGB(0xff, 0xde, 0xad),
	"navy": RGB(0x00, 0x00, 0x80), "oldlace": RGB(0xfd, 0xf5, 0xe6),
	"olive": RGB(0x80, 0x80, 0x00), "olivedrab": RGB(0x6b, 0x8e, 0x23),
	"orange": RGB(0xff, 0xa5, 0x00), "orangered": RGB(0xff, 0x45, 0x00),
	"orchid": RGB(0xda, 0x70, 0xd6), "palegoldenrod": RGB(0xee, 0xe8, 0xaa),
	"palegreen": RGB(0x98, 0xfb, 0x98), "paleturquoise": RGB(0xaf, 0xee, 0xee),
	"palevioletred": RGB(0xdb, 0x70, 0x93), "papayawhip": RGB(0xff, 0xef, 0xd5),
	"peachpuff": RGB(0xff, 0xda, 0xb9), "peru": RGB(0xcd, 0x85, 0x3f),
	"pink": RGB(0xff, 0xc0, 0xcb), "plum": RGB(0xdd, 0xa0, 0xdd),
	"powderblue": RGB(0xb0, 0xe0, 0xe6), "purple": RGB(0x80, 0x00, 0x80),
	"rebeccapurple": RGB(0x66, 0x33, 0x99), "red": RGB(0xff, 0x00, 0x00),
	"rosybrown": RGB(0xbc, 0x8f, 0x8f), "royalblue": RGB(0x41, 0x69, 0xe1),
	"saddlebrown": RGB(0x8b, 0x45, 0x13), "salmon": RGB(0xfa, 0x80, 0x72),
	"sandybrown": RGB(0xf4, 0xa4, 0x60), "seagreen": RGB(0x2e, 0x8b, 0x57),
	"seashell": RGB(0xff, 0xf5, 0xee), "sienna": RGB(0xa0, 0x52, 0x2d),
	"silver": RGB(0xc0, 0xc0, 0xc0), "skyblue": RGB(0x87, 0xce, 0xeb),
	"slateblue": RGB(0x6a, 0x5a, 0xcd), "slategray": RGB(0x70, 0x80, 0x90),
	"slategrey": RGB(0x70, 0x80, 0x90), "snow": RGB(0xff, 0xfa, 0xfa),
	"springgreen": RGB(0x00, 0xff, 0x7f), "steelblue": RGB(0x46, 0x82, 0xb4),
	"tan": RGB(0xd2, 0xb4, 0x8c), "teal": RGB(0x00, 0x80, 0x80),
	"thistle": RGB(0xd8, 0xbf, 0xd8), "tomato": RGB(0xff, 0x63, 0x47),
	"turquoise": RGB(0x40, 0xe0, 0xd0), "violet": RGB(0xee, 0x82, 0xee),
	"wheat": RGB(0xf5, 0xde, 0xb3), "white": RGB(0xff, 0xff, 0xff),
	"whitesmoke": RGB(0xf5, 0xf5, 0xf5), "yellow": RGB(0xff, 0xff, 0x00),
	"yellowgreen": RGB(0x9a, 0xcd, 0x32),
}

func parseColor(v string, current Color) (Color, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return Color{}, false
	}
	if v == "transparent" {
		return Transparent(), true
	}
	if v == "currentcolor" || v == "inherit" {
		return current, true
	}
	if c, ok := colorNames[v]; ok {
		return c, true
	}
	if strings.HasPrefix(v, "#") {
		h := v[1:]
		hex := func(s string) (uint8, bool) {
			n, err := strconv.ParseUint(s, 16, 8)
			return uint8(n), err == nil
		}
		switch len(h) {
		case 3, 4:
			r, ok1 := hex(h[0:1] + h[0:1])
			g, ok2 := hex(h[1:2] + h[1:2])
			b, ok3 := hex(h[2:3] + h[2:3])
			a := uint8(255)
			ok4 := true
			if len(h) == 4 {
				a, ok4 = hex(h[3:4] + h[3:4])
			}
			if ok1 && ok2 && ok3 && ok4 {
				return Color{R: r, G: g, B: b, A: a, Set: true}, true
			}
		case 6, 8:
			r, ok1 := hex(h[0:2])
			g, ok2 := hex(h[2:4])
			b, ok3 := hex(h[4:6])
			a := uint8(255)
			ok4 := true
			if len(h) == 8 {
				a, ok4 = hex(h[6:8])
			}
			if ok1 && ok2 && ok3 && ok4 {
				return Color{R: r, G: g, B: b, A: a, Set: true}, true
			}
		}
		return Color{}, false
	}
	if strings.HasPrefix(v, "rgb(") || strings.HasPrefix(v, "rgba(") {
		inner := v[strings.IndexByte(v, '(')+1:]
		inner = strings.TrimSuffix(inner, ")")
		parts := splitTopLevel(inner, ',')
		if len(parts) < 3 {
			parts = splitFields(inner)
		}
		if len(parts) >= 3 {
			ch := func(s string) (uint8, bool) {
				s = strings.TrimSpace(s)
				if strings.HasSuffix(s, "%") {
					f, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
					if err != nil {
						return 0, false
					}
					return uint8(math.Round(clamp(f, 0, 100) * 2.55)), true
				}
				f, err := strconv.ParseFloat(s, 64)
				if err != nil {
					return 0, false
				}
				return uint8(math.Round(clamp(f, 0, 255))), true
			}
			r, a := ch(parts[0])
			g, b := ch(parts[1])
			bl, c := ch(parts[2])
			if a && b && c {
				alpha := uint8(255)
				if len(parts) >= 4 {
					s := strings.TrimSpace(parts[3])
					if strings.HasSuffix(s, "%") {
						f, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
						if err == nil {
							alpha = uint8(math.Round(clamp(f, 0, 100) * 2.55))
						}
					} else if f, err := strconv.ParseFloat(s, 64); err == nil {
						alpha = uint8(math.Round(clamp(f, 0, 1) * 255))
					}
				}
				return Color{R: r, G: g, B: bl, A: alpha, Set: true}, true
			}
		}
		return Color{}, false
	}
	if strings.HasPrefix(v, "hsl(") || strings.HasPrefix(v, "hsla(") {
		inner := v[strings.IndexByte(v, '(')+1:]
		inner = strings.TrimSuffix(inner, ")")
		parts := splitTopLevel(inner, ',')
		if len(parts) >= 3 {
			h, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
			s, err2 := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(parts[1]), "%"), 64)
			l, err3 := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(parts[2]), "%"), 64)
			if err1 == nil && err2 == nil && err3 == nil {
				r, g, b := hslToRGB(h, s/100, l/100)
				return Color{R: r, G: g, B: b, A: 255, Set: true}, true
			}
		}
	}
	return Color{}, false
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func hslToRGB(h, s, l float64) (uint8, uint8, uint8) {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	c := (1 - math.Abs(2*l-1)) * s
	hp := h / 60
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))
	var r, g, b float64
	switch {
	case hp < 1:
		r, g, b = c, x, 0
	case hp < 2:
		r, g, b = x, c, 0
	case hp < 3:
		r, g, b = 0, c, x
	case hp < 4:
		r, g, b = 0, x, c
	case hp < 5:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	m := l - c/2
	return uint8(math.Round((r + m) * 255)), uint8(math.Round((g + m) * 255)), uint8(math.Round((b + m) * 255))
}
