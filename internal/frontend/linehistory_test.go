package frontend

import (
	"fmt"
	"testing"
)

func TestLineHistoryRecordAndRecall(t *testing.T) {
	var h lineHistory
	h.record("a")
	h.record("a") // consecutive duplicate dropped
	h.record("b")
	got, ok := h.up("")
	if !ok || got != "b" {
		t.Fatalf("up = %q, %v; want b", got, ok)
	}
	got, ok = h.up("b")
	if !ok || got != "a" {
		t.Fatalf("second up = %q, %v; want a", got, ok)
	}
	if _, ok := h.up("a"); ok {
		t.Fatal("up past the oldest entry succeeded")
	}
}

func TestLineHistoryDownRestoresEditedDraft(t *testing.T) {
	var h lineHistory
	h.record("a")
	h.record("b")
	if got, ok := h.up(""); !ok || got != "b" {
		t.Fatalf("up = %q, %v", got, ok)
	}
	// Editing a recalled entry keeps it as the draft.
	got, ok := h.down("b-edited")
	if !ok || got != "b-edited" {
		t.Fatalf("down = %q, %v; want the edited draft", got, ok)
	}
	if _, ok := h.down("b-edited"); ok {
		t.Fatal("down below the draft succeeded")
	}
}

func TestLineHistoryResetAndCap(t *testing.T) {
	var h lineHistory
	for i := 0; i < lineHistoryLimit+5; i++ {
		h.record(fmt.Sprintf("%d", i))
	}
	h.reset()
	count := 0
	last := ""
	for {
		got, ok := h.up(last)
		if !ok {
			break
		}
		last = got
		count++
		if count > lineHistoryLimit {
			t.Fatalf("recalled more than %d entries", lineHistoryLimit)
		}
	}
	if count != lineHistoryLimit {
		t.Fatalf("recalled %d entries, want %d", count, lineHistoryLimit)
	}
	if last != "5" {
		t.Fatalf("oldest retained entry = %q, want 5", last)
	}
}
