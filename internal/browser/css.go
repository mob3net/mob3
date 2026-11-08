package browser

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// Declaration is one CSS property assignment.
type Declaration struct {
	Prop      string
	Value     string
	Important bool
}

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
	Not     []SelPart
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

// ParseDeclarations parses the body of a style attribute or rule block.
func ParseDeclarations(s string) []Declaration {
	var out []Declaration
	for _, chunk := range splitTopLevel(s, ';') {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		idx := indexTopLevel(chunk, ':')
		if idx < 0 {
			continue
		}
		prop := strings.ToLower(strings.TrimSpace(chunk[:idx]))
		val := strings.TrimSpace(chunk[idx+1:])
		if prop == "" || val == "" {
			continue
		}
		imp := false
		lv := strings.ToLower(val)
		if j := strings.LastIndex(lv, "!important"); j >= 0 {
			after := strings.TrimSpace(lv[j+len("!important"):])
			if after == "" {
				imp = true
				val = strings.TrimSpace(val[:j])
			}
		}
		out = append(out, Declaration{Prop: prop, Value: val, Important: imp})
	}
	return out
}

// ParseStylesheet parses a stylesheet into rules, preserving source order.
// Stylesheet parsing budgets. These keep hostile or generated multi-megabyte
// CSS from burning CPU and GC time during a page load.
var (
	maxCSSBytes     = 4 << 20
	maxCSSRules     = 50000
	maxCSSRuleBody  = 256 << 10
	maxCSSParseTime = time.Second
)

type parseState struct {
	order int
	rules int
	cache map[string]*Selector
	ctx   context.Context
	start time.Time
}

func ParseStylesheet(src string) []Rule {
	return ParseStylesheetContext(context.Background(), src)
}

// ParseStylesheetContext is ParseStylesheet with cancellation support.
func ParseStylesheetContext(ctx context.Context, src string) []Rule {
	if maxCSSBytes > 0 && len(src) > maxCSSBytes {
		src = src[:maxCSSBytes]
	}
	st := &parseState{cache: make(map[string]*Selector), ctx: ctx, start: time.Now()}
	return parseRules(src, st, "")
}

func parseRules(src string, st *parseState, media string) []Rule {
	capHint := len(src) / 64
	if capHint < 8 {
		capHint = 8
	}
	if capHint > 8192 {
		capHint = 8192
	}
	rules := make([]Rule, 0, capHint)
	i := 0
	skipWSComments(src, &i)
	for i < len(src) {
		if maxCSSRules > 0 && st.rules >= maxCSSRules {
			break
		}
		if st.ctx != nil && st.ctx.Err() != nil {
			break
		}
		if maxCSSParseTime > 0 && time.Since(st.start) > maxCSSParseTime {
			break
		}
		if src[i] == '@' {
			j := i + 1
			for j < len(src) && isCSSIdentChar(src[j]) {
				j++
			}
			name := strings.ToLower(src[i+1 : j])
			preludeStart := j
			for j < len(src) && src[j] != '{' && src[j] != ';' {
				j++
			}
			if j >= len(src) {
				break
			}
			if src[j] == ';' {
				i = j + 1
			} else {
				body, next, ok := scanBlock(src, j)
				if !ok {
					break
				}
				inner := media
				switch name {
				case "media":
					q := strings.TrimSpace(src[preludeStart:j])
					if inner != "" && q != "" {
						inner = inner + " and " + q
					} else if q != "" {
						inner = q
					}
					rules = append(rules, parseRules(body, st, inner)...)
				case "supports":
					rules = append(rules, parseRules(body, st, inner)...)
				}
				i = next
			}
			skipWSComments(src, &i)
			continue
		}
		// selector { declarations }
		start := i
		depth := 0
		for i < len(src) {
			c := src[i]
			if c == '"' || c == '\'' {
				i = skipString(src, i)
				continue
			}
			if c == '(' || c == '[' {
				depth++
			} else if c == ')' || c == ']' {
				if depth > 0 {
					depth--
				}
			} else if c == '{' && depth == 0 {
				break
			} else if c == '}' && depth == 0 {
				i++
				break
			}
			i++
		}
		if i >= len(src) {
			break
		}
		if src[i] != '{' {
			skipWSComments(src, &i)
			continue
		}
		selText := strings.TrimSpace(src[start:i])
		body, next, ok := scanBlock(src, i)
		if !ok {
			break
		}
		i = next
		if maxCSSRuleBody > 0 && len(body) > maxCSSRuleBody {
			skipWSComments(src, &i)
			continue
		}
		decls := ParseDeclarations(body)
		for _, part := range splitTopLevel(selText, ',') {
			if maxCSSRules > 0 && st.rules >= maxCSSRules {
				break
			}
			part = strings.TrimSpace(part)
			if part == "" || len(part) > 4096 {
				continue
			}
			sel := st.cache[part]
			if sel == nil {
				parsed, err := ParseSelector(part)
				if err != nil {
					continue
				}
				sel = parsed
				st.cache[part] = sel
			}
			st.order++
			st.rules++
			rules = append(rules, Rule{Selector: *sel, Decls: decls, Order: st.order, Media: media})
		}
		skipWSComments(src, &i)
	}
	return rules
}

// mediaMatches reports whether a comma-separated media query list applies to
// a screen viewport of viewportPx CSS pixels. A non-positive width means the
// viewport is unknown; only print-only rules are excluded then.
func mediaMatches(query string, viewportPx int) bool {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return true
	}
	for _, part := range splitTopLevel(query, ',') {
		if mediaQueryMatches(strings.TrimSpace(part), viewportPx) {
			return true
		}
	}
	return false
}

func mediaQueryMatches(q string, vw int) bool {
	if q == "" {
		return true
	}
	neg := false
	if strings.HasPrefix(q, "not ") {
		neg = true
		q = strings.TrimSpace(q[4:])
	}
	if strings.HasPrefix(q, "only ") {
		q = strings.TrimSpace(q[5:])
	}
	match := true
	for _, term := range strings.Split(q, " and ") {
		term = strings.TrimSpace(term)
		switch term {
		case "", "all", "screen":
		case "print", "speech":
			match = false
		default:
			if strings.HasPrefix(term, "(") && strings.HasSuffix(term, ")") {
				if !mediaFeatureMatches(term[1:len(term)-1], vw) {
					match = false
				}
			} else {
				match = false
			}
		}
	}
	if neg {
		return !match
	}
	return match
}

func mediaFeatureMatches(f string, vw int) bool {
	f = strings.TrimSpace(f)
	if k, v, ok := strings.Cut(f, ":"); ok {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if vw <= 0 {
			return true
		}
		switch k {
		case "min-width", "min-device-width":
			return vw >= mediaPx(v)
		case "max-width", "max-device-width":
			return vw <= mediaPx(v)
		case "width", "device-width":
			return vw == mediaPx(v)
		case "prefers-color-scheme":
			return v == "light" || v == "no-preference"
		case "orientation", "hover", "any-hover", "pointer", "any-pointer",
			"min-height", "max-height", "height", "min-resolution",
			"max-resolution", "resolution":
			return true
		}
		return false
	}
	switch f {
	case "color", "monochrome", "grid":
		return true
	}
	return false
}

func mediaPx(v string) int {
	if l := parseLength(v, 16); l.Set {
		return int(l.cssPx(16))
	}
	return 0
}

func scanBlock(s string, open int) (body string, next int, ok bool) {
	if open >= len(s) || s[open] != '{' {
		return "", open, false
	}
	depth := 1
	i := open + 1
	start := i
	for i < len(s) {
		switch s[i] {
		case '"', '\'':
			i = skipString(s, i)
			continue
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start:i], i + 1, true
			}
		}
		i++
	}
	return "", len(s), false
}

func skipString(s string, i int) int {
	q := s[i]
	i++
	for i < len(s) {
		if s[i] == '\\' {
			i += 2
			continue
		}
		if s[i] == q {
			return i + 1
		}
		i++
	}
	return len(s)
}

func skipWSComments(s string, i *int) {
	for *i < len(s) {
		c := s[*i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' {
			*i++
			continue
		}
		if c == '/' && *i+1 < len(s) && s[*i+1] == '*' {
			if j := strings.Index(s[*i+2:], "*/"); j >= 0 {
				*i += j + 4
			} else {
				*i = len(s)
			}
			continue
		}
		break
	}
}

func isCSSIdentChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '\\'
}

// splitTopLevel splits on sep, ignoring separators inside quotes, brackets and
// parentheses.
func splitTopLevel(s string, sep byte) []string {
	var out []string
	start := 0
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\'':
			i = skipString(s, i) - 1
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		default:
			if s[i] == sep && depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

func indexTopLevel(s string, sep byte) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\'':
			i = skipString(s, i) - 1
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		default:
			if s[i] == sep && depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitFields splits a CSS value on whitespace at the top level.
func splitFields(s string) []string {
	var out []string
	var b strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"', '\'':
			j := skipString(s, i)
			b.WriteString(s[i:j])
			i = j - 1
			continue
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
		if (c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f') && depth == 0 {
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
			continue
		}
		b.WriteByte(c)
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
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

func parseCompound(s string) (SelPart, [3]int, int, bool) {
	var p SelPart
	var spec [3]int
	i := 0
	if i < len(s) && s[i] == '*' {
		p.Tag = "*"
		i++
	} else if i < len(s) && isCSSIdentStart(s[i]) {
		j := i
		for j < len(s) && isCSSIdentChar(s[j]) {
			j++
		}
		p.Tag = strings.ToLower(s[i:j])
		spec[2]++
		i = j
	}
	for i < len(s) {
		switch s[i] {
		case '#':
			j := i + 1
			for j < len(s) && isCSSIdentChar(s[j]) {
				j++
			}
			if j == i+1 {
				return p, spec, 0, false
			}
			p.ID = s[i+1 : j]
			spec[0]++
			i = j
		case '.':
			j := i + 1
			for j < len(s) && isCSSIdentChar(s[j]) {
				j++
			}
			if j == i+1 {
				return p, spec, 0, false
			}
			p.Classes = append(p.Classes, s[i+1:j])
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
			// ::before/::after cannot match because we do not generate content.
			if strings.HasPrefix(s[nameStart:], ":") || name == "before" || name == "after" {
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
	return matchPart(s.Parts, len(s.Parts)-1, n)
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
	case "never", "hover", "focus", "active", "visited", "target":
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
