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
	b.deferredImages = false
	b.imageTotal = 0
	b.imageDone = 0
}

// sortImageTasks orders pending images by their position relative to the
// viewport: visible first, then within one viewport, then the rest by distance.
// DOM order is preserved for equal ranks.
func (b *Browser) sortImageTasks(tasks []imageTask) {
	if len(tasks) < 2 || b.layout == nil || b.layout.Frags == nil {
		return
	}
	viewTop := b.scroll
	viewH := b.viewportHeight()
	if viewH < 1 {
		viewH = 1
	}
	viewBottom := viewTop + viewH
	yOf := make(map[*Node]int, len(tasks))
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
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
	if b.imagesPaused || b.loader == nil || b.loader.noImages || b.page == nil || b.page.Root == nil || b.reader {
		return
	}
	tasks := b.loader.imageTasks(b.page.Root, b.page.Base)
	if len(tasks) == 0 {
		return
	}
	b.sortImageTasks(tasks)
	b.cancelDeferredImageLoad()
	b.imageTotal = len(tasks)
	b.imageDone = 0
	// Keep the URL-row progress bar visible while images stream in; the
	// document itself is already complete.
	b.progress = 0.9
	b.progressHold = 0
	ctx, cancel := context.WithCancel(context.Background())
	b.imageCancel = cancel
	b.deferredImages = true
	seq := b.loadSeq
	allowFile := b.page.URL != nil && b.page.URL.Scheme == "file"
	go func() {
		stream := b.loader.imageResultStream(ctx, tasks, allowFile)
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

// applyDeferredImageBatch installs background image results for the current
// page and rebuilds once. It reports whether anything changed.
func (b *Browser) applyDeferredImageBatch(batch imageBatch) bool {
	if b.page == nil || batch.seq != b.loadSeq || !b.deferredImages {
		return false
	}
	if b.imageTotal > 0 {
		b.imageDone += len(batch.results)
		if b.imageDone > b.imageTotal {
			b.imageDone = b.imageTotal
		}
		frac := float64(b.imageDone) / float64(b.imageTotal)
		b.progress = 0.9 + 0.09*frac
	}
	changed := false
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
		}
	}
	if batch.done {
		b.deferredImages = false
		b.imageCancel = nil
		b.finishProgress()
	}
	if changed {
		b.rebuild()
		// Reflowed text and new terminal image placements can leave stale
		// cells and scaled glyphs behind. Force the same full repaint that a
		// scroll triggers instead of trusting the line diff.
		b.prev = nil
	}
	// A batch with only failures or the final done marker still advances the
	// loading bar and footer count, so the frame needs a repaint.
	return changed || len(batch.results) > 0 || batch.done
}

func (b *Browser) refreshImages() bool {
	if b == nil || b.loader == nil || b.loader.noImages || b.page == nil || b.page.Root == nil {
		return false
	}
	allowFile := b.page.URL != nil && b.page.URL.Scheme == "file"
	return b.loader.loadImages(context.Background(), b.page.Root, b.page.Base, allowFile)
}
