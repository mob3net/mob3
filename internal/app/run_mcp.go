package app

import (
	"context"
	"fmt"
	"os"

	"browser/internal/engine"
	"browser/internal/frontend"
	"browser/internal/host"
	"browser/internal/protocol"
)

// runMCP starts one headless automation session and serves the MCP transport.
// The default keeps the engine in a child process so an engine crash cannot
// take down the server; -process=single keeps it in this process.
func runMCP(o runOptions, hostSess *host.Session, browserContext *engine.BrowserContext, graphicsMode frontend.GraphicsMode, budget engine.LayoutBudget, zoomFactor float64) int {
	var runner automationRunner
	stop := func() {}
	if o.Process == "single" {
		loader, err := engine.NewConfiguredLoader(hostSess, loaderConfigFor(o, graphicsMode, false))
		if err != nil {
			fmt.Fprintf(os.Stderr, "mob3: -certs: %s\n", cleanError(err))
			return 2
		}
		auto := engine.NewAutomationWithConfig(engine.AutomationConfig{
			Loader: loader, BrowserContext: browserContext,
			Width: o.Width, Height: o.Height, Zoom: zoomFactor,
			NoJS: o.NoJS, NoPDF: o.NoPDF,
			MemLimitBytes: int64(o.MemLimit) << 20, LayoutBudget: budget,
			Home: o.Home, Reader: o.Reader, Dark: o.Dark,
		})
		auto.SetPNGEncoder(frontend.RenderPNG)
		session, err := (&protocol.InProc{EventQueue: 8}).Open(context.Background(), 1, &automationHandler{a: auto})
		if err != nil {
			fmt.Fprintf(os.Stderr, "mob3 mcp: session: %s\n", cleanError(err))
			return 1
		}
		stop = func() { _ = session.Close() }
		runner = &automationClient{s: session}
	} else {
		eng, err := spawnProcessEngine(hostSess, browserContext, executablePath(), os.Args[1:], nil, processWorkerConfig{
			Kind:  workerKindAutomation,
			Width: o.Width, Height: o.Height,
			Zoom: zoomFactor, Graphics: int(graphicsMode),
			TabID: 1, SessionID: 1,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "mob3 mcp: engine: %s\n", cleanError(err))
			return 1
		}
		stop = eng.Close
		runner = &automationClient{s: eng.Session(), exit: eng}
	}
	defer stop()
	ctx := context.Background()
	if err := runner.Navigate(ctx, o.Target); err != nil {
		fmt.Fprintf(os.Stderr, "mob3 mcp: initial page: %s\n", cleanError(err))
	}
	return RunMCP(runner, os.Stdin, os.Stdout, os.Stderr)
}
