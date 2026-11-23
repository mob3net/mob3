package engine

import "time"

const jsRedrawInterval = 250 * time.Millisecond

// StartJS creates a fresh runtime and runs the page's scripts in order.
func (s *Session) StartJS(page *Page, h SessionHooks) {
	s.StopJS()
	if s.NoJS || page == nil {
		return
	}
	j := NewJSRuntime(h.NewJSHost(), page)
	if s.JSPageBudget > 0 {
		j.SetPageBudget(s.JSPageBudget)
	}
	s.JS = j
	j.StartPage(page)
	h.SetModuleErr(j.ModuleErr())
}

// StopJS tears down the current runtime.
func (s *Session) StopJS() {
	if s.JS != nil {
		s.JS.Stop()
		s.JS = nil
	}
}

// NextJSTimerDelay returns the next engine timer or hover-rebuild deadline.
func (s *Session) NextJSTimerDelay(h SessionHooks) (time.Duration, bool) {
	if h.ConsoleOpen() {
		return 0, false
	}
	var next time.Time
	if pending, at := h.HoverRebuild(); pending {
		next = at
	}
	if s.JS != nil {
		if d, ok := s.JS.NextTimerDelay(); ok {
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

// RunJSTimers runs due timers and rebuilds when the DOM changed.
func (s *Session) RunJSTimers(h SessionHooks) bool {
	now := time.Now()
	if h.ConsoleOpen() {
		return false
	}
	if pending, at := h.HoverRebuild(); pending && !now.Before(at) {
		h.ClearHoverRebuild()
		h.Rebuild()
		return true
	}
	if s.JS == nil {
		return false
	}
	s.JS.Tick(now)
	if h.ApplyPendingJS(false, 0, false) {
		return false
	}
	if !s.JS.Dirty() {
		return false
	}
	wait := jsRedrawInterval
	if 2*s.LastBuild > wait {
		wait = 2 * s.LastBuild
	}
	if !s.LastRebuild.IsZero() && now.Sub(s.LastRebuild) < wait {
		return false
	}
	s.JS.ConsumeDirty()
	h.Rebuild()
	return true
}

// FinishJSImport completes a background dynamic import.
func (s *Session) FinishJSImport(task ImportTask) {
	if s.JS != nil {
		s.JS.FinishImport(task)
	}
}
