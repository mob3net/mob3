package frontend

import "browser/internal/movekeys"

// moveList applies the line and top/bottom subset of the shared movement keys
// to a list index. Half-page and page keys stay unhandled so overlays can keep
// Space as choose/toggle.
func moveList(key string, index, count int) (int, bool) {
	m, ok := movekeys.Lookup(key)
	if !ok {
		return index, false
	}
	switch m {
	case movekeys.LineUp:
		return moveChoice(index, -1, count), true
	case movekeys.LineDown:
		return moveChoice(index, 1, count), true
	case movekeys.Top:
		return moveChoice(index, -index, count), true
	case movekeys.Bottom:
		return moveChoice(index, count-1-index, count), true
	}
	return index, false
}

// moveChoice shifts a selection by delta and clamps it to the list.
func moveChoice(index, delta, count int) int {
	if count < 1 {
		return 0
	}
	index += delta
	if index < 0 {
		return 0
	}
	if index >= count {
		return count - 1
	}
	return index
}
