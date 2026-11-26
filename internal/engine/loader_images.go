package engine

import (
	"context"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"sync"

	"browser/internal/protocol"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

// ImageTasks collects images that still need loading. It must run on the main
// layout goroutine because it reads node attributes.
func (l *Loader) ImageTasks(root *Node, base *url.URL) []ImageTask {
	if l.NoImages {
		return nil
	}
	return ImageTasks(root, base, l.ViewportPx)
}

// ImageContext prepares the request context for a page's image batch.
func (l *Loader) ImageContext(page *Page) context.Context {
	if page == nil {
		return context.Background()
	}
	return protocol.WithRequestPage(context.Background(), page.URL, page.Local)
}

// ImageResults fetches and decodes a task list, streaming results as they
// finish.
func (l *Loader) ImageResults(ctx context.Context, tasks []ImageTask) <-chan ImageLoadResult {
	return l.imageResultStream(ctx, tasks)
}

// ImagesDisabled reports whether image loading is turned off.
func (l *Loader) ImagesDisabled() bool { return l.NoImages }

// DeferImages reports whether the page uses deferred image loading.
func (l *Loader) DeferImages() bool { return l.DeferImageLoad }

// ImageViewportPx is the CSS-pixel viewport width used for source-set picks.
func (l *Loader) ImageViewportPx() int { return l.ViewportPx }

// FitImage downscales a decoded image so neither side exceeds the loader's
// pixel cap.
func (l *Loader) FitImage(img image.Image) image.Image {
	maxW, maxH := l.ImageCap()
	return FitImage(img, maxW, maxH)
}

// DecodeAndFit decodes one image under the in-flight byte budget and scales it
// to the terminal box before releasing the reservation.
func (l *Loader) DecodeAndFit(ctx context.Context, ctype string, data []byte) (image.Image, error) {
	weight := ImageDecodeWeight(ctype, data)
	if l.ImageBudget != nil {
		admitted, ok := l.ImageBudget.Acquire(ctx, weight)
		if !ok {
			return nil, ctx.Err()
		}
		defer l.ImageBudget.Release(admitted)
	}
	img, err := DecodeMedia(ctx, ctype, data)
	if err != nil {
		return nil, err
	}
	return l.FitImage(img), nil
}

// FetchImageResult downloads and decodes one image, using the session cache.
func (l *Loader) FetchImageResult(ctx context.Context, u *url.URL, gen int) (image.Image, bool) {
	u, err := l.Host.ApplyMixedContent(ctx, u)
	if err != nil {
		l.RecordBlocked(gen, "GET", u, "img", err)
		return nil, false
	}
	key := u.String()
	meta := protocol.RequestMetaFrom(ctx)
	cache := l.imageCache()
	if cache != nil {
		if cached, rawSize, cachedLocal, ok := cache.GetCachedImage(key); ok && !(meta.BlockLocal && cachedLocal) {
			l.RecordCacheHit(gen, "GET", key, "img", rawSize)
			return cached, true
		}
	}
	data, ctype, _, status, err := l.FetchCtx(ctx, u)
	if err != nil || status < 200 || status >= 400 {
		return nil, false
	}
	img, err := l.DecodeAndFit(ctx, ctype, data)
	if err != nil {
		return nil, false
	}
	if cache != nil {
		cache.PutCachedImage(key, img, len(data), !meta.BlockLocal)
	}
	return img, true
}

// imageCache returns the optional decoded-image cache. A process-backed host
// may omit it; the loader then decodes each image instead of sharing one.
func (l *Loader) imageCache() protocol.ImageCache {
	if l == nil || l.Host == nil {
		return nil
	}
	cache, _ := l.Host.(protocol.ImageCache)
	return cache
}

// imageResultStream fetches the queued images concurrently and streams decoded
// results as they finish. It never writes to the DOM, so it can run off the
// main loop; the caller applies results when it owns the event loop.
func (l *Loader) imageResultStream(ctx context.Context, tasks []ImageTask) <-chan ImageLoadResult {
	out := make(chan ImageLoadResult)
	if len(tasks) == 0 {
		close(out)
		return out
	}
	ctx = protocol.WithRequestKind(ctx, "img")
	gen := protocol.RequestMetaFrom(ctx).Gen
	if gen == 0 {
		gen = l.CurrentPageGeneration()
	}
	go func() {
		defer close(out)
		// Six workers pull tasks in priority order. A worker takes the next
		// queued image as soon as its current download finishes.
		jobs := make(chan ImageTask)
		go func() {
			defer close(jobs)
			for _, t := range tasks {
				select {
				case jobs <- t:
				case <-ctx.Done():
					return
				}
			}
		}()
		var wg sync.WaitGroup
		for w := 0; w < 6; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for t := range jobs {
					if t.URL == nil {
						select {
						case out <- ImageLoadResult{Node: t.Node, Src: t.Src, Failed: true}:
						case <-ctx.Done():
							return
						}
						continue
					}
					img, ok := l.FetchImageResult(ctx, t.URL, gen)
					select {
					case out <- ImageLoadResult{Node: t.Node, Src: t.Src, Img: img, Failed: !ok}:
					case <-ctx.Done():
						return
					}
				}
			}()
		}
		wg.Wait()
	}()
	return out
}

// LoadImageTaskResults collects the stream for callers that want one batch.
func (l *Loader) LoadImageTaskResults(ctx context.Context, tasks []ImageTask) []ImageLoadResult {
	var results []ImageLoadResult
	for r := range l.imageResultStream(ctx, tasks) {
		results = append(results, r)
	}
	return results
}

func (l *Loader) LoadImages(ctx context.Context, root *Node, base *url.URL) bool {
	if l.NoImages {
		return false
	}
	ctx = protocol.WithRequestKind(ctx, "img")
	imgs := root.FindAll(func(n *Node) bool {
		if n.Tag == "img" {
			return true
		}
		return n.Style != nil && n.Style.BackgroundImage != ""
	})
	if len(imgs) == 0 {
		return false
	}
	meta := protocol.RequestMetaFrom(ctx)
	gen := meta.Gen
	if gen == 0 {
		gen = l.CurrentPageGeneration()
	}
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	changed := false
	cache := l.imageCache()
	for _, n := range imgs {
		src := ImageSource(n, l.ViewportPx)
		if src == "" {
			continue
		}
		if n.ImgSrc == src && (n.Img != nil || n.ImgFailed) {
			continue
		}
		changed = true
		n.ImgSrc = src
		n.Img = nil
		n.ImgFailed = false
		u, err := base.Parse(src)
		if err != nil {
			n.ImgFailed = true
			continue
		}
		u, mcErr := l.Host.ApplyMixedContent(ctx, u)
		if mcErr != nil {
			l.RecordBlocked(gen, "GET", u, "img", mcErr)
			n.ImgFailed = true
			continue
		}
		key := u.String()
		if cache != nil {
			if cached, rawSize, cachedLocal, ok := cache.GetCachedImage(key); ok && !(meta.BlockLocal && cachedLocal) {
				l.RecordCacheHit(gen, "GET", key, "img", rawSize)
				n.Img = cached
				continue
			}
		}
		wg.Add(1)
		go func(node *Node, u *url.URL) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				node.ImgFailed = true
				return
			}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				node.ImgFailed = true
				return
			}
			data, ctype, _, status, err := l.FetchCtx(ctx, u)
			if err != nil || status < 200 || status >= 400 {
				node.ImgFailed = true
				return
			}
			img, err := l.DecodeAndFit(ctx, ctype, data)
			if err != nil {
				node.ImgFailed = true
				return
			}
			node.Img = img
			if cache != nil {
				cache.PutCachedImage(key, img, len(data), !meta.BlockLocal)
			}
		}(n, u)
	}
	wg.Wait()
	return changed
}

// HelpPageHTML wraps the help body in the shared page shell.

// HistoryPageHTML renders the in-memory session history as clickable links.
