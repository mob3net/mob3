package protocol

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
)

// ServeStream runs a handler over one stream connection. It closes rw when the
// session ends. Abort commands are applied directly on the reader goroutine so
// a busy script stops without waiting for the command loop.
func ServeStream(ctx context.Context, rw io.ReadWriteCloser, id SessionID, h Handler, lim Limits, queue int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	lim = lim.normalized()
	if queue < 1 {
		queue = defaultEventQueue
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	mux := NewMuxReadWriter(rw, lim)
	in := make(chan Command, defaultCommandQueue)
	events := make(chan Event, queue)
	out := newOutbox(ctx, events, queue)
	sink := &eventSink{id: id, lim: lim, out: out}
	abortCh := make(chan struct{}, 1)

	readErr := make(chan error, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		defer close(in)
		for {
			c, err := mux.ReadCommand()
			if err != nil {
				select {
				case readErr <- err:
				default:
				}
				if a, ok := h.(Aborter); ok {
					a.AbortSession()
				}
				cancel()
				return
			}
			if _, ok := c.Body.(Abort); ok {
				if a, ok := h.(Aborter); ok {
					a.AbortSession()
				}
				select {
				case abortCh <- struct{}{}:
				default:
				}
			}
			select {
			case in <- c:
			case <-ctx.Done():
				return
			}
		}
	}()

	writeErr := make(chan error, 1)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case ev, ok := <-events:
				if !ok {
					return
				}
				if err := mux.WriteEvent(ev); err != nil {
					select {
					case writeErr <- err:
					default:
					}
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	err := runHandler(ctx, abortCh, in, sink, h)
	if err != nil && ctx.Err() == nil {
		_ = sink.Send(Event{Body: Crash{Message: err.Error()}})
	}
	// Drain the outbox into the writer, then let the writer flush every queued
	// event before closing the connection. Cancelling first would drop the
	// final State/Quit on the floor.
	out.finish()
	<-writerDone
	cancel()
	_ = rw.Close()
	<-readerDone

	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	select {
	case e := <-readErr:
		if e != nil && !errors.Is(e, io.EOF) && !errors.Is(e, context.Canceled) {
			return e
		}
	default:
	}
	select {
	case e := <-writeErr:
		if e != nil && !errors.Is(e, context.Canceled) && !errors.Is(e, io.ErrClosedPipe) {
			return e
		}
	default:
	}
	return nil
}

// DialStream returns the front-end handle for a stream connection.
func DialStream(ctx context.Context, rw io.ReadWriteCloser, id SessionID, lim Limits, queue int) Session {
	if ctx == nil {
		ctx = context.Background()
	}
	if queue < 1 {
		queue = defaultEventQueue
	}
	lim = lim.normalized()
	s := &streamSession{
		rw:     rw,
		mux:    NewMuxReadWriter(rw, lim),
		id:     id,
		events: make(chan Event, queue),
		done:   make(chan struct{}),
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	go s.readLoop()
	return s
}

type streamSession struct {
	rw     io.ReadWriteCloser
	mux    *Mux
	id     SessionID
	ctx    context.Context
	cancel context.CancelFunc
	events chan Event
	done   chan struct{}

	closed atomic.Bool
}

func (s *streamSession) Send(c Command) error {
	if s.closed.Load() {
		return ErrSessionClosed
	}
	if c.Session == 0 {
		c.Session = s.id
	}
	return s.mux.WriteCommand(c)
}

func (s *streamSession) Events() <-chan Event { return s.events }

// Abort sends an Abort command. The serving reader applies it directly, so it
// interrupts a busy script even while the command loop is not running.
func (s *streamSession) Abort() {
	_ = s.Send(Command{Body: Abort{Reason: "escape"}})
}

func (s *streamSession) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		s.cancel()
		_ = s.rw.Close()
	}
	<-s.done
	return nil
}

func (s *streamSession) readLoop() {
	defer close(s.done)
	defer close(s.events)
	for {
		ev, err := s.mux.ReadEvent()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && s.ctx.Err() == nil {
				select {
				case s.events <- Event{Body: Crash{Message: "transport: " + err.Error()}}:
				case <-s.ctx.Done():
				}
			}
			return
		}
		select {
		case s.events <- ev:
		case <-s.ctx.Done():
			return
		}
	}
}
