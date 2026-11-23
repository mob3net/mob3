package browser

// SetCertSource selects the TLS trust store for every later request.
func (l *Loader) SetCertSource(source string) error {
	if l.host == nil {
		return nil
	}
	if err := l.host.SetCertSource(source); err != nil {
		return err
	}
	l.certSource = l.host.CertSource()
	l.certPool = l.host.CertPool()
	return nil
}

// CertSource reports the active trust-store label for the app's config view.
func (l *Loader) CertSource() string {
	if l == nil || l.host == nil || l.certSource == "" {
		return "system"
	}
	return l.certSource
}

// certSourceLabel reports the active trust store for about:config.
func (b *Browser) certSourceLabel() string {
	if b.loader != nil {
		return b.loader.CertSource()
	}
	return "system"
}
