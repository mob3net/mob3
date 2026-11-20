package browser

import (
	"strconv"
	"strings"
)

// AttrTest is a parsed attribute selector such as [href] or [type="text"].
type AttrTest struct {
	Name  string
	Op    string // "", "=", "~=", "|=", "^=", "$=", "*="
	Value string
	Fold  bool
}

// SelPart is one compound selector plus the combinator that links it to the
// compound on its left.
type SelPart struct {
	Tag     string // empty or "*" means any
	ID      string
	Classes []string
	Attrs   []AttrTest
	Pseudos []string
	Pseudo  string // "before" or "after" for a pseudo-element selector
	Not     []SelPart
	Is      []SelPart
	Comb    byte // 0, ' ', '>', '+', '~'
}

// Selector is a complex selector (one item of a comma-separated list).
type Selector struct {
	Parts []SelPart
	Spec  [3]int
	Text  string
}

// Rule is a selector plus its declarations and source order.
type Rule struct {
	Selector Selector
	Decls    []Declaration
	Order    int
	Media    string // @media condition that must match, or empty
}

// rulesUseHover reports whether any selector depends on the :hover state, so
// pointer motion only triggers a style pass when it can matter.
func rulesUseHover(rules []Rule) bool {
	for _, r := range rules {
		if selectorUsesHover(r.Selector) {
			return true
		}
	}
	return false
}

func selectorUsesHover(s Selector) bool {
	for _, p := range s.Parts {
		if partUsesHover(p) {
			return true
		}
	}
	return false
}

func partUsesHover(p SelPart) bool {
	for _, ps := range p.Pseudos {
		name := ps
		if i := strings.IndexByte(ps, '('); i >= 0 {
			name = ps[:i]
		}
		if name == "hover" {
			return true
		}
	}
	for _, np := range p.Not {
		if partUsesHover(np) {
			return true
		}
	}
	for _, np := range p.Is {
		if partUsesHover(np) {
			return true
		}
	}
	return false
}

// ParseSelector parses a single complex selector.
func ParseSelector(s string) (*Selector, error) {
	sel := &Selector{Text: strings.TrimSpace(s)}
	i := 0
	var comb byte
	for {
		for i < len(s) && isCSSSpace(s[i]) {
			i++
		}
		if i >= len(s) {
			break
		}
		if len(sel.Parts) > 0 {
			if comb == 0 {
				comb = ' '
			}
		}
		if s[i] == '>' || s[i] == '+' || s[i] == '~' {
			comb = s[i]
			i++
			for i < len(s) && isCSSSpace(s[i]) {
				i++
			}
			if i >= len(s) {
				return nil, errSelector
			}
		}
		part, spec, n, ok := parseCompound(s[i:])
		if !ok || n <= 0 {
			// No progress would make the loop below spin forever while
			// appending empty parts (and exhaust memory/GC).
			return nil, errSelector
		}
		part.Comb = comb
		comb = 0
		sel.Parts = append(sel.Parts, part)
		sel.Spec[0] += spec[0]
		sel.Spec[1] += spec[1]
		sel.Spec[2] += spec[2]
		i += n
	}
	if len(sel.Parts) == 0 {
		return nil, errSelector
	}
	return sel, nil
}

type selectorError struct{}

func (selectorError) Error() string { return "invalid selector" }

var errSelector = selectorError{}

func isCSSSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

// readCSSIdent reads a CSS identifier starting at i, resolving backslash
// escapes. Utility frameworks escape the separators in class names, so
// ".desktop\:grid-col-3" must yield the class "desktop:grid-col-3" instead of
// a class plus a bogus pseudo-class.
func readCSSIdent(s string, i int) (string, int) {
	var b strings.Builder
	for i < len(s) {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			j := i + 1
			if isHexDigit(s[j]) {
				val, digits := 0, 0
				for j < len(s) && digits < 6 && isHexDigit(s[j]) {
					val = val*16 + hexDigit(s[j])
					j++
					digits++
				}
				if j < len(s) && isSpaceByte(s[j]) {
					j++
				}
				if val > 0 && val <= 0x10FFFF {
					b.WriteRune(rune(val))
				} else {
					b.WriteRune('\uFFFD')
				}
				i = j
				continue
			}
			b.WriteByte(s[j])
			i += 2
			continue
		}
		if !isCSSIdentChar(c) {
			break
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), i
}

func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func hexDigit(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	default:
		return int(c-'A') + 10
	}
}

func parseCompound(s string) (SelPart, [3]int, int, bool) {
	var p SelPart
	var spec [3]int
	i := 0
	if i < len(s) && s[i] == '*' {
		p.Tag = "*"
		i++
	} else if i < len(s) && isCSSIdentStart(s[i]) {
		name, j := readCSSIdent(s, i)
		p.Tag = strings.ToLower(name)
		spec[2]++
		i = j
	}
	for i < len(s) {
		switch s[i] {
		case '#':
			name, j := readCSSIdent(s, i+1)
			if name == "" {
				return p, spec, 0, false
			}
			p.ID = name
			spec[0]++
			i = j
		case '.':
			name, j := readCSSIdent(s, i+1)
			if name == "" {
				return p, spec, 0, false
			}
			p.Classes = append(p.Classes, name)
			spec[1]++
			i = j
		case '[':
			end := strings.IndexByte(s[i:], ']')
			if end < 0 {
				return p, spec, 0, false
			}
			at, ok := parseAttrTest(s[i+1 : i+end])
			if !ok {
				return p, spec, 0, false
			}
			p.Attrs = append(p.Attrs, at)
			spec[1]++
			i += end + 1
		case ':':
			j := i + 1
			if j < len(s) && s[j] == ':' {
				j++
			}
			nameStart := j
			for j < len(s) && (isCSSIdentChar(s[j]) || s[j] == '-') {
				j++
			}
			name := strings.ToLower(s[nameStart:j])
			arg := ""
			if j < len(s) && s[j] == '(' {
				depth := 1
				k := j + 1
				for k < len(s) && depth > 0 {
					if s[k] == '(' {
						depth++
					} else if s[k] == ')' {
						depth--
					}
					k++
				}
				if depth != 0 {
					return p, spec, 0, false
				}
				arg = strings.TrimSpace(s[j+1 : k-1])
				j = k
			}
			// ::before/::after generate an empty box; other pseudo-elements
			// cannot match because we do not generate their content.
			if name == "before" || name == "after" {
				p.Pseudo = name
				spec[1]++
				i = j
				continue
			}
			if strings.HasPrefix(s[nameStart:], ":") {
				p.Pseudos = append(p.Pseudos, "!never")
				spec[1]++
				i = j
				continue
			}
			if name == "not" {
				inner, ispec, n, ok := parseCompound(arg)
				if !ok || n != len(arg) || inner.Comb != 0 {
					return p, spec, 0, false
				}
				inner.Comb = 0
				p.Not = append(p.Not, inner)
				spec[1]++
				for k := range spec {
					spec[k] += ispec[k]
				}
			} else if name == "is" || name == "where" {
				var best [3]int
				found := false
				for _, alt := range splitTopLevel(arg, ',') {
					alt = strings.TrimSpace(alt)
					inner, ispec, n, ok := parseCompound(alt)
					if !ok || n != len(alt) {
						continue
					}
					inner.Comb = 0
					p.Is = append(p.Is, inner)
					found = true
					for k := range best {
						if ispec[k] > best[k] {
							best[k] = ispec[k]
						}
					}
				}
				if !found {
					return p, spec, 0, false
				}
				spec[1]++
				for k := range spec {
					spec[k] += best[k]
				}
			} else {
				p.Pseudos = append(p.Pseudos, name+"("+arg+")")
				spec[1]++
			}
			i = j
		default:
			return p, spec, i, true
		}
	}
	return p, spec, i, true
}

func isCSSIdentStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == '-' || c == '\\' || c >= 0x80
}

func parseAttrTest(s string) (AttrTest, bool) {
	var at AttrTest
	i := 0
	for i < len(s) && isCSSSpace(s[i]) {
		i++
	}
	start := i
	for i < len(s) && !isCSSSpace(s[i]) && s[i] != '=' && s[i] != '~' && s[i] != '|' && s[i] != '^' && s[i] != '$' && s[i] != '*' {
		i++
	}
	at.Name = strings.ToLower(s[start:i])
	if at.Name == "" {
		return at, false
	}
	for i < len(s) && isCSSSpace(s[i]) {
		i++
	}
	if i < len(s) {
		if s[i] == '=' {
			at.Op = "="
			i++
		} else if i+1 < len(s) && s[i+1] == '=' {
			switch s[i] {
			case '~', '|', '^', '$', '*':
				at.Op = string(s[i]) + "="
				i += 2
			default:
				return at, false
			}
		} else {
			return at, false
		}
		for i < len(s) && isCSSSpace(s[i]) {
			i++
		}
		if i < len(s) && (s[i] == '"' || s[i] == '\'') {
			q := s[i]
			i++
			start = i
			for i < len(s) && s[i] != q {
				i++
			}
			at.Value = s[start:i]
			i++
		} else {
			start = i
			for i < len(s) && !isCSSSpace(s[i]) {
				i++
			}
			at.Value = s[start:i]
		}
		for i < len(s) && isCSSSpace(s[i]) {
			i++
		}
		if i < len(s) && (s[i] == 'i' || s[i] == 'I') {
			at.Fold = true
			i++
		}
	}
	for i < len(s) && isCSSSpace(s[i]) {
		i++
	}
	return at, i == len(s)
}

// Match reports whether the selector matches element n.
func (s *Selector) Match(n *Node) bool {
	if n == nil || n.Type != ElementNode || len(s.Parts) == 0 {
		return false
	}
	for _, p := range s.Parts {
		if p.Pseudo != "" {
			return false
		}
	}
	return matchPart(s.Parts, len(s.Parts)-1, n)
}

// MatchPseudo reports whether the selector matches element n as the given
// generated pseudo-element.
func (s *Selector) MatchPseudo(n *Node, pseudo string) bool {
	if n == nil || n.Type != ElementNode || len(s.Parts) == 0 {
		return false
	}
	last := len(s.Parts) - 1
	for i, p := range s.Parts {
		if p.Pseudo == "" {
			continue
		}
		if i != last || p.Pseudo != pseudo {
			return false
		}
	}
	return matchPart(s.Parts, last, n)
}

func matchPart(parts []SelPart, i int, n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	if !parts[i].matches(n) {
		return false
	}
	if i == 0 {
		return true
	}
	switch parts[i].Comb {
	case '>':
		return matchPart(parts, i-1, parentElement(n))
	case '+':
		p := prevElement(n)
		return p != nil && matchPart(parts, i-1, p)
	case '~':
		for p := prevElement(n); p != nil; p = prevElement(p) {
			if matchPart(parts, i-1, p) {
				return true
			}
		}
		return false
	default:
		for p := parentElement(n); p != nil; p = parentElement(p) {
			if matchPart(parts, i-1, p) {
				return true
			}
		}
		return false
	}
}

func (p *SelPart) matches(n *Node) bool {
	if p.Tag != "" && p.Tag != "*" && p.Tag != n.Tag {
		return false
	}
	if p.ID != "" && n.Attr("id") != p.ID {
		return false
	}
	if len(p.Classes) > 0 {
		cls := n.ClassList()
		for _, want := range p.Classes {
			found := false
			for _, c := range cls {
				if c == want {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	for _, at := range p.Attrs {
		v := n.Attr(at.Name)
		if !n.HasAttr(at.Name) {
			return false
		}
		a, b := v, at.Value
		if at.Fold {
			a, b = strings.ToLower(a), strings.ToLower(b)
		}
		switch at.Op {
		case "":
		case "=":
			if a != b {
				return false
			}
		case "~=":
			ok := false
			for _, f := range strings.Fields(a) {
				if f == b {
					ok = true
					break
				}
			}
			if !ok {
				return false
			}
		case "|=":
			if a != b && !strings.HasPrefix(a, b+"-") {
				return false
			}
		case "^=":
			if b == "" || !strings.HasPrefix(a, b) {
				return false
			}
		case "$=":
			if b == "" || !strings.HasSuffix(a, b) {
				return false
			}
		case "*=":
			if b == "" || !strings.Contains(a, b) {
				return false
			}
		default:
			return false
		}
	}
	for _, ps := range p.Pseudos {
		if !matchPseudo(ps, n) {
			return false
		}
	}
	for i := range p.Not {
		if p.Not[i].matches(n) {
			return false
		}
	}
	if len(p.Is) > 0 {
		ok := false
		for i := range p.Is {
			if p.Is[i].matches(n) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func parentElement(n *Node) *Node {
	if n == nil {
		return nil
	}
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == ElementNode {
			return p
		}
	}
	return nil
}

func prevElement(n *Node) *Node {
	if n == nil || n.Parent == nil {
		return nil
	}
	var prev *Node
	for _, c := range n.Parent.Children {
		if c == n {
			return prev
		}
		if c.Type == ElementNode {
			prev = c
		}
	}
	return nil
}

func nextElement(n *Node) *Node {
	if n == nil || n.Parent == nil {
		return nil
	}
	seen := false
	for _, c := range n.Parent.Children {
		if c == n {
			seen = true
			continue
		}
		if seen && c.Type == ElementNode {
			return c
		}
	}
	return nil
}

func elementIndex(n *Node) (idx, count int) {
	if n == nil || n.Parent == nil {
		return 0, 0
	}
	for _, c := range n.Parent.Children {
		if c.Type != ElementNode {
			continue
		}
		count++
		if c == n {
			idx = count
		}
	}
	return idx, count
}

func matchPseudo(ps string, n *Node) bool {
	name := ps
	arg := ""
	if i := strings.IndexByte(ps, '('); i >= 0 {
		name = ps[:i]
		arg = strings.TrimSuffix(ps[i+1:], ")")
	}
	switch name {
	case "first-child":
		i, _ := elementIndex(n)
		return i == 1
	case "last-child":
		i, c := elementIndex(n)
		return i == c
	case "only-child":
		_, c := elementIndex(n)
		return c == 1
	case "first-of-type":
		idx := 0
		for p := prevElement(n); p != nil; p = prevElement(p) {
			if p.Tag == n.Tag {
				idx++
			}
		}
		return idx == 0
	case "last-of-type":
		for p := nextElement(n); p != nil; p = nextElement(p) {
			if p.Tag == n.Tag {
				return false
			}
		}
		return true
	case "nth-child":
		i, _ := elementIndex(n)
		return matchNth(arg, i)
	case "nth-last-child":
		i, c := elementIndex(n)
		return matchNth(arg, c-i+1)
	case "root":
		return n.Tag == "html" && parentElement(n) == nil || n.Parent != nil && n.Parent.Type == DocumentNode && n.Tag == "html"
	case "empty":
		for _, c := range n.Children {
			if c.Type == ElementNode {
				return false
			}
			if c.Type == TextNode && strings.TrimSpace(c.Text) != "" {
				return false
			}
		}
		return true
	case "link", "any-link":
		return n.Tag == "a" && n.HasAttr("href")
	case "checked":
		return n.HasAttr("checked") || n.HasAttr("selected")
	case "disabled":
		return n.HasAttr("disabled")
	case "enabled":
		return !n.HasAttr("disabled")
	case "hover":
		return n.Hovered
	case "never", "focus", "active", "visited", "target":
		return false
	}
	return false
}

func matchNth(arg string, i int) bool {
	arg = strings.ToLower(strings.TrimSpace(arg))
	if arg == "odd" {
		return i%2 == 1
	}
	if arg == "even" || arg == "2n" {
		return i%2 == 0
	}
	nArg := 0
	rest := arg
	neg := false
	if strings.HasPrefix(rest, "-") {
		neg = true
		rest = rest[1:]
	}
	if j := strings.IndexByte(rest, 'n'); j >= 0 {
		num := rest[:j]
		if num == "" {
			nArg = 1
		} else if v, err := strconv.Atoi(num); err == nil {
			nArg = v
		} else {
			return false
		}
		if neg {
			nArg = -nArg
		}
		off := strings.TrimSpace(rest[j+1:])
		off = strings.ReplaceAll(off, " ", "")
		if off == "" {
			return i%nArg == 0
		}
		b, err := strconv.Atoi(strings.TrimPrefix(off, "+"))
		if err != nil {
			return false
		}
		if nArg == 0 {
			return i == b
		}
		d := i - b
		return d%nArg == 0 && d/nArg >= 0
	}
	if v, err := strconv.Atoi(arg); err == nil {
		return i == v
	}
	return false
}
