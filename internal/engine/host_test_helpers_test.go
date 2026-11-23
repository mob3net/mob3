package engine

import (
	"context"
	"crypto/x509"
	"net"
	"net/http"
	"net/url"
	"syscall"

	"browser/internal/host"
	"browser/internal/protocol"
)

func overlayLayer(n *Node) int { return OverlayLayer(n) }

func testHost(l *Loader) *host.Session {
	if l == nil {
		return nil
	}
	h, _ := l.Host.(*host.Session)
	return h
}

func testHTTP(l *Loader) *http.Client { return testHost(l).Client().HTTP() }

func testSetHTTPClient(l *Loader, c *http.Client) { testHost(l).SetHTTPClient(c) }

func newCookieJar() *host.CookieJar { return host.NewCookieJar() }

func newResourceCache(limit int64) *host.ResourceCache { return host.NewResourceCache(limit) }
func cacheControlAllows(header string) bool            { return host.CacheControlAllows(header) }

const (
	CertSourceSystem   = host.CertSourceSystem
	CertSourceEmbedded = host.CertSourceEmbedded
)

var embeddedCACertPool = host.EmbeddedCACertPool

func resolveCertSource(source string) (string, *x509.CertPool, error) {
	return host.ResolveCertSource(source)
}

type pageSpace = protocol.PageSpace
type requestMeta = protocol.RequestMeta

func withPageSpace(ctx context.Context, space *pageSpace) context.Context {
	return protocol.WithPageSpace(ctx, space)
}
func pageSpaceFrom(ctx context.Context) *pageSpace { return protocol.PageSpaceFrom(ctx) }
func withRequestMeta(ctx context.Context, gen int, kind string) context.Context {
	return protocol.WithRequestMeta(ctx, gen, kind)
}
func withRequestKind(ctx context.Context, kind string) context.Context {
	return protocol.WithRequestKind(ctx, kind)
}
func withRequestPage(ctx context.Context, page *url.URL, local bool) context.Context {
	return protocol.WithRequestPage(ctx, page, local)
}
func withRequestTarget(ctx context.Context, target *url.URL) context.Context {
	return protocol.WithRequestTarget(ctx, target)
}
func requestMetaFrom(ctx context.Context) requestMeta { return protocol.RequestMetaFrom(ctx) }

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
func localNetworkControl(network, address string, c syscall.RawConn) error {
	return host.LocalNetworkControl(network, address, c)
}

var (
	errLocalNetworkBlocked = host.ErrLocalNetworkBlocked
	errMixedContentBlocked = host.ErrMixedContentBlocked
)
