package frontend

// historyLogLimit bounds the shared visited-page list. Per-tab back/forward
// stacks keep their own entries; this list is for the history overlay and the
// rendered about:history page.
const historyLogLimit = 500

// historyLog is the browser-wide visited-page list. Entries are appended in
// visit order and displayed newest first by the history overlay.
type historyLog struct {
	entries []historyEntry
}

func newHistoryLog() *historyLog { return &historyLog{} }

// Upsert replaces an entry with the same ID or appends a new visit.
func (h *historyLog) Upsert(entry historyEntry) {
	if h == nil {
		return
	}
	for i := range h.entries {
		if h.entries[i].id == entry.id {
			h.entries[i] = entry
			return
		}
	}
	h.entries = append(h.entries, entry)
	if len(h.entries) > historyLogLimit {
		h.entries = append([]historyEntry(nil), h.entries[len(h.entries)-historyLogLimit:]...)
	}
}

// Entries returns a copy in visit order.
func (h *historyLog) Entries() []historyEntry {
	if h == nil {
		return nil
	}
	return append([]historyEntry(nil), h.entries...)
}
