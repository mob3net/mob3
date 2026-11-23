package engine

import "testing"

func TestBuildTabTargetsOrderAndRadioGroup(t *testing.T) {
	doc := ParseHTML(`<html><body>
		<a id="link" href="/next">next</a>
		<form><input name="q"><input type="radio" name="r" value="a"><input type="radio" name="r" value="b" checked></form>
	</body></html>`)
	link := doc.FindFirst("a")
	inputs := doc.FindAll(func(n *Node) bool { return n.Tag == "input" })
	for _, n := range inputs {
		n.Style = &Style{}
	}
	layout := &Layout{Links: []*LinkInfo{{Node: link}}}
	targets := BuildTabTargets(&Page{Root: doc}, layout)
	if len(targets) != 3 {
		t.Fatalf("targets = %d, want link + text input + one radio stop", len(targets))
	}
	if targets[0].Node != link || targets[0].Link != 0 {
		t.Fatalf("first target = %+v", targets[0])
	}
	if targets[1].Node != inputs[0] {
		t.Fatalf("second target = %+v", targets[1])
	}
	if targets[2].Node != inputs[2] {
		t.Fatalf("radio stop = %+v, want the checked radio", targets[2])
	}
}

func TestTabIndexWrapsAndRadioGroup(t *testing.T) {
	doc := ParseHTML(`<html><body><a id="l" href="/">l</a><input id="i"><form>
		<input type="radio" name="r" value="a"><input type="radio" name="r" value="b" checked>
	</form></body></html>`)
	link := doc.FindFirst("a")
	input := doc.FindFirst("input")
	radios := doc.FindAll(func(n *Node) bool { return n.Tag == "input" && n.Attr("type") == "radio" })
	for _, n := range radios {
		n.Style = &Style{}
	}
	targets := BuildTabTargets(&Page{Root: doc}, &Layout{Links: []*LinkInfo{{Node: link}}})
	if len(targets) != 3 {
		t.Fatalf("targets = %d", len(targets))
	}
	if got := TabIndex(targets, input, 1); got != 2 {
		t.Fatalf("from text input = %d, want 2", got)
	}
	if got := TabIndex(targets, radios[0], -1); got != 1 {
		t.Fatalf("from unchecked radio = %d, want 1", got)
	}
	if got := TabIndex(targets, radios[1], 1); got != 0 {
		t.Fatalf("from checked radio = %d, want wrap to 0", got)
	}
}
