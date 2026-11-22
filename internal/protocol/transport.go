package protocol

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// ErrSessionClosed is returned when a command is sent to a session that has
// already closed.
var ErrSessionClosed = errors.New("protocol: session closed")

// Handler is the engine side of one session. Serve runs on the session's own
// goroutine and owns every DOM, layout, style and runtime value. It processes
// commands from in and emits events on out until ctx is cancelled or in is
// closed.
//
// abort is closed, or receives a value, when the front end asks to stop a busy
// script without closing the session. The handler must treat it as a one-shot
// cancel request and keep serving afterwards.
type Handler interface {
	Serve(ctx context.Context, abort <-chan struct{}, in <-chan Command, out chan<- Event) error
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(ctx context.Context, abort <-chan struct{}, in <-chan Command, out chan<- Event) error

// Serve calls f.
func (f HandlerFunc) Serve(ctx context.Context, abort <-chan struct{}, in <-chan Command, out chan<- Event) error {
	return f(ctx, abort, in, out)
}

// Transport opens sessions. The front end only sees the Session handle; the
// engine handler runs behind it, either in this process or over a pipe or
// socket later.
type Transport interface {
	Open(ctx context.Context, id SessionID, h Handler) (Session, error)
}

// InProc runs a handler on its own goroutine and connects it to the front end
// with the fixed binary codec. Every command and event is encoded and decoded
// before delivery, so the in-process transport exercises the same validation
// as a later child process.
type InProc struct {
	// Limits bounds every message. The zero value uses DefaultLimits.
	Limits Limits
	// CommandQueue and EventQueue are the channel depths. Zero uses a small
	// default; a full queue applies backpressure to the sender.
	CommandQueue int
	EventQueue   int
}

const (
	defaultCommandQueue = 32
	defaultEventQueue   = 64
)

var _ Transport = (*InProc)(nil)
var _ Session = (*inProcSession)(nil)

// Open starts h on a new goroutine and returns the front-end handle.
func (t *InProc) Open(ctx context.Context, id SessionID, h Handler) (Session, error) {
	if h == nil {
		return nil, errors.New("protocol: nil handler")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s := &inProcSession{
		id:         id,
		lim:        t.Limits.normalized(),
		abortCh:    make(chan struct{}, 1),
		in:         make(chan Command, queueDepth(t.CommandQueue, defaultCommandQueue)),
		handlerOut: make(chan Event, queueDepth(t.EventQueue, defaultEventQueue)),
		events:     make(chan Event, queueDepth(t.EventQueue, defaultEventQueue)),
		done:       make(chan struct{}),
		serveDone:  make(chan struct{}),
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	go s.forward()
	go s.serve(h)
	return s, nil
}

func queueDepth(n, def int) int {
	if n <= 0 {
		return def
	}
	return n
}

type inProcSession struct {
	id  SessionID
	lim Limits
	ctx context.Context
	// cancel releases the handler and the forwarding goroutine.
	cancel func()

	abortCh chan struct{}
	in      chan Command
	// handlerOut is the engine's event channel; forward decodes from it.
	handlerOut chan Event
	events     chan Event

	sendMu sync.Mutex
	buf    []byte

	closed    atomic.Bool
	done      chan struct{}
	serveDone chan struct{}
}

func (s *inProcSession) Send(c Command) error {
	if s.closed.Load() {
		return ErrSessionClosed
	}
	if c.Session == 0 {
		c.Session = s.id
	}
	s.sendMu.Lock()
	var err error
	s.buf, err = AppendCommand(s.buf[:0], c, s.lim)
	if err != nil {
		s.sendMu.Unlock()
		return err
	}
	buf := s.buf
	decoded, _, err := DecodeCommand(buf, s.lim)
	s.sendMu.Unlock()
	if err != nil {
		return err
	}
	select {
	case s.in <- decoded:
		return nil
	case <-s.ctx.Done():
		return ErrSessionClosed
	}
}

func (s *inProcSession) Events() <-chan Event { return s.events }

// Abort requests that the handler stop a running script. Repeated requests
// coalesce; it never blocks the front end.
func (s *inProcSession) Abort() {
	select {
	case s.abortCh <- struct{}{}:
	default:
	}
}

// Close stops the session and waits for the handler and forwarding goroutine
// to exit. It is idempotent. Pending events are discarded: the front end has
// already asked the session to go away.
func (s *inProcSession) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		s.cancel()
		// A handler may be blocked publishing an event that nobody will read
		// now. Drain both queues so it can observe the cancelled context and
		// return instead of holding Close forever.
		go func() {
			for range s.events {
			}
		}()
		go func() {
			for range s.handlerOut {
			}
		}()
	}
	<-s.serveDone
	<-s.done
	return nil
}

func (s *inProcSession) serve(h Handler) {
	defer close(s.serveDone)
	err := h.Serve(s.ctx, s.abortCh, s.in, s.handlerOut)
	if err != nil && s.ctx.Err() == nil {
		// A handler error is a contained engine failure. Report it before the
		// event stream closes so the front end can show it.
		select {
		case s.handlerOut <- Event{Session: s.id, Body: Crash{Message: err.Error()}}:
		case <-s.ctx.Done():
		}
	}
	close(s.handlerOut)
}

// forward turns handler events into wire messages and then back into values.
// A malformed event is replaced by Crash so no trusted side ever sees partial
// or unsafe data from the engine.
func (s *inProcSession) forward() {
	defer close(s.done)
	defer close(s.events)
	var buf []byte
	for {
		select {
		case ev, ok := <-s.handlerOut:
			if !ok {
				return
			}
			if ev.Session == 0 {
				ev.Session = s.id
			}
			buf, _ = AppendEvent(buf[:0], ev, s.lim)
			decoded, _, err := DecodeEvent(buf, s.lim)
			if err != nil {
				decoded = Event{Session: s.id, Body: Crash{Message: "invalid engine event: " + err.Error()}}
			}
			select {
			case s.events <- decoded:
			case <-s.ctx.Done():
				return
			}
		case <-s.ctx.Done():
			return
		}
	}
}
