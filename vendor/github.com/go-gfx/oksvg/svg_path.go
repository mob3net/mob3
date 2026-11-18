// Copyright 2017 The oksvg Authors. All rights reserved.
// created: 2/12/2017 by S.R.Wiley
//
// utils.go implements translation of an SVG2.0 path into a rasterx Path.

package oksvg

import (
	"image/color"
	"math"

	"github.com/srwiley/rasterx"
	"golang.org/x/image/math/fixed"
)

// SvgPath binds a style to a path.
type SvgPath struct {
	PathStyle
	Path rasterx.Path
	// Keys are the keys of the element the path was read from and of its
	// ancestors, outermost first: an element's key is its position among the
	// document's elements in document order, from 0 (the root svg). A path a
	// use element draws has the use's keys.
	Keys []int
}

// Draw the compiled SvgPath into the Dasher.
func (svgp *SvgPath) Draw(r *rasterx.Dasher, opacity float64) {
	svgp.DrawTransformed(r, opacity, rasterx.Identity)
}

// DrawTransformed draws the compiled SvgPath into the Dasher while applying transform t.
func (svgp *SvgPath) DrawTransformed(r *rasterx.Dasher, opacity float64, t rasterx.Matrix2D) {
	m := svgp.mAdder.M
	svgp.mAdder.M = t.Mult(m)
	defer func() { svgp.mAdder.M = m }() // Restore untransformed matrix
	masks, clippedAway := svgp.clipMasks(r, t)
	if clippedAway {
		return
	}
	// withMask scales a colour's alpha by the clips in force (clipPath): a
	// path with none pays one length test.
	withMask := func(clr interface{}) interface{} {
		if len(masks) == 0 {
			return clr
		}
		return clipColor(clr, masks)
	}
	if svgp.fillerColor != nil {
		r.Clear()
		rf := &r.Filler
		rf.SetWinding(svgp.UseNonZeroWinding)
		svgp.mAdder.Adder = rf // This allows transformations to be applied
		svgp.Path.AddTo(&svgp.mAdder)

		switch fillerColor := svgp.fillerColor.(type) {
		case color.Color:
			rf.SetColor(withMask(rasterx.ApplyOpacity(fillerColor, svgp.FillOpacity*opacity)))
		case rasterx.Gradient:
			if fillerColor.Units == rasterx.ObjectBoundingBox {
				fRect := rf.Scanner.GetPathExtent()
				mnx, mny := float64(fRect.Min.X)/64, float64(fRect.Min.Y)/64
				mxx, mxy := float64(fRect.Max.X)/64, float64(fRect.Max.Y)/64
				fillerColor.Bounds.X, fillerColor.Bounds.Y = mnx, mny
				fillerColor.Bounds.W, fillerColor.Bounds.H = mxx-mnx, mxy-mny
			}
			rf.SetColor(withMask(fillerColor.GetColorFunction(svgp.FillOpacity * opacity)))
		}
		if svgp.UseNonZeroWinding || !fillEvenOdd(rf.Scanner, svgp.Path, svgp.mAdder.M) {
			rf.Draw()
		}
		// default is true
		rf.SetWinding(true)
	}
	if svgp.linerColor != nil {
		r.Clear()
		svgp.mAdder.Adder = r
		lineGap := svgp.LineGap
		if lineGap == nil {
			lineGap = DefaultStyle.LineGap
		}
		lineCap := svgp.LineCap
		if lineCap == nil {
			lineCap = DefaultStyle.LineCap
		}
		leadLineCap := lineCap
		if svgp.LeadLineCap != nil {
			leadLineCap = svgp.LeadLineCap
		}
		// The stroker works in device space, on the transformed path: the
		// width, the dashes and their offset, given in the element's user
		// units, scale with the transform as the path does (SVG 1.1 §11.4:
		// they are lengths in the user space), by the square root of its
		// determinant (exact for a uniform scale, a rotation, a reflection).
		k := math.Sqrt(math.Abs(svgp.mAdder.M.A*svgp.mAdder.M.D - svgp.mAdder.M.B*svgp.mAdder.M.C))
		dash := svgp.Dash
		if !svgp.dashesBounded(svgp.mAdder.M) {
			// more dashes than maxDashes (tiny ones along a long path): the
			// stroker would take one step each, without end in practice;
			// the stroke is drawn solid instead
			dash = nil
		}
		if k != 1 && len(dash) > 0 {
			dash = make([]float64, len(svgp.Dash))
			for i, v := range svgp.Dash {
				dash[i] = v * k
			}
		}
		r.SetStroke(fixed.Int26_6(svgp.LineWidth*k*64),
			fixed.Int26_6(svgp.MiterLimit*64), leadLineCap, lineCap,
			lineGap, svgp.LineJoin, dash, svgp.DashOffset*k)
		svgp.Path.AddTo(&svgp.mAdder)
		switch linerColor := svgp.linerColor.(type) {
		case color.Color:
			r.SetColor(withMask(rasterx.ApplyOpacity(linerColor, svgp.LineOpacity*opacity)))
		case rasterx.Gradient:
			if linerColor.Units == rasterx.ObjectBoundingBox {
				fRect := r.Scanner.GetPathExtent()
				mnx, mny := float64(fRect.Min.X)/64, float64(fRect.Min.Y)/64
				mxx, mxy := float64(fRect.Max.X)/64, float64(fRect.Max.Y)/64
				linerColor.Bounds.X, linerColor.Bounds.Y = mnx, mny
				linerColor.Bounds.W, linerColor.Bounds.H = mxx-mnx, mxy-mny
			}
			r.SetColor(withMask(linerColor.GetColorFunction(svgp.LineOpacity * opacity)))
		}
		r.Draw()
	}
}

// clipMasks resolves the clip-path properties in force on the path to masks
// under the icon transform t. clippedAway reports a clip that keeps nothing:
// the path is not drawn. A clip that cannot be applied (the reference does not
// name a clipPath, is circular, or its mask would pass maxMaskPixels) is
// skipped, as if the property were absent.
func (svgp *SvgPath) clipMasks(r *rasterx.Dasher, t rasterx.Matrix2D) (masks []*maskBuf, clippedAway bool) {
	if len(svgp.clips) == 0 {
		return nil, false
	}
	limit := limitFor(r)
	for _, cu := range svgp.clips {
		m := cu.mask(t, limit)
		switch {
		case m == nil:
		case m.empty():
			return nil, true
		default:
			masks = append(masks, m)
		}
	}
	return masks, false
}

// GetFillColor returns the fill color of the SvgPath if one is defined and otherwise returns colornames.Black
func (svgp *SvgPath) GetFillColor() color.Color {
	return getColor(svgp.fillerColor)
}

// GetLineColor returns the stroke color of the SvgPath if one is defined and otherwise returns colornames.Black
func (svgp *SvgPath) GetLineColor() color.Color {
	return getColor(svgp.linerColor)
}

// SetFillColor sets the fill color of the SvgPath
func (svgp *SvgPath) SetFillColor(clr color.Color) {
	svgp.fillerColor = clr
}

// SetLineColor sets the line color of the SvgPath
func (svgp *SvgPath) SetLineColor(clr color.Color) {
	svgp.linerColor = clr
}
