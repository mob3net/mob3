package engine

import (
	"context"
	"net/url"

	"github.com/grafana/sobek"

	"browser/internal/protocol"
)

// Cookie is one document-visible cookie value. The host owns parsing and the
// HttpOnly policy; the engine only serializes these values for document.cookie.
type Cookie struct {
	Name  string
	Value string
}

// ImportTask is one dynamic import() graph loaded off the main goroutine.
// The runtime fields stay opaque to the engine contract.
type ImportTask struct {
	Owner  any
	Ref    any
	Spec   string
	Parent any
	Record sobek.ModuleRecord
	Err    error
}

// JSHost is the engine's contract with the session/host around the JavaScript
// runtime. It intentionally exposes no browser package types: viewport state,
// layout, DOM hit testing, dialogs and brokered I/O only.
type JSHost interface {
	Width() int
	Height() int
	CellW() int
	CellH() int
	Dark() bool

	Layout() *Layout
	Scroll() int
	SetScroll(int)
	ClampScroll()
	InputFocus() *Node
	SetInputFocus(*Node)
	NodeAt(x, y int) *Node

	Status() string
	SetStatus(string)
	LogJS(kind, msg string)
	ShowAlert(msg string)
	ShowConfirm(msg string) bool
	ShowPrompt(msg, def string) (string, bool)
	ShowUploadNotice()
	QueueFormSubmit(form *Node)

	AbortGen() uint64
	ImportCh() chan<- ImportTask

	UserAgent() string
	Interactive() bool
	RequestContext(kind string, page *url.URL, pageLocal bool, target *url.URL) context.Context
	CheckRequest(ctx context.Context, u *url.URL) error
	DecodeBody(data []byte, contentType string) string
	IsJavaScriptMIME(contentType string) bool
	MIMEEssence(contentType string) string
	DocumentCookies(u *url.URL) []Cookie
	SetDocumentCookie(u *url.URL, raw string)
	FetchResource(ctx context.Context, u *url.URL) (data []byte, ctype string, final *url.URL, status int, err error)
	JSFetch(ctx context.Context, req protocol.FetchRequest) (protocol.FetchResponse, error)
}
