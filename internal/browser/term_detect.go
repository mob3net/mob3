package browser

// Terminal capability detection. This file is frontend-owned: the engine's
// paint path takes capabilities as input and never reads the environment.

import (
	"os"
	"strings"
)

// DetectColorMode reports the color depth to use. macOS Terminal.app does not
// implement 24-bit SGR sequences and garbles them, so it gets 256-color output;
// terminals that advertise truecolor through COLORTERM keep 24-bit output.
func DetectColorMode() ColorMode {
	ct := strings.ToLower(os.Getenv("COLORTERM"))
	if strings.Contains(ct, "truecolor") || strings.Contains(ct, "24bit") {
		return ColorTrueColor
	}
	if strings.EqualFold(os.Getenv("TERM_PROGRAM"), "Apple_Terminal") {
		return Color256
	}
	return ColorTrueColor
}

func inMultiplexer() bool {
	term := strings.ToLower(os.Getenv("TERM"))
	return os.Getenv("TMUX") != "" || os.Getenv("ZELLIJ") != "" ||
		strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux")
}

func kittyTerminal() bool {
	term := strings.ToLower(os.Getenv("TERM"))
	program := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	if strings.Contains(term, "kitty") || os.Getenv("KITTY_WINDOW_ID") != "" {
		return true
	}
	if strings.Contains(program, "wezterm") || os.Getenv("WEZTERM_PANE") != "" {
		return true
	}
	if strings.Contains(program, "ghostty") || os.Getenv("GHOSTTY_RESOURCES_DIR") != "" {
		return true
	}
	return false
}

// kittyKeyboardEnabled reports whether the terminal is expected to implement
// the kitty keyboard protocol. Multiplexers are excluded because their
// forwarding of the push/pop sequences is inconsistent.
func kittyKeyboardEnabled() bool {
	if inMultiplexer() || os.Getenv("STY") != "" {
		return false
	}
	if kittyTerminal() {
		return true
	}
	return strings.HasPrefix(strings.ToLower(os.Getenv("TERM")), "foot")
}

func itermTerminal() bool {
	program := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	return strings.Contains(program, "iterm") || strings.EqualFold(os.Getenv("LC_TERMINAL"), "iTerm2")
}
