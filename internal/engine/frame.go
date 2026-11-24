package engine

import "net/url"

// Frame is one browsing context. For now it is the tab's top-level document;
// child frames and a frame tree join it with the iframe work. A frame owns its
// document, layout, URL and scroll position, while the Session remains the
// tab-level façade that also carries the shell, loader and history state.
type Frame struct {
	Page   *Page
	Layout *Layout
	URL    *url.URL
	Scroll int
}

// NewFrame creates an empty top-level frame.
func NewFrame() *Frame {
	return &Frame{}
}
