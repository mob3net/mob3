package engine

// Release detaches a closed tab from the shared budget manager and stops its
// background work. The transport must be closed first: the handler goroutine
// owns every other session field.
func (b *Session) Release() {
	if b == nil {
		return
	}
	b.cancelLoadNow()
	b.cancelDeferredImageLoad()
	b.StopJS()
	if b.budgets != nil {
		b.budgets.Unregister(b)
		b.budgets = nil
	}
}
