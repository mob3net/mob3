package engine

import (
	"math"
	"strconv"
	"strings"
)

// Unit is a CSS length unit.
type Unit int

// Length is a CSS length or auto.
type Length struct {
	V    float64
	U    Unit
	Set  bool
	Auto bool
	// Calc marks a simple calc() result as the linear combination
	// CalcPct percent of the basis plus CalcPx CSS pixels.
	Calc    bool
	CalcPct float64
	CalcPx  float64
	// CalcVW/CalcVH/CalcVMin/CalcVMax are the viewport components of a
	// calc() expression. resolveStyleUnits folds them into CalcPx once the
	// layout viewport is known.
	CalcVW, CalcVH, CalcVMin, CalcVMax float64
	// Func is a min()/max()/clamp() expression. Like calc() it resolves
	// against the containing-block basis.
	Func *LengthFunc
}

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

var (
	cellW      = baseCellW
	cellH      = baseCellH
	layoutZoom = 1.0
)

// SetZoom installs a session zoom factor. Every length resolution in the
// engine reads the resulting cell size.
func SetZoom(z float64) {
	if z <= 0 {
		z = 1
	}
	layoutZoom = z
	cellW = baseCellW / z
	cellH = baseCellH / z
}

// Zoom returns the current layout zoom.
func Zoom() float64 { return layoutZoom }

// CellW and CellH return the current CSS pixels per terminal column and row.
func CellW() float64 { return cellW }
func CellH() float64 { return cellH }

// LengthFuncKind identifies a CSS length function.
type LengthFuncKind int

const (
	LengthFuncMin LengthFuncKind = iota
	LengthFuncMax
	LengthFuncClamp
)

// LengthTerm is one argument of a length function: a percentage of the basis
// plus pixels, viewport components resolved after the cascade, or a nested
// function.
type LengthTerm struct {
	Pct, Px            float64
	VW, VH, VMin, VMax float64
	Fn                 *LengthFunc
}

// LengthFunc is a min()/max()/clamp() expression resolved against a basis.
type LengthFunc struct {
	Kind  LengthFuncKind
	Terms []LengthTerm
}

// px evaluates the function in CSS pixels for a percentage basis in pixels.
func (f *LengthFunc) PX(basis float64) float64 {
	if f == nil {
		return 0
	}
	switch f.Kind {
	case LengthFuncClamp:
		if len(f.Terms) != 3 {
			return 0
		}
		lo := f.Term(f.Terms[0], basis)
		v := f.Term(f.Terms[1], basis)
		hi := f.Term(f.Terms[2], basis)
		if v < lo {
			v = lo
		}
		if v > hi {
			v = hi
		}
		return v
	case LengthFuncMin, LengthFuncMax:
		if len(f.Terms) == 0 {
			return 0
		}
		v := f.Term(f.Terms[0], basis)
		for i := 1; i < len(f.Terms); i++ {
			x := f.Term(f.Terms[i], basis)
			if (f.Kind == LengthFuncMin && x < v) || (f.Kind == LengthFuncMax && x > v) {
				v = x
			}
		}
		return v
	}
	return 0
}

func (f *LengthFunc) Term(t LengthTerm, basis float64) float64 {
	if t.Fn != nil {
		return t.Fn.PX(basis)
	}
	return t.Pct*basis/100 + t.Px
}

// resolveViewport folds viewport-unit terms into pixels once the layout
// viewport is known.
func (f *LengthFunc) ResolveViewport(vw, vh float64) {
	if f == nil {
		return
	}
	for i := range f.Terms {
		t := &f.Terms[i]
		if t.Fn != nil {
			t.Fn.ResolveViewport(vw, vh)
			continue
		}
		t.Px += t.VW/100*vw + t.VH/100*vh +
			t.VMin/100*math.Min(vw, vh) + t.VMax/100*math.Max(vw, vh)
		t.VW, t.VH, t.VMin, t.VMax = 0, 0, 0, 0
	}
}

func PX(v float64) Length { return Length{V: v, U: UnitPX, Set: true} }

func Pct(v float64) Length { return Length{V: v, U: UnitPercent, Set: true} }

func (l Length) IsAuto() bool { return !l.Set || l.Auto }

func (l Length) CSSPx(fontPx float64) float64 {
	if l.Calc {
		return l.CalcPx
	}
	if l.Func != nil {
		// No basis is available here; percentage terms contribute nothing.
		return l.Func.PX(0)
	}
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
	case UnitLH:
		// resolveStyleUnits converts lh to px once the cascade is complete;
		// this fallback is only reached while parsing line-height itself.
		return l.V * fontPx
	}
	return 0
}

// resolveH resolves to terminal columns.
func (l Length) ResolveH(basis int, fontPx float64) int {
	if !l.Set {
		return 0
	}
	if l.Calc {
		return int(math.Round(l.CalcPx/cellW + float64(basis)*l.CalcPct/100))
	}
	if l.Func != nil {
		return int(math.Round(l.Func.PX(float64(basis)*cellW) / cellW))
	}
	if l.U == UnitPercent {
		return int(math.Round(float64(basis) * l.V / 100))
	}
	return int(math.Round(l.CSSPx(fontPx) / cellW))
}

// resolveV resolves to terminal rows.
func (l Length) ResolveV(basis int, fontPx float64) int {
	if !l.Set {
		return 0
	}
	if l.Calc {
		return int(math.Round(l.CalcPx/cellH + float64(basis)*l.CalcPct/100))
	}
	if l.Func != nil {
		return int(math.Round(l.Func.PX(float64(basis)*cellH) / cellH))
	}
	if l.U == UnitPercent {
		return int(math.Round(float64(basis) * l.V / 100))
	}
	return int(math.Round(l.CSSPx(fontPx) / cellH))
}

// ResolveLengthLH converts a computed lh length to px. lh depends on the
// element's own line-height, so this runs after the cascade rather than while
// declarations are parsed.
func ResolveLengthLH(l *Length, linePx float64) {
	if l.Set && l.U == UnitLH {
		*l = PX(l.V * linePx)
	}
}

// resolveLengthREM converts a rem length to px using the root font size.
func ResolveLengthREM(l *Length, rootPx float64) {
	if l.Set && !l.Calc && l.U == UnitREM {
		*l = PX(l.V * rootPx)
	}
}

// resolveViewportLength converts vw/vh/vmin/vmax lengths to px.
func ResolveViewportLength(l *Length, vw, vh float64) {
	if !l.Set {
		return
	}
	if l.Func != nil {
		l.Func.ResolveViewport(vw, vh)
		return
	}
	if l.Calc {
		l.CalcPx += l.CalcVW/100*vw + l.CalcVH/100*vh +
			l.CalcVMin/100*math.Min(vw, vh) + l.CalcVMax/100*math.Max(vw, vh)
		l.CalcVW, l.CalcVH, l.CalcVMin, l.CalcVMax = 0, 0, 0, 0
		return
	}
	var px float64
	switch l.U {
	case UnitVW:
		px = l.V / 100 * vw
	case UnitVH:
		px = l.V / 100 * vh
	case UnitVMin:
		px = l.V / 100 * math.Min(vw, vh)
	case UnitVMax:
		px = l.V / 100 * math.Max(vw, vh)
	default:
		return
	}
	*l = Length{V: px, U: UnitPX, Set: true}
}

func ParseLength(v string, fontPx float64) Length {
	v = strings.ToLower(strings.TrimSpace(v))
	if strings.HasPrefix(v, "calc(") && strings.HasSuffix(v, ")") {
		return ParseCalcLength(v[len("calc("):len(v)-1], fontPx)
	}
	if strings.HasPrefix(v, "min(") || strings.HasPrefix(v, "max(") || strings.HasPrefix(v, "clamp(") {
		if l, ok := parseLengthFunction(v, fontPx); ok {
			return l
		}
		return Length{}
	}
	return ParseLengthSimple(v)
}

// ParseLengthSimple parses a plain length token, without calc() or the
// min()/max()/clamp() functions.
func ParseLengthSimple(v string) Length {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "auto" {
		return Length{Auto: true}
	}
	if v == "" || v == "none" || v == "normal" {
		return Length{}
	}
	if v == "0" {
		return PX(0)
	}
	units := []struct {
		suffix string
		unit   Unit
		mul    float64
	}{
		// Longer suffixes must come first: "1rem" also ends with "em".
		{"vmin", UnitVMin, 1}, {"vmax", UnitVMax, 1},
		{"rem", UnitREM, 1}, {"em", UnitEM, 1},
		{"px", UnitPX, 1}, {"pt", UnitPT, 1}, {"ch", UnitCH, 1},
		{"lh", UnitLH, 1}, {"vw", UnitVW, 1}, {"vh", UnitVH, 1},
		{"dvw", UnitVW, 1}, {"dvh", UnitVH, 1},
		{"svw", UnitVW, 1}, {"svh", UnitVH, 1},
		{"lvw", UnitVW, 1}, {"lvh", UnitVH, 1},
		{"%", UnitPercent, 1},
	}
	for _, u := range units {
		if strings.HasSuffix(v, u.suffix) {
			num := strings.TrimSpace(strings.TrimSuffix(v, u.suffix))
			f, err := strconv.ParseFloat(num, 64)
			if err != nil || !FiniteNumber(f) {
				return Length{}
			}
			return Length{V: f * u.mul, U: u.unit, Set: true}
		}
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil && FiniteNumber(f) {
		return Length{V: f, U: UnitPX, Set: true}
	}
	return Length{}
}

// FiniteNumber reports whether f is a usable CSS number. Scripts sometimes
// write NaN or Infinity into inline styles; browsers ignore such declarations,
// so the same values must not become layout geometry here.
func FiniteNumber(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// NonFiniteLength reports whether a declaration value is a NaN or Infinity
// length. Those declarations are dropped so an earlier rule keeps its value.
func NonFiniteLength(val string) bool {
	v := strings.ToLower(strings.TrimSpace(val))
	if v == "" {
		return false
	}
	if strings.HasPrefix(v, "calc(") || strings.HasPrefix(v, "min(") ||
		strings.HasPrefix(v, "max(") || strings.HasPrefix(v, "clamp(") {
		for _, bad := range []string{"nan", "infinity", "inf"} {
			if strings.Contains(v, bad) {
				return true
			}
		}
		return false
	}
	num := v
	for _, u := range []string{"vmin", "vmax", "rem", "em", "px", "pt", "ch", "lh", "vw", "vh", "%"} {
		if strings.HasSuffix(num, u) {
			num = strings.TrimSpace(strings.TrimSuffix(num, u))
			break
		}
	}
	f, err := strconv.ParseFloat(num, 64)
	return err == nil && !FiniteNumber(f)
}

// LengthProperty reports whether prop takes a length, so an invalid numeric
// value can be dropped instead of clearing a previously declared length.
func LengthProperty(prop string) bool {
	for _, part := range []string{"width", "height", "margin", "padding", "gap", "top", "right", "bottom", "left", "size", "spacing", "indent", "basis", "radius", "inset"} {
		if strings.Contains(prop, part) {
			return true
		}
	}
	return prop == "line-height"
}

// calcValue is a simple calc() value: a percentage of the basis plus a CSS
// pixel length. scalar marks a unitless number used only inside * and /.
type calcValue struct {
	pct, px            float64
	vw, vh, vmin, vmax float64
	scalar             bool
}

// parseCalcLength evaluates the simple arithmetic that covers common CSS
// calc() uses: percentages and absolute units combined with + - * / and
// parentheses. Font-relative units are resolved with fontPx.
func ParseCalcLength(expr string, fontPx float64) Length {
	pos := 0
	v, ok := parseCalcSum(expr, &pos, fontPx)
	skipCalcSpace(expr, &pos)
	if !ok || pos != len(expr) || v.scalar ||
		!FiniteNumber(v.pct) || !FiniteNumber(v.px) ||
		!FiniteNumber(v.vw) || !FiniteNumber(v.vh) ||
		!FiniteNumber(v.vmin) || !FiniteNumber(v.vmax) {
		return Length{}
	}
	return Length{
		Set: true, Calc: true, CalcPct: v.pct, CalcPx: v.px,
		CalcVW: v.vw, CalcVH: v.vh, CalcVMin: v.vmin, CalcVMax: v.vmax,
	}
}

func skipCalcSpace(s string, pos *int) {
	for *pos < len(s) && (s[*pos] == ' ' || s[*pos] == '\t' || s[*pos] == '\n' || s[*pos] == '\r') {
		*pos++
	}
}

func parseCalcSum(s string, pos *int, fontPx float64) (calcValue, bool) {
	left, ok := parseCalcProduct(s, pos, fontPx)
	if !ok {
		return calcValue{}, false
	}
	for {
		skipCalcSpace(s, pos)
		if *pos >= len(s) || (s[*pos] != '+' && s[*pos] != '-') {
			break
		}
		op := s[*pos]
		*pos++
		right, ok := parseCalcProduct(s, pos, fontPx)
		if !ok || left.scalar != right.scalar {
			return calcValue{}, false
		}
		if op == '+' {
			left.pct += right.pct
			left.px += right.px
			left.vw += right.vw
			left.vh += right.vh
			left.vmin += right.vmin
			left.vmax += right.vmax
		} else {
			left.pct -= right.pct
			left.px -= right.px
			left.vw -= right.vw
			left.vh -= right.vh
			left.vmin -= right.vmin
			left.vmax -= right.vmax
		}
	}
	return left, true
}

func parseCalcProduct(s string, pos *int, fontPx float64) (calcValue, bool) {
	left, ok := parseCalcFactor(s, pos, fontPx)
	if !ok {
		return calcValue{}, false
	}
	for {
		skipCalcSpace(s, pos)
		if *pos >= len(s) || (s[*pos] != '*' && s[*pos] != '/') {
			break
		}
		op := s[*pos]
		*pos++
		right, ok := parseCalcFactor(s, pos, fontPx)
		if !ok {
			return calcValue{}, false
		}
		if op == '*' {
			switch {
			case left.scalar && right.scalar:
				left.px *= right.px
			case left.scalar:
				k := left.px
				left = calcValue{
					pct: right.pct * k, px: right.px * k, vw: right.vw * k,
					vh: right.vh * k, vmin: right.vmin * k, vmax: right.vmax * k,
				}
			case right.scalar:
				left.pct *= right.px
				left.px *= right.px
				left.vw *= right.px
				left.vh *= right.px
				left.vmin *= right.px
				left.vmax *= right.px
			default:
				return calcValue{}, false
			}
		} else {
			if !right.scalar || right.px == 0 {
				return calcValue{}, false
			}
			if left.scalar {
				left.px /= right.px
			} else {
				left.pct /= right.px
				left.px /= right.px
				left.vw /= right.px
				left.vh /= right.px
				left.vmin /= right.px
				left.vmax /= right.px
			}
		}
	}
	return left, true
}

func parseCalcFactor(s string, pos *int, fontPx float64) (calcValue, bool) {
	skipCalcSpace(s, pos)
	sign := 1.0
	if *pos < len(s) && (s[*pos] == '+' || s[*pos] == '-') {
		if s[*pos] == '-' {
			sign = -1
		}
		*pos++
		skipCalcSpace(s, pos)
	}
	if *pos < len(s) && s[*pos] == '(' {
		*pos++
		v, ok := parseCalcSum(s, pos, fontPx)
		if !ok {
			return calcValue{}, false
		}
		skipCalcSpace(s, pos)
		if *pos >= len(s) || s[*pos] != ')' {
			return calcValue{}, false
		}
		*pos++
		v.pct *= sign
		v.px *= sign
		return v, true
	}
	start := *pos
	digits := false
	for *pos < len(s) && s[*pos] >= '0' && s[*pos] <= '9' {
		*pos++
		digits = true
	}
	if *pos < len(s) && s[*pos] == '.' {
		*pos++
		for *pos < len(s) && s[*pos] >= '0' && s[*pos] <= '9' {
			*pos++
			digits = true
		}
	}
	if !digits {
		return calcValue{}, false
	}
	if *pos < len(s) && (s[*pos] == 'e' || s[*pos] == 'E') {
		save := *pos
		*pos++
		if *pos < len(s) && (s[*pos] == '+' || s[*pos] == '-') {
			*pos++
		}
		exp := false
		for *pos < len(s) && s[*pos] >= '0' && s[*pos] <= '9' {
			*pos++
			exp = true
		}
		if !exp {
			*pos = save
		}
	}
	num, err := strconv.ParseFloat(s[start:*pos], 64)
	if err != nil {
		return calcValue{}, false
	}
	num *= sign
	unitStart := *pos
	for *pos < len(s) && ((s[*pos] >= 'a' && s[*pos] <= 'z') || s[*pos] == '%') {
		*pos++
	}
	unit := s[unitStart:*pos]
	if unit == "" {
		return calcValue{px: num, scalar: true}, true
	}
	if unit == "%" {
		return calcValue{pct: num}, true
	}
	u := UnitNone
	switch unit {
	case "px":
		u = UnitPX
	case "em":
		u = UnitEM
	case "rem":
		u = UnitREM
	case "pt":
		u = UnitPT
	case "ch":
		u = UnitCH
	case "lh":
		u = UnitLH
	case "vw":
		return calcValue{vw: num}, true
	case "vh":
		return calcValue{vh: num}, true
	case "vmin":
		return calcValue{vmin: num}, true
	case "vmax":
		return calcValue{vmax: num}, true
	default:
		return calcValue{}, false
	}
	l := Length{V: num, U: u, Set: true}
	return calcValue{px: l.CSSPx(fontPx)}, true
}

// splitCSSFunction splits a "name(args)" value, returning the lowercased name
// and the argument text. Nested parentheses are respected.
func splitCSSFunction(v string) (string, string, bool) {
	open := strings.IndexByte(v, '(')
	if open <= 0 || !strings.HasSuffix(v, ")") {
		return "", "", false
	}
	name := strings.ToLower(strings.TrimSpace(v[:open]))
	if name == "" {
		return "", "", false
	}
	depth := 0
	for i := open; i < len(v); i++ {
		switch v[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				if strings.TrimSpace(v[i+1:]) != "" {
					return "", "", false
				}
				return name, v[open+1 : i], true
			}
		}
	}
	return "", "", false
}

// splitCSSArgs splits function arguments at top-level commas.
func splitCSSArgs(body string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, body[start:i])
				start = i + 1
			}
		}
	}
	return append(out, body[start:])
}

// parseLengthFunction parses min(), max() or clamp() with length arguments,
// which may be plain lengths, calc() expressions, percentages, viewport units,
// or nested length functions.
func parseLengthFunction(v string, fontPx float64) (Length, bool) {
	name, body, ok := splitCSSFunction(v)
	if !ok {
		return Length{}, false
	}
	var kind LengthFuncKind
	switch name {
	case "min":
		kind = LengthFuncMin
	case "max":
		kind = LengthFuncMax
	case "clamp":
		kind = LengthFuncClamp
	default:
		return Length{}, false
	}
	args := splitCSSArgs(body)
	if kind == LengthFuncClamp {
		if len(args) != 3 {
			return Length{}, false
		}
	} else if len(args) == 0 {
		return Length{}, false
	}
	terms := make([]LengthTerm, 0, len(args))
	for _, arg := range args {
		t, ok := parseLengthTerm(arg, fontPx)
		if !ok {
			return Length{}, false
		}
		terms = append(terms, t)
	}
	return Length{Set: true, Func: &LengthFunc{Kind: kind, Terms: terms}}, true
}

func parseLengthTerm(arg string, fontPx float64) (LengthTerm, bool) {
	s := strings.ToLower(strings.TrimSpace(arg))
	if s == "" {
		return LengthTerm{}, false
	}
	if name, body, ok := splitCSSFunction(s); ok {
		switch name {
		case "min", "max", "clamp":
			l, ok := parseLengthFunction(s, fontPx)
			if !ok {
				return LengthTerm{}, false
			}
			return LengthTerm{Fn: l.Func}, true
		case "calc":
			l := ParseCalcLength(body, fontPx)
			if !l.Set || l.Func != nil {
				return LengthTerm{}, false
			}
			return termFromLength(l, fontPx), true
		}
		return LengthTerm{}, false
	}
	l := ParseLengthSimple(s)
	if !l.Set || l.Auto || l.Func != nil {
		return LengthTerm{}, false
	}
	// A non-zero unitless number is not a length. Zero is allowed anywhere.
	if f, err := strconv.ParseFloat(s, 64); err == nil && f != 0 {
		return LengthTerm{}, false
	}
	return termFromLength(l, fontPx), true
}

// termFromLength converts a parsed length into a function argument.
func termFromLength(l Length, fontPx float64) LengthTerm {
	if l.Calc {
		return LengthTerm{
			Pct: l.CalcPct, Px: l.CalcPx,
			VW: l.CalcVW, VH: l.CalcVH, VMin: l.CalcVMin, VMax: l.CalcVMax,
		}
	}
	switch l.U {
	case UnitPercent:
		return LengthTerm{Pct: l.V}
	case UnitVW:
		return LengthTerm{VW: l.V}
	case UnitVH:
		return LengthTerm{VH: l.V}
	case UnitVMin:
		return LengthTerm{VMin: l.V}
	case UnitVMax:
		return LengthTerm{VMax: l.V}
	}
	return LengthTerm{Px: l.CSSPx(fontPx)}
}

func ParseFontSize(v string, parentPx float64) (float64, bool) {
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
	if l := ParseLength(v, parentPx); l.Set {
		return l.CSSPx(parentPx), true
	}
	return 0, false
}
