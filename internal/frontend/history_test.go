package frontend

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"browser/internal/protocol"
)

// attestedLoop returns a loop whose host knows the supplied entry URLs.
func attestedLoop(urls map[uint64]string) *Tab {
	return &Tab{AttestedURL: func(id uint64) (string, bool) {
		u, ok := urls[id]
		return u, ok
	}}
}

func TestApplyHistoryCommitPushReplaceAndTruncate(t *testing.T) {
	urls := map[uint64]string{
		1: "https://example.com/a", 2: "https://example.com/b",
		3: "https://example.com/c", 4: "https://example.com/c",
	}
	l := attestedLoop(urls)
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
	urls[4] = "https://example.com/d"
	l.applyHistoryCommit(protocol.HistoryCommit{EntryID: 4, URL: "https://example.com/d", Scroll: 2})
	if len(l.history) != 2 || l.historyIndex != 1 || l.history[1].url != "https://example.com/d" || l.history[1].scroll != 2 {
		t.Fatalf("replace = %+v at %d", l.history, l.historyIndex)
	}
}

func TestTabHistoryIsBounded(t *testing.T) {
	urls := map[uint64]string{}
	l := attestedLoop(urls)
	total := maxTabHistory + 25
	for i := 1; i <= total; i++ {
		url := fmt.Sprintf("https://example.com/%d", i)
		urls[uint64(i)] = url
		l.applyHistoryCommit(protocol.HistoryCommit{EntryID: uint64(i), URL: url, Push: true})
	}
	if len(l.history) != maxTabHistory {
		t.Fatalf("history = %d entries, want %d", len(l.history), maxTabHistory)
	}
	if l.historyIndex != len(l.history)-1 {
		t.Fatalf("index = %d, want %d", l.historyIndex, len(l.history)-1)
	}
	if got := l.history[l.historyIndex].url; got != fmt.Sprintf("https://example.com/%d", total) {
		t.Fatalf("newest entry = %q", got)
	}
	if got := l.history[0].url; got != fmt.Sprintf("https://example.com/%d", total-maxTabHistory+1) {
		t.Fatalf("oldest kept entry = %q", got)
	}
}

func TestSendHistorySnapshotCapsAtLimit(t *testing.T) {
	s := &recordSession{}
	l := &Tab{Session: s}
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
	keys := make(chan string, 9)
	keys <- MouseToken(MouseEvent{Button: MouseButtonBack, X: 1, Y: 1, Press: true})
	// A held side button also produces drag reports; each one must not step.
	keys <- MouseToken(MouseEvent{Button: MouseButtonBack, X: 2, Y: 1, Press: true, Motion: true})
	keys <- MouseToken(MouseEvent{Button: MouseButtonBack, X: 3, Y: 1, Press: true, Motion: true})
	keys <- MouseToken(MouseEvent{Button: MouseButtonBack, X: 3, Y: 1, Press: false})
	keys <- MouseToken(MouseEvent{Button: MouseButtonForward, X: 1, Y: 1, Press: true})
	keys <- MouseToken(MouseEvent{Button: MouseButtonForward, X: 2, Y: 1, Press: true, Motion: true})
	keys <- MouseToken(MouseEvent{Button: MouseButtonForward, X: 2, Y: 1, Press: false})
	keys <- "eof"
	tab := &Tab{Session: s, Stderr: io.Discard}
	tab.history = []historyEntry{
		{id: 1, url: "https://example.com/a"},
		{id: 2, url: "https://example.com/b"},
		{id: 3, url: "https://example.com/c"},
	}
	tab.historyIndex = 2
	loop := &SessionLoop{Tabs: []*Tab{tab}, Active: tab, Keys: keys}
	if err := loop.Run(); err != nil {
		t.Fatal(err)
	}
	want := []protocol.CommandBody{
		protocol.Restore{EntryID: 2, URL: "https://example.com/b"},
		protocol.Restore{EntryID: 3, URL: "https://example.com/c"},
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

func TestPageStateUsesHistoryLedger(t *testing.T) {
	l := attestedLoop(map[uint64]string{7: "https://example.com/a"})
	l.applyHistoryCommit(protocol.HistoryCommit{
		EntryID: 7, URL: "https://example.com/a", Push: true,
	})
	got := l.pageState(protocol.State{URL: "https://example.com/other", Title: "Other"})
	if got.URL != "https://example.com/a" || got.Title != "Other" {
		t.Fatalf("state = %+v, want the committed URL and the engine title", got)
	}
}

func TestInvalidHistoryCommitIgnored(t *testing.T) {
	l := &Tab{}
	for _, u := range []string{"file:///etc/passwd", "javascript:alert(1)", "", "not a url\n"} {
		l.applyHistoryCommit(protocol.HistoryCommit{EntryID: 1, URL: u, Push: true})
	}
	if len(l.history) != 0 {
		t.Fatalf("invalid commits were stored: %+v", l.history)
	}
}

func TestHostAttestationOverridesForgedURL(t *testing.T) {
	l := attestedLoop(map[uint64]string{1: "https://real.example/"})
	l.applyHistoryCommit(protocol.HistoryCommit{EntryID: 1, URL: "https://spoofed.example/", Push: true})
	if len(l.history) != 1 || l.history[0].url != "https://real.example/" {
		t.Fatalf("history = %+v, want the host-attested URL", l.history)
	}
}

func TestUnattestedNewCommitBlocked(t *testing.T) {
	l := attestedLoop(nil)
	l.applyHistoryCommit(protocol.HistoryCommit{EntryID: 9, URL: "https://spoofed.example/", Push: true})
	if len(l.history) != 0 {
		t.Fatalf("unattested commit was stored: %+v", l.history)
	}
}

func TestAboutAndFragmentCommitsNeedNoAttestation(t *testing.T) {
	l := attestedLoop(map[uint64]string{2: "https://example.com/a"})
	l.applyHistoryCommit(protocol.HistoryCommit{EntryID: 1, URL: "about:home", Push: true})
	if len(l.history) != 1 || l.history[0].url != "about:home" {
		t.Fatalf("about commit = %+v", l.history)
	}
	l.applyHistoryCommit(protocol.HistoryCommit{EntryID: 2, URL: "https://example.com/a", Push: true})
	l.applyHistoryCommit(protocol.HistoryCommit{EntryID: 3, URL: "https://example.com/a#section", Push: true})
	if len(l.history) != 3 || l.history[2].url != "https://example.com/a#section" {
		t.Fatalf("fragment commit = %+v", l.history)
	}
}

// An attested URL that cannot be displayed in full must still be committed in
// bounded form; dropping it left the address bar on the previous page.
func TestAttestedLongURLIsTruncatedNotDropped(t *testing.T) {
	long := "https://evil.example/" + strings.Repeat("a", maxDisplayURL+100)
	l := attestedLoop(map[uint64]string{1: long})
	l.applyHistoryCommit(protocol.HistoryCommit{EntryID: 1, URL: "https://bank.example/", Push: true})
	if len(l.history) != 1 {
		t.Fatalf("commit was dropped: %+v", l.history)
	}
	got := l.history[0].url
	if len(got) > maxDisplayURL {
		t.Fatalf("stored URL is %d bytes, want at most %d", len(got), maxDisplayURL)
	}
	if !strings.HasPrefix(got, "https://evil.example/") || !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated URL = %q", got)
	}
	state := l.pageState(protocol.State{URL: "https://bank.example/"})
	if state.URL != got {
		t.Fatalf("address bar = %q, want %q", state.URL, got)
	}
}

func TestExistingEntryLongURLIsBounded(t *testing.T) {
	urls := map[uint64]string{2: "https://real.example/"}
	l := attestedLoop(urls)
	l.applyHistoryCommit(protocol.HistoryCommit{EntryID: 2, URL: "https://real.example/", Push: true})
	urls[2] = "https://real.example/" + strings.Repeat("b", maxDisplayURL+200)
	l.applyHistoryCommit(protocol.HistoryCommit{EntryID: 2, URL: "https://spoofed.example/", Scroll: 5})
	if len(l.history) != 1 || len(l.history[0].url) > maxDisplayURL {
		t.Fatalf("existing entry was not bounded: %+v", l.history)
	}
	if l.history[0].scroll != 5 {
		t.Fatalf("scroll = %d, want 5", l.history[0].scroll)
	}
}

func TestExistingEntryCommitKeepsAttestedURL(t *testing.T) {
	l := attestedLoop(map[uint64]string{4: "https://real.example/a"})
	l.applyHistoryCommit(protocol.HistoryCommit{EntryID: 4, URL: "https://real.example/a", Push: true})
	l.applyHistoryCommit(protocol.HistoryCommit{EntryID: 4, URL: "https://spoofed.example/", Scroll: 5})
	if len(l.history) != 1 || l.history[0].url != "https://real.example/a" || l.history[0].scroll != 5 {
		t.Fatalf("entry = %+v", l.history)
	}
}
