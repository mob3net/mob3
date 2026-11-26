package protocol

import (
	"context"
	"sync"
)

// commandInbox keeps the stream reader from blocking on a full engine command
// queue. Host results share the same reader, so a command burst must not stop
// them from being delivered while the engine is waiting on a host call.
type commandInbox struct {
	ctx  context.Context
	out  chan Command
	wake chan struct{}
	done chan struct{}

	mu      sync.Mutex
	pending []Command
	max     int
}

func newCommandInbox(ctx context.Context, out chan Command, max int) *commandInbox {
	if max < 1 {
		max = 1
	}
	return &commandInbox{
		ctx: ctx, out: out, wake: make(chan struct{}, 1), done: make(chan struct{}), max: max,
	}
}

func (q *commandInbox) push(c Command) error {
	select {
	case <-q.done:
		return ErrSessionClosed
	default:
	}
	q.mu.Lock()
	if len(q.pending) >= q.max {
		q.mu.Unlock()
		return ErrLimit
	}
	q.pending = append(q.pending, c)
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return nil
}

func (q *commandInbox) run() {
	defer close(q.done)
	defer close(q.out)
	for {
		q.mu.Lock()
		if len(q.pending) > 0 {
			c := q.pending[0]
			q.pending = q.pending[1:]
			q.mu.Unlock()
			select {
			case q.out <- c:
			case <-q.ctx.Done():
				return
			}
			continue
		}
		q.mu.Unlock()
		select {
		case <-q.wake:
		case <-q.ctx.Done():
			return
		}
	}
}
