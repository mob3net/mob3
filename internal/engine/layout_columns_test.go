package engine

import (
	"testing"
)

func columnItems(n int) string {
	names := []string{"One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine", "Ten", "Eleven", "Twelve"}
	out := ""
	for i := 0; i < n && i < len(names); i++ {
		out += "<li>" + names[i] + "</li>"
	}
	return out
}

func findTextFrag(t *testing.T, ly *Layout, text string) *Frag {
	t.Helper()
	for i := range ly.Frags {
		if ly.Frags[i].Kind == FragText && ly.Frags[i].Text == text {
			return &ly.Frags[i]
		}
	}
	t.Fatalf("missing text %q", text)
	return nil
}

func TestMultiColumnBlockChildren(t *testing.T) {
	doc := ParseHTML(`<html><body style="margin:0"><ul style="columns:3;column-gap:8px;list-style:none;margin:0;padding:0">` +
		columnItems(9) + `</ul></body></html>`)
	ly := BuildLayout(doc, nil, 60)
	one := findTextFrag(t, ly, "One")
	two := findTextFrag(t, ly, "Two")
	four := findTextFrag(t, ly, "Four")
	seven := findTextFrag(t, ly, "Seven")
	if two.X != one.X || two.Y <= one.Y {
		t.Fatalf("first column did not stack: one=%d,%d two=%d,%d", one.X, one.Y, two.X, two.Y)
	}
	if four.X <= one.X || four.Y != one.Y {
		t.Fatalf("second column not beside the first: one=%d,%d four=%d,%d", one.X, one.Y, four.X, four.Y)
	}
	if seven.X <= four.X || seven.Y != one.Y {
		t.Fatalf("third column not beside the second: four=%d,%d seven=%d,%d", four.X, four.Y, seven.X, seven.Y)
	}
}

func TestMultiColumnWidthResolvesCount(t *testing.T) {
	// 10rem is 20 cells; with a one-cell gap an 80-cell row fits three columns.
	doc := ParseHTML(`<html><body style="margin:0"><ul style="columns:10rem;column-gap:8px;list-style:none;margin:0;padding:0">` +
		columnItems(9) + `</ul></body></html>`)
	ly := BuildLayout(doc, nil, 80)
	one := findTextFrag(t, ly, "One")
	four := findTextFrag(t, ly, "Four")
	seven := findTextFrag(t, ly, "Seven")
	if four.X <= one.X || seven.X <= four.X {
		t.Fatalf("column-width did not create three columns: %d %d %d", one.X, four.X, seven.X)
	}
}
