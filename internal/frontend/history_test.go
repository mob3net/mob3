package frontend

import (
	"io"
	"testing"

	"browser/internal/protocol"
)

func TestApplyHistoryCommitPushReplaceAndTruncate(t *testing.T) {
	l := &SessionLoop{}
	l.applyHistoryCommit(protocol.HistoryCommit{EntryID: 1, URL: "https://example.com/a", Scroll: 3, Push: true})
	if len(l.history) != 1 || l.historyIndex != 0 || l.history[0].scroll != 3 {
		t.Fatalf("first push = %+v at %d", l.history, l.historyIndex)
	}
	l.applyHistoryCommit(protocol.HistoryCommit{EntryID: 2, URL: "https://example.com/b", Push: true})
	if len(l.history) != 2 || l.historyIndex != 1 {
		t.Fatalf("second push = %+v at %d", l.history, l.historyIndex)
	}

	// A push from an older entry truncates the forward entries.
	l.historyIndex = 0
	l.applyHistoryCommit(protocol.HistoryCommit{EntryID: 3, URL: "https://example.com/c", Push: true})
	if len(l.history) != 2 || l.historyIndex != 1 || l.history[1].url != "https://example.com/c" {
		t.Fatalf("truncating push = %+v at %d", l.history, l.historyIndex)
	}

	// Pushing the current URL merges into it and updates the ID/scroll.
	l.applyHistoryCommit(protocol.HistoryCommit{EntryID: 4, URL: "https://example.com/c", Scroll: 9, Push: true})
	if len(l.history) != 2 || l.historyIndex != 1 || l.history[1].id != 4 || l.history[1].scroll != 9 {
		t.Fatalf("merged push = %+v at %d", l.history, l.historyIndex)
	}

	// A replace updates the active entry in place.
	l.applyHistoryCommit(protocol.HistoryCommit{EntryID: 4, URL: "https://example.com/d", Scroll: 2})
	if len(l.history) != 2 || l.historyIndex != 1 || l.history[1].url != "https://example.com/d" || l.history[1].scroll != 2 {
		t.Fatalf("replace = %+v at %d", l.history, l.historyIndex)
	}
}

func TestSendHistorySnapshotCapsAtLimit(t *testing.T) {
	s := &recordSession{}
	l := &SessionLoop{Session: s}
	for i := 0; i < historyLimit+10; i++ {
		l.history = append(l.history, historyEntry{id: uint64(i + 1), url: string(rune('a' + i%26))})
	}
	l.historyIndex = len(l.history) - 3
	l.sendHistorySnapshot()
	if len(s.cmds) != 1 {
		t.Fatalf("commands = %#v", s.cmds)
	}
	h, ok := s.cmds[0].Body.(protocol.History)
	if !ok {
		t.Fatalf("body = %T", s.cmds[0].Body)
	}
	if len(h.Entries) != historyLimit {
		t.Fatalf("entries = %d, want %d", len(h.Entries), historyLimit)
	}
	if h.Current != historyLimit-3 {
		t.Fatalf("current = %d, want %d", h.Current, historyLimit-3)
	}

	// An index older than the window is not marked.
	s.cmds = nil
	l.historyIndex = 2
	l.sendHistorySnapshot()
	h = s.cmds[0].Body.(protocol.History)
	if h.Current != -1 {
		t.Fatalf("old current = %d, want -1", h.Current)
	}
}

func TestMouseSideButtonsUseHistory(t *testing.T) {
	s := &recordSession{}
	keys := make(chan string, 3)
	keys <- MouseToken(MouseEvent{Button: MouseButtonBack, X: 1, Y: 1, Press: true})
	keys <- MouseToken(MouseEvent{Button: MouseButtonForward, X: 1, Y: 1, Press: true})
	keys <- "eof"
	loop := &SessionLoop{Session: s, Keys: keys, Stderr: io.Discard}
	loop.history = []historyEntry{{id: 1, url: "https://example.com/a"}, {id: 2, url: "https://example.com/b"}}
	loop.historyIndex = 1
	if err := loop.Run(); err != nil {
		t.Fatal(err)
	}
	want := []protocol.CommandBody{
		protocol.Restore{EntryID: 1, URL: "https://example.com/a"},
		protocol.Restore{EntryID: 2, URL: "https://example.com/b"},
	}
	if len(s.cmds) != len(want) {
		t.Fatalf("commands = %#v", s.cmds)
	}
	for i := range want {
		if s.cmds[i].Body != want[i] {
			t.Fatalf("command %d = %#v, want %#v", i, s.cmds[i].Body, want[i])
		}
	}
}
