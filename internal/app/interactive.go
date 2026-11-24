package app

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"browser/internal/engine"
	"browser/internal/frontend"
	"browser/internal/host"
	"browser/internal/protocol"
)

// runInteractive boots the terminal session after flags, loader and shared
// browser context are ready. It falls back to the one-shot dump path when the
// terminal cannot enter raw mode.
func runInteractive(o runOptions, hostSess *host.Session, loader *engine.Loader, browserContext *engine.BrowserContext, graphicsMode frontend.GraphicsMode, budget engine.LayoutBudget, zoomFactor float64) int {
	stdin := os.Stdin
	state, err := frontend.MakeRaw(stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mob3: interactive mode unavailable (%s); rendering to stdout.\n", cleanError(err))
		return runDump(o, loader, budget, zoomFactor)
	}
	w, h, ok := frontend.TerminalSize(os.Stdout)
	if !ok {
		w, h = 100, 30
	}
	if o.Width > 0 {
		w = o.Width
	}
	cellW, cellH, ok := frontend.TerminalCellPixels(os.Stdout)
	if !ok || cellW <= 0 {
		cellW = 8
	}
	if !ok || cellH <= 0 {
		cellH = 16
	}
	noBigText := false
	switch strings.ToLower(strings.TrimSpace(o.TextScale)) {
	case "off", "none", "false":
		noBigText = true
	}
	b := engine.NewConfiguredSession(engine.SessionConfig{
		Loader: loader, BrowserContext: browserContext,
		Width: w, Height: h, Zoom: zoomFactor, Interactive: true,
		Reader: o.Reader, Dark: o.Dark, Home: o.Home,
		CLIConfig: map[string]string{
			"-dump":          onOff(o.Dump),
			"-png":           onOff(o.PNG),
			"-width":         strconv.Itoa(w),
			"-height":        strconv.Itoa(o.Height),
			"-pdf-page":      o.PDFPageLabel,
			"-graphics":      o.Graphics,
			"-mem-limit":     strconv.Itoa(o.MemLimit),
			"-layout-budget": budget.Name,
			"-zoom":          engine.ZoomPercentLabel(zoomFactor),
			"-color":         onOff(o.Color),
			"-no-color":      onOff(o.NoColor),
			"-certs":         loader.CertSource(),
			"-mcp":           onOff(o.MCP),
			"-version":       Version,
		},
		Color: !o.NoColor, NoJS: o.NoJS, NoPDF: o.NoPDF,
		LayoutBudget:  budget,
		MemLimitBytes: int64(o.MemLimit) << 20, NoBigText: noBigText,
		CellW: cellW, CellH: cellH, Graphics: graphicsMode,
		GraphicsModes: graphicsModesFor(graphicsMode),
		Caps:          capabilitiesFor(nativeGraphicsMode(graphicsMode), cellW, cellH),
	})
	b.SetPNGEncoder(frontend.RenderPNG)
	presenter := frontend.NewPresenter(os.Stdout, b.Color())
	presenterMode := graphicsMode
	if presenterMode == frontend.GraphicsAuto || presenterMode == frontend.GraphicsBlocks || presenterMode == frontend.GraphicsNone {
		presenterMode = frontend.DetectGraphics(frontend.GraphicsAuto)
	}
	presenter.SetGraphics(presenterMode, cellW, cellH)

	kittyKeys := frontend.KittyKeyboardEnabled()
	var startSeq strings.Builder
	startSeq.WriteString("\x1b[?1049h\x1b[?25l\x1b[?7l\x1b[2J\x1b[?1000h\x1b[?1003h\x1b[?1006h")
	if kittyKeys {
		startSeq.WriteString("\x1b[>19u")
	}
	_, _ = os.Stdout.WriteString(startSeq.String())
	cleanup := func() {
		var sb strings.Builder
		if kittyKeys {
			sb.WriteString("\x1b[<u")
		}
		sb.WriteString("\x1b[?1003l\x1b[?1000l\x1b[?1006l")
		b.Cleanup()
		presenter.ClearGraphics(&sb)
		sb.WriteString("\x1b[0m\x1b[?7h\x1b[?25h\x1b[?1049l")
		_, _ = os.Stdout.WriteString(sb.String())
		frontend.RestoreTerm(stdin, state)
	}
	defer cleanup()

	keys := make(chan string, 32)
	go frontend.ReadKeyEvents(stdin, keys, nil)

	winch := make(chan os.Signal, 1)
	quitSig := make(chan os.Signal, 1)
	frontend.StartSignalHandlers(winch, quitSig)
	defer frontend.StopSignalHandlers(winch, quitSig)

	session, err := (&protocol.InProc{}).Open(context.Background(), 1, b)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mob3: session: %s\n", cleanError(err))
		return 1
	}
	defer session.Close()
	top, contentH := frontend.ContentBounds(h)
	if err := session.Send(protocol.Command{Body: protocol.Init{
		Width: w, Height: h, ContentTop: top, ContentHeight: contentH,
		CellW: cellW, CellH: cellH, Zoom: zoomFactor, Caps: b.Capabilities(),
	}}); err != nil {
		fmt.Fprintf(os.Stderr, "mob3: session init: %s\n", cleanError(err))
		return 1
	}
	if err := session.Send(protocol.Command{Body: protocol.Navigate{URL: o.Target, Push: true}}); err != nil {
		fmt.Fprintf(os.Stderr, "mob3: session navigate: %s\n", cleanError(err))
		return 1
	}
	tab := &frontend.Tab{
		Session: session, Presenter: presenter, Stderr: os.Stderr,
		AttestedURL: hostSess.AttestedURL,
	}
	tab.State.Clipboard = os.Stdout
	loop := &frontend.SessionLoop{
		Tabs: []*frontend.Tab{tab}, Active: tab,
		Keys: keys, Resize: winch, Quit: quitSig,
		TerminalSize: func() (int, int, bool) { return frontend.TerminalSize(os.Stdout) },
		CellW:        cellW, CellH: cellH,
	}
	if err := loop.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "mob3: %s\n", cleanError(err))
		return 1
	}
	return 0
}
