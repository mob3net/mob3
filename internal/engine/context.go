package engine

import "sync/atomic"

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
}

// NewBrowserContext creates the shared state for one browser process.
func NewBrowserContext() *BrowserContext {
	return &BrowserContext{}
}

// NextEntryID returns a process-unique opaque history-entry ID.
func (c *BrowserContext) NextEntryID() uint64 {
	return c.nextEntryID.Add(1)
}
