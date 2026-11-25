package linehistory

import (
	"fmt"
	"testing"
)

func TestRecordAndRecall(t *testing.T) {
	var h History
	h.Record("a")
	h.Record("a") // consecutive duplicate dropped
	h.Record("b")
	got, ok := h.Up("")
	if !ok || got != "b" {
		t.Fatalf("Up = %q, %v; want b", got, ok)
	}
	got, ok = h.Up("b")
	if !ok || got != "a" {
		t.Fatalf("second Up = %q, %v; want a", got, ok)
	}
	if _, ok := h.Up("a"); ok {
		t.Fatal("Up past the oldest entry succeeded")
	}
}

func TestDownRestoresEditedDraft(t *testing.T) {
	var h History
	h.Record("a")
	h.Record("b")
	if got, ok := h.Up(""); !ok || got != "b" {
		t.Fatalf("Up = %q, %v", got, ok)
	}
	// Editing a recalled entry keeps it as the draft.
	got, ok := h.Down("b-edited")
	if !ok || got != "b-edited" {
		t.Fatalf("Down = %q, %v; want the edited draft", got, ok)
	}
	if _, ok := h.Down("b-edited"); ok {
		t.Fatal("Down below the draft succeeded")
	}
}

func TestResetAndCap(t *testing.T) {
	var h History
	for i := 0; i < limit+5; i++ {
		h.Record(fmt.Sprintf("%d", i))
	}
	h.Reset()
	count := 0
	last := ""
	for {
		got, ok := h.Up(last)
		if !ok {
			break
		}
		last = got
		count++
		if count > limit {
			t.Fatalf("recalled more than %d entries", limit)
		}
	}
	if count != limit {
		t.Fatalf("recalled %d entries, want %d", count, limit)
	}
	if last != "5" {
		t.Fatalf("oldest retained entry = %q, want 5", last)
	}
}
