package browser

import (
	"flag"
	"fmt"
	"os"
	"runtime/pprof"
	"strings"
	"time"
)

// Run executes the MOB³ command line and returns a process exit code.
func Run(args []string) int {
	fs := flag.NewFlagSet("mob3", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dump := fs.Bool("dump", false, "render the page to stdout and exit")
	widthFlag := fs.Int("width", 0, "layout width in columns (default: terminal width or 100)")
	noColor := fs.Bool("no-color", false, "disable ANSI colors in dump mode")
	forceColor := fs.Bool("color", false, "force ANSI colors in dump mode")
	ua := fs.String("ua", "", "custom User-Agent (default MOB3/1.0 (+terminal; +https://mob3.net))")
	search := fs.String("search", DefaultSearchURL, "search URL template with %s; empty disables search fallback")
	showVersion := fs.Bool("version", false, "print the version and exit")
	graphics := fs.String("graphics", "auto", "image protocol: auto, kitty, iterm or blocks")
	noJS := fs.Bool("no-js", false, "disable page JavaScript")
	textScale := fs.String("text-scale", "auto", "kitty scaled text: auto or off")
	cpuProfile := fs.String("cpuprofile", "", "write a CPU profile to this file")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] [url|file]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "MOB³ — A small terminal browser with its own HTML/CSS engine.\n\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n  %s https://example.com\n  %s -dump -width 120 page.html\n  %s -dump -no-color https://go.dev\n", os.Args[0], os.Args[0], os.Args[0])
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
	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mob3: cannot create CPU profile: %v\n", err)
		} else if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintf(os.Stderr, "mob3: cannot start CPU profile: %v\n", err)
			f.Close()
		} else {
			defer func() {
				pprof.StopCPUProfile()
				f.Close()
			}()
		}
	}

	start := "about:start"
	if fs.NArg() > 0 {
		start = fs.Arg(0)
	}

	interactive := !*dump && isTTY(os.Stdin) && isTTY(os.Stdout)
	loader := NewLoader()
	loader.noScripts = *noJS
	loader.searchURL = *search
	if *ua != "" {
		loader.userAgent = *ua
	}

	if !interactive {
		return dumpPage(loader, start, *widthFlag, *noColor, *forceColor, *noJS)
	}

	stdin := os.Stdin
	state, err := makeRaw(stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mob3: interactive mode unavailable (%v); rendering to stdout.\n", err)
		return dumpPage(loader, start, *widthFlag, *noColor, *forceColor, *noJS)
	}
	w, h, ok := terminalSize(os.Stdout)
	if !ok {
		w, h = 100, 30
	}
	if *widthFlag > 0 {
		w = *widthFlag
	}
	b := NewBrowser(w, h)
	b.loader = loader
	b.color = !*noColor
	b.noJS = *noJS
	switch strings.ToLower(strings.TrimSpace(*textScale)) {
	case "off", "none", "false":
		b.noBigText = true
	}
	cellW, cellH, _ := terminalCellPixels(os.Stdout)
	b.gfx = NewGFXRenderer(ParseGraphicsMode(*graphics), cellW, cellH)

	_, _ = os.Stdout.WriteString("\x1b[?1049h\x1b[?25l\x1b[?7l\x1b[2J\x1b[?1000h\x1b[?1002h\x1b[?1006h")
	cleanup := func() {
		b.cancelLoadNow()
		var sb strings.Builder
		sb.WriteString("\x1b[?1002l\x1b[?1000l\x1b[?1006l")
		b.gfx.Clear(&sb)
		sb.WriteString("\x1b[0m\x1b[?7h\x1b[?25h\x1b[?1049l")
		_, _ = os.Stdout.WriteString(sb.String())
		restoreTerm(stdin, state)
	}
	defer cleanup()

	keys := make(chan string, 32)
	b.keys = keys
	go readKeyEvents(stdin, keys)

	winch := make(chan os.Signal, 1)
	quitSig := make(chan os.Signal, 1)
	startSignalHandlers(winch, quitSig)
	defer stopSignalHandlers(winch, quitSig)

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	usageTicker := time.NewTicker(2 * time.Second)
	defer usageTicker.Stop()

	b.sampleUsage()
	b.Navigate(start, true)
	for {
		b.draw()
		select {
		case k := <-keys:
			if k == "eof" {
				return 0
			}
			if b.HandleKey(k) {
				return 0
			}
		case <-winch:
			if nw, nh, ok := terminalSize(os.Stdout); ok {
				b.resize(nw, nh)
			}
		case res := <-b.loadCh:
			b.finishLoad(res)
		case <-ticker.C:
			if b.runJSTimers() {
				b.draw()
			}
		case <-usageTicker.C:
			b.sampleUsage()
			if !b.consoleOpen {
				b.draw()
			}
		case <-quitSig:
			return 0
		}
	}
}

func dumpPage(loader *Loader, target string, width int, noColor, forceColor, noJS bool) int {
	if width <= 0 {
		if w, _, ok := terminalSize(os.Stdout); ok {
			width = w
		} else {
			width = 100
		}
	}
	page, err := loader.Load(target, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mob3: %v\n", err)
		return 1
	}
	if !noJS {
		runner := NewBrowser(width, 3)
		runner.noJS = false
		runner.startJS(page)
	}
	layout := BuildLayout(page.Root, page.Author, width)
	height := layout.Height
	if height < 1 {
		height = 1
	}
	if height > 20000 {
		height = 20000
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
	color := !noColor && (forceColor || isTTY(os.Stdout))
	lines := g.Lines(color)
	for _, line := range lines {
		fmt.Println(line)
	}
	return 0
}
