package browser

import (
	"testing"
)

func TestRuleIndexCascade(t *testing.T) {
	doc := ParseHTML(`<div id="hero" class="card" data-x="1"><span class="title">x</span></div>`)
	rules := ParseStylesheet(`
		div { color: #ff0000; }
		.card { background-color: #eeeeee; }
		#hero { font-size: 32px; }
		.card .title { font-weight: bold; }
		div[data-x] { text-align: center; }
		* { text-indent: 1px; }
	`)
	newStyleEngine(rules, 0).computeAll(doc)
	div := doc.FindFirst("div")
	span := doc.FindFirst("span")
	if div.Style.Color.R != 255 || div.Style.Background.R != 0xee {
		t.Fatalf("div style = %+v", div.Style)
	}
	if div.Style.FontSize != 32 || div.Style.TextAlign != "center" || div.Style.TextIndent.V != 1 {
		t.Fatalf("div indexed styles = %+v", div.Style)
	}
	if !span.Style.Bold || span.Style.TextIndent.V != 1 {
		t.Fatalf("span style = %+v", span.Style)
	}
}
