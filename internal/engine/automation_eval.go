package engine

import (
	"context"
	"errors"
)

// Evaluate runs JavaScript in the current page realm and returns the result's
// string form. Objects appear as "[object Object]"; use JSON.stringify to get
// structured data.
func (a *Automation) Evaluate(ctx context.Context, expression string) (string, error) {
	if a == nil || a.b == nil {
		return "", errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.JS == nil {
		return "", errors.New("JavaScript is disabled or no page is loaded")
	}
	v, err := b.JS.RunCode("automation", expression)
	if b.consumePendingJS() {
		if err := a.waitLoad(ctx); err != nil {
			return "", err
		}
	}
	if b.JS.ConsumeDirty() {
		b.rebuild()
	}
	if err != nil {
		return "", err
	}
	if v == nil {
		return "", nil
	}
	return clipText(v.String(), 20000), nil
}
