package frontend

import (
	"bytes"
	"image"
	"io"
	"strings"
	"testing"

	"browser/internal/protocol"
)

func TestApplySessionEventPresentsNativeFrame(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	p.SetGraphics(GraphicsKitty, 8, 16)
	loop := &SessionLoop{Presenter: p, Stderr: io.Discard}
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
	loop := &SessionLoop{Presenter: p, Stderr: io.Discard}
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
	loop := &SessionLoop{Presenter: p, Stderr: io.Discard}
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
	loop := &SessionLoop{Presenter: p, Stderr: io.Discard}
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
	loop := &SessionLoop{Session: s, Presenter: NewPresenter(io.Discard, false)}
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

func TestShellKeysSendAppActions(t *testing.T) {
	s := &recordSession{}
	loop := &SessionLoop{Session: s}
	loop.State.Page = protocol.State{FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	cases := map[string]any{
		"r": protocol.App{Action: protocol.AppReload},
		"R": protocol.App{Action: protocol.AppToggleReader},
		"D": protocol.App{Action: protocol.AppToggleDark},
		"+": protocol.App{Action: protocol.AppZoomIn},
		"-": protocol.App{Action: protocol.AppZoomOut},
		"0": protocol.App{Action: protocol.AppZoomReset},
		"J": protocol.App{Action: protocol.AppToggleJS},
		"i": protocol.App{Action: protocol.AppToggleImages},
		"E": protocol.App{Action: protocol.AppStatusDetails},
		"d": protocol.App{Action: protocol.AppDownload},
		"!": protocol.App{Action: protocol.AppToggleConsole},
		"X": protocol.App{Action: protocol.AppOpenViewMenu},
		"Z": protocol.App{Action: protocol.AppOpenZoom},
		"?": protocol.Navigate{URL: "about:help", Push: true},
		"H": protocol.Navigate{URL: "about:history", Push: true},
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

func TestShellKeysStayWithEngineWhenCaptured(t *testing.T) {
	s := &recordSession{}
	loop := &SessionLoop{Session: s}
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
	loop := &SessionLoop{Session: s}
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

func TestPromptEditsAndNavigates(t *testing.T) {
	s := &recordSession{}
	loop := &SessionLoop{Session: s}
	loop.State.Page = protocol.State{URL: "https://example.com/", FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	loop.startPrompt(false)
	if !loop.promptActive || !loop.promptSelectAll || loop.promptText != loop.State.Page.URL {
		t.Fatalf("prompt = %v %q select=%v", loop.promptActive, loop.promptText, loop.promptSelectAll)
	}
	loop.handlePromptKey("h")
	loop.handlePromptKey("i")
	if loop.promptText != "hi" || loop.promptSelectAll {
		t.Fatalf("text = %q select=%v", loop.promptText, loop.promptSelectAll)
	}
	if handled, quit := loop.handlePromptKey("enter"); !handled || quit {
		t.Fatalf("enter handled=%v quit=%v", handled, quit)
	}
	if loop.promptActive {
		t.Fatal("prompt stayed open")
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.Navigate{URL: "hi", Push: true}) {
		t.Fatalf("commands = %#v", s.cmds)
	}
	loop.startPrompt(false)
	if _, ok := loop.promptHist.up(""); !ok {
		t.Fatal("history did not record the URL")
	}
}

func TestPromptEscapeAndVimQuit(t *testing.T) {
	s := &recordSession{}
	loop := &SessionLoop{Session: s}
	loop.State.Page = protocol.State{URL: "https://example.com/", FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	loop.startPrompt(false)
	if handled, quit := loop.handlePromptKey("esc"); !handled || quit || loop.promptActive {
		t.Fatalf("esc handled=%v quit=%v active=%v", handled, quit, loop.promptActive)
	}
	loop.startPrompt(true)
	loop.handlePromptKey("q")
	if handled, quit := loop.handlePromptKey("enter"); !handled || !quit {
		t.Fatalf("vim q handled=%v quit=%v", handled, quit)
	}
	if loop.promptActive {
		t.Fatal("vim prompt stayed open")
	}
}

func TestPromptMouseMovesCaret(t *testing.T) {
	s := &recordSession{}
	loop := &SessionLoop{Session: s}
	loop.State.Page = protocol.State{URL: "https://example.com/", FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	loop.startPrompt(false)
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
	if !loop.handlePromptMouse(MouseEvent{X: hit.X + 8, Y: hit.Y + 1, Press: true}) {
		t.Fatal("prompt click not consumed")
	}
	if loop.promptSelectAll {
		t.Fatal("click left the whole prompt selected")
	}
	if loop.promptCursor < 0 || loop.promptCursor > len(loop.promptText) {
		t.Fatalf("cursor = %d", loop.promptCursor)
	}
}
