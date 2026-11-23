package browser

import (
	"context"
	"errors"
	"net/url"

	"browser/internal/engine"
	"browser/internal/protocol"
)

// jsHost is the engine-owned contract; the browser only implements it.
type jsHost = engine.JSHost

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

func (h browserJSHost) Width() int      { return h.b.Width }
func (h browserJSHost) Height() int     { return h.b.Height }
func (h browserJSHost) CellW() int      { return h.b.CellW }
func (h browserJSHost) CellH() int      { return h.b.CellH }
func (h browserJSHost) Dark() bool      { return h.b.Dark }
func (h browserJSHost) Layout() *Layout { return h.b.Layout }
func (h browserJSHost) Scroll() int     { return h.b.Scroll }
func (h browserJSHost) SetScroll(v int) { h.b.Scroll = v }
func (h browserJSHost) ClampScroll()    { h.b.clampScroll() }
func (h browserJSHost) InputFocus() *Node {
	return h.b.Forms.InputFocus
}
func (h browserJSHost) SetInputFocus(n *Node) { h.b.Forms.InputFocus = n }
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
func (h browserJSHost) Interactive() bool             { return h.b.Interactive }
func (h browserJSHost) AbortGen() uint64              { return h.b.abortGen.Load() }
func (h browserJSHost) ImportCh() chan<- jsImportTask { return h.b.ImportCh }

func (h browserJSHost) UserAgent() string {
	if h.b.loader == nil {
		return ""
	}
	return h.b.loader.requestUserAgent()
}
func (h browserJSHost) DocumentCookies(u *url.URL) []engine.Cookie {
	if h.b.loader == nil || h.b.loader.host == nil || u == nil {
		return nil
	}
	cookies := h.b.loader.host.DocumentCookieValues(u)
	out := make([]engine.Cookie, 0, len(cookies))
	for _, c := range cookies {
		out = append(out, engine.Cookie{Name: c.Name, Value: c.Value})
	}
	return out
}
func (h browserJSHost) SetDocumentCookie(u *url.URL, raw string) {
	if h.b.loader == nil || h.b.loader.host == nil || u == nil {
		return
	}
	h.b.loader.host.SetDocumentCookieRaw(u, raw)
}
func (h browserJSHost) FetchResource(ctx context.Context, u *url.URL) ([]byte, string, *url.URL, int, error) {
	if h.b.loader == nil {
		return nil, "", nil, 0, errors.New("loader unavailable")
	}
	return h.b.loader.fetchCtx(ctx, u)
}
func (h browserJSHost) JSFetch(ctx context.Context, req protocol.FetchRequest) (protocol.FetchResponse, error) {
	if h.b.loader == nil {
		return protocol.FetchResponse{}, errors.New("loader unavailable")
	}
	return h.b.loader.host.JSFetch(ctx, req)
}

func (h browserJSHost) RequestContext(kind string, page *url.URL, pageLocal bool, target *url.URL) context.Context {
	ctx := protocol.WithRequestKind(context.Background(), kind)
	ctx = protocol.WithRequestPage(ctx, page, pageLocal)
	if target != nil {
		ctx = protocol.WithRequestTarget(ctx, target)
	}
	return ctx
}
func (h browserJSHost) DecodeBody(data []byte, contentType string) string {
	return decodeBody(data, contentType)
}
func (h browserJSHost) IsJavaScriptMIME(contentType string) bool {
	return isJavaScriptMIME(contentType)
}
func (h browserJSHost) MIMEEssence(contentType string) string {
	return mimeEssence(contentType)
}

func (h browserJSHost) CheckRequest(ctx context.Context, u *url.URL) error {
	meta := protocol.RequestMetaFrom(ctx)
	if meta.BlockLocal && h.b.loader.host.IsLocalURL(u) {
		return protocol.ErrLocalNetworkBlocked
	}
	if meta.PageSecure && u.Scheme == "http" {
		return protocol.ErrMixedContentBlocked
	}
	return nil
}
