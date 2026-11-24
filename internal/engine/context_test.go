package engine

import (
	"sync"
	"testing"
)

func TestSharedBrowserContextAllocatesUniqueEntryIDs(t *testing.T) {
	ctx := NewBrowserContext()
	a := NewSessionWithBrowserContext(80, 24, ctx)
	b := NewSessionWithBrowserContext(80, 24, ctx)

	got := []uint64{a.NextHistoryID(), b.NextHistoryID(), a.NextHistoryID()}
	want := []uint64{1, 2, 3}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry IDs = %v, want %v", got, want)
		}
	}
}

func TestSessionsWithoutSharedContextDoNotCollide(t *testing.T) {
	a := NewSession(80, 24)
	b := NewSession(80, 24)
	if a.NextHistoryID() != 1 || b.NextHistoryID() != 1 {
		t.Fatal("sessions with private contexts should start at 1")
	}
}

func TestZeroValueSessionAllocatesLazily(t *testing.T) {
	var s Session
	if got := s.NextHistoryID(); got != 1 {
		t.Fatalf("first entry ID = %d, want 1", got)
	}
	if got := s.NextHistoryID(); got != 2 {
		t.Fatalf("second entry ID = %d, want 2", got)
	}
}

func TestConfiguredSessionUsesSharedBrowserContext(t *testing.T) {
	ctx := NewBrowserContext()
	a := NewConfiguredSession(SessionConfig{BrowserContext: ctx})
	b := NewConfiguredSession(SessionConfig{BrowserContext: ctx})
	if a.NextHistoryID() != 1 || b.NextHistoryID() != 2 {
		t.Fatal("configured sessions did not share the browser context")
	}
}

func TestBrowserContextConcurrentAllocation(t *testing.T) {
	ctx := NewBrowserContext()
	const n = 128
	ids := make(chan uint64, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids <- ctx.NextEntryID()
		}()
	}
	wg.Wait()
	close(ids)

	seen := make(map[uint64]bool, n)
	for id := range ids {
		if id == 0 || id > n {
			t.Fatalf("entry ID %d out of range 1..%d", id, n)
		}
		if seen[id] {
			t.Fatalf("duplicate entry ID %d", id)
		}
		seen[id] = true
	}
	if len(seen) != n {
		t.Fatalf("unique entry IDs = %d, want %d", len(seen), n)
	}
}
