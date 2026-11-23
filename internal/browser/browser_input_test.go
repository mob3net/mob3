package browser

import (
	"strings"
	"testing"
)

func TestEscapeShowsExitHint(t *testing.T) {
	b := NewBrowser(80, 10)
	b.Layout = &Layout{Height: 10}
	b.HandleKey("esc")
	if b.status != exitHint {
		t.Fatalf("status = %q, want %q", b.status, exitHint)
	}
	left := b.footerModel().left
	for _, want := range []string{"q Quit", "? Help"} {
		if !strings.Contains(left, want) {
			t.Errorf("footer = %q, want %q", left, want)
		}
	}
}

func TestEscapeFromInputShowsExitHint(t *testing.T) {
	doc := ParseHTML(`<html><body><input name="q"></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(80, 10)
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.rebuild()
	q := inputByName(doc, "q")
	if q == nil {
		t.Fatal("input not found")
	}
	b.setInputFocus(q)
	b.HandleKey("esc")
	if b.Forms.InputFocus != nil {
		t.Fatal("escape did not leave the input")
	}
	if b.status != exitHint {
		t.Fatalf("status = %q, want %q", b.status, exitHint)
	}
}

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
		{"<8;1;1", false, MouseEvent{Button: 8, X: 1, Y: 1, Press: true}, true},
		{"<9;1;1", false, MouseEvent{Button: 9, X: 1, Y: 1, Press: true}, true},
		{"<128;1;1", false, MouseEvent{Button: 8, X: 1, Y: 1, Press: true}, true},
		{"<129;1;1", false, MouseEvent{Button: 9, X: 1, Y: 1, Press: true}, true},
		{"<136;1;1", false, MouseEvent{Button: 8, X: 1, Y: 1, Press: true}, true},
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
	ev, ok = parseX10Mouse(32+8, 32+1, 32+1)
	if !ok || ev != (MouseEvent{Button: MouseButtonBack, X: 1, Y: 1, Press: true}) {
		t.Fatalf("back button = %+v,%v", ev, ok)
	}
	ev, ok = parseX10Mouse(32+128, 32+1, 32+1)
	if !ok || ev != (MouseEvent{Button: MouseButtonBack, X: 1, Y: 1, Press: true}) {
		t.Fatalf("xterm back button = %+v,%v", ev, ok)
	}
}

func TestMouseSideButtonsAreFrontendOwned(t *testing.T) {
	b := NewBrowser(40, 10)
	b.HandleMouse(MouseEvent{Button: MouseButtonBack, X: 5, Y: 5, Press: true})
	b.HandleMouse(MouseEvent{Button: MouseButtonForward, X: 5, Y: 5, Press: true})
	if b.status != "" {
		t.Fatalf("engine handled side buttons: status = %q", b.status)
	}
}

func TestMouseTokenRoundTrip(t *testing.T) {
	cases := []MouseEvent{
		{Button: 0, X: 3, Y: 7, Press: true},
		{Button: 2, X: 1, Y: 2, Press: false},
		{Button: MouseButtonBack, X: 2, Y: 3, Press: true},
		{Button: MouseButtonForward, X: 2, Y: 3, Press: true},
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

func TestShiftMouseTokenRoundTrip(t *testing.T) {
	want := MouseEvent{Button: MouseButtonLeft, X: 4, Y: 9, Press: true, Shift: true}
	got, ok := ParseMouseToken(mouseToken(want))
	if !ok || got != want {
		t.Fatalf("round trip = %+v, %v; want %+v", got, ok, want)
	}
}

func TestEditLineKeys(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		caret     int
		key       string
		cfg       editConfig
		value     string
		caretWant int
		handled   bool
	}{
		{name: "left", in: "abcd", caret: 2, key: "left", value: "abcd", caretWant: 1, handled: true},
		{name: "right stops at end", in: "abcd", caret: 4, key: "right", value: "abcd", caretWant: 4, handled: true},
		{name: "ctrl-b", in: "abcd", caret: 3, key: "ctrl-b", value: "abcd", caretWant: 2, handled: true},
		{name: "word right", in: "one two", caret: 0, key: "alt-f", value: "one two", caretWant: 3, handled: true},
		{name: "word left", in: "one two", caret: 7, key: "alt-b", value: "one two", caretWant: 4, handled: true},
		{name: "ctrl-left word left", in: "one two", caret: 7, key: "ctrl-left", value: "one two", caretWant: 4, handled: true},
		{name: "home", in: "abcd", caret: 2, key: "home", value: "abcd", caretWant: 0, handled: true},
		{name: "end", in: "abcd", caret: 1, key: "end", value: "abcd", caretWant: 4, handled: true},
		{name: "ctrl-e", in: "abcd", caret: 1, key: "ctrl-e", value: "abcd", caretWant: 4, handled: true},
		{name: "backspace", in: "abcd", caret: 2, key: "backspace", value: "acd", caretWant: 1, handled: true},
		{name: "delete", in: "abcd", caret: 2, key: "delete", value: "abd", caretWant: 2, handled: true},
		{name: "ctrl-w", in: "one two", caret: 7, key: "ctrl-w", value: "one ", caretWant: 4, handled: true},
		{name: "alt-d", in: "one two", caret: 3, key: "alt-d", value: "one", caretWant: 3, handled: true},
		{name: "ctrl-u", in: "abcd", caret: 2, key: "ctrl-u", value: "", caretWant: 0, handled: true},
		{name: "ctrl-k", in: "abcd", caret: 2, key: "ctrl-k", value: "ab", caretWant: 2, handled: true},
		{name: "type", in: "abcd", caret: 2, key: "X", value: "abXcd", caretWant: 3, handled: true},
		{name: "type multibyte", in: "ab", caret: 1, key: "é", value: "aéb", caretWant: 3, handled: true},
		{name: "enter belongs to the caller", in: "abcd", caret: 2, key: "enter", value: "abcd", caretWant: 2, handled: false},
		{name: "escape belongs to the caller", in: "abcd", caret: 2, key: "esc", value: "abcd", caretWant: 2, handled: false},
		{name: "up belongs to the caller", in: "abcd", caret: 2, key: "up", value: "abcd", caretWant: 2, handled: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, caret, handled := editLineKey(tc.in, tc.caret, tc.key, tc.cfg)
			if value != tc.value || caret != tc.caretWant || handled != tc.handled {
				t.Fatalf("editLineKey(%q,%d,%q) = %q,%d,%v; want %q,%d,%v",
					tc.in, tc.caret, tc.key, value, caret, handled, tc.value, tc.caretWant, tc.handled)
			}
		})
	}
}

func TestEditLineSelection(t *testing.T) {
	sel := true
	cfg := editConfig{selAll: &sel, collapseSelection: true}
	value, caret, handled := editLineKey("abcd", 4, "left", cfg)
	if value != "abcd" || caret != 0 || !handled || sel {
		t.Fatalf("left over a selection = %q,%d,%v sel=%v; want the caret at the start and no selection",
			value, caret, handled, sel)
	}

	sel = true
	cfg = editConfig{selAll: &sel}
	if _, caret, _ = editLineKey("abcd", 4, "left", cfg); caret != 3 {
		t.Fatalf("left without collapse = %d, want 3", caret)
	}

	sel = true
	value, caret, _ = editLineKey("abcd", 4, "X", editConfig{selAll: &sel})
	if value != "X" || caret != 1 || sel {
		t.Fatalf("typing over a selection = %q,%d sel=%v; want X,1,false", value, caret, sel)
	}

	sel = true
	value, caret, _ = editLineKey("abcd", 0, "backspace", editConfig{selAll: &sel})
	if value != "" || caret != 0 || sel {
		t.Fatalf("backspace over a selection = %q,%d sel=%v; want empty", value, caret, sel)
	}

	sel = false
	if _, _, handled = editLineKey("abcd", 2, "ctrl-a", editConfig{selAll: &sel}); !handled || !sel {
		t.Fatalf("ctrl-a = handled %v sel %v; want both true", handled, sel)
	}
	if _, _, handled = editLineKey("abcd", 2, "ctrl-a", editConfig{}); handled {
		t.Fatal("ctrl-a without a selection model must fall through to the caller")
	}
}

func TestEditLineMaxRunes(t *testing.T) {
	value, caret, handled := editLineKey("abcd", 4, "X", editConfig{maxRunes: 4})
	if value != "abcd" || caret != 4 || !handled {
		t.Fatalf("typing at the cap = %q,%d,%v; want the value unchanged", value, caret, handled)
	}
	if value, _, _ := editLineKey("abc", 3, "X", editConfig{maxRunes: 4}); value != "abcX" {
		t.Fatalf("typing below the cap = %q, want abcX", value)
	}
}
