package browser

import "browser/internal/protocol"

// hostFactory lets the app inject the host-side session without the browser
// importing internal/host.
var hostFactory func() protocol.Host

// SetHostFactory installs the host-session constructor for new loaders.
func SetHostFactory(f func() protocol.Host) { hostFactory = f }

func newLoaderHost() protocol.Host {
	if hostFactory == nil {
		return nil
	}
	return hostFactory()
}
