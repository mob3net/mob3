package app

import (
	"browser/internal/browser"
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"browser/internal/protocol"
)

func TestAutomationTransportRoundTrip(t *testing.T) {
	auto := browser.NewAutomationWithConfig(browser.AutomationConfig{Width: 60, Height: 20, NoJS: true})
	session, err := (&protocol.InProc{EventQueue: 8}).Open(context.Background(), 1, &automationHandler{a: auto})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	client := &automationClient{s: session}
	ctx := context.Background()
	if err := client.Navigate(ctx, "data:text/html,<h1>Hello transport</h1>"); err != nil {
		t.Fatal(err)
	}
	if snap := client.Snapshot(); !bytes.Contains([]byte(snap), []byte("Hello transport")) {
		t.Fatalf("snapshot = %q", snap)
	}
	data, err := client.Screenshot(1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("screenshot is not a PNG: %x", data[:min(len(data), 8)])
	}
	text, err := client.RunCommand(ctx, "snapshot", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(text), []byte("Hello transport")) {
		t.Fatalf("command snapshot = %q", text)
	}
	if _, err := client.RunCommand(ctx, "does-not-exist", nil); err == nil {
		t.Fatal("unknown command succeeded")
	}
}
