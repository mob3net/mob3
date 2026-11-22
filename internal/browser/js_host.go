package browser

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
)

// jsHost is everything the JavaScript runtime is allowed to ask the session
// and host to do. It deliberately has no Browser fields: the runtime can see
// viewport state, layout, DOM hit testing, dialogs and brokered I/O through
// this interface only.
type jsHost interface {
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
	ImportCh() chan<- jsImportTask

	UserAgent() string
	Interactive() bool
	DocumentCookies(u *url.URL) []*http.Cookie
	SetDocumentCookie(u *url.URL, c *http.Cookie)
	FetchResource(ctx context.Context, u *url.URL) (data []byte, ctype string, final *url.URL, status int, err error)
	CurrentPageGeneration() int
	StartRequest(gen int, method, rawURL, kind string) (int, bool)
	FinishRequest(id, gen, status, size int, elapsed time.Duration, err error)
	HTTPClient() *http.Client
}

// browserJSHost is the in-process adapter from the runtime to the current
// Browser. When the session moves to internal/engine, this stays on the
// browser side and implements the engine-owned interface.
type browserJSHost struct{ b *Browser }

func newJSHost(b *Browser) jsHost {
	if b == nil {
		return nil
	}
	return browserJSHost{b: b}
}

func (h browserJSHost) Width() int      { return h.b.width }
func (h browserJSHost) Height() int     { return h.b.height }
func (h browserJSHost) CellW() int      { return h.b.cellW }
func (h browserJSHost) CellH() int      { return h.b.cellH }
func (h browserJSHost) Dark() bool      { return h.b.dark }
func (h browserJSHost) Layout() *Layout { return h.b.layout }
func (h browserJSHost) Scroll() int     { return h.b.scroll }
func (h browserJSHost) SetScroll(v int) { h.b.scroll = v }
func (h browserJSHost) ClampScroll()    { h.b.clampScroll() }
func (h browserJSHost) InputFocus() *Node {
	return h.b.inputFocus
}
func (h browserJSHost) SetInputFocus(n *Node) { h.b.inputFocus = n }
func (h browserJSHost) NodeAt(x, y int) *Node { return h.b.nodeAt(x, y) }

func (h browserJSHost) Status() string     { return h.b.status }
func (h browserJSHost) SetStatus(s string) { h.b.status = s }
func (h browserJSHost) LogJS(kind, msg string) {
	h.b.logJS(kind, msg)
}
func (h browserJSHost) ShowAlert(msg string)        { h.b.showAlert(msg) }
func (h browserJSHost) ShowConfirm(msg string) bool { return h.b.showConfirm(msg) }
func (h browserJSHost) ShowPrompt(msg, def string) (string, bool) {
	return h.b.showPrompt(msg, def)
}
func (h browserJSHost) ShowUploadNotice()             { h.b.showUploadNotice() }
func (h browserJSHost) QueueFormSubmit(form *Node)    { h.b.queueFormSubmit(form) }
func (h browserJSHost) Interactive() bool             { return h.b.interactive }
func (h browserJSHost) AbortGen() uint64              { return h.b.abortGen.Load() }
func (h browserJSHost) ImportCh() chan<- jsImportTask { return h.b.importCh }

func (h browserJSHost) UserAgent() string {
	if h.b.loader == nil {
		return ""
	}
	return h.b.loader.requestUserAgent()
}
func (h browserJSHost) DocumentCookies(u *url.URL) []*http.Cookie {
	if h.b.loader == nil || h.b.loader.cookies == nil || u == nil {
		return nil
	}
	return h.b.loader.cookies.documentCookies(u)
}
func (h browserJSHost) SetDocumentCookie(u *url.URL, c *http.Cookie) {
	if h.b.loader == nil || h.b.loader.cookies == nil || u == nil {
		return
	}
	h.b.loader.cookies.setDocumentCookie(u, c)
}
func (h browserJSHost) FetchResource(ctx context.Context, u *url.URL) ([]byte, string, *url.URL, int, error) {
	if h.b.loader == nil {
		return nil, "", nil, 0, errors.New("loader unavailable")
	}
	return h.b.loader.fetchCtx(ctx, u)
}
func (h browserJSHost) CurrentPageGeneration() int {
	if h.b.loader == nil {
		return 0
	}
	return h.b.loader.currentPageGeneration()
}
func (h browserJSHost) StartRequest(gen int, method, rawURL, kind string) (int, bool) {
	if h.b.loader == nil {
		return 0, false
	}
	return h.b.loader.startRequest(gen, method, rawURL, kind)
}
func (h browserJSHost) FinishRequest(id, gen, status, size int, elapsed time.Duration, err error) {
	if h.b.loader != nil {
		h.b.loader.finishRequest(id, gen, status, size, elapsed, err)
	}
}
func (h browserJSHost) HTTPClient() *http.Client {
	if h.b.loader == nil {
		return nil
	}
	return h.b.loader.client
}
