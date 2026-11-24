package engine

import (
	"context"
	"errors"
	"net/url"

	"browser/internal/protocol"
)

// jsHost is the engine-owned contract; the browser only implements it.
type jsHost = JSHost

// frameJSHost is the in-process adapter from one frame's runtime to the tab
// shell. Layout, scroll and hit testing come from the frame; the terminal
// viewport, loader, dialogs and form focus remain tab-level until child frames
// get their own viewport and input state.
type frameJSHost struct {
	b *Session
	f *Frame
}

func newFrameJSHost(b *Session, f *Frame) jsHost {
	if b == nil || f == nil {
		return nil
	}
	return frameJSHost{b: b, f: f}
}

func (h frameJSHost) Width() int      { return h.b.Width }
func (h frameJSHost) Height() int     { return h.b.Height }
func (h frameJSHost) CellW() int      { return h.b.CellW }
func (h frameJSHost) CellH() int      { return h.b.CellH }
func (h frameJSHost) Dark() bool      { return h.b.Dark }
func (h frameJSHost) Layout() *Layout { return h.f.Layout }
func (h frameJSHost) Scroll() int     { return h.f.Scroll }
func (h frameJSHost) SetScroll(v int) { h.f.Scroll = v }
func (h frameJSHost) ClampScroll()    { h.b.clampScroll() }
func (h frameJSHost) InputFocus() *Node {
	return h.b.Forms.InputFocus
}
func (h frameJSHost) SetInputFocus(n *Node) { h.b.Forms.InputFocus = n }
func (h frameJSHost) NodeAt(x, y int) *Node { return h.f.NodeAt(x, y) }

func (h frameJSHost) Status() string     { return h.b.status }
func (h frameJSHost) SetStatus(s string) { h.b.status = s }
func (h frameJSHost) LogJS(kind, msg string) {
	h.b.logJS(kind, msg)
}
func (h frameJSHost) ShowAlert(msg string)        { h.b.showAlert(msg) }
func (h frameJSHost) ShowConfirm(msg string) bool { return h.b.showConfirm(msg) }
func (h frameJSHost) ShowPrompt(msg, def string) (string, bool) {
	return h.b.showPrompt(msg, def)
}
func (h frameJSHost) ShowUploadNotice()             { h.b.showUploadNotice() }
func (h frameJSHost) QueueFormSubmit(form *Node)    { h.b.queueFormSubmit(form) }
func (h frameJSHost) Interactive() bool             { return h.b.Interactive }
func (h frameJSHost) AbortGen() uint64              { return h.b.abortGen.Load() }
func (h frameJSHost) ImportCh() chan<- jsImportTask { return h.b.ImportCh }

func (h frameJSHost) UserAgent() string {
	if h.b.loader == nil {
		return ""
	}
	return h.b.loader.RequestUserAgent()
}
func (h frameJSHost) DocumentCookies(u *url.URL) []Cookie {
	if h.b.loader == nil || h.b.loader.Host == nil || u == nil {
		return nil
	}
	cookies := h.b.loader.Host.DocumentCookieValues(u)
	out := make([]Cookie, 0, len(cookies))
	for _, c := range cookies {
		out = append(out, Cookie{Name: c.Name, Value: c.Value})
	}
	return out
}
func (h frameJSHost) SetDocumentCookie(u *url.URL, raw string) {
	if h.b.loader == nil || h.b.loader.Host == nil || u == nil {
		return
	}
	h.b.loader.Host.SetDocumentCookieRaw(u, raw)
}
func (h frameJSHost) FetchResource(ctx context.Context, u *url.URL) ([]byte, string, *url.URL, int, error) {
	if h.b.loader == nil {
		return nil, "", nil, 0, errors.New("loader unavailable")
	}
	return h.b.loader.FetchCtx(ctx, u)
}
func (h frameJSHost) JSFetch(ctx context.Context, req protocol.FetchRequest) (protocol.FetchResponse, error) {
	if h.b.loader == nil {
		return protocol.FetchResponse{}, errors.New("loader unavailable")
	}
	return h.b.loader.Host.JSFetch(ctx, req)
}

func (h frameJSHost) RequestContext(kind string, page *url.URL, pageLocal bool, target *url.URL) context.Context {
	ctx := protocol.WithRequestKind(context.Background(), kind)
	ctx = protocol.WithRequestPage(ctx, page, pageLocal)
	if target != nil {
		ctx = protocol.WithRequestTarget(ctx, target)
	}
	return ctx
}
func (h frameJSHost) DecodeBody(data []byte, contentType string) string {
	return decodeBody(data, contentType)
}
func (h frameJSHost) IsJavaScriptMIME(contentType string) bool {
	return isJavaScriptMIME(contentType)
}
func (h frameJSHost) MIMEEssence(contentType string) string {
	return mimeEssence(contentType)
}

func (h frameJSHost) CheckRequest(ctx context.Context, u *url.URL) error {
	meta := protocol.RequestMetaFrom(ctx)
	if meta.BlockLocal && h.b.loader.Host.IsLocalURL(u) {
		return protocol.ErrLocalNetworkBlocked
	}
	if meta.PageSecure && u.Scheme == "http" {
		return protocol.ErrMixedContentBlocked
	}
	return nil
}
