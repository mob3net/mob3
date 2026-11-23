package engine

import "net/url"

// Session holds one page's content and view state. The terminal shell and
// trusted navigation state stay in the front end; this type is the engine-side
// half of the session split.
type Session struct {
	Page   *Page
	Layout *Layout
	URL    *url.URL
	Scroll int
}

// NewSession creates an empty page session.
func NewSession() *Session { return &Session{} }
