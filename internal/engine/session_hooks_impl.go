package engine

import (
	"context"
	"time"

	"browser/internal/protocol"
)

func (b *Session) BeginPage() {
	if b.loader != nil {
		b.loader.BeginPage()
	}
}

func (b *Session) ResetUI() {
	b.selectMenu = nil
	b.selectFrontend = false
	b.selectID = 0
	b.downloadFrontend = false
	b.noticeFrontend = false
	b.HintsFrontend = false
	b.quitPending = false
	b.jsErr = false
	b.moduleErr = false
}

func (b *Session) ResetConsole()        { b.resetConsole() }
func (b *Session) Viewport() (int, int) { return b.viewport() }
func (b *Session) NavigateRequest(method, target string, body []byte, contentType string, push bool) {
	b.navigateRequest(method, target, body, contentType, push)
}
func (b *Session) ActivateHintControl(n *Node, r Rect) { b.activateHintControl(n, r) }
func (b *Session) OpenSelectMenu(n *Node)              { b.openSelectMenu(n) }
func (b *Session) ConsoleOpen() bool                   { return b.consoleOpen }
func (b *Session) HoverRebuild() (bool, time.Time)     { return b.hoverRebuildPending, b.hoverRebuildAt }
func (b *Session) ClearHoverRebuild()                  { b.hoverRebuildPending = false }
func (b *Session) NewJSHost(f *Frame) JSHost           { return newFrameJSHost(b, f) }
func (b *Session) SetModuleErr(v bool)                 { b.moduleErr = v }
func (b *Session) StartJS(f *Frame) {
	if f != nil {
		b.startJSFrame(f, f.Page, b)
	}
}
func (b *Session) ApplyPendingJS(duringLoad bool, entryID uint64, historyPush bool) bool {
	return b.applyPendingJS(duringLoad, entryID, historyPush)
}
func (b *Session) NavigateEntry(method, target string, body []byte, contentType string, entryID uint64, historyPush, force bool) {
	b.navigateRequestEntry(method, target, body, contentType, entryID, historyPush, force)
}
func (b *Session) Rebuild()            { b.rebuild() }
func (b *Session) InvalidateFrame()    {}
func (b *Session) SetStatus(s string)  { b.status = s }
func (b *Session) ViewportHeight() int { return b.viewportHeight() }
func (b *Session) PageLoadStatus(page *Page, readerFailed bool) string {
	return b.pageLoadStatus(page, readerFailed)
}
func (b *Session) TakePendingStatus() string {
	v := b.pendingStatus
	b.pendingStatus = ""
	return v
}
func (b *Session) ShowDownload(url, name string) { b.showDownload(url, name) }
func (b *Session) EmitHistoryCommit(c protocol.HistoryCommit) {
	b.emitHistoryCommit(c)
}

func (b *Session) Init(v protocol.Init) {
	if v.CellW > 0 {
		b.CellW = v.CellW
	}
	if v.CellH > 0 {
		b.CellH = v.CellH
	}
	if v.Zoom > 0 {
		b.Zoom = v.Zoom
		SetZoom(v.Zoom)
		b.zoomGen = zoomGen()
	}
	b.color = v.Caps&protocol.CapColor != 0
	b.resizeWithContent(v.Width, v.Height, v.ContentTop, v.ContentHeight)
}

func (b *Session) Resize(v protocol.Resize) {
	b.resizeWithContent(v.Width, v.Height, v.ContentTop, v.ContentHeight)
	if v.CellW > 0 {
		b.CellW = v.CellW
	}
	if v.CellH > 0 {
		b.CellH = v.CellH
	}
	b.applyImageCap()
}

func (b *Session) SetHistoryView(v protocol.History) { b.setHistoryView(v) }
func (b *Session) RestoreEntry(entryID uint64, rawURL string, scroll int) {
	b.restoreEntry(entryID, rawURL, scroll)
}
func (b *Session) ProtocolMouse(v protocol.Mouse) bool {
	return b.HandleMouse(b.protocolMouse(v))
}
func (b *Session) ApplyScroll(v protocol.Scroll) {
	switch {
	case v.Top:
		b.scrollBy(-b.Scroll)
	case v.Bottom:
		b.scrollBy(b.maxScroll() - b.Scroll)
	case v.HasTo:
		b.scrollBy(v.To - b.Scroll)
	default:
		b.scrollBy(v.Delta)
	}
}
func (b *Session) ApplyApp(v protocol.App) (bool, error) { return b.handleAppAction(v) }
func (b *Session) ApplyFind(v protocol.Find)             { b.applyFindCommand(v) }
func (b *Session) ApplySelect(v protocol.SelectResult)   { b.applySelectResult(v) }
func (b *Session) ApplyModal(v protocol.ModalClose)      { b.applyModalClose(v) }
func (b *Session) ApplyHint(v protocol.HintResult)       { b.applyHintResult(v) }
func (b *Session) ApplyConsole(v protocol.ConsoleAction) { b.applyConsoleAction(v) }
func (b *Session) LogClientError(msg string)             { b.logEngineError(msg) }
func (b *Session) Abort()                                { b.abortGen.Add(1) }

func (b *Session) BeginSession(ctx context.Context, abort <-chan struct{}, in <-chan protocol.Command, out protocol.EventSink) {
	b.sessionCtx, b.sessionIn, b.sessionAbort = ctx, in, abort
	b.sessionOut = out
	b.sessionQuit = false
	b.LastState = protocol.State{}
	b.emitFrames = true
	b.frontendChrome = true
}

func (b *Session) EndSession() {
	b.sessionCtx, b.sessionIn, b.sessionAbort = nil, nil, nil
	b.sessionOut = nil
	b.frameReady = false
	b.emitFrames = false
	b.frontendChrome = false
}

func (b *Session) Draw()                 { b.draw() }
func (b *Session) State() protocol.State { return b.stateMessage() }
func (b *Session) EmitPendingFrame()     { b.emitPendingFrame() }
func (b *Session) SessionQuit() bool     { return b.sessionQuit }
func (b *Session) UsageFooter() string   { return b.usage.footer() }
func (b *Session) SampleUsage() string   { b.sampleUsage(); return b.usage.footer() }
func (b *Session) ApplyImageBatch(batch ImageBatch) bool {
	return b.applyDeferredImageBatch(batch)
}
