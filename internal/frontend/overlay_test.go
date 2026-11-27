package frontend

import (
	"fmt"
	"testing"

	"browser/internal/protocol"
)

// TestWindowStackOrder locks the layer order shared by the key, mouse and
// page-state paths: the topmost open window owns input first.
func TestWindowStackOrder(t *testing.T) {
	l := &Tab{}
	l.linkPreviewActive = true
	l.consoleActive = true
	l.hintActive = true
	l.downloadActive = true
	l.noticeActive = true
	l.selectActive = true
	l.configActive = true
	l.historyActive = true
	l.dialogActive = true
	l.promptActive = true
	l.findActive = true
	l.menuKind = protocol.MenuView
	want := []string{
		"*frontend.dialogOverlay",
		"*frontend.downloadOverlay",
		"*frontend.noticeOverlay",
		"*frontend.selectOverlay",
		"*frontend.configOverlay",
		"*frontend.historyOverlay",
		"*frontend.menuOverlay",
		"*frontend.linkPreviewOverlay",
		"*frontend.promptOverlay",
		"*frontend.findOverlay",
		"*frontend.consoleOverlay",
		"*frontend.hintOverlay",
	}
	stack, count := l.windowStack()
	if count != len(want) {
		t.Fatalf("windows = %d, want %d", count, len(want))
	}
	for i := 0; i < count; i++ {
		if got := fmt.Sprintf("%T", stack[i]); got != want[i] {
			t.Fatalf("window %d = %s, want %s", i, got, want[i])
		}
	}
}

// TestWindowStackOnlyOpen checks that closed windows stay out of dispatch.
func TestWindowStackOnlyOpen(t *testing.T) {
	l := &Tab{}
	l.consoleActive = true
	if stack, count := l.windowStack(); count != 1 || stack[0] != &l.consoleOverlay {
		t.Fatalf("stack = %d %v", count, stack[:count])
	}
	l.consoleActive = false
	if _, count := l.windowStack(); count != 0 {
		t.Fatalf("closed console stayed in the stack: %d", count)
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
