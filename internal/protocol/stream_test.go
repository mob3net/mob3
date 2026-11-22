package protocol

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func streamPair(t *testing.T, h Handler) (Session, chan error) {
	t.Helper()
	server, client := net.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- ServeStream(context.Background(), server, 1, h, DefaultLimits, 8)
	}()
	s := DialStream(context.Background(), client, 1, DefaultLimits, 8)
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s, done
}

func TestStreamRoundTrip(t *testing.T) {
	h := HandlerFunc(func(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error {
		for {
			select {
			case c := <-in:
				if n, ok := c.Body.(Navigate); ok {
					_ = out.Send(Event{Body: State{URL: n.URL, Title: "stream"}})
					return nil
				}
			case <-ctx.Done():
				return nil
			}
		}
	})
	s, _ := streamPair(t, h)
	if err := s.Send(Command{Body: Navigate{URL: "https://example.test/"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case ev, ok := <-s.Events():
		if !ok {
			t.Fatal("event stream closed")
		}
		st, ok := ev.Body.(State)
		if !ok || st.URL != "https://example.test/" || st.Title != "stream" {
			t.Fatalf("event = %#v", ev.Body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for state")
	}
}

func TestStreamAbort(t *testing.T) {
	aborted := make(chan struct{})
	h := HandlerFunc(func(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error {
		for {
			select {
			case <-abort:
				close(aborted)
				return nil
			case <-ctx.Done():
				return nil
			}
		}
	})
	s, done := streamPair(t, h)
	s.Abort()
	select {
	case <-aborted:
	case <-time.After(3 * time.Second):
		t.Fatal("abort was not delivered")
	}
	_ = s.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
}

func TestStreamRepairsBadFrame(t *testing.T) {
	h := HandlerFunc(func(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error {
		_ = out.Send(Event{Body: Frame{
			W: 2, H: 1, Cells: []Cell{{R: 'a'}, {R: 'b'}},
			Placements: []Placement{{Key: "x", X: 5, Y: 0, W: 2, H: 1, TotalRows: 1, Format: PlacementPNG, Data: []byte{1}}},
		}})
		return nil
	})
	s, _ := streamPair(t, h)
	first := <-s.Events()
	if _, ok := first.Body.(Crash); !ok {
		t.Fatalf("first event = %T, want Crash", first.Body)
	}
	second := <-s.Events()
	f, ok := second.Body.(Frame)
	if !ok {
		t.Fatalf("second event = %T, want Frame", second.Body)
	}
	if err := f.Validate(DefaultLimits); err != nil {
		t.Fatal(err)
	}
	if len(f.Placements) != 0 {
		t.Fatalf("placement survived: %+v", f.Placements)
	}
}

func TestStreamSendAfterClose(t *testing.T) {
	h := HandlerFunc(func(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error {
		<-ctx.Done()
		return nil
	})
	s, _ := streamPair(t, h)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Send(Command{Body: Navigate{URL: "https://example.test/"}}); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("Send after Close = %v, want ErrSessionClosed", err)
	}
}
