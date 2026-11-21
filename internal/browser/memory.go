package browser

import (
	"math"
	"runtime/debug"
)

// setMemoryLimitMB applies a process soft heap limit. Zero removes the limit;
// a GOMEMLIMIT environment variable from startup is replaced when a limit is
// changed at runtime.
func (b *Browser) setMemoryLimitMB(mb int) {
	if mb < 0 {
		mb = 0
	}
	if mb == 0 {
		b.memLimitBytes = 0
		debug.SetMemoryLimit(math.MaxInt64)
		return
	}
	b.memLimitBytes = int64(mb) << 20
	debug.SetMemoryLimit(b.memLimitBytes)
}

// takeScavengePending consumes one pending scavenge request.
func (b *Browser) takeScavengePending() bool {
	return b.scavengePending.Swap(false)
}
