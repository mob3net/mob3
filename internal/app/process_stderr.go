package app

import (
	"strings"
	"sync"

	"browser/internal/protocol"
)

const (
	maxChildStderrBytes = 8 << 10
	maxChildStderrRunes = 8 << 10
)

// boundedStderr keeps the beginning and end of a child's stderr within one
// fixed budget. The start carries the fatal error line and the end carries the
// final stack frames; the middle is dropped with a marker when the child is too
// noisy. It is the parent-side writer for cmd.Stderr, so a child can report
// startup errors without gaining a direct terminal-output channel.
type boundedStderr struct {
	mu        sync.Mutex
	head      []byte
	tail      []byte
	limit     int
	truncated bool
}

func newBoundedStderr() *boundedStderr {
	return &boundedStderr{limit: maxChildStderrBytes}
}

func (b *boundedStderr) headLimit() int { return b.limit / 2 }
func (b *boundedStderr) tailLimit() int { return b.limit - b.headLimit() }

func (b *boundedStderr) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	total := len(p)
	if len(b.head) < b.headLimit() {
		take := min(b.headLimit()-len(b.head), len(p))
		b.head = append(b.head, p[:take]...)
		p = p[take:]
	}
	if len(p) > 0 {
		b.tail = append(b.tail, p...)
		if len(b.tail) > b.tailLimit() {
			b.truncated = true
			drop := len(b.tail) - b.tailLimit()
			copy(b.tail, b.tail[drop:])
			b.tail = b.tail[:b.tailLimit()]
		}
	}
	return total, nil
}

// Text returns the sanitized, single-line beginning and end suitable for the
// crash view. Terminal controls and layout-changing whitespace never escape.
func (b *boundedStderr) Text() string {
	b.mu.Lock()
	var sb strings.Builder
	sb.Write(b.head)
	if b.truncated {
		sb.WriteString(" … ")
	}
	sb.Write(b.tail)
	raw := strings.ToValidUTF8(sb.String(), "")
	b.mu.Unlock()
	raw = protocol.StripControls(raw)
	raw = strings.Join(strings.Fields(raw), " ")
	runes := []rune(raw)
	if len(runes) > maxChildStderrRunes {
		runes = runes[:maxChildStderrRunes]
	}
	return string(runes)
}
