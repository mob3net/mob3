package engine

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

// ParseDeclarations parses the body of a style attribute or rule block.
func ParseDeclarations(s string) []Declaration {
	s = stripCSSComments(s)
	var out []Declaration
	for _, chunk := range SplitTopLevel(s, ';') {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		idx := indexTopLevel(chunk, ':')
		if idx < 0 {
			continue
		}
		rawProp := strings.TrimSpace(chunk[:idx])
		prop := strings.ToLower(rawProp)
		if strings.HasPrefix(rawProp, "--") {
			prop = rawProp
		}
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

// scanCSSStatement returns the index of the first top-level ;, { or } at or
// after i, ignoring semicolons and braces inside strings and parentheses.
// Data URLs and font URLs commonly contain semicolons.
func scanCSSStatement(src string, i int) int {
	paren := 0
	var quote byte
	for i < len(src) {
		c := src[i]
		if quote != 0 {
			if c == '\\' {
				i += 2
				continue
			}
			if c == quote {
				quote = 0
			}
			i++
			continue
		}
		switch c {
		case '\\':
			// An escape is part of the identifier, not a statement boundary.
			i += 2
			continue
		case '"', '\'':
			quote = c
		case '(':
			paren++
		case ')':
			if paren > 0 {
				paren--
			}
		case ';', '{', '}':
			if paren == 0 {
				return i
			}
		}
		i++
	}
	return i
}

// cssImport is one @import rule: a URL and an optional media condition.
type cssImport struct {
	Href  string
	Media string
}

// cssImports extracts top-level @import rules without parsing the rest of the
// sheet. Comments, strings and nested blocks are skipped so @import text in
// values or media queries is not mistaken for a rule.
func cssImports(src string) []cssImport {
	var out []cssImport
	i, depth := 0, 0
	for i < len(src) {
		if strings.HasPrefix(src[i:], "/*") {
			j := strings.Index(src[i+2:], "*/")
			if j < 0 {
				break
			}
			i += 2 + j + 2
			continue
		}
		c := src[i]
		if c == '"' || c == '\'' {
			q := c
			i++
			for i < len(src) && src[i] != q {
				if src[i] == '\\' {
					i++
				}
				i++
			}
			i++
			continue
		}
		if c == '{' {
			depth++
			i++
			continue
		}
		if c == '}' {
			if depth > 0 {
				depth--
			}
			i++
			continue
		}
		// Compare a fixed-length ASCII prefix. Lowercasing the whole remaining
		// stylesheet here was quadratic and made a multi-megabyte sheet take
		// tens of seconds to scan.
		if depth == 0 && (c == '@' || c == 'A') && len(src)-i >= len("@import") &&
			strings.EqualFold(src[i:i+len("@import")], "@import") {
			j := i + len("@import")
			if j < len(src) {
				n := src[j]
				if n == ' ' || n == '\t' || n == '\n' || n == '\r' || n == '"' || n == '\'' || n == 'u' || n == 'U' {
					end := scanCSSStatement(src, j)
					if href, media := parseImportSpec(src[j:end]); href != "" {
						out = append(out, cssImport{Href: href, Media: media})
					}
					i = end
					if i < len(src) && src[i] == ';' {
						i++
					}
					continue
				}
			}
		}
		i++
	}
	return out
}

func parseImportSpec(spec string) (href, media string) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", ""
	}
	if strings.HasPrefix(strings.ToLower(spec), "url(") {
		inner := spec[4:]
		if j := strings.IndexByte(inner, ')'); j >= 0 {
			href = strings.Trim(strings.TrimSpace(inner[:j]), "\"'")
			spec = strings.TrimSpace(inner[j+1:])
		}
	} else if spec[0] == '"' || spec[0] == '\'' {
		q := spec[0]
		if j := strings.IndexByte(spec[1:], q); j >= 0 {
			href = spec[1 : 1+j]
			spec = strings.TrimSpace(spec[2+j:])
		}
	} else {
		fields := strings.Fields(spec)
		if len(fields) > 0 {
			href = fields[0]
			spec = strings.TrimSpace(strings.TrimPrefix(spec, fields[0]))
		}
	}
	if href == "" {
		return "", ""
	}
	// Ignore unsupported @import modifiers, keeping any real media condition.
	for {
		low := strings.ToLower(spec)
		if strings.HasPrefix(low, "layer(") || strings.HasPrefix(low, "supports(") {
			if j := strings.IndexByte(spec, ')'); j >= 0 {
				spec = strings.TrimSpace(spec[j+1:])
				continue
			}
		}
		break
	}
	return href, strings.TrimSpace(spec)
}

// Stylesheet parsing budgets. These keep hostile or generated multi-megabyte
// CSS from burning CPU and GC time during a page load.
var (
	maxCSSBytes     = 4 << 20
	maxCSSRules     = 50000
	maxCSSRuleBody  = 256 << 10
	maxCSSParseTime = time.Second
	// maxAuthorRules bounds the accumulated document stylesheet, including
	// rules a script keeps injecting through new or rewritten <style> tags.
	maxAuthorRules = 100000
)

type parseState struct {
	order  int
	rules  int
	cache  map[string]*Selector
	ctx    context.Context
	start  time.Time
	layers *layerRegistry
	layer  int    // current @layer rank, 0 when unlayered
	name   string // current layer name for nested @layer blocks
}

// layerRegistry assigns cascade layers a stable rank in declaration order.
// Later-declared layers take precedence over earlier ones, while unlayered
// rules (rank 0) beat every layer, as the cascade specifies.
type layerRegistry struct {
	ranks map[string]int
	next  int
	anon  int
}

func newLayerRegistry() *layerRegistry {
	return &layerRegistry{ranks: make(map[string]int)}
}

// declare registers a layer name and returns its rank. Ranks start at 1 so a
// zero Layer field can mean "unlayered".
func (r *layerRegistry) declare(name string) int {
	if rank, ok := r.ranks[name]; ok {
		return rank
	}
	r.next++
	r.ranks[name] = r.next
	return r.next
}

func (r *layerRegistry) anonymous() int {
	r.anon++
	return r.declare("\x00anon" + strconv.Itoa(r.anon))
}

// layerFullName joins a nested layer name onto its parent layer.
func layerFullName(parent, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if parent == "" {
		return name
	}
	return parent + "." + name
}

// declareCSSLayers pre-registers every layer a sheet mentions so a document's
// layer order follows source order across stylesheets, not the order in which
// rules happen to be parsed.
func declareCSSLayers(src string, reg *layerRegistry) {
	i := 0
	for i < len(src) {
		switch {
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '*':
			i = skipComment(src, i)
		case src[i] == '"' || src[i] == '\'':
			i = skipString(src, i)
		case src[i] == '@' && len(src)-i >= len("@layer") && strings.EqualFold(src[i:i+len("@layer")], "@layer"):
			j := i + len("@layer")
			k := scanCSSStatement(src, j)
			prelude := strings.TrimSpace(src[j:k])
			if k < len(src) && src[k] == ';' {
				for _, part := range SplitTopLevel(prelude, ',') {
					if name := strings.TrimSpace(part); name != "" {
						reg.declare(name)
					}
				}
			} else if name := strings.Fields(prelude); len(name) > 0 {
				reg.declare(name[0])
			}
			i = k
		default:
			i++
		}
	}
}

// ParseStylesheet parses a stylesheet into rules, preserving source order.
func ParseStylesheet(src string) []Rule {
	return ParseStylesheetContext(context.Background(), src)
}

// ParseStylesheetContext is ParseStylesheet with cancellation support.
func ParseStylesheetContext(ctx context.Context, src string) []Rule {
	return parseStylesheetWithLayers(ctx, src, nil)
}

// parseStylesheetWithLayers parses a sheet, sharing a layer registry with the
// caller when one is supplied so every sheet in a document agrees on the layer
// order. A nil registry starts a fresh one.
func parseStylesheetWithLayers(ctx context.Context, src string, layers *layerRegistry) []Rule {
	if maxCSSBytes > 0 && len(src) > maxCSSBytes {
		src = src[:maxCSSBytes]
	}
	if layers == nil {
		layers = newLayerRegistry()
	}
	st := &parseState{cache: make(map[string]*Selector), ctx: ctx, start: time.Now(), layers: layers}
	return parseRules(src, st, "")
}

// maxNestedDepth bounds CSS nesting so a malformed sheet cannot expand
// recursively without limit.
const maxNestedDepth = 8

// addStyleRules appends the rules for one style rule, expanding CSS nesting: a
// declaration block may contain nested style rules whose selectors resolve
// against the parent. Nested at-rules are skipped.
func addStyleRules(st *parseState, rules *[]Rule, selText, body, media string, depth int) {
	if depth > maxNestedDepth {
		return
	}
	declText, nested := splitNestedRule(body)
	if strings.TrimSpace(declText) != "" {
		if decls := ParseDeclarations(declText); len(decls) > 0 {
			addDeclRules(st, rules, selText, decls, media)
		}
	}
	for _, nr := range nested {
		child := strings.TrimSpace(nr[0])
		if child == "" || strings.HasPrefix(child, "@") {
			continue
		}
		composed := composeNestedSelector(selText, child)
		if composed == "" {
			continue
		}
		addStyleRules(st, rules, composed, nr[1], media, depth+1)
	}
}

func addDeclRules(st *parseState, rules *[]Rule, selText string, decls []Declaration, media string) {
	for _, part := range SplitTopLevel(selText, ',') {
		if maxCSSRules > 0 && st.rules >= maxCSSRules {
			return
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
		*rules = append(*rules, Rule{Selector: *sel, Decls: decls, Order: st.order, Media: media, Layer: st.layer})
	}
}

// composeNestedSelector resolves a nested selector list against its parent
// list: "&" is replaced by the parent, a leading combinator is appended, and
// any other selector becomes a descendant.
func composeNestedSelector(parents, children string) string {
	plist := SplitTopLevel(parents, ',')
	clist := SplitTopLevel(children, ',')
	if len(plist)*len(clist) > 256 {
		return ""
	}
	var out []string
	for _, p := range plist {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		for _, c := range clist {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			if strings.Contains(c, "&") {
				out = append(out, strings.ReplaceAll(c, "&", p))
				continue
			}
			out = append(out, p+" "+c)
		}
	}
	return strings.Join(out, ", ")
}

// splitNestedRule separates a declaration block's own declarations from nested
// style rules, which modern CSS nesting writes as a selector followed by
// another block. Without this, inner declarations leaked onto the outer
// selector; a theme's "a::after { width:0 }" underline effect used to give
// every heading a zero width.
func splitNestedRule(body string) (string, [][2]string) {
	var decls []string
	var nested [][2]string
	i, seg := 0, 0
	for i < len(body) {
		switch c := body[i]; {
		case c == '"' || c == '\'':
			i = skipString(body, i)
		case c == '/' && i+1 < len(body) && body[i+1] == '*':
			i = skipComment(body, i)
		case c == '(' || c == '[':
			i = skipGroup(body, i)
		case c == '{':
			selStart := i
			for selStart > seg && body[selStart-1] != ';' {
				selStart--
			}
			if selStart > seg {
				decls = append(decls, body[seg:selStart])
			}
			end, ok := matchingBrace(body, i)
			if !ok {
				return strings.Join(decls, ";"), nested
			}
			if child := strings.TrimSpace(body[selStart:i]); child != "" {
				nested = append(nested, [2]string{child, body[i+1 : end]})
			}
			i = end + 1
			seg = i
		default:
			i++
		}
	}
	if seg < len(body) {
		decls = append(decls, body[seg:])
	}
	return strings.Join(decls, ";"), nested
}

func matchingBrace(s string, open int) (int, bool) {
	depth := 0
	for i := open; i < len(s); {
		switch s[i] {
		case '\\':
			i += 2
			continue
		case '"', '\'':
			i = skipString(s, i)
			continue
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
		i++
	}
	return 0, false
}

func skipComment(s string, i int) int {
	if end := strings.Index(s[i+2:], "*/"); end >= 0 {
		return i + 2 + end + 2
	}
	return len(s)
}

func skipGroup(s string, i int) int {
	open := s[i]
	closeCh := byte(')')
	if open == '[' {
		closeCh = ']'
	}
	depth := 0
	for i < len(s) {
		switch c := s[i]; {
		case c == '"' || c == '\'':
			i = skipString(s, i)
			continue
		case c == open:
			depth++
		case c == closeCh:
			depth--
			if depth == 0 {
				return i + 1
			}
		}
		i++
	}
	return len(s)
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
			j = scanCSSStatement(src, j)
			if j >= len(src) {
				break
			}
			if src[j] == ';' {
				if name == "layer" {
					// @layer a, b; declares names without adding rules.
					for _, part := range SplitTopLevel(strings.TrimSpace(src[preludeStart:j]), ',') {
						if n := layerFullName(st.name, strings.TrimSpace(part)); n != "" {
							st.layers.declare(n)
						}
					}
				}
				i = j + 1
			} else {
				body, next, ok := scanBlock(src, j)
				if !ok {
					break
				}
				inner := media
				switch name {
				case "layer":
					prelude := strings.TrimSpace(src[preludeStart:j])
					prevRank, prevName := st.layer, st.name
					if prelude == "" {
						st.layer = st.layers.anonymous()
					} else {
						if full := layerFullName(st.name, prelude); full != "" {
							st.name = full
						}
						st.layer = st.layers.declare(st.name)
					}
					rules = append(rules, parseRules(body, st, inner)...)
					st.layer, st.name = prevRank, prevName
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
			if c == '\\' {
				// A backslash escapes the next byte in a CSS identifier, so an
				// escaped quote (for example `.before\:content-\[\'\'\]`) must
				// not be read as the start of a string. Missing this made the
				// scanner consume the rest of the sheet looking for a closing
				// quote, dropping every later rule.
				i += 2
				continue
			}
			if c == '/' && i+1 < len(src) && src[i+1] == '*' {
				if j := strings.Index(src[i+2:], "*/"); j >= 0 {
					i += j + 4
					continue
				}
				break
			}
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
		addStyleRules(st, &rules, selText, body, media, 0)
		skipWSComments(src, &i)
	}
	return rules
}

func scanBlock(s string, open int) (body string, next int, ok bool) {
	if open >= len(s) || s[open] != '{' {
		return "", open, false
	}
	depth := 1
	i := open + 1
	start := i
	for i < len(s) {
		if s[i] == '/' && i+1 < len(s) && s[i+1] == '*' {
			// Comments are skipped here too: an apostrophe or a brace inside
			// one would otherwise start a phantom string or change the depth
			// and make the rest of the sheet unparseable.
			if j := strings.Index(s[i+2:], "*/"); j >= 0 {
				i += j + 4
				continue
			}
			return "", len(s), false
		}
		switch s[i] {
		case '\\':
			i += 2
			continue
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

// stripCSSComments removes /* ... */ comments outside strings, so a comment
// between declarations cannot merge them into one bogus property.
func stripCSSComments(s string) string {
	if !strings.Contains(s, "/*") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c == '"' || c == '\'' {
			j := skipString(s, i)
			b.WriteString(s[i:j])
			i = j
			continue
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '*' {
			if j := strings.Index(s[i+2:], "*/"); j >= 0 {
				i += j + 4
				continue
			}
			break
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// seedParsedStyles records the inline styles already collected at load, so the
// first dynamic-style scan does not parse their rules a second time.

// LayerRegistry holds @layer ranks for one document.
type LayerRegistry = layerRegistry

// NewLayerRegistry creates an empty layer registry.
func NewLayerRegistry() *LayerRegistry { return newLayerRegistry() }

// CSSImports returns the @import targets in a sheet.
func CSSImports(src string) []cssImport { return cssImports(src) }

// DeclareCSSLayers registers layer names in declaration order.
func DeclareCSSLayers(src string, reg *LayerRegistry) { declareCSSLayers(src, reg) }

// ParseStylesheetWithLayers parses a sheet after its layers are declared.
func ParseStylesheetWithLayers(ctx context.Context, src string, layers *LayerRegistry) []Rule {
	return parseStylesheetWithLayers(ctx, src, layers)
}

// MaxAuthorRules bounds the accumulated document stylesheet.
var MaxAuthorRules = maxAuthorRules

// CSSImport is one parsed @import target.
type CSSImport = cssImport
