package browser

import (
	"context"
	"fmt"

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
	return b.Session.Serve(ctx, abort, in, out, b)
}

// handleCommand applies one front-end command. It reports whether the session
// should exit.
func (b *Browser) handleCommand(cmd protocol.Command) (bool, error) {
	return b.Session.HandleCommand(cmd, b)
}

func (b *Browser) applyHintResult(v protocol.HintResult) {
	if !b.HintsFrontend || !b.LinkHints {
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
	if t.Node != nil {
		b.activateHintControl(t.Node, r)
		return
	}
	if t.Link >= 0 {
		b.Focus = t.Link
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
		b.HideFixed = !b.HideFixed
		b.rebuild()
		if b.HideFixed {
			b.status = "Fixed elements hidden"
		} else {
			b.status = "Fixed elements shown"
		}
	case protocol.AppToggleSticky:
		b.NoSticky = !b.NoSticky
		b.rebuild()
		if b.NoSticky {
			b.status = "Sticky elements disabled"
		} else {
			b.status = "Sticky elements enabled"
		}
	case protocol.AppToggleBackgrounds:
		b.HideBackgrounds = !b.HideBackgrounds
		b.rebuild()
		if b.HideBackgrounds {
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
