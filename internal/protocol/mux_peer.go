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

	done      chan struct{}
	closeOnce sync.Once
	errMu     sync.Mutex
	err       error
}

// NewMuxPeer creates a peer over separate reader and writer streams.
func NewMuxPeer(r io.Reader, w io.Writer, lim Limits) *MuxPeer {
	lim = lim.normalized()
	return &MuxPeer{
		mux:      NewMux(r, w, lim),
		lim:      lim,
		handlers: make(map[MuxKind]func(MuxFrame) error),
		done:     make(chan struct{}),
	}
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

// Run reads and dispatches frames until the stream closes or ctx is cancelled.
func (p *MuxPeer) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	defer close(p.done)
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

// Close closes the underlying stream. Run returns once the read loop notices.
func (p *MuxPeer) Close() error {
	if p == nil {
		return nil
	}
	var err error
	p.closeOnce.Do(func() {
		if p.closer != nil {
			err = p.closer.Close()
		}
	})
	return err
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

// WriteFrame writes one frame through the shared stream.
func (p *MuxPeer) WriteFrame(kind MuxKind, id uint64, body []byte) error {
	return p.mux.WriteFrame(kind, id, body)
}

// WriteCommand writes one page command.
func (p *MuxPeer) WriteCommand(c Command) error { return p.mux.WriteCommand(c) }

// WriteEvent writes one page event.
func (p *MuxPeer) WriteEvent(ev Event) error { return p.mux.WriteEvent(ev) }

// WriteHostCall writes one host RPC request.
func (p *MuxPeer) WriteHostCall(call HostCall) error {
	body, err := AppendHostCall(nil, call, p.lim)
	if err != nil {
		return err
	}
	return p.mux.WriteFrame(MuxHostCall, call.ID, body)
}

// WriteHostResult writes one host RPC response.
func (p *MuxPeer) WriteHostResult(result HostResult) error {
	body, err := AppendHostResult(nil, result, p.lim)
	if err != nil {
		return err
	}
	return p.mux.WriteFrame(MuxHostResult, result.ID, body)
}
