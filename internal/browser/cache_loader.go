package browser

// setCacheLimitMB updates the session resource-cache budget in MiB.
func (l *Loader) setCacheLimitMB(mb int) {
	if l == nil || l.host == nil {
		return
	}
	if mb < 0 {
		mb = 0
	}
	l.host.SetCacheLimitBytes(int64(mb) << 20)
}
