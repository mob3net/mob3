package browser

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	// The fallback package registers the same official roots with
	// x509.SetFallbackRoots, so a machine whose platform store is missing
	// entirely still verifies HTTPS under the default -certs=system. It only
	// engages when no platform roots are available; it does not override a
	// present but stale store, which is what -certs=embedded is for.
	_ "golang.org/x/crypto/x509roots/fallback"
	"golang.org/x/crypto/x509roots/fallback/bundle"
)

// TLS trust store selection.
//
// The embedded roots are the official bundle from
// golang.org/x/crypto/x509roots/fallback, parsed from Mozilla's NSS trust
// store and carrying its distrust-after constraints. It is compiled into the
// binary so a machine whose platform store is missing, stale or no longer
// updated can still verify HTTPS. There is no data file to refresh: bump the
// module and re-vendor it.
//
//	go get golang.org/x/crypto/x509roots/fallback@latest
//	go mod vendor
//
// Keep THIRD_PARTY_NOTICES and docs/cli.md in sync when the module changes.

// CertSourceSystem and CertSourceEmbedded name the built-in sources accepted
// by -certs. No certificate file is read: the binary has no filesystem input.
const (
	CertSourceSystem   = "system"
	CertSourceEmbedded = "embedded"
)

// embeddedCACertPool builds the embedded trust store once per process from the
// official bundle. Constraints the X.509 encoding cannot express (NSS
// distrust-after dates) are attached with AddCertWithConstraint so chains
// through a constrained root are rejected after that date.
var embeddedCACertPool = sync.OnceValues(func() (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	added := 0
	for root := range bundle.Roots() {
		cert, err := x509.ParseCertificate(root.Certificate)
		if err != nil {
			return nil, fmt.Errorf("embedded CA bundle: %w", err)
		}
		if root.Constraint != nil {
			pool.AddCertWithConstraint(cert, root.Constraint)
		} else {
			pool.AddCert(cert)
		}
		added++
	}
	if added == 0 {
		return nil, errors.New("embedded CA bundle is empty")
	}
	return pool, nil
})

// resolveCertSource turns a -certs value into a label and a pool. A nil pool
// means the platform store, which is what an unset TLS config asks for.
func resolveCertSource(source string) (string, *x509.CertPool, error) {
	s := strings.TrimSpace(source)
	if s == "" || strings.EqualFold(s, CertSourceSystem) {
		return CertSourceSystem, nil, nil
	}
	if strings.EqualFold(s, CertSourceEmbedded) {
		pool, err := embeddedCACertPool()
		if err != nil {
			return "", nil, err
		}
		return CertSourceEmbedded, pool, nil
	}
	return "", nil, errors.New("certificate source must be system or embedded")
}

// SetCertSource selects the TLS trust store for every later request: "system"
// (the default) uses the platform store, and "embedded" uses the CA bundle
// compiled into the binary. Certificate files are not supported.
// The selected source replaces the platform store rather than adding to it, so
// trust decisions are the same on every machine. It may be called while
// requests are in flight: in-flight requests finish on the old transport and
// pooled connections to the old store are closed.
func (l *Loader) SetCertSource(source string) error {
	label, pool, err := resolveCertSource(source)
	if err != nil {
		return err
	}
	l.certSource = label
	l.certPool = pool
	if l.transport != nil {
		l.transport.swap(newHTTPTransport(pool))
	}
	return nil
}

// certSourceLabel reports the active trust store for about:config.
func (b *Browser) certSourceLabel() string {
	if b.loader != nil && b.loader.certSource != "" {
		return b.loader.certSource
	}
	return CertSourceSystem
}

// certTransport is the stable RoundTripper installed on the Loader's client.
// Its underlying transport is replaced when the certificate source changes, so
// a swap never races an in-flight request: each request reads the current base
// under the lock and finishes on it.
type certTransport struct {
	mu   sync.RWMutex
	base http.RoundTripper
}

func (t *certTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.RLock()
	base := t.base
	t.mu.RUnlock()
	return base.RoundTrip(req)
}

// swap installs a new underlying transport and closes the idle connections of
// the previous one, so no pooled TLS connection keeps the old trust store.
func (t *certTransport) swap(next http.RoundTripper) {
	t.mu.Lock()
	old := t.base
	t.base = next
	t.mu.Unlock()
	if tr, ok := old.(*http.Transport); ok {
		tr.CloseIdleConnections()
	}
}

// current returns the active transport, for tests and diagnostics.
func (t *certTransport) current() *http.Transport {
	t.mu.RLock()
	defer t.mu.RUnlock()
	tr, _ := t.base.(*http.Transport)
	return tr
}

// newHTTPTransport builds the connection pool used for pages and resources. A
// non-nil pool replaces the platform trust store for TLS.
func newHTTPTransport(pool *x509.CertPool) *http.Transport {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           localNetworkDialContext,
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
