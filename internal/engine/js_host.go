package engine

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/grafana/sobek"
)

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
	DecodeBody(data []byte, contentType string) string
	IsJavaScriptMIME(contentType string) bool
	MIMEEssence(contentType string) string
	DocumentCookies(u *url.URL) []*http.Cookie
	SetDocumentCookie(u *url.URL, c *http.Cookie)
	FetchResource(ctx context.Context, u *url.URL) (data []byte, ctype string, final *url.URL, status int, err error)
	CurrentPageGeneration() int
	StartRequest(gen int, method, rawURL, kind string) (int, bool)
	FinishRequest(id, gen, status, size int, elapsed time.Duration, err error)
	HTTPClient() *http.Client
}
