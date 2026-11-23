package engine

import (
	"context"
	"runtime/debug"
	"time"

	"browser/internal/protocol"
)

const progressTickInterval = 80 * time.Millisecond

// RuntimeHooks are the terminal/shell effects around the session event loop.
type RuntimeHooks interface {
	CommandHooks
	SessionHooks
	BeginSession(ctx context.Context, abort <-chan struct{}, in <-chan protocol.Command, out protocol.EventSink)
	EndSession()
	Draw()
	State() protocol.State
	EmitPendingFrame()
	ApplyImageBatch(ImageBatch) bool
	SessionQuit() bool
	UsageFooter() string
	SampleUsage() string
}

// Serve runs the interactive session loop on its own goroutine. Commands arrive
// from the trusted front end through the transport; page loads, JavaScript
// timers, deferred images and imports are serviced here, and the resulting
// shell state leaves as events.
func (s *Session) Serve(ctx context.Context, abort <-chan struct{}, in <-chan protocol.Command, out protocol.EventSink, h RuntimeHooks) error {
	var jsTimer, progressTimer *time.Timer
	defer func() {
		if jsTimer != nil {
			jsTimer.Stop()
		}
		if progressTimer != nil {
			progressTimer.Stop()
		}
		s.CancelLoadNow()
		s.StopJS()
	}()
	usageTicker := time.NewTicker(2 * time.Second)
	defer usageTicker.Stop()

	h.BeginSession(ctx, abort, in, out)
	defer h.EndSession()
	skipDraw := false
	for {
		if s.TakeScavengePending() {
			debug.FreeOSMemory()
		}
		if skipDraw {
			skipDraw = false
		} else {
			h.Draw()
			if st := h.State(); st != s.LastState {
				s.LastState = st
				if err := out.Send(protocol.Event{Body: st}); err != nil {
					return err
				}
			}
			h.EmitPendingFrame()
		}
		var timerC <-chan time.Time
		if d, ok := s.NextJSTimerDelay(h); ok {
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
		if s.ProgressActive() {
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
		case cmd, ok := <-in:
			if !ok {
				return nil
			}
			quit, err := s.HandleCommand(cmd, h)
			if err != nil {
				return err
			}
			if quit || h.SessionQuit() {
				return out.Send(protocol.Event{Body: protocol.Quit{}})
			}
		case res := <-s.LoadCh:
			s.ApplyLoadResult(res, h)
		case batch := <-s.ImageCh:
			if !h.ApplyImageBatch(batch) {
				skipDraw = true
			}
		case task := <-s.ImportCh:
			s.FinishJSImport(task)
			if s.JS != nil && s.JS.ConsumeDirty() {
				h.Rebuild()
			}
			h.ApplyPendingJS(false, 0, false)
		case <-timerC:
			if !s.RunJSTimers(h) {
				skipDraw = true
			}
		case <-progressC:
			if !s.TickProgress() {
				skipDraw = true
			}
		case <-usageTicker.C:
			before := h.UsageFooter()
			after := h.SampleUsage()
			if h.ConsoleOpen() || after == before {
				skipDraw = true
			}
		}
	}
}
