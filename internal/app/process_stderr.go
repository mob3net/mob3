package app

import (
	"strings"
	"sync"

	"browser/internal/protocol"
)

const (
	maxChildStderrBytes = 8 << 10
	maxChildStderrRunes = 400
)

// boundedStderr keeps only the tail of a child's stderr. It is the parent-side
// writer for cmd.Stderr, so a child can report startup errors without gaining a
// direct terminal-output channel.
type boundedStderr struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func newBoundedStderr() *boundedStderr {
	return &boundedStderr{limit: maxChildStderrBytes}
}

func (b *boundedStderr) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.limit {
		copy(b.buf, b.buf[len(b.buf)-b.limit:])
		b.buf = b.buf[:b.limit]
	}
	return len(p), nil
}

// Text returns the sanitized, single-line tail suitable for a footer or crash
// message. Terminal controls and layout-changing whitespace never escape.
func (b *boundedStderr) Text() string {
	b.mu.Lock()
	raw := string(b.buf)
	b.mu.Unlock()
	raw = protocol.StripControls(raw)
	raw = strings.Join(strings.Fields(raw), " ")
	runes := []rune(raw)
	if len(runes) > maxChildStderrRunes {
		runes = runes[:maxChildStderrRunes]
	}
	return string(runes)
}
