package engine

import "time"

func (b *Session) startJS(page *Page) { b.StartJSWithHooks(page, b) }
func (b *Session) stopJS()            { b.StopJS() }
func (b *Session) nextJSTimerDelay() (time.Duration, bool) {
	return b.NextJSTimerDelay(b)
}
func (b *Session) runJSTimers() bool { return b.RunJSTimers(b) }
func (b *Session) finishJSImport(task jsImportTask) {
	b.FinishJSImport(task)
}
