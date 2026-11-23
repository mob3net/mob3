package engine

import (
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func TestMemoryLimitAppliedAndCleared(t *testing.T) {
	prev := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
	b := newTestSession(60, 10)
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

func TestInlineSVGBudget(t *testing.T) {
	l := newTestLoader()
	l.NoImages = false
	l.SVGBudgetPixels = 10000
	root := ParseHTML(strings.Repeat(
		`<svg viewBox="0 0 100 100"><rect width="100" height="100"/></svg>`, 3))
	l.LoadInlineSVGs(root)
	total := 0
	for _, n := range root.FindAll(func(n *Node) bool { return n.Tag == "svg" }) {
		if n.Img != nil {
			total += n.Img.Bounds().Dx() * n.Img.Bounds().Dy()
		}
	}
	if total > int(l.SVGBudgetPixels) {
		t.Fatalf("inline SVG rasterized %d pixels, budget %d", total, l.SVGBudgetPixels)
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
	b := newTestSession(40, 10)
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
	b := newTestSession(40, 10)
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
	b := newTestSession(60, 10)
	b.NoteEvictedPage(1, 0)
	if !b.takeScavengePending() {
		t.Fatal("pending scavenge was not reported")
	}
	if b.takeScavengePending() {
		t.Fatal("scavenge request survived its use")
	}
}
