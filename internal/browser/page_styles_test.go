package browser

import (
	"testing"
)

func TestCollectDynamicStyles(t *testing.T) {
	root := ParseHTML(`<html><head><style>body{margin:0}</style></head>` +
		`<body><div id="x">BOX</div></body></html>`)
	page := &Page{Root: root, Author: ParseStylesheet(`body{margin:0}`)}
	page.seedParsedStyles()
	if page.collectDynamicStyles() {
		t.Fatal("static style was collected twice")
	}
	head := root.FindFirst("head")
	if head == nil {
		t.Fatal("head not found")
	}
	cssText := &Node{Type: TextNode, Text: "#x{padding-left:40px}"}
	style := &Node{Type: ElementNode, Tag: "style", Parent: head, Children: []*Node{cssText}}
	cssText.Parent = style
	head.Children = append(head.Children, style)
	if !page.collectDynamicStyles() {
		t.Fatal("new style was not collected")
	}
	textX := func() int {
		ly := BuildLayout(root, page.Author, 40)
		for i := range ly.Frags {
			if f := &ly.Frags[i]; f.Kind == fragText && f.Text == "BOX" {
				return f.X
			}
		}
		t.Fatal("BOX text not found")
		return -1
	}
	if got := textX(); got != 5 {
		t.Fatalf("BOX at X=%d, want 5 after 40px padding", got)
	}
	if page.collectDynamicStyles() {
		t.Fatal("unchanged style was collected twice")
	}
	cssText.Text = "#x{padding-left:80px}"
	if !page.collectDynamicStyles() {
		t.Fatal("changed style was not collected")
	}
	if got := textX(); got != 10 {
		t.Fatalf("BOX at X=%d, want 10 after the later 80px rule", got)
	}
}
