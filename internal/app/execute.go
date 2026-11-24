package app

import (
	"fmt"
	"os"
	"runtime/debug"

	"browser/internal/engine"
	"browser/internal/frontend"
	"browser/internal/host"
)

// execute runs one MOB³ invocation after flag parsing and validation.
func execute(o runOptions) int {
	if o.MemLimit > 0 {
		debug.SetMemoryLimit(int64(o.MemLimit) << 20)
	}
	if o.PNG {
		o.Dump = true
	}
	interactive := !o.Dump && frontend.IsTTY(os.Stdin) && frontend.IsTTY(os.Stdout)
	graphicsMode := frontend.ParseGraphicsMode(o.Graphics)
	if graphicsMode == frontend.GraphicsAuto {
		graphicsMode = frontend.DetectGraphics(frontend.GraphicsAuto)
	}
	hostSess := host.NewSession()
	browserContext := engine.NewBrowserContext()
	loader, err := engine.NewConfiguredLoader(hostSess, engine.LoaderConfig{
		NoScripts:    o.NoJS,
		NoPDF:        o.NoPDF,
		NoImages:     graphicsMode == frontend.GraphicsNone,
		IgnoreBlocks: o.IgnoreBlocks,
		Search:       o.Search,
		UserAgent:    o.UA,
		Certs:        o.Certs,
		DeferImages:  interactive && !o.MCP,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "mob3: -certs: %s\n", cleanError(err))
		return 2
	}
	budget, ok := engine.ParseLayoutBudget(o.LayoutBudget)
	if !ok {
		fmt.Fprintf(os.Stderr, "mob3: invalid -layout-budget %q (use low, standard, high or none)\n", o.LayoutBudget)
		return 2
	}
	zoomPct, ok := engine.ParseZoomPercent(o.Zoom)
	if !ok {
		fmt.Fprintf(os.Stderr, "mob3: invalid -zoom %q (use a percentage from 25 to 200)\n", o.Zoom)
		return 2
	}
	frontend.SetColorMode(frontend.DetectColorMode())
	zoomFactor := zoomPct / 100

	if o.MCP {
		return runMCP(o, loader, browserContext, budget, zoomFactor)
	}
	if !interactive {
		return runDump(o, loader, budget, zoomFactor)
	}
	return runInteractive(o, hostSess, loader, browserContext, graphicsMode, budget, zoomFactor)
}
