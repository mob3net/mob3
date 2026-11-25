package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Type sends text to the focused input, textarea or URL prompt.
func (a *Automation) Type(ctx context.Context, text string) error {
	if a == nil || a.b == nil {
		return errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.Forms.InputFocus == nil {
		return errors.New("no focused input; click a text field first")
	}
	if utf8LenString(text) > 20000 {
		return errors.New("text is too long")
	}
	for _, r := range text {
		var key string
		switch r {
		case '\n':
			key = "enter"
		case '\t':
			key = "tab"
		default:
			key = string(r)
		}
		if b.HandleKey(key) {
			return errors.New("browser requested quit")
		}
	}
	return a.waitLoad(ctx)
}

func utf8LenString(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// Press sends one named key (Enter, Tab, Escape, arrows, PageDown, …) to the
// browser and waits for any resulting navigation.
func (a *Automation) Press(ctx context.Context, key string) error {
	if a == nil || a.b == nil {
		return errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	token := keyToken(key)
	if token == "" {
		return fmt.Errorf("unknown key %q", key)
	}
	if a.b.HandleKey(token) {
		return errors.New("browser requested quit")
	}
	return a.waitLoad(ctx)
}

func keyToken(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "enter", "return":
		return "enter"
	case "tab":
		return "tab"
	case "escape", "esc":
		return "esc"
	case "backspace":
		return "backspace"
	case "delete", "del":
		return "delete"
	case "arrowup", "up":
		return "up"
	case "arrowdown", "down":
		return "down"
	case "arrowleft", "left":
		return "left"
	case "arrowright", "right":
		return "right"
	case "home":
		return "home"
	case "end":
		return "end"
	case "pageup", "pgup":
		return "pgup"
	case "pagedown", "pgdn":
		return "pgdn"
	case "space":
		return " "
	}
	if len([]rune(key)) == 1 {
		return key
	}
	return ""
}

// Scroll moves the page by lines, or to the top/bottom.
func (a *Automation) Scroll(lines, pages int, top, bottom bool) {
	if a == nil || a.b == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case top:
		a.b.Scroll = 0
		a.b.clampScroll()
	case bottom:
		a.b.Scroll = a.b.maxScroll()
	case pages != 0:
		a.b.scrollBy(pages * a.b.viewportHeight())
	default:
		a.b.scrollBy(lines)
	}
}
