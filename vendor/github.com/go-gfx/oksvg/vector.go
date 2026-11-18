// Copyright (c) 2026 the go-gfx authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package oksvg

import (
	"image"
	"image/color"
	"math"
	"sort"

	"github.com/srwiley/rasterx"
	"golang.org/x/image/math/fixed"
)

// The icon as vector shapes, for a writer that draws paths itself (a PDF, a
// PowerPoint slide) instead of rasterising: Shapes returns what Draw would
// paint, in the icon's viewBox units, each fill and each stroke a filled
// outline with a solid colour or a gradient, under the clips in force.
//
// SVG 1.1 painting (https://www.w3.org/TR/SVG11/painting.html) is what Draw
// implements, and Shapes follows Draw: a stroke is the outline rasterx's own
// stroker makes of it (width, caps, joins, miter limit, dashes, as Draw
// strokes it), a fill keeps its curves, transforms are applied to the
// points (an affine map keeps a Bézier curve a Bézier curve). Opacities go
// into the colour's alpha.
//
// Some of what Draw paints has no exact form here, and Shapes then says so
// (exact is false), for the caller to rasterise instead:
//   - a gradient whose stops change opacity (a shading has one), that is
//     reflected or repeated (spreadMethod), a linear one under a skew, a
//     radial one whose focus is off its centre or that is rotated or skewed;
//   - a clipPath whose silhouettes mix clip rules or are clipped
//     themselves (the union of clipped pieces is not one clip path).

// PathOp is one step of a Shape's outline: 'M' move to Pts[0], 'L' line to
// Pts[0], 'Q' quadratic curve through Pts[0] to Pts[1], 'C' cubic curve
// through Pts[0] and Pts[1] to Pts[2], 'Z' close.
type PathOp struct {
	Op  byte
	Pts [3][2]float64
}

// Shape is one painted piece of an icon, in viewBox units.
type Shape struct {
	Path    []PathOp
	EvenOdd bool
	// Color is the paint, alpha included, when Gradient is nil.
	Color    color.NRGBA
	Gradient *VectorGradient
	// Clips are the clip paths in force; the shape shows where it is inside
	// every one.
	Clips []ClipPath
	// Keys are the element keys of the SvgPath the shape was made from (see
	// SvgPath.Keys).
	Keys []int
}

// ClipPath is one clip path, in viewBox units.
type ClipPath struct {
	Path    []PathOp
	EvenOdd bool
}

// VectorGradient is a gradient in viewBox units. A linear one runs from
// (X1,Y1) at offset 0 to (X2,Y2) at 1; a radial one from its centre (CX,CY)
// at 0 to its ellipse of radii (RX,RY), axis-aligned, at 1. Both pad: the
// first and last stops' colours extend beyond.
type VectorGradient struct {
	Radial         bool
	X1, Y1, X2, Y2 float64
	CX, CY, RX, RY float64
	Stops          []VectorStop
}

// VectorStop is one stop: an offset in [0,1] and a colour, alpha included.
type VectorStop struct {
	Offset float64
	Color  color.NRGBA
}

// strokeUnits is the size, in fixed-point units, the stroker works at: the
// icon is scaled so that its larger viewBox side is this many units before
// stroking, so that 26.6 fixed point keeps sub-1/1000 precision whatever the
// viewBox, then scaled back.
const strokeUnits = 4096

// Shapes returns the icon's drawing as vector shapes, in drawing order, and
// whether they are exactly what Draw paints (see the package comment of
// this file). A shape a clip removes entirely is left out.
func (s *SvgIcon) Shapes() (shapes []Shape, exact bool) {
	exact = true
	k := strokeUnits / math.Max(math.Max(s.ViewBox.W, s.ViewBox.H), 1e-9)
	for i := range s.SVGPaths {
		p := &s.SVGPaths[i]
		clips, ok, gone := p.vectorClips()
		if !ok {
			exact = false
		}
		if gone {
			continue
		}
		m := p.mAdder.M
		if p.fillerColor != nil {
			sh := Shape{Path: transformPath(p.Path, m), EvenOdd: !p.UseNonZeroWinding, Clips: clips, Keys: p.Keys}
			if paintOK := sh.setPaint(p.fillerColor, p.FillOpacity, m); !paintOK {
				exact = false
			}
			if len(sh.Path) > 0 {
				shapes = append(shapes, sh)
			}
		}
		if p.linerColor != nil && p.LineWidth > 0 {
			outline, ok := p.strokeOutline(k)
			if !ok {
				exact = false
			}
			sh := Shape{Path: outline, Clips: clips, Keys: p.Keys}
			if paintOK := sh.setPaint(p.linerColor, p.LineOpacity, m); !paintOK {
				exact = false
			}
			if len(sh.Path) > 0 {
				shapes = append(shapes, sh)
			}
		}
	}
	return shapes, exact
}

// setPaint gives the shape its colour or gradient, under the element
// transform m. It reports whether the paint is exact.
func (sh *Shape) setPaint(paint interface{}, opacity float64, m rasterx.Matrix2D) bool {
	switch c := paint.(type) {
	case color.Color:
		sh.Color = rasterx.ApplyOpacity(c, opacity)
		return true
	case rasterx.Gradient:
		if len(c.Stops) == 0 {
			// no stops: as Draw, which paints black (one stop: its colour)
			sh.Color = rasterx.ApplyOpacity(color.Black, opacity)
			return true
		}
		g, ok := vectorGradient(c, opacity, m, pathBounds(sh.Path, m))
		if g == nil {
			sh.Path = nil // a bounding box with no area: not painted
			return ok
		}
		if len(g.Stops) == 1 {
			sh.Color = g.Stops[0].Color
			return ok
		}
		sh.Gradient = g
		return ok
	}
	return false
}

// vectorGradient maps a gradient to viewBox units. box is the bounding box,
// in the element's user space, of the shape it fills (objectBoundingBox).
func vectorGradient(g rasterx.Gradient, opacity float64, m rasterx.Matrix2D, box [4]float64) (*VectorGradient, bool) {
	exact := g.Spread == rasterx.PadSpread
	stops := append([]rasterx.GradStop(nil), g.Stops...)
	sort.SliceStable(stops, func(i, j int) bool { return stops[i].Offset < stops[j].Offset })
	v := &VectorGradient{Radial: g.IsRadial}
	for _, st := range stops {
		v.Stops = append(v.Stops, VectorStop{Offset: math.Max(0, math.Min(1, st.Offset)), Color: rasterx.ApplyOpacity(st.StopColor, st.Opacity*opacity)})
		if st.Opacity != stops[0].Opacity {
			exact = false // one opacity per shading
		}
	}
	// The gradient's own space to the element's user space: for
	// objectBoundingBox, the box, then gradientTransform within it, as
	// rasterx does (translate(box) scale(box) Matrix scale(1/box)
	// translate(-box)); for userSpaceOnUse, gradientTransform.
	gm := g.Matrix
	if gm == (rasterx.Matrix2D{}) {
		gm = rasterx.Identity
	}
	// The points are mapped to viewBox units by m·base·gm, base being the
	// bounding box (objectBoundingBox) or the identity. The gradient's
	// geometry around them (the iso-lines perpendicular to the axis, the
	// circle) is in the space rasterx computes it in: the device, through
	// the gradientTransform conjugated by the box (translate(box)
	// scale(box) Matrix scale(1/box) translate(-box)), so its shape is
	// kept exactly when m·conj is a similarity (axis-aligned for a radial
	// one).
	base := rasterx.Identity
	sx, sy := 1.0, 1.0
	if g.Units == rasterx.ObjectBoundingBox {
		x, y, w, h := box[0], box[1], box[2]-box[0], box[3]-box[1]
		if !(w > 0) || !(h > 0) {
			// SVG 1.1 §13.2.2: such an element is not painted
			return nil, exact
		}
		base = rasterx.Identity.Translate(x, y).Scale(w, h)
		sx, sy = w, h
	}
	conj := base.Mult(gm).Mult(base.Invert())
	shape := m.Mult(conj)
	full := m.Mult(base).Mult(gm)
	if g.IsRadial {
		cx, cy, fx, fy, r := g.Points[0], g.Points[1], g.Points[2], g.Points[3], g.Points[4]
		if cx != fx || cy != fy {
			exact = false // a focal point: no form here
		}
		if math.Abs(shape.B) > 1e-9 || math.Abs(shape.C) > 1e-9 {
			exact = false // rotated or skewed: not an axis-aligned ellipse
		}
		v.CX, v.CY = full.Transform(cx, cy)
		v.RX, v.RY = math.Abs(shape.A*r*sx), math.Abs(shape.D*r*sy)
		return v, exact
	}
	if !similarity(shape) {
		exact = false // a skew makes the iso-lines oblique to the axis
	}
	v.X1, v.Y1 = full.Transform(g.Points[0], g.Points[1])
	v.X2, v.Y2 = full.Transform(g.Points[2], g.Points[3])
	return v, exact
}

// similarity reports whether m keeps angles: a rotation, a uniform scale, a
// reflection and a translation.
func similarity(m rasterx.Matrix2D) bool {
	const eps = 1e-9
	return (math.Abs(m.A-m.D) < eps && math.Abs(m.B+m.C) < eps) || (math.Abs(m.A+m.D) < eps && math.Abs(m.B-m.C) < eps)
}

// pathBounds is the bounding box of the control points of a path given in
// viewBox units, mapped back to the user space of the element by m's
// inverse: [minX, minY, maxX, maxY].
func pathBounds(ops []PathOp, m rasterx.Matrix2D) [4]float64 {
	inv := m.Invert()
	b := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	n := map[byte]int{'M': 1, 'L': 1, 'Q': 2, 'C': 3}
	for _, op := range ops {
		for i := 0; i < n[op.Op]; i++ {
			x, y := inv.Transform(op.Pts[i][0], op.Pts[i][1])
			b[0], b[1] = math.Min(b[0], x), math.Min(b[1], y)
			b[2], b[3] = math.Max(b[2], x), math.Max(b[3], y)
		}
	}
	return b
}

// pathRecorder is a rasterx.Adder that keeps the path it is given, in
// float64, under a transform.
type pathRecorder struct {
	m   rasterx.Matrix2D
	ops []PathOp
}

func (r *pathRecorder) pt(p fixed.Point26_6) [2]float64 {
	x, y := r.m.Transform(float64(p.X)/64, float64(p.Y)/64)
	return [2]float64{x, y}
}

func (r *pathRecorder) Start(a fixed.Point26_6) {
	r.ops = append(r.ops, PathOp{Op: 'M', Pts: [3][2]float64{r.pt(a)}})
}

func (r *pathRecorder) Line(b fixed.Point26_6) {
	r.ops = append(r.ops, PathOp{Op: 'L', Pts: [3][2]float64{r.pt(b)}})
}

func (r *pathRecorder) QuadBezier(b, c fixed.Point26_6) {
	r.ops = append(r.ops, PathOp{Op: 'Q', Pts: [3][2]float64{r.pt(b), r.pt(c)}})
}

func (r *pathRecorder) CubeBezier(b, c, d fixed.Point26_6) {
	r.ops = append(r.ops, PathOp{Op: 'C', Pts: [3][2]float64{r.pt(b), r.pt(c), r.pt(d)}})
}

func (r *pathRecorder) Stop(closeLoop bool) {
	if closeLoop {
		r.ops = append(r.ops, PathOp{Op: 'Z'})
	}
}

// transformPath is a path in viewBox units: p under m.
func transformPath(p rasterx.Path, m rasterx.Matrix2D) []PathOp {
	r := &pathRecorder{m: m}
	p.AddTo(r)
	return r.ops
}

// outlineScanner is a rasterx.Scanner that keeps the edges it is given (a
// stroke's outline, as the stroker emits it) instead of filling them.
//
// The stroker emits an outline as open pieces, one side of a segment, the
// other side, a cap, a join, that close only together: the scanner adds up
// their edges' windings, which needs no piece to be closed. A writer's fill
// closes every subpath on its own, so the pieces are stitched back into
// closed loops, each piece followed by the one that starts where it ends
// (the stroker joins them at the very same fixed-point coordinates).
type outlineScanner struct {
	inv    float64 // back to viewBox units
	pieces [][]fixed.Point26_6
	ops    []PathOp
	// points counts what was recorded; past maxOutlinePoints the outline is
	// dropped (over), so that a stroke cannot grow memory without bound.
	points int
	over   bool
}

// maxOutlinePoints bounds one stroke's outline.
var maxOutlinePoints = 1 << 20

func (s *outlineScanner) pt(p fixed.Point26_6) [2]float64 {
	return [2]float64{float64(p.X) / 64 * s.inv, float64(p.Y) / 64 * s.inv}
}

// room counts one more point, and reports whether there is room for it.
func (s *outlineScanner) room() bool {
	if s.over {
		return false
	}
	if s.points++; s.points > maxOutlinePoints {
		s.over, s.pieces = true, nil
		return false
	}
	return true
}

func (s *outlineScanner) Start(a fixed.Point26_6) {
	if s.room() {
		s.pieces = append(s.pieces, []fixed.Point26_6{a})
	}
}

func (s *outlineScanner) Line(b fixed.Point26_6) {
	if len(s.pieces) == 0 {
		s.Start(b)
		return
	}
	if s.room() {
		last := &s.pieces[len(s.pieces)-1]
		*last = append(*last, b)
	}
}

func (s *outlineScanner) Draw() {
	used := make([]bool, len(s.pieces))
	starts := map[fixed.Point26_6][]int{}
	for i, p := range s.pieces {
		if len(p) < 2 {
			used[i] = true // a lone point draws nothing
			continue
		}
		starts[p[0]] = append(starts[p[0]], i)
	}
	next := func(at fixed.Point26_6) int {
		for _, j := range starts[at] {
			if !used[j] {
				return j
			}
		}
		return -1
	}
	for i := range s.pieces {
		if used[i] {
			continue
		}
		used[i] = true
		loop := append([]fixed.Point26_6(nil), s.pieces[i]...)
		for loop[len(loop)-1] != loop[0] {
			j := next(loop[len(loop)-1])
			if j < 0 {
				break // an open end: the fill closes it, as the scanner would
			}
			used[j] = true
			loop = append(loop, s.pieces[j][1:]...)
		}
		s.ops = append(s.ops, PathOp{Op: 'M', Pts: [3][2]float64{s.pt(loop[0])}})
		for _, q := range loop[1:] {
			s.ops = append(s.ops, PathOp{Op: 'L', Pts: [3][2]float64{s.pt(q)}})
		}
		s.ops = append(s.ops, PathOp{Op: 'Z'})
	}
	s.pieces = nil
}

func (s *outlineScanner) GetPathExtent() fixed.Rectangle26_6 { return fixed.Rectangle26_6{} }
func (s *outlineScanner) SetBounds(int, int)                 {}
func (s *outlineScanner) SetColor(interface{})               {}
func (s *outlineScanner) SetWinding(bool)                    {}
func (s *outlineScanner) Clear()                             {}
func (s *outlineScanner) SetClip(image.Rectangle)            {}

// strokeOutline is the outline of the path's stroke in viewBox units, made
// by rasterx's stroker as Draw makes it, at k fixed-point units per viewBox
// unit. ok is false when the outline is past the bounds (maxDashes,
// maxOutlinePoints): there is then no outline.
func (p *SvgPath) strokeOutline(k float64) (ops []PathOp, ok bool) {
	if !p.dashesBounded(p.mAdder.M) {
		return nil, false
	}
	sc := &outlineScanner{inv: 1 / k}
	d := rasterx.NewDasher(4*strokeUnits, 4*strokeUnits, sc)
	lineGap, lineCap := p.LineGap, p.LineCap
	if lineGap == nil {
		lineGap = DefaultStyle.LineGap
	}
	if lineCap == nil {
		lineCap = DefaultStyle.LineCap
	}
	leadLineCap := lineCap
	if p.LeadLineCap != nil {
		leadLineCap = p.LeadLineCap
	}
	// The stroke is drawn in the scaled space: width, dashes and offset are
	// in the element's user units, scaled with the element's transform by
	// the stroker itself (it strokes the transformed path), so the
	// transform here is the element's, then k.
	scale := math.Sqrt(math.Abs(p.mAdder.M.A*p.mAdder.M.D - p.mAdder.M.B*p.mAdder.M.C))
	dash := make([]float64, len(p.Dash))
	for i, v := range p.Dash {
		dash[i] = v * scale * k
	}
	d.SetStroke(fixed.Int26_6(p.LineWidth*scale*k*64), fixed.Int26_6(p.MiterLimit*64),
		leadLineCap, lineCap, lineGap, p.LineJoin, dash, p.DashOffset*scale*k)
	ma := rasterx.MatrixAdder{Adder: d, M: rasterx.Identity.Scale(k, k).Mult(p.mAdder.M)}
	p.Path.AddTo(&ma)
	d.Draw()
	if sc.over {
		return nil, false
	}
	return sc.ops, true
}

// maxDashes bounds the dashes of one stroke: a dash array of tiny dashes
// along a long path would otherwise take the stroker one step per dash
// without end (a hundredth of a unit along a million units is 10¹⁰).
var maxDashes = 1 << 16

// dashesBounded reports whether the path's dashes under m number at most
// maxDashes, counted along its control polygon, which is at least as long
// as the path. A path without dashes is bounded.
func (p *SvgPath) dashesBounded(m rasterx.Matrix2D) bool {
	if len(p.Dash) == 0 {
		return true
	}
	period := 0.0
	for _, v := range p.Dash {
		period += math.Abs(v)
	}
	period *= math.Sqrt(math.Abs(m.A*m.D - m.B*m.C))
	length := 0.0
	var cur [2]float64
	for _, op := range transformPath(p.Path, m) {
		n := map[byte]int{'M': 1, 'L': 1, 'Q': 2, 'C': 3}[op.Op]
		for i, q := range op.Pts[:n] {
			if !(op.Op == 'M' && i == 0) {
				length += math.Hypot(q[0]-cur[0], q[1]-cur[1])
			}
			cur = q
		}
	}
	if length == 0 {
		return true
	}
	return period > 0 && length/period <= float64(maxDashes)
}

// vectorClips resolves the clip-path properties in force on the path to clip
// paths in viewBox units. ok is false when one has no exact form; gone when
// one keeps nothing. A clip that cannot be applied (it names no clipPath, or
// is circular) is skipped, as Draw skips it.
func (p *SvgPath) vectorClips() (clips []ClipPath, ok, gone bool) {
	ok = true
	for _, cu := range p.clips {
		cs, cok, cgone := cu.vector(cu.m, cu.box, map[*clipDef]bool{})
		if !cok {
			ok = false
		}
		if cgone {
			return nil, ok, true
		}
		clips = append(clips, cs...)
	}
	return clips, ok, false
}

// vector is the clip as clip paths in viewBox units: the union of its
// silhouettes, then its own clip-path's.
func (cu *clipUse) vector(d rasterx.Matrix2D, box bounds, visiting map[*clipDef]bool) (clips []ClipPath, ok, gone bool) {
	ok = true
	if cu.icon == nil {
		return nil, true, false
	}
	def := cu.icon.clips[cu.id]
	if def == nil || visiting[def] || len(visiting) >= maxClipDepth {
		return nil, true, false
	}
	visiting[def] = true
	defer delete(visiting, def)
	content := d.Mult(def.transform)
	if def.bboxUnits {
		w, h := box.maxX-box.minX, box.maxY-box.minY
		if !box.ok || !(w > 0) || !(h > 0) {
			return nil, true, true
		}
		content = content.Mult(rasterx.Matrix2D{A: w, D: h, E: box.minX, F: box.minY})
	}
	var union ClipPath
	for i := range def.shapes {
		sh := &def.shapes[i]
		if len(sh.clips) > 0 {
			ok = false // a clipped silhouette: not one path
		}
		if i == 0 {
			union.EvenOdd = sh.evenOdd
		} else if sh.evenOdd != union.EvenOdd {
			ok = false // mixed rules: not one path
		}
		union.Path = append(union.Path, transformPath(sh.path, content.Mult(sh.m))...)
	}
	if len(union.Path) == 0 {
		return nil, ok, true
	}
	clips = append(clips, union)
	for _, nested := range def.clips {
		ns, nok, ngone := nested.vector(d, box, visiting)
		if !nok {
			ok = false
		}
		if ngone {
			return nil, ok, true
		}
		clips = append(clips, ns...)
	}
	return clips, ok, false
}
