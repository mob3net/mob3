package engine

import ()

// Deferred-image types live in the engine; the aliases keep the browser
// tests and the event loop on the same values during the migration.
type imageBatch = ImageBatch
type imageTask = ImageTask
type imageLoadResult = ImageLoadResult

// cancelDeferredImageLoad abandons a background image load.
func (b *Session) cancelDeferredImageLoad() { b.CancelDeferredImageLoad() }

// sortImageTasks orders pending images by their position relative to the
// viewport: visible first, then within one viewport, then the rest by distance.
// DOM order is preserved for equal ranks.
func (b *Session) sortImageTasks(tasks []imageTask) {
	b.SortImageTasks(tasks, b.viewportHeight())
}

// startDeferredImageLoad queues the current page's images on a background
// goroutine. The page is already laid out and visible; results arrive on
// ImageCh and are applied by the event loop.
func (b *Session) startDeferredImageLoad() {
	if b.loader == nil {
		b.Images = nil
	} else {
		b.Images = b.loader
	}
	b.StartDeferredImageLoad(b.viewportHeight())
}

// fragmentRows maps each node to the first laid-out row of its own fragments.
func (b *Session) fragmentRows(nodes []*Node) map[*Node]int {
	return b.ImageFragmentRows(nodes)
}

// imageChangeReachesViewport reports whether any changed image starts above
// the current viewport bottom, and returns the topmost laid-out row of the
// images that do not. A node with no laid-out fragment counts as reaching the
// viewport because it cannot be proven to be out of view.
func (b *Session) imageChangeReachesViewport(nodes []*Node) (bool, int) {
	return b.ImageChangeReachesViewport(nodes, b.viewportHeight())
}

// applyPendingImageLayout lays out deferred images once the viewport is about
// to show them, or once it reaches the end of the document where a taller page
// may be waiting. While every pending image is still below the fold it leaves
// the layout and the frame untouched.
func (b *Session) applyPendingImageLayout() {
	if b.PendingImageRow <= 0 || b.Layout == nil {
		return
	}
	viewH := b.viewportHeight()
	if viewH < 1 {
		viewH = 1
	}
	viewBottom := b.Scroll + viewH
	atBottom := b.Layout.Height > 0 && viewBottom >= b.Layout.Height
	if !atBottom && b.PendingImageRow >= viewBottom {
		return
	}
	b.PendingImageRow = 0
	b.rebuild()
	b.prev = nil
}

// applyDeferredImageBatch installs background image results for the current
// page, rebuilding only when the new images can affect what the viewport shows.
// It reports whether anything changed.
func (b *Session) applyDeferredImageBatch(batch imageBatch) bool {
	if b.Page == nil || batch.Seq != b.LoadSeq || !b.DeferredImages {
		return false
	}
	if b.ImageTotal > 0 {
		b.ImageDone += len(batch.Results)
		if b.ImageDone > b.ImageTotal {
			b.ImageDone = b.ImageTotal
		}
		frac := float64(b.ImageDone) / float64(b.ImageTotal)
		b.Progress = 0.9 + 0.09*frac
	}
	changed := false
	var changedNodes []*Node
	for _, r := range batch.Results {
		if r.Node == nil {
			continue
		}
		if b.loader == nil || imageSource(r.Node, b.loader.ViewportPx) != r.Src {
			continue
		}
		if r.Failed {
			r.Node.ImgSrc = r.Src
			r.Node.ImgFailed = true
			continue
		}
		if r.Img != nil {
			r.Node.ImgSrc = r.Src
			r.Node.Img = r.Img
			r.Node.ImgFailed = false
			changed = true
			changedNodes = append(changedNodes, r.Node)
		}
	}
	if batch.Done {
		b.DeferredImages = false
		b.ImageCancel = nil
		b.finishProgress()
	}
	if changed {
		if reaches, top := b.imageChangeReachesViewport(changedNodes); !reaches {
			// The new images sit below the viewport, so their boxes cannot move
			// anything the user can see. Keep the current layout and frame, and
			// lay them out only when the viewport approaches their row.
			if b.PendingImageRow == 0 || top < b.PendingImageRow {
				b.PendingImageRow = top
			}
		} else {
			b.PendingImageRow = 0
			b.rebuild()
			// Native terminal placements and kitty scaled glyphs are drawn
			// outside the cell text, so a line diff cannot erase a stale copy.
			// Half-block art lives in the cells the diff repaints, so a
			// text-only frame keeps the diff and avoids a full-screen blink.
			if b.inlineImagesEnabled() || b.BigText {
				b.prev = nil
			}
		}
	}
	// A batch with only failures or the final done marker still advances the
	// loading bar and footer count, so the frame needs a repaint.
	return changed || len(batch.Results) > 0 || batch.Done
}

func (b *Session) refreshImages() bool {
	if b.loader == nil {
		b.Images = nil
	} else {
		b.Images = b.loader
	}
	return b.RefreshImages()
}
