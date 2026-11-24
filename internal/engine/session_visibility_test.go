package engine

import (
	"testing"

	"browser/internal/protocol"
)

func TestActivationResamplesProcessMemory(t *testing.T) {
	b := NewSession(80, 24)
	b.usage.rss = 0
	b.SetActive(false)
	b.SetActive(true)
	if b.usage.rss == 0 {
		t.Fatal("activation did not refresh the process-wide memory figure")
	}
	if footer := b.UsageFooter(); footer == "" || footer == "MEM 0MB" {
		t.Fatalf("usage footer = %q", footer)
	}
}

func TestActivationSyncsBackgroundTabZoom(t *testing.T) {
	defer SetZoom(1)
	b := newSessionWithBrowserContext(80, 24, nil, false)
	b.Zoom = 1
	b.zoomGen = zoomGen()
	SetZoom(1.5)
	b.SetActive(false)
	b.SetActive(true)
	if b.Zoom != 1.5 {
		t.Fatalf("zoom = %v, want the browser-wide 1.5", b.Zoom)
	}
	if b.zoomGen != zoomGen() {
		t.Fatal("zoom generation not synchronized")
	}
}

func TestPreserveZoomJoinsCurrentBrowserZoom(t *testing.T) {
	defer SetZoom(1)
	SetZoom(1.75)
	b := NewConfiguredSession(SessionConfig{Width: 80, Height: 24, PreserveZoom: true})
	if b.Zoom != 1.75 {
		t.Fatalf("zoom = %v, want 1.75", b.Zoom)
	}
	if Zoom() != 1.75 {
		t.Fatalf("browser zoom = %v, want 1.75", Zoom())
	}
}

func TestVisibilityCommandSuspendsBackgroundTab(t *testing.T) {
	b := NewSession(80, 24)
	canceled := false
	b.DeferredImages = true
	b.ImageCancel = func() { canceled = true }
	b.Progress = 0.5
	b.hoverRebuildPending = true

	if _, err := b.HandleCommand(protocol.Command{Body: protocol.Visibility{Active: false}}, b); err != nil {
		t.Fatal(err)
	}
	if b.Active() {
		t.Fatal("session still active")
	}
	if !canceled || b.DeferredImages {
		t.Fatalf("deferred image load not canceled: canceled=%v deferred=%v", canceled, b.DeferredImages)
	}
	if b.Progress != 0 {
		t.Fatalf("progress = %v, want cleared", b.Progress)
	}
	if b.hoverRebuildPending {
		t.Fatal("hover rebuild still pending in a background tab")
	}

	if _, err := b.HandleCommand(protocol.Command{Body: protocol.Visibility{Active: true}}, b); err != nil {
		t.Fatal(err)
	}
	if !b.Active() {
		t.Fatal("session did not resume")
	}
}

func TestGraphicsCommandAppliesBrowserMode(t *testing.T) {
	b := NewSession(80, 24)
	if _, err := b.HandleCommand(protocol.Command{Body: protocol.Graphics{Mode: protocol.GraphicsNone}}, b); err != nil {
		t.Fatal(err)
	}
	if b.graphicsMode != GraphicsNone {
		t.Fatalf("graphics mode = %v, want none", b.graphicsMode)
	}
	if _, err := b.HandleCommand(protocol.Command{Body: protocol.Graphics{Mode: 99}}, b); err != nil {
		t.Fatal(err)
	}
	if b.graphicsMode != GraphicsNone {
		t.Fatalf("invalid mode changed graphics mode to %v", b.graphicsMode)
	}
}
