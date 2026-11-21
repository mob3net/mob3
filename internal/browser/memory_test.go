package browser

import (
	"math"
	"runtime/debug"
	"testing"
)

func TestMemoryLimitAppliedAndCleared(t *testing.T) {
	prev := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
	b := NewBrowser(60, 10)
	b.setMemoryLimitMB(96)
	if got := debug.SetMemoryLimit(-1); got != 96<<20 {
		t.Fatalf("runtime limit = %d, want %d", got, int64(96)<<20)
	}
	if b.memLimitBytes != int64(96)<<20 {
		t.Fatalf("recorded limit = %d", b.memLimitBytes)
	}
	b.setMemoryLimitMB(0)
	if b.memLimitBytes != 0 {
		t.Fatalf("recorded limit after clear = %d", b.memLimitBytes)
	}
	if got := debug.SetMemoryLimit(-1); got != math.MaxInt64 {
		t.Fatalf("runtime limit after clear = %d, want unlimited", got)
	}
}

func TestTakeScavengePendingConsumesOnce(t *testing.T) {
	b := NewBrowser(60, 10)
	b.scavengePending.Store(true)
	if !b.takeScavengePending() {
		t.Fatal("pending scavenge was not reported")
	}
	if b.takeScavengePending() {
		t.Fatal("scavenge request survived its use")
	}
}
