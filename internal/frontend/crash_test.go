package frontend

import (
	"bytes"
	"image"
	"strings"
	"testing"

	"browser/internal/protocol"
)

func TestCrashedTabPresentsErrorView(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, false)
	tab := multiTab(1, "one")
	tab.State.Page.URL = "https://example.test/page"
	tab.Presenter = p
	p.SetState(tab.State.Page)

	loop := &SessionLoop{Tabs: []*Tab{tab}, Active: tab, Presenter: p}
	loop.markDead(tab, "page engine panic: boom")

	if !tab.Dead || tab.Session != nil {
		t.Fatalf("crashed tab = dead %v session %v", tab.Dead, tab.Session)
	}
	got := out.String()
	for _, want := range []string{
		"Page engine stopped",
		"page engine panic: boom",
		"Page: https://example.test/page",
		"t: new tab",
		"W: close tab",
		"q: quit",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("crash view missing %q:\n%s", want, got)
		}
	}
}

func TestChildExitDetailReplacesGenericSessionEnd(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, false)
	tab := multiTab(1, "one")
	tab.State.Page.URL = "https://example.test/page"
	tab.Presenter = p
	p.SetState(tab.State.Page)
	loop := &SessionLoop{Tabs: []*Tab{tab}, Active: tab, Presenter: p}

	loop.markDead(tab, sessionEndedMessage)
	if tab.CrashMessage != sessionEndedMessage {
		t.Fatalf("initial crash = %q", tab.CrashMessage)
	}
	out.Reset()
	detail := "exit status 2: fatal error: stack overflow"
	loop.markDead(tab, detail)
	if tab.CrashMessage != detail {
		t.Fatalf("updated crash = %q", tab.CrashMessage)
	}
	if !strings.Contains(out.String(), "fatal error: stack overflow") {
		t.Fatalf("updated crash view missing detail:\n%s", out.String())
	}

	// A late stream-close report must not replace the specific exit detail.
	loop.markDead(tab, sessionEndedMessage)
	if tab.CrashMessage != detail {
		t.Fatalf("generic report replaced detail: %q", tab.CrashMessage)
	}
}

func TestCrashViewScrollsLongError(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, false)
	tab := multiTab(1, "one")
	tab.Presenter = p
	p.SetState(tab.State.Page)
	tab.MarkDead("first " + strings.Repeat("word ", 1200) + "last")

	if tab.crashScroll != 0 {
		t.Fatalf("initial crash scroll = %d", tab.crashScroll)
	}
	if !strings.Contains(out.String(), "first") {
		t.Fatalf("first crash line missing:\n%s", out.String())
	}
	if strings.Contains(out.String(), "last") {
		t.Fatal("long crash text was not clipped to the viewport")
	}

	out.Reset()
	if quit, err := tab.handleDeadInput("end"); err != nil || quit {
		t.Fatalf("end on crash view: quit=%v err=%v", quit, err)
	}
	if tab.crashScroll == 0 {
		t.Fatal("end did not scroll the crash view")
	}
	if !strings.Contains(out.String(), "last") {
		t.Fatalf("last crash line missing after end:\n%s", out.String())
	}

	out.Reset()
	if quit, err := tab.handleDeadInput("home"); err != nil || quit {
		t.Fatalf("home on crash view: quit=%v err=%v", quit, err)
	}
	if tab.crashScroll != 0 || !strings.Contains(out.String(), "first") {
		t.Fatalf("home did not return to the top: scroll=%d", tab.crashScroll)
	}
}

func TestDeadTabKeepsTabCycleKeys(t *testing.T) {
	tab := multiTab(1, "one")
	tab.Dead = true
	got := 0
	tab.Actions.Cycle = func(delta int) { got = delta }
	if quit, err := tab.handleDeadInput("ctrl-n"); err != nil || quit {
		t.Fatalf("ctrl-n on dead tab: quit=%v err=%v", quit, err)
	}
	if got != 1 {
		t.Fatalf("ctrl-n cycle = %d, want 1", got)
	}
	if tab.crashScroll != 0 {
		t.Fatalf("ctrl-n scrolled the crash view to %d", tab.crashScroll)
	}
}

func TestCrashViewDropsNativePlacements(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	p.SetGraphics(GraphicsKitty, 8, 16)
	st := protocol.State{FrontendChrome: true, Width: 20, Height: 7, HeaderRows: 1}
	p.SetState(st)
	f := protocol.Frame{W: 20, H: 5, Cells: make([]protocol.Cell, 100)}
	for i := range f.Cells {
		f.Cells[i].R = ' '
	}
	f.Placements = []protocol.Placement{
		testPlacement("pic.png", image.NewRGBA(image.Rect(0, 0, 16, 16)), 1, 1, 2, 1, 0, 1),
	}
	if err := p.Present(f, 0, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b_Ga=p") {
		t.Fatalf("native placement missing: %.120q", out.String())
	}
	out.Reset()
	p.SetCrash(&CrashView{Message: "boom"})
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b_Ga=p") {
		t.Fatalf("native placement drawn over the crash view: %.120q", out.String())
	}
	if !strings.Contains(out.String(), "boom") {
		t.Fatalf("crash text missing from repaint: %.200q", out.String())
	}
}

func TestBackgroundCrashShowsViewWhenSelected(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, false)
	tab := multiTab(1, "one")
	tab.State.Page.URL = "about:home"

	loop := &SessionLoop{Tabs: []*Tab{tab}}
	loop.markDead(tab, "protocol session ended")
	if !tab.Dead || tab.Presenter != nil {
		t.Fatalf("background crash = dead %v presenter %v", tab.Dead, tab.Presenter)
	}

	tab.AttachPresenter(p)
	got := out.String()
	for _, want := range []string{
		"Page engine stopped",
		"protocol session ended",
		"Page: about:home",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("attached crash view missing %q:\n%s", want, got)
		}
	}
}
