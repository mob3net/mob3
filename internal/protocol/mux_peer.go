package protocol

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

// MuxPeer owns the single read loop for a Mux and routes each frame kind to a
// registered handler. It is the shape a process transport needs: page
// commands/events and host RPC share one stdin/stdout pair without competing
// readers.
type MuxPeer struct {
	mux    *Mux
	lim    Limits
	closer io.Closer

	mu       sync.RWMutex
	handlers map[MuxKind]func(MuxFrame) error

	out        chan writeReq
	writerDone chan struct{}
	stop       chan struct{}

	done      chan struct{}
	startOnce sync.Once
	startErr  chan error
	closeOnce sync.Once
	errMu     sync.Mutex
	err       error
}

type writeReq struct {
	kind MuxKind
	id   uint64
	body []byte
	ack  chan struct{}
}

// NewMuxPeer creates a peer over separate reader and writer streams.
func NewMuxPeer(r io.Reader, w io.Writer, lim Limits) *MuxPeer {
	lim = lim.normalized()
	p := &MuxPeer{
		mux:        NewMux(r, w, lim),
		lim:        lim,
		handlers:   make(map[MuxKind]func(MuxFrame) error),
		out:        make(chan writeReq, 64),
		writerDone: make(chan struct{}),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
	go p.writeLoop()
	return p
}

// NewMuxPeerReadWriter creates a peer over one bidirectional stream and closes
// that stream when Close or Run shutdown is requested.
func NewMuxPeerReadWriter(rw io.ReadWriteCloser, lim Limits) *MuxPeer {
	p := NewMuxPeer(rw, rw, lim)
	p.closer = rw
	return p
}

// Handle registers the handler for one frame kind. It must be called before
// Run. A nil handler removes the registration.
func (p *MuxPeer) Handle(kind MuxKind, handler func(MuxFrame) error) error {
	if p == nil || !kind.valid() {
		return fmt.Errorf("%w: mux kind %d", ErrInvalid, kind)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if handler == nil {
		delete(p.handlers, kind)
		return nil
	}
	if _, exists := p.handlers[kind]; exists {
		return fmt.Errorf("%w: duplicate mux handler %d", ErrInvalid, kind)
	}
	p.handlers[kind] = handler
	return nil
}

// Start runs the peer in the background exactly once. It is idempotent, so a
// worker can start host RPC early and the page loop can attach later.
func (p *MuxPeer) Start(ctx context.Context) <-chan error {
	p.startOnce.Do(func() {
		p.startErr = make(chan error, 1)
		go func() { p.startErr <- p.Run(ctx) }()
	})
	return p.startErr
}

// Run reads and dispatches frames until the stream closes or ctx is cancelled.
func (p *MuxPeer) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	defer close(p.done)
	defer p.Close()
	go func() {
		select {
		case <-ctx.Done():
			_ = p.Close()
		case <-p.done:
		}
	}()
	for {
		f, err := p.mux.ReadFrame()
		if err != nil {
			p.setErr(err)
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || ctx.Err() != nil {
				return nil
			}
			return err
		}
		switch f.Kind {
		case MuxPing:
			if err := p.WriteFrame(MuxPong, f.ID, nil); err != nil {
				p.setErr(err)
				return err
			}
			continue
		case MuxPong:
			continue
		}
		p.mu.RLock()
		handler := p.handlers[f.Kind]
		p.mu.RUnlock()
		if handler == nil {
			err := fmt.Errorf("%w: no handler for mux kind %d", ErrInvalid, f.Kind)
			p.setErr(err)
			return err
		}
		if err := handler(f); err != nil {
			p.setErr(err)
			return err
		}
	}
}

// Close stops the writer and closes the underlying stream. Run returns once
// the read loop notices.
func (p *MuxPeer) Close() error {
	if p == nil {
		return nil
	}
	p.closeOnce.Do(func() {
		close(p.stop)
		if p.closer != nil {
			_ = p.closer.Close()
		}
	})
	return nil
}

// Done is closed when Run returns.
func (p *MuxPeer) Done() <-chan struct{} { return p.done }

// Err returns the first framing or handler error observed by Run.
func (p *MuxPeer) Err() error {
	p.errMu.Lock()
	defer p.errMu.Unlock()
	return p.err
}

func (p *MuxPeer) setErr(err error) {
	if err == nil {
		return
	}
	p.errMu.Lock()
	if p.err == nil {
		p.err = err
	}
	p.errMu.Unlock()
}

// WriteFrame queues one frame without waiting for the other process to read.
// A full queue returns ErrSessionBusy so the front end can discard the tab
// instead of blocking its event loop on a wedged child.
func (p *MuxPeer) WriteFrame(kind MuxKind, id uint64, body []byte) error {
	if p == nil || !kind.valid() {
		return fmt.Errorf("%w: mux kind %d", ErrInvalid, kind)
	}
	if len(body) > p.lim.MaxMessageBytes {
		return fmt.Errorf("%w: mux payload is %d bytes", ErrLimit, len(body))
	}
	select {
	case <-p.stop:
		return ErrSessionClosed
	case <-p.writerDone:
		return ErrSessionClosed
	default:
	}
	select {
	case p.out <- writeReq{kind: kind, id: id, body: body}:
		return nil
	case <-p.stop:
		return ErrSessionClosed
	case <-p.writerDone:
		return ErrSessionClosed
	default:
		return ErrSessionBusy
	}
}

// WriteCommand writes one page command.
func (p *MuxPeer) WriteCommand(c Command) error {
	body, err := AppendCommand(nil, c, p.lim)
	if err != nil {
		return err
	}
	return p.WriteFrame(MuxCommand, 0, body)
}

// WriteEvent writes one page event.
func (p *MuxPeer) WriteEvent(ev Event) error {
	body, err := AppendEvent(nil, ev, p.lim)
	if err != nil {
		return err
	}
	return p.WriteFrame(MuxEvent, 0, body)
}

// WriteHostCall writes one host RPC request.
func (p *MuxPeer) WriteHostCall(call HostCall) error {
	body, err := AppendHostCall(nil, call, p.lim)
	if err != nil {
		return err
	}
	return p.WriteFrame(MuxHostCall, call.ID, body)
}

// WriteHostResult writes one host RPC response.
func (p *MuxPeer) WriteHostResult(result HostResult) error {
	body, err := AppendHostResult(nil, result, p.lim)
	if err != nil {
		return err
	}
	return p.WriteFrame(MuxHostResult, result.ID, body)
}

// Flush waits until every frame queued before the call has been written. It
// returns ctx.Err() if the peer stops reading first.
func (p *MuxPeer) Flush(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ack := make(chan struct{})
	select {
	case p.out <- writeReq{ack: ack}:
	case <-p.stop:
		return ErrSessionClosed
	case <-p.writerDone:
		return ErrSessionClosed
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-ack:
		return nil
	case <-p.stop:
		return ErrSessionClosed
	case <-p.writerDone:
		return ErrSessionClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *MuxPeer) writeLoop() {
	defer close(p.writerDone)
	for {
		select {
		case req := <-p.out:
			if req.ack != nil {
				close(req.ack)
				continue
			}
			if err := p.mux.WriteFrame(req.kind, req.id, req.body); err != nil {
				p.setErr(err)
				return
			}
		case <-p.stop:
			return
		}
	}
}
