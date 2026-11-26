package engine

import "time"

// RebuildLayout lays out the current page under the session's configuration.
// UI overlays, hints, status text and frame invalidation stay with the caller;
// this method owns the layout pass and its page-display state.
func (s *Session) RebuildLayout(viewH int, bigText bool) {
	if s.Page == nil {
		return
	}
	// Frameworks inject their component CSS as <style> elements after load;
	// collect new or changed ones before this layout pass.
	if s.JS != nil && !s.Reader {
		s.Page.CollectDynamicStyles()
	}
	// The new layout includes every image decoded so far, so nothing is left
	// waiting for the viewport.
	s.PendingImageRow = 0
	// Scripts may set image sources on any interaction that triggers a rebuild.
	if s.JS != nil && !s.Reader && (s.Images == nil || !s.Images.DeferImages()) {
		s.RefreshImages()
	}
	start := time.Now()
	s.BigText = bigText
	root, author := s.Page.Root, s.Page.Author
	if s.Reader && s.ReaderRoot != nil {
		root, author = s.ReaderRoot, ReaderRules
	}
	abortGen := s.abortGen.Load()
	budget := s.LayoutBudget
	budget.abort = func() bool { return s.abortGen.Load() != abortGen }
	deadline := time.Time{}
	if budget.Time > 0 {
		deadline = start.Add(budget.Time)
	}
	layoutWidth := s.Width
	// Interactive pages reserve the rightmost column for the scrollbar, so
	// text and images wrap before it instead of painting underneath it.
	if !s.NoDraw && layoutWidth >= 2 {
		layoutWidth--
	}
	imagesOff := s.Images != nil && s.Images.ImagesDisabled()
	s.Layout = BuildLayoutBudgetDeadlineImages(root, author, layoutWidth, viewH, s.BigText, s.Dark, s.HideFixed, s.NoSticky, imagesOff, deadline, budget)
	s.Layout.BackgroundsOff = s.HideBackgrounds
	s.UpdatePDFPageStarts()
	if s.Layout.Stopped {
		s.pageStopped = true
		s.status = "Page stopped"
	} else {
		s.pageStopped = false
	}
	if s.BigText && !s.Layout.Stopped && LayoutTextOverlaps(s.Layout) {
		// Scaled kitty glyphs are wider than their base font. On dense
		// multi-column pages that pushes text from one column over another, so
		// this page falls back to normal-size text for the rest of its session.
		s.BigTextBlocked = true
		s.BigText = false
		start = time.Now()
		deadline = time.Time{}
		if budget.Time > 0 {
			deadline = start.Add(budget.Time)
		}
		s.Layout = BuildLayoutBudgetDeadlineImages(root, author, layoutWidth, viewH, false, s.Dark, s.HideFixed, s.NoSticky, imagesOff, deadline, budget)
		s.Layout.BackgroundsOff = s.HideBackgrounds
		s.UpdatePDFPageStarts()
		if s.Layout.Stopped {
			s.pageStopped = true
			s.status = "Page stopped"
		}
	}
	s.ScaledLayout = s.BigText
	s.PageHasHover = rulesUseHover(author)
	s.LastBuild = time.Since(start)
	s.LastRebuild = time.Now()
	if s.Layout != nil {
		s.Layout.SetFixedScroll(s.Scroll)
	}
	s.ClampScroll(viewH)
	if s.Images != nil && s.Images.DeferImages() && !s.Reader && !s.DeferredImages {
		s.StartDeferredImageLoad(viewH)
	}
}
