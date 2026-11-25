package engine

import (
	"fmt"
	"strconv"
	"strings"
)

// revealNode scrolls the page until one of n's text fragments is in view.
func revealNode(b *Session, n *Node) {
	if b.Layout == nil {
		return
	}
	vh := b.viewportHeight()
	var first *Frag
	for i := range b.Layout.Frags {
		f := &b.Layout.Frags[i]
		if f.Kind != FragText || f.Node == nil {
			continue
		}
		if f.Node != n && !isDescendant(f.Node, n) {
			continue
		}
		if first == nil {
			first = f
		}
		if f.Y >= b.Scroll && f.Y < b.Scroll+vh {
			return
		}
	}
	if first == nil {
		return
	}
	if first.Y < b.Scroll {
		b.Scroll = first.Y
	} else {
		b.Scroll = first.Y - vh + 1
	}
	b.clampScroll()
}

// selectorForNode returns a unique CSS selector for n: an id, a name qualified
// by its form, or an nth-child path as a last resort.
func selectorForNode(root, n *Node) string {
	if n == nil {
		return ""
	}
	if id := n.Attr("id"); id != "" && safeCSSIdent(id) {
		if sel := "#" + id; selectorMatchesNode(root, n, sel) {
			return sel
		}
	}
	if name := n.Attr("name"); name != "" && safeAttrText(name) {
		base := n.Tag + "[name=" + strconv.Quote(name) + "]"
		if form := NearestForm(n); form != nil {
			if id := form.Attr("id"); id != "" && safeCSSIdent(id) {
				if sel := "#" + id + " " + base; selectorMatchesNode(root, n, sel) {
					return sel
				}
			}
		}
		if selectorMatchesNode(root, n, base) {
			return base
		}
	}
	if n.Tag == "a" {
		if href := n.Attr("href"); href != "" && safeAttrText(href) {
			if sel := "a[href=" + strconv.Quote(href) + "]"; selectorMatchesNode(root, n, sel) {
				return sel
			}
		}
	}
	return cssNodePath(n)
}

func selectorMatchesNode(root, want *Node, sel string) bool {
	s, err := ParseSelector(sel)
	if err != nil {
		return false
	}
	match, count := false, 0
	root.FindAll(func(x *Node) bool {
		if x.Type == ElementNode && s.Match(x) {
			count++
			if x == want {
				match = true
			}
		}
		return false
	})
	return match && count == 1
}

func cssNodePath(n *Node) string {
	var parts []string
	for x := n; x != nil && x.Type == ElementNode; x = x.Parent {
		idx, _ := elementIndex(x)
		if idx == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:nth-child(%d)", x.Tag, idx))
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, " > ")
}

func safeCSSIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

// safeAttrText reports an attribute value the selector parser can read back
// from a double-quoted selector without escape processing.
func safeAttrText(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f || c == '"' || c == '\\' {
			return false
		}
	}
	return true
}

func quotedList(items []string) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, s := range items {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(strconv.Quote(s))
	}
	sb.WriteByte(']')
	return sb.String()
}
