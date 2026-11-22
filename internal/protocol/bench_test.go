package protocol

import (
	"context"
	"testing"
)

// BenchmarkCodecFrame measures the steady-state cost of turning one full
// 152x35 content frame into bytes and back. Phase 0 records it as the baseline
// for the later transport split.
func BenchmarkCodecFrame(b *testing.B) {
	const w, h = 152, 35
	f := Frame{
		Version:    1,
		W:          w,
		H:          h,
		Cells:      make([]Cell, w*h),
		Runs:       []ScaledRun{},
		Placements: []Placement{},
	}
	for i := range f.Cells {
		f.Cells[i] = Cell{
			R:     rune('a' + i%26),
			FG:    Color{R: 200, G: 210, B: 220, A: 255, Set: true},
			BG:    Color{R: 10, G: 20, B: 30, A: 255, Set: true},
			Attrs: AttrBold,
			Flags: 0,
		}
	}
	ev := Event{Session: 1, Body: f}
	buf := make([]byte, 0, 160<<10)
	b.ReportAllocs()
	b.SetBytes(int64(w * h))
	for i := 0; i < b.N; i++ {
		var err error
		buf, err = AppendEvent(buf[:0], ev, DefaultLimits)
		if err != nil {
			b.Fatal(err)
		}
		if _, _, err := DecodeEvent(buf, DefaultLimits); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkInProcRoundTrip measures one command and one event through the
// in-process transport, including both codec passes and the channel hops.
func BenchmarkInProcRoundTrip(b *testing.B) {
	h := HandlerFunc(func(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error {
		for {
			select {
			case c := <-in:
				_ = out.Send(Event{Body: State{URL: c.Body.(Navigate).URL, Title: "t"}})
			case <-ctx.Done():
				return nil
			}
		}
	})
	s, err := (&InProc{}).Open(context.Background(), 1, h)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	cmd := Command{Body: Navigate{URL: "https://example.test/a"}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.Send(cmd); err != nil {
			b.Fatal(err)
		}
		if _, ok := <-s.Events(); !ok {
			b.Fatal("event stream closed")
		}
	}
}

// BenchmarkInProcFrameHandoff measures one validated frame copy through the
// in-process event queue, the path used for interactive repaints.
func BenchmarkInProcFrameHandoff(b *testing.B) {
	const w, h = 100, 35
	f := Frame{Version: 1, W: w, H: h, Cells: make([]Cell, w*h)}
	for i := range f.Cells {
		f.Cells[i] = Cell{R: rune('a' + i%26), FG: Color{R: 30, G: 40, B: 50, A: 255, Set: true}}
	}
	hnd := HandlerFunc(func(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error {
		for i := 0; i < b.N; i++ {
			if err := out.Send(Event{Body: f}); err != nil {
				return err
			}
		}
		return nil
	})
	s, err := (&InProc{EventQueue: 8}).Open(context.Background(), 1, hnd)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := <-s.Events(); !ok {
			b.Fatal("event stream closed")
		}
	}
}
