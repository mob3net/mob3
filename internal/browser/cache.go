package browser

import (
	"image"

	"browser/internal/host"
)

// resourceCache is the host-owned session cache with the browser's old API.
type resourceCache struct{ *host.ResourceCache }

func newResourceCache(limit int64) *resourceCache {
	return &resourceCache{host.NewResourceCache(limit)}
}
func (c *resourceCache) setLimit(v int64)                        { c.SetLimit(v) }
func (c *resourceCache) limitBytes() int64                       { return c.LimitBytes() }
func (c *resourceCache) stats() (used, limit int64, entries int) { return c.Stats() }
func (c *resourceCache) getBytes(key string) ([]byte, string, string, bool, bool) {
	return c.GetBytes(key)
}
func (c *resourceCache) putBytes(key string, data []byte, contentType, finalURL string, local bool) {
	c.PutBytes(key, data, contentType, finalURL, local)
}
func (c *resourceCache) getImage(key string) (image.Image, int, bool, bool) {
	return c.GetImage(key)
}
func (c *resourceCache) putImage(key string, img image.Image, rawSize int, local bool) {
	c.PutImage(key, img, rawSize, local)
}
