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

func matchSelectors(n *Node, sels []*Selector) bool {
	for _, s := range sels {
		if s.Match(n) {
			return true
		}
	}
	return false
}

func (j *jsRuntime) querySelector(root *Node, sel string, includeRoot bool) *Node {
	sels := j.ParsedSelectors(sel)
	if len(sels) == 0 || root == nil {
		return nil
	}
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
			if matchSelectors(c, sels) {
				found = c
				return
			}
			walk(c)
			if found != nil {
				return
			}
		}
	}
	if includeRoot && root.Type == ElementNode && matchSelectors(root, sels) {
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
	var out []*Node
	if includeRoot && root.Type == ElementNode && matchSelectors(root, sels) {
		out = append(out, root)
	}
	root.FindAll(func(x *Node) bool {
		if matchSelectors(x, sels) {
			out = append(out, x)
		}
		return false
	})
	return out
}
