package engine

import (
	"unicode"
	"unicode/utf8"
)

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

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// prevWordBoundary returns the start of the word before i, skipping any
// separators immediately before the caret. Deleting [start, i) therefore
// removes one URL segment while keeping the separators before it.
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

// nextWordBoundary returns the end of the word at or after i, skipping leading
// separators first. It is used for forward word motion.
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

// forwardWordBoundary returns the end of the next word including the
// separators that follow it, matching kill-word behavior.
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

// editConfig selects the per-control differences for editLineKey.
type EditConfig struct {
	// SelAll, when non-nil, enables Ctrl-A select-all: the next edit replaces
	// the whole value and a caret move clears the selection. Controls without a
	// selection model leave it nil.
	SelAll *bool
	// collapseSelection makes a caret move with a full selection jump to the
	// edge of the value instead of moving from the caret.
	CollapseSelection bool
	// maxRunes refuses new input once the value already holds that many runes;
	// 0 means unlimited.
	MaxRunes int
}

// editLineKey applies one text-editing key to a value and its caret. handled is
// false for keys the control owns (Enter, Escape, Tab, Up/Down, history) and
// for unprintable input, so the caller can run those itself. Visible-window and
// event side effects stay with the caller.
func EditLineKey(value string, caret int, k string, cfg EditConfig) (string, int, bool) {
	if caret < 0 {
		caret = 0
	}
	if caret > len(value) {
		caret = len(value)
	}
	all := cfg.SelAll != nil && *cfg.SelAll
	clearSelection := func() {
		if cfg.SelAll != nil {
			*cfg.SelAll = false
		}
	}
	at := func(n int) (string, int, bool) {
		clearSelection()
		return value, n, true
	}
	switch k {
	case "ctrl-a":
		if cfg.SelAll == nil {
			return value, caret, false
		}
		*cfg.SelAll = true
		return value, len(value), true
	case "left", "ctrl-b":
		if all && cfg.CollapseSelection {
			return at(0)
		}
		return at(prevRuneBoundary(value, caret))
	case "right", "ctrl-f":
		if all && cfg.CollapseSelection {
			return at(len(value))
		}
		return at(nextRuneBoundary(value, caret))
	case "alt-b", "ctrl-left", "alt-left":
		if all && cfg.CollapseSelection {
			return at(0)
		}
		return at(prevWordBoundary(value, caret))
	case "alt-f", "ctrl-right", "alt-right":
		if all && cfg.CollapseSelection {
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
			if cfg.MaxRunes > 0 && utf8.RuneCountInString(value) >= cfg.MaxRunes {
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
