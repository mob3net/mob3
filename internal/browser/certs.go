package browser

import (
	"crypto/x509"
	"net/http"

	"browser/internal/host"
)

// SetCertSource selects the TLS trust store for every later request.
func (l *Loader) SetCertSource(source string) error {
	label, pool, err := host.ResolveCertSource(source)
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

// CertSource reports the active trust-store label for the app's config view.
func (l *Loader) CertSource() string {
	if l != nil && l.certSource != "" {
		return l.certSource
	}
	return host.CertSourceSystem
}

// certSourceLabel reports the active trust store for about:config.
func (b *Browser) certSourceLabel() string {
	if b.loader != nil && b.loader.certSource != "" {
		return b.loader.certSource
	}
	return host.CertSourceSystem
}

// certTransport wraps the host transport for the Loader's old API.
type certTransport struct{ *host.CertTransport }

func (t *certTransport) swap(next http.RoundTripper) { t.CertTransport.Swap(next) }
func (t *certTransport) current() *http.Transport    { return t.CertTransport.Current() }

func newCertTransport(base http.RoundTripper) *certTransport {
	return &certTransport{host.NewCertTransport(base)}
}

const (
	CertSourceSystem   = host.CertSourceSystem
	CertSourceEmbedded = host.CertSourceEmbedded
)

func newHTTPTransport(pool *x509.CertPool) *http.Transport { return host.NewHTTPTransport(pool) }

var embeddedCACertPool = host.EmbeddedCACertPool

func resolveCertSource(source string) (string, *x509.CertPool, error) {
	return host.ResolveCertSource(source)
}
