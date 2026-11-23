package browser

import "time"

func (b *Browser) startJS(page *Page) { b.Session.StartJS(page, b) }
func (b *Browser) stopJS()            { b.Session.StopJS() }
func (b *Browser) nextJSTimerDelay() (time.Duration, bool) {
	return b.Session.NextJSTimerDelay(b)
}
func (b *Browser) runJSTimers() bool { return b.Session.RunJSTimers(b) }
func (b *Browser) finishJSImport(task jsImportTask) {
	b.Session.FinishJSImport(task)
}
