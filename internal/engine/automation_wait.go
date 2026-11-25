package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// waitPendingLocked applies any navigation queued by input/change/submit
// handlers and waits for the resulting load.
func (a *Automation) waitPendingLocked(ctx context.Context) error {
	if a.b.consumePendingJS() || a.b.Loading {
		return a.waitLoad(ctx)
	}
	return nil
}

// waitPollInterval bounds how often wait and wait_for re-check the page and
// pump due timers.
const waitPollInterval = 20 * time.Millisecond

// Wait advances the page for ms milliseconds, running due JavaScript timers,
// applying the rebuilds they cause, and waiting for navigations they start.
func (a *Automation) Wait(ctx context.Context, ms int) (int, error) {
	if a == nil || a.b == nil {
		return 0, errors.New("automation is closed")
	}
	if ms < 0 {
		ms = 0
	}
	if ms > 10000 {
		ms = 10000
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	deadline := time.Now().Add(time.Duration(ms) * time.Millisecond)
	for {
		if err := a.waitStepLocked(ctx, deadline); err != nil {
			return ms, err
		}
		if !time.Now().Before(deadline) {
			break
		}
	}
	a.flushDirtyLocked()
	return ms, nil
}

// WaitFor pumps timers until a selector matches an element and/or text appears
// in the rendered page, or the timeout expires.
func (a *Automation) WaitFor(ctx context.Context, selector, text string, timeoutMS int) (string, error) {
	if a == nil || a.b == nil {
		return "", errors.New("automation is closed")
	}
	if selector == "" && strings.TrimSpace(text) == "" {
		return "", errors.New("wait_for needs a selector or text")
	}
	var sel *Selector
	if selector != "" {
		s, err := ParseSelector(selector)
		if err != nil {
			return "", fmt.Errorf("bad selector: %w", err)
		}
		sel = s
	}
	if timeoutMS <= 0 {
		timeoutMS = 5000
	}
	if timeoutMS > 30000 {
		timeoutMS = 30000
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	start := time.Now()
	deadline := start.Add(time.Duration(timeoutMS) * time.Millisecond)
	needle := strings.ToLower(collapseWhitespace(text))
	for {
		if a.conditionLocked(sel, needle) {
			a.flushDirtyLocked()
			return fmt.Sprintf("found %s after %dms", waitTarget(selector, text), time.Since(start).Milliseconds()), nil
		}
		if !time.Now().Before(deadline) {
			return "", fmt.Errorf("timed out after %dms waiting for %s", timeoutMS, waitTarget(selector, text))
		}
		if err := a.waitStepLocked(ctx, deadline); err != nil {
			return "", err
		}
	}
}

func waitTarget(selector, text string) string {
	switch {
	case selector != "" && strings.TrimSpace(text) != "":
		return fmt.Sprintf("selector %q and text %q", selector, text)
	case selector != "":
		return fmt.Sprintf("selector %q", selector)
	default:
		return fmt.Sprintf("text %q", text)
	}
}

func (a *Automation) conditionLocked(sel *Selector, needle string) bool {
	b := a.b
	if b.Page == nil || b.Page.Root == nil {
		return false
	}
	if sel != nil {
		found := false
		b.Page.Root.FindAll(func(n *Node) bool {
			if !found && n.Type == ElementNode && sel.Match(n) {
				found = true
			}
			return false
		})
		if !found {
			return false
		}
	}
	if needle != "" {
		return strings.Contains(strings.ToLower(a.renderedTextLocked()), needle)
	}
	return true
}

// renderedTextLocked returns the laid-out page text, falling back to DOM text
// when no layout exists yet. Script and style contents are never included.
func (a *Automation) renderedTextLocked() string {
	b := a.b
	var sb strings.Builder
	if b.Layout != nil {
		for i := range b.Layout.Frags {
			f := &b.Layout.Frags[i]
			if f.Kind == FragText && f.Text != "" {
				sb.WriteString(f.Text)
				sb.WriteByte(' ')
			}
		}
	}
	if sb.Len() == 0 && b.Page != nil && b.Page.Root != nil {
		writeDOMText(&sb, b.Page.Root)
	}
	return collapseWhitespace(sb.String())
}

func writeDOMText(sb *strings.Builder, n *Node) {
	for _, c := range n.Children {
		switch c.Type {
		case TextNode:
			sb.WriteString(c.Text)
			sb.WriteByte(' ')
		case ElementNode:
			switch c.Tag {
			case "script", "style", "noscript", "template":
				continue
			}
			writeDOMText(sb, c)
		}
	}
}

// waitStepLocked applies a pending load, runs due timers, forces any delayed
// rebuild, and sleeps briefly.
func (a *Automation) waitStepLocked(ctx context.Context, deadline time.Time) error {
	b := a.b
	if b.Loading {
		if err := a.waitLoad(ctx); err != nil {
			return err
		}
	}
	if b.JS != nil {
		b.runJSTimers()
		if b.consumePendingJS() || b.Loading {
			if err := a.waitLoad(ctx); err != nil {
				return err
			}
		}
	}
	a.flushDirtyLocked()
	d := waitPollInterval
	if nd, ok := b.nextJSTimerDelay(); ok && nd < d {
		d = nd
	}
	if rem := time.Until(deadline); rem < d {
		d = rem
	}
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// flushDirtyLocked rebuilds immediately when a timer changed the DOM but the
// interactive redraw coalescing deferred it.
func (a *Automation) flushDirtyLocked() {
	if a.b.JS != nil && a.b.JS.ConsumeDirty() {
		a.b.rebuild()
	}
}
