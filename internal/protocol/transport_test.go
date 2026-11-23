package protocol

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestInProcTransportRoundTrip(t *testing.T) {
	var seen Command
	got := make(chan Command, 1)
	h := HandlerFunc(func(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error {
		select {
		case c := <-in:
			seen = c
			got <- c
			_ = out.Send(Event{Body: State{URL: c.Body.(Navigate).URL, Title: "decoded"}})
		case <-ctx.Done():
		}
		return nil
	})
	tr := &InProc{}
	s, err := tr.Open(context.Background(), 7, h)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.Send(Command{Body: Navigate{URL: "https://example.test/a"}}); err != nil {
		t.Fatal(err)
	}
	c := <-got
	if c.Session != 7 {
		t.Fatalf("handler session = %d, want 7", c.Session)
	}
	ev, ok := <-s.Events()
	if !ok {
		t.Fatal("event stream closed")
	}
	st, ok := ev.Body.(State)
	if !ok {
		t.Fatalf("event body = %T, want State", ev.Body)
	}
	if st.URL != "https://example.test/a" || st.Title != "decoded" {
		t.Fatalf("state = %+v", st)
	}
	if ev.Session != 7 {
		t.Fatalf("event session = %d, want 7", ev.Session)
	}
	// The handler sees a decoded copy, not the caller's struct.
	if seen.Seq != 0 {
		t.Fatalf("handler seq = %d, want 0", seen.Seq)
	}
}

func TestInProcTransportContainsHandlerPanic(t *testing.T) {
	h := HandlerFunc(func(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error {
		panic("boom")
	})
	s, err := (&InProc{}).Open(context.Background(), 3, h)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var crash Crash
	for ev := range s.Events() {
		if c, ok := ev.Body.(Crash); ok {
			crash = c
		}
	}
	if !strings.Contains(crash.Message, "boom") {
		t.Fatalf("crash message = %q, want the panic value", crash.Message)
	}
}

func TestInProcTransportRejectsInvalidCommand(t *testing.T) {
	called := make(chan struct{}, 1)
	h := HandlerFunc(func(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error {
		<-ctx.Done()
		called <- struct{}{}
		return nil
	})
	s, err := (&InProc{}).Open(context.Background(), 1, h)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.Send(Command{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Send error = %v, want ErrInvalid", err)
	}
}

func TestInProcTransportAbort(t *testing.T) {
	ready := make(chan struct{})
	aborted := make(chan struct{}, 2)
	h := HandlerFunc(func(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error {
		close(ready)
		for {
			select {
			case <-abort:
				aborted <- struct{}{}
			case <-ctx.Done():
				return nil
			}
		}
	})
	s, err := (&InProc{}).Open(context.Background(), 1, h)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	<-ready
	s.Abort()
	s.Abort() // coalesces while the first is pending, but never blocks
	select {
	case <-aborted:
	case <-time.After(2 * time.Second):
		t.Fatal("abort was not delivered")
	}
}

func TestInProcTransportRejectsUnsafeEvent(t *testing.T) {
	h := HandlerFunc(func(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error {
		// A frame whose scaled run carries an escape sequence must never reach
		// the front end as page content.
		_ = out.Send(Event{Body: Frame{
			W: 1, H: 1,
			Cells: []Cell{{R: 'x'}},
			Runs:  []ScaledRun{{X: 0, Y: 0, Cells: 1, Scale: 2, Text: "\x1b[31m"}},
		}})
		return nil
	})
	s, err := (&InProc{}).Open(context.Background(), 3, h)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ev, ok := <-s.Events()
	if !ok {
		t.Fatal("event stream closed")
	}
	cr, ok := ev.Body.(Crash)
	if !ok {
		t.Fatalf("event body = %T, want Crash", ev.Body)
	}
	if !strings.Contains(cr.Message, "invalid engine event") {
		t.Fatalf("crash = %q", cr.Message)
	}
}

func TestInProcTransportClose(t *testing.T) {
	h := HandlerFunc(func(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error {
		<-ctx.Done()
		return nil
	})
	s, err := (&InProc{}).Open(context.Background(), 1, h)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-s.Events(); ok {
		t.Fatal("event stream still open after Close")
	}
	if err := s.Send(Command{Body: Navigate{URL: "https://example.test/"}}); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("Send after Close = %v, want ErrSessionClosed", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
}

type abortCountingHandler struct{ aborts atomic.Int32 }

func (h *abortCountingHandler) AbortSession() { h.aborts.Add(1) }
func (h *abortCountingHandler) Serve(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error {
	<-ctx.Done()
	return nil
}

func TestInProcAbortCallsHandlerDirectly(t *testing.T) {
	h := &abortCountingHandler{}
	s, err := (&InProc{}).Open(context.Background(), 1, h)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Abort()
	if got := h.aborts.Load(); got != 1 {
		t.Fatalf("aborts = %d, want 1 before the loop wakes", got)
	}
}

func TestCopyFrameDetaches(t *testing.T) {
	cells := []Cell{{R: 'a'}}
	f := Frame{W: 1, H: 1, Cells: cells}
	copied, err := copyFrame(f, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	cells[0].R = 'b'
	if got := copied.Cells[0].R; got != 'a' {
		t.Fatalf("copied frame changed to %q after the engine reused its buffer", got)
	}
}

func TestInProcCoalescesStaleFrames(t *testing.T) {
	const n = 20
	done := make(chan struct{})
	h := HandlerFunc(func(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error {
		for i := 0; i < n; i++ {
			_ = out.Send(Event{Body: Frame{W: 1, H: 1, Cells: []Cell{{R: rune('a' + i)}}}})
		}
		close(done)
		return nil
	})
	s, err := (&InProc{EventQueue: 2}).Open(context.Background(), 1, h)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	<-done
	frames := 0
	last := rune(0)
	for ev := range s.Events() {
		if f, ok := ev.Body.(Frame); ok {
			frames++
			last = f.Cells[0].R
		}
	}
	if frames == 0 || frames >= n {
		t.Fatalf("frames delivered = %d, want at least one and fewer than %d", frames, n)
	}
	if last != rune('a'+n-1) {
		t.Fatalf("last frame = %q, want %q", last, rune('a'+n-1))
	}
}
func TestInProcRepairsInvalidFrame(t *testing.T) {
	h := HandlerFunc(func(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error {
		_ = out.Send(Event{Body: Frame{
			W: 2, H: 1,
			Cells:      []Cell{{R: 'a'}, {R: 'b'}},
			Runs:       []ScaledRun{{X: 0, Y: 0, Cells: 2, Scale: 2, Text: "\x1b[31m"}},
			Placements: []Placement{{Key: "x", X: 5, Y: 0, W: 2, H: 1, TotalRows: 1, Format: PlacementPNG, Data: []byte{1}}},
		}})
		return nil
	})
	s, err := (&InProc{}).Open(context.Background(), 1, h)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	first := <-s.Events()
	if _, ok := first.Body.(Crash); !ok {
		t.Fatalf("first event = %T, want Crash", first.Body)
	}
	second := <-s.Events()
	f, ok := second.Body.(Frame)
	if !ok {
		t.Fatalf("second event = %T, want repaired Frame", second.Body)
	}
	if err := f.Validate(DefaultLimits); err != nil {
		t.Fatalf("repaired frame is invalid: %v", err)
	}
	if len(f.Placements) != 0 {
		t.Fatalf("invalid placement survived repair: %+v", f.Placements)
	}
	if len(f.Runs) != 0 || f.Cells[0].Run != 0 || f.Cells[1].Run != 0 {
		t.Fatalf("invalid run survived repair: %+v cells=%+v", f.Runs, f.Cells)
	}
}

func TestInProcDropsUnrepairableFrame(t *testing.T) {
	h := HandlerFunc(func(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error {
		_ = out.Send(Event{Body: Frame{W: 2, H: 2, Cells: []Cell{{R: 'a'}}}})
		return nil
	})
	s, err := (&InProc{}).Open(context.Background(), 1, h)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ev := <-s.Events()
	if _, ok := ev.Body.(Crash); !ok {
		t.Fatalf("event = %T, want Crash", ev.Body)
	}
}
