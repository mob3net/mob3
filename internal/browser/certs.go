package browser

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"time"

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

var embeddedCACertPool = host.EmbeddedCACertPool

func resolveCertSource(source string) (string, *x509.CertPool, error) {
	return host.ResolveCertSource(source)
}
