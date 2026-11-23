package browser

import (
	"context"
	"fmt"
	"io"
	"strings"

	"browser/internal/protocol"
)

// LoaderConfig is the host/loader policy the app applies before creating a
// session. It keeps the loader fields private while the app owns flag values.
type LoaderConfig struct {
	NoScripts    bool
	NoPDF        bool
	NoImages     bool
	IgnoreBlocks bool
	Search       string
	UserAgent    string
	Certs        string
	DeferImages  bool
}

// NewConfiguredLoader creates the loader used by one app invocation.
func NewConfiguredLoader(c LoaderConfig) (*Loader, error) {
	l := NewLoader()
	l.noScripts = c.NoScripts
	l.noPDF = c.NoPDF
	l.noImages = c.NoImages
	l.setSearchURL(c.Search)
	l.ignoreBlocks = c.IgnoreBlocks
	l.deferImages = c.DeferImages
	if c.UserAgent != "" {
		l.setUserAgent(c.UserAgent)
	}
	if err := l.SetCertSource(c.Certs); err != nil {
		return nil, err
	}
	return l, nil
}

// SessionConfig is the engine-side configuration the app applies before the
// interactive transport starts. Terminal setup stays in the app/front end.
type SessionConfig struct {
	Loader        *Loader
	Width, Height int
	Zoom          float64
	Interactive   bool
	Reader, Dark  bool
	Home          string
	CLIConfig     map[string]string
	Color         bool
	NoJS, NoPDF   bool
	JSMemLimit    int64
	LayoutBudget  LayoutBudget
	MemLimitBytes int64
	NoBigText     bool
	CellW, CellH  int
	Graphics      GraphicsMode
}

// NewSession creates and configures the page session used by the interactive
// loop. It is the app's only construction entry point while the engine type is
// still in transition.
func NewSession(c SessionConfig) *Browser {
	b := NewBrowser(c.Width, c.Height)
	b.zoom = c.Zoom
	SetLayoutZoom(c.Zoom)
	b.interactive = c.Interactive
	if c.Loader != nil {
		b.setLoader(c.Loader)
	}
	b.reader = c.Reader
	b.dark = c.Dark
	b.home = c.Home
	if strings.TrimSpace(b.home) == "" {
		b.home = "about:home"
	}
	b.cliConfig = c.CLIConfig
	b.color = c.Color
	b.noJS = c.NoJS
	b.noPDF = c.NoPDF
	b.jsMemLimit = c.JSMemLimit
	b.layoutBudget = c.LayoutBudget
	b.memLimitBytes = c.MemLimitBytes
	b.noBigText = c.NoBigText
	if c.CellW > 0 {
		b.cellW = c.CellW
	}
	if c.CellH > 0 {
		b.cellH = c.CellH
	}
	b.gfx = NewGFXRenderer(c.Graphics, b.cellW, b.cellH)
	b.initGraphicsModes(c.Graphics)
	return b
}

// Color reports whether ANSI color output is enabled for this session.
func (b *Browser) Color() bool { return b.color }

// Capabilities reports the terminal features the session should assume.
func (b *Browser) Capabilities() protocol.Capabilities {
	var caps protocol.Capabilities
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
	return caps
}

// Cleanup stops background work and emits terminal graphics cleanup sequences
// into sb. The app still owns the alternate-screen and raw-mode sequences.
func (b *Browser) Cleanup(sb *strings.Builder) {
	if b == nil {
		return
	}
	b.cancelLoadNow()
	if b.gfx != nil {
		b.gfx.Clear(sb)
	}
}

// JSMemLimit returns the per-session JavaScript heap budget.
func JSMemLimit() int64 { return jsHeapBudget(effectiveMemoryLimit()) }

// ParseZoomPercent parses the -zoom value.
func ParseZoomPercent(s string) (float64, bool) { return parseZoomPercent(s) }

// ZoomPercentLabel formats a zoom factor for the config page.
func ZoomPercentLabel(z float64) string { return zoomPercentLabel(z) }

// DumpOptions carries one non-interactive render.
type DumpOptions struct {
	Loader     *Loader
	Target     string
	Width      int
	Height     int
	NoColor    bool
	Color      bool
	NoJS       bool
	Reader     bool
	Dark       bool
	PNG        bool
	PDFPage    int
	JSMemLimit int64
	Budget     LayoutBudget
	Zoom       float64
}

// Dump renders one page to stdout.
func Dump(o DumpOptions) int {
	return dumpPage(o.Loader, o.Target, o.Width, o.Height, o.NoColor, o.Color,
		o.NoJS, o.Reader, o.Dark, o.PNG, o.PDFPage, o.JSMemLimit, o.Budget, o.Zoom)
}

// MCPOptions carries one MCP server invocation.
type MCPOptions struct {
	Loader        *Loader
	Width, Height int
	Zoom          float64
	NoJS, NoPDF   bool
	JSMemLimit    int64
	MemLimitBytes int64
	LayoutBudget  LayoutBudget
	Home          string
	Reader, Dark  bool
	Target        string
}

// RunMCPBoot starts the headless automation session and serves MCP on the
// supplied streams.
func RunMCPBoot(o MCPOptions, in io.Reader, out, errOut io.Writer) int {
	w, h := o.Width, o.Height
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 30
	}
	auto := NewAutomation(w, h)
	auto.b.zoom = o.Zoom
	SetLayoutZoom(o.Zoom)
	if o.Loader != nil {
		auto.b.setLoader(o.Loader)
		o.Loader.noImages = true
	}
	auto.b.noJS = o.NoJS
	auto.b.noPDF = o.NoPDF
	auto.b.jsMemLimit = o.JSMemLimit
	auto.b.memLimitBytes = o.MemLimitBytes
	auto.b.layoutBudget = o.LayoutBudget
	auto.b.home = o.Home
	auto.b.reader = o.Reader
	auto.b.dark = o.Dark
	session, err := (&protocol.InProc{EventQueue: 8}).Open(context.Background(), 1, &automationHandler{a: auto})
	if err != nil {
		fmt.Fprintf(errOut, "mob3 mcp: session: %v\n", err)
		return 1
	}
	defer session.Close()
	client := &automationClient{s: session}
	ctx := context.Background()
	if err := client.Navigate(ctx, o.Target); err != nil {
		fmt.Fprintf(errOut, "mob3 mcp: initial page: %v\n", err)
	}
	return RunMCP(client, in, out, errOut)
}
