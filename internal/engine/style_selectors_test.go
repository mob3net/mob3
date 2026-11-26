package engine

import (
	"strings"
	"testing"
	"time"
)

func TestHasSelector(t *testing.T) {
	doc := ParseHTML(`<div id="root">
		<figure id="direct"><img></figure>
		<figure id="nested"><span><img></span></figure>
		<div id="card"><span class="badge">x</span></div>
		<div id="empty"></div>
		<ul><li id="one"></li><li id="two" class="next"></li><li id="three"></li></ul>
		<div id="childwrap"><span class="a"><i class="b"></i></span></div>
		<div id="anchor"></div><div class="sib"><span class="deep"></span></div>
	</div>`)
	check := func(sel string, id string, want bool) {
		s, err := ParseSelector(sel)
		if err != nil {
			t.Fatalf("%s: %v", sel, err)
		}
		n := findByID(doc, id)
		if n == nil {
			t.Fatalf("missing #%s", id)
		}
		if got := s.Match(n); got != want {
			t.Errorf("%s on #%s = %v, want %v", sel, id, got, want)
		}
	}
	check("figure:has(> img)", "direct", true)
	check("figure:has(> img)", "nested", false)
	check("figure:has(img)", "nested", true)
	check("#card:has(.badge)", "card", true)
	check("#empty:has(.badge)", "empty", false)
	check("#one:has(+ .next)", "one", true)
	check("#one:has(~ .next)", "one", true)
	check("#three:has(+ .next)", "three", false)
	check("#card:has(> .badge)", "card", true)
	check("#childwrap:has(> .a > .b)", "childwrap", true)
	check("#childwrap:has(> .a .b)", "childwrap", true)
	check("#anchor:has(+ .sib .deep)", "anchor", true)
	check("#anchor:has(~ .sib .deep)", "anchor", true)
}

func TestHasSelectorRulesApply(t *testing.T) {
	doc := ParseHTML(`<html><body><div class="card" id="yes"><img></div><div class="card" id="no"></div></body></html>`)
	rules := ParseStylesheet(`.card:has(img){background:#ff0000}`)
	styles := newStyleEngine(rules, 80).computeAll(doc)
	if st := styles[findByID(doc, "yes")]; st == nil || st.Background.R != 255 || st.Background.G != 0 {
		t.Fatalf("has rule did not apply: %#v", st)
	}
	if st := styles[findByID(doc, "no")]; st != nil && st.Background.R == 255 {
		t.Fatal("has rule applied without a matching descendant")
	}
}

func TestEmptyHasIsInvalid(t *testing.T) {
	for _, sel := range []string{":has()", "div:has()", "div:has(>)"} {
		if _, err := ParseSelector(sel); err == nil {
			t.Fatalf("%q parsed as a valid selector", sel)
		}
	}
}

func TestHasSelectorUsesHover(t *testing.T) {
	rules := ParseStylesheet(`.card:has(.badge:hover){color:#ff0000}`)
	if !rulesUseHover(rules) {
		t.Fatal(":has(:hover) did not mark the rules as hover-dependent")
	}
}

func TestSelectors(t *testing.T) {
	doc := ParseHTML(`<div><p class="a b" data-x="hello world">1</p><p id="last">2</p><span></span></div>`)
	cases := []struct {
		sel string
		n   int
	}{
		{"p.a", 1}, {"div > p", 2}, {"div p", 2}, {"p:first-child", 1},
		{"p:last-child", 0}, {"span:last-child", 1}, {"[data-x]", 1},
		{"[data-x~='world']", 1}, {"[data-x^=hell]", 1}, {"p:not(.a)", 1},
		{"p:nth-child(2)", 1}, {"body p", 0},
	}
	for _, c := range cases {
		sel, err := ParseSelector(c.sel)
		if err != nil {
			t.Fatalf("%s: %v", c.sel, err)
		}
		got := 0
		for _, n := range doc.FindAll(func(n *Node) bool { return true }) {
			if sel.Match(n) {
				got++
			}
		}
		if got != c.n {
			t.Errorf("%s matched %d, want %d", c.sel, got, c.n)
		}
	}
}

func TestDeepDescendantSelectorIsBounded(t *testing.T) {
	const depth = 300
	root := ParseHTML(strings.Repeat("<div>", depth) + "<span>deep</span>" + strings.Repeat("</div>", depth))
	span := root.FindFirst("span")
	if span == nil {
		t.Fatal("deep span was not parsed")
	}
	sel, err := ParseSelector("div div div div div div div div span")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan bool, 1)
	go func() { done <- sel.Match(span) }()
	select {
	case got := <-done:
		if !got {
			t.Fatal("deep selector did not match")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("deep selector did not finish")
	}

	budget := &matchBudget{max: 10}
	sel.matchWith(span, budget)
	if budget.steps > budget.max {
		t.Fatalf("selector used %d steps, max %d", budget.steps, budget.max)
	}
}

func TestUAStylesSurviveAuthorBudgetExhaustion(t *testing.T) {
	root := ParseHTML(`<html><body><script type="application/json">payload</script><div>visible</div></body></html>`)
	engine := newStyleEngine(nil, 100)
	engine.matchBudget.max = 1 // author matching is effectively exhausted
	styles := engine.computeAll(root)
	script := root.FindFirst("script")
	if script == nil || styles[script] == nil {
		t.Fatal("script node has no computed style")
	}
	if styles[script].Display != "none" {
		t.Fatalf("script display = %q, want none", styles[script].Display)
	}
}

func TestSelectorNoProgress(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		_, err := ParseSelector("0%")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("selector without progress was accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("ParseSelector did not terminate")
	}
}

func TestEscapedSelectorIdentifiers(t *testing.T) {
	// Utility frameworks escape the ":" that separates a variant from a
	// class, and may escape a leading digit. Both must resolve to the class
	// name as written in the class attribute.
	doc := ParseHTML(`<div><p class="md:flex" id="first">a</p><p class="xl:grid" id="second">b</p><p class="2col" id="third">c</p><span id="a:b">d</span></div>`)
	rules := ParseStylesheet(`.md\:flex{display:flex}.xl\:grid{display:grid}.2col{color:#00ff00}.\32 col{padding:2px}#a\:b{color:#ff0000}`)
	styles := newStyleEngine(rules, 100).computeAll(doc)
	if got := styles[findByID(doc, "first")].Display; got != "flex" {
		t.Fatalf("escaped variant class display = %q", got)
	}
	if got := styles[findByID(doc, "second")].Display; got != "grid" {
		t.Fatalf("second escaped variant class display = %q", got)
	}
	if got := styles[findByID(doc, "a:b")].Color; got != RGB(0xff, 0, 0) {
		t.Fatalf("escaped id color = %+v", got)
	}
	if got := styles[findByID(doc, "third")].Color; got != RGB(0x00, 0xff, 0x00) {
		t.Fatalf("digit class color = %+v", got)
	}
}

func TestEmptyFunctionalPseudosAreInvalid(t *testing.T) {
	// ":is()" and friends take a selector list; an empty list is invalid and
	// the whole rule must be dropped instead of matching every element.
	for _, sel := range []string{":is()", ":where()", ":not()", "div:is()", ".a:not()"} {
		if _, err := ParseSelector(sel); err == nil {
			t.Fatalf("%q parsed as a valid selector", sel)
		}
	}
	rules := ParseStylesheet(`:is(){position:absolute}p{color:#00ff00}`)
	doc := ParseHTML(`<html><body><p>x</p></body></html>`)
	styles := newStyleEngine(rules, 80).computeAll(doc)
	if st := styles[doc.FindFirst("body")]; st != nil && st.Position == "absolute" {
		t.Fatal("empty :is() rule was applied to body")
	}
	if st := styles[doc.FindFirst("p")]; st == nil || st.Color.R != 0 || st.Color.G != 255 {
		t.Fatalf("valid rule after the invalid one did not apply: %#v", st)
	}
}
