package host

import (
	"crypto/x509"
	"net/http"
)

// Client owns the session HTTP client and its swappable TLS transport. The
// browser holds this value and never needs net/http types directly.
type Client struct {
	http       *http.Client
	transport  *CertTransport
	certSource string
	certPool   *x509.CertPool
}

// NewClient builds the default session client around a cookie jar.
func NewClient(jar http.CookieJar) *Client {
	tr := NewCertTransport(NewHTTPTransport(nil))
	return &Client{
		http:       NewHTTPClient(jar, tr),
		transport:  tr,
		certSource: CertSourceSystem,
	}
}

// NewClientFromHTTP wraps an existing client for tests and injected transports.
func NewClientFromHTTP(c *http.Client) *Client {
	if c == nil {
		c = &http.Client{}
	}
	return &Client{http: c, certSource: CertSourceSystem}
}

// HTTP returns the underlying client for the host fetch path.
func (c *Client) HTTP() *http.Client {
	if c == nil {
		return http.DefaultClient
	}
	return c.http
}

// SetCertSource selects the TLS trust store for later requests.
func (c *Client) SetCertSource(source string) error {
	label, pool, err := ResolveCertSource(source)
	if err != nil {
		return err
	}
	c.certSource = label
	c.certPool = pool
	if c.transport != nil {
		c.transport.Swap(NewHTTPTransport(pool))
	}
	return nil
}

// CertSource reports the active trust-store label.
func (c *Client) CertSource() string {
	if c != nil && c.certSource != "" {
		return c.certSource
	}
	return CertSourceSystem
}

// CertPool returns the active custom root pool, or nil for the platform store.
func (c *Client) CertPool() *x509.CertPool {
	if c == nil {
		return nil
	}
	return c.certPool
}

// Transport returns the active HTTP transport, for tests and diagnostics.
func (c *Client) Transport() *http.Transport {
	if c == nil || c.transport == nil {
		if c != nil {
			tr, _ := c.http.Transport.(*http.Transport)
			return tr
		}
		return nil
	}
	return c.transport.Current()
}
