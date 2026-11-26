package engine

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"browser/internal/protocol"
)

// transportState is the Serve-owned command stream and frame emission state.
type transportState struct {
	// sessionCtx, sessionIn and sessionAbort are set while Serve owns the
	// event loop. A modal dialog then reads keys from the command stream
	// instead of a direct terminal channel.
	sessionCtx   context.Context
	sessionIn    <-chan protocol.Command
	sessionAbort <-chan struct{}
	sessionQuit  bool
	// emitFrames switches presentation to the front end: draw stashes the
	// screen frame and sends it over the transport instead of writing stdout.
	emitFrames bool
	frameReady bool
	sessionOut protocol.EventSink
	// frontendChrome lets the normal path emit a content-only frame and a
	// State snapshot; the front end draws the header/footer from it.
	frontendChrome bool
	// abortGen counts user escape requests (Esc or Ctrl-C). A guarded script
	// compares it with the value captured at entry, so a press interrupts the
	// call in flight without leaking into later timer callbacks.
	abortGen atomic.Uint64
	// jsRun is the top-level runtime currently executing. AbortSession
	// interrupts it directly, so a busy script stops without waiting for the
	// watchdog tick.
	jsRun atomic.Pointer[jsRuntime]
	// jsIOMu guards the parent context for synchronous script network calls.
	// An abort cancels the calls in flight; jsIOAborted makes the next
	// top-level script call start with a fresh context instead of inheriting
	// the cancelled one.
	jsIOMu      sync.Mutex
	jsIOCtx     context.Context
	jsIOCancel  context.CancelFunc
	jsIOAborted bool
}

// Serve runs the interactive session on its own goroutine. Commands arrive from
// the trusted front end through the transport; page loads, JavaScript timers,
// deferred images and imports are serviced here, and the resulting shell state
// leaves as events.
//
// The front end owns text/native presentation and normal header/footer chrome;
// this loop emits frames and state. The engine still composes chrome for
// overlays, the console and the find bar and handles shell dialogs.
func (b *Session) Serve(ctx context.Context, abort <-chan struct{}, in <-chan protocol.Command, out protocol.EventSink) error {
	return b.ServeWithHooks(ctx, abort, in, out, b)
}

// handleCommand applies one front-end command. It reports whether the session
// should exit.
func (b *Session) handleCommand(cmd protocol.Command) (bool, error) {
	return b.HandleCommand(cmd, b)
}

func (b *Session) applyHintResult(v protocol.HintResult) {
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

func (b *Session) applyModalClose(v protocol.ModalClose) {
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

func (b *Session) applySelectResult(v protocol.SelectResult) {
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
func (b *Session) handleAppAction(a protocol.App) (bool, error) {
	switch a.Action {
	case protocol.AppReload:
		b.reload()
	case protocol.AppStop:
		b.stopLoading()
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
	case protocol.AppFrontendOverlay:
		if open := a.Arg != 0; b.frontendOverlay != open {
			b.frontendOverlay = open
		}
	}
	return false, nil
}

// protocolMouse translates a content-rectangle pointer event into the terminal
// cell coordinates the engine hit test uses.
func (b *Session) protocolMouse(v protocol.Mouse) MouseEvent {
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

// AbortSession stops the script running in this tab. It cancels synchronous
// script network calls in flight and interrupts the runtime directly, so the
// session loop can process the close or stop immediately instead of waiting
// for a busy page to return on its own.
func (b *Session) AbortSession() {
	gen := b.abortGen.Add(1)
	b.abortJSIO()
	// A call that started after the abort already sees the new generation and
	// must not be killed by a stale press. The snapshot is stored before
	// inFlight is incremented, so a racing abort either sees no call or the
	// matching generation.
	if j := b.jsRun.Load(); j != nil && j.inFlight.Load() > 0 && j.callAbortGen.Load() < gen {
		j.vm.Interrupt("JavaScript stopped by user")
	}
}

// beginJSCall gives the next top-level script call a fresh network context
// after an abort. Requests started inside the aborted call keep the cancelled
// context, so a loop cannot block again before the runtime notices the abort.
func (b *Session) beginJSCall() {
	b.jsIOMu.Lock()
	defer b.jsIOMu.Unlock()
	if !b.jsIOAborted {
		return
	}
	if b.jsIOCancel != nil {
		b.jsIOCancel()
	}
	b.jsIOCtx, b.jsIOCancel = context.WithCancel(context.Background())
	b.jsIOAborted = false
}

// jsRequestContext returns the parent for synchronous script requests.
func (b *Session) jsRequestContext() context.Context {
	b.jsIOMu.Lock()
	defer b.jsIOMu.Unlock()
	if b.jsIOCtx == nil {
		b.jsIOCtx, b.jsIOCancel = context.WithCancel(context.Background())
	}
	return b.jsIOCtx
}

// abortJSIO cancels script network calls in flight without poisoning a later
// top-level call, which beginJSCall gives a fresh context.
func (b *Session) abortJSIO() {
	b.jsIOMu.Lock()
	defer b.jsIOMu.Unlock()
	if b.jsIOCancel != nil {
		b.jsIOCancel()
	}
	b.jsIOAborted = true
}

// emitPendingFrame sends the frame the last draw stashed. The transport copies
// it before returning, so the frame buffer can be reused immediately.
func (b *Session) emitPendingFrame() {
	if b.sessionOut == nil {
		b.frameReady = false
		return
	}
	if !b.frameReady {
		return
	}
	b.frameReady = false
	_ = b.sessionOut.Send(protocol.Event{Body: b.frameBuf})
}
