package engine

import (
	"context"
	"net/url"
	"testing"
	"time"
)

func TestPageStoppedFooter(t *testing.T) {
	b := NewSession(80, 24)
	b.pageStopped = true
	b.status = "something else"
	if left := b.footerModel().left; left != " Page stopped" {
		t.Fatalf("footer left = %q", left)
	}
	if status := b.stateMessage().Status; status != "Page stopped" {
		t.Fatalf("state status = %q", status)
	}
}

func TestAbortSessionCancelsLoad(t *testing.T) {
	b := NewSession(80, 24)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	b.LoadFn = func(ctx context.Context, _, _ string, _ []byte, _ string) (*Page, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}
	u, _ := url.Parse("https://example.test/")
	b.BeginLoad(b.LoadFn, u, "GET", u.String(), nil, "", 1, true, 0, false, "")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("load did not start")
	}
	b.AbortSession()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("AbortSession did not cancel the load")
	}
}
