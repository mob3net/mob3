package engine

import (
	"strings"
	"testing"
)

func TestDOMBudgetsRejectOverflow(t *testing.T) {
	j := &jsRuntime{}
	if !j.chargeDOMText(maxDOMTextBytes) {
		t.Fatal("full text budget was rejected")
	}
	if j.chargeDOMText(1) {
		t.Fatal("text budget overflow was accepted")
	}
	j = &jsRuntime{}
	if !j.reserveDOMNode(maxDOMNodes) {
		t.Fatal("full node budget was rejected")
	}
	if j.reserveDOMNode(1) {
		t.Fatal("node budget overflow was accepted")
	}
}

func TestDOMStringsAreBounded(t *testing.T) {
	big := strings.Repeat("x", maxDOMValueBytes+100)
	page := newJSPage(`<html><body><p id="out">before</p></body></html>`,
		`var p = document.getElementById("out");`+
			`p.textContent = "`+big+`";`+
			`p.setAttribute("data-big", "`+big+`");`)
	b := newTestSession(40, 10)
	b.startJS(page)
	p := page.Root.FindFirst("p")
	if p == nil {
		t.Fatal("page lost its paragraph")
	}
	text := p.TextContent()
	if len(text) > maxDOMValueBytes || !strings.HasSuffix(text, "…") {
		t.Fatalf("textContent = %d bytes, want a bounded prefix", len(text))
	}
	if got := len(AttrValue(p, "data-big")); got > maxDOMValueBytes {
		t.Fatalf("attribute = %d bytes, want at most %d", got, maxDOMValueBytes)
	}
}

func TestCountNodesAndTextBytes(t *testing.T) {
	root := &Node{Type: ElementNode, Tag: "div", Attrs: []Attr{{Key: "id", Value: "x"}}}
	child := &Node{Type: TextNode, Text: "hello", Parent: root}
	root.Children = []*Node{child}
	if got := countNodes(root); got != 2 {
		t.Fatalf("countNodes = %d, want 2", got)
	}
	if got := nodeTextBytes(root); got != len("id")+len("x")+len("hello") {
		t.Fatalf("nodeTextBytes = %d", got)
	}
	if countNodes(nil) != 0 || nodeTextBytes(nil) != 0 {
		t.Fatal("nil subtree was counted")
	}
}
