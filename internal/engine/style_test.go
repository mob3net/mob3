package engine

import (
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
	plain := styles[findByID(doc, "plain")]
	plainChild := styles[findByID(doc, "plain-child")]
	alt := styles[findByID(doc, "alt")]
	altChild := styles[findByID(doc, "alt-child")]
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
