package browser

import (
	"strconv"
	"strings"
)

// MouseEvent is a decoded terminal mouse report.
type MouseEvent struct {
	Button int // 0 left, 1 middle, 2 right
	X, Y   int // 1-based terminal cell coordinates
	Press  bool
	Motion bool // drag report while a button is held
	Wheel  int  // -1 wheel up, +1 wheel down, 0 otherwise
}

const (
	mouseWheelUp   = 64
	mouseWheelDown = 65
)

// parseSGRMouse decodes the parameters of an SGR mouse report. params is the
// text between CSI and the final 'M'/'m' (for example "<0;12;5"); release is
// true for the lowercase final byte.
func parseSGRMouse(params string, release bool) (MouseEvent, bool) {
	params = strings.TrimPrefix(params, "<")
	parts := strings.Split(params, ";")
	if len(parts) != 3 {
		return MouseEvent{}, false
	}
	b, err1 := strconv.Atoi(parts[0])
	x, err2 := strconv.Atoi(parts[1])
	y, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return MouseEvent{}, false
	}
	ev := MouseEvent{X: x, Y: y, Press: !release, Motion: b&32 != 0}
	switch {
	case b&64 != 0:
		ev.Button = b & 3
		if ev.Button == 1 {
			ev.Wheel = 1
		} else {
			ev.Wheel = -1
		}
	case release:
		ev.Button = b & 3
	default:
		ev.Button = b & 3
	}
	return ev, true
}

// parseX10Mouse decodes a legacy 3-byte X10 mouse report.
func parseX10Mouse(b, x, y byte) (MouseEvent, bool) {
	btn := int(b) - 32
	x -= 32
	y -= 32
	if x < 1 || y < 1 || btn < 0 {
		return MouseEvent{}, false
	}
	ev := MouseEvent{X: int(x), Y: int(y), Motion: btn&32 != 0}
	if btn&64 != 0 {
		ev.Press = true
		ev.Button = btn & 3
		if ev.Button == 1 {
			ev.Wheel = 1
		} else {
			ev.Wheel = -1
		}
		return ev, true
	}
	if btn&3 == 3 {
		ev.Button = 0
		ev.Press = false
		return ev, true
	}
	ev.Button = btn & 3
	ev.Press = true
	return ev, true
}

// mouseToken encodes a mouse event as an input token.
func mouseToken(ev MouseEvent) string {
	if ev.Wheel != 0 {
		dir := "up"
		if ev.Wheel > 0 {
			dir = "down"
		}
		return "mouse:wheel:" + dir + ":" + strconv.Itoa(ev.X) + ":" + strconv.Itoa(ev.Y)
	}
	kind := "release"
	if ev.Press {
		kind = "press"
	}
	if ev.Motion {
		kind = "drag"
	}
	return "mouse:" + kind + ":" + strconv.Itoa(ev.Button) + ":" + strconv.Itoa(ev.X) + ":" + strconv.Itoa(ev.Y)
}

// ParseMouseToken decodes a token produced by mouseToken.
func ParseMouseToken(tok string) (MouseEvent, bool) {
	if !strings.HasPrefix(tok, "mouse:") {
		return MouseEvent{}, false
	}
	parts := strings.Split(strings.TrimPrefix(tok, "mouse:"), ":")
	switch {
	case len(parts) == 4 && parts[0] == "wheel":
		x, err1 := strconv.Atoi(parts[2])
		y, err2 := strconv.Atoi(parts[3])
		if err1 != nil || err2 != nil {
			return MouseEvent{}, false
		}
		ev := MouseEvent{X: x, Y: y}
		if parts[1] == "up" {
			ev.Wheel = -1
		} else {
			ev.Wheel = 1
		}
		return ev, true
	case len(parts) == 4 && (parts[0] == "press" || parts[0] == "release" || parts[0] == "drag"):
		btn, err1 := strconv.Atoi(parts[1])
		x, err2 := strconv.Atoi(parts[2])
		y, err3 := strconv.Atoi(parts[3])
		if err1 != nil || err2 != nil || err3 != nil {
			return MouseEvent{}, false
		}
		return MouseEvent{
			Button: btn, X: x, Y: y, Press: parts[0] != "release",
			Motion: parts[0] == "drag",
		}, true
	}
	return MouseEvent{}, false
}
