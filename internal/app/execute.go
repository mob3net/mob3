package app

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"browser/internal/engine"
	"browser/internal/frontend"
	"browser/internal/host"
	"browser/internal/protocol"
)

type runOptions struct {
	Dump, PNG     bool
	Width, Height int
	PDFPage       int
	PDFPageLabel  string
	NoColor       bool
	Color         bool
	UA, Certs     string
	Search        string
	Graphics      string
	NoJS, NoPDF   bool
	Reader, Dark  bool
	IgnoreBlocks  bool
	Home          string
	TextScale     string
	MemLimit      int
	LayoutBudget  string
	Zoom          string
	MCP           bool
	Target        string
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func graphicsFor(mode engine.GraphicsMode, w, h int) engine.Graphics {
	g := frontend.NewGFXRenderer(mode, w, h)
	if g == nil {
		return nil
	}
	return g
}

// graphicsModesFor records the i-key cycle: native inline graphics when
// available, then blocks, then none.
func graphicsModesFor(requested frontend.GraphicsMode) []engine.GraphicsMode {
	current := frontend.DetectGraphics(requested)
	native := current
	if native != frontend.GraphicsKitty && native != frontend.GraphicsITerm {
		if detected := frontend.DetectGraphics(frontend.GraphicsAuto); detected == frontend.GraphicsKitty || detected == frontend.GraphicsITerm {
			native = detected
		} else {
			native = frontend.GraphicsNone
		}
	}
	modes := make([]engine.GraphicsMode, 0, 3)
	if native == frontend.GraphicsKitty || native == frontend.GraphicsITerm {
		modes = append(modes, native)
	}
	modes = append(modes, frontend.GraphicsBlocks, frontend.GraphicsNone)
	if current == frontend.GraphicsAuto {
		current = native
	}
	return modes
}

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
		fmt.Fprintf(os.Stderr, "mob3: -certs: %v\n", err)
		return 2
	}
	jsMemLimit := engine.JSMemLimit()
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
	zoomFactor := zoomPct / 100
	frontend.SetColorMode(frontend.DetectColorMode())

	if o.MCP {
		auto := engine.NewAutomationWithConfig(engine.AutomationConfig{
			Loader: loader, Width: o.Width, Height: o.Height, Zoom: zoomFactor,
			NoJS: o.NoJS, NoPDF: o.NoPDF, JSMemLimit: jsMemLimit,
			MemLimitBytes: int64(o.MemLimit) << 20, LayoutBudget: budget,
			Home: o.Home, Reader: o.Reader, Dark: o.Dark,
		})
		auto.SetEncoders(frontend.EncodeFrame, frontend.RenderPNG)
		session, err := (&protocol.InProc{EventQueue: 8}).Open(context.Background(), 1, &automationHandler{a: auto})
		if err != nil {
			fmt.Fprintf(os.Stderr, "mob3 mcp: session: %v\n", err)
			return 1
		}
		defer session.Close()
		client := &automationClient{s: session}
		ctx := context.Background()
		if err := client.Navigate(ctx, o.Target); err != nil {
			fmt.Fprintf(os.Stderr, "mob3 mcp: initial page: %v\n", err)
		}
		return RunMCP(client, os.Stdin, os.Stdout, os.Stderr)
	}

	if !interactive {
		return dumpPage(loader, o.Target, o.Width, o.Height, o.NoColor, o.Color,
			o.NoJS, o.Reader, o.Dark, o.PNG, o.PDFPage, jsMemLimit, budget, zoomFactor)
	}

	stdin := os.Stdin
	state, err := frontend.MakeRaw(stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mob3: interactive mode unavailable (%v); rendering to stdout.\n", err)
		return dumpPage(loader, o.Target, o.Width, o.Height, o.NoColor, o.Color,
			o.NoJS, o.Reader, o.Dark, o.PNG, o.PDFPage, jsMemLimit, budget, zoomFactor)
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
		Loader: loader, Width: w, Height: h, Zoom: zoomFactor, Interactive: true,
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
		JSMemLimit: jsMemLimit, LayoutBudget: budget,
		MemLimitBytes: int64(o.MemLimit) << 20, NoBigText: noBigText,
		Output: os.Stdout,
		CellW:  cellW, CellH: cellH, Graphics: graphicsMode,
		GraphicsModes: graphicsModesFor(graphicsMode),
		GFX:           graphicsFor(graphicsMode, cellW, cellH),
		NewGraphics:   graphicsFor,
	})
	b.SetEncoders(frontend.EncodeFrame, frontend.RenderPNG)
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
		b.Cleanup(&sb)
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
		fmt.Fprintf(os.Stderr, "mob3: session: %v\n", err)
		return 1
	}
	defer session.Close()
	top, contentH := frontend.ContentBounds(h)
	if err := session.Send(protocol.Command{Body: protocol.Init{
		Width: w, Height: h, ContentTop: top, ContentHeight: contentH,
		CellW: cellW, CellH: cellH, Zoom: zoomFactor, Caps: b.Capabilities(),
	}}); err != nil {
		fmt.Fprintf(os.Stderr, "mob3: session init: %v\n", err)
		return 1
	}
	if err := session.Send(protocol.Command{Body: protocol.Navigate{URL: o.Target, Push: true}}); err != nil {
		fmt.Fprintf(os.Stderr, "mob3: session navigate: %v\n", err)
		return 1
	}
	loop := &frontend.SessionLoop{
		Session: session, Presenter: presenter, Keys: keys, Resize: winch, Quit: quitSig,
		TerminalSize: func() (int, int, bool) { return frontend.TerminalSize(os.Stdout) },
		CellW:        cellW, CellH: cellH, Stderr: os.Stderr, AttestedURL: hostSess.AttestedURL,
	}
	loop.State.Clipboard = os.Stdout
	if err := loop.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "mob3: %v\n", err)
		return 1
	}
	return 0
}
