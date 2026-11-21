package browser

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestKeyframesSkipped(t *testing.T) {
	rules := ParseStylesheet(`@keyframes x { 0% { opacity: 0 } to { opacity: 1 } } a { color: red }`)
	if len(rules) != 1 || rules[0].Selector.Text != "a" {
		t.Fatalf("rules = %+v", rules)
	}
}

func TestLayerRulesParsed(t *testing.T) {
	rules := ParseStylesheet(`
		@layer base { .a { color: #111111 } }
		.b { color: #222222 }
		@layer layout { .c { color: #333333 } }
		@layer { .d { color: #444444 } }
	`)
	if len(rules) != 4 {
		t.Fatalf("rules = %d, want 4: %+v", len(rules), rules)
	}
	if rules[0].Layer == 0 {
		t.Fatalf("rule inside @layer base has no layer rank")
	}
	if rules[1].Layer != 0 {
		t.Fatalf("unlayered rule got layer rank %d", rules[1].Layer)
	}
	if rules[2].Layer <= rules[0].Layer {
		t.Fatalf("later layer rank %d does not beat earlier %d", rules[2].Layer, rules[0].Layer)
	}
	if rules[3].Layer == 0 || rules[3].Layer == rules[2].Layer {
		t.Fatalf("anonymous layer rank = %d", rules[3].Layer)
	}
}

func TestLayerStatementDeclaresOrder(t *testing.T) {
	doc := ParseHTML(`<div class="x">text</div>`)
	rules := ParseStylesheet(`
		@layer theme, base;
		@layer base { .x { color: #111111 } }
		@layer theme { .x { color: #222222 } }
	`)
	if len(rules) != 2 {
		t.Fatalf("layer statement produced rules: %+v", rules)
	}
	styles := newStyleEngine(rules, 100).computeAll(doc)
	if got := styles[doc.FindFirst("div")].Color; got != RGB(0x11, 0x11, 0x11) {
		t.Fatalf("color = %+v, want the later-declared base layer to win", got)
	}
}

func TestUnlayeredBeatsLayers(t *testing.T) {
	doc := ParseHTML(`<div class="x">text</div>`)
	rules := ParseStylesheet(`
		@layer layout { .x { color: #111111 } }
		.x { color: #222222 }
	`)
	styles := newStyleEngine(rules, 100).computeAll(doc)
	st := styles[doc.FindFirst("div")]
	if st.Color != RGB(0x22, 0x22, 0x22) {
		t.Fatalf("color = %+v, want unlayered rule to win", st.Color)
	}
}

func TestImportantReversesLayerOrder(t *testing.T) {
	doc := ParseHTML(`<div class="x">text</div>`)
	rules := ParseStylesheet(`
		@layer a { .x { color: #111111 !important } }
		@layer b { .x { color: #222222 !important } }
	`)
	styles := newStyleEngine(rules, 100).computeAll(doc)
	if got := styles[doc.FindFirst("div")].Color; got != RGB(0x11, 0x11, 0x11) {
		t.Fatalf("color = %+v, want the earlier layer to win for !important", got)
	}

	// An unlayered !important declaration is weaker than any layer's.
	rules = ParseStylesheet(`
		@layer a { .x { color: #111111 !important } }
		.x { color: #222222 !important }
	`)
	styles = newStyleEngine(rules, 100).computeAll(doc)
	if got := styles[doc.FindFirst("div")].Color; got != RGB(0x11, 0x11, 0x11) {
		t.Fatalf("color = %+v, want layered !important to beat unlayered", got)
	}
}

func TestNestedLayerRulesParsed(t *testing.T) {
	doc := ParseHTML(`<div class="x">text</div>`)
	rules := ParseStylesheet(`
		@layer outer { @layer inner { .x { color: #111111 } } }
	`)
	if len(rules) != 1 || rules[0].Layer == 0 {
		t.Fatalf("nested layer rules = %+v", rules)
	}
	styles := newStyleEngine(rules, 100).computeAll(doc)
	if got := styles[doc.FindFirst("div")].Color; got != RGB(0x11, 0x11, 0x11) {
		t.Fatalf("nested layer rule did not apply: %+v", got)
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

func TestCSSImportsScalesWithLargeSheets(t *testing.T) {
	// A multi-megabyte sheet with many top-level at-rules used to be scanned
	// quadratically: every "@" lowercased the whole remaining source. Guard
	// both correctness and a generous linear-time bound.
	var b strings.Builder
	for i := 0; i < 20000; i++ {
		b.WriteString("@media screen{.a{color:#111}}\n")
	}
	b.WriteString(`@import url("extra.css") screen;`)
	src := b.String()

	start := time.Now()
	imports := cssImports(src)
	elapsed := time.Since(start)
	if len(imports) != 1 || imports[0].href != "extra.css" || imports[0].media != "screen" {
		t.Fatalf("imports = %+v", imports)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("cssImports took %v for %d bytes; the scan is not linear", elapsed, len(src))
	}
}

func TestBackgroundImageParsing(t *testing.T) {
	doc := ParseHTML(`<div id="a"></div>`)
	rules := ParseStylesheet(`
		#a {
			background-image: url('https://example.test/hero.jpg');
			background-size: cover;
			background-position: right bottom;
		}`)
	styles := newStyleEngine(rules, 100).computeAll(doc)
	st := styles[doc.FindFirst("div")]
	if st.BackgroundImage != "https://example.test/hero.jpg" {
		t.Fatalf("background image = %q", st.BackgroundImage)
	}
	if st.BackgroundSize != "cover" {
		t.Fatalf("background size = %q", st.BackgroundSize)
	}
	if st.BackgroundPosX != 1 || st.BackgroundPosY != 1 {
		t.Fatalf("background position = %v,%v", st.BackgroundPosX, st.BackgroundPosY)
	}
}

func TestStylesheetSkipsCommentsInsideBlocks(t *testing.T) {
	// A comment holding an apostrophe used to start a phantom string in the
	// block scanner, which made the rest of the sheet unparseable (FileGator's
	// browser.css lost ~200 rules after "the button's title").
	rules := ParseStylesheet(`
		.a { color: red; /* the button's title */ width: 10px; }
		.b { color: blue; /* braces { } inside */ height: 4px; }
		.c { color: green; }
	`)
	var got []string
	for _, r := range rules {
		got = append(got, r.Selector.Text)
		if r.Selector.Text == ".a" {
			found := false
			for _, d := range r.Decls {
				if d.Prop == "width" && d.Value == "10px" {
					found = true
				}
			}
			if !found {
				t.Fatalf("comment swallowed the declaration after it: %+v", r.Decls)
			}
		}
	}
	want := []string{".a", ".b", ".c"}
	if len(got) != len(want) {
		t.Fatalf("rules = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rules = %v, want %v", got, want)
		}
	}
}
