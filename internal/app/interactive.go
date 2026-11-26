package app

import (
	"fmt"
	"os"

	"browser/internal/engine"
	"browser/internal/frontend"
	"browser/internal/host"
)

// runInteractive boots the terminal session after flags, loader and shared
// browser context are ready. It falls back to the one-shot dump path when the
// terminal cannot enter raw mode.
func runInteractive(o runOptions, hostSess *host.Session, loader *engine.Loader, loaderConfig engine.LoaderConfig, browserContext *engine.BrowserContext, graphicsMode frontend.GraphicsMode, budget engine.LayoutBudget, zoomFactor float64) int {
	stdin := os.Stdin
	state, err := frontend.MakeRaw(stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mob3: interactive mode unavailable (%s); rendering to stdout.\n", cleanError(err))
		return runDump(o, loader, budget, zoomFactor)
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
	engine.SetZoom(zoomFactor)

	sessions := newInteractiveSessionFactory(o, browserContext, budget, zoomFactor, cellW, cellH, graphicsMode)
	tabs := newInteractiveTabs(hostSess, loaderConfig, o.Home, cellW, cellH, sessions.newSession)
	if o.Process == "per-tab" {
		tabs.newBackend = newProcessBackendFactory(
			hostSess, browserContext, os.Args[1:],
			capabilitiesFor(nativeGraphicsMode(graphicsMode), cellW, cellH),
			zoomFactor, int(graphicsMode),
		)
	}

	presenter := frontend.NewPresenter(os.Stdout, !o.NoColor)
	presenterMode := graphicsMode
	if presenterMode == frontend.GraphicsAuto || presenterMode == frontend.GraphicsBlocks || presenterMode == frontend.GraphicsNone {
		presenterMode = frontend.DetectGraphics(frontend.GraphicsAuto)
	}
	presenter.SetGraphics(presenterMode, cellW, cellH)

	kittyKeys := frontend.KittyKeyboardEnabled()
	_, _ = os.Stdout.WriteString(enterTerminal(kittyKeys))

	stuck := make(chan *frontend.Tab, 4)
	tabs.setDiscard(func(tab *frontend.Tab) {
		select {
		case stuck <- tab:
		default:
		}
	})
	failed := make(chan frontend.TabFailure, 4)
	tabs.setFailure(func(tab *frontend.Tab, message string) {
		select {
		case failed <- frontend.TabFailure{Tab: tab, Message: message}:
		default:
		}
	})

	top, contentH := frontend.ContentBounds(h)
	tab, err := tabs.openWithLoader(1, w, h, top, contentH, o.Target, loader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mob3: session: %s\n", cleanError(err))
		leaveTerminal(os.Stdout, kittyKeys, presenter)
		frontend.RestoreTerm(stdin, state)
		return 1
	}

	var loop *frontend.SessionLoop
	cleanup := func() {
		if loop != nil {
			loop.CloseAll()
		}
		leaveTerminal(os.Stdout, kittyKeys, presenter)
		frontend.RestoreTerm(stdin, state)
	}
	defer cleanup()

	keys := make(chan string, 32)
	go frontend.ReadKeyEvents(stdin, keys, nil)

	winch := make(chan os.Signal, 1)
	quitSig := make(chan os.Signal, 1)
	frontend.StartSignalHandlers(winch, quitSig)
	defer frontend.StopSignalHandlers(winch, quitSig)

	loop = &frontend.SessionLoop{
		Tabs: []*frontend.Tab{tab}, Active: tab, Presenter: presenter,
		Keys: keys, Resize: winch, Quit: quitSig, Stuck: stuck, Failed: failed,
		OpenTab:      tabs.open,
		CloseTab:     tabs.close,
		KillTab:      tabs.kill,
		TerminalSize: func() (int, int, bool) { return frontend.TerminalSize(os.Stdout) },
		CellW:        cellW, CellH: cellH,

		GraphicsModes:   graphicsModesFor(graphicsMode),
		CurrentGraphics: graphicsMode,
	}
	if err := loop.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "mob3: %s\n", cleanError(err))
		return 1
	}
	return 0
}
