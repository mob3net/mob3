package engine

import "time"

// HistoryCachePageCap is the largest page the back/forward cache keeps.
func (s *Session) HistoryCachePageCap() int64 {
	if s.Cache == nil {
		return 0
	}
	return s.Cache.PageCap()
}

// SetHistoryCacheLimitMB applies a new page-cache budget.
func (s *Session) SetHistoryCacheLimitMB(mb int) {
	if s.Cache == nil {
		return
	}
	if mb < 0 {
		mb = 0
	}
	s.Cache.SetLimitBytes(int64(mb) << 20)
}

// NoteEvictedPage asks the event loop for one scavenge after a large eviction.
func (s *Session) NoteEvictedPage(size, limit int64) {
	if size <= 0 {
		return
	}
	if limit > 0 && size < limit/4 {
		return
	}
	s.scavengePending.Store(true)
}

// TakeScavengePending consumes the one-shot scavenge request.
func (s *Session) TakeScavengePending() bool { return s.scavengePending.Swap(false) }

// SuspendCurrentPage caches the applied page for back/forward and detaches its
// JavaScript runtime without destroying it.
func (s *Session) SuspendCurrentPage() {
	cached := s.CacheCurrentPage()
	if s.JS != nil {
		if cached {
			s.JS.Freeze()
		} else {
			s.JS.Stop()
		}
		s.JS = nil
	}
}

// CacheCurrentPage stores the applied page under the current history entry ID.
func (s *Session) CacheCurrentPage() bool {
	if s.Cache == nil || s.Page == nil || s.Page.Root == nil || s.URL == nil || s.Page.Download || s.EntryID == 0 {
		return false
	}
	switch s.URL.Scheme {
	case "", "about", "data", "javascript", "mailto", "tel":
		return false
	}
	weight := PageWeight(s.Page, s.JS, s.LastBody, s.ReaderRoot)
	if weight > s.Cache.PageCap() {
		return false
	}
	s.Cache.Put(s.EntryID, &CachedPage{
		Page: s.Page, JS: s.JS, ReaderRoot: s.ReaderRoot,
		ReaderSavedScroll: s.ReaderSavedScroll, Method: s.LastMethod,
		Body: s.LastBody, ContentType: s.LastContentType, At: time.Now(), Bytes: weight,
	})
	return true
}

// DropCachedEntryUnless removes an entry's cached page when its URL differs.
func (s *Session) DropCachedEntryUnless(id uint64, rawURL string) {
	if s.Cache != nil {
		s.Cache.DeleteUnlessURL(id, rawURL)
	}
}

// DropCachedPages clears the back/forward page cache.
func (s *Session) DropCachedPages() {
	if s.Cache != nil {
		s.Cache.DropAll()
	}
}
