// Package movekeys maps movement inputs to relative commands. The page and
// console share the viewport deltas; list overlays use the line and top/bottom
// subset for selection. Standard library only.
package movekeys

// WheelLines is the number of lines one wheel notch scrolls.
const WheelLines = 3

// WheelDelta converts a decoded wheel direction (-1 up, +1 down) into a
// signed line delta.
func WheelDelta(wheel int) int {
	if wheel < 0 {
		return -WheelLines
	}
	if wheel > 0 {
		return WheelLines
	}
	return 0
}

// Move is one viewport movement command.
type Move uint8

const (
	LineUp Move = iota + 1
	LineDown
	HalfUp
	HalfDown
	PageUp
	PageDown
	Top
	Bottom
)

// Lookup returns the movement bound to a key token.
func Lookup(key string) (Move, bool) {
	switch key {
	case "j", "down", "ctrl-n":
		return LineDown, true
	case "k", "up", "ctrl-p":
		return LineUp, true
	case "d", "ctrl-d":
		return HalfDown, true
	case "u", "ctrl-u":
		return HalfUp, true
	case " ", "space", "pgdn", "ctrl-v":
		return PageDown, true
	case "b", "pgup":
		return PageUp, true
	case "g", "home":
		return Top, true
	case "G", "end":
		return Bottom, true
	}
	return 0, false
}

// Delta returns the signed line delta for a line, half-page or page move over
// rows visible rows. It reports false for Top and Bottom, which the caller
// resolves against its own total.
func (m Move) Delta(rows int) (int, bool) {
	switch m {
	case LineUp:
		return -1, true
	case LineDown:
		return 1, true
	case HalfUp:
		return -(rows / 2), true
	case HalfDown:
		return rows / 2, true
	case PageUp:
		return -max(rows-1, 0), true
	case PageDown:
		return max(rows-1, 0), true
	}
	return 0, false
}
