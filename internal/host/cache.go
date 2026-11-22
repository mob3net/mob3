package host

import (
	"container/list"
	"image"
	"strings"
	"sync"
)

const (
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
	rawSize     int
	size        int64
	// local marks a resource fetched while browsing a local document. A public
	// document may not reuse it from the cache; the local-network guard would
	// otherwise be bypassed without a request.
	local bool
}

// resourceCache is a session-only LRU byte cache shared by raw subresources and
// decoded images. It is never written to disk.
type ResourceCache struct {
	mu       sync.Mutex
	entries  map[string]*list.Element
	lru      *list.List // front is most recently used
	size     int64
	limit    int64
	maxEntry int64
}

func NewResourceCache(limit int64) *ResourceCache {
	c := &ResourceCache{entries: make(map[string]*list.Element), lru: list.New()}
	c.SetLimit(limit)
	return c
}

// setLimit changes the total budget in bytes and evicts entries as needed.
func (c *ResourceCache) SetLimit(limit int64) {
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

func (c *ResourceCache) LimitBytes() int64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.limit
}

// stats returns the current bytes used, total budget and entry count.
func (c *ResourceCache) Stats() (used, limit int64, entries int) {
	if c == nil {
		return 0, 0, 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.size, c.limit, len(c.entries)
}

func (c *ResourceCache) GetBytes(key string) (data []byte, contentType, finalURL string, local, ok bool) {
	if c == nil {
		return nil, "", "", false, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el := c.entries[key]
	if el == nil {
		return nil, "", "", false, false
	}
	e := el.Value.(*cacheEntry)
	if e.data == nil {
		return nil, "", "", false, false
	}
	c.lru.MoveToFront(el)
	return e.data, e.contentType, e.finalURL, e.local, true
}

func (c *ResourceCache) PutBytes(key string, data []byte, contentType, finalURL string, local bool) {
	if c == nil || len(data) == 0 {
		return
	}
	c.put(&cacheEntry{
		key: key, data: data, contentType: contentType,
		finalURL: finalURL, size: int64(len(data)), local: local,
	})
}

func (c *ResourceCache) GetImage(key string) (image.Image, int, bool, bool) {
	if c == nil {
		return nil, 0, false, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el := c.entries[key]
	if el == nil {
		return nil, 0, false, false
	}
	e := el.Value.(*cacheEntry)
	if e.img == nil {
		return nil, 0, false, false
	}
	c.lru.MoveToFront(el)
	return e.img, e.rawSize, e.local, true
}

func (c *ResourceCache) PutImage(key string, img image.Image, rawSize int, local bool) {
	if img == nil {
		return
	}
	c.put(&cacheEntry{key: key, img: img, rawSize: rawSize, size: imageMemorySize(img), local: local})
}

func (c *ResourceCache) put(e *cacheEntry) {
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

func (c *ResourceCache) evictLocked() {
	for c.limit <= 0 || c.size > c.limit {
		back := c.lru.Back()
		if back == nil {
			return
		}
		c.removeLocked(back)
	}
}

func (c *ResourceCache) removeLocked(el *list.Element) {
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
func CacheControlAllows(header string) bool {
	for _, part := range strings.Split(strings.ToLower(header), ",") {
		if strings.TrimSpace(part) == "no-store" {
			return false
		}
	}
	return true
}
