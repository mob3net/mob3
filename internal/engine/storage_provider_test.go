package engine

import (
	"testing"

	"browser/internal/protocol"
)

type testStorage struct {
	data map[string]string
}

func (s *testStorage) Get(key string) (string, bool) {
	v, ok := s.data[key]
	return v, ok
}
func (s *testStorage) Set(key, value string) error {
	s.data[key] = value
	return nil
}
func (s *testStorage) Delete(key string) { delete(s.data, key) }
func (s *testStorage) Clear()            { s.data = make(map[string]string) }
func (s *testStorage) Keys() []string {
	keys := make([]string, 0, len(s.data))
	for key := range s.data {
		keys = append(keys, key)
	}
	return keys
}
func (s *testStorage) Len() int                    { return len(s.data) }
func (s *testStorage) Snapshot() map[string]string { return s.data }

type testStorageProvider struct {
	view  protocol.Storage
	kind  protocol.StorageKind
	tabID uint64
	used  bool
}

func (p *testStorageProvider) View(kind protocol.StorageKind, tabID uint64, origin protocol.Origin) protocol.Storage {
	p.used = true
	p.kind = kind
	p.tabID = tabID
	return p.view
}

type testEntryIDs struct{ next uint64 }

func (s *testEntryIDs) NextEntryID() uint64 {
	s.next++
	return 100 + s.next
}

func TestBrowserContextStorageProvider(t *testing.T) {
	view := &testStorage{data: make(map[string]string)}
	provider := &testStorageProvider{view: view}
	bc := NewBrowserContext()
	bc.SetStorageProvider(provider)
	s := NewSessionWithBrowserContext(80, 24, bc)
	got := s.storageView(StorageSession, s.Frame)
	if !provider.used || provider.kind != protocol.StorageSession || provider.tabID != s.tabID {
		t.Fatalf("provider usage = used=%v kind=%v tab=%d", provider.used, provider.kind, provider.tabID)
	}
	if err := got.Set("key", "value"); err != nil {
		t.Fatal(err)
	}
	if view.data["key"] != "value" {
		t.Fatalf("storage write = %+v", view.data)
	}
}

func TestSessionApplyBudgetShare(t *testing.T) {
	b := NewSession(80, 24)
	share := protocol.BudgetShare{JSHeap: 7 << 20, HistoryCache: 3 << 20, ImageBytes: 2 << 20, SVGPixels: 1000, ResourceCache: 1 << 20}
	b.ApplyBudgetShare(share)
	if b.JSMemLimit != share.JSHeap || b.budgetShare != share {
		t.Fatalf("budget share = %+v js=%d", b.budgetShare, b.JSMemLimit)
	}
}

func TestBrowserContextTabIDPrefix(t *testing.T) {
	bc := NewBrowserContext()
	bc.SetTabIDPrefix(10 << 32)
	if got := bc.NextTabID(); got != (10<<32)+1 {
		t.Fatalf("first tab ID = %d", got)
	}
	if got := bc.NextTabID(); got != (10<<32)+2 {
		t.Fatalf("second tab ID = %d", got)
	}
}

func TestBrowserContextEntryIDSource(t *testing.T) {
	ids := &testEntryIDs{}
	bc := NewBrowserContext()
	bc.SetEntryIDSource(ids)
	s := NewSessionWithBrowserContext(80, 24, bc)
	if got := s.NextHistoryID(); got != 101 {
		t.Fatalf("first entry ID = %d", got)
	}
	if got := s.NextHistoryID(); got != 102 {
		t.Fatalf("second entry ID = %d", got)
	}
}
