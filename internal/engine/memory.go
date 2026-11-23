package engine

import (
	"runtime/debug"
)

// startupMemoryLimit is the Go runtime soft limit at process start. It reflects
// GOMEMLIMIT when one was supplied, so setting -mem-limit back to 0 can restore
// it instead of overwriting it with MaxInt64.
var startupMemoryLimit = debug.SetMemoryLimit(-1)

// Internal budgets when the user did not set a limit. They match the values
// used before budgets became percentages, and are replaced by the percentages
// below whenever -mem-limit or GOMEMLIMIT gives a base.
const (
	fallbackJSHeap            = int64(256) << 20
	fallbackHistoryCacheBytes = int64(128) << 20
	jsHeapPercent             = 25
	historyCachePercent       = 12
)

// effectiveMemoryLimit reports the user's soft heap limit, or zero when the
// process is unlimited.
func effectiveMemoryLimit() int64 { return EffectiveMemoryLimit() }

// jsHeapBudget returns the per-page JS ceiling for base. Zero keeps the
// conservative fallback, used when the process itself is unlimited.
func jsHeapBudget(base int64) int64 {
	if base <= 0 {
		return fallbackJSHeap
	}
	return MemoryFraction(base, jsHeapPercent)
}

func historyCacheBudget(base int64) int64 {
	if base <= 0 {
		return fallbackHistoryCacheBytes
	}
	return MemoryFraction(base, historyCachePercent)
}

// setMemoryLimitMB applies a process soft heap limit. Zero restores the
// startup limit (including an environment GOMEMLIMIT) and keeps the process
// unlimited when the user never configured one.
func (b *Session) setMemoryLimitMB(mb int) {
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
		b.loader.SetMemoryBudget(base)
	}
}

// takeScavengePending consumes one pending scavenge request.
func (b *Session) takeScavengePending() bool { return b.TakeScavengePending() }
