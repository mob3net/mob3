package engine

import (
	"io"
	"strings"

	"browser/internal/protocol"
)

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
	Output        io.Writer
	NoJS, NoPDF   bool
	JSMemLimit    int64
	LayoutBudget  LayoutBudget
	MemLimitBytes int64
	NoBigText     bool
	CellW, CellH  int
	Graphics      GraphicsMode
	GraphicsModes []GraphicsMode
	GFX           Graphics
	NewGraphics   func(GraphicsMode, int, int) Graphics
}

// NewSession creates and configures the page session used by the interactive
// loop. It is the app's only construction entry point while the engine type is
// still in transition.
func NewConfiguredSession(c SessionConfig) *Session {
	b := NewSession(c.Width, c.Height)
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
	b.JSMemLimit = c.JSMemLimit
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
	b.gfx = c.GFX
	b.newGraphics = c.NewGraphics
	return b
}

// Color reports whether ANSI color output is enabled for this session.
func (b *Session) Color() bool { return b.color }

// Capabilities reports the terminal features the session should assume.
func (b *Session) Capabilities() protocol.Capabilities {
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
func (b *Session) Cleanup(sb *strings.Builder) {
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

// AutomationConfig is the headless session configuration used by MCP.
type AutomationConfig struct {
	Loader        *Loader
	Width, Height int
	Zoom          float64
	NoJS, NoPDF   bool
	JSMemLimit    int64
	MemLimitBytes int64
	LayoutBudget  LayoutBudget
	Home          string
	Reader, Dark  bool
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
	a := newAutomation(w, h)
	a.b.Zoom = c.Zoom
	SetZoom(c.Zoom)
	if c.Loader != nil {
		a.b.setLoader(c.Loader)
		c.Loader.NoImages = true
	}
	a.b.NoJS = c.NoJS
	a.b.NoPDF = c.NoPDF
	a.b.JSMemLimit = c.JSMemLimit
	a.b.memLimitBytes = c.MemLimitBytes
	a.b.LayoutBudget = c.LayoutBudget
	a.b.home = c.Home
	a.b.Reader = c.Reader
	a.b.Dark = c.Dark
	return a
}
