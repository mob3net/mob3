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
	scroll int
}

// historyLimit bounds the snapshot sent for about:history. It matches the
// engine HistoryPageHTML window: older entries cannot be shown or marked
// current anyway.
const historyLimit = 50

// maxDisplayURL keeps a hostile engine from parking an unbounded URL in the
// trusted history ledger.
const maxDisplayURL = 4096

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

// isAboutHistory reports whether input names the built-in history page.
func isAboutHistory(input string) bool {
	return strings.EqualFold(strings.TrimSpace(input), "about:history")
}

// recordHistoryScroll stores the current viewport row on the active entry.
func (l *Tab) recordHistoryScroll() {
	if l.historyIndex < 0 || l.historyIndex >= len(l.history) {
		return
	}
	l.history[l.historyIndex].scroll = l.State.Scroll
}

// applyHistoryCommit stores the engine's scroll and push flag. A new entry's
// URL comes from the host; the engine URL is used only for built-in pages and
// same-document fragment changes. The page title is taken from State.
func (l *Tab) applyHistoryCommit(v protocol.HistoryCommit) {
	if v.EntryID == 0 {
		return
	}
	if e := l.entryByID(v.EntryID); e != nil {
		// Reload, restore or title update: keep the host-attested URL, but let
		// a redirect on reload replace it.
		if u, ok := l.attestedURL(v.EntryID); ok {
			e.url = u
		}
		e.scroll = v.Scroll
		return
	}
	url, attested := l.attestedURL(v.EntryID)
	if !attested {
		if !l.allowUnattestedCommit(v) {
			return
		}
		url = v.URL
	}
	if !validDisplayURL(url) {
		return
	}
	l.storeHistoryCommit(v, url)
}

func (l *Tab) attestedURL(entryID uint64) (string, bool) {
	if l.AttestedURL == nil {
		return "", false
	}
	return l.AttestedURL(entryID)
}

// storeHistoryCommit applies the push/merge rule with the trusted URL.
func (l *Tab) storeHistoryCommit(v protocol.HistoryCommit, url string) {
	e := historyEntry{id: v.EntryID, url: url, scroll: v.Scroll}
	if v.Push {
		if l.historyIndex >= 0 && l.historyIndex < len(l.history) &&
			l.history[l.historyIndex].url == url {
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

func (l *Tab) entryByID(id uint64) *historyEntry {
	for i := range l.history {
		if l.history[i].id == id {
			return &l.history[i]
		}
	}
	return nil
}

// allowUnattestedCommit permits built-in pages and same-document fragment
// changes, which never perform a host document fetch.
func (l *Tab) allowUnattestedCommit(v protocol.HistoryCommit) bool {
	lower := strings.ToLower(v.URL)
	if lower == "about:home" || strings.HasPrefix(lower, "about:") {
		return true
	}
	if l.historyIndex >= 0 && l.historyIndex < len(l.history) {
		return sameDocumentWithoutFragment(l.history[l.historyIndex].url, v.URL)
	}
	return false
}

// sameDocumentWithoutFragment reports whether two URLs differ only by fragment.
func sameDocumentWithoutFragment(a, b string) bool {
	au, err := url.Parse(a)
	if err != nil {
		return false
	}
	bu, err := url.Parse(b)
	if err != nil {
		return false
	}
	au.Fragment, bu.Fragment = "", ""
	return au.String() == bu.String()
}

// pageState replaces the engine's URL with the committed history entry. The
// page title stays engine-supplied and is not part of the trusted URL ledger.
func (l *Tab) pageState(v protocol.State) protocol.State {
	if l.historyIndex < 0 || l.historyIndex >= len(l.history) {
		return v
	}
	if e := &l.history[l.historyIndex]; e.url != "" {
		v.URL = e.url
	}
	return v
}

// sendHistorySnapshot sends the bounded display snapshot the engine renders
// for about:history.
func (l *Tab) sendHistorySnapshot() {
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
func (l *Tab) navigate(target string) {
	l.recordHistoryScroll()
	if isAboutHistory(target) {
		l.sendHistorySnapshot()
	}
	_ = l.Session.Send(protocol.Command{Body: protocol.Navigate{URL: target, Push: true}})
}

// historyBack moves one entry back. The engine restores its cached page or
// reloads the URL with the entry's scroll offset.
func (l *Tab) historyBack() {
	l.recordHistoryScroll()
	if l.historyIndex <= 0 || len(l.history) == 0 {
		l.showStatus("No previous page")
		return
	}
	l.historyIndex--
	l.restoreCurrent()
}

// historyForward moves one entry forward.
func (l *Tab) historyForward() {
	l.recordHistoryScroll()
	if len(l.history) == 0 || l.historyIndex+1 >= len(l.history) {
		l.showStatus("No next page")
		return
	}
	l.historyIndex++
	l.restoreCurrent()
}

func (l *Tab) restoreCurrent() {
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
func (l *Tab) reload() {
	if isAboutHistory(l.State.Page.URL) {
		l.recordHistoryScroll()
		l.sendHistorySnapshot()
	}
	_ = l.Session.Send(protocol.Command{Body: protocol.App{Action: protocol.AppReload}})
}

// showStatus shows a transient front-end status line.
func (l *Tab) showStatus(text string) {
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
