package frontend

// lineHistoryLimit bounds the retained entries. Old entries fall off the
// front.
const lineHistoryLimit = 200

// lineHistory is one shell-style recall list for an input row. at ==
// len(entries) means the draft line is shown.
type lineHistory struct {
	entries []string
	at      int
	draft   string
}

// reset returns to the draft line without discarding recorded entries.
func (h *lineHistory) reset() {
	h.at = len(h.entries)
	h.draft = ""
}

// record appends src, dropping consecutive duplicates, caps the list and
// returns to the draft line.
func (h *lineHistory) record(src string) {
	if len(h.entries) == 0 || h.entries[len(h.entries)-1] != src {
		h.entries = append(h.entries, src)
		if len(h.entries) > lineHistoryLimit {
			h.entries = append([]string(nil), h.entries[len(h.entries)-lineHistoryLimit:]...)
		}
	}
	h.reset()
}

// saveDraft keeps the line the user is editing, including edits made to a
// recalled entry, so moving away and back does not discard it.
func (h *lineHistory) saveDraft(text string) {
	if h.at >= len(h.entries) || text != h.entries[h.at] {
		h.draft = text
	}
}

// up moves to the previous entry. It returns the current text unchanged and
// false when already at the oldest entry.
func (h *lineHistory) up(text string) (string, bool) {
	if h.at > len(h.entries) {
		h.at = len(h.entries)
	}
	h.saveDraft(text)
	if h.at == 0 {
		return text, false
	}
	h.at--
	return h.entries[h.at], true
}

// down moves to the next entry, ending at the saved draft. It returns the
// current text unchanged and false when already at the draft line.
func (h *lineHistory) down(text string) (string, bool) {
	if h.at > len(h.entries) {
		h.at = len(h.entries)
	}
	h.saveDraft(text)
	if h.at >= len(h.entries) {
		return text, false
	}
	h.at++
	if h.at == len(h.entries) {
		return h.draft, true
	}
	return h.entries[h.at], true
}
