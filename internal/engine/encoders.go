package engine

import (
	"errors"

	"browser/internal/protocol"
)

// Encoders are injected by the app (and by tests). The browser never imports
// the frontend package for terminal encoding.
var (
	defaultFrameEncoder func(protocol.Frame, bool, bool) []string
	defaultPNGEncoder   func(protocol.Frame, int) ([]byte, error)
)

// SetEncoders installs the frame and PNG encoders for this session.
func (b *Session) SetEncoders(frame func(protocol.Frame, bool, bool) []string, png func(protocol.Frame, int) ([]byte, error)) {
	b.frameEncoder = frame
	b.pngEncoder = png
}

// SetEncoders installs encoders for a headless automation session.
func (a *Automation) SetEncoders(frame func(protocol.Frame, bool, bool) []string, png func(protocol.Frame, int) ([]byte, error)) {
	a.b.SetEncoders(frame, png)
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
