package app

import (
	"browser/internal/browser"
	"browser/internal/host"
	"browser/internal/protocol"
)

// init installs the in-process host for tests that create a default loader
// through the browser compatibility constructors.
func init() {
	browser.SetHostFactory(func() protocol.Host { return host.NewSession() })
}
