package frontend

import (
	"strings"
	"testing"

	"browser/internal/protocol"
)

func configTestTab() (*Tab, *recordSession) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	return loop, s
}

func configTestState() protocol.ConfigState {
	return protocol.ConfigState{Values: []protocol.ConfigValue{
		{Field: protocol.ConfigNoJS, Value: "on"},
		{Field: protocol.ConfigNoPDF, Value: "off"},
		{Field: protocol.ConfigIgnoreBlocks, Value: "off"},
		{Field: protocol.ConfigHome, Value: "about:home"},
		{Field: protocol.ConfigMemLimit, Value: "0"},
		{Field: protocol.ConfigLayoutBudget, Value: "standard"},
		{Field: protocol.ConfigZoom, Value: "100"},
		{Field: protocol.ConfigTextScale, Value: "auto"},
		{Field: protocol.ConfigUserAgent, Value: "MOB3"},
		{Field: protocol.ConfigSearch, Value: "https://search.example/?q=%s"},
		{Field: protocol.ConfigCerts, Value: "system"},
	}}
}

func TestConfigOverlayLoadsAndChangesValues(t *testing.T) {
	loop, s := configTestTab()
	loop.configOverlay.start(loop)
	if !loop.configOverlay.isOpen() {
		t.Fatal("settings window did not open")
	}
	if len(s.cmds) != 2 ||
		s.cmds[0].Body != (protocol.App{Action: protocol.AppFrontendOverlay, Arg: 1}) ||
		s.cmds[1].Body != (protocol.Config{Action: protocol.ConfigGet}) {
		t.Fatalf("open commands = %#v", s.cmds)
	}
	if !loop.configView.Loading || len(loop.configView.Rows) != 11 {
		t.Fatalf("initial view = %+v", loop.configView)
	}

	loop.applyEvent(protocol.Event{Body: configTestState()})
	if loop.configView.Loading || loop.configView.Rows[0].Value != "on" {
		t.Fatalf("loaded view = %+v", loop.configView)
	}
	if got := loop.configView.Rows[5].Value; got != "standard" {
		t.Fatalf("layout budget row = %q", got)
	}

	s.cmds = nil
	loop.handleShellKey(" ")
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.Config{Action: protocol.ConfigSet, Field: protocol.ConfigNoJS, Value: "off"}) {
		t.Fatalf("javascript toggle = %#v", s.cmds)
	}
	if got := loop.configView.Rows[0].Value; got != "off" {
		t.Fatalf("javascript row = %q", got)
	}

	for i := 0; i < 5; i++ {
		loop.handleShellKey("down")
	}
	s.cmds = nil
	loop.handleShellKey(" ")
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.Config{Action: protocol.ConfigSet, Field: protocol.ConfigLayoutBudget, Value: "high"}) {
		t.Fatalf("layout change = %#v", s.cmds)
	}
	if got := loop.configView.Rows[5].Value; got != "high" {
		t.Fatalf("layout row = %q", got)
	}
}

func TestConfigOverlayTextEdit(t *testing.T) {
	loop, s := configTestTab()
	loop.configOverlay.start(loop)
	loop.applyEvent(protocol.Event{Body: configTestState()})
	for i := 0; i < 3; i++ {
		loop.handleShellKey("down")
	}
	s.cmds = nil
	loop.handleShellKey("enter")
	if !loop.configEditing || loop.configEditValue != "about:home" || !loop.configEditAll {
		t.Fatalf("edit state = %v %q %v", loop.configEditing, loop.configEditValue, loop.configEditAll)
	}
	loop.handleShellKey("x")
	if loop.configEditValue != "x" {
		t.Fatalf("typed value = %q", loop.configEditValue)
	}
	loop.handleShellKey("enter")
	if loop.configEditing {
		t.Fatal("edit did not commit")
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.Config{Action: protocol.ConfigSet, Field: protocol.ConfigHome, Value: "x"}) {
		t.Fatalf("home edit = %#v", s.cmds)
	}

	// Esc cancels an edit without sending anything.
	s.cmds = nil
	loop.handleShellKey("enter")
	loop.handleShellKey("y")
	loop.handleShellKey("esc")
	if loop.configEditing || len(s.cmds) != 0 {
		t.Fatalf("esc edit = editing:%v cmds:%#v", loop.configEditing, s.cmds)
	}
	if !loop.configOverlay.isOpen() {
		t.Fatal("Esc during an edit closed the window")
	}
}

func TestConfigOverlayShowsRejectedValue(t *testing.T) {
	loop, _ := configTestTab()
	loop.configOverlay.start(loop)
	state := configTestState()
	state.Err = "Certificate source not changed: system or embedded"
	loop.applyEvent(protocol.Event{Body: state})
	if !strings.Contains(loop.configView.Err, "system or embedded") {
		t.Fatalf("error = %q", loop.configView.Err)
	}
	state.Err = ""
	loop.applyEvent(protocol.Event{Body: state})
	if loop.configView.Err != "" {
		t.Fatalf("stale error = %q", loop.configView.Err)
	}
}

func TestConfigOverlayMouseRowsAndClose(t *testing.T) {
	loop, s := configTestTab()
	loop.configOverlay.start(loop)
	loop.applyEvent(protocol.Event{Body: configTestState()})
	layout := LayoutConfig(loop.State.Page, &loop.configView)
	if layout.BoxW <= 0 || len(layout.Rects) != 11 {
		t.Fatalf("layout = %+v", layout)
	}
	s.cmds = nil
	row := layout.Rects[1]
	loop.handleChromeMouse(MouseEvent{X: row.X + 1, Y: row.Y + 1, Press: true})
	if loop.configIndex != 1 {
		t.Fatalf("clicked row = %d", loop.configIndex)
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.Config{Action: protocol.ConfigSet, Field: protocol.ConfigNoPDF, Value: "on"}) {
		t.Fatalf("row click = %#v", s.cmds)
	}

	loop.handleChromeMouse(MouseEvent{X: 1, Y: 1, Press: true})
	if loop.configOverlay.isOpen() {
		t.Fatal("backdrop click did not close settings")
	}
	if len(s.cmds) != 2 || s.cmds[1].Body != (protocol.App{Action: protocol.AppFrontendOverlay, Arg: 0}) {
		t.Fatalf("close commands = %#v", s.cmds)
	}
}

func TestNavigateAboutConfigOpensSettings(t *testing.T) {
	loop, s := configTestTab()
	loop.navigate("about:config?home=https%3A%2F%2Fexample.com%2F")
	if !loop.configOverlay.isOpen() {
		t.Fatal("about:config did not open settings")
	}
	for _, cmd := range s.cmds {
		if _, ok := cmd.Body.(protocol.Navigate); ok {
			t.Fatalf("about:config was sent to the engine: %#v", s.cmds)
		}
	}

	loop, s = configTestTab()
	loop.handleShellKey("o")
	loop.promptText = "about:config"
	loop.promptCursor = len(loop.promptText)
	loop.handleShellKey("enter")
	if !loop.configOverlay.isOpen() {
		t.Fatal("typed about:config did not open settings")
	}
	for _, cmd := range s.cmds {
		if _, ok := cmd.Body.(protocol.Navigate); ok {
			t.Fatalf("typed about:config was sent to the engine: %#v", s.cmds)
		}
	}
}

func TestOpenConfigEventOpensSettings(t *testing.T) {
	loop, s := configTestTab()
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.OpenConfig{}}); err != nil {
		t.Fatal(err)
	}
	if !loop.configOverlay.isOpen() {
		t.Fatal("OpenConfig did not open settings")
	}
	if len(s.cmds) != 2 || s.cmds[1].Body != (protocol.Config{Action: protocol.ConfigGet}) {
		t.Fatalf("open commands = %#v", s.cmds)
	}
}
