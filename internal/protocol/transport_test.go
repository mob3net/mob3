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

func TestInProcFrameIsDetached(t *testing.T) {
	cells := []Cell{{R: 'a'}}
	h := HandlerFunc(func(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error {
		_ = out.Send(Event{Body: Frame{W: 1, H: 1, Cells: cells}})
		cells[0].R = 'b'
		_ = out.Send(Event{Body: Frame{W: 1, H: 1, Cells: cells}})
		return nil
	})
	s, err := (&InProc{}).Open(context.Background(), 1, h)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first := <-s.Events()
	second := <-s.Events()
	if got := first.Body.(Frame).Cells[0].R; got != 'a' {
		t.Fatalf("first frame changed to %q after the engine reused its buffer", got)
	}
	if got := second.Body.(Frame).Cells[0].R; got != 'b' {
		t.Fatalf("second frame = %q, want b", got)
	}
}
