package engine

import (
	"net/url"
	"time"

	"browser/internal/protocol"
)

// MaxAutoNavigations caps consecutive scripted or meta-refresh redirects.
const MaxAutoNavigations = 20

// SessionHooks are the trusted-shell effects around applying a page or a
// cached history entry. The browser implements them during the migration.
type SessionHooks interface {
	BeginPage()
	ResetUI()
	ResetConsole()
	StartJS(*Page)
	ApplyPendingJS(duringLoad bool, entryID uint64, historyPush bool) bool
	NavigateEntry(method, target string, body []byte, contentType string, entryID uint64, historyPush, force bool)
	Rebuild()
	InvalidateFrame()
	SetStatus(string)
	PageLoadStatus(*Page, bool) string
	ViewportHeight() int
	TakePendingStatus() string
	ShowDownload(url, name string)
	EmitHistoryCommit(protocol.HistoryCommit)
	Viewport() (top, bottom int)
	HandleKey(string) bool
	Navigate(input string, push bool)
	NavigateRequest(method, target string, body []byte, contentType string, push bool)
	ActivateHintControl(*Node, Rect)
	OpenSelectMenu(*Node)
	ConsoleOpen() bool
	HoverRebuild() (pending bool, at time.Time)
	ClearHoverRebuild()
	NewJSHost() JSHost
	SetModuleErr(bool)
}

// ApplyLoadResult applies a completed navigation result. Stale results are
// ignored by the caller's sequence check.
func (s *Session) ApplyLoadResult(res LoadResult, h SessionHooks) {
	if res.Seq != s.LoadSeq {
		return
	}
	note := h.TakePendingStatus()
	if s.ClearHints() {
		h.SetStatus("")
	}
	s.HintConfirm = false
	s.HintConfirmLink = -1
	s.Forms.ClearInputFocus()
	s.Forms.ClearControl()
	h.ResetUI()
	s.CancelLoad = nil
	s.Loading = false
	if res.Err != nil {
		s.AutoNavigations = 0
		s.LoopStopped = false
		h.SetStatus("Error: " + res.Err.Error())
		s.FinishProgress()
		if s.Page == nil {
			doc := LoadErrorPageHTML(res.Err.Error())
			root := ParseHTML(doc)
			page := &Page{URL: res.URL, Base: res.URL, Root: root, RawHTML: doc, Title: "Error", Status: 0}
			ApplyPageStyles(root, page)
			s.Page = page
			s.URL = res.URL
			h.Rebuild()
		}
		return
	}
	page := res.Page
	if page != nil && page.Download && res.Method == protocol.MethodGet && page.URL != nil {
		name := page.DownloadName
		if res.DownloadName != "" {
			name = res.DownloadName
		}
		h.ShowDownload(page.URL.String(), name)
		h.SetStatus("Download link — y copies the curl command")
		s.FinishProgress()
		h.InvalidateFrame()
		return
	}
	s.Page = page
	s.URL = page.URL
	s.ImagesPaused = false
	if s.URL != nil && res.URL != nil && res.URL.Fragment != "" && s.URL.Fragment == "" {
		u := *s.URL
		u.Fragment = res.URL.Fragment
		s.URL = &u
	}
	s.LastMethod = res.Method
	s.LastBody = res.Body
	s.LastContentType = res.ContentType
	s.Layout = nil
	s.BigTextBlocked = false
	if !s.NoJS {
		RemoveNoscript(page.Root)
	}
	loopStopped := false
	if target := MetaRefreshTarget(page); target != "" && target != page.URL.String() {
		if s.AutoNavigations >= MaxAutoNavigations {
			loopStopped = true
		} else {
			s.AutoNavigations++
			h.NavigateEntry(protocol.MethodGet, target, nil, "", res.EntryID, res.HistoryPush, false)
			return
		}
	}
	h.ResetConsole()
	h.StartJS(page)
	if h.ApplyPendingJS(true, res.EntryID, res.HistoryPush) {
		return
	}
	if s.LoopStopped {
		s.LoopStopped = false
		loopStopped = true
	}
	s.ReaderRoot = nil
	s.ReaderSavedScroll = 0
	readerFailed := false
	if s.Reader {
		if page.PDF != nil {
			readerFailed = true
		} else if root, ok := ExtractReaderPage(page.Root, page.Title); ok {
			s.ReaderRoot = root
		} else {
			readerFailed = true
		}
	}
	if !s.Reader && (s.Images == nil || !s.Images.DeferImages()) {
		s.RefreshImages()
	}
	h.Rebuild()
	s.Scroll = 0
	s.Focus = -1
	s.Forms.ClearControl()
	status := h.PageLoadStatus(page, readerFailed)
	h.SetStatus(status)
	switch {
	case loopStopped:
		h.SetStatus("Redirect loop stopped")
	case note != "" && status == "":
		h.SetStatus(note)
	}
	if frag := res.URL.Fragment; frag != "" {
		s.ScrollToAnchor(frag, h.ViewportHeight())
	}
	if res.HasRestoreScroll {
		s.Scroll = res.RestoreScroll
		s.ClampScroll(h.ViewportHeight())
	}
	if !res.HistoryPush && s.EntryID == 0 {
		res.EntryID = 0
	}
	s.EntryID = res.EntryID
	if s.URL != nil && res.EntryID != 0 {
		if !res.HistoryPush {
			s.DropCachedEntryUnless(res.EntryID, s.URL.String())
		}
		h.EmitHistoryCommit(protocol.HistoryCommit{
			EntryID: res.EntryID, URL: s.URL.String(), Scroll: s.Scroll, Push: res.HistoryPush,
		})
	}
	s.AutoNavigations = 0
	h.InvalidateFrame()
	if s.DeferredImages {
		s.Progress = 0.9
		s.ProgressHold = 0
	} else {
		s.FinishProgress()
	}
}

// RestoreEntry returns to a front-end history entry.
func (s *Session) RestoreEntryWithHooks(entryID uint64, rawURL string, scroll int, h SessionHooks) {
	if entryID == 0 {
		return
	}
	if s.ClearHints() {
		h.SetStatus("")
	}
	if s.URL != nil && s.Page != nil && s.Page.Root != nil {
		if u, err := url.Parse(rawURL); err == nil && SameDocument(u, s.URL) {
			s.EntryID = entryID
			s.URL = u
			s.Scroll = scroll
			s.ClampScroll(h.ViewportHeight())
			s.Focus = -1
			s.Hover = -1
			h.SetStatus("")
			h.InvalidateFrame()
			return
		}
	}
	if c := s.Cache.Get(entryID); c != nil {
		s.ApplyCachedPage(entryID, c, scroll, h)
		return
	}
	s.SuspendCurrentPage()
	s.NextScroll = scroll
	s.NextScrollSet = true
	h.NavigateEntry(protocol.MethodGet, rawURL, nil, "", entryID, false, false)
}

// ApplyCachedPage restores a page kept by the back/forward cache.
func (s *Session) ApplyCachedPage(entryID uint64, c *CachedPage, scroll int, h SessionHooks) {
	s.SuspendCurrentPage()
	s.Forms.ClearInputFocus()
	s.CancelLoadNow()
	h.BeginPage()
	s.LoadSeq++
	s.EntryID = entryID
	s.Page = c.Page
	s.URL = c.Page.URL
	s.ImagesPaused = false
	s.LastMethod = c.Method
	s.LastBody = c.Body
	s.LastContentType = c.ContentType
	s.Layout = nil
	s.BigTextBlocked = false
	h.ResetConsole()
	if c.JS != nil {
		s.JS = c.JS
		c.JS.Resume()
	}
	s.ReaderRoot = nil
	s.ReaderSavedScroll = 0
	readerFailed := false
	if s.Reader {
		if c.ReaderRoot != nil {
			s.ReaderRoot = c.ReaderRoot
			s.ReaderSavedScroll = c.ReaderSavedScroll
		} else if c.Page.PDF != nil {
			readerFailed = true
		} else if root, ok := ExtractReaderPage(c.Page.Root, c.Page.Title); ok {
			s.ReaderRoot = root
		} else {
			readerFailed = true
		}
	}
	h.Rebuild()
	s.Scroll = scroll
	s.ClampScroll(h.ViewportHeight())
	s.Focus = -1
	s.Hover = -1
	s.ClearSelection()
	h.SetStatus(h.PageLoadStatus(c.Page, readerFailed))
	h.InvalidateFrame()
}
