package engine

import (
	"context"
	"image"
	"net/url"
	"sort"
)

// ImageTask is one image to fetch without touching the DOM, so the work can
// run on a background goroutine.
type ImageTask struct {
	Node *Node
	Src  string
	URL  *url.URL
}

// ImageLoadResult is one decoded image (or failure) returned to the main loop.
type ImageLoadResult struct {
	Node   *Node
	Src    string
	Img    image.Image
	Failed bool
}

// ImageBatch carries the decoded images of one deferred load.
type ImageBatch struct {
	Seq     int
	Results []ImageLoadResult
	Done    bool
}

// ImageLoader is the host-backed image fetch/decode port used by the engine
// session. The browser Loader implements it during the boundary migration.
type ImageLoader interface {
	ImageContext(page *Page) context.Context
	ImageResults(ctx context.Context, tasks []ImageTask) <-chan ImageLoadResult
	LoadImages(ctx context.Context, root *Node, base *url.URL) bool
	ImagesDisabled() bool
	DeferImages() bool
	ImageViewportPx() int
}

// ImageTasks collects images that still need loading. It must run on the main
// layout goroutine because it reads node attributes.
func ImageTasks(root *Node, base *url.URL, viewportPx int) []ImageTask {
	if root == nil || base == nil {
		return nil
	}
	var tasks []ImageTask
	for _, n := range root.FindAll(func(n *Node) bool {
		if n.Tag == "img" {
			return true
		}
		return n.Style != nil && n.Style.BackgroundImage != ""
	}) {
		src := ImageSource(n, viewportPx)
		if src == "" {
			continue
		}
		if n.ImgSrc == src && (n.Img != nil || n.ImgFailed) {
			continue
		}
		t := ImageTask{Node: n, Src: src}
		if u, err := base.Parse(src); err == nil {
			t.URL = u
		}
		tasks = append(tasks, t)
	}
	return tasks
}

// SortImageTasks orders pending images by their position relative to the
// viewport: visible first, then within one viewport, then the rest by distance.
// DOM order is preserved for equal ranks.
func (s *Session) SortImageTasks(tasks []ImageTask, viewH int) {
	if len(tasks) < 2 || s.Layout == nil || s.Layout.Frags == nil {
		return
	}
	viewTop := s.Scroll
	if viewH < 1 {
		viewH = 1
	}
	viewBottom := viewTop + viewH
	yOf := make(map[*Node]int, len(tasks))
	for i := range s.Layout.Frags {
		f := &s.Layout.Frags[i]
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
		y, ok := yOf[t.Node]
		if !ok {
			ranks[t.Node] = rank{bucket: 2, dist: 1 << 30}
			continue
		}
		switch {
		case y+1 > viewTop && y < viewBottom:
			ranks[t.Node] = rank{bucket: 0}
		case y+1 <= viewTop:
			d := viewTop - (y + 1)
			if d <= viewH {
				ranks[t.Node] = rank{bucket: 1, dist: d}
			} else {
				ranks[t.Node] = rank{bucket: 2, dist: d}
			}
		default:
			d := y - viewBottom
			if d <= viewH {
				ranks[t.Node] = rank{bucket: 1, dist: d}
			} else {
				ranks[t.Node] = rank{bucket: 2, dist: d}
			}
		}
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		a, c := ranks[tasks[i].Node], ranks[tasks[j].Node]
		if a.bucket != c.bucket {
			return a.bucket < c.bucket
		}
		return a.dist < c.dist
	})
}

// ImageFragmentRows maps each node to the first laid-out row of its own
// fragments.
func (s *Session) ImageFragmentRows(nodes []*Node) map[*Node]int {
	rows := make(map[*Node]int, len(nodes))
	if s.Layout == nil || len(s.Layout.Frags) == 0 || len(nodes) == 0 {
		return rows
	}
	want := make(map[*Node]struct{}, len(nodes))
	for _, n := range nodes {
		want[n] = struct{}{}
	}
	for i := range s.Layout.Frags {
		f := &s.Layout.Frags[i]
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

// ImageChangeReachesViewport reports whether any changed image starts above
// the current viewport bottom, and returns the topmost laid-out row of the
// images that do not. A node with no laid-out fragment counts as reaching the
// viewport because it cannot be proven to be out of view.
func (s *Session) ImageChangeReachesViewport(nodes []*Node, viewH int) (bool, int) {
	rows := s.ImageFragmentRows(nodes)
	if viewH < 1 {
		viewH = 1
	}
	viewBottom := s.Scroll + viewH
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

// CancelDeferredImageLoad abandons a background image load.
func (s *Session) CancelDeferredImageLoad() {
	if s.ImageCancel != nil {
		s.ImageCancel()
		s.ImageCancel = nil
	}
	s.DeferredImages = false
	s.ImageTotal = 0
	s.ImageDone = 0
}

// StartDeferredImageLoad queues the current page's images on a background
// goroutine. The page is already laid out and visible; results arrive on
// ImageCh and are applied by the event loop.
func (s *Session) StartDeferredImageLoad(viewH int) {
	if !s.active || s.ImagesPaused || s.Images == nil || s.Images.ImagesDisabled() || s.Page == nil || s.Page.Root == nil || s.Reader {
		return
	}
	tasks := ImageTasks(s.Page.Root, s.Page.Base, s.Images.ImageViewportPx())
	if len(tasks) == 0 {
		return
	}
	s.SortImageTasks(tasks, viewH)
	s.CancelDeferredImageLoad()
	s.ImageTotal = len(tasks)
	s.ImageDone = 0
	// Keep the URL-row progress bar visible while images stream in; the
	// document itself is already complete.
	s.Progress = 0.9
	s.ProgressHold = 0
	ctx, cancel := context.WithCancel(s.Images.ImageContext(s.Page))
	s.ImageCancel = cancel
	s.DeferredImages = true
	if s.ImageCh == nil {
		s.ImageCh = make(chan ImageBatch, 1)
	}
	seq := s.LoadSeq
	go func() {
		stream := s.Images.ImageResults(ctx, tasks)
		var pending []ImageLoadResult
		send := func(done bool) bool {
			select {
			case s.ImageCh <- ImageBatch{Seq: seq, Results: pending, Done: done}:
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

// RefreshImages loads images that appeared or changed after the document's
// first image pass, such as lazy-loading scripts copying data-src into src or
// inserting new image elements. It returns true when a new source was found.
func (s *Session) RefreshImages() bool {
	if s.Images == nil || s.Images.ImagesDisabled() || s.Page == nil || s.Page.Root == nil {
		return false
	}
	return s.Images.LoadImages(s.Images.ImageContext(s.Page), s.Page.Root, s.Page.Base)
}
