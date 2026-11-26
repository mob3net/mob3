// Package app wires the command line to the browser session. It owns flag
// parsing, validation and dispatch; the session/engine and terminal
// implementations still live in internal/browser and internal/frontend until
// the boundary migration finishes.
package app

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"browser/internal/engine"
)

// Version is the MOB³ release version reported by -version. It is set at
// build time with -ldflags "-X browser/internal/app.Version=v1.2.3" and
// remains "dev" for local builds that do not pass a version.
var Version = "dev"

// Run executes the MOB³ command line and returns a process exit code.
func Run(args []string) int {
	engine.SetBuildVersion(Version)
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
	search := fs.String("search", engine.DefaultSearchURL, "search URL template with %s; empty disables search fallback")
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
	memLimit := fs.Int("mem-limit", 0, "soft memory limit in MB; per tab in process mode, divided across tabs in inproc mode; also sets the 25% JS, 12% image, 3% SVG, 3% cache and 12% history budgets (0 leaves it unlimited and honors GOMEMLIMIT)")
	layoutBudget := fs.String("layout-budget", "standard", "layout limits: low, standard, high or none")
	zoomFlag := fs.String("zoom", "100", "layout zoom percent from 25 to 200 (e.g. 150 or 150%)")
	engineMode := fs.String("engine", "inproc", "interactive engine placement: inproc or process")
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
	if *memLimit < 0 {
		fmt.Fprintln(os.Stderr, "mob3: -mem-limit must be 0 or positive")
		return 2
	}
	placement := strings.ToLower(strings.TrimSpace(*engineMode))
	if placement != "inproc" && placement != "process" {
		fmt.Fprintln(os.Stderr, "mob3: -engine must be inproc or process")
		return 2
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
	o := runOptions{
		Dump: *dump, PNG: *pngOut, Width: *widthFlag, Height: *heightFlag,
		PDFPage: pdfPageSel, PDFPageLabel: strings.TrimSpace(*pdfPage),
		NoColor: *noColor, Color: *forceColor, UA: *ua, Certs: *certs,
		Search: *search, Graphics: *graphics, NoJS: *noJS, NoPDF: *noPDF,
		Reader: *reader, Dark: *dark, IgnoreBlocks: *ignoreBlocks,
		Home: *home, TextScale: *textScale, MemLimit: *memLimit,
		LayoutBudget: *layoutBudget, Zoom: *zoomFlag, MCP: *mcpMode,
		Engine: placement, Target: start,
	}
	if os.Getenv(engineWorkerEnv) == "1" {
		return runEngineWorker(o)
	}
	return execute(o)
}
