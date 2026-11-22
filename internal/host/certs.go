package host

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

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

// EmbeddedCACertPool builds the embedded trust store once per process from the
// official bundle. Constraints the X.509 encoding cannot express (NSS
// distrust-after dates) are attached with AddCertWithConstraint so chains
// through a constrained root are rejected after that date.
var EmbeddedCACertPool = sync.OnceValues(func() (*x509.CertPool, error) {
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
func ResolveCertSource(source string) (string, *x509.CertPool, error) {
	s := strings.TrimSpace(source)
	if s == "" || strings.EqualFold(s, CertSourceSystem) {
		return CertSourceSystem, nil, nil
	}
	if strings.EqualFold(s, CertSourceEmbedded) {
		pool, err := EmbeddedCACertPool()
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
type CertTransport struct {
	mu   sync.RWMutex
	base http.RoundTripper
}

func (t *CertTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.RLock()
	base := t.base
	t.mu.RUnlock()
	return base.RoundTrip(req)
}

// swap installs a new underlying transport and closes the idle connections of
// the previous one, so no pooled TLS connection keeps the old trust store.
func (t *CertTransport) Swap(next http.RoundTripper) {
	t.mu.Lock()
	old := t.base
	t.base = next
	t.mu.Unlock()
	if tr, ok := old.(*http.Transport); ok {
		tr.CloseIdleConnections()
	}
}

// current returns the active transport, for tests and diagnostics.
func (t *CertTransport) Current() *http.Transport {
	t.mu.RLock()
	defer t.mu.RUnlock()
	tr, _ := t.base.(*http.Transport)
	return tr
}

// NewCertTransport installs a stable transport wrapper.
func NewCertTransport(base http.RoundTripper) *CertTransport { return &CertTransport{base: base} }
