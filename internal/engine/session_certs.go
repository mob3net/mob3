package engine

// certSourceLabel reports the active trust store for about:config.
func (b *Session) certSourceLabel() string {
	if b.loader != nil {
		return b.loader.CertSource()
	}
	return "system"
}
