package frontend

import "browser/internal/protocol"

type MouseEvent = protocol.MouseEvent

const (
	MouseButtonLeft    = protocol.MouseButtonLeft
	MouseButtonMiddle  = protocol.MouseButtonMiddle
	MouseButtonRight   = protocol.MouseButtonRight
	MouseButtonBack    = protocol.MouseButtonBack
	MouseButtonForward = protocol.MouseButtonForward
)

func ParseSGRMouse(params string, release bool) (MouseEvent, bool) {
	return protocol.ParseSGRMouse(params, release)
}

func ParseX10Mouse(b, x, y byte) (MouseEvent, bool) { return protocol.ParseX10Mouse(b, x, y) }

func MouseToken(ev MouseEvent) string { return protocol.MouseToken(ev) }

func ParseMouseToken(tok string) (MouseEvent, bool) { return protocol.ParseMouseToken(tok) }
