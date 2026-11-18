package browser

import (
	"testing"
)

func TestMeasureSkipsOutOfFlowChildren(t *testing.T) {
	doc := ParseHTML(`<html><body><div id="box"><span style="position:absolute;width:100px">very wide absolute label</span></div></body></html>`)
	styles := newStyleEngine(nil, 0).computeAll(doc)
	l := &layouter{styles: styles, width: 40}
	box := doc.FindFirst("div")
	maxW, minW := l.measure(box)
	if maxW > 10 || minW > 10 {
		t.Fatalf("measure included out-of-flow child: max=%d min=%d", maxW, minW)
	}
}

func TestMeasureCollapsesWhitespace(t *testing.T) {
	if mw, mi := measureInlineText("\n   alpha   beta \n", "normal", 1); mw != 10 || mi != 5 {
		t.Fatalf("normal text measure = %d,%d, want 10,5", mw, mi)
	}
	if mw, _ := measureInlineText(" a  b ", "pre", 1); mw != 6 {
		t.Fatalf("pre text measure = %d, want 6 preserved cells", mw)
	}
}

func TestDefiniteWidthCountsInMeasure(t *testing.T) {
	doc := ParseHTML(`<div style="width:400px"><div id="box" style="float:left"><div style="width:48px;height:16px"></div></div></div>`)
	ly := BuildLayout(doc, nil, 50)
	var box *Node
	doc.FindAll(func(n *Node) bool {
		if n.Attr("id") == "box" {
			box = n
		}
		return false
	})
	if box == nil {
		t.Fatal("box missing")
	}
	if b := ly.Boxes[box]; b.W != 6 {
		t.Fatalf("float around a 48px child = %d cells, want 6", b.W)
	}
}

func TestBlockInlineChildrenMeasureAsOneLine(t *testing.T) {
	doc := ParseHTML(`<div id="box" style="display:block"><span>aa</span> <span>bb</span></div>`)
	ly := BuildLayout(doc, nil, 40)
	l := &layouter{width: 40, styles: ly.Styles}
	node := doc.FindFirst("div")
	if node == nil {
		t.Fatal("box missing")
	}
	if mw, _ := l.measure(node); mw != 5 {
		t.Fatalf("block with inline children measure = %d, want 5 (aa bb)", mw)
	}
}
