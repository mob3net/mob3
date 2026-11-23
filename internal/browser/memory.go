package browser

import (
	"context"
	"math"
	"runtime/debug"
	"sync"
)

// startupMemoryLimit is the Go runtime soft limit at process start. It reflects
// GOMEMLIMIT when one was supplied, so setting -mem-limit back to 0 can restore
// it instead of overwriting it with MaxInt64.
var startupMemoryLimit = debug.SetMemoryLimit(-1)

// Internal budgets when the user did not set a limit. They match the values
// used before budgets became percentages, and are replaced by the percentages
// below whenever -mem-limit or GOMEMLIMIT gives a base.
const (
	fallbackJSHeap             = int64(256) << 20
	fallbackImageBytes         = int64(128) << 20
	fallbackSVGPixels          = int64(8_000_000)
	fallbackResourceCacheBytes = int64(32) << 20
	fallbackHistoryCacheBytes  = int64(128) << 20
	jsHeapPercent              = 25
	imageBudgetPercent         = 12
	svgBudgetPercent           = 3
	resourceCachePercent       = 3
	historyCachePercent        = 12
	maxInlineSVGCount          = 256
)

// effectiveMemoryLimit reports the user's soft heap limit, or zero when the
// process is unlimited. It reads the runtime limit rather than the environment
// so -mem-limit and GOMEMLIMIT are handled the same way.
func effectiveMemoryLimit() int64 {
	limit := debug.SetMemoryLimit(-1)
	if limit <= 0 || limit >= math.MaxInt64/2 {
		return 0
	}
	return limit
}

// memoryFraction returns percent of base, with at least one byte.
func memoryFraction(base int64, percent int) int64 {
	if base <= 0 || percent <= 0 {
		return 0
	}
	var part int64
	if base <= math.MaxInt64/int64(percent) {
		part = base * int64(percent) / 100
	} else {
		part = base / 100 * int64(percent)
	}
	if part < 1 {
		part = 1
	}
	return part
}

// jsHeapBudget returns the per-page JS ceiling for base. Zero keeps the
// conservative fallback, used when the process itself is unlimited.
func jsHeapBudget(base int64) int64 {
	if base <= 0 {
		return fallbackJSHeap
	}
	return memoryFraction(base, jsHeapPercent)
}

func imageByteBudget(base int64) int64 {
	if base <= 0 {
		return fallbackImageBytes
	}
	return memoryFraction(base, imageBudgetPercent)
}

func svgPixelBudget(base int64) int64 {
	if base <= 0 {
		return fallbackSVGPixels
	}
	return memoryFraction(base, svgBudgetPercent) / 4
}

func resourceCacheBudget(base int64) int64 {
	if base <= 0 {
		return fallbackResourceCacheBytes
	}
	return memoryFraction(base, resourceCachePercent)
}

func historyCacheBudget(base int64) int64 {
	if base <= 0 {
		return fallbackHistoryCacheBytes
	}
	return memoryFraction(base, historyCachePercent)
}

// setMemoryLimitMB applies a process soft heap limit. Zero restores the
// startup limit (including an environment GOMEMLIMIT) and keeps the process
// unlimited when the user never configured one.
func (b *Browser) setMemoryLimitMB(mb int) {
	if mb < 0 {
		mb = 0
	}
	if mb == 0 {
		b.memLimitBytes = 0
		debug.SetMemoryLimit(startupMemoryLimit)
	} else {
		b.memLimitBytes = int64(mb) << 20
		debug.SetMemoryLimit(b.memLimitBytes)
	}
	base := effectiveMemoryLimit()
	b.JSMemLimit = jsHeapBudget(base)
	if b.JS != nil {
		b.JS.SetMemLimit(b.JSMemLimit)
	}
	b.Cache.SetLimitBytes(historyCacheBudget(base))
	if b.loader != nil {
		b.loader.setMemoryBudget(base)
	}
}

// setMemoryBudget derives the image and inline-SVG budgets from the effective
// process limit. A zero base keeps the conservative fallbacks.
func (l *Loader) setMemoryBudget(base int64) {
	if l == nil {
		return
	}
	bytes := imageByteBudget(base)
	if l.imageBudget != nil {
		l.imageBudget.setLimit(bytes)
	}
	l.svgMu.Lock()
	l.svgBudgetPixels = svgPixelBudget(base)
	l.svgMu.Unlock()
	if l.cache != nil {
		l.cache.setLimit(resourceCacheBudget(base))
	}
}

// byteLimiter is a context-aware weighted semaphore for decoded image bytes.
// A release wakes every waiter; there are only a handful of image workers.
type byteLimiter struct {
	mu    sync.Mutex
	limit int64
	used  int64
	wake  chan struct{}
}

func newByteLimiter(limit int64) *byteLimiter {
	if limit < 1 {
		limit = 1
	}
	return &byteLimiter{limit: limit, wake: make(chan struct{})}
}

func (l *byteLimiter) setLimit(limit int64) {
	if limit < 1 {
		limit = 1
	}
	l.mu.Lock()
	l.limit = limit
	close(l.wake)
	l.wake = make(chan struct{})
	l.mu.Unlock()
}

// acquire reserves n bytes, waiting until they fit, and returns the amount
// actually reserved. A single item larger than the whole budget is admitted
// alone rather than blocking forever.
func (l *byteLimiter) acquire(ctx context.Context, n int64) (int64, bool) {
	if n <= 0 {
		return 0, true
	}
	for {
		l.mu.Lock()
		if n > l.limit {
			n = l.limit
		}
		if l.used+n <= l.limit {
			l.used += n
			l.mu.Unlock()
			return n, true
		}
		wake := l.wake
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return 0, false
		case <-wake:
		}
	}
}

func (l *byteLimiter) release(n int64) {
	if n <= 0 {
		return
	}
	l.mu.Lock()
	l.used -= n
	if l.used < 0 {
		l.used = 0
	}
	close(l.wake)
	l.wake = make(chan struct{})
	l.mu.Unlock()
}

// takeScavengePending consumes one pending scavenge request.
func (b *Browser) takeScavengePending() bool {
	return b.scavengePending.Swap(false)
}
