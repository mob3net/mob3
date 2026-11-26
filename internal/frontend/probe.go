package frontend

import (
	"sync"
	"sync/atomic"
	"time"

	"browser/internal/protocol"
)

// ProbeSession wraps a page session and watches it for liveness. A command
// starts a deadline; if no event arrives, the probe sends a Ping and waits for
// a Pong. Missing both means the engine loop is wedged, and onStuck is called
// once so the tab can be discarded.
type ProbeSession struct {
	inner       protocol.Session
	events      chan protocol.Event
	timeout     time.Duration
	pongTimeout time.Duration
	onStuck     func()

	wake      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	closed    atomic.Bool
	nextPing  atomic.Uint64

	mu       sync.Mutex
	awaiting bool
	lastSend time.Time
	probing  uint64
	probeAt  time.Time
	stuck    bool
}

// NewProbeSession starts a liveness probe for one session. The initial state is
// armed so a load that never produces its first event is still detected.
func NewProbeSession(inner protocol.Session, timeout, pongTimeout time.Duration, onStuck func()) *ProbeSession {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if pongTimeout <= 0 {
		pongTimeout = time.Second
	}
	p := &ProbeSession{
		inner:       inner,
		events:      make(chan protocol.Event, 64),
		timeout:     timeout,
		pongTimeout: pongTimeout,
		onStuck:     onStuck,
		wake:        make(chan struct{}, 1),
		done:        make(chan struct{}),
		awaiting:    true,
		lastSend:    time.Now(),
	}
	go p.forward()
	go p.watch()
	return p
}

func (p *ProbeSession) Send(c protocol.Command) error {
	if p.closed.Load() {
		return protocol.ErrSessionClosed
	}
	if err := p.inner.Send(c); err != nil {
		return err
	}
	p.markSend()
	return nil
}

func (p *ProbeSession) Events() <-chan protocol.Event { return p.events }

func (p *ProbeSession) Abort() { p.inner.Abort() }

func (p *ProbeSession) Close() error {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		_ = p.inner.Close()
	})
	<-p.done
	return nil
}

// Abandon stops the underlying session without waiting for its handler.
func (p *ProbeSession) Abandon() {
	p.closed.Store(true)
	if abandoner, ok := p.inner.(protocol.Abandoner); ok {
		abandoner.Abandon()
		return
	}
	go func() { _ = p.inner.Close() }()
}

// Done is closed when the event forwarder has stopped.
func (p *ProbeSession) Done() <-chan struct{} { return p.done }

func (p *ProbeSession) forward() {
	defer close(p.done)
	defer close(p.events)
	events := p.inner.Events()
	if events == nil {
		return
	}
	for ev := range events {
		if _, ok := ev.Body.(protocol.Pong); ok {
			p.noteEvent()
			continue
		}
		p.noteEvent()
		select {
		case p.events <- ev:
		case <-p.done:
			return
		}
	}
}

func (p *ProbeSession) watch() {
	for {
		p.mu.Lock()
		if p.stuck {
			p.mu.Unlock()
			return
		}
		awaiting := p.awaiting
		var deadline time.Time
		if awaiting {
			if p.probing != 0 {
				deadline = p.probeAt.Add(p.pongTimeout)
			} else {
				deadline = p.lastSend.Add(p.timeout)
			}
		}
		p.mu.Unlock()

		if !awaiting {
			select {
			case <-p.wake:
			case <-p.done:
				return
			}
			continue
		}
		delay := time.Until(deadline)
		if delay < 0 {
			delay = 0
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
			p.expire()
		case <-p.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-p.done:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}
	}
}

func (p *ProbeSession) expire() {
	p.mu.Lock()
	if p.stuck || !p.awaiting {
		p.mu.Unlock()
		return
	}
	if p.probing != 0 {
		p.stuck = true
		p.mu.Unlock()
		if p.onStuck != nil {
			p.onStuck()
		}
		return
	}
	id := p.nextPing.Add(1)
	p.probing = id
	p.probeAt = time.Now()
	p.mu.Unlock()
	// Send can block when the command queue is already full. Treat that as
	// part of the same liveness deadline instead of waiting forever here.
	sent := make(chan error, 1)
	go func() { sent <- p.inner.Send(protocol.Command{Body: protocol.Ping{ID: id}}) }()
	select {
	case err := <-sent:
		if err != nil {
			p.markStuck()
			return
		}
		p.mu.Lock()
		if p.probing == id {
			p.probeAt = time.Now()
		}
		p.mu.Unlock()
		p.wakeup()
	case <-time.After(p.pongTimeout):
		p.markStuck()
	case <-p.done:
	}
}

func (p *ProbeSession) markStuck() {
	p.mu.Lock()
	if p.stuck {
		p.mu.Unlock()
		return
	}
	p.stuck = true
	p.mu.Unlock()
	if p.onStuck != nil {
		p.onStuck()
	}
}

func (p *ProbeSession) markSend() {
	p.mu.Lock()
	p.awaiting = true
	p.lastSend = time.Now()
	p.probing = 0
	p.mu.Unlock()
	p.wakeup()
}

func (p *ProbeSession) noteEvent() {
	p.mu.Lock()
	p.awaiting = false
	p.probing = 0
	p.mu.Unlock()
	p.wakeup()
}

func (p *ProbeSession) wakeup() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}
