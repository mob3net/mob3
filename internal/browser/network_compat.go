package browser

import (
	"context"
	"errors"
	"net"
	"net/url"
	"syscall"

	"browser/internal/host"
)

type pageSpace = host.PageSpace
type requestMeta = host.RequestMeta

func withPageSpace(ctx context.Context, space *pageSpace) context.Context {
	return host.WithPageSpace(ctx, space)
}
func pageSpaceFrom(ctx context.Context) *pageSpace { return host.PageSpaceFrom(ctx) }
func withRequestMeta(ctx context.Context, gen int, kind string) context.Context {
	return host.WithRequestMeta(ctx, gen, kind)
}
func withRequestKind(ctx context.Context, kind string) context.Context {
	return host.WithRequestKind(ctx, kind)
}
func withRequestPage(ctx context.Context, page *url.URL, pageLocal bool) context.Context {
	return host.WithRequestPage(ctx, page, pageLocal)
}
func withRequestTarget(ctx context.Context, target *url.URL) context.Context {
	return host.WithRequestTarget(ctx, target)
}
func requestMetaFrom(ctx context.Context) requestMeta { return host.RequestMetaFrom(ctx) }

var (
	errLocalNetworkBlocked = host.ErrLocalNetworkBlocked
	errMixedContentBlocked = host.ErrMixedContentBlocked
)

func applyMixedContent(ctx context.Context, u *url.URL) (*url.URL, error) {
	return host.ApplyMixedContent(ctx, u)
}
func isMixedContentError(err error) bool { return host.IsMixedContentError(err) }
func isLocalNetworkError(err error) bool { return host.IsLocalNetworkError(err) }
func isLocalURL(u *url.URL) bool         { return host.IsLocalURL(u) }
func isLocalHost(h string) bool          { return host.IsLocalHost(h) }
func requestUsesProxy(raw string) bool   { return host.RequestUsesProxy(raw) }
func remoteAddrLocal(addr net.Addr) bool { return host.RemoteAddrLocal(addr) }
func localNetworkDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return host.LocalNetworkDialContext(ctx, network, addr)
}

var _ = errors.Is

func localNetworkControl(network, address string, c syscall.RawConn) error {
	return host.LocalNetworkControl(network, address, c)
}
