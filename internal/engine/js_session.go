package engine

// newJSRuntime creates the runtime for one page in a session.
func newJSRuntime(b *Session, page *Page) *JSRuntime {
	j := NewJSRuntime(newJSHost(b), page)
	if b != nil {
		j.SetMemLimit(b.JSMemLimit)
	}
	return j
}
