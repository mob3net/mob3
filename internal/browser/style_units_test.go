package browser

import (
	"testing"
)

func TestLengthUnitsParse(t *testing.T) {
	doc := ParseHTML(`<p style="font-size:20px; margin:2rem 3em 4px 5ch; padding:1lh">x</p>`)
	st := newStyleEngine(nil, 0).computeAll(doc)[doc.FindFirst("p")]
	cases := []struct {
		name string
		got  Length
		unit Unit
		v    float64
	}{
		{"margin-top", st.Margin[0], UnitREM, 2},
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
