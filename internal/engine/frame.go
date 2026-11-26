package engine

import (
	"net/url"

	"browser/internal/protocol"
)

// FrameID identifies one frame inside a tab.
type FrameID uint32

// Frame is the tab's top-level browsing context. Iframes are inert stubs with
// no browsing context or child frame tree, so one Frame owns the whole tab's
// document, layout, URL and scroll position. The Session remains the tab-level
// façade that also carries the shell, loader and history state.
type Frame struct {
	id     FrameID
	Page   *Page
	Layout *Layout
	URL    *url.URL
	Scroll int
	// JS is the frame's JavaScript runtime. It is nil when scripts are
	// disabled or before the document starts.
	JS *JSRuntime
}

// NewFrame creates an empty top-level frame.
func NewFrame() *Frame {
	return &Frame{}
}

// ID returns the frame's router ID, or zero when it is not registered.
func (f *Frame) ID() FrameID {
	if f == nil {
		return 0
	}
	return f.id
}

// Origin returns the frame's canonical origin. It is derived from the
// committed document URL, never from a script-supplied value.
func (f *Frame) Origin() protocol.Origin {
	if f == nil {
		return protocol.Origin{Opaque: true}
	}
	return protocol.OriginOf(f.URL)
}
