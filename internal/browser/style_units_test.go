package browser

import (
	"testing"
)

func TestNonFiniteLengthsAreIgnored(t *testing.T) {
	// Scripts can write NaN or Infinity into inline styles (for example a
	// carousel computing a slide width while the layout reports zero). Invalid
	// values must behave like an unset declaration, not become geometry.
	for _, in := range []string{"NaN", "NaNpx", "NaNeM", "Infinitypx", "-Infinity%", "Inf", "calc(1px / 0)"} {
		if l := parseLength(in, 16); l.Set {
			t.Errorf("parseLength(%q) = %+v, want an unset length", in, l)
		}
	}

	doc := ParseHTML(`<div class="slide" style="width:NaNpx;margin-left:Infinitypx">x</div>`)
	st := newStyleEngine(ParseStylesheet(`.slide{width:48px;margin-left:4px}`), 100).computeAll(doc)[doc.FindFirst("div")]
	if !st.Width.Set || st.Width.V != 48 {
		t.Fatalf("width = %+v, want the 48px stylesheet value", st.Width)
	}
	if !st.Margin[3].Set || st.Margin[3].V != 4 {
		t.Fatalf("margin-left = %+v, want the 4px stylesheet value", st.Margin[3])
	}
}

func TestLengthUnitsParse(t *testing.T) {
	doc := ParseHTML(`<p style="font-size:20px; margin:2rem 3em 4px 5ch; padding:1lh">x</p>`)
	st := newStyleEngine(nil, 0).computeAll(doc)[doc.FindFirst("p")]
	cases := []struct {
		name string
		got  Length
		unit Unit
		v    float64
	}{
		// rem resolves to px against the root font size during the cascade.
		{"margin-top", st.Margin[0], UnitPX, 32},
		{"margin-right", st.Margin[1], UnitEM, 3},
		{"margin-bottom", st.Margin[2], UnitPX, 4},
		{"margin-left", st.Margin[3], UnitCH, 5},
		{"padding-top", st.Padding[0], UnitPX, 20},
	}
	for _, c := range cases {
		if c.got.U != c.unit || c.got.V != c.v {
			t.Errorf("%s = %+v, want %v %v", c.name, c.got, c.v, c.unit)
		}
	}
	if got := st.Margin[0].resolveH(100, st.FontSize); got != 4 {
		t.Errorf("2rem resolved to %d cells, want 4", got)
	}
}

func TestLHUnitsResolveAfterCascade(t *testing.T) {
	doc := ParseHTML(`<div style="font-size:20px; line-height:1.5">` +
		`<p style="margin:1lh; margin-block:2lh 3lh; padding-inline:1ch; ` +
		`padding-inline-start:2ch; height:2lh; max-width:10lh">x</p></div>`)
	st := newStyleEngine(nil, 0).computeAll(doc)[doc.FindFirst("p")]
	if got := st.Margin[0]; got.U != UnitPX || got.V != 60 {
		t.Fatalf("margin-block-start = %+v, want 60px", got)
	}
	if got := st.Margin[2]; got.U != UnitPX || got.V != 90 {
		t.Fatalf("margin-block-end = %+v, want 90px", got)
	}
	if got := st.Margin[1]; got.U != UnitPX || got.V != 30 {
		t.Fatalf("margin-inline-end = %+v, want 30px", got)
	}
	if got := st.Margin[3]; got.U != UnitPX || got.V != 30 {
		t.Fatalf("margin-inline-start = %+v, want 30px", got)
	}
	if got := st.Padding[3]; got.U != UnitCH || got.V != 2 {
		t.Fatalf("padding-inline-start = %+v, want 2ch", got)
	}
	if got := st.Padding[1]; got.U != UnitCH || got.V != 1 {
		t.Fatalf("padding-inline-end = %+v, want 1ch", got)
	}
	if got := st.Height; got.U != UnitPX || got.V != 60 {
		t.Fatalf("height = %+v, want 60px", got)
	}
	if got := st.MaxWidth; got.U != UnitPX || got.V != 300 {
		t.Fatalf("max-width = %+v, want 300px", got)
	}
}

func TestCalcLengths(t *testing.T) {
	cases := []struct {
		value string
		basis int
		want  int
	}{
		{"calc(100% - 312px)", 60, 21},
		{"calc(33rem - 24px)", 100, 63},
		{"calc(250 / 970 * 100%)", 100, 26},
		{"calc(100% - (1rem + 19px))", 80, 76},
		{"calc(60% + 8px)", 50, 31},
	}
	for _, tc := range cases {
		l := parseLength(tc.value, 16)
		if !l.Set || !l.Calc {
			t.Fatalf("parseLength(%q) = %+v, want a calc length", tc.value, l)
		}
		if got := l.resolveH(tc.basis, 16); got != tc.want {
			t.Errorf("%q at %d columns = %d, want %d", tc.value, tc.basis, got, tc.want)
		}
	}
	for _, bad := range []string{"calc(50vw - 650px)", "calc(var(--gap) + 1px)"} {
		if l := parseLength(bad, 16); l.Set {
			t.Errorf("parseLength(%q) = %+v, want unsupported", bad, l)
		}
	}
}

func TestRemUsesRootFontSize(t *testing.T) {
	doc := ParseHTML(`<html><head><style>
		html { font-size: 14px; }
		body { margin: 0; }
		.box { width: 10rem; margin-left: 1.5rem; padding-left: 1rem; }
	</style></head><body><div class="box">text</div></body></html>`)
	rules := ParseStylesheet(`html { font-size: 14px } body { margin: 0 }
		.box { width: 10rem; margin-left: 1.5rem; padding-left: 1rem }`)
	ly := BuildLayout(doc, rules, 80)
	box, _ := findSelectorNode(doc, ".box")
	st := ly.Styles[box]
	// 1rem is the root's 14px: 10rem = 140px = 18 cells, 1.5rem = 21px = 3 cells.
	if got := st.Width.resolveH(80, st.FontSize); got != 18 {
		t.Fatalf("width:10rem = %d cells, want 18 (root font 14px)", got)
	}
	if got := st.Margin[3].resolveH(80, st.FontSize); got != 3 {
		t.Fatalf("margin-left:1.5rem = %d cells, want 3", got)
	}
	if got := st.Padding[3].resolveH(80, st.FontSize); got != 2 {
		t.Fatalf("padding-left:1rem = %d cells, want 2", got)
	}
}

func TestZoomCellRatio(t *testing.T) {
	defer setLayoutZoom(1)

	setLayoutZoom(1)
	if got := px(80).resolveH(0, 16); got != 10 {
		t.Fatalf("zoom 1: 80px = %d cells, want 10", got)
	}
	if got := px(16).resolveV(0, 16); got != 1 {
		t.Fatalf("zoom 1: 16px = %d rows, want 1", got)
	}
	if got := cssWidthPx(100); got != 800 {
		t.Fatalf("zoom 1: 100 columns = %d CSS px, want 800", got)
	}

	setLayoutZoom(2)
	if got := px(80).resolveH(0, 16); got != 20 {
		t.Fatalf("zoom 2: 80px = %d cells, want 20", got)
	}
	if got := px(16).resolveV(0, 16); got != 2 {
		t.Fatalf("zoom 2: 16px = %d rows, want 2", got)
	}
	// The CSS viewport shrinks, so responsive rules see a narrower screen.
	if got := cssWidthPx(100); got != 400 {
		t.Fatalf("zoom 2: 100 columns = %d CSS px, want 400", got)
	}
	// Percentages stay relative to their container, so they do not zoom.
	if got := pct(50).resolveH(100, 16); got != 50 {
		t.Fatalf("zoom 2: 50%% of 100 = %d cells, want 50", got)
	}

	// Text metrics keep their normal cell size.
	if got := (&Style{FontSize: 16, LineHeight: 1}).LineHeightCells(); got != 1 {
		t.Fatalf("body line height at zoom 2 = %d rows, want 1", got)
	}
	if got := (&Style{FontSize: 32, LineHeight: 1}).LineHeightCells(); got != 2 {
		t.Fatalf("32px line height at zoom 2 = %d rows, want 2", got)
	}
	// Scaled headings grow with the zoom.
	big := &Style{FontSize: 32}
	if got := bigTextScale(big, 1); got != 2 {
		t.Fatalf("32px scale at zoom 1 = %d, want 2", got)
	}
	if got := bigTextScale(big, 2); got != 4 {
		t.Fatalf("32px scale at zoom 2 = %d, want 4", got)
	}
	if got := bigTextScale(&Style{FontSize: 96}, 2); got != maxTextScale {
		t.Fatalf("large heading scale = %d, want cap %d", got, maxTextScale)
	}
	if got := bigTextScale(&Style{FontSize: 16}, 2); got != 1 {
		t.Fatalf("body text scale = %d, want 1", got)
	}
}
