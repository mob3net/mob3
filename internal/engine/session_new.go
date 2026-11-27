package engine

import (
	"strings"
	"time"

	"browser/internal/protocol"
)

// viewportState is the terminal and page geometry a tab presents at.
type viewportState struct {
	Width, Height int
	// contentTop/contentHeight are the front-end-owned page rectangle. A
	// negative top means direct callers/tests use the legacy derivation.
	ContentTop    int
	ContentHeight int
	CellW, CellH  int
	// Zoom is the layout zoom factor: 1 is normal. It changes the CSS pixels
	// per terminal cell for everything except text metrics.
	Zoom float64
	// scaledLayout records whether the current layout was built with kitty
	// scaled text, so draw can rebuild when an overlay toggles it.
	ScaledLayout bool
	zoomGen      uint64
	// active marks the tab the front end is presenting. Direct callers and
	// tests start active; the tab manager sends Visibility when it switches.
	active bool
}

// optionState is session configuration that stays fixed for a tab's lifetime.
type optionState struct {
	NoJS bool
	// noPDF disables the built-in PDF text viewer, restoring the download
	// prompt for PDF responses.
	NoPDF     bool
	NoBigText bool
	// JSMemLimit caps the JS heap; JSPageBudget caps one page's load-time
	// script phase.
	JSMemLimit   int64
	JSPageBudget time.Duration
	LayoutBudget LayoutBudget
	// hideFixed suppresses position:fixed boxes for the current page.
	// noSticky lays position:sticky boxes out as normal flow, so they scroll
	// with the page instead of following a top offset.
	// hideBackgrounds paints CSS background images as a neutral placeholder
	// instead of their pixels; the images stay loaded and laid out.
	HideFixed       bool
	NoSticky        bool
	HideBackgrounds bool
	// Interactive marks the terminal UI, where a background dynamic import can
	// wait for the event loop instead of blocking it.
	Interactive bool
	NoDraw      bool
}

// renderState is the palette, terminal capabilities and reusable paint scratch.
type renderState struct {
	color      bool
	caps       protocol.Capabilities
	pngEncoder func(protocol.Frame, int) ([]byte, error)
	// contentGrid is reused between frames and is exactly the page viewport.
	contentGrid *Grid
	frameBuf    frameScratch
	// memLimitBytes is the process soft heap limit in bytes; zero leaves the
	// runtime default in place.
	memLimitBytes int64
	LastBuild     time.Duration
	LastRebuild   time.Time
	LastState     protocol.State
	// graphicsModes lists the image modes available to the i key in cycle
	// order; graphicsMode is the current one.
	graphicsModes []GraphicsMode
	graphicsMode  GraphicsMode
}

// SessionConfig is the engine-side configuration the app applies before the
// interactive transport starts. Terminal setup stays in the app/front end.
type SessionConfig struct {
	Loader         *Loader
	BrowserContext *BrowserContext
	Width, Height  int
	Zoom           float64
	// PreserveZoom makes this session join the current browser-wide zoom
	// instead of installing Zoom and resetting every other tab.
	PreserveZoom  bool
	Interactive   bool
	Reader, Dark  bool
	Home          string
	Color         bool
	NoJS, NoPDF   bool
	LayoutBudget  LayoutBudget
	MemLimitBytes int64
	NoBigText     bool
	CellW, CellH  int
	Graphics      GraphicsMode
	GraphicsModes []GraphicsMode
	Caps          protocol.Capabilities
}

// NewSession creates and configures the page session used by the interactive
// loop. It is the app's only construction entry point while the engine type is
// still in transition.
func NewConfiguredSession(c SessionConfig) *Session {
	b := newSessionWithBrowserContext(c.Width, c.Height, c.BrowserContext, !c.PreserveZoom)
	if c.PreserveZoom {
		b.Zoom = Zoom()
	} else {
		b.Zoom = c.Zoom
		SetZoom(c.Zoom)
	}
	b.zoomGen = zoomGen()
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
