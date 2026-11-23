package browser

import "time"

// startJS creates a fresh runtime and runs the page's scripts in order.
func (b *Browser) startJS(page *Page) {
	b.stopJS()
	if b.NoJS || page == nil {
		return
	}
	j := newJSRuntime(b, page)
	if b.JSPageBudget > 0 {
		j.SetPageBudget(b.JSPageBudget)
	}
	b.JS = j
	j.StartPage(page)
	b.moduleErr = j.ModuleErr()
}

func (b *Browser) stopJS() {
	if b.JS != nil {
		b.JS.Stop()
		b.JS = nil
	}
}

func (b *Browser) nextJSTimerDelay() (time.Duration, bool) {
	if b.consoleOpen {
		return 0, false
	}
	var next time.Time
	if b.hoverRebuildPending {
		next = b.hoverRebuildAt
	}
	if b.JS != nil {
		if d, ok := b.JS.NextTimerDelay(); ok {
			at := time.Now().Add(d)
			if next.IsZero() || at.Before(next) {
				next = at
			}
		}
	}
	if next.IsZero() {
		return 0, false
	}
	d := time.Until(next)
	if d < 0 {
		d = 0
	}
	return d, true
}

func (b *Browser) runJSTimers() bool {
	now := time.Now()
	if b.consoleOpen {
		return false
	}
	if b.hoverRebuildPending && !now.Before(b.hoverRebuildAt) {
		b.hoverRebuildPending = false
		b.rebuild()
		return true
	}
	if b.JS == nil {
		return false
	}
	b.JS.Tick(now)
	if b.consumePendingJS() {
		return false
	}
	if !b.JS.Dirty() {
		return false
	}
	wait := jsRedrawInterval
	if 2*b.LastBuild > wait {
		wait = 2 * b.LastBuild
	}
	if !b.LastRebuild.IsZero() && now.Sub(b.LastRebuild) < wait {
		return false
	}
	b.JS.ConsumeDirty()
	b.rebuild()
	return true
}

func (b *Browser) finishJSImport(task jsImportTask) {
	if b.JS != nil {
		b.JS.FinishImport(task)
	}
}
