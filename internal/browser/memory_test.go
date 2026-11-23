package browser

import (
	"context"
	"runtime/debug"
	"strings"
	"testing"
	"time"
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
	if got := debug.SetMemoryLimit(-1); got != startupMemoryLimit {
		t.Fatalf("runtime limit after clear = %d, want startup %d", got, startupMemoryLimit)
	}
}

func TestMemoryBudgetsScaleWithLimit(t *testing.T) {
	const gb = int64(1) << 30
	if got := jsHeapBudget(0); got != fallbackJSHeap {
		t.Fatalf("fallback JS budget = %d, want %d", got, fallbackJSHeap)
	}
	if got := jsHeapBudget(gb); got != 256<<20 {
		t.Fatalf("JS budget at 1 GB = %d, want 256 MB", got)
	}
	if got := imageByteBudget(gb); got <= 0 || got > gb {
		t.Fatalf("image budget at 1 GB = %d", got)
	}
	if got := svgPixelBudget(gb); got <= 0 || got*4 > gb {
		t.Fatalf("SVG pixel budget at 1 GB = %d", got)
	}
	if got := imageByteBudget(0); got != fallbackImageBytes {
		t.Fatalf("fallback image budget = %d", got)
	}
}

func TestByteLimiter(t *testing.T) {
	l := newByteLimiter(100)
	if _, ok := l.acquire(context.Background(), 60); !ok {
		t.Fatal("first acquire failed")
	}
	done := make(chan bool, 1)
	go func() {
		_, ok := l.acquire(context.Background(), 60)
		done <- ok
	}()
	select {
	case <-done:
		t.Fatal("second acquire did not wait")
	case <-time.After(20 * time.Millisecond):
	}
	l.release(60)
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("second acquire was cancelled")
		}
	case <-time.After(time.Second):
		t.Fatal("second acquire did not resume after release")
	}
	l.release(60)

	// Cancellation while waiting must fail without charging the budget.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, ok := l.acquire(ctx, 100); !ok {
		t.Fatal("full acquire failed")
	}
	if _, ok := l.acquire(ctx, 100); ok {
		t.Fatal("cancelled acquire succeeded")
	}
	l.release(100)
}

func TestByteLimiterClampsLargeReservation(t *testing.T) {
	l := newByteLimiter(100)
	admitted, ok := l.acquire(context.Background(), 500)
	if !ok || admitted != 100 {
		t.Fatalf("large reservation = %d, %v; want 100, true", admitted, ok)
	}
	l.release(admitted)
}

func TestInlineSVGBudget(t *testing.T) {
	l := NewLoader()
	l.noImages = false
	l.svgBudgetPixels = 10000
	root := ParseHTML(strings.Repeat(
		`<svg viewBox="0 0 100 100"><rect width="100" height="100"/></svg>`, 3))
	l.loadInlineSVGs(root)
	total := 0
	for _, n := range root.FindAll(func(n *Node) bool { return n.Tag == "svg" }) {
		if n.Img != nil {
			total += n.Img.Bounds().Dx() * n.Img.Bounds().Dy()
		}
	}
	if total > int(l.svgBudgetPixels) {
		t.Fatalf("inline SVG rasterized %d pixels, budget %d", total, l.svgBudgetPixels)
	}
	if total == 0 {
		t.Fatal("no inline SVG was rasterized")
	}
}

func TestDynamicStyleRuleCap(t *testing.T) {
	doc := ParseHTML(`<html><body><style>.a{color:red}</style></body></html>`)
	p := &Page{Root: doc}
	p.SeedParsedStyles()
	style := doc.FindFirst("style")
	style.Children[0].Text = `.a{color:red}.b{color:blue}`
	p.Author = make([]Rule, maxAuthorRules-1)
	if !p.CollectDynamicStyles() {
		t.Fatal("dynamic style was not collected")
	}
	if len(p.Author) > maxAuthorRules {
		t.Fatalf("author rules = %d, cap %d", len(p.Author), maxAuthorRules)
	}
}

func TestJSMemorySamplerLifecycle(t *testing.T) {
	b := NewBrowser(40, 10)
	b.JSMemLimit = 1 << 20
	j := newJSRuntime(b, newJSPage(`<html><body></body></html>`, ""))
	if !j.MemSamplerActive() {
		t.Fatal("memory sampler did not start")
	}
	j.SetMemLimit(0)
	if j.MemSamplerActive() {
		t.Fatal("memory sampler did not stop")
	}
	j.SetMemLimit(1 << 20)
	if !j.MemSamplerActive() {
		t.Fatal("memory sampler did not restart")
	}
	j.Stop()
	if j.MemSamplerActive() {
		t.Fatal("memory sampler survived runtime stop")
	}
}

func TestJSMemorySamplerInterrupts(t *testing.T) {
	b := NewBrowser(40, 10)
	b.JSMemLimit = 1 << 20
	j := newJSRuntime(b, newJSPage(`<html><body></body></html>`, ""))
	defer j.Stop()
	j.SetMemBase(0)
	j.SetMemLimit(1)
	j.SetTimeout(10 * time.Second)
	start := time.Now()
	_, err := j.RunCode("loop", "while (true) {}")
	if err == nil || !strings.Contains(err.Error(), "memory limit") {
		t.Fatalf("memory watchdog error = %v, want a memory limit", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("memory watchdog took %v", elapsed)
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
