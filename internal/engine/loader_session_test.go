package engine

import "browser/internal/protocol"

var errResourceTooLarge = protocol.ErrResourceTooLarge

func newTestLoader() *Loader { return NewLoader(newLoaderHost()) }
