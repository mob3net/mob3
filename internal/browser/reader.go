package browser

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// readerRules is the author stylesheet used for the article view. It keeps the
// text column narrow on wide terminals and gives paragraphs room to breathe.
var readerRules = ParseStylesheet(`
html { background:#ffffff; }
body { background:#ffffff; color:#1f2328; }
article { display:block; max-width:600px; margin:16px auto; padding:0 8px; text-align:justify; }
form { display:flex; gap:8px; margin:16px 0; }
input[type=search] { flex:1; }
h1 { margin:24px 0 24px; }
h2 { margin:20px 0 8px; }
h3, h4, h5, h6 { margin:16px 0 6px; }
h1, h2, h3, h4, h5, h6 { text-align:left; color:#1f2328; text-decoration:none; }
p { margin:10px 0; }
ul, ol { margin:10px 0; }
blockquote { margin:12px 16px; }
pre { margin:12px 0; }
`)

// readerStats is the text profile used to rank candidate article containers.
type readerStats struct {
	text       int
	links      int
	paragraphs int
}

func (s readerStats) linkDensity() float64 {
	if s.text <= 0 {
		return 0
	}
	return float64(s.links) / float64(s.text)
}

// readerJunkTokens are class/id words that almost never belong to article
// prose. They are matched as whole tokens, so "site-nav" and "comments" count
// while "navigation-history" does not accidentally become an ad.
var readerJunkTokens = map[string]bool{
	"ad": true, "ads": true, "advert": true, "advertisement": true,
	"banner": true, "breadcrumb": true, "breadcrumbs": true,
	"comment": true, "comments": true, "consent": true, "cookie": true,
	"cookies": true, "disqus": true, "menu": true, "navbar": true,
	"nav": true, "navigation": true, "newsletter": true, "overlay": true,
	"pager": true, "pagination": true, "popup": true, "promo": true,
	"promoted": true, "recommended": true, "related": true, "share": true,
	"sharing": true, "sidebar": true, "social": true, "sponsor": true,
	"sponsored": true, "subscribe": true, "subscription": true,
	"toolbar": true, "widget": true,
}

// readerPositiveTokens identify likely article containers.
var readerPositiveTokens = map[string]bool{
	"article": true, "body": true, "content": true, "entry": true,
	"main": true, "page": true, "post": true, "story": true, "text": true,
}

// readerBlockTags are kept verbatim in the extracted tree. Unknown tags are
// unwrapped so custom elements cannot hide their text from the reader.
var readerBlockTags = map[string]bool{
	"a": true, "abbr": true, "address": true, "article": true,
	"b": true, "blockquote": true, "br": true, "caption": true,
	"cite": true, "code": true, "dd": true, "del": true, "details": true,
	"dfn": true, "div": true, "dl": true, "dt": true, "em": true,
	"figcaption": true, "figure": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "hr": true,
	"i": true, "ins": true, "kbd": true, "li": true,
	"main": true, "mark": true, "ol": true, "p": true, "pre": true,
	"q": true, "s": true, "samp": true, "section": true, "small": true,
	"span": true, "strong": true, "sub": true, "summary": true,
	"sup": true, "table": true, "tbody": true, "td": true, "tfoot": true,
	"th": true, "thead": true, "time": true, "tr": true, "u": true,
	"ul": true, "var": true,
}

// readerKeptAttrs intentionally drops class, style and event handlers. Keeping
// them would let the page's own CSS recreate sidebars and overlays inside the
// reader column.
var readerKeptAttrs = map[string]bool{
	"alt": true, "cite": true, "colspan": true, "datetime": true,
	"dir": true, "headers": true, "height": true, "href": true,
	"id": true, "lang": true, "name": true, "reversed": true,
	"rowspan": true, "scope": true, "src": true, "start": true,
	"title": true, "type": true, "width": true,
}

// extractReaderPage builds a clean article document from the live page tree.
// It returns false when no meaningful content can be found.
func extractReaderPage(root *Node, title string) (*Node, bool) {
	if root == nil {
		return nil, false
	}
	stats := make(map[*Node]readerStats)
	readerStatsFor(root, stats)
	best := readerBestCandidate(root, stats)
	if best == nil {
		return nil, false
	}
	article := &Node{Type: ElementNode, Tag: "article"}
	appendReaderChildren(article, best, 0)
	if readerContentEmpty(article) {
		return nil, false
	}
	if strings.TrimSpace(title) != "" && article.FindFirst("h1") == nil {
		h1 := &Node{Type: ElementNode, Tag: "h1", Parent: article}
		h1.Children = []*Node{{Type: TextNode, Text: title, Parent: h1}}
		article.Children = append([]*Node{h1}, article.Children...)
	}
	html := &Node{Type: ElementNode, Tag: "html"}
	body := &Node{Type: ElementNode, Tag: "body", Parent: html}
	article.Parent = body
	body.Children = []*Node{article}
	html.Children = []*Node{body}
	doc := &Node{Type: DocumentNode, Children: []*Node{html}}
	html.Parent = doc
	return doc, true
}

// readerStatsFor computes text profiles bottom-up in one pass.
func readerStatsFor(n *Node, out map[*Node]readerStats) readerStats {
	if n == nil {
		return readerStats{}
	}
	if n.Type == TextNode {
		return readerStats{text: utf8.RuneCountInString(strings.TrimSpace(n.Text))}
	}
	if n.Type == ElementNode && isReaderJunk(n) {
		out[n] = readerStats{}
		return readerStats{}
	}
	var s readerStats
	for _, c := range n.Children {
		cs := readerStatsFor(c, out)
		s.text += cs.text
		s.links += cs.links
		s.paragraphs += cs.paragraphs
	}
	if n.Type == ElementNode {
		if n.Tag == "a" {
			s.links = s.text
		}
		if n.Tag == "p" && s.text >= 40 {
			s.paragraphs++
		}
		out[n] = s
	}
	return s
}

// readerBestCandidate prefers a real article/main container, then falls back
// to the highest-scoring prose block. Sidebar, nav and footer subtrees are
// excluded as candidates.
func readerBestCandidate(root *Node, stats map[*Node]readerStats) *Node {
	var best, semantic *Node
	var bestScore, semanticScore int
	root.FindAll(func(n *Node) bool {
		if !readerCandidateTag(n) || !readerCandidateAllowed(n) {
			return false
		}
		s := stats[n]
		if s.text == 0 {
			return false
		}
		score := s.text + s.paragraphs*80 - int(s.linkDensity()*float64(s.text)*2)
		score += readerTokenScore(n)
		switch n.Tag {
		case "article":
			score += 60
		case "main":
			score += 50
		case "section":
			score += 10
		}
		if strings.EqualFold(n.Attr("role"), "main") || strings.EqualFold(n.Attr("itemprop"), "articleBody") {
			score += 60
		}
		if best == nil || score > bestScore {
			best, bestScore = n, score
		}
		if readerSemanticCandidate(n) && (semantic == nil || score > semanticScore) {
			semantic, semanticScore = n, score
		}
		return false
	})
	if semantic != nil && stats[semantic].text >= 120 {
		return semantic
	}
	if best != nil && stats[best].text >= 40 {
		return best
	}
	return nil
}

func readerCandidateTag(n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	switch n.Tag {
	case "article", "main", "section", "div", "td", "body":
		return true
	}
	return strings.EqualFold(n.Attr("role"), "main") ||
		strings.EqualFold(n.Attr("itemprop"), "articleBody")
}

func readerSemanticCandidate(n *Node) bool {
	if n == nil {
		return false
	}
	return n.Tag == "article" || n.Tag == "main" ||
		strings.EqualFold(n.Attr("role"), "main") ||
		strings.EqualFold(n.Attr("itemprop"), "articleBody")
}

func readerCandidateAllowed(n *Node) bool {
	for p := n; p != nil; p = p.Parent {
		if p.Type != ElementNode {
			continue
		}
		if isReaderJunk(p) {
			return false
		}
	}
	return true
}

func readerTokenScore(n *Node) int {
	score := 0
	for _, token := range readerTokens(n.Attr("class") + " " + n.Attr("id")) {
		if readerPositiveTokens[token] {
			score += 20
		}
		if readerJunkTokens[token] {
			score -= 60
		}
	}
	return score
}

// isReaderJunk reports elements that are chrome, controls or tracked clutter.
func isReaderJunk(n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	switch n.Tag {
	case "script", "style", "noscript", "template", "iframe", "object",
		"embed", "canvas", "svg", "nav", "aside", "header", "footer",
		"form", "button", "input", "select", "textarea", "dialog", "menu":
		return true
	}
	if n.HasAttr("hidden") || strings.EqualFold(n.Attr("aria-hidden"), "true") {
		return true
	}
	switch strings.ToLower(n.Attr("role")) {
	case "navigation", "banner", "contentinfo", "complementary", "search",
		"dialog", "menu", "menubar":
		return true
	}
	style := strings.ReplaceAll(strings.ToLower(n.Attr("style")), " ", "")
	if strings.Contains(style, "display:none") || strings.Contains(style, "visibility:hidden") {
		return true
	}
	// html/body often carry site-wide feature flags whose tokenised class names
	// include words such as "menu". Never let those hide the whole document.
	if n.Tag == "html" || n.Tag == "body" {
		return false
	}
	for _, token := range readerTokens(n.Attr("class") + " " + n.Attr("id")) {
		if readerJunkTokens[token] {
			return true
		}
	}
	return false
}

func readerTokens(s string) []string {
	if s == "" {
		return nil
	}
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// appendReaderChildren deep-copies the useful parts of src into dst, unwrapping
// unknown containers and dropping junk subtrees.
func appendReaderChildren(dst, src *Node, depth int) {
	if dst == nil || src == nil || depth > 200 {
		return
	}
	for _, c := range src.Children {
		if c.Type == TextNode {
			if c.Text != "" {
				dst.Children = append(dst.Children, &Node{Type: TextNode, Text: c.Text, Parent: dst})
			}
			continue
		}
		if c.Type != ElementNode || isReaderJunk(c) {
			continue
		}
		if !readerBlockTags[c.Tag] {
			appendReaderChildren(dst, c, depth+1)
			continue
		}
		cc := &Node{Type: ElementNode, Tag: c.Tag, Parent: dst}
		for _, at := range c.Attrs {
			if readerKeptAttrs[strings.ToLower(at.Key)] {
				cc.Attrs = append(cc.Attrs, Attr{Key: at.Key, Value: at.Value})
			}
		}
		appendReaderChildren(cc, c, depth+1)
		dst.Children = append(dst.Children, cc)
	}
}

func readerContentEmpty(article *Node) bool {
	if article == nil {
		return true
	}
	return utf8.RuneCountInString(strings.TrimSpace(article.TextContent())) < 40
}
