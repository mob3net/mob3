package app

import (
	"os"
	"strings"
	"time"

	"browser/internal/engine"
	"browser/internal/frontend"
	"browser/internal/host"
	"browser/internal/protocol"
)

// tabCloseGrace is how long a normal close may wait before the backend is
// force-discarded.
const (
	tabCloseGrace    = 2 * time.Second
	probeTimeout     = 10 * time.Second
	probePongTimeout = 2 * time.Second
)

// interactiveTabs owns the per-tab backends for the interactive loop. The host,
// browser context and settings stay shared; each tab gets its own loader,
// document session and budget share.
type interactiveTabs struct {
	hostSess     *host.Session
	loaderConfig engine.LoaderConfig
	home         string
	cellW, cellH int
	closeGrace   time.Duration
	newBackend   tabBackendFactory
	backends     map[*frontend.Tab]tabBackend
	onDiscard    func(*frontend.Tab)
}

func newInteractiveTabs(hostSess *host.Session, loaderConfig engine.LoaderConfig, home string, cellW, cellH int, newSession func(*engine.Loader, int, int) *engine.Session) *interactiveTabs {
	if strings.TrimSpace(home) == "" {
		home = "about:home"
	}
	return &interactiveTabs{
		hostSess: hostSess, loaderConfig: loaderConfig, home: home,
		cellW: cellW, cellH: cellH, closeGrace: tabCloseGrace,
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
	tab.Session = frontend.NewProbeSession(tab.Session, probeTimeout, probePongTimeout, func() {
		t.discard(tab)
	})
	tab.State.Clipboard = os.Stdout
	t.backends[tab] = backend
	return tab, nil
}

func (t *interactiveTabs) setDiscard(fn func(*frontend.Tab)) {
	t.onDiscard = fn
}

func (t *interactiveTabs) discard(tab *frontend.Tab) {
	if t.onDiscard != nil {
		t.onDiscard(tab)
		return
	}
	t.kill(tab)
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

// close releases a tab's backend. If the graceful close wedges, the backend is
// force-discarded so quitting or closing a tab cannot hang the browser.
func (t *interactiveTabs) close(tab *frontend.Tab) {
	if tab == nil {
		return
	}
	if backend := t.backends[tab]; backend != nil {
		done := make(chan struct{})
		go func() {
			backend.Close()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(t.closeGrace):
			backend.Kill()
		}
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
