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
