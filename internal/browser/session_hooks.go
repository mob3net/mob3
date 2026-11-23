package browser

import (
	"time"

	"browser/internal/engine"
	"browser/internal/protocol"
)

func (b *Browser) BeginPage() {
	if b.loader != nil {
		b.loader.beginPage()
	}
}

func (b *Browser) ResetUI() {
	b.selectMenu = nil
	b.selectHits = nil
	b.selectFrontend = false
	b.selectID = 0
	b.downloadFrontend = false
	b.noticeFrontend = false
	b.HintsFrontend = false
	b.viewMenu = nil
	b.viewMenuHits = nil
	b.zoomMenu = nil
	b.zoomMenuHits = nil
	b.quitPending = false
	b.jsErr = false
	b.moduleErr = false
}

func (b *Browser) ResetConsole()        { b.resetConsole() }
func (b *Browser) Viewport() (int, int) { return b.viewport() }
func (b *Browser) NavigateRequest(method, target string, body []byte, contentType string, push bool) {
	b.navigateRequest(method, target, body, contentType, push)
}
func (b *Browser) ActivateHintControl(n *Node, r Rect) { b.activateHintControl(n, r) }
func (b *Browser) OpenSelectMenu(n *Node)              { b.openSelectMenu(n) }
func (b *Browser) ConsoleOpen() bool                   { return b.consoleOpen }
func (b *Browser) HoverRebuild() (bool, time.Time)     { return b.hoverRebuildPending, b.hoverRebuildAt }
func (b *Browser) ClearHoverRebuild()                  { b.hoverRebuildPending = false }
func (b *Browser) NewJSHost() engine.JSHost            { return newJSHost(b) }
func (b *Browser) SetModuleErr(v bool)                 { b.moduleErr = v }
func (b *Browser) StartJS(page *Page)                  { b.startJS(page) }
func (b *Browser) ApplyPendingJS(duringLoad bool, entryID uint64, historyPush bool) bool {
	return b.applyPendingJS(duringLoad, entryID, historyPush)
}
func (b *Browser) NavigateEntry(method, target string, body []byte, contentType string, entryID uint64, historyPush, force bool) {
	b.navigateRequestEntry(method, target, body, contentType, entryID, historyPush, force)
}
func (b *Browser) Rebuild()            { b.rebuild() }
func (b *Browser) InvalidateFrame()    { b.prev = nil }
func (b *Browser) SetStatus(s string)  { b.status = s }
func (b *Browser) ViewportHeight() int { return b.viewportHeight() }
func (b *Browser) PageLoadStatus(page *Page, readerFailed bool) string {
	return b.pageLoadStatus(page, readerFailed)
}
func (b *Browser) TakePendingStatus() string {
	v := b.pendingStatus
	b.pendingStatus = ""
	return v
}
func (b *Browser) ShowDownload(url, name string) { b.showDownload(url, name) }
func (b *Browser) EmitHistoryCommit(c protocol.HistoryCommit) {
	b.emitHistoryCommit(c)
}
