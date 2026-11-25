package engine

import (
	"github.com/grafana/sobek"
)

func (j *jsRuntime) nodeArray(nodes []*Node) sobek.Value {
	vals := make([]interface{}, 0, len(nodes))
	for _, n := range nodes {
		vals = append(vals, j.wrap(n))
	}
	return j.vm.NewArray(vals...)
}

func (j *jsRuntime) setInnerHTML(n *Node, html string) {
	// Script and style content is raw text: assigning a string containing `<`
	// must not start a tag. Bundlers usually set textContent, but consent
	// stubs and older loaders use innerHTML on a script element, where parsing
	// the code as markup silently deletes everything from the first `<` to the
	// next `>`.
	if n != nil && (n.Tag == "script" || n.Tag == "style") {
		for _, c := range n.Children {
			c.Parent = nil
		}
		n.Children = nil
		if html != "" {
			n.Children = append(n.Children, &Node{Type: TextNode, Text: html, Parent: n})
		}
		j.markDirty()
		return
	}
	for _, c := range n.Children {
		c.Parent = nil
	}
	n.Children = nil
	wrapper := ParseHTML("<html><body>" + html + "</body></html>")
	if body := wrapper.FindFirst("body"); body != nil {
		for _, c := range body.Children {
			c.Parent = n
			n.Children = append(n.Children, c)
		}
	}
	j.markDirty()
}
