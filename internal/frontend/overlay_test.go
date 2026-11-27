package frontend

import (
	"fmt"
	"testing"

	"browser/internal/protocol"
)

// TestOverlayDispatchOrder locks the single priority order shared by the key,
// mouse and page-state paths: the first open window owns input.
func TestOverlayDispatchOrder(t *testing.T) {
	l := &Tab{}
	want := []string{
		"*frontend.linkPreviewOverlay",
		"*frontend.dialogOverlay",
		"*frontend.consoleOverlay",
		"*frontend.hintOverlay",
		"*frontend.downloadOverlay",
		"*frontend.noticeOverlay",
		"*frontend.selectOverlay",
		"*frontend.configOverlay",
		"*frontend.historyOverlay",
		"*frontend.promptOverlay",
		"*frontend.findOverlay",
		"*frontend.menuOverlay",
	}
	list := l.overlayList()
	if len(list) != len(want) {
		t.Fatalf("windows = %d, want %d", len(list), len(want))
	}
	for i, o := range list {
		if got := fmt.Sprintf("%T", o); got != want[i] {
			t.Fatalf("window %d = %s, want %s", i, got, want[i])
		}
	}
}

// TestDismissOverlaysClosesOnlyModals checks that opening a new modal closes
// the shell and popup windows but leaves the dialogs that are answered
// explicitly, link hints and the console alone.
func TestDismissOverlaysClosesOnlyModals(t *testing.T) {
	l := &Tab{Session: &recordSession{}}
	l.promptActive = true
	l.findActive = true
	l.menuKind = protocol.MenuView
	l.selectActive = true
	l.historyActive = true
	l.linkPreviewActive = true
	l.downloadActive = true
	l.noticeActive = true
	l.dialogActive = true
	l.hintActive = true
	l.consoleActive = true

	l.dismissOverlays()

	closed := map[string]bool{
		"prompt":      l.promptActive,
		"find":        l.findActive,
		"menu":        l.menuKind != protocol.MenuNone,
		"select":      l.selectActive,
		"history":     l.historyActive,
		"linkPreview": l.linkPreviewActive,
		"download":    l.downloadActive,
		"notice":      l.noticeActive,
	}
	for name, open := range closed {
		if open {
			t.Errorf("%s stayed open", name)
		}
	}
	kept := map[string]bool{
		"dialog":  l.dialogActive,
		"hint":    l.hintActive,
		"console": l.consoleActive,
	}
	for name, open := range kept {
		if !open {
			t.Errorf("%s closed", name)
		}
	}
}
