package app

import (
	"io"
	"strings"

	"browser/internal/frontend"
)

// Terminal control sequences used around the interactive session. Keeping them
// named stops the alternate-screen, mouse and kitty-keyboard modes from drifting
// apart between the normal exit path and the early-boot failure path.
const (
	termEnterAltScreen = "\x1b[?1049h"
	termHideCursor     = "\x1b[?25l"
	termDisableWrap    = "\x1b[?7l"
	termClearScreen    = "\x1b[2J"
	termEnableMouse    = "\x1b[?1000h\x1b[?1003h\x1b[?1006h"
	termKittyKeysOn    = "\x1b[>19u"
	termKittyKeysOff   = "\x1b[<u"
	termDisableMouse   = "\x1b[?1003l\x1b[?1000l\x1b[?1006l"
	termResetAndLeave  = "\x1b[0m\x1b[?7h\x1b[?25h\x1b[?1049l"
)

func enterTerminal(kittyKeys bool) string {
	seq := termEnterAltScreen + termHideCursor + termDisableWrap + termClearScreen + termEnableMouse
	if kittyKeys {
		seq += termKittyKeysOn
	}
	return seq
}

// leaveTerminal restores the terminal in the reverse order. Presenter-owned
// native image placements are cleared before the alternate screen is left.
func leaveTerminal(out io.Writer, kittyKeys bool, presenter *frontend.Presenter) {
	var sb strings.Builder
	if kittyKeys {
		sb.WriteString(termKittyKeysOff)
	}
	sb.WriteString(termDisableMouse)
	presenter.ClearGraphics(&sb)
	sb.WriteString(termResetAndLeave)
	_, _ = io.WriteString(out, sb.String())
}
