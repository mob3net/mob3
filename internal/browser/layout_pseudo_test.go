package browser

import (
	"strings"
	"testing"
)

// TestGeneratedContent renders literal ::before/::after text, including inline
// wrappers and a block pseudo, and checks that icon-font private-use glyphs and
// unsupported functional values generate nothing.
func TestGeneratedContent(t *testing.T) {
	root := ParseHTML(`<html><head><style>
	body { margin: 0; }
	p { margin: 0; }
	.labels::before { content: "["; }
	.labels::after { content: "]"; }
	.arrow::after { content: " →"; }
	.icon::before { content: "\f101"; }
	.attr::before { content: attr(data-label); }
	.inline .tag::before { content: "<"; }
	.inline .tag::after { content: ">"; }
	.block::before { content: "Note: "; display: block; }
	</style></head><body>
	<p class="labels">labels</p>
	<p class="arrow">next</p>
	<p class="icon">icon</p>
	<p class="attr" data-label="SECRET">attr</p>
	<p class="inline">a <span class="tag">t</span> b</p>
	<p class="block">body text</p>
	</body></html>`)
	got := renderFixture(root, fixtureAuthorRules(root), 40)
	for _, want := range []string{"[labels]", "next →", "a <t> b", "Note:", "body text"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered content is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "SECRET") {
		t.Errorf("attr() content was rendered:\n%s", got)
	}
	for _, r := range got {
		if isPrivateUseRune(r) {
			t.Errorf("private-use glyph %U was rendered:\n%s", r, got)
		}
	}
	if i := strings.Index(got, "Note:"); i < 0 || strings.Index(got[i:], "body text") < 0 {
		t.Errorf("block pseudo content did not precede the body text:\n%s", got)
	}
}
