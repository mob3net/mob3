package engine

import (
	"strings"
)

// ParsedSelectors caches querySelector selector parsing per page runtime.
func (j *jsRuntime) ParsedSelectors(sel string) []*Selector {
	if s, ok := j.selectors[sel]; ok {
		return s
	}
	s := parseSelectors(sel)
	j.selectors[sel] = s
	return s
}

// parseSelectors parses a comma-separated selector list, ignoring invalid
// entries.
func parseSelectors(sel string) []*Selector {
	var out []*Selector
	for _, part := range SplitTopLevel(sel, ',') {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if s, err := ParseSelector(part); err == nil {
			out = append(out, s)
		}
	}
	return out
}

func matchSelectors(n *Node, sels []*Selector, budget *matchBudget) bool {
	for _, s := range sels {
		if s.matchWith(n, budget) {
			return true
		}
	}
	return false
}

// selectorBudget bounds one DOM query and makes an Esc/Ctrl-C abort or the
// page's script deadline visible inside native selector matching.
func (j *jsRuntime) selectorBudget() *matchBudget {
	abortGen := uint64(0)
	if j != nil && j.host != nil {
		abortGen = j.host.AbortGen()
	}
	return &matchBudget{max: queryMatchSteps, abort: func() bool {
		if j == nil {
			return true
		}
		if j.host != nil && j.host.AbortGen() != abortGen {
			return true
		}
		return j.loadDeadline.Load() != 0 && j.deadlineExpired()
	}}
}

func (j *jsRuntime) querySelector(root *Node, sel string, includeRoot bool) *Node {
	sels := j.ParsedSelectors(sel)
	if len(sels) == 0 || root == nil {
		return nil
	}
	budget := j.selectorBudget()
	var found *Node
	var walk func(*Node)
	walk = func(x *Node) {
		if found != nil {
			return
		}
		for _, c := range x.Children {
			if c.Type != ElementNode {
				continue
			}
			if matchSelectors(c, sels, budget) {
				found = c
				return
			}
			walk(c)
			if found != nil {
				return
			}
		}
	}
	if includeRoot && root.Type == ElementNode && matchSelectors(root, sels, budget) {
		return root
	}
	walk(root)
	return found
}

func (j *jsRuntime) querySelectorAll(root *Node, sel string, includeRoot bool) []*Node {
	sels := j.ParsedSelectors(sel)
	if len(sels) == 0 || root == nil {
		return nil
	}
	budget := j.selectorBudget()
	var out []*Node
	if includeRoot && root.Type == ElementNode && matchSelectors(root, sels, budget) {
		out = append(out, root)
	}
	root.FindAll(func(x *Node) bool {
		if matchSelectors(x, sels, budget) {
			out = append(out, x)
		}
		return false
	})
	return out
}
