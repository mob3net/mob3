package protocol

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

// ReadCommand reads and decodes one command from a stream. Malformed or
// oversized input returns an error without allocating beyond the limit.
func ReadCommand(r io.Reader, lim Limits) (Command, error) {
	buf, err := readWire(r, lim)
	if err != nil {
		return Command{}, err
	}
	c, rest, err := DecodeCommand(buf, lim)
	if err != nil {
		return Command{}, err
	}
	if len(rest) != 0 {
		return Command{}, ErrInvalid
	}
	return c, nil
}

// ReadEvent reads and decodes one event from a stream.
func ReadEvent(r io.Reader, lim Limits) (Event, error) {
	buf, err := readWire(r, lim)
	if err != nil {
		return Event{}, err
	}
	ev, rest, err := DecodeEvent(buf, lim)
	if err != nil {
		return Event{}, err
	}
	if len(rest) != 0 {
		return Event{}, ErrInvalid
	}
	return ev, nil
}

func readWire(r io.Reader, lim Limits) ([]byte, error) {
	lim = lim.normalized()
	var hdr [headerSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := int(binary.LittleEndian.Uint32(hdr[20:24]))
	if n > lim.MaxMessageBytes {
		return nil, ErrLimit
	}
	buf := make([]byte, headerSize+n)
	copy(buf, hdr[:])
	if _, err := io.ReadFull(r, buf[headerSize:]); err != nil {
		return nil, err
	}
	return buf, nil
}

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
			c, err := ReadCommand(rw, lim)
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
				buf, err := AppendEvent(nil, ev, lim)
				if err == nil {
					err = writeAll(rw, buf)
				}
				if err != nil {
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

	err := h.Serve(ctx, abortCh, in, sink)
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
	s := &streamSession{
		rw:     rw,
		id:     id,
		lim:    lim.normalized(),
		events: make(chan Event, queue),
		done:   make(chan struct{}),
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	go s.readLoop()
	return s
}

type streamSession struct {
	rw     io.ReadWriteCloser
	id     SessionID
	lim    Limits
	ctx    context.Context
	cancel context.CancelFunc
	events chan Event
	done   chan struct{}

	writeMu sync.Mutex
	buf     []byte
	closed  atomic.Bool
}

func (s *streamSession) Send(c Command) error {
	if s.closed.Load() {
		return ErrSessionClosed
	}
	if c.Session == 0 {
		c.Session = s.id
	}
	s.writeMu.Lock()
	var err error
	s.buf, err = AppendCommand(s.buf[:0], c, s.lim)
	if err == nil {
		err = writeAll(s.rw, s.buf)
	}
	s.writeMu.Unlock()
	return err
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
		ev, err := ReadEvent(s.rw, s.lim)
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

func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}
