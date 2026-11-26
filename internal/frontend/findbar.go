package frontend

import (
	"browser/internal/protocol"
	"browser/internal/textedit"
)

// findBarMaxRunes bounds one find query.
const findBarMaxRunes = 512

// findBar is the shared query, caret and key handling for the page find bar and
// the console HTML find bar. The engine owns matching; the caller owns the bar,
// results display and scrolling.
type findBar struct {
	text   string
	cursor int
}

// findKeyAction reports what a find-bar key asked the caller to do.
type findKeyAction uint8

const (
	findKeyEdit findKeyAction = iota
	findKeyClose
	findKeyNext
	findKeyPrev
)

// reset clears the query and caret without sending a command.
func (f *findBar) reset() {
	f.text = ""
	f.cursor = 0
}

// open starts a fresh find in scope and clears the bar.
func (f *findBar) open(l *Tab, scope protocol.FindScope) {
	f.reset()
	_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: scope, Query: ""}})
}

// step asks the engine for the next or previous match.
func (f *findBar) step(l *Tab, scope protocol.FindScope, next bool) {
	_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: scope, Query: f.text, Next: next, Prev: !next}})
}

// handleKey edits the query or reports a navigation/close action. It sends the
// query command itself and reports whether the text changed.
func (f *findBar) handleKey(l *Tab, k string, scope protocol.FindScope) (findKeyAction, bool) {
	switch k {
	case "esc", "enter", "tab", "backtab":
		return findKeyClose, false
	case "down":
		f.step(l, scope, true)
		return findKeyNext, false
	case "up":
		f.step(l, scope, false)
		return findKeyPrev, false
	}
	if k == "ctrl-a" {
		k = "home"
	}
	text, cur, _ := textedit.EditLineKey(f.text, f.cursor, k, textedit.EditConfig{MaxRunes: findBarMaxRunes})
	changed := text != f.text
	f.text = text
	if cur > len(text) {
		cur = len(text)
	}
	f.cursor = cur
	if changed {
		_ = l.Session.Send(protocol.Command{Body: protocol.Find{Scope: scope, Query: text}})
	}
	return findKeyEdit, changed
}
