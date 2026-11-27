package frontend

import (
	"testing"

	"browser/internal/protocol"
)

func TestLayoutPanesFullScreen(t *testing.T) {
	panes := layoutPanes(80, 24)
	want := paneRect{W: 80, H: 24}
	if panes.page != want || panes.console != want {
		t.Fatalf("panes = %+v %+v, want %+v", panes.page, panes.console, want)
	}
	l := &Tab{State: LoopState{Page: protocol.State{Width: 80, Height: 24}}}
	if got := l.paneFor(surfacePage); got != want {
		t.Fatalf("page pane = %+v", got)
	}
	if got := l.paneFor(surfaceConsole); got != want {
		t.Fatalf("console pane = %+v", got)
	}
}

func TestPaneRectContainsAndCovers(t *testing.T) {
	full := paneRect{W: 80, H: 24}
	if full.empty() || !full.contains(0, 0) || !full.contains(79, 23) ||
		full.contains(80, 23) || full.contains(0, 24) {
		t.Fatalf("full pane bounds = %+v", full)
	}
	if !full.covers(80, 24) || full.covers(81, 24) {
		t.Fatalf("full pane coverage = %+v", full)
	}
	split := paneRect{X: 40, Y: 0, W: 40, H: 24}
	if split.contains(39, 0) || !split.contains(40, 0) || split.covers(80, 24) {
		t.Fatalf("split pane bounds = %+v", split)
	}
}
