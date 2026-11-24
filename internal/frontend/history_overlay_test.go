package frontend

import (
	"io"
	"testing"

	"browser/internal/protocol"
)

func TestHistoryOverlayOpensAndRestoresEntry(t *testing.T) {
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
	if tab.historyIndex != 1 {
		t.Fatalf("history index = %d, want 1", tab.historyIndex)
	}
	found := false
	for _, cmd := range s.cmds {
		if cmd.Body == (protocol.Restore{EntryID: 2, URL: "https://example.test/b", Scroll: 4}) {
			found = true
		}
		if _, ok := cmd.Body.(protocol.Navigate); ok {
			t.Fatalf("history overlay navigated instead of restoring: %#v", cmd.Body)
		}
	}
	if !found {
		t.Fatalf("history restore command missing: %#v", s.cmds)
	}
}
