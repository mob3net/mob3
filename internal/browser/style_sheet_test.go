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

func TestNestedRulesExpandAgainstParent(t *testing.T) {
	doc := ParseHTML(`<div class="card"><a id="a">link</a><span id="s">text</span></div>`)
	rules := ParseStylesheet(`
		.card {
			color: #111111;
			a { color: #0000ff; }
			> span { color: #ff0000; }
			&[data-kind='x'] a { color: #00ff00; }
		}
	`)
	styles := newStyleEngine(rules, 100).computeAll(doc)
	card := doc.FindFirst("div")
	link := findElementByID(doc, "a")
	span := findElementByID(doc, "s")
	if got := styles[card].Color; got != RGB(0x11, 0x11, 0x11) {
		t.Fatalf("card color = %+v", got)
	}
	if got := styles[link].Color; got != RGB(0x00, 0x00, 0xff) {
		t.Fatalf("descendant rule color = %+v", got)
	}
	if got := styles[span].Color; got != RGB(0xff, 0x00, 0x00) {
		t.Fatalf("child combinator rule color = %+v", got)
	}
}

func TestNestedRulesDoNotLeakIntoParent(t *testing.T) {
	// A theme underlines links with an absolutely positioned zero-width
	// ::after. Flattening the nested block used to give the heading itself
	// width:0 and display:inline-block, so its text wrapped one character per
	// line.
	doc := ParseHTML(`<h3 class="h"><a href="#">Heading</a></h3>`)
	rules := ParseStylesheet(`
		.h {
			color: #111111;
			a { display: inline-block; position: relative; }
			a::after { content: ''; position: absolute; width: 0; height: 1px; }
		}
	`)
	styles := newStyleEngine(rules, 100).computeAll(doc)
	h := doc.FindFirst("h3")
	if styles[h].Width.Set {
		t.Fatalf("nested rule leaked width onto the heading: %+v", styles[h].Width)
	}
	if styles[h].Display != "block" && styles[h].Display != "" {
		t.Fatalf("heading display = %q, want block", styles[h].Display)
	}
	link := h.FindFirst("a")
	if link == nil || styles[link].Display != "inline-block" {
		t.Fatalf("nested link rule did not apply")
	}
}
