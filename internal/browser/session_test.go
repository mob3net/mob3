package browser

import (
	"bytes"
	"context"
	"image"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"browser/internal/frontend"
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

func openTestSession(t *testing.T, b *Browser) protocol.Session {
	t.Helper()
	s, err := (&protocol.InProc{}).Open(context.Background(), 1, b)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(protocol.Command{Body: protocol.Init{Width: b.width, Height: b.height, CellW: b.cellW, CellH: b.cellH, Zoom: b.zoom}}); err != nil {
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
	b := NewBrowser(60, 10)
	b.noJS = true
	restore := quietStdout(t)
	defer restore()
	s := openTestSession(t, b)
	if err := s.Send(protocol.Command{Body: protocol.Key{Token: "q"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Send(protocol.Command{Body: protocol.Key{Token: "y"}}); err != nil {
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
	b := NewBrowser(60, 10)
	b.noJS = true
	b.page = &Page{Root: root, Title: "t"}
	b.layout = BuildLayout(root, nil, 60)
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
	maxBefore := b.layout.Height - 7
	if maxBefore < 1 {
		t.Fatalf("test layout is too short: %d rows", b.layout.Height)
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
	b := NewBrowser(60, 10)
	b.noJS = true
	restore := quietStdout(t)
	defer restore()
	s := openTestSession(t, b)
	s.Abort()
	// The front end also forwards the Esc key. Either path counts the abort,
	// even when the command is ready before the out-of-band branch is selected.
	if err := s.Send(protocol.Command{Body: protocol.Key{Token: "esc"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Send(protocol.Command{Body: protocol.Key{Token: "q"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Send(protocol.Command{Body: protocol.Key{Token: "y"}}); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, s, func(body protocol.EventBody) bool {
		_, ok := body.(protocol.Quit)
		return ok
	})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got := b.abortGen.Load(); got == 0 {
		t.Fatal("out-of-band abort was not counted")
	}
}

func TestSessionEmitsScreenFrames(t *testing.T) {
	b := NewBrowser(30, 8)
	b.noJS = true
	restore := quietStdout(t)
	defer restore()
	s := openTestSession(t, b)
	defer s.Close()
	if err := s.Send(protocol.Command{Body: protocol.Key{Token: "j"}}); err != nil {
		t.Fatal(err)
	}
	f, ok := waitEvent(t, s, func(body protocol.EventBody) bool {
		_, ok := body.(protocol.Frame)
		return ok
	}).(protocol.Frame)
	if !ok {
		t.Fatalf("frame event has the wrong type")
	}
	if f.W != 30 || f.H != 8 {
		t.Fatalf("frame = %dx%d, want 30x8", f.W, f.H)
	}
	if len(f.Cells) != f.W*f.H {
		t.Fatalf("frame cells = %d, want %d", len(f.Cells), f.W*f.H)
	}
}

func TestSessionAppActionReachesState(t *testing.T) {
	b := NewBrowser(60, 10)
	b.noJS = true
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

func TestDialogReadsTransportCommands(t *testing.T) {
	b := NewBrowser(60, 10)
	b.noDraw = true
	in := make(chan protocol.Command, 4)
	b.sessionCtx = context.Background()
	b.sessionIn = in
	b.sessionAbort = make(chan struct{})

	in <- protocol.Command{Body: protocol.Key{Token: "h"}}
	in <- protocol.Command{Body: protocol.Key{Token: "i"}}
	in <- protocol.Command{Body: protocol.Key{Token: "enter"}}
	text, ok := b.showPrompt("name?", "")
	if !ok || text != "hi" {
		t.Fatalf("prompt = %q, %v; want %q, true", text, ok, "hi")
	}

	in <- protocol.Command{Body: protocol.Key{Token: "n"}}
	if b.showConfirm("sure?") {
		t.Fatal("confirm accepted n")
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
	b := NewBrowser(40, 12)
	b.noJS = true
	b.color = true
	b.page = &Page{Root: ParseHTML(`<html><body></body></html>`), Title: "img"}
	b.layout = &Layout{Height: 8, Frags: []Frag{{Kind: fragImage, X: 1, Y: 0, W: 4, H: 4, Img: img}}}
	b.gfx = NewGFXRenderer(GraphicsKitty, 8, 16)
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

func TestApplySessionEventPresentsNativeFrame(t *testing.T) {
	var out bytes.Buffer
	p := frontend.NewPresenter(&out, true)
	p.SetGraphics(frontend.GraphicsKitty, 8, 16)
	st := &frontEndState{}
	if _, err := applySessionEvent(p, protocol.Event{Body: protocol.State{Flags: protocol.StateNativeImages}}, st, nil); err != nil {
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
	if _, err := applySessionEvent(p, protocol.Event{Body: f}, st, nil); err != nil {
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

func TestSessionClipsOutOfBoundsPlacements(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	b := NewBrowser(10, 8)
	b.noJS = true
	b.noDraw = false
	b.page = &Page{Root: ParseHTML(`<html><body></body></html>`), Title: "clip"}
	b.layout = &Layout{Height: 8, Frags: []Frag{{Kind: fragImage, X: -5, Y: 1, W: 20, H: 3, Img: img}}}
	b.gfx = NewGFXRenderer(GraphicsKitty, 8, 16)
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

func TestApplySessionEventDeduplicatesCrash(t *testing.T) {
	p := frontend.NewPresenter(io.Discard, false)
	st := &frontEndState{}
	var reports []string
	report := func(msg string) { reports = append(reports, msg) }
	crash := protocol.Event{Body: protocol.Crash{Message: "bad\x1b[31m"}}
	if _, err := applySessionEvent(p, crash, st, report); err != nil {
		t.Fatal(err)
	}
	if _, err := applySessionEvent(p, crash, st, report); err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 {
		t.Fatalf("reports = %d, want 1 for repeated crash", len(reports))
	}
	if strings.Contains(reports[0], "\x1b") {
		t.Fatalf("report kept a control sequence: %q", reports[0])
	}
	frame := protocol.Frame{W: 1, H: 1, Cells: []protocol.Cell{{R: 'x'}}}
	if _, err := applySessionEvent(p, protocol.Event{Body: frame}, st, report); err != nil {
		t.Fatal(err)
	}
	if _, err := applySessionEvent(p, crash, st, report); err != nil {
		t.Fatal(err)
	}
	if len(reports) != 2 {
		t.Fatalf("reports = %d, want 2 after a good frame re-armed reporting", len(reports))
	}
}

func TestClientErrorLogsOnce(t *testing.T) {
	b := NewBrowser(40, 10)
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
