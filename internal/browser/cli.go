package browser

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"browser/internal/frontend"
	"browser/internal/protocol"
)

// RunOptions is the parsed command line handed to Execute by internal/app.
type RunOptions struct {
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

// Execute runs one MOB³ invocation after the command line has been parsed.
func Execute(o RunOptions) int {
	if o.MemLimit > 0 {
		debug.SetMemoryLimit(int64(o.MemLimit) << 20)
	}
	if o.PNG {
		o.Dump = true
	}
	interactive := !o.Dump && frontend.IsTTY(os.Stdin) && frontend.IsTTY(os.Stdout)
	graphicsMode := ParseGraphicsMode(o.Graphics)
	loader := NewLoader()
	loader.noScripts = o.NoJS
	loader.noPDF = o.NoPDF
	loader.noImages = graphicsMode == GraphicsNone
	loader.setSearchURL(o.Search)
	loader.ignoreBlocks = o.IgnoreBlocks
	if o.UA != "" {
		loader.setUserAgent(o.UA)
	}
	if err := loader.SetCertSource(o.Certs); err != nil {
		fmt.Fprintf(os.Stderr, "mob3: -certs: %v\n", err)
		return 2
	}
	jsMemLimit := jsHeapBudget(effectiveMemoryLimit())
	budget, ok := ParseLayoutBudget(o.LayoutBudget)
	if !ok {
		fmt.Fprintf(os.Stderr, "mob3: invalid -layout-budget %q (use low, standard, high or none)\n", o.LayoutBudget)
		return 2
	}
	zoomPct, ok := parseZoomPercent(o.Zoom)
	if !ok {
		fmt.Fprintf(os.Stderr, "mob3: invalid -zoom %q (use a percentage from 25 to 200)\n", o.Zoom)
		return 2
	}
	zoomFactor := zoomPct / 100
	SetColorMode(DetectColorMode())

	if o.MCP {
		w := o.Width
		if w <= 0 {
			w = 100
		}
		h := o.Height
		if h <= 0 {
			h = 30
		}
		auto := NewAutomation(w, h)
		auto.b.zoom = zoomFactor
		setLayoutZoom(zoomFactor)
		auto.b.setLoader(loader)
		loader.noImages = true
		auto.b.noJS = o.NoJS
		auto.b.noPDF = o.NoPDF
		auto.b.jsMemLimit = jsMemLimit
		auto.b.memLimitBytes = int64(o.MemLimit) << 20
		auto.b.layoutBudget = budget
		auto.b.home = o.Home
		auto.b.reader = o.Reader
		auto.b.dark = o.Dark
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
		return dumpPage(loader, o.Target, o.Width, o.Height, o.NoColor, o.Color, o.NoJS, o.Reader, o.Dark, o.PNG, o.PDFPage, jsMemLimit, budget, zoomFactor)
	}

	// Interactive pages show text first and fetch images in the background so
	// image-heavy pages do not delay the first paint.
	loader.deferImages = true
	stdin := os.Stdin
	state, err := frontend.MakeRaw(stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mob3: interactive mode unavailable (%v); rendering to stdout.\n", err)
		return dumpPage(loader, o.Target, o.Width, o.Height, o.NoColor, o.Color, o.NoJS, o.Reader, o.Dark, o.PNG, o.PDFPage, jsMemLimit, budget, zoomFactor)
	}
	w, h, ok := frontend.TerminalSize(os.Stdout)
	if !ok {
		w, h = 100, 30
	}
	if o.Width > 0 {
		w = o.Width
	}
	b := NewBrowser(w, h)
	b.zoom = zoomFactor
	setLayoutZoom(zoomFactor)
	b.interactive = true
	b.setLoader(loader)
	b.reader = o.Reader
	b.dark = o.Dark
	b.home = o.Home
	if strings.TrimSpace(b.home) == "" {
		b.home = "about:home"
	}
	b.cliConfig = map[string]string{
		"-dump":          onOff(o.Dump),
		"-png":           onOff(o.PNG),
		"-width":         strconv.Itoa(w),
		"-height":        strconv.Itoa(o.Height),
		"-pdf-page":      o.PDFPageLabel,
		"-graphics":      o.Graphics,
		"-mem-limit":     strconv.Itoa(o.MemLimit),
		"-layout-budget": budget.Name,
		"-zoom":          zoomPercentLabel(zoomFactor),
		"-color":         onOff(o.Color),
		"-no-color":      onOff(o.NoColor),
		"-certs":         loader.certSource,
		"-mcp":           onOff(o.MCP),
		"-version":       buildVersion,
	}
	b.color = !o.NoColor
	b.noJS = o.NoJS
	b.noPDF = o.NoPDF
	b.jsMemLimit = jsMemLimit
	b.layoutBudget = budget
	b.memLimitBytes = int64(o.MemLimit) << 20
	switch strings.ToLower(strings.TrimSpace(o.TextScale)) {
	case "off", "none", "false":
		b.noBigText = true
	}
	cellW, cellH, ok := frontend.TerminalCellPixels(os.Stdout)
	if !ok || cellW <= 0 {
		cellW = 8
	}
	if !ok || cellH <= 0 {
		cellH = 16
	}
	b.cellW, b.cellH = cellW, cellH
	b.gfx = NewGFXRenderer(graphicsMode, cellW, cellH)
	b.initGraphicsModes(graphicsMode)
	presenter := frontend.NewPresenter(os.Stdout, b.color)
	// The graphics cycle can switch into native mode from blocks or none, so
	// the front-end renderer must exist whenever the terminal supports it.
	presenterMode := graphicsMode
	if presenterMode == GraphicsAuto || presenterMode == GraphicsBlocks || presenterMode == GraphicsNone {
		presenterMode = DetectGraphics(GraphicsAuto)
	}
	presenter.SetGraphics(presenterMode, cellW, cellH)

	kittyKeys := frontend.KittyKeyboardEnabled()
	var startSeq strings.Builder
	startSeq.WriteString("\x1b[?1049h\x1b[?25l\x1b[?7l\x1b[2J\x1b[?1000h\x1b[?1003h\x1b[?1006h")
	if kittyKeys {
		// Disambiguate escape codes, report event types, and report the text
		// generated by each key (shifted symbols and AltGr layouts).
		startSeq.WriteString("\x1b[>19u")
	}
	_, _ = os.Stdout.WriteString(startSeq.String())
	cleanup := func() {
		b.cancelLoadNow()
		var sb strings.Builder
		if kittyKeys {
			sb.WriteString("\x1b[<u")
		}
		sb.WriteString("\x1b[?1003l\x1b[?1000l\x1b[?1006l")
		b.gfx.Clear(&sb)
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

	// The engine runs on its own goroutine behind the transport. The front end
	// owns the terminal: it reads keys and resizes, sends commands, and reacts
	// to quit events.
	caps := protocol.Capabilities(0)
	if b.color {
		caps |= protocol.CapColor
	}
	if b.gfx != nil {
		switch b.gfx.Mode() {
		case GraphicsKitty:
			caps |= protocol.CapKittyGraphics
		case GraphicsITerm:
			caps |= protocol.CapITermGraphics
		}
		if b.gfx.TextSizing() {
			caps |= protocol.CapKittyText
		}
	}
	session, err := (&protocol.InProc{}).Open(context.Background(), 1, b)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mob3: session: %v\n", err)
		return 1
	}
	defer session.Close()
	top, contentH := frontend.ContentBounds(h)
	if err := session.Send(protocol.Command{Body: protocol.Init{
		Width: w, Height: h, ContentTop: top, ContentHeight: contentH,
		CellW: cellW, CellH: cellH, Zoom: zoomFactor, Caps: caps,
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
		CellW:        cellW, CellH: cellH, Stderr: os.Stderr,
	}
	loop.State.Clipboard = os.Stdout
	if err := loop.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "mob3: %v\n", err)
		return 1
	}
	return 0
}

// buildVersion is the app's build version, set at startup with
// SetBuildVersion. It is used by session-generated pages, the config view, the
// copy header and MCP metadata so they agree with -version.
var buildVersion = "dev"

// SetBuildVersion supplies the build version used by session-generated pages.
func SetBuildVersion(v string) {
	if v == "" {
		v = "dev"
	}
	buildVersion = v
}
