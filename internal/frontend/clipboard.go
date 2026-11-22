package frontend

import "encoding/base64"

// OSC52 encodes text as an OSC 52 clipboard sequence for the terminal.
func OSC52(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
}
