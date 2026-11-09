package browser

import (
	"image"
	"strconv"
	"strings"
	"unicode/utf8"
)

// NodeType is the kind of a DOM node.
type NodeType int

const (
	DocumentNode NodeType = iota
	ElementNode
	TextNode
)

// Attr is a single HTML attribute.
type Attr struct {
	Key   string
	Value string
}

// Node is a minimal DOM node. Parsing produces a tree of Document, Element and
// Text nodes. Style is filled in by the cascade stage, Img by the loader.
type Node struct {
	Type     NodeType
	Tag      string
	Attrs    []Attr
	Text     string
	Parent   *Node
	Children []*Node

	Style *Style
	Img   image.Image

	// Image load state, used by the loader and painter.
	ImgFailed bool
	ImgSrc    string

	// Editable form state.
	Value        string
	ValueSet     bool
	InputFocused bool

	// Cached class attribute split, reused by selector matching. The raw value
	// is kept so mutations invalidate the cache without explicit bookkeeping.
	classRaw    string
	classList   []string
	classCached bool
}

// Attr returns the value of an attribute, case-insensitively for the key.
func (n *Node) Attr(key string) string {
	if n == nil {
		return ""
	}
	for i := range n.Attrs {
		if n.Attrs[i].Key == key {
			return n.Attrs[i].Value
		}
	}
	return ""
}

// HasAttr reports whether the attribute is present.
func (n *Node) HasAttr(key string) bool {
	if n == nil {
		return false
	}
	for i := range n.Attrs {
		if n.Attrs[i].Key == key {
			return true
		}
	}
	return false
}

// ClassList splits the class attribute. The result is cached until the
// attribute value changes; selector matching calls this for every rule and
// element, so re-splitting the string there is expensive.
func (n *Node) ClassList() []string {
	if n == nil {
		return nil
	}
	v := n.Attr("class")
	if n.classCached && n.classRaw == v {
		return n.classList
	}
	n.classRaw = v
	n.classCached = true
	if v == "" {
		n.classList = nil
	} else {
		n.classList = strings.Fields(v)
	}
	return n.classList
}

// IsElement reports whether n is an element with the given tag.
func (n *Node) IsElement(tag string) bool {
	return n != nil && n.Type == ElementNode && n.Tag == tag
}

// TextContent concatenates the text below n.
func (n *Node) TextContent() string {
	if n == nil {
		return ""
	}
	if n.Type == TextNode {
		return n.Text
	}
	var b strings.Builder
	var walk func(*Node)
	walk = func(x *Node) {
		for _, c := range x.Children {
			if c.Type == TextNode {
				b.WriteString(c.Text)
			} else {
				walk(c)
			}
		}
	}
	walk(n)
	return b.String()
}

// FindFirst returns the first descendant element with the given tag.
func (n *Node) FindFirst(tag string) *Node {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.Type != ElementNode {
			continue
		}
		if c.Tag == tag {
			return c
		}
		if f := c.FindFirst(tag); f != nil {
			return f
		}
	}
	return nil
}

// FindAll returns all descendant elements matching fn in document order.
func (n *Node) FindAll(fn func(*Node) bool) []*Node {
	var out []*Node
	var walk func(*Node)
	walk = func(x *Node) {
		for _, c := range x.Children {
			if c.Type != ElementNode {
				continue
			}
			if fn(c) {
				out = append(out, c)
			}
			walk(c)
		}
	}
	if n != nil {
		walk(n)
	}
	return out
}

var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

var rawTextElements = map[string]bool{
	"script": true, "style": true, "title": true, "textarea": true,
	"xmp": true, "plaintext": true,
}

var blockClosersP = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true,
	"details": true, "div": true, "dl": true, "fieldset": true,
	"figcaption": true, "figure": true, "footer": true, "form": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"header": true, "hr": true, "main": true, "nav": true, "ol": true,
	"p": true, "pre": true, "section": true, "table": true, "ul": true,
}

type htmlParser struct {
	s     string
	i     int
	root  *Node
	stack []*Node
}

// ParseHTML parses an HTML document into a DOM tree. It is deliberately
// forgiving: unknown tags become plain elements and unmatched end tags are
// dropped.
func ParseHTML(src string) *Node {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	src = strings.ReplaceAll(src, "\x00", "")
	p := &htmlParser{s: src}
	p.root = &Node{Type: DocumentNode}
	p.stack = []*Node{p.root}
	for p.i < len(p.s) {
		if p.s[p.i] == '<' && p.parseMarkup() {
			continue
		}
		p.parseText()
	}
	return p.root
}

func (p *htmlParser) top() *Node { return p.stack[len(p.stack)-1] }

func (p *htmlParser) addChild(n *Node) *Node {
	parent := p.top()
	n.Parent = parent
	parent.Children = append(parent.Children, n)
	return n
}

func (p *htmlParser) addText(s string) {
	if s == "" {
		return
	}
	parent := p.top()
	if n := len(parent.Children); n > 0 {
		last := parent.Children[n-1]
		if last.Type == TextNode {
			last.Text += s
			return
		}
	}
	p.addChild(&Node{Type: TextNode, Text: s})
}

func (p *htmlParser) parseText() {
	start := p.i
	for p.i < len(p.s) && p.s[p.i] != '<' {
		p.i++
	}
	if p.i == start {
		p.addText(string(p.s[p.i]))
		p.i++
		return
	}
	p.addText(Unescape(p.s[start:p.i]))
}

// parseMarkup consumes one markup construct. It returns false when the '<' is
// not actually the start of markup, so the caller can treat it as text.
func (p *htmlParser) parseMarkup() bool {
	if p.i+1 >= len(p.s) {
		return false
	}
	switch c := p.s[p.i+1]; {
	case c == '!':
		if strings.HasPrefix(p.s[p.i:], "<!--") {
			if j := strings.Index(p.s[p.i+4:], "-->"); j >= 0 {
				p.i += 4 + j + 3
			} else {
				p.i = len(p.s)
			}
			return true
		}
		p.skipToGT()
		return true
	case c == '?':
		p.skipToGT()
		return true
	case c == '/':
		if p.i+2 < len(p.s) && isNameStart(p.s[p.i+2]) {
			p.parseEndTag()
			return true
		}
		return false
	case isNameStart(c):
		p.parseStartTag()
		return true
	}
	return false
}

func (p *htmlParser) skipToGT() {
	if j := strings.IndexByte(p.s[p.i:], '>'); j >= 0 {
		p.i += j + 1
	} else {
		p.i = len(p.s)
	}
}

func isNameStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isNameChar(c byte) bool {
	return isNameStart(c) || c >= '0' && c <= '9' || c == '-' || c == '_' || c == ':' || c == '.'
}

func (p *htmlParser) parseEndTag() {
	p.i += 2 // "</"
	start := p.i
	for p.i < len(p.s) && isNameChar(p.s[p.i]) {
		p.i++
	}
	tag := strings.ToLower(p.s[start:p.i])
	p.skipToGT()
	p.closeTag(tag)
}

func (p *htmlParser) closeTag(tag string) {
	for i := len(p.stack) - 1; i >= 1; i-- {
		if p.stack[i].Tag == tag {
			p.stack = p.stack[:i]
			return
		}
	}
}

func (p *htmlParser) closeIfOpen(tag string) {
	for i := len(p.stack) - 1; i >= 1; i-- {
		if p.stack[i].Tag == tag {
			p.stack = p.stack[:i]
			return
		}
	}
}

// closeWhileTop pops only while the current top element is in tags. Unlike
// closeIfOpen it can never cross a table/list boundary, so nested structures
// keep their parent chain intact.
func (p *htmlParser) closeWhileTop(tags ...string) {
	for len(p.stack) > 1 {
		top := p.stack[len(p.stack)-1].Tag
		found := false
		for _, t := range tags {
			if top == t {
				found = true
				break
			}
		}
		if !found {
			return
		}
		p.stack = p.stack[:len(p.stack)-1]
	}
}

func (p *htmlParser) parseStartTag() {
	p.i++ // '<'
	start := p.i
	for p.i < len(p.s) && isNameChar(p.s[p.i]) {
		p.i++
	}
	tag := strings.ToLower(p.s[start:p.i])

	var attrs []Attr
	selfClose := false
	for p.i < len(p.s) {
		for p.i < len(p.s) && isSpaceByte(p.s[p.i]) {
			p.i++
		}
		if p.i >= len(p.s) {
			break
		}
		if p.s[p.i] == '>' {
			p.i++
			break
		}
		if p.s[p.i] == '/' {
			if p.i+1 < len(p.s) && p.s[p.i+1] == '>' {
				selfClose = true
				p.i += 2
				break
			}
			p.i++
			continue
		}
		// attribute name
		as := p.i
		for p.i < len(p.s) && !isSpaceByte(p.s[p.i]) && p.s[p.i] != '=' && p.s[p.i] != '>' && p.s[p.i] != '/' {
			p.i++
		}
		name := strings.ToLower(p.s[as:p.i])
		for p.i < len(p.s) && isSpaceByte(p.s[p.i]) {
			p.i++
		}
		val := ""
		if p.i < len(p.s) && p.s[p.i] == '=' {
			p.i++
			for p.i < len(p.s) && isSpaceByte(p.s[p.i]) {
				p.i++
			}
			if p.i < len(p.s) && (p.s[p.i] == '"' || p.s[p.i] == '\'') {
				q := p.s[p.i]
				p.i++
				vs := p.i
				for p.i < len(p.s) && p.s[p.i] != q {
					p.i++
				}
				val = p.s[vs:p.i]
				if p.i < len(p.s) {
					p.i++
				}
			} else {
				vs := p.i
				for p.i < len(p.s) && !isSpaceByte(p.s[p.i]) && p.s[p.i] != '>' {
					p.i++
				}
				val = p.s[vs:p.i]
			}
		}
		if name != "" {
			attrs = append(attrs, Attr{Key: name, Value: Unescape(val)})
		}
	}
	_ = selfClose

	// Raw-text elements swallow everything up to their end tag.
	if rawTextElements[tag] {
		lower := strings.ToLower(p.s)
		needle := "</" + tag
		idx := strings.Index(lower[p.i:], needle)
		var content string
		if idx >= 0 {
			content = p.s[p.i : p.i+idx]
			if gt := strings.IndexByte(p.s[p.i+idx:], '>'); gt >= 0 {
				p.i += idx + gt + 1
			} else {
				p.i = len(p.s)
			}
		} else {
			content = p.s[p.i:]
			p.i = len(p.s)
		}
		if tag == "title" {
			p.addElementRaw(tag, attrs, content, true)
		} else {
			// Keep raw_text content (including script source) in the tree;
			// the UA stylesheet hides these elements from layout.
			p.addElementRaw(tag, attrs, content, false)
		}
		return
	}

	// Implicit closes for common HTML structures. These are deliberately
	// scoped to the current container so nested tables/lists survive.
	switch tag {
	case "li":
		p.closeWhileTop("p", "li")
	case "dt", "dd":
		p.closeWhileTop("p", "dt", "dd")
	case "tr":
		p.closeWhileTop("td", "th", "tr")
	case "td", "th":
		p.closeWhileTop("td", "th")
	case "thead", "tbody", "tfoot":
		p.closeWhileTop("td", "th", "tr")
	case "option":
		p.closeWhileTop("option")
	default:
		if blockClosersP[tag] {
			p.closeIfOpen("p")
		}
	}

	n := &Node{Type: ElementNode, Tag: tag, Attrs: attrs}
	p.addChild(n)
	if !voidElements[tag] && !selfClose {
		p.stack = append(p.stack, n)
	}
}

func (p *htmlParser) addElementRaw(tag string, attrs []Attr, content string, unescape bool) {
	n := &Node{Type: ElementNode, Tag: tag, Attrs: attrs}
	p.addChild(n)
	if content != "" {
		if unescape {
			content = Unescape(content)
		}
		c := &Node{Type: TextNode, Text: content, Parent: n}
		n.Children = append(n.Children, c)
	}
}

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\v' || c == '\r'
}

var entityTable = map[string]string{
	"amp": "&", "lt": "<", "gt": ">", "quot": `"`, "apos": "'",
	"nbsp": "\u00a0", "ensp": "\u2002", "emsp": "\u2003", "thinsp": "\u2009",
	"copy": "©", "reg": "®", "trade": "™", "hellip": "…",
	"mdash": "—", "ndash": "–", "lsquo": "‘", "rsquo": "’",
	"ldquo": "“", "rdquo": "”", "laquo": "«", "raquo": "»",
	"bull": "•", "middot": "·", "sect": "§", "para": "¶",
	"dagger": "†", "Dagger": "‡", "permil": "‰", "prime": "′",
	"Prime": "″", "times": "×", "divide": "÷", "plusmn": "±",
	"deg": "°", "micro": "µ", "frac12": "½", "frac14": "¼",
	"frac34": "¾", "sup2": "²", "sup3": "³", "sup1": "¹",
	"euro": "€", "pound": "£", "yen": "¥", "cent": "¢",
	"curren": "¤", "larr": "←", "rarr": "→", "uarr": "↑", "darr": "↓",
	"harr": "↔", "crarr": "↵", "lArr": "⇐", "rArr": "⇒",
	"hArr": "⇔", "forall": "∀", "exist": "∃", "empty": "∅",
	"nabla": "∇", "isin": "∈", "notin": "∉", "ni": "∋",
	"prod": "∏", "sum": "∑", "minus": "−", "lowast": "∗",
	"radic": "√", "prop": "∝", "infin": "∞", "ang": "∠",
	"and": "∧", "or": "∨", "cap": "∩", "cup": "∪",
	"int": "∫", "there4": "∴", "sim": "∼", "cong": "≅",
	"asymp": "≈", "ne": "≠", "equiv": "≡", "le": "≤", "ge": "≥",
	"sub": "⊂", "sup": "⊃", "nsub": "⊄", "sube": "⊆", "supe": "⊇",
	"oplus": "⊕", "otimes": "⊗", "perp": "⊥", "sdot": "⋅",
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ",
	"epsilon": "ε", "zeta": "ζ", "eta": "η", "theta": "θ",
	"iota": "ι", "kappa": "κ", "lambda": "λ", "mu": "μ",
	"nu": "ν", "xi": "ξ", "omicron": "ο", "pi": "π",
	"rho": "ρ", "sigma": "σ", "tau": "τ", "upsilon": "υ",
	"phi": "φ", "chi": "χ", "psi": "ψ", "omega": "ω",
	"Alpha": "Α", "Beta": "Β", "Gamma": "Γ", "Delta": "Δ",
	"Omega": "Ω", "Sigma": "Σ", "Pi": "Π", "Lambda": "Λ",
	"check": "✓", "cross": "✗", "star": "☆", "starf": "★",
	"hearts": "♥", "spades": "♠", "clubs": "♣", "diams": "♦",
	"loz": "◊", "iexcl": "¡", "iquest": "¿", "shy": "\u00ad",
	"not": "¬", "macr": "¯", "acute": "´", "cedil": "¸",
	"ordf": "ª", "ordm": "º", "brvbar": "¦", "uml": "¨",
	"aacute": "á", "eacute": "é", "iacute": "í", "oacute": "ó",
	"uacute": "ú", "agrave": "à", "egrave": "è", "igrave": "ì",
	"ograve": "ò", "ugrave": "ù", "acirc": "â", "ecirc": "ê",
	"icirc": "î", "ocirc": "ô", "ucirc": "û", "atilde": "ã",
	"otilde": "õ", "ntilde": "ñ", "ccedil": "ç", "auml": "ä",
	"euml": "ë", "iuml": "ï", "ouml": "ö", "uuml": "ü",
	"yuml": "ÿ", "szlig": "ß", "AElig": "Æ", "aelig": "æ",
	"OElig": "Œ", "oelig": "œ", "Aring": "Å", "aring": "å",
	"Oslash": "Ø", "oslash": "ø", "Ccedil": "Ç", "Ntilde": "Ñ",
}

// Unescape decodes HTML character references.
func Unescape(s string) string {
	if !strings.ContainsRune(s, '&') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '&' {
			b.WriteByte(s[i])
			i++
			continue
		}
		semi := strings.IndexByte(s[i:], ';')
		if semi <= 1 || semi > 34 {
			b.WriteByte(s[i])
			i++
			continue
		}
		body := s[i+1 : i+semi]
		if body[0] == '#' {
			var code int64
			var err error
			if len(body) > 2 && (body[1] == 'x' || body[1] == 'X') {
				code, err = strconv.ParseInt(body[2:], 16, 32)
			} else {
				code, err = strconv.ParseInt(body[1:], 10, 32)
			}
			if err == nil && code > 0 && code <= utf8.MaxRune {
				b.WriteRune(rune(code))
				i += semi + 1
				continue
			}
		} else if v, ok := entityTable[body]; ok {
			b.WriteString(v)
			i += semi + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
