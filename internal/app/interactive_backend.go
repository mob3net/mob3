package app

import (
	"context"
	"sync"

	"browser/internal/engine"
	"browser/internal/protocol"
)

// tabBackend owns one tab's engine session and transport. In-process and
// process-backed implementations expose the same small surface so the front end
// never needs to know which one it has.
type tabBackend interface {
	Session() protocol.Session
	Close()
	Kill()
}

// tabBackendFactory starts one backend for a tab. It applies the initial
// geometry and navigation before returning.
type tabBackendFactory func(loader *engine.Loader, id protocol.SessionID, init protocol.Init, target string) (tabBackend, error)

type inProcTabBackend struct {
	mu           sync.Mutex
	startedClose bool
	killed       bool
	releaseOnce  sync.Once
	session      protocol.Session
	engine       *engine.Session
}

func newInProcBackendFactory(newSession func(*engine.Loader, int, int) *engine.Session) tabBackendFactory {
	return func(loader *engine.Loader, id protocol.SessionID, init protocol.Init, target string) (tabBackend, error) {
		b := newSession(loader, init.Width, init.Height)
		init.Caps = b.Capabilities()
		session, err := (&protocol.InProc{}).Open(context.Background(), id, b)
		if err != nil {
			b.Release()
			return nil, err
		}
		backend := &inProcTabBackend{session: session, engine: b}
		if err := session.Send(protocol.Command{Body: init}); err != nil {
			backend.Close()
			return nil, err
		}
		if err := session.Send(protocol.Command{Body: protocol.Navigate{URL: target, Push: true}}); err != nil {
			backend.Close()
			return nil, err
		}
		return backend, nil
	}
}

func (b *inProcTabBackend) Session() protocol.Session { return b.session }

// Close releases the transport and then the engine, waiting for the handler.
func (b *inProcTabBackend) Close() {
	b.mu.Lock()
	if b.killed || b.startedClose {
		b.mu.Unlock()
		return
	}
	b.startedClose = true
	b.mu.Unlock()
	if b.session != nil {
		_ = b.session.Close()
	}
	b.release()
}

// Kill discards the tab without waiting for a wedged handler. The engine
// goroutine is leaked until the process exits; that is the in-process
// counterpart of killing a child process. Release still runs if the handler
// eventually returns.
func (b *inProcTabBackend) Kill() {
	b.mu.Lock()
	if b.killed {
		b.mu.Unlock()
		return
	}
	b.killed = true
	b.mu.Unlock()
	if abandoner, ok := b.session.(protocol.Abandoner); ok {
		abandoner.Abandon()
	} else if b.session != nil {
		go func() { _ = b.session.Close() }()
	}
	if waiter, ok := b.session.(protocol.Waiter); ok {
		go func() {
			<-waiter.Done()
			b.release()
		}()
	} else {
		b.release()
	}
}

func (b *inProcTabBackend) release() {
	b.releaseOnce.Do(func() {
		if b.engine != nil {
			b.engine.Release()
		}
	})
}
