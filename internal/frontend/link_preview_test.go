package frontend

import (
	"io"
	"strings"
	"testing"

	"browser/internal/protocol"
)

func hintPreviewTab(t *testing.T, clipboard *strings.Builder) (*Tab, *recordSession) {
	t.Helper()
	s := &recordSession{}
	tab := &Tab{Session: s, Stderr: io.Discard}
	tab.State.Page = protocol.State{FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	tab.State.Clipboard = clipboard
	items := []protocol.HintItem{{
		Target: 3, Label: "d", Link: true, Href: "https://example.com/next",
		Chars: []protocol.HintChar{{R: 'D', X: 2, Y: 1}},
	}}
	if _, err := tab.applyEvent(protocol.Event{Body: protocol.Hints{Mode: protocol.HintPreview, Items: items}}); err != nil {
		t.Fatal(err)
	}
	tab.handleShellKey("d")
	return tab, s
}

func TestHintPreviewShowsDestinationAndFollows(t *testing.T) {
	var clipboard strings.Builder
	tab, s := hintPreviewTab(t, &clipboard)
	if !tab.hintView.Preview || tab.hintView.PreviewHref != "https://example.com/next" {
		t.Fatalf("preview = %+v", tab.hintView)
	}
	if !strings.Contains(tab.State.Page.FooterLeft, "Enter follows") {
		t.Fatalf("footer = %q", tab.State.Page.FooterLeft)
	}
	if handled, quit := tab.handleShellKey("enter"); !handled || quit {
		t.Fatalf("enter handled=%v quit=%v", handled, quit)
	}
	if tab.hintActive {
		t.Fatal("hint stayed active")
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.HintResult{Target: 3}) {
		t.Fatalf("commands = %#v", s.cmds)
	}
}

func TestHintPreviewCopiesDestination(t *testing.T) {
	var clipboard strings.Builder
	tab, s := hintPreviewTab(t, &clipboard)
	if handled, quit := tab.handleShellKey("y"); !handled || quit {
		t.Fatalf("y handled=%v quit=%v", handled, quit)
	}
	if got := decodeOSC52(t, clipboard.String()); got != "https://example.com/next" {
		t.Fatalf("clipboard = %q", got)
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.HintResult{Cancel: true}) {
		t.Fatalf("commands = %#v", s.cmds)
	}
	if tab.hintActive {
		t.Fatal("hint stayed active")
	}
}

func TestLinkPreviewEventFollowsAndCopies(t *testing.T) {
	var clipboard strings.Builder
	s := &recordSession{}
	tab := &Tab{Session: s, Stderr: io.Discard}
	tab.State.Page = protocol.State{FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2}
	tab.State.Clipboard = &clipboard
	if _, err := tab.applyEvent(protocol.Event{Body: protocol.LinkPreview{URL: "https://example.com/preview", Target: 4}}); err != nil {
		t.Fatal(err)
	}
	if !tab.linkPreviewActive || !strings.Contains(tab.State.Page.FooterLeft, "Enter follows") {
		t.Fatalf("preview active=%v footer=%q", tab.linkPreviewActive, tab.State.Page.FooterLeft)
	}
	if handled, quit := tab.handleShellKey("enter"); !handled || quit {
		t.Fatalf("enter handled=%v quit=%v", handled, quit)
	}
	if len(s.cmds) != 1 || s.cmds[0].Body != (protocol.FollowLink{Target: 4}) {
		t.Fatalf("commands = %#v", s.cmds)
	}

	if _, err := tab.applyEvent(protocol.Event{Body: protocol.LinkPreview{URL: "https://example.com/copy", Target: 5}}); err != nil {
		t.Fatal(err)
	}
	if handled, quit := tab.handleShellKey("y"); !handled || quit {
		t.Fatalf("y handled=%v quit=%v", handled, quit)
	}
	if got := decodeOSC52(t, clipboard.String()); got != "https://example.com/copy" {
		t.Fatalf("clipboard = %q", got)
	}
}
