package browser

import (
	"strings"
	"testing"
)

func TestSearchFieldMinimumWidth(t *testing.T) {
	input := &Node{Type: ElementNode, Tag: "input", Attrs: []Attr{
		{Key: "type", Value: "search"}, {Key: "placeholder", Value: "Search"},
	}}
	input.Style = &Style{Display: "inline", FontSize: 16, LineHeight: 1}
	if w := stringWidth(formLabel(input)); w < 34 {
		t.Fatalf("search field width = %d, want at least 34", w)
	}
	area := &Node{Type: ElementNode, Tag: "textarea", Attrs: []Attr{{Key: "placeholder", Value: "Search"}}}
	area.Style = &Style{Display: "inline", FontSize: 16, LineHeight: 1}
	if w := stringWidth(formLabel(area)); w < 34 {
		t.Fatalf("textarea width = %d, want at least 34", w)
	}
}

func TestInputFocusClearsPlaceholder(t *testing.T) {
	input := &Node{Type: ElementNode, Tag: "input", Attrs: []Attr{{Key: "placeholder", Value: "Search query"}}}
	focusStyle := func(n *Node) {
		n.Style = &Style{Display: "inline", FontSize: 16, LineHeight: 1, Color: RGB(0, 0, 0)}
	}
	focusStyle(input)
	if label := formLabel(input); !strings.Contains(label, "Search query") {
		t.Fatalf("unfocused label = %q", label)
	}
	input.InputFocused = true
	if label := formLabel(input); strings.Contains(label, "Search query") {
		t.Fatalf("focused field still shows placeholder: %q", label)
	}
}

func TestFocusedInputIsProminent(t *testing.T) {
	ly := build(`<input value="abc">`, 40)
	input := ly.Frags
	var node *Node
	for i := range input {
		if input[i].Kind == fragText && input[i].Node != nil && input[i].Node.Tag == "input" {
			node = input[i].Node
			break
		}
	}
	if node == nil {
		t.Fatal("input fragment not found")
	}
	node.InputFocused = true
	ly = build(`<input value="abc">`, 40)
	g := NewGrid(40, 4, RGB(0, 0, 0), RGB(255, 255, 255))
	PaintLayout(g, ly, 0, 0, 4, nil, RGB(0, 0, 0))
	// Rebuild with the focused node so the painting path sees the flag.
	doc := ParseHTML(`<input value="abc">`)
	doc.FindFirst("input").InputFocused = true
	ly = BuildLayout(doc, nil, 40)
	PaintLayout(g, ly, 0, 0, 4, nil, RGB(0, 0, 0))
	blue := RGB(0x2f, 0x6f, 0xbf)
	found := false
	for _, c := range g.Cells {
		if c.BG == blue {
			found = true
			if c.FG.R != 255 || c.FG.G != 255 || c.FG.B != 255 {
				t.Fatalf("focused field text color = %+v", c.FG)
			}
		}
	}
	if !found {
		t.Fatal("focused input background was not painted")
	}
}

func TestButtonSkipsStyleText(t *testing.T) {
	ly := build(`<button>Go<style>.x{display:none}</style></button>`, 40)
	found := false
	for _, f := range ly.Frags {
		if f.Kind == fragText && strings.Contains(f.Text, ".x{") {
			t.Fatal("style text leaked into button label")
		}
		if f.Kind == fragText && strings.Contains(f.Text, "Go") {
			found = true
		}
	}
	if !found {
		t.Fatal("button label missing")
	}
}

func TestTextareaLargeValueBounded(t *testing.T) {
	doc := ParseHTML(`<textarea name="t">` + strings.Repeat("word ", 50000) + `</textarea>`)
	ly := BuildLayout(doc, nil, 80)
	area := doc.FindFirst("textarea")
	if area == nil {
		t.Fatal("textarea not found")
	}
	ys := map[int]bool{}
	for _, f := range ly.Frags {
		if f.Kind == fragText && f.Node == area {
			ys[f.Y] = true
		}
	}
	if len(ys) == 0 || len(ys) > 12 {
		t.Fatalf("large textarea rendered %d rows, want 1-12", len(ys))
	}
}

func TestTextareaRendersAtLeastTwoRows(t *testing.T) {
	doc := ParseHTML(`<textarea name="t">hello</textarea>`)
	ly := BuildLayout(doc, nil, 60)
	area := doc.FindFirst("textarea")
	if area == nil {
		t.Fatal("textarea not found")
	}
	ys := map[int]bool{}
	for _, f := range ly.Frags {
		if f.Kind == fragText && f.Node == area {
			ys[f.Y] = true
		}
	}
	if len(ys) < 2 {
		t.Fatalf("textarea rendered %d row(s), want at least 2", len(ys))
	}
}

func TestTextareaWrapsAndHonoursRows(t *testing.T) {
	doc := ParseHTML(`<textarea name="t" rows="4">` + strings.Repeat("word ", 40) + `</textarea>`)
	ly := BuildLayout(doc, nil, 60)
	area := doc.FindFirst("textarea")
	ys := map[int]bool{}
	for _, f := range ly.Frags {
		if f.Kind == fragText && f.Node == area {
			ys[f.Y] = true
		}
	}
	if len(ys) < 4 {
		t.Fatalf("textarea rendered %d row(s), want at least 4 rows", len(ys))
	}
}

func TestFormControlGlyphs(t *testing.T) {
	control := func(typ string, checked bool) *Node {
		attrs := []Attr{{Key: "type", Value: typ}}
		if checked {
			attrs = append(attrs, Attr{Key: "checked"})
		}
		return &Node{Type: ElementNode, Tag: "input", Attrs: attrs}
	}
	cases := []struct {
		node *Node
		want string
	}{
		{control("checkbox", true), glyphCheckboxOn},
		{control("checkbox", false), glyphCheckboxOff},
		{control("radio", true), glyphRadioOn},
		{control("radio", false), glyphRadioOff},
	}
	for _, c := range cases {
		if got := formLabel(c.node); got != c.want {
			t.Errorf("formLabel(%s) = %q, want %q", c.node.Attr("type"), got, c.want)
		}
		if w := stringWidth(c.want); w != 1 {
			t.Errorf("glyph %q width = %d, want 1 cell", c.want, w)
		}
	}
}
