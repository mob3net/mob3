package browser

func (e *styleEngine) resolveStyleUnits(st *Style) {
	// rem resolves against the root element's font size, not a fixed 16px, so
	// a page that sets html{font-size:14px} (Bulma, many frameworks) gets the
	// same rem-based margins, padding and widths as a browser.
	for _, l := range []*Length{
		&st.TextIndent,
		&st.Margin[0], &st.Margin[1], &st.Margin[2], &st.Margin[3],
		&st.Padding[0], &st.Padding[1], &st.Padding[2], &st.Padding[3],
		&st.Border[0], &st.Border[1], &st.Border[2], &st.Border[3],
		&st.Width, &st.Height, &st.MinWidth, &st.MaxWidth,
		&st.MinHeight, &st.MaxHeight,
		&st.Top, &st.Right, &st.Bottom, &st.Left,
		&st.FlexBasis, &st.Gap, &st.RowGap,
	} {
		resolveLengthREM(l, e.rootFontPx)
	}
	lh := st.LineHeight
	if lh <= 0 {
		lh = 1
	}
	linePx := st.FontSize * lh
	resolveViewportLength(&st.TextIndent, float64(e.viewportPx), e.viewportH)
	resolveLengthLH(&st.TextIndent, linePx)
	for i := range st.Margin {
		resolveViewportLength(&st.Margin[i], float64(e.viewportPx), e.viewportH)
		resolveLengthLH(&st.Margin[i], linePx)
	}
	for i := range st.Padding {
		resolveViewportLength(&st.Padding[i], float64(e.viewportPx), e.viewportH)
		resolveLengthLH(&st.Padding[i], linePx)
	}
	for i := range st.Border {
		resolveViewportLength(&st.Border[i], float64(e.viewportPx), e.viewportH)
		resolveLengthLH(&st.Border[i], linePx)
	}
	resolveViewportLength(&st.Width, float64(e.viewportPx), e.viewportH)
	resolveLengthLH(&st.Width, linePx)
	resolveViewportLength(&st.Height, float64(e.viewportPx), e.viewportH)
	resolveLengthLH(&st.Height, linePx)
	resolveViewportLength(&st.MinWidth, float64(e.viewportPx), e.viewportH)
	resolveLengthLH(&st.MinWidth, linePx)
	resolveViewportLength(&st.MaxWidth, float64(e.viewportPx), e.viewportH)
	resolveLengthLH(&st.MaxWidth, linePx)
	resolveViewportLength(&st.MinHeight, float64(e.viewportPx), e.viewportH)
	resolveLengthLH(&st.MinHeight, linePx)
	resolveViewportLength(&st.MaxHeight, float64(e.viewportPx), e.viewportH)
	resolveLengthLH(&st.MaxHeight, linePx)
	resolveViewportLength(&st.Top, float64(e.viewportPx), e.viewportH)
	resolveLengthLH(&st.Top, linePx)
	resolveViewportLength(&st.Right, float64(e.viewportPx), e.viewportH)
	resolveLengthLH(&st.Right, linePx)
	resolveViewportLength(&st.Bottom, float64(e.viewportPx), e.viewportH)
	resolveLengthLH(&st.Bottom, linePx)
	resolveViewportLength(&st.Left, float64(e.viewportPx), e.viewportH)
	resolveLengthLH(&st.Left, linePx)
	resolveViewportLength(&st.FlexBasis, float64(e.viewportPx), e.viewportH)
	resolveLengthLH(&st.FlexBasis, linePx)
	resolveViewportLength(&st.Gap, float64(e.viewportPx), e.viewportH)
	resolveLengthLH(&st.Gap, linePx)
	resolveViewportLength(&st.RowGap, float64(e.viewportPx), e.viewportH)
	resolveLengthLH(&st.RowGap, linePx)
}
