package engine

import "time"

const jsRedrawInterval = 250 * time.Millisecond

// StartJSWithHooks creates a fresh runtime for the tab's top-level frame and
// runs the page's scripts in order.
func (s *Session) StartJSWithHooks(page *Page, h SessionHooks) {
	s.startJSFrame(s.Frame, page, h)
}

// startJSFrame creates the runtime owned by f and runs page's scripts in order.
func (s *Session) startJSFrame(f *Frame, page *Page, h SessionHooks) {
	f.stopJS()
	if f == nil || s.NoJS || page == nil {
		return
	}
	j := NewJSRuntime(h.NewJSHost(f), page)
	if s.JSPageBudget > 0 {
		j.SetPageBudget(s.JSPageBudget)
	}
	f.JS = j
	j.StartPage(page)
	h.SetModuleErr(j.ModuleErr())
}

// StopJS tears down the top-level frame's runtime.
func (s *Session) StopJS() {
	if s.Frame != nil {
		s.Frame.stopJS()
	}
}

// stopJS tears down the frame's runtime.
func (f *Frame) stopJS() {
	if f == nil || f.JS == nil {
		return
	}
	f.JS.Stop()
	f.JS = nil
}

// NextJSTimerDelay returns the next frame timer or hover-rebuild deadline.
func (s *Session) NextJSTimerDelay(h SessionHooks) (time.Duration, bool) {
	if s.Frame == nil || !s.active {
		return 0, false
	}
	return s.Frame.nextJSTimerDelay(h)
}

func (f *Frame) nextJSTimerDelay(h SessionHooks) (time.Duration, bool) {
	if h.ConsoleOpen() {
		return 0, false
	}
	var next time.Time
	if pending, at := h.HoverRebuild(); pending {
		next = at
	}
	if f.JS != nil {
		if d, ok := f.JS.NextTimerDelay(); ok {
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

// RunJSTimers runs the frame's due timers and rebuilds when the DOM changed.
func (s *Session) RunJSTimers(h SessionHooks) bool {
	if s.Frame == nil || !s.active {
		return false
	}
	return s.Frame.runJSTimers(s, h)
}

func (f *Frame) runJSTimers(s *Session, h SessionHooks) bool {
	now := time.Now()
	if h.ConsoleOpen() {
		return false
	}
	if pending, at := h.HoverRebuild(); pending && !now.Before(at) {
		h.ClearHoverRebuild()
		h.Rebuild()
		return true
	}
	if f.JS == nil {
		return false
	}
	f.JS.Tick(now)
	if h.ApplyPendingJS(false, 0, false) {
		return false
	}
	if !f.JS.Dirty() {
		return false
	}
	wait := jsRedrawInterval
	if 2*s.LastBuild > wait {
		wait = 2 * s.LastBuild
	}
	if !s.LastRebuild.IsZero() && now.Sub(s.LastRebuild) < wait {
		return false
	}
	f.JS.ConsumeDirty()
	h.Rebuild()
	return true
}

// FinishJSImport completes a background dynamic import for the top-level frame.
func (s *Session) FinishJSImport(task ImportTask) {
	if s.Frame != nil {
		s.Frame.finishJSImport(task)
	}
}

func (f *Frame) finishJSImport(task ImportTask) {
	if f.JS != nil {
		f.JS.FinishImport(task)
	}
}
