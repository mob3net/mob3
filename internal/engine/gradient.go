package engine

import (
	"math"
	"strconv"
	"strings"
)

// GradientKind selects the background gradient geometry.
type GradientKind uint8

const (
	GradientLinear GradientKind = iota
	GradientRadial
)

// GradientStop is one color stop. Pos is in [0,1].
type GradientStop struct {
	Pos   float64
	Color Color
}

// BackgroundGradient is a parsed CSS linear or radial gradient background.
// Angle is in degrees; 0 points to the top, 90 to the right, 180 down.
type BackgroundGradient struct {
	Kind             GradientKind
	Angle            float64
	Repeat           bool
	CenterX, CenterY float64
	Stops            []GradientStop
}

// setBackgroundImageValue chooses the first usable background layer in a
// background-image or background value: an image URL or a gradient.
func setBackgroundImageValue(st *Style, val string) {
	if st == nil {
		return
	}
	st.BackgroundImage = ""
	st.BackgroundGradient = nil
	lower := strings.ToLower(val)
	grad, gradIdx := parseBackgroundGradient(val, st.Color)
	urlIdx := strings.Index(lower, "url(")
	if grad != nil && (urlIdx < 0 || gradIdx < urlIdx) {
		st.BackgroundGradient = grad
		return
	}
	st.BackgroundImage = backgroundURL(val)
}

var gradientFunctions = []string{
	"repeating-linear-gradient",
	"linear-gradient",
	"repeating-radial-gradient",
	"radial-gradient",
}

// parseBackgroundGradient returns the first gradient in a background value and
// its byte offset. A nil result means the value has no usable gradient.
func parseBackgroundGradient(val string, current Color) (*BackgroundGradient, int) {
	lower := strings.ToLower(val)
	best, name := -1, ""
	for _, fn := range gradientFunctions {
		if i := strings.Index(lower, fn+"("); i >= 0 && (best < 0 || i < best) {
			best, name = i, fn
		}
	}
	if best < 0 {
		return nil, -1
	}
	body, ok := gradientFunctionBody(val, best+len(name))
	if !ok {
		return nil, -1
	}
	g := &BackgroundGradient{
		Kind:    GradientLinear,
		Angle:   180,
		CenterX: 0.5,
		CenterY: 0.5,
		Repeat:  strings.HasPrefix(name, "repeating-"),
	}
	if strings.Contains(name, "radial") {
		g.Kind = GradientRadial
	}
	var stops []GradientStop
	for _, arg := range SplitTopLevel(body, ',') {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		if c, pos, ok := parseGradientStop(arg, current); ok {
			stops = append(stops, GradientStop{Pos: pos, Color: c})
			continue
		}
		if g.Kind == GradientLinear {
			if angle, ok := parseGradientAngle(arg); ok {
				g.Angle = angle
			}
		} else if cx, cy, ok := parseGradientCenter(arg); ok {
			g.CenterX, g.CenterY = cx, cy
		}
	}
	if !normalizeGradientStops(stops) {
		return nil, -1
	}
	g.Stops = stops
	return g, best
}

// gradientFunctionBody extracts the contents of a function whose opening
// parenthesis is at open (the name ends immediately before it).
func gradientFunctionBody(s string, open int) (string, bool) {
	if open < 0 || open >= len(s) || s[open] != '(' {
		return "", false
	}
	depth := 0
	start := open + 1
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[start:i], true
			}
		}
	}
	return "", false
}

func parseGradientStop(arg string, current Color) (Color, float64, bool) {
	fields := SplitFields(arg)
	if len(fields) == 0 {
		return Color{}, 0, false
	}
	c, ok := ParseColor(fields[0], current)
	if !ok || !c.Set {
		return Color{}, 0, false
	}
	pos := -1.0
	for _, f := range fields[1:] {
		lower := strings.ToLower(f)
		if lower == "0" {
			pos = 0
			break
		}
		if strings.HasSuffix(lower, "%") {
			if n, err := strconv.ParseFloat(strings.TrimSuffix(lower, "%"), 64); err == nil {
				pos = n / 100
				break
			}
		}
	}
	return c, pos, true
}

// normalizeGradientStops fills unspecified positions and clamps the result to
// a nondecreasing [0,1] sequence.
func normalizeGradientStops(stops []GradientStop) bool {
	if len(stops) < 2 {
		return false
	}
	if stops[0].Pos < 0 {
		stops[0].Pos = 0
	}
	if stops[len(stops)-1].Pos < 0 {
		stops[len(stops)-1].Pos = 1
	}
	for i := 0; i < len(stops); {
		if stops[i].Pos < 0 {
			i++
			continue
		}
		j := i + 1
		for j < len(stops) && stops[j].Pos < 0 {
			j++
		}
		if j >= len(stops) {
			break
		}
		for k := i + 1; k < j; k++ {
			f := float64(k-i) / float64(j-i)
			stops[k].Pos = stops[i].Pos + (stops[j].Pos-stops[i].Pos)*f
		}
		i = j
	}
	for i := range stops {
		if stops[i].Pos < 0 {
			stops[i].Pos = 0
		}
		if stops[i].Pos > 1 {
			stops[i].Pos = 1
		}
		if i > 0 && stops[i].Pos < stops[i-1].Pos {
			stops[i].Pos = stops[i-1].Pos
		}
	}
	return true
}

func parseGradientAngle(arg string) (float64, bool) {
	lower := strings.TrimSpace(strings.ToLower(arg))
	if strings.HasPrefix(lower, "to ") {
		vx, vy := 0.0, 0.0
		for _, f := range strings.Fields(strings.TrimPrefix(lower, "to ")) {
			switch f {
			case "left":
				vx = -1
			case "right":
				vx = 1
			case "top":
				vy = -1
			case "bottom":
				vy = 1
			}
		}
		if vx == 0 && vy == 0 {
			return 0, false
		}
		return math.Atan2(vx, -vy) * 180 / math.Pi, true
	}
	for _, unit := range []string{"deg", "turn", "rad", "grad"} {
		if !strings.HasSuffix(lower, unit) {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(lower, unit)), 64)
		if err != nil {
			return 0, false
		}
		switch unit {
		case "turn":
			v *= 360
		case "rad":
			v *= 180 / math.Pi
		case "grad":
			v *= 0.9
		}
		return v, true
	}
	return 0, false
}

func parseGradientCenter(arg string) (float64, float64, bool) {
	lower := strings.ToLower(arg)
	i := strings.Index(lower, "at ")
	if i < 0 {
		return 0, 0, false
	}
	if x, y, ok := parseBackgroundPosition(strings.TrimSpace(arg[i+3:])); ok {
		return x, y, true
	}
	return 0.5, 0.5, true
}

// colorAt samples the gradient at normalized box coordinates.
func (g *BackgroundGradient) colorAt(nx, ny float64) Color {
	if g == nil || len(g.Stops) == 0 {
		return Color{}
	}
	t := 0.0
	switch g.Kind {
	case GradientRadial:
		maxD := 0.0
		for _, cx := range []float64{0, 1} {
			for _, cy := range []float64{0, 1} {
				d := math.Hypot(cx-g.CenterX, cy-g.CenterY)
				if d > maxD {
					maxD = d
				}
			}
		}
		if maxD > 0 {
			t = math.Hypot(nx-g.CenterX, ny-g.CenterY) / maxD
		}
	default:
		rad := g.Angle * math.Pi / 180
		vx, vy := math.Sin(rad), -math.Cos(rad)
		minP, maxP := 0.0, 0.0
		first := true
		for _, cx := range []float64{0, 1} {
			for _, cy := range []float64{0, 1} {
				p := cx*vx + cy*vy
				if first {
					minP, maxP, first = p, p, false
				} else {
					if p < minP {
						minP = p
					}
					if p > maxP {
						maxP = p
					}
				}
			}
		}
		p := nx*vx + ny*vy
		if maxP > minP {
			t = (p - minP) / (maxP - minP)
		}
	}
	if g.Repeat {
		first, last := g.Stops[0].Pos, g.Stops[len(g.Stops)-1].Pos
		if span := last - first; span > 0 {
			t = first + math.Mod(t-first, span)
			if t < first {
				t += span
			}
		}
	}
	return gradientAt(g.Stops, t)
}

func gradientAt(stops []GradientStop, t float64) Color {
	if t <= stops[0].Pos {
		return stops[0].Color
	}
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].Pos {
			span := stops[i].Pos - stops[i-1].Pos
			if span <= 0 {
				return stops[i].Color
			}
			return mixColor(stops[i-1].Color, stops[i].Color, (t-stops[i-1].Pos)/span)
		}
	}
	return stops[len(stops)-1].Color
}

func mixColor(a, b Color, t float64) Color {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	lerp := func(x, y uint8) uint8 {
		return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t))
	}
	return Color{
		R: lerp(a.R, b.R), G: lerp(a.G, b.G), B: lerp(a.B, b.B),
		A: lerp(a.A, b.A), Set: true,
	}
}

// paintGradientFragment fills the visible rows of a background gradient.
func paintGradientFragment(g *Grid, f *Frag, y0, y1 int) {
	if g == nil || f == nil || f.Gradient == nil || f.W < 1 || f.H < 1 {
		return
	}
	x0, x1 := MaxInt(f.X, 0), MinInt(f.X+f.W, g.W)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	for row := y0; row < y1; row++ {
		if row < 0 || row >= g.H {
			continue
		}
		ny := (float64(row-f.Y) + 0.5) / float64(f.H)
		for x := x0; x < x1; x++ {
			nx := (float64(x-f.X) + 0.5) / float64(f.W)
			cell := &g.Cells[row*g.W+x]
			cell.BG = blendColorOver(cell.BG, f.Gradient.colorAt(nx, ny))
		}
	}
}

// blendColorOver composites a normal (non-premultiplied) source color over a
// terminal background.
func blendColorOver(base, over Color) Color {
	if !over.Set {
		return base
	}
	if over.A == 255 {
		return over
	}
	if !base.Set {
		base = RGB(255, 255, 255)
	}
	a := float64(over.A) / 255
	lerp := func(x, y uint8) uint8 {
		return uint8(math.Round(float64(x)*(1-a) + float64(y)*a))
	}
	return Color{R: lerp(base.R, over.R), G: lerp(base.G, over.G), B: lerp(base.B, over.B), A: 255, Set: true}
}
