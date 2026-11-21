package browser

import (
	"math"
	"reflect"
	"testing"
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

func TestLogicalBoxProperties(t *testing.T) {
	doc := ParseHTML(`<p style="margin-inline:4px 8px; margin-block-start:1px; ` +
		`padding-block:2px 6px; padding-inline-end:3px">x</p>`)
	st := newStyleEngine(nil, 0).computeAll(doc)[doc.FindFirst("p")]
	if st.Margin[3].V != 4 || st.Margin[1].V != 8 {
		t.Fatalf("margin-inline = %+v", st.Margin)
	}
	if st.Margin[0].V != 1 || st.Margin[2].V != 8 {
		t.Fatalf("margin-block-start = %+v", st.Margin)
	}
	if st.Padding[0].V != 2 || st.Padding[2].V != 6 {
		t.Fatalf("padding-block = %+v", st.Padding)
	}
	if st.Padding[1].V != 3 {
		t.Fatalf("padding-inline-end = %+v", st.Padding)
	}
}

func TestCSSVariables(t *testing.T) {
	doc := ParseHTML(`<html><body>
<div id="a" class="a"></div>
<div class="b"><span id="s"></span></div>
<div id="c" class="c"></div>
<div id="d" class="d"></div>
</body></html>`)
	rules := ParseStylesheet(`
		html { --gap: 8px; --brand: #ff0000; }
		.a { margin-left: var(--gap); width: calc(100% - var(--gap)); color: var(--brand); }
		.b { --gap: 4px; }
		.b span { margin-left: var(--gap); }
		.c { margin-left: var(--missing, 3px); }
		.d { margin-left: var(--missing); }
	`)
	newStyleEngine(rules, 0).computeAll(doc)
	var a, s, c, d *Node
	doc.FindAll(func(n *Node) bool {
		switch n.Attr("id") {
		case "a":
			a = n
		case "s":
			s = n
		case "c":
			c = n
		case "d":
			d = n
		}
		return false
	})
	if a.Style.Margin[3].V != 8 {
		t.Fatalf("var margin = %+v, want 8px", a.Style.Margin[3])
	}
	if !a.Style.Width.Calc || a.Style.Width.CalcPct != 100 || a.Style.Width.CalcPx != -8 {
		t.Fatalf("var calc width = %+v", a.Style.Width)
	}
	if a.Style.Color.R != 255 {
		t.Fatalf("var color = %+v", a.Style.Color)
	}
	if s.Style.Margin[3].V != 4 {
		t.Fatalf("inherited override = %+v, want 4px", s.Style.Margin[3])
	}
	if c.Style.Margin[3].V != 3 {
		t.Fatalf("var fallback = %+v, want 3px", c.Style.Margin[3])
	}
	if d.Style.Margin[3].Set {
		t.Fatalf("unresolved var should drop the declaration: %+v", d.Style.Margin[3])
	}
}

func TestBorderStyleNoneSuppressesWidth(t *testing.T) {
	st := defaultStyle()
	applyDecl(st, "border-style", "none none solid none", nil)
	applyDecl(st, "border-width", "15px", nil)
	l := &layouter{width: 100}
	_, _, _, _, bl, br, bt, bb, _, _, _, _ := l.boxMetrics(st, 100)
	if bl != 0 || br != 0 || bt != 0 {
		t.Fatalf("none sides took border width: left=%d right=%d top=%d", bl, br, bt)
	}
	if bb == 0 {
		t.Fatal("solid bottom border lost its width")
	}

	// The same applies when border-style comes after border-width.
	rev := defaultStyle()
	applyDecl(rev, "border-width", "15px", nil)
	applyDecl(rev, "border-style", "solid", nil)
	_, _, _, _, bl, br, bt, bb, _, _, _, _ = l.boxMetrics(rev, 100)
	if bl == 0 || br == 0 || bt == 0 || bb == 0 {
		t.Fatalf("solid borders lost their width: l=%d r=%d t=%d b=%d", bl, br, bt, bb)
	}

	// Per-side style works too.
	side := defaultStyle()
	applyDecl(side, "border-left-width", "15px", nil)
	applyDecl(side, "border-left-style", "none", nil)
	_, _, _, _, bl, _, _, _, _, _, _, _ = l.boxMetrics(side, 100)
	if bl != 0 {
		t.Fatalf("border-left-style:none width = %d, want 0", bl)
	}
}

func TestFlexWrapNowrap(t *testing.T) {
	doc := ParseHTML(`<div class="row nowrap"></div>`)
	rules := ParseStylesheet(`.row{flex-wrap:wrap}.nowrap{flex-wrap:nowrap !important}`)
	_ = BuildLayout(doc, rules, 100)
	n := doc.FindFirst("div")
	if n.Style.FlexWrap != "nowrap" {
		t.Fatalf("flex-wrap = %q, want nowrap (!important)", n.Style.FlexWrap)
	}
}

func TestFontShorthandWeights(t *testing.T) {
	cases := []struct {
		css   string
		size  float64
		lineH float64
	}{
		{`font: 400 12px/18px Arial`, 12, 1.5},
		{`font: italic 400 14px/21px Arial`, 14, 1.5},
		{`font: bold 1.4em/38px Arial`, 22.4, 38.0 / 22.4},
	}
	for _, tc := range cases {
		doc := ParseHTML(`<p style="` + tc.css + `">x</p>`)
		rules := ParseStylesheet("")
		_ = BuildLayout(doc, rules, 40)
		n := doc.FindFirst("p")
		if n.Style.FontSize != tc.size {
			t.Fatalf("%s: font-size = %v, want %v", tc.css, n.Style.FontSize, tc.size)
		}
		if math.Abs(n.Style.LineHeight-tc.lineH) > 0.01 {
			t.Fatalf("%s: line-height = %v, want %v", tc.css, n.Style.LineHeight, tc.lineH)
		}
	}
}

// sameVarsMap reports whether two styles share one custom-property map. Map
// headers are compared because sharing is the memory optimization itself.
func sameVarsMap(a, b map[string]string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return reflect.ValueOf(a).UnsafePointer() == reflect.ValueOf(b).UnsafePointer()
}

func TestCustomPropertiesCopyOnWrite(t *testing.T) {
	doc := ParseHTML(`<html><body>
		<div id="plain"><span id="plain-child" class="use">a</span></div>
		<div id="alt" class="alt"><span id="alt-child" class="use">b</span></div>
	</body></html>`)
	rules := ParseStylesheet(`
		:root { --brand: #112233; --accent: #cc3300; }
		body { color: var(--brand); }
		.alt { --brand: #ff8800; }
		.alt .use { color: var(--brand); }
	`)
	styles := newStyleEngine(rules, 0).computeAll(doc)

	root := styles[doc.FindFirst("html")]
	plain := styles[findElementByID(doc, "plain")]
	plainChild := styles[findElementByID(doc, "plain-child")]
	alt := styles[findElementByID(doc, "alt")]
	altChild := styles[findElementByID(doc, "alt-child")]
	for name, st := range map[string]*Style{"root": root, "plain": plain, "plainChild": plainChild, "alt": alt, "altChild": altChild} {
		if st == nil {
			t.Fatalf("%s has no computed style", name)
		}
	}

	// Elements that declare nothing share the inherited map instead of
	// copying it; the subtree that overrides --brand owns a private map.
	if !sameVarsMap(root.Vars, plainChild.Vars) {
		t.Fatal("inherited custom properties were copied per element")
	}
	if sameVarsMap(plain.Vars, alt.Vars) {
		t.Fatal("overriding a custom property must not reuse the ancestor map")
	}
	if !sameVarsMap(alt.Vars, altChild.Vars) {
		t.Fatal("a descendant of an override should share the override map")
	}

	// The override is visible down its own subtree and nowhere else.
	if got := plainChild.Vars["--brand"]; got != "#112233" {
		t.Fatalf("plain subtree --brand = %q", got)
	}
	if got := altChild.Vars["--brand"]; got != "#ff8800" {
		t.Fatalf("override subtree --brand = %q", got)
	}
	if got := root.Vars["--brand"]; got != "#112233" {
		t.Fatalf("root --brand was mutated by a descendant: %q", got)
	}
	if got := plain.Vars["--accent"]; got != "#cc3300" {
		t.Fatalf("sibling --accent = %q", got)
	}

	// var() resolution follows the same maps.
	if want := RGB(0x11, 0x22, 0x33); plainChild.Color != want {
		t.Fatalf("plain child color = %+v, want %+v", plainChild.Color, want)
	}
	if want := RGB(0xff, 0x88, 0x00); altChild.Color != want {
		t.Fatalf("override child color = %+v, want %+v", altChild.Color, want)
	}
}

func TestBoxSizingInherit(t *testing.T) {
	doc := ParseHTML(`<html><body><div id="d"><span id="s">x</span></div></body></html>`)
	rules := ParseStylesheet(`html{box-sizing:border-box} *{box-sizing:inherit}`)
	ly := BuildLayout(doc, rules, 40)
	for _, id := range []string{"d", "s"} {
		var n *Node
		for _, c := range doc.FindAll(func(n *Node) bool { return n.Attr("id") == id }) {
			n = c
		}
		if n == nil {
			t.Fatalf("no #%s", id)
		}
		st := ly.Styles[n]
		if st == nil || st.BoxSizing != "border-box" {
			t.Fatalf("#%s box-sizing = %v, want border-box through inherit", id, st)
		}
	}
}

func TestParseContentValue(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{`""`, "", true},
		{`"→ "`, "→ ", true},
		{`'a' 'b'`, "ab", true},
		{`"\2192"`, "→", true},
		{`"\f101"`, "", true},   // private-use icon glyph
		{`"\2605 "`, "★", true}, // the space ends the hex escape
		{`"\""`, `"`, true},
		{`attr(data-label)`, "", false},
		{`counter(items)`, "", false},
		{`url(icon.png)`, "", false},
		{`none`, "", false},
		{`normal`, "", false},
		{`"unterminated`, "", false},
		{`"a" junk`, "", false},
	}
	for _, tc := range cases {
		got, ok := parseContentValue(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseContentValue(%q) = %q,%v; want %q,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
