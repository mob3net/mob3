package engine

import (
	"errors"

	"browser/internal/protocol"
)

// The PNG encoder is injected by the app (and by tests) for the MCP screenshot
// tool. ANSI encoding belongs to the front end.
var defaultPNGEncoder func(protocol.Frame, int) ([]byte, error)

// SetPNGEncoder installs the PNG encoder for this session.
func (b *Session) SetPNGEncoder(png func(protocol.Frame, int) ([]byte, error)) {
	b.pngEncoder = png
}

// SetPNGEncoder installs the PNG encoder for a headless automation session.
func (a *Automation) SetPNGEncoder(png func(protocol.Frame, int) ([]byte, error)) {
	a.b.SetPNGEncoder(png)
}

func (b *Session) screenshotPNG(g *Grid, scale int) ([]byte, error) {
	enc := b.pngEncoder
	if enc == nil {
		enc = defaultPNGEncoder
	}
	if enc == nil {
		return nil, errNoPNGEncoder
	}
	return enc(toProtocolFrame(g, 0), scale)
}

var errNoPNGEncoder = errors.New("no PNG encoder")
