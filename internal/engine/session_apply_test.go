package engine

import (
	"testing"
	"time"

	"browser/internal/protocol"
)

type testSessionHooks struct {
	status  string
	rebuild int
	commits []protocol.HistoryCommit
}

func (h *testSessionHooks) BeginPage()                                                       {}
func (h *testSessionHooks) ResetUI()                                                         {}
func (h *testSessionHooks) ResetConsole()                                                    {}
func (h *testSessionHooks) StartJS(*Page)                                                    {}
func (h *testSessionHooks) ApplyPendingJS(bool, uint64, bool) bool                           { return false }
func (h *testSessionHooks) NavigateEntry(string, string, []byte, string, uint64, bool, bool) {}
func (h *testSessionHooks) Rebuild()                                                         { h.rebuild++ }
func (h *testSessionHooks) InvalidateFrame()                                                 {}
func (h *testSessionHooks) SetStatus(s string)                                               { h.status = s }
func (h *testSessionHooks) PageLoadStatus(*Page, bool) string                                { return "" }
func (h *testSessionHooks) ViewportHeight() int                                              { return 20 }
func (h *testSessionHooks) Viewport() (int, int)                                             { return 0, 20 }
func (h *testSessionHooks) HandleKey(string) bool                                            { return false }
func (h *testSessionHooks) Navigate(string, bool)                                            {}
func (h *testSessionHooks) NavigateRequest(string, string, []byte, string, bool)             {}
func (h *testSessionHooks) ActivateHintControl(*Node, Rect)                                  {}
func (h *testSessionHooks) OpenSelectMenu(*Node)                                             {}
func (h *testSessionHooks) ConsoleOpen() bool                                                { return false }
func (h *testSessionHooks) HoverRebuild() (bool, time.Time)                                  { return false, time.Time{} }
func (h *testSessionHooks) ClearHoverRebuild()                                               {}
func (h *testSessionHooks) NewJSHost() JSHost                                                { return nil }
func (h *testSessionHooks) SetModuleErr(bool)                                                {}
func (h *testSessionHooks) TakePendingStatus() string                                        { return "" }
func (h *testSessionHooks) ShowDownload(string, string)                                      {}
func (h *testSessionHooks) EmitHistoryCommit(c protocol.HistoryCommit) {
	h.commits = append(h.commits, c)
}

func TestApplyLoadResult(t *testing.T) {
	u := mustParseURL(t, "https://example.com/a")
	page := &Page{URL: u, Base: u, Root: ParseHTML(`<html><body>ok</body></html>`), Title: "ok", Status: 200}
	s := NewSession()
	s.NoJS = true
	s.LoadSeq = 1
	h := &testSessionHooks{}
	s.ApplyLoadResult(LoadResult{Seq: 1, Page: page, URL: u, EntryID: 5, HistoryPush: true, Method: "GET"}, h)
	if s.Page != page || s.URL != u || s.EntryID != 5 {
		t.Fatalf("page/url/entry not applied: %+v %v %d", s.Page, s.URL, s.EntryID)
	}
	if h.rebuild == 0 {
		t.Fatal("rebuild hook was not called")
	}
	if len(h.commits) != 1 || h.commits[0].EntryID != 5 || h.commits[0].URL != u.String() {
		t.Fatalf("commits = %+v", h.commits)
	}
}
