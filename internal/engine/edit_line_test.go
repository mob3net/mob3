package engine

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
