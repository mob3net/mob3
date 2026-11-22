package engine

import (
	"math"
	"sort"
	"time"
)

type styleEngine struct {
	ua         []Rule
	author     []Rule
	viewportPx int
	viewportH  float64
	// rootFontPx is the font size of the root element, used to resolve rem
	// lengths. It starts at the initial 16px and follows html's font-size.
	rootFontPx float64
	dark       bool
	deadline   time.Time
	media      map[string]bool
	expired    bool
	// Rules are indexed by their rightmost compound so each element only
	// scans plausible candidates instead of the whole stylesheet.
	authorIndex  *ruleIndex
	authorBefore []Rule
	authorAfter  []Rule
	beforeIndex  *ruleIndex
	afterIndex   *ruleIndex
}

// StyleEngine is the exported name of the cascade engine.
type StyleEngine = styleEngine

// ruleIndex buckets rules by the most specific key of their rightmost
// compound. A rule lives in exactly one bucket, and every element queries all
// buckets that could contain a matching selector.
type ruleIndex struct {
	byID      map[string][]Rule
	byClass   map[string][]Rule
	byTag     map[string][]Rule
	universal []Rule
}

func indexRules(rules []Rule) *ruleIndex {
	idx := &ruleIndex{
		byID:    make(map[string][]Rule),
		byClass: make(map[string][]Rule),
		byTag:   make(map[string][]Rule),
	}
	for _, r := range rules {
		if len(r.Selector.Parts) == 0 {
			idx.universal = append(idx.universal, r)
			continue
		}
		p := r.Selector.Parts[len(r.Selector.Parts)-1]
		switch {
		case p.ID != "":
			idx.byID[p.ID] = append(idx.byID[p.ID], r)
		case len(p.Classes) > 0:
			idx.byClass[p.Classes[0]] = append(idx.byClass[p.Classes[0]], r)
		case p.Tag != "" && p.Tag != "*":
			idx.byTag[p.Tag] = append(idx.byTag[p.Tag], r)
		default:
			idx.universal = append(idx.universal, r)
		}
	}
	return idx
}

// candidates collects the buckets an element could match.
func (idx *ruleIndex) candidates(n *Node) []Rule {
	if idx == nil {
		return nil
	}
	out := make([]Rule, 0, len(idx.universal)+len(idx.byTag[n.Tag]))
	out = append(out, idx.universal...)
	out = append(out, idx.byTag[n.Tag]...)
	if id := n.Attr("id"); id != "" {
		out = append(out, idx.byID[id]...)
	}
	classes := n.ClassList()
	for i, c := range classes {
		seen := false
		for j := 0; j < i; j++ {
			if classes[j] == c {
				seen = true
				break
			}
		}
		if !seen {
			out = append(out, idx.byClass[c]...)
		}
	}
	return out
}

func newStyleEngine(author []Rule, viewportPx int) *styleEngine {
	e := &styleEngine{ua: uaRules, author: author, viewportPx: viewportPx, rootFontPx: defaultStyle().FontSize}
	e.viewportH = 24 * cellH
	e.authorIndex = indexRules(filterNormalRules(author))
	e.authorBefore = filterPseudoRules(author, "before")
	e.authorAfter = filterPseudoRules(author, "after")
	e.beforeIndex = indexRules(e.authorBefore)
	e.afterIndex = indexRules(e.authorAfter)
	return e
}

func filterNormalRules(rules []Rule) []Rule {
	out := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if len(r.Selector.Parts) == 0 {
			out = append(out, r)
			continue
		}
		if r.Selector.Parts[len(r.Selector.Parts)-1].Pseudo == "" {
			out = append(out, r)
		}
	}
	return out
}

func filterPseudoRules(rules []Rule, pseudo string) []Rule {
	var out []Rule
	for _, r := range rules {
		if len(r.Selector.Parts) == 0 {
			continue
		}
		if r.Selector.Parts[len(r.Selector.Parts)-1].Pseudo == pseudo {
			out = append(out, r)
		}
	}
	return out
}

func newStyleEngineDeadline(author []Rule, viewportPx int, deadline time.Time) *styleEngine {
	e := newStyleEngine(author, viewportPx)
	e.deadline = deadline
	return e
}

// outOfTime reports whether the style pass has exceeded its deadline. Once
// expired, the remaining tree is only given inherited styles so layout can
// still produce a partial page without doing unbounded selector work.
func (e *styleEngine) outOfTime() bool {
	if e.expired {
		return true
	}
	if !e.deadline.IsZero() && time.Now().After(e.deadline) {
		e.expired = true
	}
	return e.expired
}

// mediaMatches caches media query evaluation for the duration of one style
// pass. The same handful of queries is otherwise re-parsed for every element
// and rule.
func (e *styleEngine) mediaMatches(query string) bool {
	if query == "" {
		return true
	}
	if v, ok := e.media[query]; ok {
		return v
	}
	v := mediaMatches(query, e.viewportPx, e.dark)
	if e.media == nil {
		e.media = make(map[string]bool)
	}
	e.media[query] = v
	return v
}

func (e *styleEngine) computeAll(root *Node) map[*Node]*Style {
	styles := make(map[*Node]*Style)
	base := defaultStyle()
	var walk func(n *Node, parent *Style)
	walk = func(n *Node, parent *Style) {
		if n.Type == DocumentNode {
			for _, c := range n.Children {
				walk(c, base)
			}
			return
		}
		if n.Type == TextNode {
			if parent != nil {
				n.Style = parent
			}
			return
		}
		st := e.nodeStyle(n, parent)
		styles[n] = st
		if len(e.authorBefore) > 0 {
			n.Before = e.computePseudo(n, st, "before")
		}
		if len(e.authorAfter) > 0 {
			n.After = e.computePseudo(n, st, "after")
		}
		closedDetails := n.Tag == "details" && !n.HasAttr("open")
		for _, c := range n.Children {
			walk(c, st)
			// A closed <details> only renders its summary. The rest of the
			// content still exists in the DOM, but must not take layout space.
			if closedDetails && c.Type == ElementNode && c.Tag != "summary" && c.Style != nil {
				c.Style.Display = "none"
			}
		}
	}
	walk(root, base)
	return styles
}

func (e *styleEngine) applyMatching(rules []Rule, n *Node, st *Style, parent *Style) {
	var matched []Rule
	for i, r := range rules {
		if i&63 == 0 && e.outOfTime() {
			break
		}
		if r.Media != "" && !e.mediaMatches(r.Media) {
			continue
		}
		if r.Selector.Match(n) {
			matched = append(matched, r)
		}
	}
	e.applyMatched(matched, st, parent)
}

func (e *styleEngine) applyMatchingIndex(idx *ruleIndex, n *Node, st *Style, parent *Style) {
	var matched []Rule
	rules := idx.candidates(n)
	for i, r := range rules {
		if i&63 == 0 && e.outOfTime() {
			break
		}
		if r.Media != "" && !e.mediaMatches(r.Media) {
			continue
		}
		if r.Selector.Match(n) {
			matched = append(matched, r)
		}
	}
	e.applyMatched(matched, st, parent)
}

// computePseudo builds a generated ::before/::after box when its rule sets
// content. An empty string produces the aspect-ratio box; a literal string
// carries text; functional values were already rejected while parsing.
func (e *styleEngine) computePseudo(n *Node, parent *Style, pseudo string) *Style {
	idx := e.beforeIndex
	if pseudo == "after" {
		idx = e.afterIndex
	}
	matched := e.matchingPseudo(idx, n, pseudo)
	if len(matched) == 0 {
		return nil
	}
	st := inheritStyle(parent)
	e.applyMatched(matched, st, parent)
	e.resolveStyleUnits(st)
	if !st.ContentSet || st.Display == "none" || st.Hidden {
		return nil
	}
	return st
}

func (e *styleEngine) matchingPseudo(idx *ruleIndex, n *Node, pseudo string) []Rule {
	var matched []Rule
	rules := idx.candidates(n)
	for i, r := range rules {
		if i&63 == 0 && e.outOfTime() {
			break
		}
		if r.Media != "" && !e.mediaMatches(r.Media) {
			continue
		}
		if r.Selector.MatchPseudo(n, pseudo) {
			matched = append(matched, r)
		}
	}
	return matched
}

func (e *styleEngine) applyMatched(matched []Rule, st, parent *Style) {
	if len(matched) == 0 {
		return
	}
	sortMatched := func(important bool) {
		sort.SliceStable(matched, func(i, j int) bool {
			a, b := matched[i], matched[j]
			if la, lb := layerPriority(a.Layer, important), layerPriority(b.Layer, important); la != lb {
				return la < lb
			}
			for k := 0; k < 3; k++ {
				if a.Selector.Spec[k] != b.Selector.Spec[k] {
					return a.Selector.Spec[k] < b.Selector.Spec[k]
				}
			}
			return a.Order < b.Order
		})
	}
	base := parentSize(parent)
	sortMatched(false)
	for _, r := range matched {
		applyDecls(st, r.Decls, parent, false, base)
	}
	// Important declarations reverse the layer order: an earlier layer beats a
	// later one, and every layer beats unlayered important declarations.
	sortMatched(true)
	for _, r := range matched {
		applyDecls(st, r.Decls, parent, true, base)
	}
}

// layerPriority orders rules for the cascade. Higher values win; the slice is
// sorted ascending and applied in order, so the highest priority is last.
func layerPriority(layer int, important bool) int {
	if important {
		if layer == 0 {
			return math.MinInt
		}
		return -layer
	}
	if layer == 0 {
		return math.MaxInt
	}
	return layer
}

// nodeStyle computes one element's style against its parent's, exactly as the
// full pass does. A node with no inheritance payload bypasses the cascade when
// the style pass is out of time, matching computeAll.
func (e *styleEngine) nodeStyle(n *Node, parent *Style) *Style {
	st := inheritStyle(parent)
	if !e.outOfTime() {
		e.applyMatching(e.ua, n, st, parent)
		ApplyPresentationAttrs(n, st)
		e.applyMatchingIndex(e.authorIndex, n, st, parent)
		if inline := n.Attr("style"); inline != "" {
			decls := ParseDeclarations(inline)
			applyDecls(st, decls, parent, false, parentSize(parent))
			applyDecls(st, decls, parent, true, parentSize(parent))
		}
	}
	e.resolveStyleUnits(st)
	if n.Tag == "html" && st.FontSize > 0 {
		// rem on the root element itself uses the initial font size; rem in
		// the rest of the document follows the root's computed size.
		e.rootFontPx = st.FontSize
	}
	if clipHidesContent(st.Clip) {
		st.Display = "none"
	}
	// Common screen-reader-only pattern: a 1px clipped box. Rendering it
	// would produce one character per line, so hide it.
	if st.Overflow == "hidden" && st.Width.Set && st.Width.U == UnitPX && st.Width.V <= 1 &&
		st.Height.Set && st.Height.U == UnitPX && st.Height.V <= 1 {
		st.Display = "none"
	}
	if (n.Tag == "body" || n.Tag == "html") && st.Hidden {
		// Treat body/html visibility:hidden as the common anti-FOUC guard
		// that scripts remove; the rest of the document is already being
		// rendered, so inheriting it would only hide fixed chrome.
		st.Hidden = false
	}
	n.Style = st
	return st
}

// computeNode computes the style of one element on demand, walking from the
// document root down so inherited values and ancestor selectors match a full
// pass. It is used when script measurement or getComputedStyle asks about an
// element created after the last layout.
func (e *styleEngine) computeNode(n *Node) *Style {
	if n == nil {
		return nil
	}
	if n.Type == TextNode {
		if n.Parent != nil {
			return n.Parent.Style
		}
		return nil
	}
	var chain []*Node
	for x := n; x != nil && x.Type != DocumentNode; x = x.Parent {
		chain = append(chain, x)
	}
	parent := defaultStyle()
	for i := len(chain) - 1; i >= 0; i-- {
		parent = e.nodeStyle(chain[i], parent)
	}
	return n.Style
}

// NewStyleEngine builds the cascade engine for one layout pass.
func NewStyleEngine(author []Rule, viewportPx int) *StyleEngine {
	return newStyleEngine(author, viewportPx)
}

// NewStyleEngineDeadline is NewStyleEngine with a cooperative deadline.
func NewStyleEngineDeadline(author []Rule, viewportPx int, deadline time.Time) *StyleEngine {
	return newStyleEngineDeadline(author, viewportPx, deadline)
}

// DefaultStyle returns the style an element starts from.
func DefaultStyle() *Style { return defaultStyle() }

// SetViewportHeight sets the CSS-pixel viewport height for this pass.
func (e *StyleEngine) SetViewportHeight(h float64) { e.viewportH = h }

// SetDark selects the dark color-scheme media query.
func (e *StyleEngine) SetDark(dark bool) { e.dark = dark }

// ComputeAll computes styles for the whole tree.
func (e *StyleEngine) ComputeAll(root *Node) map[*Node]*Style { return e.computeAll(root) }

// ComputeNode computes one element on demand.
func (e *StyleEngine) ComputeNode(n *Node) *Style { return e.computeNode(n) }

// Expired reports whether the style/layout deadline was reached.
func (e *StyleEngine) Expired() bool { return e.expired }
