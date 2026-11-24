package frontend

import (
	"io"
	"testing"

	"browser/internal/protocol"
)

func TestHistoryListIsGlobalAcrossTabs(t *testing.T) {
	log := newHistoryLog()
	first := &Tab{historyLog: log, AttestedURL: func(id uint64) (string, bool) {
		if id == 1 {
			return "https://example.test/a", true
		}
		return "", false
	}}
	second := &Tab{historyLog: log, AttestedURL: func(id uint64) (string, bool) {
		if id == 2 {
			return "https://example.test/b", true
		}
		return "", false
	}}
	first.applyHistoryCommit(protocol.HistoryCommit{EntryID: 1, Scroll: 2, Push: true})
	second.applyHistoryCommit(protocol.HistoryCommit{EntryID: 2, Scroll: 4, Push: true})

	firstView := first.buildHistoryView()
	secondView := second.buildHistoryView()
	if len(firstView.entries) != 2 || len(secondView.entries) != 2 {
		t.Fatalf("global history rows = %d/%d", len(firstView.entries), len(secondView.entries))
	}
	if firstView.entries[0].url != "https://example.test/b" ||
		firstView.entries[1].url != "https://example.test/a" {
		t.Fatalf("global history order = %+v", firstView.entries)
	}
	if firstView.current != 1 || secondView.current != 0 {
		t.Fatalf("tab current markers = %d/%d", firstView.current, secondView.current)
	}
	if len(first.history) != 1 || len(second.history) != 1 {
		t.Fatalf("per-tab stacks = %d/%d", len(first.history), len(second.history))
	}

	second.Session = &recordSession{}
	second.Presenter = NewPresenter(io.Discard, true)
	second.State.Page = protocol.State{Width: 80, Height: 24, HeaderRows: 2, FrontendChrome: true}
	second.openHistory()
	second.handleHistoryKey("end")
	second.handleHistoryKey("enter")
	found := false
	for _, cmd := range second.Session.(*recordSession).cmds {
		if cmd.Body == (protocol.Navigate{URL: "https://example.test/a", Push: true}) {
			found = true
		}
	}
	if !found {
		t.Fatal("selecting a global entry did not open it in the current tab")
	}
}

func TestHistoryOverlayOpensAndChoosesEntry(t *testing.T) {
	s := &recordSession{}
	tab := &Tab{
		Session: s, Presenter: NewPresenter(io.Discard, true),
		history: []historyEntry{
			{id: 1, url: "https://example.test/a", scroll: 2},
			{id: 2, url: "https://example.test/b", scroll: 4},
			{id: 3, url: "https://example.test/c", scroll: 6},
		},
		historyIndex: 2,
	}
	tab.State.Page = protocol.State{Width: 80, Height: 24, HeaderRows: 2, FrontendChrome: true}
	tab.openHistory()
	if !tab.historyActive {
		t.Fatal("history overlay did not open")
	}
	if got := len(tab.historyView.entries); got != 3 {
		t.Fatalf("history rows = %d", got)
	}
	if tab.historyView.index != 0 || tab.historyView.current != 0 {
		t.Fatalf("history selection = %d current = %d", tab.historyView.index, tab.historyView.current)
	}
	if !tab.State.Page.KeysEngine || tab.Presenter.history == nil {
		t.Fatal("history overlay state was not presented")
	}
	if handled, quit := tab.handleHistoryKey("down"); !handled || quit || tab.historyView.index != 1 {
		t.Fatalf("down: handled=%v quit=%v index=%d", handled, quit, tab.historyView.index)
	}
	if handled, quit := tab.handleHistoryKey("enter"); !handled || quit {
		t.Fatalf("enter: handled=%v quit=%v", handled, quit)
	}
	if tab.historyActive || tab.Presenter.history != nil {
		t.Fatal("history overlay stayed open after choosing")
	}
	if tab.historyIndex != 2 {
		t.Fatalf("back/forward index changed to %d", tab.historyIndex)
	}
	found := false
	for _, cmd := range s.cmds {
		if cmd.Body == (protocol.Navigate{URL: "https://example.test/b", Push: true}) {
			found = true
		}
		if _, ok := cmd.Body.(protocol.Restore); ok {
			t.Fatalf("history overlay restored an old entry instead of opening: %#v", cmd.Body)
		}
	}
	if !found {
		t.Fatalf("history navigation missing: %#v", s.cmds)
	}
}
