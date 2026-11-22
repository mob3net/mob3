package engine

import (
	"strings"
	"testing"
)

func TestPrefersColorSchemeMedia(t *testing.T) {
	rules := ParseStylesheet(`
		@media (prefers-color-scheme: dark) { p { color: #ffffff; } }
		@media (prefers-color-scheme: light) { p { background-color: #ffffff; } }
	`)
	darkDoc := ParseHTML(`<p>x</p>`)
	darkEngine := newStyleEngine(rules, 800)
	darkEngine.dark = true
	dark := darkEngine.computeAll(darkDoc)[darkDoc.FindFirst("p")]
	if !dark.Color.Set || dark.Color.R != 255 || dark.Color.G != 255 || dark.Color.B != 255 {
		t.Fatalf("dark color = %+v, want white", dark.Color)
	}
	if dark.Background.Set {
		t.Fatalf("light-scheme rule applied in dark mode: %+v", dark.Background)
	}

	lightDoc := ParseHTML(`<p>x</p>`)
	light := newStyleEngine(rules, 800).computeAll(lightDoc)[lightDoc.FindFirst("p")]
	if light.Background.R != 255 || light.Background.G != 255 || light.Background.B != 255 {
		t.Fatalf("light background = %+v, want white", light.Background)
	}
	if light.Color.R == 255 && light.Color.G == 255 && light.Color.B == 255 {
		t.Fatal("dark-scheme rule applied in light mode")
	}
}

func TestMediaCalcWidthQueries(t *testing.T) {
	if !mediaMatches("(max-width:calc(1680px - 1px))", 960, false) {
		t.Fatal("max-width calc did not match a narrower viewport")
	}
	if mediaMatches("(max-width:calc(1120px - 1px))", 1200, false) {
		t.Fatal("max-width calc matched a wider viewport")
	}
	if !mediaMatches("(min-width:calc(640px - 1px))", 960, false) {
		t.Fatal("min-width calc did not match a wider viewport")
	}
	if mediaMatches("(min-width:calc(640px - 1px))", 320, false) {
		t.Fatal("min-width calc matched a narrower viewport")
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
			if mediaMatches(r.Media, vw, false) {
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
