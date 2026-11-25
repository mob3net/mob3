package engine

import (
	"context"
	"image"
	"os"
	"strings"
	"testing"
	"time"

	"browser/internal/protocol"
)

// quietStdout discards frames drawn by the engine goroutine for the duration of
// the test. Restore happens after the session is closed.
func quietStdout(t *testing.T) func() {
	t.Helper()
	old := os.Stdout
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = devnull
	return func() {
		os.Stdout = old
		_ = devnull.Close()
	}
}

func openTestSession(t *testing.T, b *Session) protocol.Session {
	t.Helper()
	s, err := (&protocol.InProc{}).Open(context.Background(), 1, b)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(protocol.Command{Body: protocol.Init{Width: b.Width, Height: b.Height, CellW: b.CellW, CellH: b.CellH, Zoom: b.Zoom}}); err != nil {
		t.Fatal(err)
	}
	return s
}

func waitEvent(t *testing.T, s protocol.Session, match func(protocol.EventBody) bool) protocol.EventBody {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("event stream closed before a matching event")
			}
			if match(ev.Body) {
				return ev.Body
			}
		case <-deadline:
			t.Fatal("timed out waiting for an event")
		}
	}
}

func TestSessionQuitThroughTransport(t *testing.T) {
	b := newTestSession(60, 10)
	b.NoJS = true
	restore := quietStdout(t)
	defer restore()
	s := openTestSession(t, b)
	if err := s.Send(protocol.Command{Body: protocol.Shutdown{}}); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, s, func(body protocol.EventBody) bool {
		_, ok := body.(protocol.Quit)
		return ok
	})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSessionInitResizeAndScroll(t *testing.T) {
	root := ParseHTML(`<html><body>` + strings.Repeat("<p>line</p>", 200) + `</body></html>`)
	b := newTestSession(60, 10)
	b.NoJS = true
	b.Page = &Page{Root: root, Title: "t"}
	b.Layout = BuildLayout(root, nil, 60)
	restore := quietStdout(t)
	defer restore()

	s, err := (&protocol.InProc{}).Open(context.Background(), 1, b)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Send(protocol.Command{Body: protocol.Init{Width: 60, Height: 10, CellW: 8, CellH: 16, Zoom: 1}}); err != nil {
		t.Fatal(err)
	}
	// Height 10 reserves two header rows and one footer row: viewport 7.
	maxBefore := b.Layout.Height - 7
	if maxBefore < 1 {
		t.Fatalf("test layout is too short: %d rows", b.Layout.Height)
	}
	if err := s.Send(protocol.Command{Body: protocol.Key{Token: "G"}}); err != nil {
		t.Fatal(err)
	}
	st, ok := waitEvent(t, s, func(body protocol.EventBody) bool {
		v, ok := body.(protocol.State)
		return ok && v.Scroll > 0
	}).(protocol.State)
	if !ok {
		t.Fatalf("state event has the wrong type")
	}
	if st.MaxScroll != maxBefore {
		t.Fatalf("max scroll = %d, want %d", st.MaxScroll, maxBefore)
	}
	if st.Scroll != st.MaxScroll {
		t.Fatalf("scroll = %d, want max %d", st.Scroll, st.MaxScroll)
	}

	if err := s.Send(protocol.Command{Body: protocol.Resize{Width: 80, Height: 14, CellW: 8, CellH: 16}}); err != nil {
		t.Fatal(err)
	}
	// A larger viewport lowers the maximum scroll. "G" applies the new value.
	if err := s.Send(protocol.Command{Body: protocol.Key{Token: "G"}}); err != nil {
		t.Fatal(err)
	}
	st, ok = waitEvent(t, s, func(body protocol.EventBody) bool {
		v, ok := body.(protocol.State)
		return ok && v.MaxScroll != maxBefore
	}).(protocol.State)
	if !ok {
		t.Fatalf("state event has the wrong type")
	}
	if st.Scroll != st.MaxScroll || st.MaxScroll >= maxBefore {
		t.Fatalf("resized scroll = %d/%d, want max below %d", st.Scroll, st.MaxScroll, maxBefore)
	}
}

func TestSessionAbortIsOutOfBand(t *testing.T) {
	b := newTestSession(60, 10)
	b.NoJS = true
	restore := quietStdout(t)
	defer restore()
	s := openTestSession(t, b)
	s.Abort()
	// The front end also forwards the Esc key. Either path counts the abort,
	// even when the command is ready before the out-of-band branch is selected.
	if err := s.Send(protocol.Command{Body: protocol.Key{Token: "esc"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got := b.abortGen.Load(); got == 0 {
		t.Fatal("out-of-band abort was not counted")
	}
}

func TestSessionEmitsScreenFrames(t *testing.T) {
	b := newTestSession(30, 8)
	b.NoJS = true
	restore := quietStdout(t)
	defer restore()
	s := openTestSession(t, b)
	defer s.Close()
	if err := s.Send(protocol.Command{Body: protocol.Key{Token: "j"}}); err != nil {
		t.Fatal(err)
	}
	var f protocol.Frame
	deadline := time.After(3 * time.Second)
	for len(f.Cells) == 0 {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("event stream closed")
			}
			switch v := ev.Body.(type) {
			case protocol.Frame:
				f = v
			}
		case <-deadline:
			t.Fatal("timed out waiting for a frame")
		}
	}
	if f.W != 30 || f.H != 5 {
		t.Fatalf("content frame = %dx%d, want 30x5", f.W, f.H)
	}
	if len(f.Cells) != f.W*f.H {
		t.Fatalf("frame cells = %d, want %d", len(f.Cells), f.W*f.H)
	}
}

func TestSessionAppActionReachesState(t *testing.T) {
	b := newTestSession(60, 10)
	b.NoJS = true
	restore := quietStdout(t)
	defer restore()
	s := openTestSession(t, b)
	defer s.Close()
	if err := s.Send(protocol.Command{Body: protocol.App{Action: protocol.AppToggleDark}}); err != nil {
		t.Fatal(err)
	}
	st, ok := waitEvent(t, s, func(body protocol.EventBody) bool {
		v, ok := body.(protocol.State)
		return ok && v.Flags&protocol.StateDark != 0
	}).(protocol.State)
	if !ok {
		t.Fatalf("state event has the wrong type")
	}
	if st.Flags&protocol.StateReader != 0 {
		t.Fatalf("unexpected reader flag: %#x", st.Flags)
	}
}

type frameCapture struct{ frame protocol.Frame }

func (s *frameCapture) Send(ev protocol.Event) error {
	if f, ok := ev.Body.(protocol.Frame); ok {
		s.frame = f
	}
	return nil
}

func TestSessionFrameCarriesNativePlacements(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 64))
	b := newTestSession(40, 12)
	b.NoJS = true
	b.color = true
	b.Page = &Page{Root: ParseHTML(`<html><body></body></html>`), Title: "img"}
	b.Layout = &Layout{Height: 8, Frags: []Frag{{Kind: FragImage, X: 1, Y: 0, W: 4, H: 4, Img: img}}}
	b.caps |= protocol.CapKittyGraphics
	b.emitFrames = true
	capture := &frameCapture{}
	b.sessionOut = capture
	b.draw()
	b.emitPendingFrame()
	if len(capture.frame.Placements) != 1 {
		t.Fatalf("placements = %d, want 1", len(capture.frame.Placements))
	}
	p := capture.frame.Placements[0]
	if p.Format != protocol.PlacementPNG || p.Key == "" || len(p.Data) == 0 {
		t.Fatalf("placement = %+v", p)
	}
	first := p.Data
	b.draw()
	b.emitPendingFrame()
	if len(capture.frame.Placements) != 1 {
		t.Fatalf("second frame placements = %d", len(capture.frame.Placements))
	}
	if &capture.frame.Placements[0].Data[0] != &first[0] {
		t.Fatal("placement bytes were re-encoded instead of reusing the cache")
	}
}

func TestSessionClipsOutOfBoundsPlacements(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	b := newTestSession(10, 8)
	b.NoJS = true
	b.NoDraw = false
	b.Page = &Page{Root: ParseHTML(`<html><body></body></html>`), Title: "clip"}
	b.Layout = &Layout{Height: 8, Frags: []Frag{{Kind: FragImage, X: -5, Y: 1, W: 20, H: 3, Img: img}}}
	b.caps |= protocol.CapKittyGraphics
	b.emitFrames = true
	capture := &frameCapture{}
	b.sessionOut = capture
	b.draw()
	b.emitPendingFrame()
	f := capture.frame
	if err := f.Validate(protocol.DefaultLimits); err != nil {
		t.Fatalf("frame is invalid: %v", err)
	}
	if len(f.Placements) != 1 {
		t.Fatalf("placements = %d, want 1", len(f.Placements))
	}
	p := f.Placements[0]
	if p.X != 0 || p.W <= 0 || p.X+p.W > f.W {
		t.Fatalf("placement x/w = %d/%d in frame %d, want clipped to [0,%d]", p.X, p.W, f.W, f.W)
	}
}

func TestClientErrorLogsOnce(t *testing.T) {
	b := newTestSession(40, 10)
	cmd := protocol.Command{Body: protocol.ClientError{Message: "bad frame\x1b[31m"}}
	if _, err := b.handleCommand(cmd); err != nil {
		t.Fatal(err)
	}
	if _, err := b.handleCommand(cmd); err != nil {
		t.Fatal(err)
	}
	if len(b.jsLog) != 1 {
		t.Fatalf("jsLog = %q, want one entry", b.jsLog)
	}
	if strings.Contains(b.jsLog[0], "\x1b") {
		t.Fatalf("console entry kept a control sequence: %q", b.jsLog[0])
	}
	if !strings.Contains(b.status, "engine") {
		t.Fatalf("status = %q, want an engine-error hint", b.status)
	}
}

func TestConsoleDrawEmitsView(t *testing.T) {
	b := newTestSession(40, 10)
	b.NoJS = true
	b.emitFrames = true
	b.consoleOpen = true
	capture := &consoleSink{}
	b.sessionOut = capture
	b.draw()
	if !capture.got || capture.view.Width != 40 || capture.view.Height != 10 {
		t.Fatalf("console view = %+v", capture.view)
	}
	if len(capture.view.Tabs) != 5 {
		t.Fatalf("console tabs = %d", len(capture.view.Tabs))
	}
}

type dialogSink struct{ dialog protocol.Dialog }

func (s *dialogSink) Send(ev protocol.Event) error {
	if d, ok := ev.Body.(protocol.Dialog); ok {
		s.dialog = d
	}
	return nil
}

func TestDialogTransportRoundTrip(t *testing.T) {
	b := newTestSession(60, 10)
	b.NoDraw = true
	in := make(chan protocol.Command, 2)
	sink := &dialogSink{}
	b.sessionCtx = context.Background()
	b.sessionIn = in
	b.sessionAbort = make(chan struct{})
	b.sessionOut = sink
	in <- protocol.Command{Body: protocol.DialogResult{ID: 1, OK: true, Text: "answer"}}
	text, ok := b.showPrompt("name?", "def")
	if !ok || text != "answer" {
		t.Fatalf("prompt = %q, %v", text, ok)
	}
	if sink.dialog.Kind != protocol.DialogPrompt || sink.dialog.Default != "def" || sink.dialog.Text != "name?" {
		t.Fatalf("dialog event = %+v", sink.dialog)
	}
}

type selectSink struct{ sel protocol.Select }

func (s *selectSink) Send(ev protocol.Event) error {
	if v, ok := ev.Body.(protocol.Select); ok {
		s.sel = v
	}
	return nil
}

func TestSelectTransportRoundTrip(t *testing.T) {
	sel := &Node{Type: ElementNode, Tag: "select", Children: []*Node{
		{Type: ElementNode, Tag: "option", Text: "One"},
		{Type: ElementNode, Tag: "option", Text: "Two", Attrs: []Attr{{Key: "selected", Value: ""}}},
	}}
	b := newTestSession(60, 10)
	b.NoDraw = true
	sink := &selectSink{}
	in := make(chan protocol.Command, 2)
	b.sessionCtx = context.Background()
	b.sessionIn = in
	b.sessionAbort = make(chan struct{})
	b.sessionOut = sink
	b.openSelectMenu(sel)
	if sink.sel.ID != 1 || len(sink.sel.Options) != 2 || sink.sel.Selected != 1 {
		t.Fatalf("select event = %+v", sink.sel)
	}
	b.applySelectResult(protocol.SelectResult{ID: sink.sel.ID, Index: 0})
	if !sel.Children[0].HasAttr("selected") || sel.Children[1].HasAttr("selected") {
		t.Fatal("select result did not commit the option")
	}
	if b.selectMenu != nil || b.selectFrontend {
		t.Fatal("select menu stayed open")
	}
}

type modalSink struct {
	download protocol.Download
	notice   protocol.Notice
}

func (s *modalSink) Send(ev protocol.Event) error {
	switch v := ev.Body.(type) {
	case protocol.Download:
		s.download = v
	case protocol.Notice:
		s.notice = v
	}
	return nil
}

func TestDownloadAndNoticeTransport(t *testing.T) {
	b := newTestSession(60, 10)
	b.NoDraw = true
	sink := &modalSink{}
	b.sessionOut = sink
	b.showDownload("https://example.com/f.bin", "f.bin")
	if sink.download.Command == "" || sink.download.Name != "f.bin" || !b.downloadFrontend {
		t.Fatalf("download = %+v frontend=%v", sink.download, b.downloadFrontend)
	}
	if b.overlayOpen() {
		t.Fatal("front-end download counted as an engine overlay")
	}
	b.applyModalClose(protocol.ModalClose{Kind: protocol.ModalDownload, Copied: true, Chars: 12})
	if b.download != nil || b.downloadFrontend || b.status != "Copied 12 chars" {
		t.Fatalf("download close = %+v %v %q", b.download, b.downloadFrontend, b.status)
	}

	b.showNotice("Notice", "Body text")
	if sink.notice.Title != "Notice" || !b.noticeFrontend {
		t.Fatalf("notice = %+v frontend=%v", sink.notice, b.noticeFrontend)
	}
	b.applyModalClose(protocol.ModalClose{Kind: protocol.ModalNotice})
	if b.notice != nil || b.noticeFrontend {
		t.Fatal("notice stayed open")
	}
}

type hintSink struct{ hints protocol.Hints }

func (s *hintSink) Send(ev protocol.Event) error {
	if v, ok := ev.Body.(protocol.Hints); ok {
		s.hints = v
	}
	return nil
}

func TestHintTransportRoundTrip(t *testing.T) {
	b := hintTestSession(t, `<html><body><p><a href="#one">Documentation</a></p></body></html>`, 50, 20)
	sink := &hintSink{}
	b.sessionOut = sink
	b.HandleKey("f")
	if len(sink.hints.Items) != 1 || sink.hints.Items[0].Label != "d" || !b.HintsFrontend {
		t.Fatalf("hints = %+v frontend=%v", sink.hints, b.HintsFrontend)
	}
	b.applyHintResult(protocol.HintResult{Target: sink.hints.Items[0].Target})
	if b.LinkHints || b.HintsFrontend || b.Focus != 0 {
		t.Fatalf("hint result: active=%v frontend=%v focus=%d", b.LinkHints, b.HintsFrontend, b.Focus)
	}
}

type consoleSink struct {
	view protocol.ConsoleView
	got  bool
}

func (s *consoleSink) Send(ev protocol.Event) error {
	if v, ok := ev.Body.(protocol.ConsoleView); ok {
		s.view, s.got = v, true
	}
	return nil
}

func TestConsoleViewTransport(t *testing.T) {
	b := newTestSession(80, 24)
	sink := &consoleSink{}
	b.sessionOut = sink
	b.toggleConsole()
	if !sink.got || sink.view.Width != 80 || len(sink.view.Tabs) != 5 || !b.consoleOpen {
		t.Fatalf("console view = %+v open=%v", sink.view, b.consoleOpen)
	}
	if sink.view.Pane != 0 || sink.view.Hint == "" {
		t.Fatalf("pane=%d hint=%q", sink.view.Pane, sink.view.Hint)
	}
	b.consolePane = consoleRequests
	sink.got = false
	b.draw()
	if !sink.got || sink.view.Pane != consoleRequests {
		t.Fatalf("request view = %+v", sink.view)
	}
}

func TestConsoleActionInputAndTabs(t *testing.T) {
	b := newTestSession(80, 24)
	b.NoDraw = true
	b.toggleConsole()
	b.applyConsoleAction(protocol.ConsoleAction{Op: protocol.ConsoleFocus})
	if !b.consoleEdit {
		t.Fatal("focus did not open input")
	}
	b.applyConsoleAction(protocol.ConsoleAction{Op: protocol.ConsoleInput, Text: "1+1"})
	if len(b.jsLog) == 0 || b.consoleText != "" {
		t.Fatalf("jsLog = %#v text=%q", b.jsLog, b.consoleText)
	}
	b.applyConsoleAction(protocol.ConsoleAction{Op: protocol.ConsolePane, Pane: consoleRequests})
	if b.consolePane != consoleRequests || b.consoleEdit {
		t.Fatalf("pane=%d edit=%v", b.consolePane, b.consoleEdit)
	}
	b.applyConsoleAction(protocol.ConsoleAction{Op: protocol.ConsoleScroll, Delta: 1})
	if b.reqScroll == 0 {
		t.Fatal("scroll did not change")
	}
	b.applyConsoleAction(protocol.ConsoleAction{Op: protocol.ConsoleClose})
	if b.consoleOpen {
		t.Fatal("console stayed open")
	}
}

func TestConsoleHTMLFindCommand(t *testing.T) {
	b := newTestSession(60, 20)
	b.consoleOpen = true
	b.consolePane = consoleHTML
	b.Page = &Page{RawHTML: "<html>\n<p>Needle</p>\n<p>needle</p>\n</html>"}
	sink := &consoleSink{}
	b.sessionOut = sink
	b.applyFindCommand(protocol.Find{Scope: protocol.FindHTML, Query: "needle"})
	if b.FindScope != findScopeHTML || len(b.FindMatches) != 2 {
		t.Fatalf("scope=%d matches=%d", b.FindScope, len(b.FindMatches))
	}
	b.emitConsoleView()
	if !sink.got || sink.view.FindText != "needle" || sink.view.FindTotal != 2 || len(sink.view.Highlights) == 0 {
		t.Fatalf("console view = %+v", sink.view)
	}
	first := b.FindIdx
	b.applyFindCommand(protocol.Find{Scope: protocol.FindHTML, Next: true})
	if b.FindText != "needle" || len(b.FindMatches) != 2 || b.FindIdx == first {
		t.Fatalf("next: text=%q matches=%d idx=%d first=%d", b.FindText, len(b.FindMatches), b.FindIdx, first)
	}
	b.applyFindCommand(protocol.Find{Scope: protocol.FindHTML, Prev: true})
	if b.FindIdx != first {
		t.Fatalf("prev: idx=%d want %d", b.FindIdx, first)
	}
}

func TestInitAndResizeUseContentGeometry(t *testing.T) {
	b := newTestSession(80, 24)
	if _, err := b.handleCommand(protocol.Command{Body: protocol.Init{
		Width: 80, Height: 24, ContentTop: 2, ContentHeight: 21,
	}}); err != nil {
		t.Fatal(err)
	}
	if top, bottom := b.viewport(); top != 2 || bottom != 23 {
		t.Fatalf("init viewport = %d,%d", top, bottom)
	}
	if _, err := b.handleCommand(protocol.Command{Body: protocol.Resize{
		Width: 100, Height: 40, ContentTop: 2, ContentHeight: 37,
	}}); err != nil {
		t.Fatal(err)
	}
	if top, bottom := b.viewport(); top != 2 || bottom != 39 {
		t.Fatalf("resize viewport = %d,%d", top, bottom)
	}
	if b.Width != 100 || b.Height != 40 {
		t.Fatalf("terminal = %dx%d", b.Width, b.Height)
	}
}
