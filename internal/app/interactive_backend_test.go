package app

import (
	"errors"
	"testing"
	"time"

	"browser/internal/engine"
	"browser/internal/frontend"
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

type abandonTestSession struct {
	done      chan struct{}
	abandoned chan struct{}
}

func (s *abandonTestSession) Send(protocol.Command) error   { return nil }
func (s *abandonTestSession) Events() <-chan protocol.Event { return nil }
func (s *abandonTestSession) Abort()                        {}
func (s *abandonTestSession) Close() error                  { return nil }
func (s *abandonTestSession) Abandon()                      { close(s.abandoned) }
func (s *abandonTestSession) Done() <-chan struct{}         { return s.done }

func TestInProcKillDoesNotWaitForHandler(t *testing.T) {
	session := &abandonTestSession{done: make(chan struct{}), abandoned: make(chan struct{})}
	backend := &inProcTabBackend{session: session}
	done := make(chan struct{})
	go func() {
		backend.Kill()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Kill waited for the handler")
	}
	select {
	case <-session.abandoned:
	default:
		t.Fatal("Kill did not abandon the session")
	}
	close(session.done)
}

type blockingCloseBackend struct {
	block  chan struct{}
	killed chan struct{}
}

func (b *blockingCloseBackend) Session() protocol.Session { return backendTestSession{} }
func (b *blockingCloseBackend) Close()                    { <-b.block }
func (b *blockingCloseBackend) Kill()                     { close(b.killed) }

func TestInteractiveTabsCloseTimesOutAndKills(t *testing.T) {
	tabs := newInteractiveTabs(host.NewSession(), engine.LoaderConfig{}, "about:home", 8, 16, nil)
	tabs.closeGrace = 20 * time.Millisecond
	backend := &blockingCloseBackend{block: make(chan struct{}), killed: make(chan struct{})}
	tab := &frontend.Tab{}
	tabs.backends[tab] = backend
	done := make(chan struct{})
	go func() {
		tabs.close(tab)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("close did not time out")
	}
	select {
	case <-backend.killed:
	default:
		t.Fatal("backend was not killed after close timeout")
	}
	close(backend.block)
}

type exitTestBackend struct {
	session protocol.Session
	done    chan struct{}
	err     error
}

func (b *exitTestBackend) Session() protocol.Session { return b.session }
func (b *exitTestBackend) Close()                    {}
func (b *exitTestBackend) Kill()                     {}
func (b *exitTestBackend) Done() <-chan struct{}     { return b.done }
func (b *exitTestBackend) ExitError() error          { return b.err }
func (b *exitTestBackend) UserClosed() bool          { return false }

func TestInteractiveTabsReportsBackendFailure(t *testing.T) {
	tabs := newInteractiveTabs(host.NewSession(), engine.LoaderConfig{}, "about:home", 8, 16, nil)
	backend := &exitTestBackend{session: backendTestSession{}, done: make(chan struct{}), err: errors.New("exit status 2")}
	tabs.newBackend = func(*engine.Loader, protocol.SessionID, protocol.Init, string) (tabBackend, error) {
		return backend, nil
	}
	failed := make(chan string, 1)
	tabs.setFailure(func(_ *frontend.Tab, message string) { failed <- message })
	tab, err := tabs.openWithLoader(1, 80, 24, 2, 21, "about:home", &engine.Loader{})
	if err != nil {
		t.Fatal(err)
	}
	close(backend.done)
	select {
	case message := <-failed:
		if message != "exit status 2" {
			t.Fatalf("failure message = %q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("backend failure was not reported")
	}
	tabs.kill(tab)
}

func TestInteractiveTabsReportsExitAfterDeadClose(t *testing.T) {
	tabs := newInteractiveTabs(host.NewSession(), engine.LoaderConfig{}, "about:home", 8, 16, nil)
	backend := &exitTestBackend{
		session: backendTestSession{}, done: make(chan struct{}),
		err: errors.New("exit status 2: fatal error: stack overflow"),
	}
	tabs.newBackend = func(*engine.Loader, protocol.SessionID, protocol.Init, string) (tabBackend, error) {
		return backend, nil
	}
	failed := make(chan string, 1)
	tabs.setFailure(func(_ *frontend.Tab, message string) { failed <- message })
	tab, err := tabs.openWithLoader(1, 80, 24, 2, 21, "about:home", &engine.Loader{})
	if err != nil {
		t.Fatal(err)
	}
	tab.MarkDead("protocol session ended")
	tabs.close(tab)
	select {
	case message := <-failed:
		if message != "exit status 2: fatal error: stack overflow" {
			t.Fatalf("failure message = %q", message)
		}
	default:
		t.Fatal("dead tab close did not report the exit error")
	}
}

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
