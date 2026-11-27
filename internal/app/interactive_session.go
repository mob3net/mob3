package app

import (
	"strings"

	"browser/internal/engine"
	"browser/internal/frontend"
)

// interactiveSessionFactory holds the per-session configuration that stays
// constant for every tab in one interactive run.
type interactiveSessionFactory struct {
	o              runOptions
	browserContext *engine.BrowserContext
	budget         engine.LayoutBudget
	zoomFactor     float64
	cellW, cellH   int
	graphicsMode   frontend.GraphicsMode
	noBigText      bool
}

func newInteractiveSessionFactory(o runOptions, browserContext *engine.BrowserContext, budget engine.LayoutBudget, zoomFactor float64, cellW, cellH int, graphicsMode frontend.GraphicsMode) interactiveSessionFactory {
	return interactiveSessionFactory{
		o: o, browserContext: browserContext, budget: budget, zoomFactor: zoomFactor,
		cellW: cellW, cellH: cellH, graphicsMode: graphicsMode,
		noBigText: bigTextDisabled(o.TextScale),
	}
}

func bigTextDisabled(textScale string) bool {
	switch strings.ToLower(strings.TrimSpace(textScale)) {
	case "off", "none", "false":
		return true
	}
	return false
}

// newSession builds one configured engine session for a tab.
func (f interactiveSessionFactory) newSession(tabLoader *engine.Loader, w, h int) *engine.Session {
	o := f.o
	b := engine.NewConfiguredSession(engine.SessionConfig{
		Loader: tabLoader, BrowserContext: f.browserContext,
		Width: w, Height: h, Zoom: f.zoomFactor, PreserveZoom: true, Interactive: true,
		Reader: o.Reader, Dark: o.Dark, Home: o.Home,
		Color: !o.NoColor, NoJS: o.NoJS, NoPDF: o.NoPDF,
		LayoutBudget:  f.budget,
		MemLimitBytes: int64(o.MemLimit) << 20, NoBigText: f.noBigText,
		CellW: f.cellW, CellH: f.cellH, Graphics: f.graphicsMode,
		GraphicsModes: graphicsModesFor(f.graphicsMode),
		Caps:          capabilitiesFor(nativeGraphicsMode(f.graphicsMode), f.cellW, f.cellH),
	})
	b.SetPNGEncoder(frontend.RenderPNG)
	return b
}
