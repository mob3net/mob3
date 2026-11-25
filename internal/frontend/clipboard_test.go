package frontend

import (
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"browser/internal/protocol"
)

func decodeOSC52(t *testing.T, raw string) string {
	t.Helper()
	if !strings.HasPrefix(raw, "\x1b]52;c;") || !strings.HasSuffix(raw, "\x07") {
		t.Fatalf("not an OSC 52 sequence: %q", raw)
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(raw, "\x1b]52;c;"), "\x07")
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return string(data)
}

func TestOSC52SanitizesAndCapsText(t *testing.T) {
	if got := decodeOSC52(t, OSC52("a\x1bb\x07c\u202e\n")); got != "abc\n" {
		t.Fatalf("sanitized clipboard = %q", got)
	}
	text := strings.Repeat("x", maxClipboardBytes+100)
	if got := decodeOSC52(t, OSC52(text)); len(got) != maxClipboardBytes {
		t.Fatalf("capped clipboard length = %d, want %d", len(got), maxClipboardBytes)
	}
}

func TestClipboardEventRequiresPendingCopy(t *testing.T) {
	var out strings.Builder
	loop := &Tab{State: LoopState{Clipboard: &out}, Stderr: io.Discard}
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.Clipboard{Text: "forged"}}); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("unsolicited clipboard event was written: %q", out.String())
	}

	loop.expectClipboard()
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.Clipboard{Text: "a\x1bb"}}); err != nil {
		t.Fatal(err)
	}
	if got := decodeOSC52(t, out.String()); got != "ab" {
		t.Fatalf("clipboard = %q", got)
	}

	out.Reset()
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.Clipboard{Text: "second"}}); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("clipboard permission was not one-shot: %q", out.String())
	}
}

func TestClipboardPendingPermissionExpires(t *testing.T) {
	var out strings.Builder
	loop := &Tab{State: LoopState{Clipboard: &out}, Stderr: io.Discard}
	loop.clipboardPendingUntil = time.Now().Add(-time.Second)
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.Clipboard{Text: "late"}}); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("expired clipboard permission was accepted: %q", out.String())
	}
}

func TestConsoleCopySetsClipboardPermission(t *testing.T) {
	loop := &Tab{Session: &recordSession{}, Stderr: io.Discard}
	loop.consoleActive = true
	loop.handleShellKey("y")
	if loop.clipboardPendingUntil.IsZero() {
		t.Fatal("console copy did not authorize a clipboard event")
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("no clipboard") }

func TestClipboardWriteFailureIsReported(t *testing.T) {
	loop := &Tab{State: LoopState{Clipboard: errorWriter{}}, Stderr: io.Discard}
	loop.expectClipboard()
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.Clipboard{Text: "x"}}); err != nil {
		t.Fatal(err)
	}
	if loop.State.Page.Status != "Clipboard unavailable" {
		t.Fatalf("clipboard failure status = %q", loop.State.Page.Status)
	}
}

func TestClipboardWithoutWriterIsReported(t *testing.T) {
	loop := &Tab{Stderr: io.Discard}
	loop.expectClipboard()
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.Clipboard{Text: "x"}}); err != nil {
		t.Fatal(err)
	}
	if loop.State.Page.Status != "Clipboard unavailable" {
		t.Fatalf("clipboard missing status = %q", loop.State.Page.Status)
	}
}
