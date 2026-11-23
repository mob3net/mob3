package engine

import (
	"reflect"
	"testing"

	"browser/internal/protocol"
)

func TestGridToProtocolFrameRoundTrip(t *testing.T) {
	g := NewGrid(8, 4, RGB(255, 255, 255), RGB(0, 0, 0))
	g.Put(0, 0, 'A', &Style{Bold: true, Underline: true}, RGB(10, 20, 30), RGB(1, 2, 3))
	g.TextBig(0, 1, "Big", &Style{FontSize: 32, Bold: true, LineHeight: 1}, RGB(4, 5, 6), RGB(7, 8, 9), 2)

	want := toProtocolFrame(g, 3)
	want.Placements = []protocol.Placement{}
	if err := want.Validate(protocol.DefaultLimits); err != nil {
		t.Fatalf("converted frame invalid: %v", err)
	}
	msg, err := protocol.AppendEvent(nil, protocol.Event{Session: 1, Seq: 1, Body: want}, protocol.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	ev, rest, err := protocol.DecodeEvent(msg, protocol.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 0 {
		t.Fatalf("%d trailing bytes", len(rest))
	}
	got, ok := ev.Body.(protocol.Frame)
	if !ok {
		t.Fatalf("decoded body is %T", ev.Body)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("frame round trip:\n got %#v\nwant %#v", got, want)
	}
}
