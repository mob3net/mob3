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

// TabIndex returns the next target index for cur and delta, wrapping at the
// ends. A radio whose group stop differs still continues from the group's
// single stop.
func TabIndex(targets []TabTarget, cur *Node, delta int) int {
	if len(targets) == 0 {
		return -1
	}
	idx := -1
	for i, t := range targets {
		if t.Node == cur {
			idx = i
			break
		}
	}
	if idx < 0 && cur != nil && ControlIsRadio(cur) && cur.Attr("name") != "" {
		form := NearestForm(cur)
		name := cur.Attr("name")
		for i, t := range targets {
			n := t.Node
			if n != nil && ControlIsRadio(n) && NearestForm(n) == form && n.Attr("name") == name {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		if delta < 0 {
			return len(targets) - 1
		}
		return 0
	}
	return (idx + delta + len(targets)) % len(targets)
}
