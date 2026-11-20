package browser

import (
	"testing"
	"time"
)

func TestSelectors(t *testing.T) {
	doc := ParseHTML(`<div><p class="a b" data-x="hello world">1</p><p id="last">2</p><span></span></div>`)
	cases := []struct {
		sel string
		n   int
	}{
		{"p.a", 1}, {"div > p", 2}, {"div p", 2}, {"p:first-child", 1},
		{"p:last-child", 0}, {"span:last-child", 1}, {"[data-x]", 1},
		{"[data-x~='world']", 1}, {"[data-x^=hell]", 1}, {"p:not(.a)", 1},
		{"p:nth-child(2)", 1}, {"body p", 0},
	}
	for _, c := range cases {
		sel, err := ParseSelector(c.sel)
		if err != nil {
			t.Fatalf("%s: %v", c.sel, err)
		}
		got := 0
		for _, n := range doc.FindAll(func(n *Node) bool { return true }) {
			if sel.Match(n) {
				got++
			}
		}
		if got != c.n {
			t.Errorf("%s matched %d, want %d", c.sel, got, c.n)
		}
	}
}

func TestSelectorNoProgress(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		_, err := ParseSelector("0%")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("selector without progress was accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("ParseSelector did not terminate")
	}
}

func TestEscapedSelectorIdentifiers(t *testing.T) {
	// Utility frameworks escape the ":" that separates a variant from a
	// class, and may escape a leading digit. Both must resolve to the class
	// name as written in the class attribute.
	doc := ParseHTML(`<div><p class="md:flex" id="first">a</p><p class="xl:grid" id="second">b</p><p class="2col" id="third">c</p><span id="a:b">d</span></div>`)
	rules := ParseStylesheet(`.md\:flex{display:flex}.xl\:grid{display:grid}.2col{color:#00ff00}.\32 col{padding:2px}#a\:b{color:#ff0000}`)
	styles := newStyleEngine(rules, 100).computeAll(doc)
	if got := styles[findElementByID(doc, "first")].Display; got != "flex" {
		t.Fatalf("escaped variant class display = %q", got)
	}
	if got := styles[findElementByID(doc, "second")].Display; got != "grid" {
		t.Fatalf("second escaped variant class display = %q", got)
	}
	if got := styles[findElementByID(doc, "a:b")].Color; got != RGB(0xff, 0, 0) {
		t.Fatalf("escaped id color = %+v", got)
	}
	if got := styles[findElementByID(doc, "third")].Color; got != RGB(0x00, 0xff, 0x00) {
		t.Fatalf("digit class color = %+v", got)
	}
}
