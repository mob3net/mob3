package browser

import "time"

// startJS creates a fresh runtime and runs the page's scripts in order.
func (b *Browser) startJS(page *Page) {
	b.stopJS()
	if b.noJS || page == nil {
		return
	}
	j := newJSRuntime(b, page)
	if b.jsPageBudget > 0 {
		j.SetPageBudget(b.jsPageBudget)
	}
	b.js = j
	j.StartPage(page)
	b.moduleErr = j.ModuleErr()
}

func (b *Browser) stopJS() {
	if b.js != nil {
		b.js.Stop()
		b.js = nil
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
	if b.js != nil {
		if d, ok := b.js.NextTimerDelay(); ok {
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
	if b.js == nil {
		return false
	}
	b.js.Tick(now)
	if b.consumePendingJS() {
		return false
	}
	if !b.js.Dirty() {
		return false
	}
	wait := jsRedrawInterval
	if 2*b.lastBuild > wait {
		wait = 2 * b.lastBuild
	}
	if !b.lastRebuild.IsZero() && now.Sub(b.lastRebuild) < wait {
		return false
	}
	b.js.ConsumeDirty()
	b.rebuild()
	return true
}

func (b *Browser) finishJSImport(task jsImportTask) {
	if b.js != nil {
		b.js.FinishImport(task)
	}
}
