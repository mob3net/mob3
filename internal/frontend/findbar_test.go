package frontend

import (
	"testing"

	"browser/internal/protocol"
)

func TestFindBarOpenEditAndStep(t *testing.T) {
	s := &recordSession{}
	l := &Tab{Session: s}
	var f findBar
	f.open(l, protocol.FindHTML)
	if f.text != "" || f.cursor != 0 {
		t.Fatalf("open reset = %q/%d", f.text, f.cursor)
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.Find{Scope: protocol.FindHTML, Query: ""}) {
		t.Fatalf("open command = %#v", s.cmds)
	}
	s.cmds = nil
	for _, r := range "ab" {
		action, changed := f.handleKey(l, string(r), protocol.FindHTML)
		if action != findKeyEdit || !changed {
			t.Fatalf("%q action=%v changed=%v", r, action, changed)
		}
	}
	if f.text != "ab" || f.cursor != 2 {
		t.Fatalf("text = %q cursor=%d", f.text, f.cursor)
	}
	if len(s.cmds) != 2 || s.cmds[1].Body != (protocol.Find{Scope: protocol.FindHTML, Query: "ab"}) {
		t.Fatalf("edit commands = %#v", s.cmds)
	}
	s.cmds = nil
	if action, _ := f.handleKey(l, "down", protocol.FindHTML); action != findKeyNext {
		t.Fatalf("down action = %v", action)
	}
	if action, _ := f.handleKey(l, "up", protocol.FindHTML); action != findKeyPrev {
		t.Fatalf("up action = %v", action)
	}
	if len(s.cmds) != 2 || s.cmds[0].Body != (protocol.Find{Scope: protocol.FindHTML, Query: "ab", Next: true}) ||
		s.cmds[1].Body != (protocol.Find{Scope: protocol.FindHTML, Query: "ab", Prev: true}) {
		t.Fatalf("step commands = %#v", s.cmds)
	}
}

func TestFindBarCtrlAAndCloseKeys(t *testing.T) {
	s := &recordSession{}
	l := &Tab{Session: s}
	f := findBar{text: "ab", cursor: 2}
	if action, changed := f.handleKey(l, "ctrl-a", protocol.FindPage); action != findKeyEdit || changed {
		t.Fatalf("ctrl-a action=%v changed=%v", action, changed)
	}
	if f.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", f.cursor)
	}
	for _, key := range []string{"esc", "enter", "tab", "backtab"} {
		if action, _ := f.handleKey(l, key, protocol.FindPage); action != findKeyClose {
			t.Errorf("%q action = %v, want close", key, action)
		}
	}
	if len(s.cmds) != 0 {
		t.Fatalf("close keys sent commands: %#v", s.cmds)
	}
}
