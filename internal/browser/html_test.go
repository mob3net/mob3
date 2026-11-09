package browser

import (
	"strings"
	"testing"
)

func findByID(n *Node, id string) *Node {
	var out *Node
	var walk func(*Node)
	walk = func(x *Node) {
		if out != nil {
			return
		}
		if x.Type == ElementNode && x.Attr("id") == id {
			out = x
			return
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(n)
	return out
}

func TestParseNestedTablesPreserved(t *testing.T) {
	doc := ParseHTML(`<table id="outer"><tr><td><table id="inner"><tr><td>x</td></tr></table></td></tr><tr><td>y</td></tr></table>`)
	outer := findByID(doc, "outer")
	inner := findByID(doc, "inner")
	if outer == nil || inner == nil {
		t.Fatal("missing table")
	}
	trs := 0
	for _, c := range outer.Children {
		if c.Type == ElementNode && c.Tag == "tr" {
			trs++
		}
	}
	if trs != 2 {
		t.Fatalf("outer direct tr = %d, want 2", trs)
	}
	if inner.Parent == nil || inner.Parent.Tag != "td" {
		t.Fatalf("inner table parent = %v, want td", inner.Parent)
	}
}

func TestSelfClosingRow(t *testing.T) {
	doc := ParseHTML(`<table><tr style="height:5px"/><tr><td>x</td></tr></table>`)
	tbl := doc.FindFirst("table")
	trs := 0
	for _, c := range tbl.Children {
		if c.Tag == "tr" {
			trs++
		}
	}
	if trs != 2 {
		t.Fatalf("trs = %d, want 2", trs)
	}
}

func TestEntities(t *testing.T) {
	doc := ParseHTML(`<p>&lt;x&gt; &amp; &#65; &#x42; &nbsp; &hellip;</p>`)
	got := doc.FindFirst("p").TextContent()
	want := "<x> & A B \u00a0 …"
	if got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestRawStyleAndScript(t *testing.T) {
	doc := ParseHTML(`<style>a > b { color: red }</style><script>if (a < b) {}</script><p>ok</p>`)
	st := doc.FindFirst("style")
	if got := st.TextContent(); got != "a > b { color: red }" {
		t.Fatalf("style text = %q", got)
	}
	if s := doc.FindFirst("script"); s == nil || !strings.Contains(s.TextContent(), "if (a < b)") {
		t.Fatal("script source should be retained for the JS runtime")
	}
}

func TestMalformedMarkupRecovers(t *testing.T) {
	doc := ParseHTML(`<div><p>one<p>two</div><span>tail`)
	ps := doc.FindAll(func(n *Node) bool { return n.Tag == "p" })
	if len(ps) != 2 {
		t.Fatalf("p count = %d, want 2", len(ps))
	}
	if ps[0].Parent.Tag != "div" || ps[1].Parent.Tag != "div" {
		t.Fatal("paragraphs should stay in div")
	}
}

func TestClassListCacheInvalidates(t *testing.T) {
	n := &Node{Type: ElementNode, Tag: "div", Attrs: []Attr{{Key: "class", Value: "a b"}}}
	if got := strings.Join(n.ClassList(), ","); got != "a,b" {
		t.Fatalf("class list = %q", got)
	}
	n.Attrs[0].Value = "c d"
	if got := strings.Join(n.ClassList(), ","); got != "c,d" {
		t.Fatalf("cached class list = %q after mutation", got)
	}
	n.Attrs = []Attr{{Key: "class", Value: "e"}}
	if got := strings.Join(n.ClassList(), ","); got != "e" {
		t.Fatalf("class list = %q after attribute replacement", got)
	}
}
