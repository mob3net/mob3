package app

import (
	"os"
	"testing"
	"time"

	"browser/internal/engine"
	"browser/internal/frontend"
	"browser/internal/host"
	"browser/internal/protocol"
)

// TestProcessWorkerHelper is the child side of the process-backend tests. The
// real binary reaches the same code through app.Run and the hidden worker env.
func TestProcessWorkerHelper(t *testing.T) {
	if os.Getenv("MOB3_PROCESS_TEST_HELPER") != "1" {
		return
	}
	os.Exit(runProcessWorker(runOptions{
		Home: "about:home", Graphics: "none", LayoutBudget: "standard",
		Zoom: "100", TextScale: "auto", Certs: "system", Process: "per-tab",
	}))
}

func processTestFactory() tabBackendFactory {
	return startProcessBackend(
		host.NewSession(), engine.NewBrowserContext(),
		os.Args[0],
		[]string{"-test.run=^TestProcessWorkerHelper$"},
		[]string{"MOB3_PROCESS_TEST_HELPER=1"},
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
