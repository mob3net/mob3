package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"browser/internal/engine"
	"browser/internal/frontend"
	"browser/internal/host"
	"browser/internal/protocol"
)

// TestProcessWorkerHelper is the child side of the process-backend tests. The
// real binary reaches the same code through app.Run and the worker process env.
func TestProcessWorkerHelper(t *testing.T) {
	if os.Getenv("MOB3_PROCESS_TEST_HELPER") != "1" {
		return
	}
	if os.Getenv("MOB3_PROCESS_TEST_FAIL") == "1" {
		fmt.Fprint(os.Stderr, "\x1b[31mfatal worker test\n")
		os.Exit(3)
	}
	os.Exit(runProcessWorker(runOptions{
		Home: "about:home", Graphics: "none", LayoutBudget: "standard",
		Zoom: "100", TextScale: "auto", Certs: "system", Process: "per-tab",
	}))
}

func processTestFactory() tabBackendFactory {
	return processTestFactoryWithEnv(nil)
}

func processTestFactoryWithEnv(extra []string) tabBackendFactory {
	env := append([]string{"MOB3_PROCESS_TEST_HELPER=1"}, extra...)
	return startProcessBackend(
		host.NewSession(), engine.NewBrowserContext(),
		os.Args[0],
		[]string{"-test.run=^TestProcessWorkerHelper$"},
		env,
		0, 1, int(frontend.GraphicsNone),
	)
}

func TestProcessBackendLifecycle(t *testing.T) {
	backend, err := processTestFactory()(nil, 1, protocol.Init{Width: 80, Height: 24, CellW: 8, CellH: 16}, "about:help")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Kill()
	select {
	case ev, ok := <-backend.Session().Events():
		if !ok {
			t.Fatal("event stream closed before the first event")
		}
		if _, ok := ev.Body.(protocol.State); !ok {
			if _, ok := ev.Body.(protocol.Frame); !ok {
				t.Fatalf("first event = %T", ev.Body)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not produce an event")
	}
	backend.Close()
	select {
	case <-backend.(*processTabBackend).done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker process did not exit")
	}
}

func TestBoundedStderrSanitizesAndBounds(t *testing.T) {
	b := newBoundedStderr()
	_, _ = b.Write([]byte("\x1b[31mfatal\x1b[0m\nmessage\n"))
	text := b.Text()
	if strings.Contains(text, "\x1b") || strings.Contains(text, "\n") {
		t.Fatalf("stderr = %q", text)
	}
	if !strings.Contains(text, "fatal") || !strings.Contains(text, "message") {
		t.Fatalf("stderr = %q", text)
	}
	big := strings.Repeat("x", maxChildStderrBytes+1024)
	_, _ = b.Write([]byte(big))
	if n := len([]rune(b.Text())); n > maxChildStderrRunes {
		t.Fatalf("stderr tail = %d runes", n)
	}

	both := newBoundedStderr()
	_, _ = both.Write([]byte("fatal error: stack overflow\n"))
	_, _ = both.Write([]byte(strings.Repeat("frame ", maxChildStderrBytes)))
	_, _ = both.Write([]byte("\nlast frame"))
	text = both.Text()
	if !strings.Contains(text, "fatal error: stack overflow") {
		t.Fatalf("stderr beginning was dropped: %q", text[:min(len(text), 200)])
	}
	if !strings.Contains(text, "last frame") {
		t.Fatalf("stderr end was dropped: %q", text[max(0, len(text)-200):])
	}
	if !strings.Contains(text, "…") {
		t.Fatalf("truncated stderr has no marker: %q", text[:min(len(text), 200)])
	}
}

func TestProcessBackendCapturesStderr(t *testing.T) {
	factory := processTestFactoryWithEnv([]string{"MOB3_PROCESS_TEST_FAIL=1"})
	_, err := factory(nil, 1, protocol.Init{Width: 80, Height: 24, CellW: 8, CellH: 16}, "about:help")
	if err == nil {
		t.Fatal("failing worker started successfully")
	}
	message := err.Error()
	if strings.Contains(message, "\x1b") {
		t.Fatalf("raw terminal escape in %q", message)
	}
	if !strings.Contains(message, "fatal worker test") {
		t.Fatalf("stderr tail missing from %q", message)
	}
}

func TestProcessBackendKillReaps(t *testing.T) {
	backend, err := processTestFactory()(nil, 2, protocol.Init{Width: 80, Height: 24, CellW: 8, CellH: 16}, "about:help")
	if err != nil {
		t.Fatal(err)
	}
	backend.Kill()
	select {
	case <-backend.(*processTabBackend).done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker was not reaped after Kill")
	}
}

// TestAutomationProcessWorker drives the automation worker kind through a real
// child and checks that a killed engine keeps reporting the crash to later
// calls instead of hanging or being respawned.
func TestAutomationProcessWorker(t *testing.T) {
	eng, err := spawnProcessEngine(
		host.NewSession(), engine.NewBrowserContext(),
		os.Args[0],
		[]string{"-test.run=^TestProcessWorkerHelper$"},
		[]string{"MOB3_PROCESS_TEST_HELPER=1"},
		processWorkerConfig{
			Kind: workerKindAutomation, Width: 80, Height: 24,
			Zoom: 1, TabID: 1, SessionID: 1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Kill()
	client := &automationClient{s: eng.Session(), exit: eng}
	if err := client.Navigate(context.Background(), "about:help"); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if snap := client.Snapshot(); !strings.Contains(snap, "MOB3") {
		t.Fatalf("snapshot = %q", snap)
	}
	eng.Kill()
	select {
	case <-eng.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("worker was not reaped after Kill")
	}
	err = client.Navigate(context.Background(), "about:help")
	if err == nil {
		t.Fatal("navigation succeeded after the engine was killed")
	}
	if !strings.Contains(err.Error(), "engine") {
		t.Fatalf("crash error = %q", err)
	}
	if err := client.Navigate(context.Background(), "about:help"); err == nil {
		t.Fatal("a later call did not report the dead engine")
	}
}
