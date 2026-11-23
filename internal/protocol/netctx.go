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
		m.BlockLocal = (page.Scheme == "http" || page.Scheme == "https") && !pageLocal
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
	return context.WithValue(ctx, RequestMetaKey{}, RequestMeta{Gen: gen, Kind: kind})
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
