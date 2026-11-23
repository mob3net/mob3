package engine

import "testing"

func TestInputLineHelpers(t *testing.T) {
	s := "ab\ncde\nf"
	if got := InputLineStart(s, 5); got != 3 {
		t.Fatalf("line start = %d, want 3", got)
	}
	if got := InputLineEnd(s, 4); got != 6 {
		t.Fatalf("line end = %d, want 6", got)
	}
	if got := TextareaVerticalOffset(s, 4, -1); got != 1 {
		t.Fatalf("up = %d, want 1", got)
	}
	if got := TextareaVerticalOffset(s, 4, 1); got != 8 {
		t.Fatalf("down = %d, want 8", got)
	}
}

func TestTabControlPredicates(t *testing.T) {
	input := &Node{Type: ElementNode, Tag: "input", Attrs: []Attr{{Key: "type", Value: "hidden"}}}
	if IsTabControl(input) {
		t.Fatal("hidden input is tabbable")
	}
	input.Attrs[0].Value = "text"
	if !IsTabControl(input) {
		t.Fatal("text input is not tabbable")
	}
	if !IsToggleControl(&Node{Type: ElementNode, Tag: "input", Attrs: []Attr{{Key: "type", Value: "checkbox"}}}) {
		t.Fatal("checkbox is not toggle")
	}
}
