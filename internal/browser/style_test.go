package browser

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCascadeSpecificityAndInheritance(t *testing.T) {
	doc := ParseHTML(`<div class="box" id="b"><p>Hello <span class="hi">world</span></p></div>`)
	rules := ParseStylesheet(`
		.box p { color: #ff0000; }
		p span.hi { color: #0000ff; font-weight: bold; }
		#b { background: #eeeeee; }
		p { color: #00ff00; }
	`)
	newStyleEngine(rules, 0).computeAll(doc)
	span := doc.FindFirst("span")
	p := doc.FindFirst("p")
	div := doc.FindFirst("div")
	if span.Style.Color.R != 0 || span.Style.Color.B != 255 {
		t.Fatalf("span color = %+v, want blue", span.Style.Color)
	}
	if !span.Style.Bold {
		t.Fatal("span should be bold")
	}
	if p.Style.Color.R != 255 || p.Style.Color.G != 0 {
		t.Fatalf("p color = %+v, want red", p.Style.Color)
	}
	if !div.Style.Background.Set || div.Style.Background.R != 0xee {
		t.Fatalf("div background = %+v", div.Style.Background)
	}
	if div.Style.Display != "block" {
		t.Fatalf("div display = %q", div.Style.Display)
	}
}

func TestDialogOpenStateDisplay(t *testing.T) {
	doc := ParseHTML(`<html><body><dialog id="closed"><p>closed</p></dialog><dialog id="open" open><p>open</p></dialog></body></html>`)
	styles := newStyleEngine(nil, 0).computeAll(doc)
	dialogs := doc.FindAll(func(n *Node) bool { return n.Tag == "dialog" })
	if len(dialogs) != 2 {
		t.Fatalf("found %d dialogs", len(dialogs))
	}
	if got := styles[dialogs[0]].Display; got != "none" {
		t.Fatalf("closed dialog display = %q, want none", got)
	}
	if got := styles[dialogs[1]].Display; got != "block" {
		t.Fatalf("open dialog display = %q, want block", got)
	}
}

func TestInlineStyleAndShorthands(t *testing.T) {
	doc := ParseHTML(`<p style="margin: 8px 0 16px 4px; padding: 4px; border: 1px solid #123456; background: #abcdef; font-size: 20px">x</p>`)
	st := newStyleEngine(nil, 0).computeAll(doc)[doc.FindFirst("p")]
	if st.Margin[0].V != 8 || st.Margin[3].V != 4 || st.Margin[2].V != 16 {
		t.Fatalf("margin = %+v", st.Margin)
	}
	if st.Padding[1].V != 4 {
		t.Fatalf("padding = %+v", st.Padding)
	}
	if st.Border[0].V != 1 || st.BorderColor.R != 0x12 {
		t.Fatalf("border = %+v color = %+v", st.Border, st.BorderColor)
	}
	if st.Background.R != 0xab || st.FontSize != 20 {
		t.Fatalf("background = %+v size = %v", st.Background, st.FontSize)
	}
}

func TestThreeValueBoxShorthand(t *testing.T) {
	doc := ParseHTML(`<p style="margin: 8px 12px 16px; padding: 2px 4px 6px">x</p>`)
	st := newStyleEngine(nil, 0).computeAll(doc)[doc.FindFirst("p")]
	if st.Margin[0].V != 8 || st.Margin[1].V != 12 || st.Margin[2].V != 16 || st.Margin[3].V != 12 {
		t.Fatalf("margin = %+v", st.Margin)
	}
	if st.Padding[0].V != 2 || st.Padding[1].V != 4 || st.Padding[2].V != 6 || st.Padding[3].V != 4 {
		t.Fatalf("padding = %+v", st.Padding)
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

func TestMediaQueries(t *testing.T) {
	rules := ParseStylesheet(`
		@media print { p { color: red } }
		@media screen { p { color: blue } }
		@media screen and (max-width: 1023px) { p { margin: 1px } }
		@media screen and (min-width: 1024px) { p { margin: 2px } }
	`)
	kept := func(vw int) []string {
		var out []string
		for _, r := range rules {
			if mediaMatches(r.Media, vw) {
				out = append(out, r.Decls[0].Prop+"="+r.Decls[0].Value)
			}
		}
		return out
	}
	narrow := strings.Join(kept(800), ",")
	if !strings.Contains(narrow, "color=blue") || !strings.Contains(narrow, "margin=1px") {
		t.Errorf("800px kept %q", narrow)
	}
	if strings.Contains(narrow, "margin=2px") || strings.Contains(narrow, "color=red") {
		t.Errorf("800px kept wrong rules: %q", narrow)
	}
	wide := strings.Join(kept(1200), ",")
	if !strings.Contains(wide, "margin=2px") || strings.Contains(wide, "margin=1px") {
		t.Errorf("1200px kept %q", wide)
	}
}
