package engine

import (
	"sync"
	"sync/atomic"

	"browser/internal/protocol"
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

	storageProvider protocol.StorageProvider
	entryIDs        protocol.EntryIDSource
}

// NewBrowserContext creates the shared state for one browser process.
func NewBrowserContext() *BrowserContext {
	storage := NewStorageBroker()
	return &BrowserContext{
		storage:         storage,
		storageProvider: storageProvider{broker: storage},
		budgets:         NewBudgetManager(effectiveMemoryLimit()),
	}
}

// SetStorageProvider replaces the storage-area resolver. A process-backed
// engine uses this to forward localStorage and sessionStorage to the parent.
func (c *BrowserContext) SetStorageProvider(p protocol.StorageProvider) {
	if c == nil {
		return
	}
	c.storageProvider = p
}

// SetEntryIDSource replaces the history-entry ID allocator. A process-backed
// engine uses this when the parent owns the process-wide ID sequence.
func (c *BrowserContext) SetEntryIDSource(source protocol.EntryIDSource) {
	if c == nil {
		return
	}
	c.entryIDs = source
}

// SetEntryIDPrefix reserves a high-bit range for one tab's history entries, so
// separate engine processes cannot return the same ID to the shared host.
func (c *BrowserContext) SetEntryIDPrefix(prefix uint64) {
	if c == nil {
		return
	}
	c.nextEntryID.Store(prefix)
}

// View implements protocol.StorageProvider over the context's local broker.
// A process-backed engine replaces it with a remote provider, while the parent
// uses this method to serve child storage calls.
func (c *BrowserContext) View(kind protocol.StorageKind, tabID uint64, origin protocol.Origin) protocol.Storage {
	if c == nil {
		return nil
	}
	return c.Storage().View(kind, tabID, origin)
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

// SetTabIDPrefix reserves a high-bit range for one child tab's sessionStorage
// scope, so separate engine processes cannot collide in the parent's broker.
func (c *BrowserContext) SetTabIDPrefix(prefix uint64) {
	if c == nil {
		return
	}
	c.nextTabID.Store(prefix)
}

// NextEntryID returns a process-unique opaque history-entry ID.
func (c *BrowserContext) NextEntryID() uint64 {
	if c.entryIDs != nil {
		return c.entryIDs.NextEntryID()
	}
	return c.nextEntryID.Add(1)
}
