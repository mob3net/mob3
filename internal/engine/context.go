package engine

import (
	"sync"
	"sync/atomic"
)

// BrowserContext is the state shared by every tab session in one browser
// process. It currently allocates the opaque history-entry IDs that the host
// uses for URL attestation and the engine uses for its page cache; shared
// storage, memory budgets and settings join it with the tab and frame model.
//
// A context is safe for concurrent use. Sessions that share one never
// allocate the same entry ID, so the host's per-entry URL record cannot
// collide across tabs.
type BrowserContext struct {
	nextEntryID atomic.Uint64
	nextTabID   atomic.Uint64
	storageOnce sync.Once
	storage     *StorageBroker
	budgetsOnce sync.Once
	budgets     *BudgetManager
}

// NewBrowserContext creates the shared state for one browser process.
func NewBrowserContext() *BrowserContext {
	return &BrowserContext{
		storage: NewStorageBroker(),
		budgets: NewBudgetManager(effectiveMemoryLimit()),
	}
}

// Storage returns the context's shared storage pools.
func (c *BrowserContext) Storage() *StorageBroker {
	if c == nil {
		return nil
	}
	c.storageOnce.Do(func() {
		if c.storage == nil {
			c.storage = NewStorageBroker()
		}
	})
	return c.storage
}

// Budgets returns the context's shared memory-budget manager.
func (c *BrowserContext) Budgets() *BudgetManager {
	if c == nil {
		return nil
	}
	c.budgetsOnce.Do(func() {
		if c.budgets == nil {
			c.budgets = NewBudgetManager(effectiveMemoryLimit())
		}
	})
	return c.budgets
}

// NextTabID returns a process-unique tab ID for sessionStorage scoping.
func (c *BrowserContext) NextTabID() uint64 {
	return c.nextTabID.Add(1)
}

// NextEntryID returns a process-unique opaque history-entry ID.
func (c *BrowserContext) NextEntryID() uint64 {
	return c.nextEntryID.Add(1)
}
