package frontend

import (
	"unicode/utf8"

	"browser/internal/protocol"
)

// promptOverlay is the URL and command line.
type promptOverlay struct {
	promptActive    bool
	promptVim       bool
	promptText      string
	promptCursor    int
	promptSelectAll bool
	promptBase      protocol.State
	promptHist      promptHistory
}

func (w *promptOverlay) isOpen() bool         { return w.promptActive }
func (w *promptOverlay) dismissOnModal() bool { return true }

func (w *promptOverlay) reset(l *Tab) {
	w.promptActive = false
	w.promptVim = false
	w.promptText = ""
	w.promptCursor = 0
	w.promptSelectAll = false
}

// install is a no-op: the prompt travels in protocol.State.
func (w *promptOverlay) install(l *Tab) bool { return false }

func (w *promptOverlay) close(l *Tab) { l.closePrompt() }

func (w *promptOverlay) applyState(l *Tab, st protocol.State) bool {
	w.promptBase = st
	if st.KeysEngine {
		l.closePrompt()
		return true
	}
	nextPageState(l, st)
	l.syncPrompt()
	return true
}

func (w *promptOverlay) handleKey(l *Tab, k string) (bool, bool) { return l.handlePromptKey(k) }

func (w *promptOverlay) handleMouse(l *Tab, ev MouseEvent) bool { return l.handlePromptMouse(ev) }

// promptHistory is the URL/command prompt's recall list.
type promptHistory struct {
	entries []string
	at      int
	draft   string
}

func (h *promptHistory) reset() {
	h.at = len(h.entries)
	h.draft = ""
}

func (h *promptHistory) record(src string) {
	if len(h.entries) == 0 || h.entries[len(h.entries)-1] != src {
		h.entries = append(h.entries, src)
		if len(h.entries) > 200 {
			h.entries = append([]string(nil), h.entries[len(h.entries)-200:]...)
		}
	}
	h.reset()
}

func (h *promptHistory) saveDraft(text string) {
	if h.at >= len(h.entries) || text != h.entries[h.at] {
		h.draft = text
	}
}

func (h *promptHistory) up(text string) (string, bool) {
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

func (h *promptHistory) down(text string) (string, bool) {
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

func runeIndexToByte(runes []rune, i int) int {
	if i <= 0 {
		return 0
	}
	if i > len(runes) {
		i = len(runes)
	}
	return len(string(runes[:i]))
}

func promptWindow(text string, cursor, contentW int, selectAll bool) (runes []rune, start, end, cur int) {
	runes = []rune(text)
	if contentW < 1 {
		return runes, 0, 0, 0
	}
	if selectAll {
		visible := text
		if stringWidth(visible) > contentW {
			visible = tailWidth(visible, contentW)
		}
		start = len(runes) - utf8.RuneCountInString(visible)
		return runes, start, len(runes), -1
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(text) {
		cursor = len(text)
	}
	cur = utf8.RuneCountInString(text[:cursor])
	if cur < 0 {
		cur = 0
	}
	if cur > len(runes) {
		cur = len(runes)
	}
	start = cur
	used := 0
	for start > 0 {
		rw := runeWidth(runes[start-1])
		if used+rw > contentW-1 {
			break
		}
		used += rw
		start--
	}
	after := cur
	if cur < len(runes) {
		after = cur + 1
	}
	used++
	end = after
	for end < len(runes) {
		rw := runeWidth(runes[end])
		if used+rw > contentW {
			break
		}
		used += rw
		end++
	}
	return runes, start, end, cur
}

func promptOffsetAt(text string, cursor int, selectAll bool, x, width int) int {
	prefixW := stringWidth(" Open: ")
	return textOffsetAt(text, cursor, selectAll, x-prefixW, width-prefixW)
}

// textOffsetAt maps a click at cell rel in a text field of contentW cells to a
// byte offset, using the same visible window as the field painter.
func textOffsetAt(text string, cursor int, selectAll bool, rel, contentW int) int {
	if contentW < 1 || rel <= 0 {
		return 0
	}
	runes, start, end, cur := promptWindow(text, cursor, contentW, selectAll)
	used := 0
	for i := start; i < end; i++ {
		if i == cur {
			if rel < used+1 {
				return runeIndexToByte(runes, i)
			}
			used++
			continue
		}
		rw := runeWidth(runes[i])
		if rel < used+rw {
			return runeIndexToByte(runes, i)
		}
		used += rw
	}
	return runeIndexToByte(runes, end)
}
