package browser

import (
	"context"
	"testing"
)

func TestKeyframesSkipped(t *testing.T) {
	rules := ParseStylesheet(`@keyframes x { 0% { opacity: 0 } to { opacity: 1 } } a { color: red }`)
	if len(rules) != 1 || rules[0].Selector.Text != "a" {
		t.Fatalf("rules = %+v", rules)
	}
}

func TestStylesheetContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if rules := ParseStylesheetContext(ctx, `a{x:1} b{x:2}`); len(rules) != 0 {
		t.Fatalf("canceled parse kept %d rules", len(rules))
	}
}

func TestStylesheetBudgets(t *testing.T) {
	oldBytes, oldRules := maxCSSBytes, maxCSSRules
	defer func() {
		maxCSSBytes, maxCSSRules = oldBytes, oldRules
	}()

	maxCSSRules = 2
	rules := ParseStylesheet(`a{x:1} b{x:2} c{x:3}`)
	if len(rules) != 2 {
		t.Fatalf("rule budget kept %d rules, want 2", len(rules))
	}

	maxCSSBytes = 8
	if rules := ParseStylesheet(`a{color:red}`); len(rules) != 0 {
		t.Fatalf("byte budget kept %d rules from truncated CSS", len(rules))
	}
	maxCSSBytes = oldBytes
	if rules := ParseStylesheet(`a{color:red}`); len(rules) != 1 {
		t.Fatalf("normal parse kept %d rules, want 1", len(rules))
	}
}

func TestCSSImportsParsing(t *testing.T) {
	src := `/* @import "ignored.css"; */
@import url("a.css");
@import 'b.css' screen and (max-width: 600px);
p { content: "@import nope.css"; }
@media screen { .x { color: red } }
@import "c.css" print;`
	got := cssImports(src)
	if len(got) != 3 {
		t.Fatalf("imports = %+v, want 3", got)
	}
	if got[0].href != "a.css" || got[0].media != "" {
		t.Fatalf("first import = %+v", got[0])
	}
	if got[1].href != "b.css" || got[1].media != "screen and (max-width: 600px)" {
		t.Fatalf("second import = %+v", got[1])
	}
	if got[2].href != "c.css" || got[2].media != "print" {
		t.Fatalf("third import = %+v", got[2])
	}
}

func TestImportURLWithSemicolons(t *testing.T) {
	src := `@import url("https://fonts.example/css2?family=Open+Sans:ital,wght@0,300;0,400;0,700&display=swap");` +
		`.after { color: red }`
	imports := cssImports(src)
	if len(imports) != 1 {
		t.Fatalf("imports = %+v, want 1", imports)
	}
	if imports[0].href != "https://fonts.example/css2?family=Open+Sans:ital,wght@0,300;0,400;0,700&display=swap" {
		t.Fatalf("import href = %q", imports[0].href)
	}
	rules := ParseStylesheet(src)
	if len(rules) != 1 {
		t.Fatalf("rules after @import = %d, want 1", len(rules))
	}
}
