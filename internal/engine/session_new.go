package engine

import (
	"io"
	"strings"

	"browser/internal/protocol"
)

// SessionConfig is the engine-side configuration the app applies before the
// interactive transport starts. Terminal setup stays in the app/front end.
type SessionConfig struct {
	Loader         *Loader
	BrowserContext *BrowserContext
	Width, Height  int
	Zoom           float64
	Interactive    bool
	Reader, Dark   bool
	Home           string
	CLIConfig      map[string]string
	Color          bool
	Output         io.Writer
	NoJS, NoPDF    bool
	LayoutBudget   LayoutBudget
	MemLimitBytes  int64
	NoBigText      bool
	CellW, CellH   int
	Graphics       GraphicsMode
	GraphicsModes  []GraphicsMode
	Caps           protocol.Capabilities
}

// NewSession creates and configures the page session used by the interactive
// loop. It is the app's only construction entry point while the engine type is
// still in transition.
func NewConfiguredSession(c SessionConfig) *Session {
	b := NewSessionWithBrowserContext(c.Width, c.Height, c.BrowserContext)
	b.Zoom = c.Zoom
	SetZoom(c.Zoom)
	b.Interactive = c.Interactive
	if c.Loader != nil {
		b.setLoader(c.Loader)
	}
	b.Reader = c.Reader
	b.Dark = c.Dark
	b.home = c.Home
	if strings.TrimSpace(b.home) == "" {
		b.home = "about:home"
	}
	b.cliConfig = c.CLIConfig
	b.color = c.Color
	b.NoJS = c.NoJS
	b.NoPDF = c.NoPDF
	b.LayoutBudget = c.LayoutBudget
	b.memLimitBytes = c.MemLimitBytes
	b.NoBigText = c.NoBigText
	if c.CellW > 0 {
		b.CellW = c.CellW
	}
	if c.CellH > 0 {
		b.CellH = c.CellH
	}
	b.Output = c.Output
	b.graphicsModes = c.GraphicsModes
	b.graphicsMode = c.Graphics
	b.caps = c.Caps &^ protocol.CapColor
	return b
}

// Color reports whether ANSI color output is enabled for this session.
func (b *Session) Color() bool { return b.color }

// Capabilities reports the terminal features the session should assume.
func (b *Session) Capabilities() protocol.Capabilities {
	caps := b.caps &^ protocol.CapColor
	if b.color {
		caps |= protocol.CapColor
	}
	return caps
}

// Cleanup stops background work. Terminal cleanup stays with the front end.
func (b *Session) Cleanup() {
	if b == nil {
		return
	}
	b.cancelLoadNow()
}

// JSMemLimit returns the total JavaScript heap pool before the BudgetManager
// divides it across sessions. The one-shot dump path uses it directly.
func JSMemLimit() int64 { return jsHeapBudget(effectiveMemoryLimit()) }

// ParseZoomPercent parses the -zoom value.
func ParseZoomPercent(s string) (float64, bool) { return parseZoomPercent(s) }

// ZoomPercentLabel formats a zoom factor for the config page.
func ZoomPercentLabel(z float64) string { return zoomPercentLabel(z) }

// AutomationConfig is the headless session configuration used by MCP.
type AutomationConfig struct {
	Loader         *Loader
	BrowserContext *BrowserContext
	Width, Height  int
	Zoom           float64
	NoJS, NoPDF    bool
	MemLimitBytes  int64
	LayoutBudget   LayoutBudget
	Home           string
	Reader, Dark   bool
}

// NewAutomationWithConfig creates a configured headless session.
func NewAutomationWithConfig(c AutomationConfig) *Automation {
	w, h := c.Width, c.Height
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 30
	}
	a := newAutomation(w, h, c.BrowserContext)
	a.b.Zoom = c.Zoom
	SetZoom(c.Zoom)
	if c.Loader != nil {
		a.b.setLoader(c.Loader)
		c.Loader.NoImages = true
	}
	a.b.NoJS = c.NoJS
	a.b.NoPDF = c.NoPDF
	a.b.memLimitBytes = c.MemLimitBytes
	a.b.LayoutBudget = c.LayoutBudget
	a.b.home = c.Home
	a.b.Reader = c.Reader
	a.b.Dark = c.Dark
	return a
}
