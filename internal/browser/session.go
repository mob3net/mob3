package browser

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"browser/internal/protocol"
)

// Serve runs the interactive session on its own goroutine. Commands arrive from
// the trusted front end through the transport; page loads, JavaScript timers,
// deferred images and imports are serviced here, and the resulting shell state
// leaves as events.
//
// The front end owns text/native presentation and normal header/footer chrome;
// this loop emits frames and state. The engine still composes chrome for
// overlays, the console and the find bar and handles shell dialogs.
func (b *Browser) Serve(ctx context.Context, abort <-chan struct{}, in <-chan protocol.Command, out protocol.EventSink) error {
	var jsTimer, progressTimer *time.Timer
	defer func() {
		if jsTimer != nil {
			jsTimer.Stop()
		}
		if progressTimer != nil {
			progressTimer.Stop()
		}
		b.cancelLoadNow()
		b.stopJS()
	}()
	usageTicker := time.NewTicker(2 * time.Second)
	defer usageTicker.Stop()

	b.sessionCtx, b.sessionIn, b.sessionAbort = ctx, in, abort
	b.sessionOut = out
	b.sessionQuit = false
	b.lastState = protocol.State{}
	b.emitFrames = true
	b.frontendChrome = true
	defer func() {
		b.sessionCtx, b.sessionIn, b.sessionAbort = nil, nil, nil
		b.sessionOut = nil
		b.frameReady = false
		b.chromeReady = false
		b.emitFrames = false
		b.frontendChrome = false
	}()
	b.sampleUsage()
	for {
		// A heavy page evicted from the history cache asks for one scavenge so
		// its memory leaves the process instead of waiting for the background
		// scavenger. This runs between frames, never inside a load.
		if b.takeScavengePending() {
			debug.FreeOSMemory()
		}
		if b.skipDraw {
			b.skipDraw = false
		} else {
			b.draw()
			// A redraw can change pixels without changing the shell state, so
			// only cross the seam when the snapshot differs. State precedes the
			// frame so the presenter knows the scroll offset and native mode
			// before it sees the pixels.
			if st := b.stateMessage(); st != b.lastState {
				b.lastState = st
				if err := out.Send(protocol.Event{Body: st}); err != nil {
					return err
				}
			}
			b.emitPendingFrame()
		}
		var timerC <-chan time.Time
		if d, ok := b.nextJSTimerDelay(); ok {
			if jsTimer == nil {
				jsTimer = time.NewTimer(d)
			} else {
				if !jsTimer.Stop() {
					select {
					case <-jsTimer.C:
					default:
					}
				}
				jsTimer.Reset(d)
			}
			timerC = jsTimer.C
		}
		var progressC <-chan time.Time
		if b.progressActive() {
			if progressTimer == nil {
				progressTimer = time.NewTimer(progressTickInterval)
			} else {
				if !progressTimer.Stop() {
					select {
					case <-progressTimer.C:
					default:
					}
				}
				progressTimer.Reset(progressTickInterval)
			}
			progressC = progressTimer.C
		}
		select {
		case <-ctx.Done():
			return nil
		case <-abort:
			// AbortSession already counted the request on the front-end
			// goroutine. This is only a wakeup so the loop can redraw and let
			// the following key command run.
		case cmd, ok := <-in:
			if !ok {
				return nil
			}
			quit, err := b.handleCommand(cmd)
			if err != nil {
				return err
			}
			if quit || b.sessionQuit {
				return out.Send(protocol.Event{Body: protocol.Quit{}})
			}
		case res := <-b.loadCh:
			b.finishLoad(res)
		case batch := <-b.imageCh:
			if !b.applyDeferredImageBatch(batch) {
				b.skipDraw = true
			}
		case task := <-b.importCh:
			b.finishJSImport(task)
			if b.js != nil && b.js.ConsumeDirty() {
				b.rebuild()
			}
			b.consumePendingJS()
		case <-timerC:
			// Repaint only when a timer rebuilt the page; an idle page must not
			// repaint at every timer wakeup.
			if !b.runJSTimers() {
				b.skipDraw = true
			}
		case <-progressC:
			if !b.tickProgress() {
				b.skipDraw = true
			}
		case <-usageTicker.C:
			// Refresh the memory readout only when the displayed value changed.
			before := b.usage.footer()
			b.sampleUsage()
			if b.consoleOpen || b.usage.footer() == before {
				b.skipDraw = true
			}
		}
	}
}

// handleCommand applies one front-end command. It reports whether the session
// should exit.
func (b *Browser) handleCommand(cmd protocol.Command) (bool, error) {
	switch v := cmd.Body.(type) {
	case protocol.Init:
		if v.CellW > 0 {
			b.cellW = v.CellW
		}
		if v.CellH > 0 {
			b.cellH = v.CellH
		}
		if v.Zoom > 0 {
			b.zoom = v.Zoom
			setLayoutZoom(v.Zoom)
		}
		b.color = v.Caps&protocol.CapColor != 0
		b.resizeWithContent(v.Width, v.Height, v.ContentTop, v.ContentHeight)
	case protocol.Navigate:
		b.Navigate(v.URL, v.Push)
	case protocol.History:
		b.setHistoryView(v)
	case protocol.Restore:
		b.restoreEntry(v.EntryID, v.URL, v.Scroll)
	case protocol.Resize:
		b.resizeWithContent(v.Width, v.Height, v.ContentTop, v.ContentHeight)
		if v.CellW > 0 {
			b.cellW = v.CellW
		}
		if v.CellH > 0 {
			b.cellH = v.CellH
		}
		b.applyImageCap()
	case protocol.Key:
		// Esc and Ctrl-C also arrive as keys; count them here as well so a
		// reader cannot lose the abort when both messages are ready and the
		// select picks the command first.
		if v.Token == "esc" || v.Token == "ctrl-c" {
			b.abortGen.Add(1)
		}
		return b.HandleKey(v.Token), nil
	case protocol.Mouse:
		return b.HandleMouse(b.protocolMouse(v)), nil
	case protocol.Scroll:
		switch {
		case v.Top:
			b.scrollBy(-b.scroll)
		case v.Bottom:
			b.scrollBy(b.maxScroll() - b.scroll)
		case v.HasTo:
			b.scrollBy(v.To - b.scroll)
		default:
			b.scrollBy(v.Delta)
		}
	case protocol.App:
		return b.handleAppAction(v)
	case protocol.Abort:
		b.abortGen.Add(1)
	case protocol.Find:
		b.applyFindCommand(v)
	case protocol.SelectResult:
		b.applySelectResult(v)
	case protocol.ModalClose:
		b.applyModalClose(v)
	case protocol.HintResult:
		b.applyHintResult(v)
	case protocol.ConsoleAction:
		b.applyConsoleAction(v)
	case protocol.ClientError:
		b.logEngineError(v.Message)
	case protocol.Shutdown:
		return true, nil
	}
	return false, nil
}

func (b *Browser) applyHintResult(v protocol.HintResult) {
	if !b.hintsFrontend || !b.linkHints {
		return
	}
	if v.Cancel {
		b.clearLinkHints()
		b.status = exitHint
		return
	}
	target := v.Target
	t := b.hintTargetAt(target)
	top, bottom := b.viewport()
	r, _ := b.hintTargetRect(target, top, bottom)
	b.clearLinkHints()
	if t.node != nil {
		b.activateHintControl(t.node, r)
		return
	}
	if t.link >= 0 {
		b.focus = t.link
		b.followFocused()
	}
}

func (b *Browser) applyModalClose(v protocol.ModalClose) {
	switch v.Kind {
	case protocol.ModalDownload:
		if !b.downloadFrontend {
			return
		}
		b.download = nil
		b.downloadFrontend = false
		if v.Copied {
			b.status = fmt.Sprintf("Copied %d chars", v.Chars)
		}
	case protocol.ModalNotice:
		if !b.noticeFrontend {
			return
		}
		b.notice = nil
		b.noticeFrontend = false
	}
}

func (b *Browser) applySelectResult(v protocol.SelectResult) {
	if !b.selectFrontend || v.ID != b.selectID {
		return
	}
	b.selectFrontend = false
	b.selectID = 0
	m := b.selectMenu
	if v.Cancel || m == nil || v.Index < 0 || v.Index >= len(m.options) {
		b.selectMenu = nil
		b.selectHits = nil
		b.status = ""
		return
	}
	b.commitSelectOption(m.options[v.Index])
}

// handleAppAction applies an application-level action from the front end.
func (b *Browser) handleAppAction(a protocol.App) (bool, error) {
	switch a.Action {
	case protocol.AppReload:
		b.reload()
	case protocol.AppStop:
		b.stopLoading()
	case protocol.AppOpenURL:
		b.clearInputFocus()
		b.openURLPrompt()
	case protocol.AppOpenVimPrompt:
		b.clearInputFocus()
		b.openVimPrompt()
	case protocol.AppOpenZoom:
		b.openZoomMenu()
	case protocol.AppOpenViewMenu:
		b.openViewMenu()
	case protocol.AppToggleConsole:
		b.toggleConsole()
	case protocol.AppToggleJS:
		b.toggleJavaScript()
	case protocol.AppStatusDetails:
		b.showStatusDetails()
	case protocol.AppDownload:
		b.downloadCurrentPDF()
	case protocol.AppHome:
		target := b.home
		if target == "" {
			target = "about:home"
		}
		b.Navigate(target, true)
	case protocol.AppToggleDark:
		b.toggleDark()
	case protocol.AppToggleReader:
		b.toggleReader()
	case protocol.AppToggleImages:
		b.cycleGraphics()
	case protocol.AppToggleFixed:
		b.hideFixed = !b.hideFixed
		b.rebuild()
		if b.hideFixed {
			b.status = "Fixed elements hidden"
		} else {
			b.status = "Fixed elements shown"
		}
	case protocol.AppToggleSticky:
		b.noSticky = !b.noSticky
		b.rebuild()
		if b.noSticky {
			b.status = "Sticky elements disabled"
		} else {
			b.status = "Sticky elements enabled"
		}
	case protocol.AppToggleBackgrounds:
		b.hideBackgrounds = !b.hideBackgrounds
		b.rebuild()
		if b.hideBackgrounds {
			b.status = "Background images hidden"
		} else {
			b.status = "Background images shown"
		}
	case protocol.AppZoomSet:
		if a.Arg >= 25 && a.Arg <= 200 {
			b.applyZoom(float64(a.Arg) / 100)
		}
	case protocol.AppZoomIn:
		b.zoomBy(1)
	case protocol.AppZoomOut:
		b.zoomBy(-1)
	case protocol.AppZoomReset:
		b.resetZoom()
	}
	return false, nil
}

// protocolMouse translates a content-rectangle pointer event into the terminal
// cell coordinates the engine hit test uses.
func (b *Browser) protocolMouse(v protocol.Mouse) MouseEvent {
	return MouseEvent{
		Button: v.Button,
		X:      v.X + 1,
		Y:      b.contentRect().Top + v.Y + 1,
		Press:  v.Press,
		Motion: v.Motion,
		Wheel:  v.Wheel,
		Shift:  v.Shift,
	}
}

// AbortSession counts an out-of-band abort from the front end. The JS runtime
// reads this atomic from inside a script, so a busy page stops even though the
// session loop has not run yet.
func (b *Browser) AbortSession() { b.abortGen.Add(1) }

// emitPendingFrame sends the frame the last draw stashed. The transport copies
// it before returning, so the frame buffer can be reused immediately.
func (b *Browser) emitPendingFrame() {
	if b.sessionOut == nil {
		b.chromeReady = false
		b.frameReady = false
		return
	}
	if b.chromeReady {
		b.chromeReady = false
		_ = b.sessionOut.Send(protocol.Event{Body: b.chromeBuf})
	}
	if !b.frameReady {
		return
	}
	b.frameReady = false
	_ = b.sessionOut.Send(protocol.Event{Body: b.frameBuf})
}
