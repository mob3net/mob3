// Package movekeys maps viewport movement key tokens to relative commands.
// The page and the console share the same Vim-style vocabulary and step
// formulas; each caller supplies its own visible row count. Standard library
// only.
package movekeys

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
