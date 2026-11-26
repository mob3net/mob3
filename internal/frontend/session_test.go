package frontend

import (
	"bytes"
	"image"
	"io"
	"strings"
	"testing"

	"browser/internal/protocol"
	"browser/internal/textwidth"
)

func TestApplySessionEventPresentsNativeFrame(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	p.SetGraphics(GraphicsKitty, 8, 16)
	loop := &Tab{Presenter: p, Stderr: io.Discard}
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.State{Flags: protocol.StateNativeImages}}); err != nil {
		t.Fatal(err)
	}
	data, err := encodePNG(image.NewNRGBA(image.Rect(0, 0, 16, 16)))
	if err != nil {
		t.Fatal(err)
	}
	f := protocol.Frame{W: 8, H: 3, Cells: make([]protocol.Cell, 24), Placements: []protocol.Placement{{
		Key: "1", W: 2, H: 1, TotalRows: 1, Format: protocol.PlacementPNG, Data: data,
	}}}
	for i := range f.Cells {
		f.Cells[i].R = 'x'
	}
	if _, err := loop.applyEvent(protocol.Event{Body: f}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "x") {
		t.Fatal("native frame text was not presented")
	}
	if !strings.Contains(got, "\x1b_G") {
		t.Fatal("native placement was not presented")
	}
}

func TestApplySessionEventDeduplicatesCrash(t *testing.T) {
	p := NewPresenter(io.Discard, false)
	loop := &Tab{Presenter: p, Stderr: io.Discard}
	var reports []string
	report := func(msg string) { reports = append(reports, msg) }
	loop.Report = report
	crash := protocol.Event{Body: protocol.Crash{Message: "bad\x1b[31m"}}
	if _, err := loop.applyEvent(crash); err != nil {
		t.Fatal(err)
	}
	if _, err := loop.applyEvent(crash); err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 {
		t.Fatalf("reports = %d, want 1 for repeated crash", len(reports))
	}
	if strings.Contains(reports[0], "\x1b") {
		t.Fatalf("report kept a control sequence: %q", reports[0])
	}
	frame := protocol.Frame{W: 1, H: 1, Cells: []protocol.Cell{{R: 'x'}}}
	if _, err := loop.applyEvent(protocol.Event{Body: frame}); err != nil {
		t.Fatal(err)
	}
	if _, err := loop.applyEvent(crash); err != nil {
		t.Fatal(err)
	}
	if len(reports) != 2 {
		t.Fatalf("reports = %d, want 2 after a good frame re-armed reporting", len(reports))
	}
}

func TestApplySessionEventCrashForcesFullRepaint(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	loop := &Tab{Presenter: p, Stderr: io.Discard}
	f := protocol.Frame{W: 2, H: 2, Cells: []protocol.Cell{{R: 'a'}, {R: 'b'}, {R: 'c'}, {R: 'd'}}}
	if _, err := loop.applyEvent(protocol.Event{Body: f}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.Crash{Message: "boom"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := loop.applyEvent(protocol.Event{Body: f}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b[2J") {
		t.Fatalf("frame after Crash did not clear the screen: %q", out.String())
	}
}

func TestPresenterRepaintsAfterConsoleFrame(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	loop := &Tab{Presenter: p, Stderr: io.Discard}
	page := protocol.Frame{W: 2, H: 2, Cells: []protocol.Cell{{R: 'a'}, {R: 'b'}, {R: 'p'}, {R: 'q'}}}
	console := protocol.Frame{W: 2, H: 2, Cells: []protocol.Cell{{R: 'x'}, {R: 'y'}, {R: 'c'}, {R: 'd'}}}
	for _, f := range []protocol.Frame{page, console} {
		if _, err := loop.applyEvent(protocol.Event{Body: f}); err != nil {
			t.Fatal(err)
		}
	}
	out.Reset()
	if _, err := loop.applyEvent(protocol.Event{Body: page}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "p") {
		t.Fatalf("page was not repainted after the console: %q", out.String())
	}
}

func TestShellKeysQuitAndActions(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s, Presenter: NewPresenter(io.Discard, false)}
	loop.State.Page = protocol.State{FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	if handled, quit := loop.handleShellKey("q"); !handled || quit || !loop.quitPending {
		t.Fatalf("q: handled=%v quit=%v pending=%v", handled, quit, loop.quitPending)
	}
	if handled, quit := loop.handleShellKey("n"); !handled || quit || loop.quitPending {
		t.Fatalf("cancel: handled=%v quit=%v pending=%v", handled, quit, loop.quitPending)
	}
	if handled, quit := loop.handleShellKey("q"); !handled || quit {
		t.Fatal("q did not reopen quit")
	}
	if handled, quit := loop.handleShellKey("y"); !handled || !quit {
		t.Fatal("y did not confirm quit")
	}
}

func TestQuitPromptDismissesOnAnyClick(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s, Presenter: NewPresenter(io.Discard, false)}
	loop.State.Page = protocol.State{FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	for _, button := range []int{MouseButtonLeft, MouseButtonMiddle, MouseButtonRight} {
		loop.startQuit()
		if !loop.quitPending {
			t.Fatal("quit prompt did not open")
		}
		if !loop.handleChromeMouse(MouseEvent{Button: button, X: 40, Y: 12, Press: true}) {
			t.Fatalf("button %d click was not consumed", button)
		}
		if loop.quitPending {
			t.Fatalf("button %d click did not dismiss the quit prompt", button)
		}
	}
}

func TestShellKeysSendAppActions(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	cases := map[string]any{
		"r":      protocol.App{Action: protocol.AppReload},
		"ctrl-r": protocol.App{Action: protocol.AppReload},
		"R":      protocol.App{Action: protocol.AppToggleReader},
		"D":      protocol.App{Action: protocol.AppToggleDark},
		"+":      protocol.App{Action: protocol.AppZoomIn},
		"-":      protocol.App{Action: protocol.AppZoomOut},
		"0":      protocol.App{Action: protocol.AppZoomReset},
		"J":      protocol.App{Action: protocol.AppToggleJS},
		"i":      protocol.App{Action: protocol.AppToggleImages},
		"E":      protocol.App{Action: protocol.AppStatusDetails},
		"S":      protocol.App{Action: protocol.AppDownload},
		"?":      protocol.Navigate{URL: "about:help", Push: true},
	}
	for key, want := range cases {
		s.cmds = nil
		if handled, quit := loop.handleShellKey(key); !handled || quit {
			t.Fatalf("%q not handled", key)
		}
		if len(s.cmds) != 1 || s.cmds[0].Body != want {
			t.Fatalf("%q sent %#v, want %#v", key, s.cmds, want)
		}
	}
}

func TestShellKeyHistory(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2, Scroll: 4}
	loop.State.Scroll = 4
	loop.history = []historyEntry{
		{id: 1, url: "https://example.com/a", scroll: 2},
		{id: 2, url: "https://example.com/b"},
	}
	loop.historyIndex = 1

	// H opens the front-end history overlay without navigating.
	if handled, quit := loop.handleShellKey("H"); !handled || quit {
		t.Fatal("H not handled")
	}
	if !loop.historyActive || len(loop.historyView.entries) != 2 {
		t.Fatalf("history overlay active=%v rows=%d", loop.historyActive, len(loop.historyView.entries))
	}
	if len(s.cmds) != 0 {
		t.Fatalf("H sent commands = %#v", s.cmds)
	}
	if handled, quit := loop.handleShellKey("esc"); !handled || quit || loop.historyActive {
		t.Fatal("Esc did not close the history overlay")
	}

	// Ctrl-H is the same binding.
	if handled, quit := loop.handleShellKey("ctrl-h"); !handled || quit || !loop.historyActive {
		t.Fatal("Ctrl-H did not open the history overlay")
	}
	if handled, quit := loop.handleShellKey("esc"); !handled || quit || loop.historyActive {
		t.Fatal("Esc did not close the history overlay opened by Ctrl-H")
	}

	// Back records the current scroll and restores the target entry.
	s.cmds = nil
	if handled, quit := loop.handleShellKey("ctrl-o"); !handled || quit {
		t.Fatal("ctrl-o not handled")
	}
	if loop.historyIndex != 0 || loop.history[1].scroll != 4 {
		t.Fatalf("index=%d history=%+v", loop.historyIndex, loop.history)
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.Restore{EntryID: 1, URL: "https://example.com/a", Scroll: 2}) {
		t.Fatalf("back command = %#v", s.cmds)
	}
	s.cmds = nil
	if handled, quit := loop.handleShellKey("]"); !handled || quit {
		t.Fatal("] not handled")
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.Restore{EntryID: 2, URL: "https://example.com/b", Scroll: 4}) {
		t.Fatalf("forward command = %#v", s.cmds)
	}

	// Boundary keys stay local and do not send a command.
	s.cmds = nil
	loop.historyIndex = 0
	if handled, quit := loop.handleShellKey("["); !handled || quit {
		t.Fatal("[ not handled")
	}
	if len(s.cmds) != 0 || loop.State.Page.Status != "No previous page" {
		t.Fatalf("boundary back = %#v status=%q", s.cmds, loop.State.Page.Status)
	}
}

func TestShellKeysStayWithEngineWhenCaptured(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{KeysEngine: true, FrontendChrome: true}
	for _, key := range []string{"q", "o", "r", "?"} {
		if handled, _ := loop.handleShellKey(key); handled {
			t.Fatalf("%q was consumed by the front end", key)
		}
	}
	if len(s.cmds) != 0 {
		t.Fatalf("commands = %#v", s.cmds)
	}
}

func TestScrollbarMouseSendsScroll(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{
		FrontendChrome: true, Width: 20, Height: 10, HeaderRows: 1,
		MaxScroll: 100,
	}
	if !loop.handleChromeMouse(MouseEvent{Button: 0, X: 20, Y: 5, Press: true}) {
		t.Fatal("scrollbar press not consumed")
	}
	if len(s.cmds) != 1 {
		t.Fatalf("commands = %d", len(s.cmds))
	}
	sc, ok := s.cmds[0].Body.(protocol.Scroll)
	if !ok || !sc.HasTo || sc.To < 0 || sc.To > 100 {
		t.Fatalf("scroll command = %#v", s.cmds[0].Body)
	}
	s.cmds = nil
	if !loop.handleChromeMouse(MouseEvent{Button: 0, X: 20, Y: 6, Press: true, Motion: true}) {
		t.Fatal("scrollbar drag not consumed")
	}
	if !loop.handleChromeMouse(MouseEvent{Button: 0, X: 20, Y: 6, Press: false}) {
		t.Fatal("scrollbar release not consumed")
	}
	if loop.scrollDrag {
		t.Fatal("drag did not end")
	}
	if loop.handleChromeMouse(MouseEvent{Button: 0, X: 10, Y: 5, Press: true}) {
		t.Fatal("content mouse was consumed")
	}
}

func TestScrollbarDragGrabsThumb(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	// Content height 12, max scroll 6: an 8-row thumb with a 4-row travel.
	loop.State.Page = protocol.State{
		FrontendChrome: true, Width: 20, Height: 14, HeaderRows: 1,
		MaxScroll: 6, Scroll: 2,
	}
	// Rows are content-relative; the thumb starts at row 1 with Scroll=2.
	if !loop.handleChromeMouse(MouseEvent{Button: 0, X: 20, Y: 1 + 1 + 3, Press: true}) {
		t.Fatal("thumb press not consumed")
	}
	if len(s.cmds) != 0 {
		t.Fatalf("thumb press moved the page: %#v", s.cmds)
	}
	if got := loop.scrollGrab; got != 2 {
		t.Fatalf("grab offset = %d, want 2", got)
	}
	// Dragging two rows down moves the thumb two rows down: thumb top 1 -> 3.
	s.cmds = nil
	if !loop.handleChromeMouse(MouseEvent{Button: 0, X: 20, Y: 1 + 1 + 5, Press: true, Motion: true}) {
		t.Fatal("thumb drag not consumed")
	}
	sc, ok := s.cmds[0].Body.(protocol.Scroll)
	if !ok || !sc.HasTo || sc.To != 4 {
		t.Fatalf("drag scroll = %#v", s.cmds[0].Body)
	}
	// The grabbed offset holds at the track ends.
	s.cmds = nil
	loop.handleChromeMouse(MouseEvent{Button: 0, X: 20, Y: 1 + 1, Press: true, Motion: true})
	sc = s.cmds[0].Body.(protocol.Scroll)
	if sc.To != 0 {
		t.Fatalf("drag past the top = %d", sc.To)
	}
	s.cmds = nil
	loop.handleChromeMouse(MouseEvent{Button: 0, X: 20, Y: 13, Press: true, Motion: true})
	sc = s.cmds[0].Body.(protocol.Scroll)
	if sc.To != 6 {
		t.Fatalf("drag past the bottom = %d", sc.To)
	}
	// A held drag keeps controlling the bar left of the track.
	s.cmds = nil
	if !loop.handleChromeMouse(MouseEvent{Button: 0, X: 5, Y: 1 + 1 + 5, Press: true, Motion: true}) {
		t.Fatal("off-track drag not consumed")
	}
	sc = s.cmds[0].Body.(protocol.Scroll)
	if sc.To != 4 {
		t.Fatalf("off-track drag = %d", sc.To)
	}
	// Moving into the header keeps dragging and clamps at the top.
	s.cmds = nil
	if !loop.handleChromeMouse(MouseEvent{Button: 0, X: 5, Y: 1, Press: true, Motion: true}) {
		t.Fatal("header drag not consumed")
	}
	sc = s.cmds[0].Body.(protocol.Scroll)
	if sc.To != 0 {
		t.Fatalf("drag into the header = %d", sc.To)
	}
}

func TestScrollbarDragEndsOffTrack(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{
		FrontendChrome: true, Width: 20, Height: 10, HeaderRows: 1,
		MaxScroll: 100,
	}
	startDrag := func() {
		t.Helper()
		if !loop.handleChromeMouse(MouseEvent{Button: 0, X: 20, Y: 5, Press: true}) || !loop.scrollDrag {
			t.Fatal("press did not start a drag")
		}
	}
	released := func() {
		t.Helper()
		s.cmds = nil
		if !loop.handleChromeMouse(MouseEvent{Button: 0, X: 10, Y: 6, Press: false}) {
			t.Fatal("off-track release was not consumed")
		}
		if loop.scrollDrag {
			t.Fatal("off-track release kept the drag active")
		}
		if loop.handleChromeMouse(MouseEvent{Button: 3, X: 20, Y: 6, Press: true, Motion: true}) {
			t.Fatal("hover after release was consumed")
		}
		if len(s.cmds) != 0 {
			t.Fatalf("hover after release scrolled: %d commands", len(s.cmds))
		}
	}
	startDrag()
	released()

	startDrag()
	s.cmds = nil
	if !loop.handleChromeMouse(MouseEvent{Button: 0, X: 20, Y: 1, Press: false}) || loop.scrollDrag {
		t.Fatal("header release did not end the drag")
	}
	if len(s.cmds) != 0 {
		t.Fatalf("header release scrolled: %d commands", len(s.cmds))
	}

	startDrag()
	if !loop.handleChromeMouse(MouseEvent{Button: 3, X: 20, Y: 6, Press: true, Motion: true}) {
		t.Fatal("buttonless hover during drag was not consumed")
	}
	if loop.scrollDrag {
		t.Fatal("buttonless hover kept the drag active")
	}
}

func TestPromptEditsAndNavigates(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{URL: "https://example.com/", FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	loop.promptOverlay.start(loop, false)
	if !loop.promptActive || !loop.promptSelectAll || loop.promptText != loop.State.Page.URL {
		t.Fatalf("prompt = %v %q select=%v", loop.promptActive, loop.promptText, loop.promptSelectAll)
	}
	loop.handleShellKey("h")
	loop.handleShellKey("i")
	if loop.promptText != "hi" || loop.promptSelectAll {
		t.Fatalf("text = %q select=%v", loop.promptText, loop.promptSelectAll)
	}
	if handled, quit := loop.handleShellKey("enter"); !handled || quit {
		t.Fatalf("enter handled=%v quit=%v", handled, quit)
	}
	if loop.promptActive {
		t.Fatal("prompt stayed open")
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.Navigate{URL: "hi", Push: true}) {
		t.Fatalf("commands = %#v", s.cmds)
	}
	loop.promptOverlay.start(loop, false)
	if _, ok := loop.promptHist.up(""); !ok {
		t.Fatal("history did not record the URL")
	}
}

func TestPromptEscapeAndVimQuit(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{URL: "https://example.com/", FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	loop.promptOverlay.start(loop, false)
	if handled, quit := loop.handleShellKey("esc"); !handled || quit || loop.promptActive {
		t.Fatalf("esc handled=%v quit=%v active=%v", handled, quit, loop.promptActive)
	}
	loop.promptOverlay.start(loop, true)
	loop.handleShellKey("q")
	if handled, quit := loop.handleShellKey("enter"); !handled || !quit {
		t.Fatalf("vim q handled=%v quit=%v", handled, quit)
	}
	if loop.promptActive {
		t.Fatal("vim prompt stayed open")
	}
}

func TestPromptMouseMovesCaret(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{URL: "https://example.com/", FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	loop.promptOverlay.start(loop, false)
	var hit ChromeHit
	for _, h := range LayoutChrome(loop.State.Page).Hits {
		if h.Kind == ChromeHitURL {
			hit = h
			break
		}
	}
	if hit.W == 0 {
		t.Fatal("no URL hit")
	}
	if !loop.handleChromeMouse(MouseEvent{X: hit.X + 8, Y: hit.Y + 1, Press: true}) {
		t.Fatal("prompt click not consumed")
	}
	if loop.promptSelectAll {
		t.Fatal("click left the whole prompt selected")
	}
	if loop.promptCursor < 0 || loop.promptCursor > len(loop.promptText) {
		t.Fatalf("cursor = %d", loop.promptCursor)
	}
}

func TestFindKeysSendQueriesAndResults(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{URL: "https://example.com/", FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	if handled, _ := loop.handleShellKey("/"); !handled || !loop.findActive {
		t.Fatalf("slash handled=%v active=%v", handled, loop.findActive)
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.Find{Query: ""}) {
		t.Fatalf("open commands = %#v", s.cmds)
	}
	s.cmds = nil
	loop.handleShellKey("a")
	loop.handleShellKey("b")
	if len(s.cmds) != 2 || s.cmds[0].Body != (protocol.Find{Query: "a"}) || s.cmds[1].Body != (protocol.Find{Query: "ab"}) {
		t.Fatalf("query commands = %#v", s.cmds)
	}
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.FindResults{Query: "ab", Current: 1, Total: 2}}); err != nil {
		t.Fatal(err)
	}
	if loop.findCurrent != 1 || loop.findTotal != 2 {
		t.Fatalf("results = %d/%d", loop.findCurrent, loop.findTotal)
	}
	s.cmds = nil
	if handled, quit := loop.handleShellKey("down"); !handled || quit {
		t.Fatalf("down handled=%v quit=%v", handled, quit)
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.Find{Query: "ab", Next: true}) {
		t.Fatalf("next command = %#v", s.cmds)
	}
	if handled, quit := loop.handleShellKey("enter"); !handled || quit || loop.findActive {
		t.Fatalf("enter handled=%v quit=%v active=%v", handled, quit, loop.findActive)
	}
	if !strings.Contains(loop.State.Page.FooterLeft, "Find 1/2: ab") {
		t.Fatalf("footer = %q", loop.State.Page.FooterLeft)
	}
}

func TestMenuKeysToggleAndZoom(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2, ZoomPercent: 100}
	if handled, _ := loop.handleShellKey("X"); !handled || loop.menuKind != protocol.MenuView {
		t.Fatalf("X handled=%v menu=%v", handled, loop.menuKind)
	}
	loop.handleShellKey("down")
	s.cmds = nil
	if handled, _ := loop.handleShellKey(" "); !handled {
		t.Fatal("space not handled")
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.App{Action: protocol.AppToggleSticky}) {
		t.Fatalf("toggle commands = %#v", s.cmds)
	}
	if loop.menuKind != protocol.MenuView {
		t.Fatal("view toggle closed the menu")
	}
	loop.handleShellKey("esc")
	if loop.menuKind != protocol.MenuNone {
		t.Fatal("esc left the menu open")
	}
	loop.menuOverlay.start(loop, protocol.MenuZoom)
	if loop.menuIndex != 4 {
		t.Fatalf("zoom index = %d, want 4", loop.menuIndex)
	}
	before := len(s.cmds)
	loop.handleChromeMouse(MouseEvent{Wheel: 1})
	loop.handleChromeMouse(MouseEvent{Wheel: -1})
	if loop.menuIndex != 4 {
		t.Fatalf("wheel zoom index = %d, want 4", loop.menuIndex)
	}
	if len(s.cmds) != before {
		t.Fatalf("wheel changed the zoom: %#v", s.cmds[before:])
	}
	layout := LayoutMenu(loop.State.Page)
	if len(layout.Rects) < 7 {
		t.Fatal("no zoom rows to hover")
	}
	hover := layout.Rects[6]
	loop.handleChromeMouse(MouseEvent{X: hover.X + 1, Y: hover.Y + 1, Motion: true})
	if loop.menuIndex != layout.Top+6 {
		t.Fatalf("hover zoom index = %d, want %d", loop.menuIndex, layout.Top+6)
	}
	hover = layout.Rects[4]
	loop.handleChromeMouse(MouseEvent{X: hover.X + 1, Y: hover.Y + 1, Motion: true})
	if loop.menuIndex != 4 {
		t.Fatalf("second hover zoom index = %d, want 4", loop.menuIndex)
	}
	if len(s.cmds) != before {
		t.Fatalf("hover changed the zoom: %#v", s.cmds[before:])
	}
	loop.handleShellKey("down")
	s.cmds = nil
	loop.handleShellKey("enter")
	if len(s.cmds) != 2 ||
		s.cmds[0].Body != (protocol.App{Action: protocol.AppFrontendOverlay, Arg: 0}) ||
		s.cmds[1].Body != (protocol.App{Action: protocol.AppZoomSet, Arg: 125}) {
		t.Fatalf("zoom commands = %#v", s.cmds)
	}
	if loop.menuKind != protocol.MenuNone {
		t.Fatal("zoom selection left the menu open")
	}
}

func TestMenuNotifiesEngineOverlay(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	loop.menuOverlay.start(loop, protocol.MenuView)
	if len(s.cmds) != 1 ||
		s.cmds[0].Body != (protocol.App{Action: protocol.AppFrontendOverlay, Arg: 1}) {
		t.Fatalf("open command = %#v", s.cmds)
	}
	s.cmds = nil
	loop.menuOverlay.sync(loop)
	if len(s.cmds) != 0 {
		t.Fatalf("unchanged menu sent %#v", s.cmds)
	}
	loop.menuOverlay.close(loop)
	if len(s.cmds) != 1 ||
		s.cmds[0].Body != (protocol.App{Action: protocol.AppFrontendOverlay, Arg: 0}) {
		t.Fatalf("close command = %#v", s.cmds)
	}
	s.cmds = nil
	loop.menuOverlay.close(loop)
	if len(s.cmds) != 0 {
		t.Fatalf("closing twice sent %#v", s.cmds)
	}
}

func TestMenuMouseRowAndBackdrop(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	loop.menuOverlay.start(loop, protocol.MenuView)
	layout := LayoutMenu(loop.State.Page)
	if len(layout.Rects) < 2 {
		t.Fatal("no menu rows")
	}
	r := layout.Rects[1]
	if !loop.handleChromeMouse(MouseEvent{X: r.X + 1, Y: r.Y + 1, Press: true}) {
		t.Fatal("row click not consumed")
	}
	if loop.menuIndex != layout.Top+1 || len(s.cmds) != 2 ||
		s.cmds[0].Body != (protocol.App{Action: protocol.AppFrontendOverlay, Arg: 1}) ||
		s.cmds[1].Body != (protocol.App{Action: protocol.AppToggleSticky}) {
		t.Fatalf("idx=%d cmds=%#v", loop.menuIndex, s.cmds)
	}
	loop.handleChromeMouse(MouseEvent{X: 1, Y: 1, Press: true})
	if loop.menuKind != protocol.MenuNone {
		t.Fatal("backdrop click did not close the menu")
	}
}

func TestDialogEventAndAnswers(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.Dialog{
		ID: 7, Kind: protocol.DialogPrompt, Text: "name?", Default: "def",
	}}); err != nil {
		t.Fatal(err)
	}
	if !loop.dialogActive || loop.dialogView.Value != "def" || !loop.dialogView.SelectAll {
		t.Fatalf("dialog = %v %+v", loop.dialogActive, loop.dialogView)
	}
	loop.handleShellKey("x")
	if loop.dialogView.Value != "x" {
		t.Fatalf("prompt value = %q", loop.dialogView.Value)
	}
	loop.handleShellKey("enter")
	if loop.dialogActive {
		t.Fatal("dialog stayed open")
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.DialogResult{ID: 7, OK: true, Text: "x"}) {
		t.Fatalf("commands = %#v", s.cmds)
	}

	s.cmds = nil
	loop.applyEvent(protocol.Event{Body: protocol.Dialog{ID: 8, Kind: protocol.DialogConfirm, Text: "sure?"}})
	loop.handleShellKey("n")
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.DialogResult{ID: 8, OK: false}) {
		t.Fatalf("confirm commands = %#v", s.cmds)
	}

	s.cmds = nil
	loop.applyEvent(protocol.Event{Body: protocol.Dialog{ID: 9, Kind: protocol.DialogAlert, Text: "hi"}})
	loop.handleShellKey(" ")
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.DialogResult{ID: 9, OK: true}) {
		t.Fatalf("alert commands = %#v", s.cmds)
	}
}

func TestDialogPromptMousePlacesCaret(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.Dialog{
		ID: 7, Kind: protocol.DialogPrompt, Text: "name?", Default: "abcdef",
	}}); err != nil {
		t.Fatal(err)
	}
	layout := LayoutDialog(loop.State.Page, &loop.dialogView)
	if layout.PromptW < 4 || layout.PromptY < 0 {
		t.Fatalf("prompt layout = %+v", layout)
	}
	if !loop.handleChromeMouse(MouseEvent{
		Button: MouseButtonLeft, X: layout.PromptX + 3 + 1, Y: layout.PromptY + 1, Press: true,
	}) {
		t.Fatal("prompt click was not consumed")
	}
	if !loop.dialogActive {
		t.Fatal("prompt click closed the dialog")
	}
	if loop.dialogView.Cursor != 3 || loop.dialogView.SelectAll {
		t.Fatalf("cursor=%d selectAll=%v, want 3/false", loop.dialogView.Cursor, loop.dialogView.SelectAll)
	}
	if len(s.cmds) != 0 {
		t.Fatalf("prompt click answered the dialog: %#v", s.cmds)
	}
}

func TestOpeningOverlayClosesNotice(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.Notice{Title: "Notice", Message: "Body"}}); err != nil {
		t.Fatal(err)
	}
	if !loop.noticeActive {
		t.Fatal("notice did not open")
	}
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.Select{
		ID: 4, Options: []string{"One", "Two"}, Selected: 0,
	}}); err != nil {
		t.Fatal(err)
	}
	if loop.noticeActive {
		t.Fatal("notice stayed open under the select popup")
	}
	if !loop.selectActive {
		t.Fatal("select did not open")
	}
}

func TestSelectEventKeysAndMouse(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.Select{
		ID: 4, Options: []string{"One", "Two", "Three"}, Selected: 0,
	}}); err != nil {
		t.Fatal(err)
	}
	if !loop.selectActive || loop.selectView.Index != 0 {
		t.Fatalf("select = %v %+v", loop.selectActive, loop.selectView)
	}
	loop.handleShellKey("down")
	loop.handleShellKey("enter")
	if loop.selectActive {
		t.Fatal("select stayed open")
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.SelectResult{ID: 4, Index: 1}) {
		t.Fatalf("commands = %#v", s.cmds)
	}

	s.cmds = nil
	loop.applyEvent(protocol.Event{Body: protocol.Select{ID: 5, Options: []string{"A", "B"}, Selected: 0}})
	layout := LayoutSelect(loop.State.Page, &loop.selectView)
	if len(layout.Rects) < 2 {
		t.Fatal("no select rows")
	}
	r := layout.Rects[1]
	if !loop.handleChromeMouse(MouseEvent{X: r.X + 1, Y: r.Y + 1, Press: true}) {
		t.Fatal("select row click not consumed")
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.SelectResult{ID: 5, Index: 1}) {
		t.Fatalf("mouse commands = %#v", s.cmds)
	}

	s.cmds = nil
	loop.applyEvent(protocol.Event{Body: protocol.Select{
		ID: 7, Options: []string{"A", "B", "C", "D", "E", "F"}, Selected: 0,
	}})
	loop.handleChromeMouse(MouseEvent{Wheel: 1})
	loop.handleChromeMouse(MouseEvent{Wheel: 1})
	if !loop.selectActive || loop.selectView.Index != 2 {
		t.Fatalf("wheel down index = %d active=%v, want 2", loop.selectView.Index, loop.selectActive)
	}
	loop.handleChromeMouse(MouseEvent{Wheel: -1})
	if loop.selectView.Index != 1 {
		t.Fatalf("wheel up index = %d, want 1", loop.selectView.Index)
	}
	if len(s.cmds) != 0 {
		t.Fatalf("wheel answered the select: %#v", s.cmds)
	}
	layout = LayoutSelect(loop.State.Page, &loop.selectView)
	if len(layout.Rects) < 5 {
		t.Fatal("no select rows to hover")
	}
	hover := layout.Rects[4]
	loop.handleChromeMouse(MouseEvent{X: hover.X + 1, Y: hover.Y + 1, Motion: true})
	if loop.selectView.Index != layout.Top+4 {
		t.Fatalf("hover index = %d, want %d", loop.selectView.Index, layout.Top+4)
	}
	if len(s.cmds) != 0 {
		t.Fatalf("hover answered the select: %#v", s.cmds)
	}
	loop.handleShellKey("esc")

	s.cmds = nil
	loop.applyEvent(protocol.Event{Body: protocol.Select{ID: 6, Options: []string{"A"}, Selected: 0}})
	loop.handleShellKey("esc")
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.SelectResult{ID: 6, Cancel: true}) {
		t.Fatalf("cancel commands = %#v", s.cmds)
	}
}

func TestDownloadCopyAndNoticeClose(t *testing.T) {
	s := &recordSession{}
	var clip strings.Builder
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	loop.State.Clipboard = &clip
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.Download{
		Name: "f.bin", Command: "curl -L -o 'f.bin' -- 'https://example.com/f.bin'",
	}}); err != nil {
		t.Fatal(err)
	}
	if !loop.downloadActive {
		t.Fatal("download did not open")
	}
	loop.handleShellKey("y")
	if loop.downloadActive {
		t.Fatal("download stayed open")
	}
	if !strings.Contains(clip.String(), "\x1b]52;c;") {
		t.Fatalf("clipboard = %q", clip.String())
	}
	if len(s.cmds) != 1 {
		t.Fatalf("commands = %#v", s.cmds)
	}
	mc, ok := s.cmds[0].Body.(protocol.ModalClose)
	if !ok || mc.Kind != protocol.ModalDownload || !mc.Copied || mc.Chars == 0 {
		t.Fatalf("modal close = %#v", s.cmds[0].Body)
	}

	s.cmds = nil
	loop.applyEvent(protocol.Event{Body: protocol.Notice{Title: "Notice", Message: "Body"}})
	if !loop.noticeActive {
		t.Fatal("notice did not open")
	}
	loop.handleShellKey("enter")
	if loop.noticeActive {
		t.Fatal("notice stayed open")
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.ModalClose{Kind: protocol.ModalNotice}) {
		t.Fatalf("commands = %#v", s.cmds)
	}
}

func TestHintEventKeys(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	items := []protocol.HintItem{
		{Target: 0, Label: "d", Chars: []protocol.HintChar{{R: 'D', X: 2, Y: 1}}},
		{Target: 1, Label: "de", Chars: []protocol.HintChar{{R: 'E', X: 4, Y: 1}}},
	}
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.Hints{Items: items}}); err != nil {
		t.Fatal(err)
	}
	if !loop.hintActive || loop.hintView.Prefix != "" {
		t.Fatalf("hint = %v %+v", loop.hintActive, loop.hintView)
	}
	loop.handleShellKey("d")
	if loop.hintView.Prefix != "d" {
		t.Fatalf("prefix = %q", loop.hintView.Prefix)
	}
	// A unique match follows immediately.
	loop.handleShellKey("e")
	if loop.hintActive {
		t.Fatal("hint stayed active")
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.HintResult{Target: 1}) {
		t.Fatalf("commands = %#v", s.cmds)
	}
}

func TestHintAskOpensBackgroundTab(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	opened := ""
	loop.Actions = TabActions{OpenTabBackground: func(url string) { opened = url }}
	items := []protocol.HintItem{{Target: 3, Label: "d", Link: true, Href: "https://example.com/next", Chars: []protocol.HintChar{{R: 'D', X: 2, Y: 1}}}}
	loop.applyEvent(protocol.Event{Body: protocol.Hints{Mode: protocol.HintNewTab, Items: items}})
	loop.handleShellKey("d")
	if loop.hintActive {
		t.Fatal("hint stayed active")
	}
	if opened != "https://example.com/next" {
		t.Fatalf("opened = %q", opened)
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.HintResult{Cancel: true}) {
		t.Fatalf("commands = %#v", s.cmds)
	}
}

func TestPromptClickSwitchesTabInOneClick(t *testing.T) {
	s := &recordSession{}
	tab := &Tab{Session: s, State: LoopState{Page: protocol.State{
		FrontendChrome: true, Width: 120, Height: 24, HeaderRows: 2,
	}}}
	tabs := []TabView{{Title: "One", Active: true}, {Title: "Two"}}
	tab.TabBar = tabs
	selected := -1
	tab.Actions = TabActions{Select: func(index int) { selected = index }}
	tab.promptOverlay.start(tab, false)
	var hit ChromeHit
	for _, h := range LayoutChromeWithTabs(tab.State.Page, tabs, 0).Hits {
		if h.Kind == ChromeHitTab && h.Index == 1 {
			hit = h
			break
		}
	}
	if hit.W == 0 {
		t.Fatal("no tab hit")
	}
	if !tab.handleChromeMouse(MouseEvent{X: hit.X + 1, Y: hit.Y + 1, Press: true}) {
		t.Fatal("tab click was not consumed")
	}
	if tab.promptActive {
		t.Fatal("prompt stayed open")
	}
	if selected != 1 {
		t.Fatalf("selected tab = %d", selected)
	}
}

func TestChromeClickBlursPageInputInOneClick(t *testing.T) {
	s := &recordSession{}
	tab := &Tab{Session: s, State: LoopState{Page: protocol.State{
		FrontendChrome: true, KeysEngine: true, Width: 120, Height: 24, HeaderRows: 2,
	}}}
	tabs := []TabView{{Title: "One", Active: true}, {Title: "Two"}}
	tab.TabBar = tabs
	selected := -1
	tab.Actions = TabActions{Select: func(index int) { selected = index }}
	var hit ChromeHit
	for _, h := range LayoutChromeWithTabs(tab.State.Page, tabs, 0).Hits {
		if h.Kind == ChromeHitTab && h.Index == 1 {
			hit = h
			break
		}
	}
	if hit.W == 0 {
		t.Fatal("no tab hit")
	}
	if !tab.handleChromeMouse(MouseEvent{X: hit.X + 1, Y: hit.Y + 1, Press: true}) {
		t.Fatal("tab click was not consumed")
	}
	if selected != 1 {
		t.Fatalf("selected tab = %d", selected)
	}
	foundEsc := false
	for _, cmd := range s.cmds {
		if cmd.Body == (protocol.Key{Token: "esc"}) {
			foundEsc = true
		}
	}
	if !foundEsc {
		t.Fatalf("chrome click did not blur the page input: %#v", s.cmds)
	}
}

func TestContentClickStaysWithFocusedPageInput(t *testing.T) {
	tab := &Tab{Session: &recordSession{}, State: LoopState{Page: protocol.State{
		FrontendChrome: true, KeysEngine: true, Width: 80, Height: 24, HeaderRows: 2,
	}}}
	if tab.handleChromeMouse(MouseEvent{X: 10, Y: 10, Press: true}) {
		t.Fatal("content click was stolen by chrome")
	}
}

func TestConsoleInputKeys(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{Width: 80, Height: 24}
	loop.consoleActive = true
	loop.consolePane = consolePaneLogs
	loop.applyEvent(protocol.Event{Body: protocol.ConsoleView{
		Width: 80, Height: 24, Pane: consolePaneLogs,
		Tabs: []string{" JS logs ", " Requests ", " Storage ", " HTML ", " Tools "},
	}})
	loop.handleShellKey(":")
	if !loop.consoleEdit {
		t.Fatal("console input did not focus")
	}
	s.cmds = nil
	for _, r := range "1+1" {
		loop.handleShellKey(string(r))
	}
	if loop.consoleText != "1+1" || loop.consoleCursor != 3 {
		t.Fatalf("input = %q cursor=%d", loop.consoleText, loop.consoleCursor)
	}
	loop.handleShellKey("enter")
	if len(s.cmds) != 2 {
		t.Fatalf("commands = %#v", s.cmds)
	}
	if a, ok := s.cmds[0].Body.(protocol.ConsoleAction); !ok || a.Op != protocol.ConsoleInput || a.Text != "1+1" || a.Pane != consolePaneLogs {
		t.Fatalf("input action = %#v", s.cmds[0].Body)
	}
	if a, ok := s.cmds[1].Body.(protocol.ConsoleAction); !ok || a.Op != protocol.ConsoleWindow || a.Start != -1 {
		t.Fatalf("window action = %#v", s.cmds[1].Body)
	}
	if loop.consoleText != "" {
		t.Fatalf("input not cleared: %q", loop.consoleText)
	}
	s.cmds = nil
	loop.handleShellKey("esc") // leave input
	s.cmds = nil
	loop.handleShellKey("tab")
	if len(s.cmds) != 1 {
		t.Fatalf("cycle commands = %#v", s.cmds)
	}
	if a, ok := s.cmds[0].Body.(protocol.ConsoleAction); !ok || a.Op != protocol.ConsoleWindow || a.Pane != consolePaneRequests || !a.Enter {
		t.Fatalf("cycle action = %#v", s.cmds[0].Body)
	}
}

func TestConsoleQuitAndCloseKeys(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{Width: 80, Height: 24}
	loop.consoleActive = true
	loop.consolePane = consolePaneLogs
	loop.applyEvent(protocol.Event{Body: protocol.ConsoleView{Width: 80, Height: 24, Pane: consolePaneLogs, Tabs: []string{"a"}}})
	loop.handleShellKey("q")
	if len(s.cmds) != 0 || !loop.consoleQuit {
		t.Fatalf("quit state=%v commands=%#v", loop.consoleQuit, s.cmds)
	}
	if handled, quit := loop.handleShellKey("y"); !handled || !quit {
		t.Fatalf("y handled=%v quit=%v", handled, quit)
	}
	s.cmds = nil
	loop.consoleQuit = false
	loop.handleShellKey("esc")
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.ConsoleAction{Op: protocol.ConsoleClose}) {
		t.Fatalf("close action = %#v", s.cmds)
	}
	if loop.consoleActive {
		t.Fatal("console stayed open")
	}
}

func TestConsoleMouseTabsInputAndClose(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{Width: 80, Height: 24}
	tabs := []string{" JS logs ", " Requests ", " Storage ", " HTML ", " Tools "}
	loop.consoleActive = true
	loop.consolePane = consolePaneLogs
	loop.applyEvent(protocol.Event{Body: protocol.ConsoleView{
		Width: 80, Height: 24, Pane: consolePaneLogs, Tabs: tabs,
	}})

	// Click the Storage tab: walk the same widths the menu draws.
	x := 0
	for _, title := range tabs[:2] {
		x += textwidth.StringWidth(title)
	}
	if !loop.handleChromeMouse(MouseEvent{X: x + 1, Y: 1, Press: true}) {
		t.Fatal("tab click not consumed")
	}
	if len(s.cmds) != 1 {
		t.Fatalf("tab commands = %#v", s.cmds)
	}
	if a, ok := s.cmds[0].Body.(protocol.ConsoleAction); !ok || a.Op != protocol.ConsoleWindow || a.Pane != consolePaneStorage || !a.Enter {
		t.Fatalf("tab action = %#v", s.cmds[0].Body)
	}
	if loop.consolePane != consolePaneStorage {
		t.Fatalf("pane = %d", loop.consolePane)
	}

	// Click the input row on the logs pane.
	loop.consolePane = consolePaneLogs
	s.cmds = nil
	inputY := consoleLayoutFor(24, consolePaneLogs).inputY
	if !loop.handleChromeMouse(MouseEvent{X: 3, Y: inputY + 1, Press: true}) {
		t.Fatal("input click not consumed")
	}
	if len(s.cmds) != 0 || !loop.consoleEdit {
		t.Fatalf("input click commands=%#v edit=%v", s.cmds, loop.consoleEdit)
	}

	// The footer >_ icon closes the console.
	s.cmds = nil
	loop.consoleEdit = false
	if !loop.handleChromeMouse(MouseEvent{X: 80, Y: 24, Press: true}) {
		t.Fatal("footer icon click not consumed")
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.ConsoleAction{Op: protocol.ConsoleClose}) {
		t.Fatalf("footer action = %#v", s.cmds)
	}
	if loop.consoleActive {
		t.Fatal("footer icon did not close the console")
	}

	// The wheel and a body click stay in the console rather than the page.
	loop.consoleActive = true
	loop.consolePane = consolePaneLogs
	loop.applyEvent(protocol.Event{Body: protocol.ConsoleView{
		Width: 80, Height: 24, Pane: consolePaneLogs, Tabs: tabs, Start: 5, Total: 30,
	}})
	s.cmds = nil
	if !loop.handleChromeMouse(MouseEvent{Wheel: 1, X: 5, Y: 5}) {
		t.Fatal("wheel not consumed")
	}
	if len(s.cmds) != 1 {
		t.Fatalf("wheel commands = %#v", s.cmds)
	}
	if a, ok := s.cmds[0].Body.(protocol.ConsoleAction); !ok || a.Op != protocol.ConsoleWindow || a.Start != 2 {
		t.Fatalf("wheel action = %#v", s.cmds[0].Body)
	}
	s.cmds = nil
	if !loop.handleChromeMouse(MouseEvent{X: 5, Y: 5, Press: true}) {
		t.Fatal("body click not consumed")
	}
	if len(s.cmds) != 0 {
		t.Fatalf("body click sent %#v", s.cmds)
	}
}

func TestConsoleOpenAndCloseKeys(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{Width: 80, Height: 24}
	if handled, quit := loop.handleShellKey("!"); !handled || quit {
		t.Fatalf("! handled=%v quit=%v", handled, quit)
	}
	if !loop.consoleActive {
		t.Fatal("console did not open")
	}
	if len(s.cmds) != 2 {
		t.Fatalf("open commands = %#v", s.cmds)
	}
	if a, ok := s.cmds[0].Body.(protocol.ConsoleAction); !ok || a.Op != protocol.ConsoleOpen {
		t.Fatalf("open action = %#v", s.cmds[0].Body)
	}
	if a, ok := s.cmds[1].Body.(protocol.ConsoleAction); !ok || a.Op != protocol.ConsoleWindow || a.Start != -1 {
		t.Fatalf("window action = %#v", s.cmds[1].Body)
	}
	s.cmds = nil
	loop.handleShellKey("!")
	if loop.consoleActive {
		t.Fatal("! did not close the console")
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.ConsoleAction{Op: protocol.ConsoleClose}) {
		t.Fatalf("close action = %#v", s.cmds)
	}
}

func TestConsoleResizeRequestsWindow(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{Width: 80, Height: 24}
	loop.consoleActive = true
	loop.consolePane = consolePaneLogs
	loop.consoleView = protocol.ConsoleView{Width: 80, Height: 24}
	loop.consoleOverlay.resize(loop, 100, 30)
	if len(s.cmds) != 1 {
		t.Fatalf("resize commands = %#v", s.cmds)
	}
	a, ok := s.cmds[0].Body.(protocol.ConsoleAction)
	if !ok || a.Op != protocol.ConsoleWindow || a.Width != 100 || a.Rows != consoleLayoutFor(30, consolePaneLogs).bodyRows-1 {
		t.Fatalf("resize action = %#v", s.cmds[0].Body)
	}
}

func TestConsoleViewScrollsToFindLine(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{Width: 80, Height: 24}
	loop.consoleActive = true
	loop.consolePane = consolePaneHTML
	loop.consoleFindActive = true
	loop.consoleFindText = "needle"
	loop.applyEvent(protocol.Event{Body: protocol.ConsoleView{
		Width: 80, Height: 24, Pane: consolePaneHTML,
		Tabs:  []string{" JS logs ", " Requests ", " Storage ", " HTML ", " Tools "},
		Start: 0, Total: 100, FindCurrent: 1, FindTotal: 2, FindLine: 50, FindLineValid: true,
	}})
	if loop.consoleStart[consolePaneHTML] != 50 {
		t.Fatalf("start = %d, want 50", loop.consoleStart[consolePaneHTML])
	}
	if len(s.cmds) != 1 {
		t.Fatalf("scroll commands = %#v", s.cmds)
	}
	if a, ok := s.cmds[0].Body.(protocol.ConsoleAction); !ok || a.Op != protocol.ConsoleWindow || a.Start != 50 {
		t.Fatalf("scroll action = %#v", s.cmds[0].Body)
	}
}

func TestConsoleFollowsBottomOnNewOutput(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{Width: 80, Height: 24}
	loop.consoleActive = true
	loop.consolePane = consolePaneLogs
	loop.consoleFollow[consolePaneLogs] = true
	loop.applyEvent(protocol.Event{Body: protocol.ConsoleView{
		Width: 80, Height: 24, Pane: consolePaneLogs, Start: 8, Total: 29,
	}})
	if loop.consoleStart[consolePaneLogs] != 8 {
		t.Fatalf("start = %d, want bottom 8", loop.consoleStart[consolePaneLogs])
	}
	s.cmds = nil
	// The engine emitted a window for the old bottom after new output arrived.
	loop.applyEvent(protocol.Event{Body: protocol.ConsoleView{
		Width: 80, Height: 24, Pane: consolePaneLogs, Start: 8, Total: 30,
	}})
	if loop.consoleStart[consolePaneLogs] != 9 {
		t.Fatalf("start = %d, want new bottom 9", loop.consoleStart[consolePaneLogs])
	}
	if len(s.cmds) != 1 {
		t.Fatalf("follow commands = %#v", s.cmds)
	}
	if a, ok := s.cmds[0].Body.(protocol.ConsoleAction); !ok || a.Op != protocol.ConsoleWindow || a.Start != -1 {
		t.Fatalf("follow action = %#v", s.cmds[0].Body)
	}
}

func TestConsoleFindContinuesAfterEsc(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{Width: 80, Height: 24}
	loop.consoleActive = true
	loop.consolePane = consolePaneHTML
	loop.consoleFindActive = true
	loop.consoleFindText = "needle"
	loop.applyEvent(protocol.Event{Body: protocol.ConsoleView{
		Width: 80, Height: 24, Pane: consolePaneHTML,
		Start: 0, Total: 100, FindLine: 5, FindLineValid: true,
	}})
	loop.handleShellKey("esc")
	if loop.consoleFindActive {
		t.Fatal("find bar stayed open")
	}
	s.cmds = nil
	// The next n moves the engine match; the hidden bar still scrolls to it.
	loop.applyEvent(protocol.Event{Body: protocol.ConsoleView{
		Width: 80, Height: 24, Pane: consolePaneHTML,
		Start: 0, Total: 100, FindLine: 50, FindLineValid: true,
	}})
	if loop.consoleStart[consolePaneHTML] != 50 {
		t.Fatalf("start = %d, want 50", loop.consoleStart[consolePaneHTML])
	}
	if len(s.cmds) != 1 {
		t.Fatalf("find scroll commands = %#v", s.cmds)
	}
	if a, ok := s.cmds[0].Body.(protocol.ConsoleAction); !ok || a.Op != protocol.ConsoleWindow || a.Start != 50 {
		t.Fatalf("find scroll action = %#v", s.cmds[0].Body)
	}
}

func TestConsoleFindKeys(t *testing.T) {
	s := &recordSession{}
	loop := &Tab{Session: s}
	loop.State.Page = protocol.State{Width: 80, Height: 24}
	loop.consoleActive = true
	loop.consolePane = consolePaneHTML
	loop.applyEvent(protocol.Event{Body: protocol.ConsoleView{
		Width: 80, Height: 24, Pane: consolePaneHTML,
		Tabs: []string{" JS logs ", " Requests ", " Storage ", " HTML ", " Tools "},
	}})
	loop.handleShellKey("/")
	if !loop.consoleFindActive {
		t.Fatal("console find did not open")
	}
	if len(s.cmds) != 1 {
		t.Fatalf("open commands = %#v", s.cmds)
	}
	if f, ok := s.cmds[0].Body.(protocol.Find); !ok || f.Scope != protocol.FindHTML || f.Query != "" {
		t.Fatalf("open find = %#v", s.cmds[0].Body)
	}
	s.cmds = nil
	for _, r := range "needle" {
		loop.handleShellKey(string(r))
	}
	if loop.consoleFindText != "needle" || len(s.cmds) != 6 {
		t.Fatalf("text=%q commands=%d", loop.consoleFindText, len(s.cmds))
	}
	s.cmds = nil
	loop.handleShellKey("down")
	loop.handleShellKey("up")
	if len(s.cmds) != 2 {
		t.Fatalf("arrow commands = %#v", s.cmds)
	}
	if f, ok := s.cmds[0].Body.(protocol.Find); !ok || f.Scope != protocol.FindHTML || f.Query != "needle" || !f.Next {
		t.Fatalf("down find = %#v", s.cmds[0].Body)
	}
	if f, ok := s.cmds[1].Body.(protocol.Find); !ok || f.Scope != protocol.FindHTML || f.Query != "needle" || !f.Prev {
		t.Fatalf("up find = %#v", s.cmds[1].Body)
	}
	s.cmds = nil
	loop.handleShellKey("enter")
	if loop.consoleFindActive {
		t.Fatal("console find stayed open")
	}
	if len(s.cmds) != 0 {
		t.Fatalf("close find commands = %#v", s.cmds)
	}
	s.cmds = nil
	loop.handleShellKey("n")
	if len(s.cmds) != 1 {
		t.Fatalf("next commands = %#v", s.cmds)
	}
	if f, ok := s.cmds[0].Body.(protocol.Find); !ok || f.Scope != protocol.FindHTML || f.Query != "needle" || !f.Next {
		t.Fatalf("next find = %#v", s.cmds[0].Body)
	}
	s.cmds = nil
	loop.handleShellKey("N")
	if len(s.cmds) != 1 {
		t.Fatalf("prev commands = %#v", s.cmds)
	}
	if f, ok := s.cmds[0].Body.(protocol.Find); !ok || f.Scope != protocol.FindHTML || f.Query != "needle" || !f.Prev {
		t.Fatalf("prev find = %#v", s.cmds[0].Body)
	}
}
