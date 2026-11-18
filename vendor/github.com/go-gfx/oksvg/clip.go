// Copyright (c) 2026 the go-gfx authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package oksvg

import (
	"encoding/xml"
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/srwiley/rasterx"
)

// Clipping paths: SVG 1.1 §14.3 (https://www.w3.org/TR/SVG11/masking.html#ClippingPaths),
// with CSS Masking Level 1 §6 where it differs.
//
//   - A <clipPath> is never drawn. Its children (basic shapes, <path>, and
//     <use> that references one of them directly) are silhouettes; a point is
//     inside the clip if it is inside ANY silhouette, so their coverages are
//     OR'd. Each is filled with its own clip-rule (nonzero by default).
//   - clipPathUnits is userSpaceOnUse (the default: the coordinate system of the
//     element that references the clip, its own transform included) or
//     objectBoundingBox (a system fitted to the fill bounding box of that
//     element; of a group, of everything it draws). The clipPath's own
//     transform is outermost: it moves the content in the user space and the
//     bounding-box mapping applies to the content within it (as in Chrome).
//   - A child with clip-path is clipped by it BEFORE the OR; a clipPath with
//     clip-path is the INTERSECTION of its content and that clip. An element's
//     clips and its ancestors' intersect; a clipPath and its children do not
//     inherit the clips of the clipPath's own ancestors.
//   - A reference that does not resolve to a <clipPath>, or that is circular, is
//     ignored (the property is treated as unspecified), and an empty clip
//     clips everything away. An objectBoundingBox clip on an element whose
//     bounding box has no width or height clips it away.
//   - Edges are anti-aliased: the mask is 8-bit coverage.
//
// A <use> inside a clipPath, and its x and y, work only for the shapes
// <defs> hold, as <use> does everywhere in oksvg.

// clipDef is one parsed <clipPath>.
type clipDef struct {
	id        string
	bboxUnits bool // clipPathUnits="objectBoundingBox"
	transform rasterx.Matrix2D
	evenOdd   bool       // the clipPath's own clip-rule, the default of its children
	clips     []*clipUse // the clipPath's own clip-path: intersected with its content
	shapes    []clipShape
}

// clipShape is one silhouette of a clipPath.
type clipShape struct {
	path    rasterx.Path
	m       rasterx.Matrix2D // path to clip content space
	evenOdd bool
	clips   []*clipUse // the child's own clip-path
}

// bounds is a bounding box in a user space.
type bounds struct {
	minX, minY, maxX, maxY float64
	ok                     bool
}

func (b *bounds) extend(f *flatSet) {
	if !f.ok {
		return
	}
	if !b.ok {
		b.minX, b.minY, b.maxX, b.maxY, b.ok = f.minX, f.minY, f.maxX, f.maxY, true
		return
	}
	b.minX, b.minY = math.Min(b.minX, f.minX), math.Min(b.minY, f.minY)
	b.maxX, b.maxY = math.Max(b.maxX, f.maxX), math.Max(b.maxY, f.maxY)
}

// clipUse is one clip-path property in force on an element: the reference, the
// user space it was written in, and (for objectBoundingBox) what that element
// draws. It is shared by every path under the element.
type clipUse struct {
	id   string
	icon *SvgIcon
	m    rasterx.Matrix2D // user space of the referencing element, in viewBox units
	box  bounds           // fill bounding box of what it draws, in that user space

	// cache: the mask last built, for the transform and limit it was built under.
	cacheT     rasterx.Matrix2D
	cacheLimit image.Rectangle
	cacheMask  *maskBuf
	cacheOK    bool
}

// parseClipRef reads a clip-path value: "none", or url(#id), url('#id').
func parseClipRef(v string) (id string, ok bool) {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "url(") {
		return "", false
	}
	end := strings.IndexByte(v, ')')
	if end < 0 {
		return "", false
	}
	u := strings.Trim(strings.TrimSpace(v[4:end]), `"'`)
	if !strings.HasPrefix(u, "#") || len(u) == 1 {
		return "", false
	}
	return u[1:], true
}

// addClip puts a clip-path value on a style (a copy of its list).
func (c *IconCursor) addClip(st *PathStyle, v string) {
	id, ok := parseClipRef(v)
	if !ok {
		return
	}
	cu := &clipUse{id: id, icon: c.icon, m: st.mAdder.M}
	st.clips = append(append([]*clipUse(nil), st.clips...), cu)
}

// noteBounds extends the bounding box of every clip in force with a path
// drawn under m. The box is kept in each clip's own user space.
func noteBounds(clips []*clipUse, path rasterx.Path, m rasterx.Matrix2D) {
	if len(clips) == 0 {
		return
	}
	var set *flatSet
	for _, cu := range clips {
		if cu.icon == nil {
			continue
		}
		inv := cu.m.Invert()
		if math.IsNaN(inv.A+inv.B+inv.C+inv.D+inv.E+inv.F) || math.IsInf(inv.A+inv.B+inv.C+inv.D+inv.E+inv.F, 0) {
			continue
		}
		set = flatten(path, inv.Mult(m))
		cu.box.extend(set)
	}
}

// ---- parsing ----

// clipBuild is the state of a <clipPath> being read.
type clipBuild struct {
	def   *clipDef
	depth int // elements open inside the clipPath
}

// clipElement handles the elements of a <clipPath> and the <clipPath> itself,
// wherever it stands (in <defs> or not): it reports whether it took the
// element.
func (c *IconCursor) clipElement(se xml.StartElement) bool {
	if c.clip != nil {
		c.clip.depth++
		c.clipChild(se)
		return true
	}
	if se.Name.Local != "clipPath" {
		return false
	}
	def := &clipDef{transform: rasterx.Identity}
	var transform string
	for _, a := range se.Attr {
		switch a.Name.Local {
		case "id":
			def.id = a.Value
		case "clipPathUnits":
			def.bboxUnits = strings.TrimSpace(a.Value) == "objectBoundingBox"
		case "clip-rule":
			def.evenOdd = strings.TrimSpace(a.Value) == "evenodd"
		case "transform":
			transform = a.Value
		}
	}
	// A pending definition of <defs> ends here, as a new one with an id ends it
	// (a <use> in this clipPath may name it).
	if c.inDefs && len(c.currentDef) > 0 {
		c.icon.Defs[c.currentDef[0].ID] = c.currentDef
		c.currentDef = make([]definition, 0)
	}
	top := &c.StyleStack[len(c.StyleStack)-1]
	// Ancestors' clips are not inherited by a clipPath or its content; only
	// the clip-path written on the clipPath itself is its own.
	top.clips = nil
	if v := ownClipPath(se.Attr); v != "" {
		c.addClip(top, v)
		def.clips, top.clips = top.clips, nil
	}
	top.mAdder.M = rasterx.Identity
	if transform != "" {
		if m, err := c.parseTransform(transform); err == nil {
			def.transform = m
		}
		top.mAdder.M = rasterx.Identity
	}
	c.clip = &clipBuild{def: def}
	return true
}

// ownClipPath returns the clip-path an element writes itself, as an attribute
// or in its style attribute.
func ownClipPath(attrs []xml.Attr) string {
	v := ""
	for _, a := range attrs {
		switch strings.ToLower(a.Name.Local) {
		case "clip-path":
			v = strings.TrimSpace(a.Value)
		case "style":
			for _, decl := range strings.Split(a.Value, ";") {
				if k, val, ok := strings.Cut(decl, ":"); ok && strings.ToLower(strings.TrimSpace(k)) == "clip-path" {
					v = strings.TrimSpace(val)
				}
			}
		}
	}
	return v
}

// clipShapes are the elements a clipPath may hold as silhouettes.
var clipShapes = map[string]bool{"path": true, "rect": true, "circle": true, "ellipse": true, "polygon": true, "polyline": true}

func (c *IconCursor) clipChild(se xml.StartElement) {
	tag := se.Name.Local
	for _, a := range se.Attr {
		if a.Name.Local == "display" && strings.TrimSpace(a.Value) == "none" {
			return
		}
	}
	top := &c.StyleStack[len(c.StyleStack)-1]
	if tag == "use" {
		c.clipUseChild(se, top)
		return
	}
	if !clipShapes[tag] {
		return
	}
	if err := drawFuncs[tag](c, se.Attr); err != nil {
		c.Path = c.Path[:0]
		return
	}
	c.takeClipShape(se.Attr, top)
}

// takeClipShape keeps the path the cursor just built as a silhouette.
func (c *IconCursor) takeClipShape(attrs []xml.Attr, st *PathStyle) {
	if len(c.Path) == 0 {
		return
	}
	def := c.clip.def
	sh := clipShape{path: append(rasterx.Path(nil), c.Path...), m: st.mAdder.M, evenOdd: def.evenOdd, clips: st.clips}
	c.Path = c.Path[:0]
	for _, a := range attrs {
		if a.Name.Local == "clip-rule" {
			sh.evenOdd = strings.TrimSpace(a.Value) == "evenodd"
		}
	}
	noteBounds(sh.clips, sh.path, sh.m)
	def.shapes = append(def.shapes, sh)
}

// clipUseChild reads <use href="#shape"> inside a clipPath: it must reference a
// shape directly (SVG 1.1 §14.3.5: indirect references are an error), and is
// then one silhouette.
func (c *IconCursor) clipUseChild(se xml.StartElement, top *PathStyle) {
	var href string
	var x, y float64
	for _, a := range se.Attr {
		switch a.Name.Local {
		case "href":
			href = a.Value
		case "x":
			x, _ = parseFloat(a.Value, 64)
		case "y":
			y, _ = parseFloat(a.Value, 64)
		}
	}
	if !strings.HasPrefix(href, "#") {
		return
	}
	defs := c.icon.Defs[href[1:]]
	if len(defs) == 0 || !clipShapes[defs[0].Tag] {
		return
	}
	top.mAdder.M = top.mAdder.M.Translate(x, y)
	if err := c.PushStyle(defs[0].Attrs); err != nil {
		return
	}
	pushed := &c.StyleStack[len(c.StyleStack)-1]
	if err := drawFuncs[defs[0].Tag](c, defs[0].Attrs); err == nil {
		c.takeClipShape(defs[0].Attrs, pushed)
	} else {
		c.Path = c.Path[:0]
	}
	c.StyleStack = c.StyleStack[:len(c.StyleStack)-1]
}

// clipEnd handles an end element while a clipPath is open; it reports whether
// it took it.
func (c *IconCursor) clipEnd(name string) bool {
	if c.clip == nil {
		return false
	}
	if c.clip.depth > 0 {
		c.clip.depth--
		return true
	}
	if name == "clipPath" {
		def := c.clip.def
		if def.id != "" {
			if c.icon.clips == nil {
				c.icon.clips = map[string]*clipDef{}
			}
			if _, dup := c.icon.clips[def.id]; !dup {
				c.icon.clips[def.id] = def
			}
		}
		c.clip = nil
	}
	return true
}

// ---- masks ----

// limitFor is the pixel box a mask is built over: the target surface when the
// scanner says what it is, else a large box (and the pixel bound decides).
func limitFor(r *rasterx.Dasher) image.Rectangle {
	if s, ok := r.Scanner.(*rasterx.ScannerGV); ok && s.Dest != nil {
		return image.Rect(0, 0, s.Dest.Bounds().Dx(), s.Dest.Bounds().Dy())
	}
	return image.Rect(-1<<15, -1<<15, 1<<15, 1<<15)
}

// maskState carries the recursion of one top-level mask build.
type maskState struct {
	limit    image.Rectangle
	visiting map[*clipDef]bool
	tooBig   bool
}

// maxClipDepth bounds nested clips (clip-path on clipPath on clipPath ...).
const maxClipDepth = 16

// mask returns the mask of this clip under the icon transform t, or nil when
// the clip is to be ignored (an unresolved or circular reference, or one too
// large to build). An empty mask clips everything.
func (cu *clipUse) mask(t rasterx.Matrix2D, limit image.Rectangle) *maskBuf {
	if cu.cacheOK && cu.cacheT == t && cu.cacheLimit == limit {
		return cu.cacheMask
	}
	st := &maskState{limit: limit, visiting: map[*clipDef]bool{}}
	m := cu.build(t.Mult(cu.m), cu.box, st)
	if st.tooBig {
		m = nil
	}
	cu.cacheT, cu.cacheLimit, cu.cacheMask, cu.cacheOK = t, limit, m, true
	return m
}

// build resolves the reference to a mask in the user space d (user space to
// device pixels), with box the fill box of the element it clips.
func (cu *clipUse) build(d rasterx.Matrix2D, box bounds, st *maskState) *maskBuf {
	def := cu.icon.clips[cu.id]
	if def == nil || st.visiting[def] || len(st.visiting) >= maxClipDepth {
		return nil
	}
	st.visiting[def] = true
	defer delete(st.visiting, def)

	// The clipPath's transform comes first (outermost), then the bounding-box
	// mapping applies to the content within it: translate(.25 0) on an
	// objectBoundingBox clipPath moves by a quarter of a USER unit, as Chrome
	// does, not by a quarter of the box.
	content := d.Mult(def.transform)
	if def.bboxUnits {
		w, h := box.maxX-box.minX, box.maxY-box.minY
		if !box.ok || !(w > 0) || !(h > 0) {
			return &maskBuf{}
		}
		content = content.Mult(rasterx.Matrix2D{A: w, D: h, E: box.minX, F: box.minY})
	}

	// Silhouettes: coverage of each, clipped by its own clip, OR'd together.
	type piece struct {
		r image.Rectangle
		a []uint8
	}
	var pieces []piece
	var union image.Rectangle
	for i := range def.shapes {
		sh := &def.shapes[i]
		set := flatten(sh.path, content.Mult(sh.m))
		r := set.rect().Intersect(st.limit)
		if r.Empty() {
			continue
		}
		if r.Dx()*r.Dy() > maxMaskPixels {
			st.tooBig = true
			return nil
		}
		a := fillCoverage(set, r, sh.evenOdd)
		for _, child := range sh.clips {
			cm := child.build(content.Mult(sh.m), child.box, st)
			if cm == nil {
				continue
			}
			multiply(a, r, cm)
		}
		pieces = append(pieces, piece{r, a})
		union = union.Union(r)
	}
	if union.Empty() {
		return &maskBuf{}
	}
	if union.Dx()*union.Dy() > maxMaskPixels {
		st.tooBig = true
		return nil
	}
	out := &maskBuf{r: union, a: make([]uint8, union.Dx()*union.Dy())}
	for _, p := range pieces {
		for y := p.r.Min.Y; y < p.r.Max.Y; y++ {
			dst := out.a[(y-union.Min.Y)*union.Dx()+(p.r.Min.X-union.Min.X):]
			src := p.a[(y-p.r.Min.Y)*p.r.Dx() : (y-p.r.Min.Y+1)*p.r.Dx()]
			for x, v := range src {
				// OR of two coverages: a + b - ab.
				o := dst[x]
				dst[x] = uint8(int(o) + int(v) - (int(o)*int(v)+127)/255)
			}
		}
	}
	// The clipPath's own clip-path intersects.
	for _, nested := range def.clips {
		nm := nested.build(d, box, st)
		if nm == nil {
			continue
		}
		multiply(out.a, out.r, nm)
	}
	return out
}

// multiply scales a (coverage over r) by the mask m, pixel by pixel.
func multiply(a []uint8, r image.Rectangle, m *maskBuf) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := (y-r.Min.Y)*r.Dx() + (x - r.Min.X)
			if a[i] != 0 {
				a[i] = uint8((int(a[i])*int(m.at(x, y)) + 127) / 255)
			}
		}
	}
}

// clipColor wraps a fill or stroke colour (a color.Color or a rasterx.ColorFunc)
// so that its alpha is scaled by the masks.
func clipColor(clr interface{}, masks []*maskBuf) interface{} {
	cov := func(x, y int) int {
		v := 255
		for _, m := range masks {
			v = v * int(m.at(x, y)) / 255
			if v == 0 {
				break
			}
		}
		return v
	}
	scale := func(c color.Color, v int) color.Color {
		n := color.NRGBAModel.Convert(c).(color.NRGBA)
		n.A = uint8((int(n.A)*v + 127) / 255)
		return n
	}
	switch c := clr.(type) {
	case rasterx.ColorFunc:
		return rasterx.ColorFunc(func(x, y int) color.Color { return scale(c(x, y), cov(x, y)) })
	case color.Color:
		// One colour at 256 alphas: handing out pointers into a table keeps the
		// per-pixel call free of allocation (a value in an interface allocates).
		n := color.NRGBAModel.Convert(c).(color.NRGBA)
		tab := new([256]color.NRGBA)
		for a := range tab {
			tab[a] = n
			tab[a].A = uint8((int(n.A)*a + 127) / 255)
		}
		return rasterx.ColorFunc(func(x, y int) color.Color { return &tab[cov(x, y)] })
	}
	return clr
}
