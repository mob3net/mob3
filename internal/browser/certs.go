package browser

import (
	"crypto/x509"

	"browser/internal/host"
)

// SetCertSource selects the TLS trust store for every later request.
func (l *Loader) SetCertSource(source string) error {
	if err := l.client.SetCertSource(source); err != nil {
		return err
	}
	l.certSource = l.client.CertSource()
	l.certPool = l.client.CertPool()
	return nil
}

// CertSource reports the active trust-store label for the app's config view.
func (l *Loader) CertSource() string {
	if l == nil {
		return host.CertSourceSystem
	}
	return l.client.CertSource()
}

// certSourceLabel reports the active trust store for about:config.
func (b *Browser) certSourceLabel() string {
	if b.loader != nil {
		return b.loader.CertSource()
	}
	return host.CertSourceSystem
}

const (
	CertSourceSystem   = host.CertSourceSystem
	CertSourceEmbedded = host.CertSourceEmbedded
)

var embeddedCACertPool = host.EmbeddedCACertPool

func resolveCertSource(source string) (string, *x509.CertPool, error) {
	return host.ResolveCertSource(source)
}
