package protocol

import "strings"

// SanitizeClipboard removes terminal controls and display-format characters
// from text that is about to leave the browser as clipboard content. Newlines
// and tabs stay because they are ordinary copied text.
func SanitizeClipboard(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 32 || (r >= 0x7f && r < 0xa0) || isDisplayFormat(r) {
			return -1
		}
		return r
	}, s)
}
