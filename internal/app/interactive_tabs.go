package app

import (
	"os"
	"strings"

	"browser/internal/engine"
	"browser/internal/frontend"
	"browser/internal/host"
	"browser/internal/protocol"
)

// interactiveTabs owns the per-tab backends for the interactive loop. The host,
// browser context and settings stay shared; each tab gets its own loader,
// document session and budget share.
type interactiveTabs struct {
	hostSess     *host.Session
	loaderConfig engine.LoaderConfig
	home         string
	cellW, cellH int
	newBackend   tabBackendFactory
	backends     map[*frontend.Tab]tabBackend
}

func newInteractiveTabs(hostSess *host.Session, loaderConfig engine.LoaderConfig, home string, cellW, cellH int, newSession func(*engine.Loader, int, int) *engine.Session) *interactiveTabs {
	if strings.TrimSpace(home) == "" {
		home = "about:home"
	}
	return &interactiveTabs{
		hostSess: hostSess, loaderConfig: loaderConfig, home: home,
		cellW: cellW, cellH: cellH,
		newBackend: newInProcBackendFactory(newSession),
		backends:   make(map[*frontend.Tab]tabBackend),
	}
}

// openWithLoader starts one tab on an existing loader and navigates to target.
func (t *interactiveTabs) openWithLoader(id protocol.SessionID, w, h, contentTop, contentHeight int, target string, loader *engine.Loader) (*frontend.Tab, error) {
	init := protocol.Init{
		Width: w, Height: h, ContentTop: contentTop, ContentHeight: contentHeight,
		CellW: t.cellW, CellH: t.cellH, Zoom: 0,
	}
	backend, err := t.newBackend(loader, id, init, target)
	if err != nil {
		return nil, err
	}
	tab := &frontend.Tab{
		Session: backend.Session(), SessionID: id, Stderr: os.Stderr,
		AttestedURL: t.hostSess.AttestedURL,
	}
	tab.State.Clipboard = os.Stdout
	t.backends[tab] = backend
	return tab, nil
}

// open creates a fresh loader for a new tab and opens target, or the home page
// when target is empty.
func (t *interactiveTabs) open(id protocol.SessionID, w, h, contentTop, contentHeight int, target string) (*frontend.Tab, error) {
	if strings.TrimSpace(target) == "" {
		target = t.home
	}
	loader, err := engine.NewConfiguredLoader(t.hostSess, t.loaderConfig)
	if err != nil {
		return nil, err
	}
	return t.openWithLoader(id, w, h, contentTop, contentHeight, target, loader)
}

// close releases a tab's backend.
func (t *interactiveTabs) close(tab *frontend.Tab) {
	if tab == nil {
		return
	}
	if backend := t.backends[tab]; backend != nil {
		backend.Close()
		delete(t.backends, tab)
		return
	}
	if tab.Session != nil {
		_ = tab.Session.Close()
	}
}

// kill discards a tab backend without waiting for the engine to finish.
func (t *interactiveTabs) kill(tab *frontend.Tab) {
	if tab == nil {
		return
	}
	if backend := t.backends[tab]; backend != nil {
		backend.Kill()
		delete(t.backends, tab)
		return
	}
	if tab.Session != nil {
		_ = tab.Session.Close()
	}
}
