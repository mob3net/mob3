package frontend

import (
	"fmt"
	"testing"

	"browser/internal/protocol"
)

// inputRoute is the owner that consumed one input event. The test keeps the
// matrix deliberately small: page/engine, console, a shell/browser action, or
// an overlay that consumed the event itself.
type inputRoute string

const (
	routeEngine  inputRoute = "engine"
	routeConsole inputRoute = "console"
	routeShell   inputRoute = "shell"
	routeOverlay inputRoute = "overlay"
	routeNone    inputRoute = "none"
)

type inputSnapshot struct {
	prompt      bool
	promptText  string
	findText    string
	menuIndex   int
	dialog      bool
	consolePane int
}

type inputHarness struct {
	tab     *Tab
	session *recordSession
	calls   []string
}

func newInputHarness() *inputHarness {
	h := &inputHarness{session: &recordSession{}}
	h.tab = &Tab{Session: h.session}
	h.tab.State.Page = protocol.State{
		FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2,
	}
	h.tab.Actions = TabActions{
		Cycle: func(delta int) { h.calls = append(h.calls, fmt.Sprintf("cycle:%d", delta)) },
	}
	return h
}

func (h *inputHarness) snapshot() inputSnapshot {
	return inputSnapshot{
		prompt:      h.tab.promptOverlay.isOpen(),
		promptText:  h.tab.promptText,
		findText:    h.tab.findOverlay.find.text,
		menuIndex:   h.tab.menuOverlay.menuIndex,
		dialog:      h.tab.dialogOverlay.isOpen(),
		consolePane: h.tab.consoleOverlay.consolePane,
	}
}

func (h *inputHarness) route(before inputSnapshot) inputRoute {
	if len(h.calls) > 0 {
		return routeShell
	}
	for _, cmd := range h.session.cmds {
		switch cmd.Body.(type) {
		case protocol.ConsoleAction:
			return routeConsole
		case protocol.Find:
			return routeOverlay
		case protocol.Key:
			return routeEngine
		}
	}
	if h.tab.dialogOverlay.isOpen() && before.dialog {
		return routeOverlay
	}
	if h.tab.menuOverlay.isOpen() && h.tab.menuOverlay.menuIndex != before.menuIndex {
		return routeOverlay
	}
	if h.tab.promptOverlay.isOpen() {
		if !before.prompt {
			return routeShell
		}
		return routeOverlay
	}
	if h.tab.consoleOverlay.isOpen() && h.tab.consoleOverlay.consolePane != before.consolePane {
		return routeConsole
	}
	return routeNone
}

func TestInputRoutingMatrix(t *testing.T) {
	consoleSetup := func(h *inputHarness) {
		h.tab.consoleOverlay.open(h.tab)
		h.tab.consoleView = protocol.ConsoleView{
			Width: 80, Height: 24, Pane: consolePaneLogs,
			Tabs: []string{" JS logs ", " Requests ", " Storage ", " HTML ", " Tools "},
		}
		h.session.cmds = nil
	}
	inputSetup := func(h *inputHarness) {
		h.tab.State.Page.KeysEngine = true
	}
	promptSetup := func(h *inputHarness) {
		h.tab.promptOverlay.start(h.tab, false)
		h.session.cmds = nil
	}
	findSetup := func(h *inputHarness) {
		h.tab.findOverlay.start(h.tab)
		h.session.cmds = nil
	}
	dialogSetup := func(h *inputHarness) {
		_, _ = h.tab.applyEvent(protocol.Event{Body: protocol.Dialog{
			ID: 1, Kind: protocol.DialogAlert, Text: "hello",
		}})
		h.session.cmds = nil
	}
	menuSetup := func(h *inputHarness) {
		h.tab.menuOverlay.start(h.tab, protocol.MenuView)
		h.session.cmds = nil
	}
	pageClick := MouseToken(MouseEvent{X: 5, Y: 5, Press: true})
	consoleClick := MouseToken(MouseEvent{X: 10, Y: 1, Press: true})

	cases := []struct {
		name  string
		setup func(*inputHarness)
		event string
		want  inputRoute
	}{
		{name: "page j reaches engine", event: "j", want: routeEngine},
		{name: "page h cycles tab", event: "h", want: routeShell},
		{name: "page l cycles tab", event: "l", want: routeShell},
		{name: "page o opens prompt", event: "o", want: routeShell},
		{name: "page bang opens console", event: "!", want: routeConsole},
		{name: "page click reaches engine", event: pageClick, want: routeEngine},

		{name: "console k scrolls console", setup: consoleSetup, event: "k", want: routeConsole},
		{name: "console h changes section", setup: consoleSetup, event: "h", want: routeConsole},
		{name: "console bang closes console", setup: consoleSetup, event: "!", want: routeConsole},
		{name: "console click selects section", setup: consoleSetup, event: consoleClick, want: routeConsole},

		{name: "page input j reaches engine", setup: inputSetup, event: "j", want: routeEngine},
		{name: "page input o remains typing", setup: inputSetup, event: "o", want: routeEngine},
		{name: "page input h remains typing", setup: inputSetup, event: "h", want: routeEngine},

		{name: "prompt o edits URL", setup: promptSetup, event: "o", want: routeOverlay},
		{name: "find x searches", setup: findSetup, event: "x", want: routeOverlay},
		{name: "dialog bang stays modal", setup: dialogSetup, event: "!", want: routeOverlay},
		{name: "menu j moves selection", setup: menuSetup, event: "j", want: routeOverlay},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newInputHarness()
			if tc.setup != nil {
				tc.setup(h)
			}
			before := h.snapshot()
			if _, err := h.tab.handleInput(tc.event); err != nil {
				t.Fatal(err)
			}
			if got := h.route(before); got != tc.want {
				t.Fatalf("route = %q, want %q; commands=%#v calls=%v", got, tc.want, h.session.cmds, h.calls)
			}
		})
	}
}
