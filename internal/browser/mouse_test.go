package browser

import "testing"

func TestParseSGRMouse(t *testing.T) {
	cases := []struct {
		params  string
		release bool
		want    MouseEvent
		ok      bool
	}{
		{"<0;10;5", false, MouseEvent{Button: 0, X: 10, Y: 5, Press: true}, true},
		{"<0;10;5", true, MouseEvent{Button: 0, X: 10, Y: 5, Press: false}, true},
		{"<2;1;1", false, MouseEvent{Button: 2, X: 1, Y: 1, Press: true}, true},
		{"<64;3;4", false, MouseEvent{Button: 0, X: 3, Y: 4, Press: true, Wheel: -1}, true},
		{"<65;3;4", false, MouseEvent{Button: 1, X: 3, Y: 4, Press: true, Wheel: 1}, true},
		{"<32;3;4", false, MouseEvent{Button: 0, X: 3, Y: 4, Press: true, Motion: true}, true},
		{"bad", false, MouseEvent{}, false},
	}
	for _, c := range cases {
		got, ok := parseSGRMouse(c.params, c.release)
		if ok != c.ok || got != c.want {
			t.Errorf("parseSGRMouse(%q,%v) = %+v,%v want %+v,%v", c.params, c.release, got, ok, c.want, c.ok)
		}
	}
}

func TestParseX10Mouse(t *testing.T) {
	ev, ok := parseX10Mouse(32, 32+10, 32+5)
	if !ok || ev != (MouseEvent{Button: 0, X: 10, Y: 5, Press: true}) {
		t.Fatalf("left press = %+v,%v", ev, ok)
	}
	ev, ok = parseX10Mouse(35, 32+2, 32+3)
	if !ok || ev != (MouseEvent{Button: 0, X: 2, Y: 3, Press: false}) {
		t.Fatalf("release = %+v,%v", ev, ok)
	}
	ev, ok = parseX10Mouse(32+64, 32+1, 32+1)
	if !ok || ev.Wheel != -1 {
		t.Fatalf("wheel up = %+v,%v", ev, ok)
	}
}

func TestMouseTokenRoundTrip(t *testing.T) {
	cases := []MouseEvent{
		{Button: 0, X: 3, Y: 7, Press: true},
		{Button: 2, X: 1, Y: 2, Press: false},
		{Button: 0, X: 4, Y: 8, Press: true, Motion: true},
		{Wheel: -1, X: 9, Y: 4},
		{Wheel: 1, X: 5, Y: 6},
	}
	for _, want := range cases {
		got, ok := ParseMouseToken(mouseToken(want))
		if !ok || got != want {
			t.Errorf("round trip %+v -> %q -> %+v,%v", want, mouseToken(want), got, ok)
		}
	}
	if _, ok := ParseMouseToken("j"); ok {
		t.Error("plain key decoded as mouse")
	}
}
