package browser

import (
	"container/list"
	"image"
	"strings"
	"sync"
)

const (
	// defaultResourceCacheLimit is the session budget for cached CSS, scripts
	// and decoded images.
	defaultResourceCacheLimit = 32 << 20
	// defaultResourceCacheEntryLimit keeps one large asset from evicting many
	// useful small ones.
	defaultResourceCacheEntryLimit = 2 << 20
)

// cacheEntry is one decoded image or raw subresource body. Exactly one of data
// and img is set.
type cacheEntry struct {
	key         string
	data        []byte
	img         image.Image
	contentType string
	finalURL    string
	size        int64
}

// resourceCache is a session-only LRU byte cache shared by raw subresources and
// decoded images. It is never written to disk.
type resourceCache struct {
	mu       sync.Mutex
	entries  map[string]*list.Element
	lru      *list.List // front is most recently used
	size     int64
	limit    int64
	maxEntry int64
}

func newResourceCache(limit int64) *resourceCache {
	c := &resourceCache{entries: make(map[string]*list.Element), lru: list.New()}
	c.setLimit(limit)
	return c
}

// setLimit changes the total budget in bytes and evicts entries as needed.
func (c *resourceCache) setLimit(limit int64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if limit < 0 {
		limit = 0
	}
	c.limit = limit
	c.maxEntry = defaultResourceCacheEntryLimit
	if limit > 0 && c.maxEntry > limit {
		c.maxEntry = limit
	}
	c.evictLocked()
}

func (c *resourceCache) limitBytes() int64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.limit
}

func (c *resourceCache) getBytes(key string) (data []byte, contentType, finalURL string, ok bool) {
	if c == nil {
		return nil, "", "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el := c.entries[key]
	if el == nil {
		return nil, "", "", false
	}
	e := el.Value.(*cacheEntry)
	if e.data == nil {
		return nil, "", "", false
	}
	c.lru.MoveToFront(el)
	return e.data, e.contentType, e.finalURL, true
}

func (c *resourceCache) putBytes(key string, data []byte, contentType, finalURL string) {
	if c == nil || len(data) == 0 {
		return
	}
	c.put(&cacheEntry{
		key: key, data: data, contentType: contentType,
		finalURL: finalURL, size: int64(len(data)),
	})
}

func (c *resourceCache) getImage(key string) (image.Image, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el := c.entries[key]
	if el == nil {
		return nil, false
	}
	e := el.Value.(*cacheEntry)
	if e.img == nil {
		return nil, false
	}
	c.lru.MoveToFront(el)
	return e.img, true
}

func (c *resourceCache) putImage(key string, img image.Image) {
	if img == nil {
		return
	}
	c.put(&cacheEntry{key: key, img: img, size: imageMemorySize(img)})
}

func (c *resourceCache) put(e *cacheEntry) {
	if c == nil || e.size <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.limit <= 0 || e.size > c.maxEntry {
		return
	}
	if old := c.entries[e.key]; old != nil {
		c.removeLocked(old)
	}
	el := c.lru.PushFront(e)
	c.entries[e.key] = el
	c.size += e.size
	c.evictLocked()
}

func (c *resourceCache) evictLocked() {
	for c.limit <= 0 || c.size > c.limit {
		back := c.lru.Back()
		if back == nil {
			return
		}
		c.removeLocked(back)
	}
}

func (c *resourceCache) removeLocked(el *list.Element) {
	e := el.Value.(*cacheEntry)
	delete(c.entries, e.key)
	c.lru.Remove(el)
	c.size -= e.size
	if c.size < 0 {
		c.size = 0
	}
}

func imageMemorySize(img image.Image) int64 {
	if img == nil {
		return 0
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return 0
	}
	return int64(w)*int64(h)*4 + 128
}

// cacheControlAllows reports whether a response may be stored in the session
// memory cache.
func cacheControlAllows(header string) bool {
	for _, part := range strings.Split(strings.ToLower(header), ",") {
		if strings.TrimSpace(part) == "no-store" {
			return false
		}
	}
	return true
}

// setCacheLimitMB updates the session resource-cache budget in MiB.
func (l *Loader) setCacheLimitMB(mb int) {
	if l == nil {
		return
	}
	if mb < 0 {
		mb = 0
	}
	if l.cache == nil {
		l.cache = newResourceCache(int64(mb) << 20)
		return
	}
	l.cache.setLimit(int64(mb) << 20)
}

func (l *Loader) cacheLimitMB() int {
	if l == nil || l.cache == nil {
		return 0
	}
	return int(l.cache.limitBytes() >> 20)
}
