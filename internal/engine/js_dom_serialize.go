package engine

import (
	"strconv"
	"strings"
)

func serializeChildren(n *Node) string {
	var b strings.Builder
	for _, c := range n.Children {
		SerializeNode(&b, c)
	}
	return b.String()
}

func SerializeNode(b *strings.Builder, n *Node) {
	if n == nil {
		return
	}
	if n.Type == TextNode {
		b.WriteString(TextEscape(n.Text))
		return
	}
	if n.Type != ElementNode {
		return
	}
	b.WriteString("<")
	b.WriteString(n.Tag)
	for _, a := range n.Attrs {
		b.WriteString(" ")
		b.WriteString(a.Key)
		b.WriteString(`="`)
		b.WriteString(EscapeHTML(a.Value))
		b.WriteString(`"`)
	}
	b.WriteString(">")
	if isVoidTag(n.Tag) {
		return
	}
	for _, c := range n.Children {
		SerializeNode(b, c)
	}
	b.WriteString("</")
	b.WriteString(n.Tag)
	b.WriteString(">")
}

func TextEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func isVoidTag(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}

func isEditableNode(n *Node) bool {
	for x := n; x != nil; x = x.Parent {
		v := strings.ToLower(AttrValue(x, "contenteditable"))
		switch v {
		case "false":
			return false
		case "", "true", "plaintext-only":
			if x.HasAttr("contenteditable") {
				return true
			}
		}
	}
	return false
}

func nodeTabIndex(n *Node) int {
	if n == nil {
		return -1
	}
	if n.HasAttr("tabindex") {
		if v, err := strconv.Atoi(strings.TrimSpace(AttrValue(n, "tabindex"))); err == nil {
			return v
		}
	}
	if isEditableNode(n) {
		return 0
	}
	switch strings.ToLower(n.Tag) {
	case "a", "area":
		if n.HasAttr("href") {
			return 0
		}
	case "button", "select", "textarea", "iframe", "summary", "object", "embed":
		return 0
	case "input":
		if !strings.EqualFold(AttrValue(n, "type"), "hidden") {
			return 0
		}
	case "audio", "video":
		if n.HasAttr("controls") {
			return 0
		}
	}
	return -1
}

func replaceNodeWithText(n *Node, text string) {
	if n == nil {
		return
	}
	if n.Parent == nil {
		n.Children = nil
		if text != "" {
			n.Children = []*Node{{Type: TextNode, Text: text, Parent: n}}
		}
		return
	}
	parent := n.Parent
	if text == "" {
		detach(n)
		return
	}
	textNode := &Node{Type: TextNode, Text: text, Parent: parent}
	insertNode(parent, textNode, n)
	detach(n)
}

func nodesEqual(a, b *Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Type != b.Type || a.Tag != b.Tag || a.Text != b.Text || len(a.Attrs) != len(b.Attrs) || len(a.Children) != len(b.Children) {
		return false
	}
	for i := range a.Attrs {
		found := false
		for j := range b.Attrs {
			if a.Attrs[i] == b.Attrs[j] {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for i := range a.Children {
		if !nodesEqual(a.Children[i], b.Children[i]) {
			return false
		}
	}
	return true
}

// zIndexCSS reports a computed z-index for the DOM measurement APIs.
func zIndexCSS(st *Style) string {
	if st == nil || st.ZIndex == 0 {
		return "auto"
	}
	return strconv.Itoa(st.ZIndex)
}
