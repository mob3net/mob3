package frontend

import (
	"bytes"
	"image/png"
	"strings"
	"testing"

	"browser/internal/protocol"
)

func testFrame() protocol.Frame {
	return protocol.Frame{
		W: 3,
		H: 1,
		Cells: []protocol.Cell{
			{R: 'A', FG: protocol.Color{R: 255, Set: true}},
			{R: 'B', FG: protocol.Color{G: 255, Set: true}, Attrs: protocol.AttrBold | protocol.AttrUnderline},
			{R: ' ', BG: protocol.Color{B: 255, Set: true}},
		},
	}
}

func TestEncodeFrameColorAndMono(t *testing.T) {
	f := testFrame()
	color := EncodeFrame(f, true, false)
	if len(color) != 1 {
		t.Fatalf("rows = %d", len(color))
	}
	if !strings.Contains(color[0], "A") || !strings.Contains(color[0], "B") {
		t.Fatalf("color row = %q", color[0])
	}
	if !strings.Contains(color[0], "38;2;255;0;0") {
		t.Fatalf("foreground missing: %q", color[0])
	}
	if !strings.HasSuffix(color[0], "\x1b[0m") {
		t.Fatalf("color row has no reset: %q", color[0])
	}

	mono := EncodeFrame(f, false, true)
	if strings.Contains(mono[0], "\x1b[38") || strings.Contains(mono[0], "\x1b[48") {
		t.Fatalf("mono row contains color: %q", mono[0])
	}
	if !strings.Contains(mono[0], "\x1b[0;1;4m") {
		t.Fatalf("mono attributes missing: %q", mono[0])
	}
	if !strings.HasSuffix(mono[0], "\x1b[0m") {
		t.Fatalf("mono row has no reset: %q", mono[0])
	}
}

func TestEncodeFrameScaledRun(t *testing.T) {
	f := protocol.Frame{
		W: 5,
		H: 1,
		Cells: []protocol.Cell{
			{R: ' ', Run: 1}, {Flags: protocol.CellSkip}, {Flags: protocol.CellSkip}, {Flags: protocol.CellSkip},
			{R: 'Z'},
		},
		Runs: []protocol.ScaledRun{{X: 0, Y: 0, Cells: 4, Scale: 2, Text: "Hi", FG: protocol.Color{Set: true}}},
	}
	rows := EncodeFrame(f, true, false)
	if !strings.Contains(rows[0], "\x1b]66;s=2;Hi\x07") {
		t.Fatalf("scaled run missing: %q", rows[0])
	}
	mono := EncodeFrame(f, false, false)
	if strings.Contains(mono[0], "\x1b") {
		t.Fatalf("mono scaled run contains an escape: %q", mono[0])
	}
	if !strings.HasPrefix(mono[0], "    ") {
		t.Fatalf("mono scaled run width = %q", mono[0])
	}
}

func TestRenderPNG(t *testing.T) {
	f := protocol.Frame{
		W: 2,
		H: 1,
		Cells: []protocol.Cell{
			{R: 'A', FG: protocol.Color{R: 10, G: 20, B: 30, A: 255, Set: true}, BG: protocol.Color{Set: true}},
			{R: ' ', BG: protocol.Color{R: 240, G: 240, B: 240, A: 255, Set: true}},
		},
	}
	data, err := RenderPNG(f, 1)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 16 || b.Dy() != 16 {
		t.Fatalf("bounds = %v", b)
	}
	if _, err := RenderPNG(protocol.Frame{}, 1); err == nil {
		t.Fatal("empty frame rendered")
	}
}
