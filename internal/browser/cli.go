package browser

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"browser/internal/frontend"
	"browser/internal/protocol"
)

// Run executes the MOB³ command line and returns a process exit code.
func Run(args []string) int {
	fs := flag.NewFlagSet("mob3", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dump := fs.Bool("dump", false, "render the page to stdout and exit")
	pngOut := fs.Bool("png", false, "write a PNG screenshot of the page to stdout (implies -dump)")
	widthFlag := fs.Int("width", 0, "layout width in columns (default: terminal width or 100)")
	heightFlag := fs.Int("height", 0, "layout viewport height in rows and output limit (0 = full page)")
	pdfPage := fs.String("pdf-page", "auto", "PDF page for dump/PNG: auto, all, or a page number")
	noColor := fs.Bool("no-color", false, "disable ANSI colors in dump mode")
	forceColor := fs.Bool("color", false, "force ANSI colors in dump mode")
	ua := fs.String("ua", "", "custom User-Agent (default MOB3/1.0 (+terminal; +https://mob3.net))")
	certs := fs.String("certs", "system", "TLS trust store: system or embedded")
	search := fs.String("search", DefaultSearchURL, "search URL template with %s; empty disables search fallback")
	showVersion := fs.Bool("version", false, "print the version and exit")
	graphics := fs.String("graphics", "none", "image mode: none, auto, kitty, iterm or blocks")
	fs.StringVar(graphics, "g", "none", "alias for -graphics")
	noJS := fs.Bool("no-js", false, "disable page JavaScript")
	noPDF := fs.Bool("no-pdf", false, "disable the built-in PDF text viewer")
	reader := fs.Bool("reader", false, "start in reader mode")
	dark := fs.Bool("dark", false, "start in dark mode")
	ignoreBlocks := fs.Bool("ignore-blocks", false, "load anti-bot/CAPTCHA pages instead of the blocked page")
	home := fs.String("home", "about:home", "home URL opened at startup and by the Home button")
	textScale := fs.String("text-scale", "auto", "kitty scaled text: auto or off")
	memLimit := fs.Int("mem-limit", 0, "soft process memory limit in MB; also sets the 25% JS, 12% image, 3% SVG, 3% cache and 12% history budgets (0 leaves it unlimited and honors GOMEMLIMIT)")
	layoutBudget := fs.String("layout-budget", "standard", "layout limits: low, standard, high or none")
	zoomFlag := fs.String("zoom", "100", "layout zoom percent from 25 to 200 (e.g. 150 or 150%)")
	mcpMode := fs.Bool("mcp", false, "run the MCP automation server on stdin/stdout")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] [url]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "MOB³ %s — A small terminal browser with its own HTML/CSS engine.\n", Version)
		fmt.Fprintf(os.Stderr, "License: AGPL-3.0-only\n")
		fmt.Fprintf(os.Stderr, "Copyright (c) 2026 Milos Stojanovic\n\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n  %s https://example.com\n  %s -dump -width 120 about:help\n  %s -dump -no-color https://go.dev\n", os.Args[0], os.Args[0], os.Args[0])
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Println(Version)
		return 0
	}
	if *heightFlag < 0 {
		fmt.Fprintln(os.Stderr, "mob3: -height must be 0 or positive")
		return 2
	}
	// The soft heap limit is process-wide, so every run mode gets it. Zero
	// leaves the Go runtime default (including a GOMEMLIMIT from the
	// environment) untouched.
	if *memLimit < 0 {
		fmt.Fprintln(os.Stderr, "mob3: -mem-limit must be 0 or positive")
		return 2
	}
	if *memLimit > 0 {
		debug.SetMemoryLimit(int64(*memLimit) << 20)
	}
	// 0 keeps every page for a dump and page 1 for a PNG; -1 means all pages.
	pdfPageSel := 0
	switch strings.ToLower(strings.TrimSpace(*pdfPage)) {
	case "", "auto":
	case "all":
		pdfPageSel = -1
	default:
		n, err := strconv.Atoi(strings.TrimSpace(*pdfPage))
		if err != nil || n < 1 {
			fmt.Fprintln(os.Stderr, "mob3: -pdf-page must be auto, all, or a page number")
			return 2
		}
		pdfPageSel = n
	}

	start := *home
	if strings.TrimSpace(start) == "" {
		start = "about:home"
	}
	if fs.NArg() > 0 {
		start = fs.Arg(0)
	}

	if *pngOut {
		*dump = true
	}
	interactive := !*dump && isTTY(os.Stdin) && isTTY(os.Stdout)
	graphicsMode := ParseGraphicsMode(*graphics)
	loader := NewLoader()
	loader.noScripts = *noJS
	loader.noPDF = *noPDF
	loader.noImages = graphicsMode == GraphicsNone
	loader.setSearchURL(*search)
	loader.ignoreBlocks = *ignoreBlocks
	if *ua != "" {
		loader.setUserAgent(*ua)
	}
	if err := loader.SetCertSource(*certs); err != nil {
		fmt.Fprintf(os.Stderr, "mob3: -certs: %v\n", err)
		return 2
	}
	jsMemLimit := jsHeapBudget(effectiveMemoryLimit())
	budget, ok := ParseLayoutBudget(*layoutBudget)
	if !ok {
		fmt.Fprintf(os.Stderr, "mob3: invalid -layout-budget %q (use low, standard, high or none)\n", *layoutBudget)
		return 2
	}
	zoomPct, ok := parseZoomPercent(*zoomFlag)
	if !ok {
		fmt.Fprintf(os.Stderr, "mob3: invalid -zoom %q (use a percentage from 25 to 200)\n", *zoomFlag)
		return 2
	}
	zoomFactor := zoomPct / 100
	SetColorMode(DetectColorMode())

	if *mcpMode {
		w := *widthFlag
		if w <= 0 {
			w = 100
		}
		h := *heightFlag
		if h <= 0 {
			h = 30
		}
		a := NewAutomation(w, h)
		defer a.Close()
		a.b.zoom = zoomFactor
		setLayoutZoom(zoomFactor)
		a.b.setLoader(loader)
		loader.noImages = true
		a.b.noJS = *noJS
		a.b.noPDF = *noPDF
		a.b.jsMemLimit = jsMemLimit
		a.b.memLimitBytes = int64(*memLimit) << 20
		a.b.layoutBudget = budget
		a.b.home = *home
		a.b.reader = *reader
		a.b.dark = *dark
		ctx := context.Background()
		if err := a.Navigate(ctx, start); err != nil {
			fmt.Fprintf(os.Stderr, "mob3 mcp: initial page: %v\n", err)
		}
		return RunMCP(a, os.Stdin, os.Stdout, os.Stderr)
	}

	if !interactive {
		return dumpPage(loader, start, *widthFlag, *heightFlag, *noColor, *forceColor, *noJS, *reader, *dark, *pngOut, pdfPageSel, jsMemLimit, budget, zoomFactor)
	}

	// Interactive pages show text first and fetch images in the background so
	// image-heavy pages do not delay the first paint.
	loader.deferImages = true
	stdin := os.Stdin
	state, err := makeRaw(stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mob3: interactive mode unavailable (%v); rendering to stdout.\n", err)
		return dumpPage(loader, start, *widthFlag, *heightFlag, *noColor, *forceColor, *noJS, *reader, *dark, *pngOut, pdfPageSel, jsMemLimit, budget, zoomFactor)
	}
	w, h, ok := terminalSize(os.Stdout)
	if !ok {
		w, h = 100, 30
	}
	if *widthFlag > 0 {
		w = *widthFlag
	}
	b := NewBrowser(w, h)
	b.zoom = zoomFactor
	setLayoutZoom(zoomFactor)
	b.interactive = true
	b.setLoader(loader)
	b.reader = *reader
	b.dark = *dark
	b.home = *home
	if strings.TrimSpace(b.home) == "" {
		b.home = "about:home"
	}
	b.cliConfig = map[string]string{
		"-dump":          onOff(*dump),
		"-png":           onOff(*pngOut),
		"-width":         strconv.Itoa(w),
		"-height":        strconv.Itoa(*heightFlag),
		"-pdf-page":      strings.TrimSpace(*pdfPage),
		"-graphics":      *graphics,
		"-mem-limit":     strconv.Itoa(*memLimit),
		"-layout-budget": budget.Name,
		"-zoom":          zoomPercentLabel(zoomFactor),
		"-color":         onOff(*forceColor),
		"-no-color":      onOff(*noColor),
		"-certs":         loader.certSource,
		"-mcp":           onOff(*mcpMode),
		"-version":       Version,
	}
	b.color = !*noColor
	b.noJS = *noJS
	b.noPDF = *noPDF
	b.jsMemLimit = jsMemLimit
	b.layoutBudget = budget
	b.memLimitBytes = int64(*memLimit) << 20
	switch strings.ToLower(strings.TrimSpace(*textScale)) {
	case "off", "none", "false":
		b.noBigText = true
	}
	cellW, cellH, ok := terminalCellPixels(os.Stdout)
	if !ok || cellW <= 0 {
		cellW = 8
	}
	if !ok || cellH <= 0 {
		cellH = 16
	}
	b.cellW, b.cellH = cellW, cellH
	b.gfx = NewGFXRenderer(graphicsMode, cellW, cellH)
	b.initGraphicsModes(graphicsMode)

	kittyKeys := kittyKeyboardEnabled()
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
		sb.WriteString("\x1b[0m\x1b[?7h\x1b[?25h\x1b[?1049l")
		_, _ = os.Stdout.WriteString(sb.String())
		restoreTerm(stdin, state)
	}
	defer cleanup()

	keys := make(chan string, 32)
	go readKeyEvents(stdin, keys, nil)

	winch := make(chan os.Signal, 1)
	quitSig := make(chan os.Signal, 1)
	startSignalHandlers(winch, quitSig)
	defer stopSignalHandlers(winch, quitSig)

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
	presenter := frontend.NewPresenter(os.Stdout, b.color)
	session, err := (&protocol.InProc{}).Open(context.Background(), 1, b)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mob3: session: %v\n", err)
		return 1
	}
	defer session.Close()
	if err := session.Send(protocol.Command{Body: protocol.Init{
		Width: w, Height: h, CellW: cellW, CellH: cellH, Zoom: zoomFactor, Caps: caps,
	}}); err != nil {
		fmt.Fprintf(os.Stderr, "mob3: session init: %v\n", err)
		return 1
	}
	if err := session.Send(protocol.Command{Body: protocol.Navigate{URL: start, Push: true}}); err != nil {
		fmt.Fprintf(os.Stderr, "mob3: session navigate: %v\n", err)
		return 1
	}
	scroll := 0
	native := false
	for {
		select {
		case k := <-keys:
			if k == "eof" {
				return 0
			}
			if k == "esc" || k == "ctrl-c" {
				// Abort is out of band so a busy script stops even when the
				// command queue is full. The key still follows for the normal
				// shell handling.
				session.Abort()
			}
			if err := session.Send(protocol.Command{Body: protocol.Key{Token: k}}); err != nil {
				if errors.Is(err, protocol.ErrSessionClosed) {
					continue
				}
				fmt.Fprintf(os.Stderr, "mob3: session key: %v\n", err)
				return 1
			}
		case <-winch:
			if nw, nh, ok := terminalSize(os.Stdout); ok {
				if err := session.Send(protocol.Command{Body: protocol.Resize{
					Width: nw, Height: nh, CellW: cellW, CellH: cellH,
				}}); err != nil {
					if errors.Is(err, protocol.ErrSessionClosed) {
						continue
					}
					fmt.Fprintf(os.Stderr, "mob3: session resize: %v\n", err)
					return 1
				}
			}
		case ev, ok := <-session.Events():
			if !ok {
				return 0
			}
			switch v := ev.Body.(type) {
			case protocol.Quit:
				return 0
			case protocol.State:
				scroll = v.Scroll
				native = v.Flags&protocol.StateNativeImages != 0
			case protocol.Frame:
				if !native {
					if err := presenter.Present(v, scroll, native); err != nil {
						fmt.Fprintf(os.Stderr, "mob3: present: %v\n", err)
						return 1
					}
				}
			}
		case <-quitSig:
			return 0
		}
	}
}

func dumpPage(loader *Loader, target string, width, viewportH int, noColor, forceColor, noJS, reader, dark, pngOut bool, pdfPage int, jsMemLimit int64, budget LayoutBudget, zoom float64) int {
	if width <= 0 {
		if w, _, ok := terminalSize(os.Stdout); ok {
			width = w
		} else {
			width = 100
		}
	}
	setLayoutZoom(zoom)
	loader.imageViewportPx = cssWidthPx(width)
	// A PNG has no browsing context, so it shows one page: page 1 unless the
	// caller selected another. Dumps keep every page.
	if pdfPage == 0 && pngOut {
		pdfPage = 1
	}
	if pdfPage < 0 {
		pdfPage = 0
	}
	loader.pdfPage = pdfPage
	page, err := loader.Load(target, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mob3: %s\n", stripTerminalControls(err.Error()))
		return 1
	}
	if page.PDF != nil && pdfPage > 0 && page.PDF.TotalPages > 0 && len(page.PDF.Pages) == 0 {
		fmt.Fprintf(os.Stderr, "mob3: -pdf-page %d is out of range (document has %d pages)\n", pdfPage, page.PDF.TotalPages)
		return 1
	}
	if !noJS {
		runner := NewBrowser(width, 3)
		// NewBrowser resets the package-wide cell ratio; restore the run's zoom
		// before scripts run so script-visible viewport sizes match the dump.
		runner.zoom = zoom
		setLayoutZoom(zoom)
		runner.noJS = false
		runner.jsMemLimit = jsMemLimit
		runner.startJS(page)
		page.CollectDynamicStyles()
		if !loader.noImages {
			loader.loadImages(context.Background(), page.Root, page.Base)
		}
	}
	setLayoutZoom(zoom)
	root, author := page.Root, page.Author
	if reader && page.PDF == nil {
		if extracted, ok := extractReaderPage(page.Root, page.Title); ok {
			root, author = extracted, readerRules
		}
	}
	deadline := time.Time{}
	if budget.Time > 0 {
		deadline = time.Now().Add(budget.Time)
	}
	layout := BuildLayoutBudgetDeadline(root, author, width, viewportH, false, dark, deadline, budget)
	// CSS background images are only known after the style pass, so a dump
	// loads them in a second image pass and rebuilds the layout once.
	if !loader.noImages {
		if loader.loadImages(context.Background(), root, page.Base) {
			if budget.Time > 0 {
				deadline = time.Now().Add(budget.Time)
			}
			layout = BuildLayoutBudgetDeadline(root, author, width, viewportH, false, dark, deadline, budget)
		}
	}
	full := layout.Height
	height := full
	if viewportH > 0 && height > viewportH {
		height = viewportH
	}
	if height < 1 {
		height = 1
	}
	rowCap := 20000
	if pngOut {
		rowCap = 2000
	}
	if (viewportH == 0 || viewportH >= rowCap) && full > rowCap {
		if pngOut && page.PDF != nil {
			fmt.Fprintf(os.Stderr, "mob3: PNG truncated to %d rows; use -pdf-page=N to export one page\n", rowCap)
		} else if pngOut {
			fmt.Fprintf(os.Stderr, "mob3: PNG truncated to %d rows\n", rowCap)
		} else {
			fmt.Fprintf(os.Stderr, "mob3: output truncated to %d rows\n", rowCap)
		}
	}
	if height > rowCap {
		height = rowCap
	}
	pageFG := RGB(0x1f, 0x23, 0x28)
	if layout.Body != nil && layout.Body.Style != nil && layout.Body.Style.Color.Set {
		pageFG = layout.Body.Style.Color
	}
	pageBG := layout.PageBG
	if !pageBG.Set {
		pageBG = RGB(255, 255, 255)
	}
	g := NewGrid(width, height, pageFG, pageBG)
	PaintLayout(g, layout, 0, 0, height, nil, pageFG)
	if pngOut {
		data, err := gridPNG(g, 1)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mob3: png: %v\n", err)
			return 1
		}
		if _, err := os.Stdout.Write(data); err != nil {
			return 1
		}
		return 0
	}
	color := !noColor && (forceColor || isTTY(os.Stdout))
	lines := gridLines(g, color)
	for _, line := range lines {
		fmt.Println(line)
	}
	return 0
}

// Version is the MOB³ release version reported by -version. It is set at
// build time with -ldflags "-X browser/internal/browser.Version=v1.2.3" and
// remains "dev" for local builds that do not pass a version.
var Version = "dev"
