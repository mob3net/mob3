package browser

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

const (
	cellW = 8.0  // CSS pixels per terminal column
	cellH = 16.0 // CSS pixels per terminal row
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

// Unit is a CSS length unit.
type Unit int

const (
	UnitNone Unit = iota
	UnitPX
	UnitEM
	UnitREM
	UnitPercent
	UnitPT
	UnitCH
)

// Length is a CSS length or auto.
type Length struct {
	V    float64
	U    Unit
	Set  bool
	Auto bool
}

func px(v float64) Length     { return Length{V: v, U: UnitPX, Set: true} }
func pct(v float64) Length    { return Length{V: v, U: UnitPercent, Set: true} }
func (l Length) IsAuto() bool { return !l.Set || l.Auto }
func (l Length) cssPx(fontPx float64) float64 {
	switch l.U {
	case UnitPX:
		return l.V
	case UnitEM:
		return l.V * fontPx
	case UnitREM:
		return l.V * 16
	case UnitPT:
		return l.V * 96 / 72
	case UnitCH:
		return l.V * cellW
	}
	return 0
}

// resolveH resolves to terminal columns.
func (l Length) resolveH(basis int, fontPx float64) int {
	if !l.Set {
		return 0
	}
	if l.U == UnitPercent {
		return int(math.Round(float64(basis) * l.V / 100))
	}
	return int(math.Round(l.cssPx(fontPx) / cellW))
}

// resolveV resolves to terminal rows.
func (l Length) resolveV(basis int, fontPx float64) int {
	if !l.Set {
		return 0
	}
	if l.U == UnitPercent {
		return int(math.Round(float64(basis) * l.V / 100))
	}
	return int(math.Round(l.cssPx(fontPx) / cellH))
}

// Style is a computed style. Lengths remain unresolved so percentages can be
// resolved against the containing block during layout.
type Style struct {
	Display       string
	Hidden        bool
	Color         Color
	Background    Color
	FontSize      float64
	Bold          bool
	Italic        bool
	Underline     bool
	Strike        bool
	TextTransform string
	TextAlign     string
	TextIndent    Length
	WhiteSpace    string
	LineHeight    float64 // multiplier
	Margin        [4]Length
	Padding       [4]Length
	Border        [4]Length
	BorderColor   Color
	Width         Length
	Height        Length
	MaxWidth      Length
	ListStyle     string
	Overflow      string
	Position      string
	Clip          string
	FlexDir       string
	FlexGrow      float64
	Gap           Length
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
		ListStyle:   "disc",
		BorderColor: RGB(0xd0, 0xd7, 0xde),
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
	}
	return st
}

// UA stylesheet: base document styling expressed in the same CSS subset the
// engine understands.
const uaStylesheet = `
html { display:block; background:#ffffff; color:#1f2328; font-size:16px; line-height:1; }
body { display:block; margin:8px; padding:0; background:#ffffff; color:#1f2328; }
head, script, style, title, meta, link, base, template, param, source, track { display:none; }
div, p, h1, h2, h3, h4, h5, h6, ul, ol, li, dl, dt, dd, blockquote, pre,
table, thead, tbody, tfoot, tr, td, th, form, fieldset, legend, figure,
figcaption, header, footer, nav, main, section, article, aside, address,
hr, center, details, summary, dialog, hgroup, search { display:block; }
p { margin:8px 0; }
h1 { font-size:32px; font-weight:bold; margin:8px 0; }
h2 { font-size:24px; font-weight:bold; margin:8px 0; }
h3 { font-size:19px; font-weight:bold; margin:8px 0 4px; }
h4, h5, h6 { font-size:16px; font-weight:bold; margin:8px 0 4px; }
b, strong, th { font-weight:bold; }
i, em, cite, var, q { font-style:italic; }
u, ins { text-decoration:underline; }
s, del, strike { text-decoration:line-through; }
a { display:inline; color:#0645ad; text-decoration:underline; }
pre, code, kbd, samp, tt { font-family:monospace; }
pre { white-space:pre; margin:8px 0; padding:4px; background:#f6f8fa; }
code { background:#f6f8fa; }
blockquote { margin:8px 12px; border-left:3px solid #d0d7de; padding-left:8px; color:#57606a; }
ul, ol { margin:8px 0; padding-left:20px; }
ul { list-style-type:disc; }
ol { list-style-type:decimal; }
li { display:list-item; margin:2px 0; }
hr { display:block; border-top:1px solid #d0d7de; margin:8px 0; height:0; }
table { display:table; margin:4px 0; border-color:#d0d7de; }
thead, tbody, tfoot { display:block; }
tr { display:table-row; }
td, th { display:table-cell; padding:2px 4px; border:1px solid #d0d7de; }
th { text-align:center; background:#f3f4f6; }
caption { display:block; font-weight:bold; text-align:center; }
img { display:inline-block; }
input, select, textarea, button { display:inline-block; border:1px solid #8c959f; padding:0 1px; background:#f6f8fa; color:#1f2328; }
small { font-size:12px; }
big { font-size:20px; }
sub, sup { font-size:12px; }
center { text-align:center; }
`

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

func parseLength(v string, fontPx float64) Length {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "auto" {
		return Length{Auto: true}
	}
	if v == "" || v == "none" || v == "normal" {
		return Length{}
	}
	if v == "0" {
		return px(0)
	}
	units := []struct {
		suffix string
		unit   Unit
		mul    float64
	}{
		{"px", UnitPX, 1}, {"pt", UnitPT, 1}, {"em", UnitEM, 1},
		{"rem", UnitREM, 1}, {"ch", UnitCH, 1}, {"%", UnitPercent, 1},
	}
	for _, u := range units {
		if strings.HasSuffix(v, u.suffix) {
			num := strings.TrimSpace(strings.TrimSuffix(v, u.suffix))
			f, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return Length{}
			}
			return Length{V: f * u.mul, U: u.unit, Set: true}
		}
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return Length{V: f, U: UnitPX, Set: true}
	}
	return Length{}
}

func parseFontSize(v string, parentPx float64) (float64, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	keywords := map[string]float64{
		"xx-small": 9, "x-small": 10, "small": 13, "medium": 16,
		"large": 18, "x-large": 24, "xx-large": 32, "smaller": parentPx * 0.83,
		"larger": parentPx * 1.2,
	}
	if f, ok := keywords[v]; ok {
		return f, true
	}
	if strings.HasSuffix(v, "%") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
		if err == nil {
			return parentPx * f / 100, true
		}
		return 0, false
	}
	if strings.HasSuffix(v, "em") && !strings.HasSuffix(v, "rem") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(v, "em"), 64)
		if err == nil {
			return parentPx * f, true
		}
		return 0, false
	}
	if strings.HasSuffix(v, "rem") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(v, "rem"), 64)
		if err == nil {
			return 16 * f, true
		}
		return 0, false
	}
	if l := parseLength(v, parentPx); l.Set {
		return l.cssPx(parentPx), true
	}
	return 0, false
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
		dst[0], dst[2] = ls[0], ls[1]
		dst[1], dst[3] = ls[1], ls[1]
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
	for _, d := range decls {
		if d.Important != important {
			continue
		}
		applyDecl(st, d.Prop, d.Value, parent)
	}
}

func applyDecl(st *Style, prop, val string, parent *Style) {
	prop = strings.ToLower(strings.TrimSpace(prop))
	val = strings.TrimSpace(val)
	if val == "" {
		return
	}
	lower := strings.ToLower(val)
	switch prop {
	case "display":
		switch lower {
		case "none":
			st.Display = "none"
		case "block", "flow-root", "flex", "grid", "list-item", "table",
			"table-row", "table-cell", "table-caption", "table-row-group",
			"table-header-group", "table-footer-group", "inline-block",
			"inline-flex", "inline-grid", "inline-table", "contents":
			switch lower {
			case "flow-root", "grid":
				st.Display = "block"
			case "flex", "inline-flex":
				st.Display = "flex"
			case "inline-grid", "contents":
				st.Display = "inline"
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
		for i, f := range fields {
			fl := strings.ToLower(f)
			if fl == "bold" || fl == "bolder" {
				st.Bold = true
			}
			if fl == "italic" || fl == "oblique" {
				st.Italic = true
			}
			sizePart := f
			lh := ""
			if j := strings.IndexByte(sizePart, '/'); j >= 0 {
				lh = sizePart[j+1:]
				sizePart = sizePart[:j]
			}
			if sz, ok := parseFontSize(sizePart, parentSize(parent)); ok && i < 4 {
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
	case "text-indent":
		st.TextIndent = parseLength(val, st.FontSize)
	case "text-transform":
		switch lower {
		case "uppercase", "lowercase", "capitalize", "none":
			st.TextTransform = lower
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
		if lower == "none" || lower == "hidden" {
			for i := range st.Border {
				st.Border[i] = Length{}
			}
		}
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
	case "max-width":
		if lower == "none" {
			st.MaxWidth = Length{}
		} else {
			st.MaxWidth = parseLength(val, st.FontSize)
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
	case "flex-grow":
		if f, err := strconv.ParseFloat(lower, 64); err == nil && f > 0 {
			st.FlexGrow = f
		}
	case "flex":
		fields := splitFields(lower)
		if len(fields) > 0 {
			if f, err := strconv.ParseFloat(fields[0], 64); err == nil && f > 0 {
				st.FlexGrow = f
			}
		}
	case "gap", "column-gap":
		if l := parseLength(val, st.FontSize); l.Set {
			st.Gap = l
		}
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
		if lower == "hidden" || lower == "clip" {
			st.Overflow = "hidden"
		}
	case "position":
		st.Position = lower
	case "clip":
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
	for _, f := range splitFields(val) {
		fl := strings.ToLower(f)
		if isBorderStyle(fl) {
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
		} else {
			st.Border[i] = w
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

type styleEngine struct {
	ua         []Rule
	author     []Rule
	viewportPx int
}

// uaRules is parsed once; rebuilding layout must not re-parse the UA sheet.
var uaRules = ParseStylesheet(uaStylesheet)

func newStyleEngine(author []Rule, viewportPx int) *styleEngine {
	return &styleEngine{ua: uaRules, author: author, viewportPx: viewportPx}
}

func (e *styleEngine) computeAll(root *Node) map[*Node]*Style {
	styles := make(map[*Node]*Style)
	base := defaultStyle()
	var walk func(n *Node, parent *Style)
	walk = func(n *Node, parent *Style) {
		if n.Type == DocumentNode {
			for _, c := range n.Children {
				walk(c, base)
			}
			return
		}
		if n.Type == TextNode {
			if parent != nil {
				n.Style = parent
			}
			return
		}
		st := inheritStyle(parent)
		applyMatching(e.ua, n, st, parent, e.viewportPx)
		ApplyPresentationAttrs(n, st)
		applyMatching(e.author, n, st, parent, e.viewportPx)
		if inline := n.Attr("style"); inline != "" {
			decls := ParseDeclarations(inline)
			applyDecls(st, decls, parent, false, parentSize(parent))
			applyDecls(st, decls, parent, true, parentSize(parent))
		}
		// Common screen-reader-only pattern: a 1px clipped box. Rendering it
		// would produce one character per line, so hide it.
		if st.Overflow == "hidden" && st.Width.Set && st.Width.U == UnitPX && st.Width.V <= 1 &&
			st.Height.Set && st.Height.U == UnitPX && st.Height.V <= 1 {
			st.Display = "none"
		}
		n.Style = st
		styles[n] = st
		for _, c := range n.Children {
			walk(c, st)
		}
	}
	walk(root, base)
	return styles
}

func applyMatching(rules []Rule, n *Node, st *Style, parent *Style, viewportPx int) {
	var matched []Rule
	for _, r := range rules {
		if r.Media != "" && !mediaMatches(r.Media, viewportPx) {
			continue
		}
		if r.Selector.Match(n) {
			matched = append(matched, r)
		}
	}
	if len(matched) == 0 {
		return
	}
	sort.SliceStable(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]
		for k := 0; k < 3; k++ {
			if a.Selector.Spec[k] != b.Selector.Spec[k] {
				return a.Selector.Spec[k] < b.Selector.Spec[k]
			}
		}
		return a.Order < b.Order
	})
	base := parentSize(parent)
	for _, r := range matched {
		applyDecls(st, r.Decls, parent, false, base)
	}
	for _, r := range matched {
		applyDecls(st, r.Decls, parent, true, base)
	}
}
