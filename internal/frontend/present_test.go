package frontend

import (
	"bytes"
	"image"
	"io"
	"strings"
	"testing"

	"browser/internal/protocol"
)

func testRowFrame(rows ...string) protocol.Frame {
	w := 1
	for _, r := range rows {
		if len(r) > w {
			w = len(r)
		}
	}
	f := protocol.Frame{W: w, H: len(rows), Cells: make([]protocol.Cell, w*len(rows))}
	for y, row := range rows {
		for x := 0; x < w; x++ {
			r := ' '
			if x < len(row) {
				r = rune(row[x])
			}
			f.Cells[y*w+x] = protocol.Cell{R: r}
		}
	}
	return f
}

func TestHintOverlayDoesNotMutateStoredFrame(t *testing.T) {
	p := NewPresenter(io.Discard, false)
	f := testRowFrame("..........", "..........", "..........")
	hint := &HintView{Items: []protocol.HintItem{
		{Label: "d", Chars: []protocol.HintChar{{R: 'D', X: 2, Y: 1}}},
		{Label: "de", Chars: []protocol.HintChar{{R: 'D', X: 4, Y: 1}, {R: 'E', X: 5, Y: 1}}},
		{Label: "x", Chars: []protocol.HintChar{{R: 'X', X: 8, Y: 1}}},
	}}
	p.SetHint(hint)
	if err := p.Present(f, 0, false); err != nil {
		t.Fatal(err)
	}
	if got := p.lastFrame.Cells[1*p.lastFrame.W+8].R; got != '.' {
		t.Fatalf("stored frame kept hint %q; filtered hints would remain on screen", got)
	}
	hint.Prefix = "d"
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	if got := p.lastFrame.Cells[1*p.lastFrame.W+8].R; got != '.' {
		t.Fatalf("refresh base kept hint %q", got)
	}
}

func TestPresenterClampsStateSize(t *testing.T) {
	p := NewPresenter(io.Discard, false)
	maxW, maxH := protocol.DefaultLimits.MaxFrameWidth, protocol.DefaultLimits.MaxFrameHeight
	p.SetState(protocol.State{FrontendChrome: true, Width: maxW + 5000, Height: 20, HeaderRows: 2})
	if p.state.Width != maxW {
		t.Fatalf("width = %d, want clamped %d", p.state.Width, maxW)
	}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	if p.lastFrame.W != maxW {
		t.Fatalf("frame width = %d, want %d", p.lastFrame.W, maxW)
	}
	p = NewPresenter(io.Discard, false)
	p.SetState(protocol.State{FrontendChrome: true, Width: 20, Height: maxH + 5000, HeaderRows: 2})
	if p.state.Height != maxH {
		t.Fatalf("height = %d, want clamped %d", p.state.Height, maxH)
	}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	if p.lastFrame.H != maxH-3 {
		t.Fatalf("frame height = %d, want %d", p.lastFrame.H, maxH-3)
	}
}

func TestPresenterDiffsChangedRows(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	f := testRowFrame("top", "url", "body", "foot")
	if err := p.Present(f, 0, false); err != nil {
		t.Fatal(err)
	}
	first := out.String()
	if !strings.Contains(first, "\x1b[2J") {
		t.Fatal("first frame did not clear the screen")
	}
	if !strings.Contains(first, "body") {
		t.Fatalf("first frame missing body: %q", first)
	}

	out.Reset()
	if err := p.Present(f, 0, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "body") {
		t.Fatalf("unchanged body row rewritten: %q", out.String())
	}

	out.Reset()
	f.Cells[2*f.W] = protocol.Cell{R: 'X'}
	if err := p.Present(f, 0, false); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "\x1b[2J") {
		t.Fatalf("row change cleared the screen: %q", got)
	}
	if !strings.Contains(got, "Xody") {
		t.Fatalf("row change missing new text: %q", got)
	}

}

func TestPresenterResetsOnScrollAndNative(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	f := testRowFrame("top", "url", "body", "foot")
	if err := p.Present(f, 0, false); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := p.Present(f, 1, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b[2J") {
		t.Fatalf("scroll did not force a clear: %q", out.String())
	}

	out.Reset()
	if err := p.Present(f, 1, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "body") {
		t.Fatalf("native mode dropped the text layer: %q", out.String())
	}
	out.Reset()
	if err := p.Present(f, 1, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b[2J") {
		t.Fatalf("leaving native mode did not force a clear: %q", out.String())
	}
}

func TestPresenterRepaintsWhenScaledRunsMove(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	f := testRowFrame("head", "sub1", "sub2")
	f.Cells[0].Run = 1
	f.Cells[f.W].Flags |= protocol.CellSkip
	f.Runs = []protocol.ScaledRun{{X: 0, Y: 0, Cells: f.W, Scale: 2, Text: "Hi"}}
	if err := p.Present(f, 0, false); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := p.Present(f, 0, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b[2J") {
		t.Fatalf("unchanged frame forced a clear: %q", out.String())
	}

	// The run moves down one row; the row it leaves is now skip-only, so a
	// line diff could not clear the old subtitle painted there.
	moved := testRowFrame("head", "sub1", "sub2")
	moved.Cells[moved.W].Run = 1
	moved.Cells[2*moved.W].Flags |= protocol.CellSkip
	moved.Runs = []protocol.ScaledRun{{X: 0, Y: 1, Cells: moved.W, Scale: 2, Text: "Hi"}}
	out.Reset()
	if err := p.Present(moved, 0, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b[2J") {
		t.Fatalf("moved scaled run did not force a clear: %q", out.String())
	}
}

func TestPresenterSkipsPlacementsUnderOverlay(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	p.SetGraphics(GraphicsKitty, 8, 16)
	f := protocol.Frame{W: 20, H: 5, Cells: make([]protocol.Cell, 100)}
	for i := range f.Cells {
		f.Cells[i].R = ' '
	}
	f.Placements = []protocol.Placement{
		testPlacement("pic.png", image.NewRGBA(image.Rect(0, 0, 16, 16)), 1, 1, 2, 1, 0, 1),
	}
	st := protocol.State{FrontendChrome: true, Width: 20, Height: 7, HeaderRows: 1}
	p.SetState(st)
	if err := p.Present(f, 0, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b_Ga=p") {
		t.Fatalf("native placement missing: %.200q", out.String())
	}

	out.Reset()
	st.Menu = protocol.MenuView
	p.SetState(st)
	if err := p.Present(f, 0, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b_Ga=p") {
		t.Fatalf("placement drawn under the overlay: %.200q", out.String())
	}

	out.Reset()
	st.Menu = protocol.MenuNone
	p.SetState(st)
	if err := p.Present(f, 0, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b_Ga=p") {
		t.Fatalf("placement did not return: %.200q", out.String())
	}
}

func TestPresenterPaintsDialogOverPage(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	st := chromeState()
	st.Width, st.Height = 60, 20
	p.SetState(st)
	p.SetTabs([]TabView{{Title: "Alpha", Active: true}})
	p.SetDialog(&DialogView{ID: 1, Kind: protocol.DialogAlert, Text: "hello dialog"})
	f := protocol.Frame{W: 60, H: 17, Cells: make([]protocol.Cell, 60*17)}
	if err := p.Present(f, 0, false); err != nil {
		t.Fatal(err)
	}
	text := testANSI.ReplaceAllString(out.String(), "")
	for _, want := range []string{"hello dialog", "Alpha"} {
		if !strings.Contains(text, want) {
			t.Errorf("dialog page output does not contain %q: %q", want, text)
		}
	}
}

func TestPresenterPaintsDialogOverConsole(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	p.SetState(chromeState())
	p.SetDialog(&DialogView{ID: 1, Kind: protocol.DialogAlert, Text: "hello dialog"})
	v := &protocol.ConsoleView{
		Width: 60, Height: 18, Pane: consolePaneLogs,
		Tabs:  []string{" JS logs ", " Requests ", " Storage ", " HTML ", " Tools "},
		Lines: []string{"console body"},
	}
	if err := p.PresentConsole(v, ConsoleUI{Pane: consolePaneLogs, Hint: consoleHint, Footer: consoleFooter}); err != nil {
		t.Fatal(err)
	}
	text := testANSI.ReplaceAllString(out.String(), "")
	for _, want := range []string{"hello dialog", "JS logs"} {
		if !strings.Contains(text, want) {
			t.Errorf("console output does not contain %q: %q", want, text)
		}
	}
}

func TestPresenterPaintsEveryWindowPainter(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	st := chromeState()
	st.Width, st.Height, st.HeaderRows = 20, 5, 1
	p.SetState(st)
	p.setWindow(windowPainter{level: layerConfig, draw: func(c *chromeCanvas, st protocol.State) bool {
		c.cells[1*c.w+1].R = 'A'
		return true
	}})
	p.setWindow(windowPainter{level: layerDialog, draw: func(c *chromeCanvas, st protocol.State) bool {
		c.cells[2*c.w+1].R = 'B'
		return true
	}})
	f := protocol.Frame{W: 20, H: 3, Cells: make([]protocol.Cell, 20*3)}
	if err := p.Present(f, 0, false); err != nil {
		t.Fatal(err)
	}
	text := testANSI.ReplaceAllString(out.String(), "")
	for _, want := range []string{"A", "B"} {
		if !strings.Contains(text, want) {
			t.Fatalf("window painter %q was not painted: %q", want, text)
		}
	}
}

func TestWindowPaintersStayLayerOrdered(t *testing.T) {
	p := NewPresenter(io.Discard, false)
	p.SetDialog(&DialogView{})
	p.SetDownload(&DownloadView{})
	p.SetNotice(&NoticeView{})
	p.SetSelect(&SelectView{})
	p.SetConfig(&ConfigView{})
	p.SetHistory(&historyView{})
	want := []windowLayer{
		layerMenu, layerHistory, layerConfig, layerSelect,
		layerNotice, layerDownload, layerDialog,
	}
	if len(p.windows) != len(want) {
		t.Fatalf("painters = %d, want %d", len(p.windows), len(want))
	}
	for i, w := range p.windows {
		if w.layer() != want[i] {
			t.Fatalf("painter %d = %d, want %d", i, w.layer(), want[i])
		}
	}
	p.SetConfig(nil)
	for _, w := range p.windows {
		if w.layer() == layerConfig {
			t.Fatal("closed config window stayed in the painter stack")
		}
	}
}

func BenchmarkPresenterFrame(b *testing.B) {
	const w, h = 100, 35
	f := protocol.Frame{Version: 1, W: w, H: h, Cells: make([]protocol.Cell, w*h)}
	for i := range f.Cells {
		f.Cells[i] = protocol.Cell{
			R:  rune('a' + i%26),
			FG: protocol.Color{R: 30, G: 40, B: 50, A: 255, Set: true},
			BG: protocol.Color{R: 250, G: 250, B: 250, A: 255, Set: true},
		}
	}
	p := NewPresenter(io.Discard, true)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.prev = nil
		if err := p.Present(f, 0, false); err != nil {
			b.Fatal(err)
		}
	}
}
