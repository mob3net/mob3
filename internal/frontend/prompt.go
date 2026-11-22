package frontend

import (
	"unicode"
	"unicode/utf8"
)

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

type editConfig struct {
	selAll            *bool
	collapseSelection bool
	maxRunes          int
}

func editLineKey(value string, caret int, k string, cfg editConfig) (string, int, bool) {
	if caret < 0 {
		caret = 0
	}
	if caret > len(value) {
		caret = len(value)
	}
	all := cfg.selAll != nil && *cfg.selAll
	clearSelection := func() {
		if cfg.selAll != nil {
			*cfg.selAll = false
		}
	}
	at := func(n int) (string, int, bool) {
		clearSelection()
		return value, n, true
	}
	switch k {
	case "ctrl-a":
		if cfg.selAll == nil {
			return value, caret, false
		}
		*cfg.selAll = true
		return value, len(value), true
	case "left", "ctrl-b":
		if all && cfg.collapseSelection {
			return at(0)
		}
		return at(prevRuneBoundary(value, caret))
	case "right", "ctrl-f":
		if all && cfg.collapseSelection {
			return at(len(value))
		}
		return at(nextRuneBoundary(value, caret))
	case "alt-b", "ctrl-left", "alt-left":
		if all && cfg.collapseSelection {
			return at(0)
		}
		return at(prevWordBoundary(value, caret))
	case "alt-f", "ctrl-right", "alt-right":
		if all && cfg.collapseSelection {
			return at(len(value))
		}
		return at(nextWordBoundary(value, caret))
	case "home":
		return at(0)
	case "end", "ctrl-e":
		return at(len(value))
	case "backspace":
		clearSelection()
		if all {
			return "", 0, true
		}
		if caret > 0 {
			prev := prevRuneBoundary(value, caret)
			return value[:prev] + value[caret:], prev, true
		}
		return value, caret, true
	case "delete":
		clearSelection()
		if all {
			return "", 0, true
		}
		if caret < len(value) {
			next := nextRuneBoundary(value, caret)
			return value[:caret] + value[next:], caret, true
		}
		return value, caret, true
	case "alt-backspace", "ctrl-backspace", "ctrl-w":
		clearSelection()
		if all {
			return "", 0, true
		}
		cut := prevWordBoundary(value, caret)
		return value[:cut] + value[caret:], cut, true
	case "alt-delete", "ctrl-delete", "alt-d":
		clearSelection()
		if all {
			return "", 0, true
		}
		end := forwardWordBoundary(value, caret)
		return value[:caret] + value[end:], caret, true
	case "ctrl-u":
		clearSelection()
		return "", 0, true
	case "ctrl-k":
		clearSelection()
		if all {
			return "", 0, true
		}
		return value[:caret], caret, true
	}
	if utf8.RuneCountInString(k) == 1 {
		r, _ := utf8.DecodeRuneInString(k)
		if r >= 32 && r != 127 {
			clearSelection()
			if cfg.maxRunes > 0 && utf8.RuneCountInString(value) >= cfg.maxRunes {
				return value, caret, true
			}
			if all {
				return k, len(k), true
			}
			return value[:caret] + k + value[caret:], caret + len(k), true
		}
	}
	return value, caret, false
}

func prevRuneBoundary(s string, i int) int {
	if i <= 0 {
		return 0
	}
	if i > len(s) {
		i = len(s)
	}
	_, size := utf8.DecodeLastRuneInString(s[:i])
	return i - size
}

func nextRuneBoundary(s string, i int) int {
	if i >= len(s) {
		return len(s)
	}
	if i < 0 {
		i = 0
	}
	_, size := utf8.DecodeRuneInString(s[i:])
	return i + size
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func prevWordBoundary(s string, i int) int {
	if i <= 0 {
		return 0
	}
	if i > len(s) {
		i = len(s)
	}
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:i])
		if isWordRune(r) {
			break
		}
		i -= size
	}
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:i])
		if !isWordRune(r) {
			break
		}
		i -= size
	}
	return i
}

func nextWordBoundary(s string, i int) int {
	if i < 0 {
		i = 0
	}
	if i > len(s) {
		i = len(s)
	}
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if isWordRune(r) {
			break
		}
		i += size
	}
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !isWordRune(r) {
			break
		}
		i += size
	}
	return i
}

func forwardWordBoundary(s string, i int) int {
	i = nextWordBoundary(s, i)
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if isWordRune(r) {
			break
		}
		i += size
	}
	return i
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
	contentW := width - prefixW
	if contentW < 1 {
		return 0
	}
	rel := x - prefixW
	if rel <= 0 {
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
