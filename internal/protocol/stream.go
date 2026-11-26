package protocol

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
)

// ServeStream runs a handler over a dedicated stream connection.
func ServeStream(ctx context.Context, rw io.ReadWriteCloser, id SessionID, h Handler, lim Limits, queue int) error {
	peer := NewMuxPeerReadWriter(rw, lim)
	return ServePeer(ctx, peer, id, h, lim, queue)
}

// ServePeer runs a handler over an existing peer. Host RPC handlers can be
// mounted on the same peer before this call, so one stream carries page and
// host traffic.
func ServePeer(ctx context.Context, peer *MuxPeer, id SessionID, h Handler, lim Limits, queue int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	lim = lim.normalized()
	if queue < 1 {
		queue = defaultEventQueue
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	in := make(chan Command, defaultCommandQueue)
	inbox := newCommandInbox(ctx, in, 1024)
	go inbox.run()
	events := make(chan Event, queue)
	out := newOutbox(ctx, events, queue)
	sink := &eventSink{id: id, lim: lim, out: out}
	abortCh := make(chan struct{}, 1)
	readerDone := make(chan struct{})

	if err := peer.Handle(MuxCommand, func(f MuxFrame) error {
		c, rest, err := DecodeCommand(f.Body, lim)
		if err == nil && len(rest) != 0 {
			err = fmt.Errorf("%w: %d trailing command bytes", ErrInvalid, len(rest))
		}
		if err != nil {
			if a, ok := h.(Aborter); ok {
				a.AbortSession()
			}
			cancel()
			return err
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
		if err := inbox.push(c); err != nil {
			cancel()
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	go func() {
		defer close(readerDone)
		_ = peer.Run(ctx)
		cancel()
	}()

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case ev, ok := <-events:
				if !ok {
					return
				}
				if err := peer.WriteEvent(ev); err != nil {
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
	_ = peer.Close()
	<-readerDone
	<-inbox.done

	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	if peerErr := peer.Err(); peerErr != nil && !errors.Is(peerErr, io.EOF) &&
		!errors.Is(peerErr, io.ErrClosedPipe) && ctx.Err() == nil {
		return peerErr
	}
	return nil
}

// DialStream returns the front-end handle for a dedicated stream connection.
func DialStream(ctx context.Context, rw io.ReadWriteCloser, id SessionID, lim Limits, queue int) Session {
	peer := NewMuxPeerReadWriter(rw, lim)
	return DialPeer(ctx, peer, id, lim, queue)
}

// DialPeer returns the front-end handle for an existing peer. Host RPC handlers
// can be mounted on the same peer before this call.
func DialPeer(ctx context.Context, peer *MuxPeer, id SessionID, lim Limits, queue int) Session {
	if ctx == nil {
		ctx = context.Background()
	}
	if queue < 1 {
		queue = defaultEventQueue
	}
	lim = lim.normalized()
	s := &streamSession{
		peer:   peer,
		id:     id,
		lim:    lim,
		events: make(chan Event, queue),
		done:   make(chan struct{}),
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	_ = peer.Handle(MuxEvent, s.handleFrame)
	go func() {
		defer close(s.done)
		defer close(s.events)
		_ = peer.Run(s.ctx)
	}()
	return s
}

type streamSession struct {
	peer   *MuxPeer
	id     SessionID
	lim    Limits
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
	return s.peer.WriteCommand(c)
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
		_ = s.peer.Close()
	}
	<-s.done
	return nil
}

func (s *streamSession) handleFrame(f MuxFrame) error {
	ev, rest, err := DecodeEvent(f.Body, s.lim)
	if err == nil && len(rest) != 0 {
		err = fmt.Errorf("%w: %d trailing event bytes", ErrInvalid, len(rest))
	}
	if err != nil {
		return err
	}
	select {
	case s.events <- ev:
		return nil
	case <-s.ctx.Done():
		return nil
	}
}
