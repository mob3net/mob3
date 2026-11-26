package app

import "browser/internal/protocol"

// runOptions is the validated command-line surface handed to one run.
type runOptions struct {
	Dump, PNG     bool
	Width, Height int
	PDFPage       int
	PDFPageLabel  string
	NoColor       bool
	Color         bool
	UA, Certs     string
	Search        string
	Graphics      string
	NoJS, NoPDF   bool
	Reader, Dark  bool
	IgnoreBlocks  bool
	Home          string
	TextScale     string
	MemLimit      int
	LayoutBudget  string
	Zoom          string
	Engine        string
	MCP           bool
	Target        string
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

// cleanError keeps page- or URL-derived error text out of the terminal's
// control, bidi and zero-width sequences.
func cleanError(err error) string {
	if err == nil {
		return ""
	}
	return protocol.StripControls(err.Error())
}
