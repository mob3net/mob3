package browser

import (
	"context"
	"time"

	"browser/internal/engine"
)

// Temporary engine aliases. They keep the browser, layout and JS packages
// compiling while the DOM/style layer moves into internal/engine.

type Node = engine.Node
type NodeType = engine.NodeType
type Attr = engine.Attr
type Style = engine.Style
type Rule = engine.Rule
type Declaration = engine.Declaration
type Selector = engine.Selector
type StyleEngine = engine.StyleEngine
type styleEngine = engine.StyleEngine

const (
	DocumentNode = engine.DocumentNode
	ElementNode  = engine.ElementNode
	TextNode     = engine.TextNode
)

func ParseHTML(src string) *Node                { return engine.ParseHTML(src) }
func Unescape(s string) string                  { return engine.Unescape(s) }
func ParseStylesheet(css string) []Rule         { return engine.ParseStylesheet(css) }
func ParseSelector(s string) (*Selector, error) { return engine.ParseSelector(s) }

func defaultStyle() *Style                     { return engine.DefaultStyle() }
func bigTextScale(st *Style, zoom float64) int { return engine.BigTextScale(st, zoom) }

// newStyleEngine/newStyleEngineDeadline keep the browser layout and the
// engine tests' call sites stable during the move.
var maxAuthorRules = engine.MaxAuthorRules

func ParseDeclarations(s string) []Declaration { return engine.ParseDeclarations(s) }

func newStyleEngine(author []Rule, viewportPx int) *StyleEngine {
	return engine.NewStyleEngine(author, viewportPx)
}
func newStyleEngineDeadline(author []Rule, viewportPx int, deadline time.Time) *StyleEngine {
	return engine.NewStyleEngineDeadline(author, viewportPx, deadline)
}

func rulesUseHover(rules []Rule) bool { return engine.RulesUseHover(rules) }
func elementIndex(n *Node) (int, int) { return engine.ElementIndex(n) }

func newLayerRegistry() *engine.LayerRegistry                { return engine.NewLayerRegistry() }
func cssImports(src string) []engine.CSSImport               { return engine.CSSImports(src) }
func declareCSSLayers(src string, reg *engine.LayerRegistry) { engine.DeclareCSSLayers(src, reg) }
func parseStylesheetWithLayers(ctx context.Context, src string, layers *engine.LayerRegistry) []Rule {
	return engine.ParseStylesheetWithLayers(ctx, src, layers)
}
func mediaMatches(query string, viewportPx int, dark bool) bool {
	return engine.MediaMatches(query, viewportPx, dark)
}

func mediaFeatureMatches(feature string, viewportPx int, dark bool) bool {
	return engine.MediaFeatureMatches(feature, viewportPx, dark)
}

const maxParseDepth = engine.MaxParseDepth

func isPrivateUseRune(r rune) bool { return engine.IsPrivateUseRune(r) }

const maxTextScale = engine.MaxTextScale

func applyDecl(st *Style, prop, val string, parent *Style) { engine.ApplyDecl(st, prop, val, parent) }
func applyDecls(st *Style, decls []Declaration, parent *Style, important bool, fontBase float64) {
	engine.ApplyDecls(st, decls, parent, important, fontBase)
}
