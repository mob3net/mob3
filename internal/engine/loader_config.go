package engine

import "browser/internal/protocol"

// LoaderConfig is the host/loader policy the app applies before creating a
// session.
type LoaderConfig struct {
	NoScripts    bool
	NoPDF        bool
	NoImages     bool
	IgnoreBlocks bool
	Search       string
	UserAgent    string
	Certs        string
	DeferImages  bool
}

// NewConfiguredLoader creates the loader used by one app invocation.
func NewConfiguredLoader(host protocol.Host, c LoaderConfig) (*Loader, error) {
	l := NewLoader(host)
	l.NoScripts = c.NoScripts
	l.NoPDF = c.NoPDF
	l.NoImages = c.NoImages
	l.SetSearchURL(c.Search)
	l.IgnoreBlocks = c.IgnoreBlocks
	l.DeferImageLoad = c.DeferImages
	if c.UserAgent != "" {
		l.SetUserAgent(c.UserAgent)
	}
	if err := l.SetCertSource(c.Certs); err != nil {
		return nil, err
	}
	return l, nil
}
