package engine

// SetCertSource selects the TLS trust store for every later request.
func (l *Loader) SetCertSource(source string) error {
	if l.Host == nil {
		return nil
	}
	if err := l.Host.SetCertSource(source); err != nil {
		return err
	}
	l.CertStore = l.Host.CertSource()
	return nil
}

// CertSource reports the active trust-store label for the config view.
func (l *Loader) CertSource() string {
	if l == nil || l.Host == nil || l.CertStore == "" {
		return "system"
	}
	return l.CertStore
}
