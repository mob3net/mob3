package engine

import (
	"errors"
	"sync"

	"browser/internal/protocol"
)

// maxRoutedMessageBytes bounds one cross-frame message. It is deliberately
// below the frame limit: a postMessage payload is copied, not shared.
const maxRoutedMessageBytes = 1 << 20

var (
	// ErrUnknownFrame reports a route through a frame ID that is not registered.
	ErrUnknownFrame = errors.New("frame router: unknown frame")
	// ErrFrameMessageTooLarge reports a cross-frame payload over the cap.
	ErrFrameMessageTooLarge = errors.New("frame router: message too large")
	// ErrFrameTargetMismatch reports a postMessage targetOrigin that does not
	// match the receiving frame's committed origin.
	ErrFrameTargetMismatch = errors.New("frame router: target origin mismatch")
)

// RoutedMessage is one copied cross-frame delivery. It carries plain data and
// the sending frame's origin only; no engine node, layout or sobek value may
// cross the boundary.
type RoutedMessage struct {
	From   FrameID
	To     FrameID
	Origin protocol.Origin
	Data   []byte
}

// FrameRouter is the only path for cross-frame access inside a tab. It owns
// the frame IDs, enforces the target-origin check and copies the payload
// before delivery. Iframes are inert stubs, so the router holds only the tab's
// top-level frame.
type FrameRouter struct {
	mu      sync.Mutex
	frames  map[FrameID]*Frame
	next    FrameID
	deliver func(RoutedMessage) error
}

func newFrameRouter() *FrameRouter {
	return &FrameRouter{frames: make(map[FrameID]*Frame)}
}

// Register assigns f an ID and adds it to the router. A frame that is already
// registered keeps its ID.
func (r *FrameRouter) Register(f *Frame) FrameID {
	if r == nil || f == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if f.id != 0 {
		return f.id
	}
	if r.frames == nil {
		r.frames = make(map[FrameID]*Frame)
	}
	r.next++
	if r.next == 0 {
		r.next++
	}
	f.id = r.next
	r.frames[f.id] = f
	return f.id
}

// Unregister removes f from the router and clears its ID.
func (r *FrameRouter) Unregister(f *Frame) {
	if r == nil || f == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if f.id == 0 {
		return
	}
	delete(r.frames, f.id)
	f.id = 0
}

// SetDeliver installs the receiver for routed messages. It is nil until the
// frame messaging layer exists.
func (r *FrameRouter) SetDeliver(fn func(RoutedMessage) error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.deliver = fn
	r.mu.Unlock()
}

// Origin reports the committed origin of a registered frame.
func (r *FrameRouter) Origin(id FrameID) (protocol.Origin, bool) {
	f, ok := r.frame(id)
	if !ok {
		return protocol.Origin{Opaque: true}, false
	}
	return f.Origin(), true
}

// SameOrigin reports whether two registered frames have the same tuple origin.
// Unknown or opaque frames never match.
func (r *FrameRouter) SameOrigin(a, b FrameID) bool {
	oa, ok := r.Origin(a)
	if !ok {
		return false
	}
	ob, ok := r.Origin(b)
	if !ok {
		return false
	}
	return oa.Equal(ob)
}

// Route copies one cross-frame message, checks that targetOrigin matches the
// receiving frame, and hands it to the delivery sink. An empty targetOrigin
// means same-origin only; "*" means any registered frame.
func (r *FrameRouter) Route(from, to FrameID, data []byte, targetOrigin string) error {
	if from == 0 || to == 0 {
		return ErrUnknownFrame
	}
	if len(data) > maxRoutedMessageBytes {
		return ErrFrameMessageTooLarge
	}
	if r == nil {
		return ErrUnknownFrame
	}
	r.mu.Lock()
	src, srcOK := r.frames[from]
	dst, dstOK := r.frames[to]
	deliver := r.deliver
	r.mu.Unlock()
	if !srcOK || !dstOK {
		return ErrUnknownFrame
	}
	srcOrigin, dstOrigin := src.Origin(), dst.Origin()
	if !frameTargetMatches(from, to, srcOrigin, dstOrigin, targetOrigin) {
		return ErrFrameTargetMismatch
	}
	if deliver == nil {
		return nil
	}
	payload := append([]byte(nil), data...)
	return deliver(RoutedMessage{From: from, To: to, Origin: srcOrigin, Data: payload})
}

func (r *FrameRouter) frame(id FrameID) (*Frame, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.Lock()
	f, ok := r.frames[id]
	r.mu.Unlock()
	return f, ok
}

// frameTargetMatches implements the postMessage targetOrigin rule. Opaque
// frames can only be addressed by "*" or by a message to themselves with an
// empty targetOrigin.
func frameTargetMatches(from, to FrameID, src, dst protocol.Origin, targetOrigin string) bool {
	switch targetOrigin {
	case "*":
		return true
	case "":
		return from == to || src.Equal(dst)
	}
	want, ok := protocol.ParseOrigin(targetOrigin)
	if !ok {
		return false
	}
	return want.Equal(dst)
}
