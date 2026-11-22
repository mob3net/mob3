package browser

import (
	"context"
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
