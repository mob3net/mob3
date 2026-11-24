package engine

import (
	"context"
	"math"
	"runtime/debug"
	"sync"
)

// Total internal pools when the user did not set a limit. BudgetManager divides
// the JS, history, image and SVG pools across sessions; the host resource
// cache is shared and not divided.
const (
	fallbackImageBytes         = int64(128) << 20
	fallbackSVGPixels          = int64(8_000_000)
	fallbackResourceCacheBytes = int64(32) << 20
	imageBudgetPercent         = 12
	svgBudgetPercent           = 3
	resourceCachePercent       = 3
	maxInlineSVGCount          = 256
)

// EffectiveMemoryLimit reports the process soft heap limit, or zero when the
// process is unlimited. It reads the runtime limit so -mem-limit and
// GOMEMLIMIT are handled the same way.
func EffectiveMemoryLimit() int64 {
	limit := debug.SetMemoryLimit(-1)
	if limit <= 0 || limit >= math.MaxInt64/2 {
		return 0
	}
	return limit
}

// MemoryFraction returns percent of base, with at least one byte.
func MemoryFraction(base int64, percent int) int64 {
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

func imageByteBudget(base int64) int64 {
	if base <= 0 {
		return fallbackImageBytes
	}
	return MemoryFraction(base, imageBudgetPercent)
}

func svgPixelBudget(base int64) int64 {
	if base <= 0 {
		return fallbackSVGPixels
	}
	return MemoryFraction(base, svgBudgetPercent) / 4
}

func resourceCacheBudget(base int64) int64 {
	if base <= 0 {
		return fallbackResourceCacheBytes
	}
	return MemoryFraction(base, resourceCachePercent)
}

// ByteLimiter is a context-aware weighted semaphore for decoded image bytes.
// A release wakes every waiter; there are only a handful of image workers.
type ByteLimiter struct {
	mu    sync.Mutex
	limit int64
	used  int64
	wake  chan struct{}
}

// NewByteLimiter creates a limiter with the given byte budget.
func NewByteLimiter(limit int64) *ByteLimiter {
	if limit < 1 {
		limit = 1
	}
	return &ByteLimiter{limit: limit, wake: make(chan struct{})}
}

// Limit reports the current byte budget.
func (l *ByteLimiter) Limit() int64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limit
}

// SetLimit changes the byte budget and wakes waiters.
func (l *ByteLimiter) SetLimit(limit int64) {
	if limit < 1 {
		limit = 1
	}
	l.mu.Lock()
	l.limit = limit
	close(l.wake)
	l.wake = make(chan struct{})
	l.mu.Unlock()
}

// Acquire reserves n bytes, waiting until they fit, and returns the amount
// actually reserved. A single item larger than the whole budget is admitted
// alone rather than blocking forever.
func (l *ByteLimiter) Acquire(ctx context.Context, n int64) (int64, bool) {
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

// Release returns n reserved bytes to the budget.
func (l *ByteLimiter) Release(n int64) {
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

// SetMemoryBudget derives the loader's image and inline-SVG budgets from the
// effective process limit. A zero base keeps the conservative fallbacks. The
// session path uses SetContentBudget and SetResourceCacheBudget so a shared
// BudgetManager can divide the pools across tabs.
func (l *Loader) SetMemoryBudget(base int64) {
	l.SetContentBudget(imageByteBudget(base), svgPixelBudget(base))
	l.SetResourceCacheBudget(resourceCacheBudget(base))
}

// SetContentBudget installs the loader's image-byte and inline-SVG pixel
// shares.
func (l *Loader) SetContentBudget(imageBytes, svgPixels int64) {
	if l == nil {
		return
	}
	if l.ImageBudget != nil {
		l.ImageBudget.SetLimit(imageBytes)
	}
	l.SVGMu.Lock()
	l.SVGBudgetPixels = svgPixels
	l.SVGMu.Unlock()
}

// SetResourceCacheBudget sets the host's shared resource-cache limit.
func (l *Loader) SetResourceCacheBudget(bytes int64) {
	if l == nil || l.Host == nil {
		return
	}
	l.Host.SetCacheLimitBytes(bytes)
}

// SetCacheLimitMB updates the session resource-cache budget in MiB.
func (l *Loader) SetCacheLimitMB(mb int) {
	if l == nil || l.Host == nil {
		return
	}
	if mb < 0 {
		mb = 0
	}
	l.Host.SetCacheLimitBytes(int64(mb) << 20)
}
