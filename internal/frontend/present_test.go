package frontend

import (
	"bytes"
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
