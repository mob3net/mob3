package frontend

import (
	"encoding/base64"
	"time"

	"browser/internal/protocol"
)

// maxClipboardBytes matches the engine's clipboard budget. The frontend
// enforces it again before encoding, because the transport can carry a larger
// event from a compromised or buggy engine.
const maxClipboardBytes = 100 * 1024

// clipboardPendingWindow gives the engine time to emit the Clipboard event
// caused by the user's copy action. It is not an idle timer.
const clipboardPendingWindow = 2 * time.Second

// expectClipboard authorizes the next Clipboard event for a user copy action.
func (l *Tab) expectClipboard() {
	l.clipboardPendingUntil = time.Now().Add(clipboardPendingWindow)
}

// takeClipboardPermission consumes the one-shot copy authorization. A stale or
// absent permission means the event is unsolicited and must be ignored.
func (l *Tab) takeClipboardPermission() bool {
	if l.clipboardPendingUntil.IsZero() || time.Now().After(l.clipboardPendingUntil) {
		l.clipboardPendingUntil = time.Time{}
		return false
	}
	l.clipboardPendingUntil = time.Time{}
	return true
}

// clipboardUnavailable reports a failed or missing terminal clipboard write
// instead of leaving the engine's "Copied" status in place.
func (l *Tab) clipboardUnavailable() {
	st := l.State.Page
	st.Status = "Clipboard unavailable"
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		_ = l.Presenter.Refresh()
	}
}

// OSC52 encodes text as an OSC 52 clipboard sequence for the terminal. It
// strips terminal controls and truncates to the clipboard budget first.
func OSC52(text string) string {
	text = protocol.SanitizeClipboard(text)
	if len(text) > maxClipboardBytes {
		cut := maxClipboardBytes
		for cut > 0 && (text[cut]&0xc0) == 0x80 {
			cut--
		}
		text = text[:cut]
	}
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
}
