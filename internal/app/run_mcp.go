package app

import (
	"context"
	"fmt"
	"os"

	"browser/internal/engine"
	"browser/internal/frontend"
	"browser/internal/protocol"
)

// runMCP starts one headless automation session and serves the MCP transport.
func runMCP(o runOptions, loader *engine.Loader, browserContext *engine.BrowserContext, budget engine.LayoutBudget, zoomFactor float64) int {
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
	defer session.Close()
	client := &automationClient{s: session}
	ctx := context.Background()
	if err := client.Navigate(ctx, o.Target); err != nil {
		fmt.Fprintf(os.Stderr, "mob3 mcp: initial page: %s\n", cleanError(err))
	}
	return RunMCP(client, os.Stdin, os.Stdout, os.Stderr)
}
