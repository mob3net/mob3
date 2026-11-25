package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Automation drives one headless Session for scripted clients. Commands are
// serialized; the browser never writes terminal output.
type Automation struct {
	mu sync.Mutex
	b  *Session

	// History is the headless client's own list. The engine owns only the page
	// cache, keyed by these entry IDs.
	history      []automationHistoryEntry
	historyIndex int
}

type automationHistoryEntry struct {
	id     uint64
	url    string
	scroll int
}

// ClickTarget selects what to click. Exactly one of Link, Text, Selector or
// HasCoords should be set. Coordinates are zero-based cells in the page
// viewport (row 0 is the first page row below the chrome).
type ClickTarget struct {
	Link      int
	Text      string
	Selector  string
	X, Y      int
	HasCoords bool
}

// newAutomation creates a headless browser with a virtual text viewport.
func newAutomation(width, height int, bc *BrowserContext) *Automation {
	b := NewSessionWithBrowserContext(width, height, bc)
	b.NoDraw = true
	if b.loader != nil {
		// Automation exposes text; image downloads would only slow it down.
		b.loader.NoImages = true
	}
	a := &Automation{b: b}
	b.historyObserver = a.applyHistoryCommit
	return a
}

// Close stops pending work and releases the page runtime.
func (a *Automation) Close() {
	if a == nil || a.b == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.b.cancelLoadNow()
	a.b.stopJS()
}

// Navigate loads a URL or resolves typed input exactly like the URL prompt,
// then waits for the page, meta refresh and scripted redirects.
func (a *Automation) Navigate(ctx context.Context, target string) error {
	if a == nil || a.b == nil {
		return errors.New("automation is closed")
	}
	if strings.TrimSpace(target) == "" {
		return errors.New("empty URL")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.recordHistoryScrollLocked()
	before := a.b.LoadSeq
	a.b.Navigate(target, true)
	if err := a.waitLoad(ctx); err != nil {
		return err
	}
	if a.b.Page == nil {
		return fmt.Errorf("navigation failed: %s", nonEmptyStatus(a.b.status))
	}
	if a.b.LoadSeq == before {
		switch {
		case strings.HasPrefix(a.b.status, "Invalid URL:"),
			strings.HasPrefix(a.b.status, "Blocked "),
			strings.HasPrefix(a.b.status, "Nothing to retry"):
			return errors.New(a.b.status)
		}
	}
	return nil
}

// waitLoad applies asynchronous load results. finishLoad can queue another
// navigation (meta refresh or location changes), so keep waiting while the
// browser reports a load.
func (a *Automation) waitLoad(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for a.b.Loading {
		select {
		case res := <-a.b.LoadCh:
			a.b.finishLoad(res)
		case <-ctx.Done():
			a.b.cancelLoadNow()
			return ctx.Err()
		}
	}
	return nil
}
