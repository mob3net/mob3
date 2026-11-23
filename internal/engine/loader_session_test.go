package engine

import (
	"browser/internal/host"
	"browser/internal/protocol"
)

func newTestLoader() *Loader { return NewLoader(host.NewSession()) }

func newTestSession(w, h int) *Session {
	s := NewSession(w, h)
	s.setLoader(newTestLoader())
	return s
}

var errResourceTooLarge = protocol.ErrResourceTooLarge
