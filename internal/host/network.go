package host

import (
	"browser/internal/protocol"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

type RequestMeta = protocol.RequestMeta
type PageSpace = protocol.PageSpace

func WithPageSpace(ctx context.Context, space *PageSpace) context.Context {
	return protocol.WithPageSpace(ctx, space)
}

func PageSpaceFrom(ctx context.Context) *PageSpace { return protocol.PageSpaceFrom(ctx) }

func WithRequestPage(ctx context.Context, page *url.URL, pageLocal bool) context.Context {
	return protocol.WithRequestPage(ctx, page, pageLocal)
}

func WithRequestTarget(ctx context.Context, target *url.URL) context.Context {
	return protocol.WithRequestTarget(ctx, target)
}

func WithRequestOrigin(ctx context.Context, origin string) context.Context {
	return protocol.WithRequestOrigin(ctx, origin)
}

func WithRequestMeta(ctx context.Context, gen int, kind string) context.Context {
	return protocol.WithRequestMeta(ctx, gen, kind)
}

func WithRequestKind(ctx context.Context, kind string) context.Context {
	return protocol.WithRequestKind(ctx, kind)
}

func RequestMetaFrom(ctx context.Context) RequestMeta { return protocol.RequestMetaFrom(ctx) }

// requestUsesProxy reports whether the environment routes this URL through an
// HTTP proxy. The proxy resolves the target, so the local-network guard cannot
// inspect it and must not mistake the proxy address for the target.
func RequestUsesProxy(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return false
	}
	proxy, err := http.ProxyFromEnvironment(req)
	return err == nil && proxy != nil
}

var (
	ErrLocalNetworkBlocked = protocol.ErrLocalNetworkBlocked
	ErrMixedContentBlocked = protocol.ErrMixedContentBlocked
)

// applyMixedContent upgrades an HTTP image requested by an HTTPS document to
// HTTPS, and rejects every other HTTP subresource. Upgraded images are not
// retried over HTTP if the HTTPS request fails.
func ApplyMixedContent(ctx context.Context, u *url.URL) (*url.URL, error) {
	meta := RequestMetaFrom(ctx)
	if u == nil || !meta.PageSecure || u.Scheme != "http" {
		return u, nil
	}
	if meta.Kind == "img" && net.ParseIP(u.Hostname()) == nil {
		up := *u
		up.Scheme = "https"
		return &up, nil
	}
	return u, ErrMixedContentBlocked
}

func IsMixedContentError(err error) bool {
	return errors.Is(err, ErrMixedContentBlocked)
}

// isLocalHost reports whether a URL host is a loopback, private, link-local or
// local name. Hosts that only resolve locally are caught after DNS resolution
// by localNetworkControl.
func IsLocalHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return false
	}
	if i := strings.IndexByte(host, '%'); i >= 0 { // IPv6 zone
		host = host[:i]
	}
	if ip := net.ParseIP(host); ip != nil {
		return IsLocalIP(ip)
	}
	return host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		host == "local" || strings.HasSuffix(host, ".local")
}

func IsLocalIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4[0] == 0: // 0.0.0.0/8, routed to loopback by some stacks
			return true
		case ip4[0] == 100 && ip4[1]&0xc0 == 0x40: // 100.64.0.0/10 CGNAT
			return true
		case ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 0: // 192.0.0.0/24
			return true
		case ip4[0] == 198 && ip4[1]&0xfe == 18: // 198.18.0.0/15
			return true
		case ip4[0] >= 240: // 240.0.0.0/4 reserved
			return true
		}
	}
	return false
}

func IsLocalURL(u *url.URL) bool {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return IsLocalHost(u.Hostname())
}

// localNetworkControl runs after DNS resolution and before the connect, so a
// public name that resolves to a local address is still caught.
func LocalNetworkControl(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil
	}
	if IsLocalHost(host) {
		return ErrLocalNetworkBlocked
	}
	return nil
}

var (
	plainDialer = &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	guardedDialer = &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   LocalNetworkControl,
	}
)

// localNetworkDialContext picks the guarded dialer for requests a public
// document initiated. The transport passes the request context through, so
// the request metadata survives into the dial.
func LocalNetworkDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	m := RequestMetaFrom(ctx)
	if m.BlockLocal && !RequestUsesProxy(m.TargetURL) {
		return guardedDialer.DialContext(ctx, network, addr)
	}
	return plainDialer.DialContext(ctx, network, addr)
}

func IsLocalNetworkError(err error) bool {
	return errors.Is(err, ErrLocalNetworkBlocked)
}

// remoteAddrLocal reports whether a completed connection's peer is local.
func RemoteAddrLocal(addr net.Addr) bool {
	if addr == nil {
		return false
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return false
	}
	return IsLocalHost(host)
}

// NewHTTPTransport builds the pooled transport.
func NewHTTPTransport(pool *x509.CertPool) *http.Transport {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           LocalNetworkDialContext,
		MaxIdleConns:          16,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		// A custom TLS config disables the automatic HTTP/2 setup, so ask for
		// it explicitly and keep it on for both trust stores.
		ForceAttemptHTTP2: true,
	}
	if pool != nil {
		tr.TLSClientConfig = &tls.Config{RootCAs: pool}
	}
	return tr
}

// NewHTTPClient builds the session HTTP client. The cookie jar and transport
// are host-owned; the browser only uses the resulting client.
func NewHTTPClient(jar http.CookieJar, transport http.RoundTripper) *http.Client {
	return &http.Client{
		Jar:       jar,
		Transport: transport,
		Timeout:   45 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}
