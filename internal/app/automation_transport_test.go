package app

import (
	"browser/internal/engine"
	"browser/internal/frontend"
	"browser/internal/host"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"browser/internal/protocol"
)

func TestAutomationTransportRoundTrip(t *testing.T) {
	loader, err := engine.NewConfiguredLoader(host.NewSession(), engine.LoaderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	auto := engine.NewAutomationWithConfig(engine.AutomationConfig{Loader: loader, Width: 60, Height: 20, NoJS: true})
	auto.SetEncoders(frontend.EncodeFrame, frontend.RenderPNG)
	session, err := (&protocol.InProc{EventQueue: 8}).Open(context.Background(), 1, &automationHandler{a: auto})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	client := &automationClient{s: session}
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<h1>Hello transport</h1>`))
	}))
	defer srv.Close()
	if err := client.Navigate(ctx, srv.URL); err != nil {
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
