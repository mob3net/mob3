package engine

// SetActive marks the tab the front end is presenting. An inactive tab keeps
// its load lifecycle and state updates, but stops drawing, JavaScript timers,
// deferred image streaming and progress animation. Direct callers and tests
// start active.
func (b *Session) SetActive(active bool) {
	if b == nil || b.active == active {
		return
	}
	b.active = active
	if !active {
		if b.DeferredImages {
			b.cancelDeferredImageLoad()
			b.ClearProgress()
		}
		b.hoverRebuildPending = false
		return
	}
	// The footer figure is process-wide, so sample it as soon as this tab is
	// presented instead of showing the value cached from its last active period.
	b.usage.sample()
	if b.zoomGen != zoomGen() {
		b.Zoom = Zoom()
		b.BigTextBlocked = false
		b.rebuild()
		if limit := b.maxScroll(); b.Scroll > limit {
			b.Scroll = limit
		}
		if b.Scroll < 0 {
			b.Scroll = 0
		}
		b.zoomGen = zoomGen()
	}
	if !b.Loading && !b.ImagesPaused && !b.DeferredImages {
		b.StartDeferredImageLoad(b.viewportHeight())
	}
}

// Active reports whether the front end is presenting this tab.
func (b *Session) Active() bool { return b != nil && b.active }
