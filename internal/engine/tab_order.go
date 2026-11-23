package engine

import "strings"

// TabTarget is one stop in the standard Tab order: a link or a form control.
type TabTarget struct {
	Link int
	Node *Node
}

type radioKey struct {
	form *Node
	name string
}

// BuildTabTargets returns links and form controls in document order. Radio
// groups take one stop at their checked button, or their first button.
func BuildTabTargets(page *Page, layout *Layout) []TabTarget {
	if page == nil || page.Root == nil || layout == nil {
		return nil
	}
	linkByNode := make(map[*Node]int, len(layout.Links))
	for i, li := range layout.Links {
		if li.Node == nil {
			continue
		}
		if _, ok := linkByNode[li.Node]; !ok {
			linkByNode[li.Node] = i
		}
	}
	var out []TabTarget
	seenRadio := make(map[radioKey]bool)
	page.Root.FindAll(func(n *Node) bool {
		if link, ok := linkByNode[n]; ok {
			out = append(out, TabTarget{Link: link, Node: n})
			return false
		}
		if !IsTabControl(n) {
			return false
		}
		if strings.EqualFold(strings.TrimSpace(n.Attr("type")), "radio") && n.Tag == "input" {
			name := n.Attr("name")
			if name != "" {
				key := radioKey{form: NearestForm(n), name: name}
				if seenRadio[key] {
					return false
				}
				seenRadio[key] = true
			}
		}
		out = append(out, TabTarget{Link: -1, Node: n})
		return false
	})
	for i := range out {
		n := out[i].Node
		if n == nil || n.Tag != "input" || !strings.EqualFold(strings.TrimSpace(n.Attr("type")), "radio") {
			continue
		}
		for _, r := range RadioGroupNodes(n) {
			if r.HasAttr("checked") {
				out[i].Node = r
				break
			}
		}
	}
	return out
}
