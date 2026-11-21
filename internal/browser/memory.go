package browser

import (
	"math"
	"runtime/debug"
)

// defaultMemoryLimitBytes is the soft heap limit applied to interactive and
// automation runs unless -mem-limit changes it. It is deliberately several
// times the working set of a heavy page plus the history cache: the runtime
// treats the value as advice, collects harder as the heap approaches it, and
// never fails an allocation because of it.
const defaultMemoryLimitBytes = 512 << 20

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
