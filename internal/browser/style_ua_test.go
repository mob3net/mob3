package browser

import (
	"testing"
)

func TestDialogOpenStateDisplay(t *testing.T) {
	doc := ParseHTML(`<html><body><dialog id="closed"><p>closed</p></dialog><dialog id="open" open><p>open</p></dialog></body></html>`)
	styles := newStyleEngine(nil, 0).computeAll(doc)
	dialogs := doc.FindAll(func(n *Node) bool { return n.Tag == "dialog" })
	if len(dialogs) != 2 {
		t.Fatalf("found %d dialogs", len(dialogs))
	}
	if got := styles[dialogs[0]].Display; got != "none" {
		t.Fatalf("closed dialog display = %q, want none", got)
	}
	if got := styles[dialogs[1]].Display; got != "block" {
		t.Fatalf("open dialog display = %q, want block", got)
	}
}
