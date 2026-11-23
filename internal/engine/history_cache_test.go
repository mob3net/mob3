package engine

import "testing"

func TestHistoryCacheLifecycle(t *testing.T) {
	s := NewSession()
	s.Cache = NewPageCache(1 << 20)
	u := mustParseURL(t, "https://example.com/a")
	s.Page = &Page{URL: u, Root: &Node{Type: DocumentNode}}
	s.URL = u
	s.EntryID = 7
	s.LastMethod = "GET"
	if !s.CacheCurrentPage() {
		t.Fatal("page was not cached")
	}
	if got := s.Cache.Get(7); got == nil || got.Page != s.Page {
		t.Fatalf("cached entry = %+v", got)
	}
	s.DropCachedEntryUnless(7, "https://example.com/other")
	if s.Cache.Get(7) != nil {
		t.Fatal("changed URL kept the cached entry")
	}
	s.NoteEvictedPage(2<<20, 4<<20)
	if !s.TakeScavengePending() {
		t.Fatal("large eviction did not request scavenge")
	}
	if s.TakeScavengePending() {
		t.Fatal("scavenge request survived its use")
	}
	s.CacheCurrentPage()
	s.DropCachedPages()
	if s.Cache.Get(7) != nil {
		t.Fatal("drop all kept the cached entry")
	}
}
