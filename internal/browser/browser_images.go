package browser

import (
	"context"
	"sort"
)

// refreshImages loads images that appeared or changed after the document's
// first image pass, such as lazy-loading scripts copying data-src into src or
// inserting new image elements. It returns true when a new source was found.
// imageBatch carries the decoded images of one deferred load.
type imageBatch struct {
	seq     int
	results []imageLoadResult
	done    bool
}

// cancelDeferredImageLoad abandons a background image load.
func (b *Browser) cancelDeferredImageLoad() {
	if b.imageCancel != nil {
		b.imageCancel()
		b.imageCancel = nil
	}
	b.DeferredImages = false
	b.ImageTotal = 0
	b.ImageDone = 0
}

// sortImageTasks orders pending images by their position relative to the
// viewport: visible first, then within one viewport, then the rest by distance.
// DOM order is preserved for equal ranks.
func (b *Browser) sortImageTasks(tasks []imageTask) {
	if len(tasks) < 2 || b.Layout == nil || b.Layout.Frags == nil {
		return
	}
	viewTop := b.Scroll
	viewH := b.viewportHeight()
	if viewH < 1 {
		viewH = 1
	}
	viewBottom := viewTop + viewH
	yOf := make(map[*Node]int, len(tasks))
	for i := range b.Layout.Frags {
		f := &b.Layout.Frags[i]
		if f.Node == nil {
			continue
		}
		if _, ok := yOf[f.Node]; !ok {
			yOf[f.Node] = f.Y
		}
	}
	type rank struct {
		bucket int
		dist   int
	}
	ranks := make(map[*Node]rank, len(tasks))
	for _, t := range tasks {
		y, ok := yOf[t.node]
		if !ok {
			ranks[t.node] = rank{bucket: 2, dist: 1 << 30}
			continue
		}
		switch {
		case y+1 > viewTop && y < viewBottom:
			ranks[t.node] = rank{bucket: 0}
		case y+1 <= viewTop:
			d := viewTop - (y + 1)
			if d <= viewH {
				ranks[t.node] = rank{bucket: 1, dist: d}
			} else {
				ranks[t.node] = rank{bucket: 2, dist: d}
			}
		default:
			d := y - viewBottom
			if d <= viewH {
				ranks[t.node] = rank{bucket: 1, dist: d}
			} else {
				ranks[t.node] = rank{bucket: 2, dist: d}
			}
		}
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		a, c := ranks[tasks[i].node], ranks[tasks[j].node]
		if a.bucket != c.bucket {
			return a.bucket < c.bucket
		}
		return a.dist < c.dist
	})
}

// startDeferredImageLoad queues the current page's images on a background
// goroutine. The page is already laid out and visible; results arrive on
// imageCh and are applied by the event loop.
func (b *Browser) startDeferredImageLoad() {
	if b.ImagesPaused || b.loader == nil || b.loader.noImages || b.Page == nil || b.Page.Root == nil || b.Reader {
		return
	}
	tasks := b.loader.imageTasks(b.Page.Root, b.Page.Base)
	if len(tasks) == 0 {
		return
	}
	b.sortImageTasks(tasks)
	b.cancelDeferredImageLoad()
	b.ImageTotal = len(tasks)
	b.ImageDone = 0
	// Keep the URL-row progress bar visible while images stream in; the
	// document itself is already complete.
	b.Progress = 0.9
	b.ProgressHold = 0
	ctx, cancel := context.WithCancel(context.Background())
	ctx = withRequestPage(ctx, b.Page.URL, b.Page.Local)
	b.imageCancel = cancel
	b.DeferredImages = true
	seq := b.LoadSeq
	go func() {
		stream := b.loader.imageResultStream(ctx, tasks)
		var pending []imageLoadResult
		send := func(done bool) bool {
			select {
			case b.imageCh <- imageBatch{seq: seq, results: pending, done: done}:
				pending = nil
				return true
			case <-ctx.Done():
				return false
			}
		}
		for r := range stream {
			pending = append(pending, r)
			if len(pending) >= 16 && !send(false) {
				return
			}
		}
		if len(pending) > 0 && !send(false) {
			return
		}
		send(true)
	}()
}

// fragmentRows maps each node to the first laid-out row of its own fragments.
func (b *Browser) fragmentRows(nodes []*Node) map[*Node]int {
	rows := make(map[*Node]int, len(nodes))
	if b.Layout == nil || len(b.Layout.Frags) == 0 || len(nodes) == 0 {
		return rows
	}
	want := make(map[*Node]struct{}, len(nodes))
	for _, n := range nodes {
		want[n] = struct{}{}
	}
	for i := range b.Layout.Frags {
		f := &b.Layout.Frags[i]
		if f.Node == nil {
			continue
		}
		if _, ok := want[f.Node]; !ok {
			continue
		}
		if _, seen := rows[f.Node]; seen {
			continue
		}
		rows[f.Node] = f.Y
	}
	return rows
}

// imageChangeReachesViewport reports whether any changed image starts above
// the current viewport bottom, and returns the topmost laid-out row of the
// images that do not. A node with no laid-out fragment counts as reaching the
// viewport because it cannot be proven to be out of view.
func (b *Browser) imageChangeReachesViewport(nodes []*Node) (bool, int) {
	rows := b.fragmentRows(nodes)
	viewH := b.viewportHeight()
	if viewH < 1 {
		viewH = 1
	}
	viewBottom := b.Scroll + viewH
	top := 0
	for _, n := range nodes {
		y, ok := rows[n]
		if !ok || y < viewBottom {
			return true, 0
		}
		if top == 0 || y < top {
			top = y
		}
	}
	return false, top
}

// applyPendingImageLayout lays out deferred images once the viewport is about
// to show them, or once it reaches the end of the document where a taller page
// may be waiting. While every pending image is still below the fold it leaves
// the layout and the frame untouched.
func (b *Browser) applyPendingImageLayout() {
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
func (b *Browser) applyDeferredImageBatch(batch imageBatch) bool {
	if b.Page == nil || batch.seq != b.LoadSeq || !b.DeferredImages {
		return false
	}
	if b.ImageTotal > 0 {
		b.ImageDone += len(batch.results)
		if b.ImageDone > b.ImageTotal {
			b.ImageDone = b.ImageTotal
		}
		frac := float64(b.ImageDone) / float64(b.ImageTotal)
		b.Progress = 0.9 + 0.09*frac
	}
	changed := false
	var changedNodes []*Node
	for _, r := range batch.results {
		if r.Node == nil {
			continue
		}
		if b.loader == nil || imageSource(r.Node, b.loader.imageViewportPx) != r.Src {
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
	if batch.done {
		b.DeferredImages = false
		b.imageCancel = nil
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
	return changed || len(batch.results) > 0 || batch.done
}

func (b *Browser) refreshImages() bool {
	if b == nil || b.loader == nil || b.loader.noImages || b.Page == nil || b.Page.Root == nil {
		return false
	}
	ctx := withRequestPage(context.Background(), b.Page.URL, b.Page.Local)
	return b.loader.loadImages(ctx, b.Page.Root, b.Page.Base)
}
