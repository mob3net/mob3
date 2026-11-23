package engine

import (
	"context"
	"testing"
	"time"
)

func TestMemoryBudgetsScaleWithLimit(t *testing.T) {
	const gb = int64(1) << 30
	if got := imageByteBudget(gb); got <= 0 || got > gb {
		t.Fatalf("image budget at 1 GB = %d", got)
	}
	if got := svgPixelBudget(gb); got <= 0 || got*4 > gb {
		t.Fatalf("SVG pixel budget at 1 GB = %d", got)
	}
	if got := imageByteBudget(0); got != fallbackImageBytes {
		t.Fatalf("fallback image budget = %d", got)
	}
}

func TestByteLimiter(t *testing.T) {
	l := NewByteLimiter(100)
	if _, ok := l.Acquire(context.Background(), 60); !ok {
		t.Fatal("first acquire failed")
	}
	done := make(chan bool, 1)
	go func() {
		_, ok := l.Acquire(context.Background(), 60)
		done <- ok
	}()
	select {
	case <-done:
		t.Fatal("second acquire did not wait")
	case <-time.After(20 * time.Millisecond):
	}
	l.Release(60)
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("second acquire was cancelled")
		}
	case <-time.After(time.Second):
		t.Fatal("second acquire did not resume after release")
	}
	l.Release(60)

	// Cancellation while waiting must fail without charging the budget.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, ok := l.Acquire(ctx, 100); !ok {
		t.Fatal("full acquire failed")
	}
	if _, ok := l.Acquire(ctx, 100); ok {
		t.Fatal("cancelled acquire succeeded")
	}
	l.Release(100)
}

func TestByteLimiterClampsLargeReservation(t *testing.T) {
	l := NewByteLimiter(100)
	admitted, ok := l.Acquire(context.Background(), 500)
	if !ok || admitted != 100 {
		t.Fatalf("large reservation = %d, %v; want 100, true", admitted, ok)
	}
	l.Release(admitted)
}
