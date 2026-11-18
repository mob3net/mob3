// Copyright (c) 2026 the go-gfx authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package oksvg

import (
	"image"
	"image/draw"
	"math"

	"github.com/srwiley/rasterx"
	"golang.org/x/image/math/fixed"
)

// This file is the coverage rasteriser behind clipPath: it turns the shapes of a
// clip into 8-bit coverage, with either fill rule. rasterx's own scanner (the
// x/image/vector rasteriser) has one rule only and no way to read the coverage
// back, so a clip, which IS a coverage mask, gets its own: signed area is
// accumulated per pixel (the method of font-rs and of x/image/vector) and
// folded into [0,1] by the rule.

// maxMaskPixels bounds one mask's area, in pixels: 16 million, a 4096-pixel
// square (the largest surface go-webengine rasterises). A clip whose mask
// would be larger is not applied (the element draws unclipped) rather than
// allocating more.
var maxMaskPixels = 1 << 24

// maxCoord is the largest device coordinate a segment may carry; a segment
// beyond it (or not finite) is dropped, so no conversion to int can overflow.
const maxCoord = 1e9

// seg is one flattened line segment, in device pixels.
type seg struct{ ax, ay, bx, by float64 }

// flatSet is a path flattened under a transform: its segments and its bounds.
type flatSet struct {
	segs                   []seg
	minX, minY, maxX, maxY float64
	ok                     bool
}

func (f *flatSet) add(ax, ay, bx, by float64) {
	for _, v := range [...]float64{ax, ay, bx, by} {
		if math.IsNaN(v) || math.Abs(v) > maxCoord {
			return
		}
	}
	f.segs = append(f.segs, seg{ax, ay, bx, by})
	if !f.ok {
		f.minX, f.minY, f.maxX, f.maxY, f.ok = ax, ay, ax, ay, true
	}
	for _, p := range [...][2]float64{{ax, ay}, {bx, by}} {
		f.minX, f.maxX = math.Min(f.minX, p[0]), math.Max(f.maxX, p[0])
		f.minY, f.maxY = math.Min(f.minY, p[1]), math.Max(f.maxY, p[1])
	}
}

// flattener is the rasterx.Adder that collects a transformed path as segments.
// Every sub-path is closed, as a fill closes it.
type flattener struct {
	set        *flatSet
	cur, first [2]float64
	open       bool
}

func pt(p fixed.Point26_6) [2]float64 { return [2]float64{float64(p.X) / 64, float64(p.Y) / 64} }

func (f *flattener) line(to [2]float64) {
	f.set.add(f.cur[0], f.cur[1], to[0], to[1])
	f.cur = to
}

// Start implements rasterx.Adder.
func (f *flattener) Start(a fixed.Point26_6) {
	f.Stop(true)
	f.cur, f.first, f.open = pt(a), pt(a), true
}

// Line implements rasterx.Adder.
func (f *flattener) Line(b fixed.Point26_6) { f.line(pt(b)) }

// QuadBezier implements rasterx.Adder.
func (f *flattener) QuadBezier(b, c fixed.Point26_6) {
	a, pb, pc := f.cur, pt(b), pt(c)
	rasterx.QuadTo(float32(a[0]), float32(a[1]), float32(pb[0]), float32(pb[1]), float32(pc[0]), float32(pc[1]),
		func(x, y float32) { f.line([2]float64{float64(x), float64(y)}) })
}

// CubeBezier implements rasterx.Adder.
func (f *flattener) CubeBezier(b, c, d fixed.Point26_6) {
	a, pb, pc, pd := f.cur, pt(b), pt(c), pt(d)
	rasterx.CubeTo(float32(a[0]), float32(a[1]), float32(pb[0]), float32(pb[1]),
		float32(pc[0]), float32(pc[1]), float32(pd[0]), float32(pd[1]),
		func(x, y float32) { f.line([2]float64{float64(x), float64(y)}) })
}

// Stop implements rasterx.Adder: it closes the sub-path.
func (f *flattener) Stop(bool) {
	if f.open && f.cur != f.first {
		f.line(f.first)
	}
	f.open = false
}

// flatten returns path under m (path coordinates to device pixels).
func flatten(path rasterx.Path, m rasterx.Matrix2D) *flatSet {
	set := &flatSet{}
	fl := &flattener{set: set}
	ma := &rasterx.MatrixAdder{M: m, Adder: fl}
	path.AddTo(ma)
	fl.Stop(true)
	return set
}

// rect is the integer pixel box of a flattened set, or empty.
func (f *flatSet) rect() image.Rectangle {
	if !f.ok {
		return image.Rectangle{}
	}
	return image.Rect(int(math.Floor(f.minX)), int(math.Floor(f.minY)), int(math.Ceil(f.maxX)), int(math.Ceil(f.maxY)))
}

// maskBuf is coverage over a pixel rectangle, 0 where the clip takes the ink
// away and 255 where it keeps all of it; outside r everything is clipped.
type maskBuf struct {
	r image.Rectangle
	a []uint8 // r.Dx()*r.Dy(), row-major
}

func (m *maskBuf) at(x, y int) uint8 {
	if !(image.Point{x, y}).In(m.r) {
		return 0
	}
	return m.a[(y-m.r.Min.Y)*m.r.Dx()+(x-m.r.Min.X)]
}

// empty reports a mask that keeps nothing.
func (m *maskBuf) empty() bool { return m.r.Empty() }

// covRaster accumulates signed area over a w×h pixel region.
type covRaster struct {
	w, h, stride int
	a            []float32
}

func newCovRaster(w, h int) *covRaster {
	// Two spare columns: a segment right of the region deposits into column w,
	// and the spreading of an edge reaches one column further.
	return &covRaster{w: w, h: h, stride: w + 2, a: make([]float32, (w+2)*h)}
}

// line accumulates one segment, already translated so the region starts at
// (0, 0).
func (c *covRaster) line(ax, ay, bx, by float64) {
	if ay == by {
		return
	}
	dir := float32(1)
	if ay > by {
		ax, ay, bx, by = bx, by, ax, ay
		dir = -1
	}
	if by <= 0 || ay >= float64(c.h) {
		return
	}
	dxdy := (bx - ax) / (by - ay)
	x := ax
	y0 := 0
	if ay > 0 {
		y0 = int(ay)
	} else {
		x -= ay * dxdy
	}
	yEnd := int(math.Ceil(by))
	if yEnd > c.h {
		yEnd = c.h
	}
	fw := float64(c.w)
	for y := y0; y < yEnd; y++ {
		dy := math.Min(float64(y+1), by) - math.Max(float64(y), ay)
		xn := x + dxdy*dy
		d := float32(dy) * dir
		x0, x1 := x, xn
		if x0 > x1 {
			x0, x1 = x1, x0
		}
		x = xn
		// A segment outside the region still covers what is to its right.
		x0 = math.Min(math.Max(x0, 0), fw)
		x1 = math.Min(math.Max(x1, 0), fw)
		row := c.a[y*c.stride : (y+1)*c.stride]
		x0f := math.Floor(x0)
		x0i := int(x0f)
		x1c := math.Ceil(x1)
		x1i := int(x1c)
		if x1i <= x0i+1 {
			xmf := float32(0.5*(x0+x1) - x0f)
			row[x0i] += d - d*xmf
			row[x0i+1] += d * xmf
			continue
		}
		s := float32(1 / (x1 - x0))
		x0r := float32(x0 - x0f)
		a0 := 0.5 * s * (1 - x0r) * (1 - x0r)
		x1r := float32(x1 - x1c + 1)
		am := 0.5 * s * x1r * x1r
		row[x0i] += d * a0
		if x1i == x0i+2 {
			row[x0i+1] += d * (1 - a0 - am)
		} else {
			a1 := s * (1.5 - x0r)
			row[x0i+1] += d * (a1 - a0)
			for xi := x0i + 2; xi < x1i-1; xi++ {
				row[xi] += d * s
			}
			a2 := a1 + float32(x1i-x0i-3)*s
			row[x1i-1] += d * (1 - a2 - am)
		}
		row[x1i] += d * am
	}
}

// coverage resolves the accumulated area into 8-bit coverage under a fill rule.
func (c *covRaster) coverage(evenOdd bool) []uint8 {
	out := make([]uint8, c.w*c.h)
	for y := 0; y < c.h; y++ {
		row := c.a[y*c.stride : y*c.stride+c.w]
		var acc float32
		for x, v := range row {
			acc += v
			if acc < 0 {
				v = -acc
			} else {
				v = acc
			}
			if evenOdd {
				v -= 2 * float32(math.Floor(float64(v)/2))
				if v > 1 {
					v = 2 - v
				}
			} else if v > 1 {
				v = 1
			}
			out[y*c.w+x] = uint8(v*255 + 0.5)
		}
	}
	return out
}

// fillEvenOdd paints path (under m, path coordinates to device pixels) with the
// even-odd rule, in the colour the scanner was last given (SetColor). rasterx's
// scanner (x/image/vector) has one rule, nonzero, and no way to ask for another,
// so the coverage comes from this file's rasteriser and goes through the same
// compositing, draw.DrawMask, that the scanner uses. It reports false when the
// scanner is not the one it knows how to feed: the caller then draws nonzero.
func fillEvenOdd(sc rasterx.Scanner, path rasterx.Path, m rasterx.Matrix2D) bool {
	s, ok := sc.(*rasterx.ScannerGV)
	if !ok || s.Dest == nil || s.Source == nil || s.Dest.Bounds().Min != (image.Point{}) || s.Offset != (image.Point{}) {
		return false
	}
	set := flatten(path, m)
	r := set.rect().Intersect(s.Dest.Bounds())
	if r.Empty() {
		return true // nothing of it is on the surface
	}
	if r.Dx()*r.Dy() > maxMaskPixels {
		return false
	}
	cov := fillCoverage(set, r, true)
	mask := &image.Alpha{Pix: cov, Stride: r.Dx(), Rect: r}
	draw.DrawMask(s.Dest, r, atOnly{s.Source}, r.Min, mask, r.Min, draw.Over)
	return true
}

// atOnly hides everything but At of an image. rasterx's colour-function image
// embeds image.Uniform, whose RGBA64At returns its constant colour: draw.DrawMask
// uses that fast path when it finds it and would paint the colour function's
// placeholder instead of the function.
type atOnly struct{ image.Image }

// fillCoverage rasterises a flattened set into coverage over r (its pixel
// box, already limited by the caller).
func fillCoverage(set *flatSet, r image.Rectangle, evenOdd bool) []uint8 {
	cr := newCovRaster(r.Dx(), r.Dy())
	ox, oy := float64(r.Min.X), float64(r.Min.Y)
	for _, s := range set.segs {
		cr.line(s.ax-ox, s.ay-oy, s.bx-ox, s.by-oy)
	}
	return cr.coverage(evenOdd)
}
