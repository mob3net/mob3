package browser

import (
	"math"
	"testing"
)

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
