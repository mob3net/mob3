package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"browser/internal/protocol"
)

// Back navigates backward in session history.
func (a *Automation) Back(ctx context.Context) error {
	return a.historyMove(ctx, -1)
}

// Forward navigates forward in session history.
func (a *Automation) Forward(ctx context.Context) error {
	return a.historyMove(ctx, 1)
}

// Reload reloads the current page.
func (a *Automation) Reload(ctx context.Context) error {
	return a.historyAction(ctx, func(b *Session) { b.reload() })
}

func (a *Automation) historyMove(ctx context.Context, delta int) error {
	if a == nil || a.b == nil {
		return errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	next := a.historyIndex + delta
	if next < 0 || next >= len(a.history) {
		return nil
	}
	a.recordHistoryScrollLocked()
	a.historyIndex = next
	e := a.history[next]
	a.b.restoreEntry(e.id, e.url, e.scroll)
	return a.waitLoad(ctx)
}

func (a *Automation) recordHistoryScrollLocked() {
	if a.historyIndex < 0 || a.historyIndex >= len(a.history) {
		return
	}
	a.history[a.historyIndex].scroll = a.b.Scroll
}

// applyHistoryCommit updates the headless history list from the
func (a *Automation) applyHistoryCommit(v protocol.HistoryCommit) {
	if v.URL == "" || v.EntryID == 0 {
		return
	}
	e := automationHistoryEntry{id: v.EntryID, url: v.URL, scroll: v.Scroll}
	if v.Push {
		if a.historyIndex >= 0 && a.historyIndex < len(a.history) &&
			a.history[a.historyIndex].url == v.URL {
			a.history[a.historyIndex] = e
			return
		}
		if a.historyIndex+1 < len(a.history) {
			a.history = a.history[:a.historyIndex+1]
		}
		a.history = append(a.history, e)
		a.historyIndex = len(a.history) - 1
		return
	}
	if len(a.history) == 0 {
		a.history = append(a.history, e)
		a.historyIndex = 0
		return
	}
	if a.historyIndex < 0 || a.historyIndex >= len(a.history) {
		a.historyIndex = len(a.history) - 1
	}
	a.history[a.historyIndex] = e
}

func (a *Automation) historyAction(ctx context.Context, action func(*Session)) error {
	if a == nil || a.b == nil {
		return errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	action(a.b)
	return a.waitLoad(ctx)
}

// History lists session history, most recent first, with the current entry
// marked by a leading "*".
func (a *Automation) History() string {
	if a == nil || a.b == nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var sb strings.Builder
	for i := len(a.history) - 1; i >= 0; i-- {
		mark := "  "
		if i == a.historyIndex {
			mark = "* "
		}
		fmt.Fprintf(&sb, "%s%s\n", mark, a.history[i].url)
	}
	return strings.TrimRight(sb.String(), "\n")
}
