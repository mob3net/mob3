package engine

import "testing"

type budgetRecorder struct {
	share budgetShare
}

func (r *budgetRecorder) applyBudgetShare(s budgetShare) { r.share = s }

func TestBudgetManagerSplitsAcrossConsumers(t *testing.T) {
	const base = int64(1) << 30
	m := NewBudgetManager(base)
	a, b := &budgetRecorder{}, &budgetRecorder{}
	m.Register(a)
	m.Register(b)

	if a.share != b.share {
		t.Fatalf("shares differ: %+v %+v", a.share, b.share)
	}
	if got, want := a.share.JSHeap, perSessionShare(jsHeapBudget(base), 2); got != want {
		t.Fatalf("js share = %d, want %d", got, want)
	}
	if got, want := a.share.HistoryCache, perSessionShare(historyCacheBudget(base), 2); got != want {
		t.Fatalf("history share = %d, want %d", got, want)
	}
	if got, want := a.share.ImageBytes, perSessionShare(imageByteBudget(base), 2); got != want {
		t.Fatalf("image share = %d, want %d", got, want)
	}
	if got, want := a.share.SVGPixels, perSessionShare(svgPixelBudget(base), 2); got != want {
		t.Fatalf("svg share = %d, want %d", got, want)
	}
	if got, want := a.share.ResourceCache, resourceCacheBudget(base); got != want {
		t.Fatalf("resource cache = %d, want %d", got, want)
	}

	m.Unregister(b)
	if got, want := a.share.JSHeap, jsHeapBudget(base); got != want {
		t.Fatalf("js share after unregister = %d, want %d", got, want)
	}
	if got, want := a.share.HistoryCache, historyCacheBudget(base); got != want {
		t.Fatalf("history share after unregister = %d, want %d", got, want)
	}
}

func TestBudgetManagerSetBaseReapplies(t *testing.T) {
	m := NewBudgetManager(0)
	a := &budgetRecorder{}
	m.Register(a)
	if got, want := a.share.JSHeap, fallbackJSHeap; got != want {
		t.Fatalf("fallback js share = %d, want %d", got, want)
	}
	m.SetBase(1 << 30)
	if got, want := a.share.JSHeap, jsHeapBudget(1<<30); got != want {
		t.Fatalf("js share after set base = %d, want %d", got, want)
	}
	m.SetBase(0)
	if got, want := a.share.JSHeap, fallbackJSHeap; got != want {
		t.Fatalf("js share after clearing base = %d, want %d", got, want)
	}
}

func TestSessionBudgetsDivideAcrossTabs(t *testing.T) {
	ctx := NewBrowserContext()
	base := effectiveMemoryLimit()
	a := NewSessionWithBrowserContext(80, 24, ctx)
	b := NewSessionWithBrowserContext(80, 24, ctx)

	wantJS := perSessionShare(jsHeapBudget(base), 2)
	wantCache := perSessionShare(historyCacheBudget(base), 2)
	wantImage := perSessionShare(imageByteBudget(base), 2)
	wantSVG := perSessionShare(svgPixelBudget(base), 2)
	for name, s := range map[string]*Session{"a": a, "b": b} {
		if s.JSMemLimit != wantJS {
			t.Errorf("%s JS = %d, want %d", name, s.JSMemLimit, wantJS)
		}
		if s.Cache.LimitBytes() != wantCache {
			t.Errorf("%s cache = %d, want %d", name, s.Cache.LimitBytes(), wantCache)
		}
		if s.loader.ImageBudget.Limit() != wantImage {
			t.Errorf("%s image = %d, want %d", name, s.loader.ImageBudget.Limit(), wantImage)
		}
		if s.loader.SVGBudgetPixels != wantSVG {
			t.Errorf("%s svg = %d, want %d", name, s.loader.SVGBudgetPixels, wantSVG)
		}
	}
}
