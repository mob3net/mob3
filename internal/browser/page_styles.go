package browser

func (p *Page) seedParsedStyles() {
	if p == nil || p.Root == nil {
		return
	}
	p.parsedStyles = make(map[*Node]string)
	for _, n := range p.Root.FindAll(func(n *Node) bool { return n.Tag == "style" }) {
		p.parsedStyles[n] = n.TextContent()
	}
}

// collectDynamicStyles appends rules from <style> elements added or changed
// since the last scan. Component CSS injected by scripts after load lands here;
// the rules are ordered after every existing author rule so they cascade as
// later stylesheets. It reports whether any rules were added.
func (p *Page) collectDynamicStyles() bool {
	if p == nil || p.Root == nil || p.parsedStyles == nil {
		return false
	}
	order := 0
	for i := range p.Author {
		if p.Author[i].Order >= order {
			order = p.Author[i].Order + 1
		}
	}
	changed := false
	for _, n := range p.Root.FindAll(func(n *Node) bool { return n.Tag == "style" }) {
		text := n.TextContent()
		if prev, ok := p.parsedStyles[n]; ok && prev == text {
			continue
		}
		p.parsedStyles[n] = text
		room := maxAuthorRules - len(p.Author)
		if room <= 0 {
			break
		}
		rules := ParseStylesheet(text)
		if len(rules) > room {
			rules = rules[:room]
		}
		for i := range rules {
			rules[i].Order = order
			order++
			p.Author = append(p.Author, rules[i])
		}
		changed = true
	}
	return changed
}
