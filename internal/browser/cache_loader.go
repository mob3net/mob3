package browser

import "browser/internal/host"

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

func cacheControlAllows(header string) bool { return host.CacheControlAllows(header) }
