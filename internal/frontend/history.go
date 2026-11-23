package frontend

import (
	"net/url"
	"strings"

	"browser/internal/protocol"
)

// historyEntry is one front-end-owned session-history position. The engine
// assigns the opaque ID, which keys its memory-only page cache.
type historyEntry struct {
	id     uint64
	url    string
	title  string
	scroll int
}

// historyLimit bounds the snapshot sent for about:history. It matches the
// engine HistoryPageHTML window: older entries cannot be shown or marked
// current anyway.
const historyLimit = 50

// Display limits keep a hostile engine from parking an unbounded URL or title
// in the trusted history ledger.
const (
	maxDisplayURL   = 4096
	maxDisplayTitle = 512
)

// validDisplayURL accepts only the schemes the session can render. The front
// end owns the address bar, so a malformed or unsupported commit is ignored.
func validDisplayURL(raw string) bool {
	if raw == "" || len(raw) > maxDisplayURL {
		return false
	}
	if strings.IndexFunc(raw, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "about", "data":
		return true
	}
	return false
}

func cleanTitle(s string) string {
	if len(s) <= maxDisplayTitle {
		return s
	}
	r := []rune(s)
	if len(r) <= maxDisplayTitle {
		return s
	}
	return string(r[:maxDisplayTitle])
}

// isAboutHistory reports whether input names the built-in history page.
func isAboutHistory(input string) bool {
	return strings.EqualFold(strings.TrimSpace(input), "about:history")
}

// recordHistoryScroll stores the current viewport row on the active entry.
func (l *SessionLoop) recordHistoryScroll() {
	if l.historyIndex < 0 || l.historyIndex >= len(l.history) {
		return
	}
	l.history[l.historyIndex].scroll = l.State.Scroll
}

// applyHistoryCommit updates the front-end list from the engine's final URL.
// Push appends a new entry, merging into the current one when the URL is
// unchanged; otherwise the entry is updated in place.
func (l *SessionLoop) applyHistoryCommit(v protocol.HistoryCommit) {
	if v.EntryID == 0 || !validDisplayURL(v.URL) {
		return
	}
	e := historyEntry{id: v.EntryID, url: v.URL, title: cleanTitle(v.Title), scroll: v.Scroll}
	if v.Push {
		if l.historyIndex >= 0 && l.historyIndex < len(l.history) &&
			l.history[l.historyIndex].url == v.URL {
			l.history[l.historyIndex] = e
			return
		}
		if l.historyIndex+1 < len(l.history) {
			l.history = l.history[:l.historyIndex+1]
		}
		l.history = append(l.history, e)
		l.historyIndex = len(l.history) - 1
		return
	}
	if len(l.history) == 0 {
		l.history = append(l.history, e)
		l.historyIndex = 0
		return
	}
	if l.historyIndex < 0 || l.historyIndex >= len(l.history) {
		l.historyIndex = len(l.history) - 1
	}
	l.history[l.historyIndex] = e
}

// pageState replaces the engine's URL/title with the front-end ledger. A title
// change is accepted only while the engine still reports the committed URL.
func (l *SessionLoop) pageState(v protocol.State) protocol.State {
	if l.historyIndex < 0 || l.historyIndex >= len(l.history) {
		return v
	}
	e := &l.history[l.historyIndex]
	if v.URL == e.url && v.Title != "" {
		e.title = cleanTitle(v.Title)
	}
	if e.url != "" {
		v.URL = e.url
	}
	if e.title != "" {
		v.Title = e.title
	}
	return v
}

// sendHistorySnapshot sends the bounded display snapshot the engine renders
// for about:history.
func (l *SessionLoop) sendHistorySnapshot() {
	start := 0
	if len(l.history) > historyLimit {
		start = len(l.history) - historyLimit
	}
	entries := make([]protocol.HistoryEntry, 0, len(l.history)-start)
	for _, e := range l.history[start:] {
		entries = append(entries, protocol.HistoryEntry{URL: e.url})
	}
	current := -1
	if l.historyIndex >= start && l.historyIndex < len(l.history) {
		current = l.historyIndex - start
	}
	_ = l.Session.Send(protocol.Command{Body: protocol.History{Entries: entries, Current: current}})
}

// navigate starts a top-level load. A typed about:history carries a fresh
// snapshot so the page reflects the current list.
func (l *SessionLoop) navigate(target string) {
	l.recordHistoryScroll()
	if isAboutHistory(target) {
		l.sendHistorySnapshot()
	}
	_ = l.Session.Send(protocol.Command{Body: protocol.Navigate{URL: target, Push: true}})
}

// openHistory opens about:history with the current list snapshot.
func (l *SessionLoop) openHistory() {
	l.recordHistoryScroll()
	l.sendHistorySnapshot()
	_ = l.Session.Send(protocol.Command{Body: protocol.Navigate{URL: "about:history", Push: true}})
}

// historyBack moves one entry back. The engine restores its cached page or
// reloads the URL with the entry's scroll offset.
func (l *SessionLoop) historyBack() {
	l.recordHistoryScroll()
	if l.historyIndex <= 0 || len(l.history) == 0 {
		l.showStatus("No previous page")
		return
	}
	l.historyIndex--
	l.restoreCurrent()
}

// historyForward moves one entry forward.
func (l *SessionLoop) historyForward() {
	l.recordHistoryScroll()
	if len(l.history) == 0 || l.historyIndex+1 >= len(l.history) {
		l.showStatus("No next page")
		return
	}
	l.historyIndex++
	l.restoreCurrent()
}

func (l *SessionLoop) restoreCurrent() {
	e := l.history[l.historyIndex]
	if isAboutHistory(e.url) {
		l.sendHistorySnapshot()
	}
	_ = l.Session.Send(protocol.Command{Body: protocol.Restore{
		EntryID: e.id, URL: e.url, Scroll: e.scroll,
	}})
}

// reload refreshes the current document. Reloading about:history refreshes the
// display snapshot as well.
func (l *SessionLoop) reload() {
	if isAboutHistory(l.State.Page.URL) {
		l.recordHistoryScroll()
		l.sendHistorySnapshot()
	}
	_ = l.Session.Send(protocol.Command{Body: protocol.App{Action: protocol.AppReload}})
}

// showStatus shows a transient front-end status line.
func (l *SessionLoop) showStatus(text string) {
	st := l.State.Page
	st.Status = text
	st.FooterLeft = " " + text
	st.FooterStatusHit = false
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		_ = l.Presenter.Refresh()
	}
}
