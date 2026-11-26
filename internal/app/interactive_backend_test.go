package app

import (
	"testing"

	"browser/internal/engine"
	"browser/internal/host"
	"browser/internal/protocol"
)

type backendTestSession struct{}

func (backendTestSession) Send(protocol.Command) error   { return nil }
func (backendTestSession) Events() <-chan protocol.Event { return nil }
func (backendTestSession) Abort()                        {}
func (backendTestSession) Close() error                  { return nil }

type backendTestBackend struct {
	session protocol.Session
	closed  int
	killed  int
}

func (b *backendTestBackend) Session() protocol.Session { return b.session }
func (b *backendTestBackend) Close()                    { b.closed++ }
func (b *backendTestBackend) Kill()                     { b.killed++ }

func TestInteractiveTabsKillAndCloseBackend(t *testing.T) {
	tabs := newInteractiveTabs(host.NewSession(), engine.LoaderConfig{}, "about:home", 8, 16, nil)
	first := &backendTestBackend{session: backendTestSession{}}
	second := &backendTestBackend{session: backendTestSession{}}
	next := first
	tabs.newBackend = func(*engine.Loader, protocol.SessionID, protocol.Init, string) (tabBackend, error) {
		backend := next
		if next == first {
			next = second
		}
		return backend, nil
	}
	loader := &engine.Loader{}
	tab1, err := tabs.openWithLoader(1, 80, 24, 2, 21, "about:home", loader)
	if err != nil {
		t.Fatal(err)
	}
	tab2, err := tabs.openWithLoader(2, 80, 24, 2, 21, "about:home", loader)
	if err != nil {
		t.Fatal(err)
	}
	if tab1.Session == nil || tab2.Session == nil || len(tabs.backends) != 2 {
		t.Fatal("tabs were not opened")
	}

	tabs.kill(tab1)
	if first.killed != 1 || first.closed != 0 {
		t.Fatalf("kill = %d, close = %d", first.killed, first.closed)
	}
	if _, ok := tabs.backends[tab1]; ok {
		t.Fatal("killed tab stayed registered")
	}
	if _, ok := tabs.backends[tab2]; !ok {
		t.Fatal("killing one tab removed another")
	}

	tabs.close(tab2)
	if second.closed != 1 || second.killed != 0 {
		t.Fatalf("close = %d, kill = %d", second.closed, second.killed)
	}
	if len(tabs.backends) != 0 {
		t.Fatal("closed tab stayed registered")
	}
}
