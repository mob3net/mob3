package frontend

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"browser/internal/protocol"
)

type eventSession struct {
	events chan protocol.Event
	closed bool
}

func newEventSession() *eventSession {
	return &eventSession{events: make(chan protocol.Event, 8)}
}

func (s *eventSession) Send(protocol.Command) error { return nil }
func (s *eventSession) Events() <-chan protocol.Event {
	return s.events
}
func (s *eventSession) Abort() {}
func (s *eventSession) Close() error {
	s.closed = true
	return nil
}

func multiTab(id int, title string) *Tab {
	return &Tab{
		Session: newEventSession(),
		Stderr:  io.Discard,
		State: LoopState{Page: protocol.State{
			FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2, Title: title,
		}},
	}
}

func TestSessionLoopRoutesOnlyActiveFrames(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	active := multiTab(1, "one")
	active.Presenter = p
	background := multiTab(2, "two")
	loop := &SessionLoop{Tabs: []*Tab{active, background}, Active: active, Presenter: p}
	loop.syncTabBar()
	out.Reset()

	if _, err := loop.handleEvent(background, protocol.Event{Body: protocol.Frame{
		W: 2, H: 1, Cells: []protocol.Cell{{R: 'Z'}, {R: 'Z'}},
	}}); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("background frame was drawn: %q", out.String())
	}

	if _, err := loop.handleEvent(active, protocol.Event{Body: protocol.Frame{
		W: 2, H: 1, Cells: []protocol.Cell{{R: 'A'}, {R: 'B'}},
	}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "A") {
		t.Fatalf("active frame was not drawn: %q", out.String())
	}

	out.Reset()
	if _, err := loop.handleEvent(background, protocol.Event{Body: protocol.State{
		Width: 80, Height: 24, HeaderRows: 2, FrontendChrome: true,
		Title: "changed", URL: "https://example.test/bg", Scroll: 7,
	}}); err != nil {
		t.Fatal(err)
	}
	if background.State.Page.Title != "changed" || background.State.Scroll != 7 {
		t.Fatalf("background state = %+v scroll=%d", background.State.Page, background.State.Scroll)
	}
	if out.Len() != 0 {
		t.Fatalf("background state drew a frame: %q", out.String())
	}
}

func TestActivateTabMovesThePresenter(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	first := multiTab(1, "one")
	second := multiTab(2, "two")
	first.Presenter = p
	loop := &SessionLoop{Tabs: []*Tab{first, second}, Active: first, Presenter: p}
	loop.syncTabBar()

	loop.activateTab(1)
	if loop.Active != second {
		t.Fatal("second tab is not active")
	}
	if first.Presenter != nil {
		t.Fatal("first tab kept the presenter")
	}
	if second.Presenter != p {
		t.Fatal("second tab did not receive the presenter")
	}
	if p.state.Title != "two" {
		t.Fatalf("presenter state = %q, want the second tab", p.state.Title)
	}
	if len(loop.lastTabs) != 2 || !loop.lastTabs[1].Active || loop.lastTabs[0].Active {
		t.Fatalf("tab bar state = %+v", loop.lastTabs)
	}
}

func TestBackgroundOverlayAppearsAfterSwitch(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	active := multiTab(1, "one")
	active.Presenter = p
	background := multiTab(2, "two")
	background.State.Page.HeaderRows = 2
	loop := &SessionLoop{Tabs: []*Tab{active, background}, Active: active, Presenter: p}
	loop.syncTabBar()

	background.startDialog(protocol.Dialog{ID: 3, Kind: protocol.DialogAlert, Text: "background alert"})
	if !background.dialogActive {
		t.Fatal("background dialog was not recorded")
	}
	if out.Len() != 0 {
		t.Fatalf("background dialog drew: %q", out.String())
	}
	loop.activateTab(1)
	if !strings.Contains(out.String(), "background alert") {
		t.Fatalf("dialog missing after switch: %q", out.String())
	}
}

func TestRemoveTabKeepsTheRemainingActiveTab(t *testing.T) {
	first := multiTab(1, "one")
	second := multiTab(2, "two")
	loop := &SessionLoop{Tabs: []*Tab{first, second}, Active: first, Presenter: NewPresenter(io.Discard, true)}
	loop.syncTabBar()
	loop.removeTab(second)
	if len(loop.Tabs) != 1 || loop.Active != first {
		t.Fatalf("after remove: tabs=%d active=%v", len(loop.Tabs), loop.Active.BarView(true).Title)
	}
	if second.Session != nil {
		t.Fatal("removed tab kept its session")
	}
}
