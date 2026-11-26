package engine

import (
	"strings"
)

// Element namespace URIs. Parsed HTML does not carry namespaces, so they are
// derived from the nearest svg/math ancestor.
const (
	xhtmlNamespace  = "http://www.w3.org/1999/xhtml"
	svgNamespace    = "http://www.w3.org/2000/svg"
	mathMLNamespace = "http://www.w3.org/1998/Math/MathML"
)

func elementNamespaceURI(n *Node) string {
	if n == nil {
		return ""
	}
	if n.Namespace != "" {
		return n.Namespace
	}
	for p := n; p != nil; p = p.Parent {
		switch strings.ToLower(p.Tag) {
		case "svg":
			return svgNamespace
		case "math":
			return mathMLNamespace
		}
	}
	return xhtmlNamespace
}

// templateContent returns the inert DocumentFragment that holds a
// <template>'s children. Accessing it moves the parsed children out of the
// template, matching the browser rule that template content is not rendered.
func (j *jsRuntime) templateContent(n *Node) *Node {
	if n == nil || !strings.EqualFold(n.Tag, "template") {
		return nil
	}
	if frag, ok := j.templates[n]; ok && frag != nil {
		return frag
	}
	frag := &Node{Type: ElementNode, Tag: "#fragment"}
	frag.Children = append(frag.Children, n.Children...)
	for _, c := range frag.Children {
		if c.Parent == n {
			c.Parent = frag
		}
	}
	n.Children = nil
	if j.templates == nil {
		j.templates = make(map[*Node]*Node)
	}
	j.templates[n] = frag
	return frag
}

// contentTarget returns the node whose children hold an element's markup:
// template content lives in its fragment instead of the element itself.
func (j *jsRuntime) contentTarget(n *Node) *Node {
	if frag, ok := j.templates[n]; ok && frag != nil {
		return frag
	}
	return n
}

// cloneNodeValue clones a node and carries a template's content fragment over
// to the clone.
func (j *jsRuntime) cloneNodeValue(n *Node, deep bool) *Node {
	count := 1
	if deep {
		count = countNodes(n)
	}
	if !j.reserveDOMNode(count) || !j.chargeDOMText(nodeTextBytes(n)) {
		return nil
	}
	c := cloneNode(n, deep)
	if frag, ok := j.templates[n]; ok && frag != nil {
		cf := &Node{Type: ElementNode, Tag: "#fragment"}
		if deep {
			cf = cloneNode(frag, true)
		}
		if j.templates == nil {
			j.templates = make(map[*Node]*Node)
		}
		j.templates[c] = cf
	}
	return c
}
