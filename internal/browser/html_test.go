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

func TestParseDepthCap(t *testing.T) {
	const n = 5000
	doc := ParseHTML(strings.Repeat("<div>", n))
	var walk func(*Node, int) (int, int)
	walk = func(x *Node, depth int) (maxDepth, count int) {
		maxDepth, count = depth, 0
		if x.Type == ElementNode {
			count = 1
		}
		for _, c := range x.Children {
			d, k := walk(c, depth+1)
			if d > maxDepth {
				maxDepth = d
			}
			count += k
		}
		return maxDepth, count
	}
	depth, count := walk(doc, 0)
	if depth > maxParseDepth+1 {
		t.Fatalf("parsed depth %d exceeds cap %d", depth, maxParseDepth+1)
	}
	if count != n {
		t.Fatalf("kept %d elements, want %d", count, n)
	}
}

func TestParseClosedPDoesNotTruncateLaterStack(t *testing.T) {
	doc := ParseHTML(`<div id="outer"><p>a</p><section id="s">b</section><div id="x">c</div></div>`)
	x := findByID(doc, "x")
	if x == nil || x.Parent == nil || x.Parent.Attr("id") != "outer" {
		t.Fatalf("div#x parent = %v, want the outer div", x)
	}
	if s := findByID(doc, "s"); s == nil || s.TextContent() != "b" {
		t.Fatalf("section = %v, want text b", s)
	}
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

func TestRawTextEndTagAfterCaseFolding(t *testing.T) {
	// U+0130 lowercases to two code points, which changes byte length. The
	// old raw-text scan lowercased the whole document first and then used the
	// resulting offset against the original string, truncating the script.
	body := "var label = \"İİİ\";\n// keep this trailing comment\n"
	src := "<script>" + body + "</script><p>after</p>"
	doc := ParseHTML(src)
	script := doc.FindFirst("script")
	if script == nil {
		t.Fatal("script element missing")
	}
	if got := script.TextContent(); got != body {
		t.Fatalf("script text truncated: got %q, want %q", got, body)
	}
	if p := doc.FindFirst("p"); p == nil || p.TextContent() != "after" {
		t.Fatal("content after the script was lost")
	}

	// The end tag is still found case-insensitively.
	doc = ParseHTML("<script>x</SCRIPT><b>y</b>")
	if got := doc.FindFirst("script").TextContent(); got != "x" {
		t.Fatalf("upper-case end tag: script text = %q", got)
	}
	if doc.FindFirst("b") == nil {
		t.Fatal("content after an upper-case end tag was lost")
	}
}
