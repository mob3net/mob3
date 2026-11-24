package engine

import "testing"

func TestReleaseUnregistersBudgetShare(t *testing.T) {
	bc := NewBrowserContext()
	first := NewSessionWithBrowserContext(80, 24, bc)
	second := NewSessionWithBrowserContext(80, 24, bc)
	before := second.budgetShare.JSHeap
	first.Release()
	if second.budgetShare.JSHeap <= before {
		t.Fatalf("remaining JS share = %d, want more than %d", second.budgetShare.JSHeap, before)
	}
	if first.budgets != nil {
		t.Fatal("released session still holds the budget manager")
	}
	second.Release()
}
