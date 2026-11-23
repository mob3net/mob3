package engine

import (
	"image"
	"sync"
	"time"
)

// MaxCachedPages bounds the session's back/forward page cache.
const MaxCachedPages = 10

const (
	pageNodeWeightBase  = 200
	pageNodeAttrWeight  = 48
	pageRuleWeightBase  = 384
	pageJSRuntimeWeight = 4 << 20
	pageJSNodeWeight    = 128
)

// CachedPage is a fully applied page kept for back/forward navigation.
type CachedPage struct {
	Page              *Page
	JS                *JSRuntime
	ReaderRoot        *Node
	ReaderSavedScroll int
	Method            string
	Body              []byte
	ContentType       string
	At                time.Time
	// Bytes is the estimated retained size used to keep the cache within its
	// budget.
	Bytes int64
}

// PageCache is a bounded, memory-only back/forward page cache.
type PageCache struct {
	mu      sync.Mutex
	pages   map[uint64]*CachedPage
	limit   int64
	onEvict func(int64, int64)
}

// NewPageCache creates a cache with an estimated byte limit. Zero disables it.
func NewPageCache(limit int64) *PageCache {
	return &PageCache{pages: make(map[uint64]*CachedPage), limit: limit}
}

// SetOnEvict installs the scavenge callback used after a heavy eviction.
func (c *PageCache) SetOnEvict(f func(int64, int64)) {
	c.mu.Lock()
	c.onEvict = f
	c.mu.Unlock()
}

// Put stores one page and trims the cache.
func (c *PageCache) Put(id uint64, p *CachedPage) {
	if c == nil || id == 0 || p == nil {
		return
	}
	c.mu.Lock()
	c.pages[id] = p
	c.trimLocked()
	c.mu.Unlock()
}

// Get returns one cached page and marks it recently used.
func (c *PageCache) Get(id uint64) *CachedPage {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.pages[id]
	if p != nil {
		p.At = time.Now()
	}
	return p
}

// Delete removes one entry and stops its runtime.
func (c *PageCache) Delete(id uint64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.deleteLocked(id)
	c.mu.Unlock()
}

// DeleteUnlessURL removes an entry when its page URL differs from u.
func (c *PageCache) DeleteUnlessURL(id uint64, u string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.pages[id]
	if p == nil || p.Page == nil || p.Page.URL == nil || p.Page.URL.String() == u {
		return
	}
	c.deleteLocked(id)
}

// DropAll clears the cache and stops runtimes.
func (c *PageCache) DropAll() {
	if c == nil {
		return
	}
	c.mu.Lock()
	for id, p := range c.pages {
		if p != nil && p.JS != nil {
			p.JS.Stop()
		}
		delete(c.pages, id)
	}
	c.mu.Unlock()
}

// SetLimitBytes changes the estimated budget and trims immediately.
func (c *PageCache) SetLimitBytes(limit int64) {
	if c == nil {
		return
	}
	if limit < 0 {
		limit = 0
	}
	c.mu.Lock()
	c.limit = limit
	c.trimLocked()
	c.mu.Unlock()
}

// Len reports the number of cached pages.
func (c *PageCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pages)
}

// CachedBytes reports the estimated retained bytes.
func (c *PageCache) CachedBytes() int64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	total := int64(0)
	for _, p := range c.pages {
		if p != nil {
			total += p.Bytes
		}
	}
	return total
}

// LimitBytes returns the configured byte budget.
func (c *PageCache) LimitBytes() int64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.limit
}

// PageCap is the largest single page the cache will retain.
func (c *PageCache) PageCap() int64 {
	if c == nil || c.limit <= 0 {
		return 0
	}
	return c.limit / 2
}

func (c *PageCache) deleteLocked(id uint64) {
	p := c.pages[id]
	if p == nil {
		return
	}
	if p.JS != nil {
		p.JS.Stop()
	}
	delete(c.pages, id)
}

func (c *PageCache) trimLocked() {
	type slot struct {
		id    uint64
		at    time.Time
		bytes int64
	}
	var slots []slot
	total := int64(0)
	for id, p := range c.pages {
		if p == nil {
			continue
		}
		slots = append(slots, slot{id: id, at: p.At, bytes: p.Bytes})
		total += p.Bytes
	}
	if c.limit <= 0 {
		for _, s := range slots {
			c.deleteLocked(s.id)
		}
		return
	}
	evictOldest := func() bool {
		if len(slots) == 0 {
			return false
		}
		oldest := 0
		for i := 1; i < len(slots); i++ {
			if slots[i].at.Before(slots[oldest].at) {
				oldest = i
			}
		}
		total -= slots[oldest].bytes
		if c.onEvict != nil {
			c.onEvict(slots[oldest].bytes, c.limit)
		}
		c.deleteLocked(slots[oldest].id)
		slots = append(slots[:oldest], slots[oldest+1:]...)
		return true
	}
	for len(slots) > MaxCachedPages {
		evictOldest()
	}
	for total > c.limit && len(slots) > 1 {
		evictOldest()
	}
}

// PageWeight estimates the retained bytes of a page, its scripts, decoded
// images, DOM and JavaScript runtime.
func PageWeight(page *Page, js *JSRuntime, body []byte, readerRoot *Node) int64 {
	if page == nil || page.Root == nil {
		return 0
	}
	w := int64(len(page.RawHTML)) + int64(len(body))
	for _, s := range page.Scripts {
		w += int64(len(s.Code))
	}
	w += int64(len(page.Author)) * pageRuleWeightBase
	w += DOMWeight(page.Root)
	if readerRoot != nil && readerRoot != page.Root {
		w += DOMWeight(readerRoot)
	}
	if js != nil {
		w += pageJSRuntimeWeight + int64(js.NodeCount())*pageJSNodeWeight
		w += int64(js.TemplateCount()) * pageNodeWeightBase
		w += int64(js.DatasetCount()) * pageJSNodeWeight
		w += int64(js.ModuleSourceBytes())
	}
	return w
}

// DOMWeight estimates a subtree's retained bytes.
func DOMWeight(root *Node) int64 {
	if root == nil {
		return 0
	}
	w := int64(0)
	var walk func(*Node)
	walk = func(n *Node) {
		w += pageNodeWeightBase
		for i := range n.Attrs {
			w += int64(len(n.Attrs[i].Key)+len(n.Attrs[i].Value)) + pageNodeAttrWeight
		}
		w += int64(len(n.Text) + len(n.Value) + len(n.ImgSrc))
		if n.Img != nil {
			w += ImageWeight(n.Img)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return w
}

// ImageWeight estimates the bytes a decoded image keeps resident.
func ImageWeight(img image.Image) int64 {
	if img == nil {
		return 0
	}
	b := img.Bounds()
	px := int64(b.Dx()) * int64(b.Dy())
	if px <= 0 {
		return 0
	}
	switch img.(type) {
	case *image.Paletted, *image.Gray:
		return px
	case *image.Gray16:
		return px * 2
	case *image.YCbCr:
		return px + px/2
	}
	return px * 4
}
