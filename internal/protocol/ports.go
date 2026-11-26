package protocol

import "context"

// Session is the front end's handle to one page session. Implementations keep
// all engine state on their own goroutine; the front end never touches a DOM,
// layout or runtime directly.
type Session interface {
	// Send queues one command. It blocks until the command is accepted or the
	// session is closed.
	Send(Command) error
	// Events delivers frames, state and other session events.
	Events() <-chan Event
	// Abort stops a running script without waiting for the command queue. It is
	// safe to call from the front end goroutine while the engine is busy.
	Abort()
	// Close releases the session and its runtime.
	Close() error
}

// Abandoner releases a session without waiting for its handler to return. It is
// the in-process equivalent of killing a child process: the caller discards the
// tab and never reuses the session.
type Abandoner interface {
	Abandon()
}

// Waiter reports when a session's handler has actually stopped.
type Waiter interface {
	Done() <-chan struct{}
}

// EntryIDSource allocates process-wide history-entry IDs. The engine's page
// cache and the host's URL attestation share the same sequence; a process
// transport can keep it in the parent.
type EntryIDSource interface {
	NextEntryID() uint64
}

// BudgetShare is one session's part of the process-wide memory pools. A
// process-backed engine receives it from the parent instead of running its own
// BudgetManager.
type BudgetShare struct {
	JSHeap        int64
	HistoryCache  int64
	ImageBytes    int64
	SVGPixels     int64
	ResourceCache int64
}

// FetchPurpose tells the host why a request exists, so it can apply the right
// origin, credential and private-network policy.
type FetchPurpose uint8

const (
	FetchPage FetchPurpose = iota + 1
	FetchSubresource
	FetchScript
	FetchModule
	FetchImage
	FetchXHR
	FetchFetch
)

// Header is one HTTP header. The host, never the engine, applies redirect,
// cookie and credential policy.
type Header struct {
	Name  string
	Value string
}

// FetchRequest asks the host for one resource. Body is decompressed and decoded
// by the engine; the host returns the wire bytes.
type FetchRequest struct {
	URL         string
	Method      string
	Headers     []Header
	Body        []byte
	Purpose     FetchPurpose
	Origin      string
	Credentials bool
	// NoCORS marks a fetch with mode "no-cors". The host skips the CORS
	// response check and does not send Origin; the engine turns a cross-origin
	// result into an opaque response.
	NoCORS bool
	// Kind is the request-log label ("fetch", "xhr", "js", …).
	Kind string
	// PageLocal records whether the initiating document may reach local
	// addresses. The host applies the same private-network policy as page loads.
	PageLocal bool
}

// FetchResponse is the host's answer. The engine owns decompression, charset
// decoding, parsing and decoder budgets.
type FetchResponse struct {
	Status      int
	Headers     []Header
	Body        []byte
	FinalURL    string
	ContentType string
}

// CookieRequest reads or writes one cookie. Script marks a document.cookie
// operation, which the host must check against HttpOnly cookies.
type CookieRequest struct {
	URL    string
	Name   string
	Value  string
	Set    bool
	Script bool
}

// CookieResponse is the value visible to script, or the result of a write.
type CookieResponse struct {
	Value string
	Found bool
}

// Settings is the host-owned snapshot a session needs at init. It is a copy;
// the engine cannot mutate live settings through it.
type Settings struct {
	SearchURL   string
	HomeURL     string
	MemoryLimit int64
}

// Broker is the engine's only path to trusted policy and I/O. Implementations
// enforce CORS, private-network, mixed-content, redirect, credential and size
// rules before any byte reaches the engine.
type Broker interface {
	Fetch(ctx context.Context, req FetchRequest) (FetchResponse, error)
	Cookie(ctx context.Context, req CookieRequest) (CookieResponse, error)
	Settings() Settings
}
