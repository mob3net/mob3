package engine

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
		ResolveLengthREM(l, e.rootFontPx)
	}
	lh := st.LineHeight
	if lh <= 0 {
		lh = 1
	}
	linePx := st.FontSize * lh
	ResolveViewportLength(&st.TextIndent, float64(e.viewportPx), e.viewportH)
	ResolveLengthLH(&st.TextIndent, linePx)
	for i := range st.Margin {
		ResolveViewportLength(&st.Margin[i], float64(e.viewportPx), e.viewportH)
		ResolveLengthLH(&st.Margin[i], linePx)
	}
	for i := range st.Padding {
		ResolveViewportLength(&st.Padding[i], float64(e.viewportPx), e.viewportH)
		ResolveLengthLH(&st.Padding[i], linePx)
	}
	for i := range st.Border {
		ResolveViewportLength(&st.Border[i], float64(e.viewportPx), e.viewportH)
		ResolveLengthLH(&st.Border[i], linePx)
	}
	ResolveViewportLength(&st.Width, float64(e.viewportPx), e.viewportH)
	ResolveLengthLH(&st.Width, linePx)
	ResolveViewportLength(&st.Height, float64(e.viewportPx), e.viewportH)
	ResolveLengthLH(&st.Height, linePx)
	ResolveViewportLength(&st.MinWidth, float64(e.viewportPx), e.viewportH)
	ResolveLengthLH(&st.MinWidth, linePx)
	ResolveViewportLength(&st.MaxWidth, float64(e.viewportPx), e.viewportH)
	ResolveLengthLH(&st.MaxWidth, linePx)
	ResolveViewportLength(&st.MinHeight, float64(e.viewportPx), e.viewportH)
	ResolveLengthLH(&st.MinHeight, linePx)
	ResolveViewportLength(&st.MaxHeight, float64(e.viewportPx), e.viewportH)
	ResolveLengthLH(&st.MaxHeight, linePx)
	ResolveViewportLength(&st.Top, float64(e.viewportPx), e.viewportH)
	ResolveLengthLH(&st.Top, linePx)
	ResolveViewportLength(&st.Right, float64(e.viewportPx), e.viewportH)
	ResolveLengthLH(&st.Right, linePx)
	ResolveViewportLength(&st.Bottom, float64(e.viewportPx), e.viewportH)
	ResolveLengthLH(&st.Bottom, linePx)
	ResolveViewportLength(&st.Left, float64(e.viewportPx), e.viewportH)
	ResolveLengthLH(&st.Left, linePx)
	ResolveViewportLength(&st.FlexBasis, float64(e.viewportPx), e.viewportH)
	ResolveLengthLH(&st.FlexBasis, linePx)
	ResolveViewportLength(&st.Gap, float64(e.viewportPx), e.viewportH)
	ResolveLengthLH(&st.Gap, linePx)
	ResolveViewportLength(&st.RowGap, float64(e.viewportPx), e.viewportH)
	ResolveLengthLH(&st.RowGap, linePx)
}
