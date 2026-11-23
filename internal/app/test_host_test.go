package app

import (
	"browser/internal/engine"
	"browser/internal/host"
	"browser/internal/protocol"
)

// init installs the in-process host for tests that create a default loader
// through the browser compatibility constructors.
func init() {
	engine.SetHostFactory(func() protocol.Host { return host.NewSession() })
}
