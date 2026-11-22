package frontend

import (
	"context"
	"strings"
	"testing"

	"browser/internal/protocol"
)

// TestInProcFrameReachesFrontend exercises the value seam end to end: an engine
// handler emits a frame, the transport validates it through the codec, and the
// front end encodes the decoded frame.
func TestInProcFrameReachesFrontend(t *testing.T) {
	h := protocol.HandlerFunc(func(ctx context.Context, abort <-chan struct{}, in <-chan protocol.Command, out chan<- protocol.Event) error {
		out <- protocol.Event{Body: protocol.Frame{
			Version: 1,
			W:       3,
			H:       1,
			Cells:   []protocol.Cell{{R: 'o'}, {R: 'k'}, {R: '!'}},
		}}
		return nil
	})
	s, err := (&protocol.InProc{}).Open(context.Background(), 1, h)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ev, ok := <-s.Events()
	if !ok {
		t.Fatal("event stream closed")
	}
	f, ok := ev.Body.(protocol.Frame)
	if !ok {
		t.Fatalf("event body = %T, want Frame", ev.Body)
	}
	lines := EncodeFrame(f, false, false)
	if len(lines) != 1 || !strings.Contains(lines[0], "ok!") {
		t.Fatalf("encoded frame = %q", lines)
	}
}
