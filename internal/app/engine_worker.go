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
	engineWorkerEnv   = "MOB3_ENGINE_WORKER"
	engineConfigEnv   = "MOB3_ENGINE_CONFIG"
	engineWorkerQueue = 8
)

// engineWorkerConfig is the geometry the parent supplies to one child. The
// child parses the normal CLI flags for policy so the two processes share one
// option surface.
type engineWorkerConfig struct {
	Width, Height int
	CellW, CellH  int
	Zoom          float64
	Graphics      int
	TabID         uint64
	SessionID     uint32
}

// runEngineWorker is the hidden child mode. It runs only the engine side over
// stdin/stdout; the parent owns the terminal, host policy and shared state.
func runEngineWorker(o runOptions) int {
	if o.MemLimit > 0 {
		debug.SetMemoryLimit(int64(o.MemLimit) << 20)
	}
	var cfg engineWorkerConfig
	if raw := os.Getenv(engineConfigEnv); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			fmt.Fprintf(os.Stderr, "mob3 worker: config: %s\n", cleanError(err))
			return 2
		}
	}
	if cfg.Width < 20 {
		cfg.Width = 80
	}
	if cfg.Height < 5 {
		cfg.Height = 24
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
	loader, err := engine.NewConfiguredLoader(remoteHost, loaderConfigFor(o, graphicsMode, true))
	if err != nil {
		fmt.Fprintf(os.Stderr, "mob3 worker: loader: %s\n", cleanError(err))
		return 2
	}

	browserContext := engine.NewBrowserContext()
	browserContext.SetEntryIDPrefix(cfg.TabID << 32)
	browserContext.SetTabIDPrefix(cfg.TabID << 32)
	browserContext.SetStorageProvider(storageClient)
	engine.SetZoom(zoomFactor)
	sessions := newInteractiveSessionFactory(o, browserContext, budget, zoomFactor, cfg.CellW, cfg.CellH, graphicsMode)
	session := sessions.newSession(loader, cfg.Width, cfg.Height)
	if err := protocol.ServePeerReady(ctx, peer, protocol.SessionID(cfg.SessionID), session, protocol.DefaultLimits, engineWorkerQueue); err != nil {
		fmt.Fprintf(os.Stderr, "mob3 worker: session: %s\n", cleanError(err))
		return 1
	}
	return 0
}
