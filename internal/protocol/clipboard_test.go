package protocol

import "testing"

func TestSanitizeClipboardKeepsTextAndLayout(t *testing.T) {
	in := "a\tb\nc\x1b\x07d\u202e\u200b\r"
	want := "a\tb\ncd"
	// The example drops ESC, BEL, bidi override, zero-width space and CR,
	// while preserving the tab and newline.
	if got := SanitizeClipboard(in); got != want {
		t.Fatalf("SanitizeClipboard = %q, want %q", got, want)
	}
}
