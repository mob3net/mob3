package engine

import (
	"strings"
	"testing"
)

func TestSerializeSVGRestoresCamelCase(t *testing.T) {
	root := ParseHTML(`<svg viewbox="0 0 10 10"><lineargradient id="g"></lineargradient></svg>`)
	svg := root.FindFirst("svg")
	out := SerializeSVG(svg)
	if !strings.Contains(out, `viewBox="0 0 10 10"`) {
		t.Fatalf("viewBox not restored: %s", out)
	}
	if !strings.Contains(out, "linearGradient") {
		t.Fatalf("linearGradient tag not restored: %s", out)
	}
	if _, err := DecodeSVG([]byte(out)); err != nil {
		t.Fatalf("serialized SVG did not decode: %v", err)
	}
}
