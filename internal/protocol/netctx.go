package protocol

import (
	"context"
	"errors"
	"net/url"
	"sync/atomic"
)

// RequestMetaKey and PageSpaceKey carry request policy through contexts shared
// by the loader and host.
type RequestMetaKey struct{}
type PageSpaceKey struct{}

// RequestMeta describes the document and request kind for host policy.
type RequestMeta struct {
	Gen  int
	Kind string
	// EntryID identifies the front-end history entry this document fetch is
	// for. The host uses it to attest the final URL; subresource kinds leave
	// it untouched.
	EntryID uint64
	// PageURL is the document that initiated the request. BlockLocal marks a
	// public document that may not reach loopback, private or link-local
	// addresses. TargetURL lets the dial guard tell a direct connection from a
	// proxy connection. PageSecure marks an HTTPS document.
	PageURL    string
	TargetURL  string
	BlockLocal bool
	PageSecure bool
}

// PageSpace records whether the document's own connection landed on a local
// address.
type PageSpace struct {
	Local atomic.Bool
}

// ErrLocalNetworkBlocked is returned when a public document tries to reach a
// loopback, private or link-local address.
var ErrLocalNetworkBlocked = errors.New("blocked local-network request")

// ErrMixedContentBlocked is returned when an HTTPS document requests a
// blockable HTTP subresource, or a redirect tries to downgrade one.
var ErrMixedContentBlocked = errors.New("blocked mixed content: HTTP subresource on an HTTPS page")

// ErrResourceTooLarge reports a response body over the host size cap.
var ErrResourceTooLarge = errors.New("resource too large")

func WithPageSpace(ctx context.Context, space *PageSpace) context.Context {
	return context.WithValue(ctx, PageSpaceKey{}, space)
}

func PageSpaceFrom(ctx context.Context) *PageSpace {
	space, _ := ctx.Value(PageSpaceKey{}).(*PageSpace)
	return space
}

// WithRequestPage records the initiating document and its local-network policy.
func WithRequestPage(ctx context.Context, page *url.URL, pageLocal bool) context.Context {
	m := RequestMetaFrom(ctx)
	if page != nil {
		m.PageURL = page.String()
		switch page.Scheme {
		case "data":
			// A data: document has no host and an opaque origin, so it is
			// never local. It cannot be navigated to any more, but internal
			// callers still get the local-network guard rather than an
			// exemption.
			m.BlockLocal = true
		case "http", "https":
			m.BlockLocal = !pageLocal
		}
		m.PageSecure = page.Scheme == "https"
	}
	return context.WithValue(ctx, RequestMetaKey{}, m)
}

// WithRequestTarget records the URL a request will contact.
func WithRequestTarget(ctx context.Context, target *url.URL) context.Context {
	if target == nil {
		return ctx
	}
	m := RequestMetaFrom(ctx)
	m.TargetURL = target.String()
	return context.WithValue(ctx, RequestMetaKey{}, m)
}

func WithRequestMeta(ctx context.Context, gen int, kind string) context.Context {
	m := RequestMetaFrom(ctx)
	m.Gen = gen
	m.Kind = kind
	return context.WithValue(ctx, RequestMetaKey{}, m)
}

// WithRequestEntry records the front-end history entry a document fetch is
// for. The loader sets it from BeginLoad; the host uses it to attest the final
// URL.
func WithRequestEntry(ctx context.Context, id uint64) context.Context {
	m := RequestMetaFrom(ctx)
	m.EntryID = id
	return context.WithValue(ctx, RequestMetaKey{}, m)
}

func WithRequestKind(ctx context.Context, kind string) context.Context {
	m, _ := ctx.Value(RequestMetaKey{}).(RequestMeta)
	m.Kind = kind
	return context.WithValue(ctx, RequestMetaKey{}, m)
}

func RequestMetaFrom(ctx context.Context) RequestMeta {
	m, _ := ctx.Value(RequestMetaKey{}).(RequestMeta)
	return m
}
