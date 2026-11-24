package app

import (
	"context"
	"os"
	"strings"

	"browser/internal/engine"
	"browser/internal/frontend"
	"browser/internal/host"
	"browser/internal/protocol"
)

// interactiveTabs owns the per-tab engine sessions and transports for the
// interactive loop. The host, browser context and settings stay shared; each
// tab gets its own loader, document session and budget share.
type interactiveTabs struct {
	hostSess     *host.Session
	loaderConfig engine.LoaderConfig
	home         string
	cellW, cellH int
	newSession   func(*engine.Loader, int, int) *engine.Session
	engines      map[*frontend.Tab]*engine.Session
}

func newInteractiveTabs(hostSess *host.Session, loaderConfig engine.LoaderConfig, home string, cellW, cellH int, newSession func(*engine.Loader, int, int) *engine.Session) *interactiveTabs {
	if strings.TrimSpace(home) == "" {
		home = "about:home"
	}
	return &interactiveTabs{
		hostSess: hostSess, loaderConfig: loaderConfig, home: home,
		cellW: cellW, cellH: cellH, newSession: newSession,
		engines: make(map[*frontend.Tab]*engine.Session),
	}
}

// openWithLoader starts one tab on an existing loader and navigates to target.
func (t *interactiveTabs) openWithLoader(id protocol.SessionID, w, h, contentTop, contentHeight int, target string, loader *engine.Loader) (*frontend.Tab, error) {
	b := t.newSession(loader, w, h)
	session, err := (&protocol.InProc{}).Open(context.Background(), id, b)
	if err != nil {
		b.Release()
		return nil, err
	}
	if err := session.Send(protocol.Command{Body: protocol.Init{
		Width: w, Height: h, ContentTop: contentTop, ContentHeight: contentHeight,
		CellW: t.cellW, CellH: t.cellH, Zoom: 0, Caps: b.Capabilities(),
	}}); err != nil {
		_ = session.Close()
		b.Release()
		return nil, err
	}
	if err := session.Send(protocol.Command{Body: protocol.Navigate{URL: target, Push: true}}); err != nil {
		_ = session.Close()
		b.Release()
		return nil, err
	}
	tab := &frontend.Tab{
		Session: session, SessionID: id, Stderr: os.Stderr,
		AttestedURL: t.hostSess.AttestedURL,
	}
	tab.State.Clipboard = os.Stdout
	t.engines[tab] = b
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

// close releases a tab's transport and engine session.
func (t *interactiveTabs) close(tab *frontend.Tab) {
	if tab == nil {
		return
	}
	if tab.Session != nil {
		_ = tab.Session.Close()
	}
	if b := t.engines[tab]; b != nil {
		b.Release()
		delete(t.engines, tab)
	}
}
