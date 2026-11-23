package engine

import (
	"strings"
	"testing"
)

func TestSelectionOrderTextAndSummary(t *testing.T) {
	layout := &Layout{Frags: []Frag{
		{Kind: FragText, X: 0, Y: 0, W: 5, Text: "hello"},
		{Kind: FragText, X: 0, Y: 1, W: 5, Text: "world"},
	}}
	s := Selection{Anchor: CellPos{X: 0, Y: 0}, Cursor: CellPos{X: 4, Y: 1}}
	if !s.Has() {
		t.Fatal("selection is empty")
	}
	a, c := s.Order()
	if a != (CellPos{X: 0, Y: 0}) || c != (CellPos{X: 4, Y: 1}) {
		t.Fatalf("order = %+v %+v", a, c)
	}
	if got := s.Text(layout); got != "hello\nworld" {
		t.Fatalf("text = %q", got)
	}
	if got := s.Summary(layout); !strings.Contains(got, "11 chars") {
		t.Fatalf("summary = %q", got)
	}
	s.Clear()
	if s.Has() || s.Text(layout) != "" {
		t.Fatal("clear did not reset the selection")
	}
}
