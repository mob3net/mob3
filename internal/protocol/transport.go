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

// EventSink accepts engine events. Send encodes and validates the event before
// returning, so the handler may immediately reuse the cells, runs and blobs
// referenced by ev on its next frame.
type EventSink interface {
	Send(Event) error
}

// Handler is the engine side of one session. Serve runs on the session's own
// goroutine and owns every DOM, layout, style and runtime value. It processes
// commands from in and emits events on out until ctx is cancelled or in is
// closed.
//
// abort is closed, or receives a value, when the front end asks to stop a busy
// script without closing the session. The handler must treat it as a one-shot
// cancel request and keep serving afterwards.
type Handler interface {
	Serve(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error

// Serve calls f.
func (f HandlerFunc) Serve(ctx context.Context, abort <-chan struct{}, in <-chan Command, out EventSink) error {
	return f(ctx, abort, in, out)
}

// Aborter is an optional handler capability. Abort must take effect without
// waiting for the command loop, because the loop is exactly where a busy script
// is running; the transport calls this from the front-end goroutine.
type Aborter interface {
	AbortSession()
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
		id:        id,
		lim:       t.Limits.normalized(),
		abortCh:   make(chan struct{}, 1),
		in:        make(chan Command, queueDepth(t.CommandQueue, defaultCommandQueue)),
		events:    make(chan Event, queueDepth(t.EventQueue, defaultEventQueue)),
		serveDone: make(chan struct{}),
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.aborter, _ = h.(Aborter)
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

	aborter Aborter
	abortCh chan struct{}
	in      chan Command
	events  chan Event

	sendMu sync.Mutex
	buf    []byte
	// outBuf is owned by the handler goroutine; Send decodes a copy into
	// events before returning.
	outBuf []byte

	closed    atomic.Bool
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

// Abort requests that the handler stop a running script. An Aborter takes
// effect immediately on the front-end goroutine; the channel remains a wakeup
// for a dialog or any handler that does not implement it. Repeated requests
// coalesce and it never blocks the front end.
func (s *inProcSession) Abort() {
	if s.aborter != nil {
		s.aborter.AbortSession()
	}
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
		// Ask a busy handler to stop before cancelling the context, so a long
		// script cannot hold Close open until it returns on its own.
		if s.aborter != nil {
			s.aborter.AbortSession()
		}
		s.cancel()
		// A handler may be blocked publishing an event that nobody will read
		// now. Drain the queue so it can observe the cancelled context and
		// return instead of holding Close forever.
		go func() {
			for range s.events {
			}
		}()
	}
	<-s.serveDone
	return nil
}

func (s *inProcSession) serve(h Handler) {
	defer close(s.serveDone)
	defer close(s.events)
	sink := inProcSink{s: s}
	if err := h.Serve(s.ctx, s.abortCh, s.in, sink); err != nil && s.ctx.Err() == nil {
		// A handler error is a contained engine failure. Report it before the
		// event stream closes so the front end can show it.
		_ = sink.Send(Event{Body: Crash{Message: err.Error()}})
	}
}

// copyFrame validates a frame and copies every mutable slice so the engine can
// reuse its paint buffers immediately.
func copyFrame(f Frame, lim Limits) (Frame, error) {
	if err := f.Validate(lim); err != nil {
		return Frame{}, err
	}
	out := Frame{Version: f.Version, W: f.W, H: f.H}
	out.Cells = append([]Cell(nil), f.Cells...)
	out.Runs = append([]ScaledRun(nil), f.Runs...)
	if len(f.Placements) > 0 {
		out.Placements = make([]Placement, len(f.Placements))
		for i, p := range f.Placements {
			p.Data = append([]byte(nil), p.Data...)
			out.Placements[i] = p
		}
	}
	return out, nil
}

// inProcSink is the handler-facing event port. Encoding and decoding happen
// before Send returns, so the engine can reuse its frame buffers immediately.
type inProcSink struct{ s *inProcSession }

// Send validates and copies one event, then queues it for the front end. A
// malformed event is replaced by Crash so no trusted side ever sees partial or
// unsafe data from the engine.
func (k inProcSink) Send(ev Event) error {
	s := k.s
	if ev.Session == 0 {
		ev.Session = s.id
	}
	var err error
	if f, ok := ev.Body.(Frame); ok {
		// Frames are the hot path and can be large. A validated copy gives the
		// same semantic checks as the codec without the wire round trip, and it
		// still detaches the engine's reusable buffers before Send returns.
		var copied Frame
		if copied, err = copyFrame(f, s.lim); err == nil {
			ev.Body = copied
		}
	} else {
		s.outBuf, err = AppendEvent(s.outBuf[:0], ev, s.lim)
		if err == nil {
			var decoded Event
			decoded, _, err = DecodeEvent(s.outBuf, s.lim)
			ev = decoded
		}
	}
	if err != nil {
		ev = Event{Session: s.id, Body: Crash{Message: "invalid engine event: " + err.Error()}}
	}
	select {
	case s.events <- ev:
		return nil
	case <-s.ctx.Done():
		return ErrSessionClosed
	}
}
