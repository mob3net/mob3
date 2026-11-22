package frontend

import (
	"bytes"
	"image"
	"io"
	"strings"
	"testing"

	"browser/internal/protocol"
)

func TestApplySessionEventPresentsNativeFrame(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	p.SetGraphics(GraphicsKitty, 8, 16)
	loop := &SessionLoop{Presenter: p, Stderr: io.Discard}
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.State{Flags: protocol.StateNativeImages}}); err != nil {
		t.Fatal(err)
	}
	data, err := encodePNG(image.NewNRGBA(image.Rect(0, 0, 16, 16)))
	if err != nil {
		t.Fatal(err)
	}
	f := protocol.Frame{W: 8, H: 3, Cells: make([]protocol.Cell, 24), Placements: []protocol.Placement{{
		Key: "1", W: 2, H: 1, TotalRows: 1, Format: protocol.PlacementPNG, Data: data,
	}}}
	for i := range f.Cells {
		f.Cells[i].R = 'x'
	}
	if _, err := loop.applyEvent(protocol.Event{Body: f}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "x") {
		t.Fatal("native frame text was not presented")
	}
	if !strings.Contains(got, "\x1b_G") {
		t.Fatal("native placement was not presented")
	}
}

func TestApplySessionEventDeduplicatesCrash(t *testing.T) {
	p := NewPresenter(io.Discard, false)
	loop := &SessionLoop{Presenter: p, Stderr: io.Discard}
	var reports []string
	report := func(msg string) { reports = append(reports, msg) }
	loop.Report = report
	crash := protocol.Event{Body: protocol.Crash{Message: "bad\x1b[31m"}}
	if _, err := loop.applyEvent(crash); err != nil {
		t.Fatal(err)
	}
	if _, err := loop.applyEvent(crash); err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 {
		t.Fatalf("reports = %d, want 1 for repeated crash", len(reports))
	}
	if strings.Contains(reports[0], "\x1b") {
		t.Fatalf("report kept a control sequence: %q", reports[0])
	}
	frame := protocol.Frame{W: 1, H: 1, Cells: []protocol.Cell{{R: 'x'}}}
	if _, err := loop.applyEvent(protocol.Event{Body: frame}); err != nil {
		t.Fatal(err)
	}
	if _, err := loop.applyEvent(crash); err != nil {
		t.Fatal(err)
	}
	if len(reports) != 2 {
		t.Fatalf("reports = %d, want 2 after a good frame re-armed reporting", len(reports))
	}
}

func TestApplySessionEventCrashForcesFullRepaint(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	loop := &SessionLoop{Presenter: p, Stderr: io.Discard}
	f := protocol.Frame{W: 2, H: 2, Cells: []protocol.Cell{{R: 'a'}, {R: 'b'}, {R: 'c'}, {R: 'd'}}}
	if _, err := loop.applyEvent(protocol.Event{Body: f}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if _, err := loop.applyEvent(protocol.Event{Body: protocol.Crash{Message: "boom"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := loop.applyEvent(protocol.Event{Body: f}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b[2J") {
		t.Fatalf("frame after Crash did not clear the screen: %q", out.String())
	}
}

func TestPresenterRepaintsAfterConsoleFrame(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	loop := &SessionLoop{Presenter: p, Stderr: io.Discard}
	page := protocol.Frame{W: 2, H: 2, Cells: []protocol.Cell{{R: 'a'}, {R: 'b'}, {R: 'p'}, {R: 'q'}}}
	console := protocol.Frame{W: 2, H: 2, Cells: []protocol.Cell{{R: 'x'}, {R: 'y'}, {R: 'c'}, {R: 'd'}}}
	for _, f := range []protocol.Frame{page, console} {
		if _, err := loop.applyEvent(protocol.Event{Body: f}); err != nil {
			t.Fatal(err)
		}
	}
	out.Reset()
	if _, err := loop.applyEvent(protocol.Event{Body: page}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "p") {
		t.Fatalf("page was not repainted after the console: %q", out.String())
	}
}
