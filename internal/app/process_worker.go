package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"

	"browser/internal/engine"
	"browser/internal/frontend"
	"browser/internal/protocol"
)

const (
	processWorkerEnv   = "MOB3_PROCESS_WORKER"
	processConfigEnv   = "MOB3_PROCESS_CONFIG"
	processWorkerQueue = 8

	workerKindTab        = "tab"
	workerKindAutomation = "automation"
)

// processWorkerConfig is the placement and geometry the parent supplies to one
// child. The child parses the normal CLI flags for policy so the two processes
// share one option surface.
type processWorkerConfig struct {
	Kind          string
	Width, Height int
	CellW, CellH  int
	Zoom          float64
	Graphics      int
	TabID         uint64
	SessionID     uint32
}

// runProcessWorker is the engine worker mode. It runs only the engine side over
// stdin/stdout; the parent owns the terminal or MCP stdio, host policy and
// shared state.
func runProcessWorker(o runOptions) int {
	if o.MemLimit > 0 {
		debug.SetMemoryLimit(int64(o.MemLimit) << 20)
	}
	var cfg processWorkerConfig
	if raw := os.Getenv(processConfigEnv); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			fmt.Fprintf(os.Stderr, "mob3 worker: config: %s\n", cleanError(err))
			return 2
		}
	}
	switch cfg.Kind {
	case "":
		cfg.Kind = workerKindTab
	case workerKindTab, workerKindAutomation:
	default:
		fmt.Fprintf(os.Stderr, "mob3 worker: unknown kind %q\n", cfg.Kind)
		return 2
	}
	if cfg.Kind == workerKindTab {
		if cfg.Width < 20 {
			cfg.Width = 80
		}
		if cfg.Height < 5 {
			cfg.Height = 24
		}
	}
	if cfg.CellW < 1 {
		cfg.CellW = 8
	}
	if cfg.CellH < 1 {
		cfg.CellH = 16
	}
	budget, ok := engine.ParseLayoutBudget(o.LayoutBudget)
	if !ok {
		fmt.Fprintf(os.Stderr, "mob3 worker: invalid -layout-budget %q\n", o.LayoutBudget)
		return 2
	}
	zoomPct, ok := engine.ParseZoomPercent(o.Zoom)
	if !ok {
		fmt.Fprintf(os.Stderr, "mob3 worker: invalid -zoom %q\n", o.Zoom)
		return 2
	}
	zoomFactor := zoomPct / 100
	graphicsMode := frontend.GraphicsMode(cfg.Graphics)
	if graphicsMode == frontend.GraphicsAuto {
		graphicsMode = frontend.GraphicsNone
	}

	peer := protocol.NewMuxPeer(os.Stdin, os.Stdout, protocol.DefaultLimits)
	hostClient := protocol.NewHostClientPeer(peer)
	storageClient := protocol.NewStorageClientPeer(peer)
	ctx := context.Background()
	peer.Start(ctx)
	remoteHost := protocol.NewRemoteHost(hostClient)
	loader, err := engine.NewConfiguredLoader(remoteHost, loaderConfigFor(o, graphicsMode, cfg.Kind == workerKindTab))
	if err != nil {
		fmt.Fprintf(os.Stderr, "mob3 worker: loader: %s\n", cleanError(err))
		return 2
	}

	browserContext := engine.NewBrowserContext()
	browserContext.SetEntryIDPrefix(cfg.TabID << 32)
	browserContext.SetTabIDPrefix(cfg.TabID << 32)
	browserContext.SetStorageProvider(storageClient)
	engine.SetZoom(zoomFactor)
	var handler protocol.Handler
	switch cfg.Kind {
	case workerKindAutomation:
		auto := engine.NewAutomationWithConfig(engine.AutomationConfig{
			Loader: loader, BrowserContext: browserContext,
			Width: cfg.Width, Height: cfg.Height, Zoom: zoomFactor,
			NoJS: o.NoJS, NoPDF: o.NoPDF,
			MemLimitBytes: int64(o.MemLimit) << 20, LayoutBudget: budget,
			Home: o.Home, Reader: o.Reader, Dark: o.Dark,
		})
		auto.SetPNGEncoder(frontend.RenderPNG)
		handler = &automationHandler{a: auto}
	default:
		sessions := newInteractiveSessionFactory(o, browserContext, budget, zoomFactor, cfg.CellW, cfg.CellH, graphicsMode)
		handler = sessions.newSession(loader, cfg.Width, cfg.Height)
	}
	if err := protocol.ServePeerReady(ctx, peer, protocol.SessionID(cfg.SessionID), handler, protocol.DefaultLimits, processWorkerQueue); err != nil {
		fmt.Fprintf(os.Stderr, "mob3 worker: session: %s\n", cleanError(err))
		return 1
	}
	return 0
}
