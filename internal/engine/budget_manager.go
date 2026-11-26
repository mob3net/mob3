package engine

import (
	"sync"

	"browser/internal/protocol"
)

// budgetShare is the part of one process budget a session may use. The JS,
// history, image and SVG pools are divided across active sessions; the
// resource cache is shared by the host and is not divided.
type budgetShare = protocol.BudgetShare

type budgetConsumer interface {
	applyBudgetShare(budgetShare)
}

// BudgetManager divides the process-wide JS, page-cache, image and inline-SVG
// pools across the sessions that share a browser context. One session gets the
// same budgets as the old per-session percentages; adding a second session
// halves each pool instead of doubling the total.
type BudgetManager struct {
	mu        sync.Mutex
	base      int64
	consumers map[budgetConsumer]struct{}
}

// NewBudgetManager creates a manager for one browser context. base is the
// effective process memory limit; zero keeps the fallback budgets.
func NewBudgetManager(base int64) *BudgetManager {
	if base < 0 {
		base = 0
	}
	return &BudgetManager{base: base, consumers: make(map[budgetConsumer]struct{})}
}

// Register adds a session and applies the new shares to every session.
func (m *BudgetManager) Register(c budgetConsumer) budgetShare {
	if m == nil {
		return budgetShare{}
	}
	m.mu.Lock()
	if c != nil {
		m.consumers[c] = struct{}{}
	}
	share, consumers := m.shareLocked(), m.snapshotLocked()
	m.mu.Unlock()
	for _, consumer := range consumers {
		consumer.applyBudgetShare(share)
	}
	return share
}

// Unregister removes a session and re-applies the larger shares to the rest.
func (m *BudgetManager) Unregister(c budgetConsumer) {
	if m == nil || c == nil {
		return
	}
	m.mu.Lock()
	delete(m.consumers, c)
	share, consumers := m.shareLocked(), m.snapshotLocked()
	m.mu.Unlock()
	for _, consumer := range consumers {
		consumer.applyBudgetShare(share)
	}
}

// SetBase changes the process limit and re-applies shares to every session.
func (m *BudgetManager) SetBase(base int64) {
	if m == nil {
		return
	}
	if base < 0 {
		base = 0
	}
	m.mu.Lock()
	m.base = base
	share, consumers := m.shareLocked(), m.snapshotLocked()
	m.mu.Unlock()
	for _, consumer := range consumers {
		consumer.applyBudgetShare(share)
	}
}

func (m *BudgetManager) shareLocked() budgetShare {
	n := len(m.consumers)
	if n < 1 {
		n = 1
	}
	return budgetShare{
		JSHeap:        perSessionShare(jsHeapBudget(m.base), n),
		HistoryCache:  perSessionShare(historyCacheBudget(m.base), n),
		ImageBytes:    perSessionShare(imageByteBudget(m.base), n),
		SVGPixels:     perSessionShare(svgPixelBudget(m.base), n),
		ResourceCache: resourceCacheBudget(m.base),
	}
}

func (m *BudgetManager) snapshotLocked() []budgetConsumer {
	out := make([]budgetConsumer, 0, len(m.consumers))
	for c := range m.consumers {
		out = append(out, c)
	}
	return out
}

func perSessionShare(total int64, sessions int) int64 {
	if sessions < 1 {
		sessions = 1
	}
	share := total / int64(sessions)
	if share < 1 {
		share = 1
	}
	return share
}

// applyBudgetShare installs the session's share on its JS runtime, page cache
// and loader. It is called by BudgetManager and again when a configured loader
// replaces the default one.
func (b *Session) applyBudgetShare(s budgetShare) {
	if b == nil {
		return
	}
	b.budgetShare = s
	b.JSMemLimit = s.JSHeap
	if b.JS != nil {
		b.JS.SetMemLimit(s.JSHeap)
	}
	if b.Cache != nil {
		b.Cache.SetLimitBytes(s.HistoryCache)
	}
	if b.loader != nil {
		b.loader.SetContentBudget(s.ImageBytes, s.SVGPixels)
		b.loader.SetResourceCacheBudget(s.ResourceCache)
	}
}
