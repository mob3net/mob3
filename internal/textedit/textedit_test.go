package textedit

import "testing"

func TestEditLineKeyBasics(t *testing.T) {
	value, caret, handled := EditLineKey("abcd", 4, "left", EditConfig{})
	if !handled || value != "abcd" || caret != 3 {
		t.Fatalf("left = %q,%d,%v", value, caret, handled)
	}
	value, caret, handled = EditLineKey("abcd", 4, "X", EditConfig{})
	if !handled || value != "abcdX" || caret != 5 {
		t.Fatalf("insert = %q,%d,%v", value, caret, handled)
	}
	all := true
	value, _, handled = EditLineKey("abcd", 4, "z", EditConfig{SelAll: &all})
	if !handled || value != "z" || all {
		t.Fatalf("select-all replacement = %q all=%v", value, all)
	}
	if _, _, handled = EditLineKey("abc", 3, "X", EditConfig{MaxRunes: 3}); !handled {
		t.Fatal("max-rune insert should be handled but rejected")
	}
}

func TestEditLineKeys(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		caret     int
		key       string
		cfg       EditConfig
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
			value, caret, handled := EditLineKey(tc.in, tc.caret, tc.key, tc.cfg)
			if value != tc.value || caret != tc.caretWant || handled != tc.handled {
				t.Fatalf("EditLineKey(%q,%d,%q) = %q,%d,%v; want %q,%d,%v",
					tc.in, tc.caret, tc.key, value, caret, handled, tc.value, tc.caretWant, tc.handled)
			}
		})
	}
}

func TestEditLineSelection(t *testing.T) {
	sel := true
	cfg := EditConfig{SelAll: &sel, CollapseSelection: true}
	value, caret, handled := EditLineKey("abcd", 4, "left", cfg)
	if value != "abcd" || caret != 0 || !handled || sel {
		t.Fatalf("left over a selection = %q,%d,%v sel=%v; want the caret at the start and no selection",
			value, caret, handled, sel)
	}

	sel = true
	cfg = EditConfig{SelAll: &sel}
	if _, caret, _ = EditLineKey("abcd", 4, "left", cfg); caret != 3 {
		t.Fatalf("left without collapse = %d, want 3", caret)
	}

	sel = true
	value, caret, _ = EditLineKey("abcd", 4, "X", EditConfig{SelAll: &sel})
	if value != "X" || caret != 1 || sel {
		t.Fatalf("typing over a selection = %q,%d sel=%v; want X,1,false", value, caret, sel)
	}

	sel = true
	value, caret, _ = EditLineKey("abcd", 0, "backspace", EditConfig{SelAll: &sel})
	if value != "" || caret != 0 || sel {
		t.Fatalf("backspace over a selection = %q,%d sel=%v; want empty", value, caret, sel)
	}

	sel = false
	if _, _, handled = EditLineKey("abcd", 2, "ctrl-a", EditConfig{SelAll: &sel}); !handled || !sel {
		t.Fatalf("ctrl-a = handled %v sel %v; want both true", handled, sel)
	}
	if _, _, handled = EditLineKey("abcd", 2, "ctrl-a", EditConfig{}); handled {
		t.Fatal("ctrl-a without a selection model must fall through to the caller")
	}
}

func TestEditLineMaxRunes(t *testing.T) {
	value, caret, handled := EditLineKey("abcd", 4, "X", EditConfig{MaxRunes: 4})
	if value != "abcd" || caret != 4 || !handled {
		t.Fatalf("typing at the cap = %q,%d,%v; want the value unchanged", value, caret, handled)
	}
	if value, _, _ := EditLineKey("abc", 3, "X", EditConfig{MaxRunes: 4}); value != "abcX" {
		t.Fatalf("typing below the cap = %q, want abcX", value)
	}
}
