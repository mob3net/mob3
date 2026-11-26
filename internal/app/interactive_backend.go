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
	once    sync.Once
	session protocol.Session
	engine  *engine.Session
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

// Close releases the transport and then the engine. An in-process backend
// cannot force a wedged goroutine to stop, so Kill has the same effect here.
func (b *inProcTabBackend) Close() { b.Kill() }

func (b *inProcTabBackend) Kill() {
	b.once.Do(func() {
		if b.session != nil {
			_ = b.session.Close()
		}
		if b.engine != nil {
			b.engine.Release()
		}
	})
}
