package engine

import "unicode/utf8"

// Page-controlled DOM copies live on the Go heap, outside the sobek heap
// sampler. These bounds keep a page from parking unbounded strings or nodes in
// the document structures.
const (
	// maxDOMValueBytes caps one text or attribute value copied from script.
	maxDOMValueBytes = 1 << 20
	// maxDOMTextBytes is the cumulative text and attribute budget per runtime.
	maxDOMTextBytes = 64 << 20
	// maxDOMNodes caps nodes created or exposed through script.
	maxDOMNodes = 200_000
)

// capDOMValue truncates one script string to the per-value bound.
func capDOMValue(s string) string {
	if len(s) <= maxDOMValueBytes {
		return s
	}
	cut := maxDOMValueBytes - len("…")
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// chargeDOMText accounts bytes copied from script and reports whether the
// runtime budget still allows them.
func (j *jsRuntime) chargeDOMText(n int) bool {
	if j == nil || n <= 0 {
		return true
	}
	if j.domTextBytes+int64(n) > maxDOMTextBytes {
		return false
	}
	j.domTextBytes += int64(n)
	return true
}

// domText caps and accounts one script string for a DOM sink.
func (j *jsRuntime) domText(s string) (string, bool) {
	s = capDOMValue(s)
	if !j.chargeDOMText(len(s)) {
		return "", false
	}
	return s, true
}

// reserveDOMNode accounts nodes created or wrapped for script.
func (j *jsRuntime) reserveDOMNode(n int) bool {
	if j == nil || n <= 0 {
		return true
	}
	if j.domNodes+n > maxDOMNodes {
		return false
	}
	j.domNodes += n
	return true
}

// textNode builds a bounded text node, or nil when a budget is exhausted.
func (j *jsRuntime) textNode(s string) *Node {
	s, ok := j.domText(s)
	if !ok || !j.reserveDOMNode(1) {
		return nil
	}
	return &Node{Type: TextNode, Text: s}
}

// setAttr copies one script attribute value into the node.
func (j *jsRuntime) setAttr(n *Node, name, value string) {
	v, ok := j.domText(value)
	if !ok {
		return
	}
	SetAttr(n, name, v)
}

// countNodes returns the number of nodes in n's subtree, without recursion so
// a script-built chain cannot overflow the stack.
func countNodes(n *Node) int {
	if n == nil {
		return 0
	}
	count := 0
	stack := []*Node{n}
	for len(stack) > 0 {
		x := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count++
		stack = append(stack, x.Children...)
	}
	return count
}

// nodeTextBytes returns the text and attribute bytes retained by a subtree.
func nodeTextBytes(n *Node) int {
	if n == nil {
		return 0
	}
	total := 0
	stack := []*Node{n}
	for len(stack) > 0 {
		x := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		total += len(x.Text)
		for _, a := range x.Attrs {
			total += len(a.Key) + len(a.Value)
		}
		stack = append(stack, x.Children...)
	}
	return total
}
