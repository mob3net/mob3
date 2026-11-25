// Package linehistory is the shell-style recall list shared by the front-end
// URL/command prompt and console input rows and the engine's console input.
// It stores recent entries, drops consecutive duplicates, keeps an edited
// draft, and caps the list. Standard library only.
package linehistory

// limit bounds the retained entries. Old entries fall off the front.
const limit = 200

// History is one recall list. at == len(entries) means the draft line is shown.
type History struct {
	entries []string
	at      int
	draft   string
}

// Reset returns to the draft line without discarding recorded entries.
func (h *History) Reset() {
	h.at = len(h.entries)
	h.draft = ""
}

// Record appends src, dropping consecutive duplicates, caps the list and
// returns to the draft line.
func (h *History) Record(src string) {
	if len(h.entries) == 0 || h.entries[len(h.entries)-1] != src {
		h.entries = append(h.entries, src)
		if len(h.entries) > limit {
			h.entries = append([]string(nil), h.entries[len(h.entries)-limit:]...)
		}
	}
	h.Reset()
}

// saveDraft keeps the line the user is editing, including edits made to a
// recalled entry, so moving away and back does not discard it.
func (h *History) saveDraft(text string) {
	if h.at >= len(h.entries) || text != h.entries[h.at] {
		h.draft = text
	}
}

// Up moves to the previous entry. It returns the current text unchanged and
// false when already at the oldest entry.
func (h *History) Up(text string) (string, bool) {
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

// Down moves to the next entry, ending at the saved draft. It returns the
// current text unchanged and false when already at the draft line.
func (h *History) Down(text string) (string, bool) {
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
