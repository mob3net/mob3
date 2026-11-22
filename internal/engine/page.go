package engine

import "net/url"

// Page is a loaded document plus the assets needed to display it.
type Page struct {
	URL     *url.URL
	Base    *url.URL
	Root    *Node
	Title   string
	Author  []Rule
	Scripts []Script
	// ImportMap holds the document's <script type="importmap"> imports, so
	// module scripts can resolve bare specifiers like "react".
	ImportMap   map[string]string
	RawHTML     string
	Status      int
	Blocked     bool
	BlockReason string
	// Local records that the document itself was fetched from a loopback,
	// private or link-local address. Its subresources may then reach other
	// local addresses; a public document may not.
	Local bool
	// Download marks a response that is not renderable (a binary or other
	// non-HTML resource). The browser shows a curl hint instead of applying it.
	Download     bool
	DownloadName string
	// PDF holds the extracted text when this page came from a PDF response.
	PDF *PDFDocument
	// parsedStyles records the inline <style> elements already represented in
	// Author, so component CSS injected by scripts is parsed once.
	parsedStyles map[*Node]string
}

// Script is one page script in document order. Src is a label for errors.
type Script struct {
	Code string
	Src  string
	Node *Node
	// Module marks an ES module script. Module scripts are parsed and linked
	// through the module loader instead of running as a classic script.
	Module bool
}

func (p *Page) SeedParsedStyles() {
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
func (p *Page) CollectDynamicStyles() bool {
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
