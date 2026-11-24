package engine

import (
	"browser/internal/frontend"
	"browser/internal/protocol"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// boundaryBaselineHTML is the synthetic page used to record the pre-migration
// engine baseline. It stays free of external resources and site-specific
// markup: text, links, a table, a grid and enough content to make layout and
// paint measurable.
func boundaryBaselineHTML() string {
	var rows, paras strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&rows, "<tr><td>row %d</td><td><a href='/p/%d'>link %d</a></td><td>%d</td></tr>", i, i, i, i*i)
	}
	for i := 0; i < 150; i++ {
		fmt.Fprintf(&paras, "<p>Paragraph %d with <b>bold</b>, <i>italic</i> and a <a href='/x/%d'>link</a>. %s</p>",
			i, i, strings.Repeat("Some longer text to fill the line. ", 3))
	}
	return `<!doctype html><html><head><style>
body{font-family:monospace;margin:8px;line-height:1.4} h1{font-size:20px}
table{border-collapse:collapse} td{border:1px solid #999;padding:2px 6px}
.grid{display:grid;grid-template-columns:1fr 2fr;gap:8px} .box{border:1px solid #ccc;padding:4px}
</style></head><body><h1>Boundary baseline</h1>
<div class="grid"><div class="box">left</div><div class="box">right</div></div>
<table>` + rows.String() + `</table>` + paras.String() + `</body></html>`
}

func captureStdout(t *testing.T, fn func()) []byte {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()
	func() {
		defer func() {
			os.Stdout = old
			_ = w.Close()
		}()
		fn()
	}()
	<-done
	_ = r.Close()
	return buf.Bytes()
}

func dumpBaseline(t *testing.T, png bool) []byte {
	t.Helper()
	h := 0
	if png {
		h = 35
	}
	return captureStdout(t, func() {
		if status := dumpPageForTest(t, png, h); status != 0 {
			t.Fatalf("dump handler status %d", status)
		}
	})
}

// dumpPageForTest mirrors the app's one-shot transport loop without importing
// app from a browser test.
func dumpPageForTest(t *testing.T, png bool, viewportH int) int {
	t.Helper()
	loader, target := htmlFixtureLoader(t, boundaryBaselineHTML())
	h := NewDumpHandler(DumpConfig{
		Loader: loader, Target: target, Width: 100,
		ViewportH: viewportH, NoJS: true, PNG: png,
		Budget: DefaultLayoutBudget, Zoom: 1,
	})
	session, err := (&protocol.InProc{Limits: DumpLimits(100, png), EventQueue: 4}).Open(context.Background(), 1, h)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	frames := 0
	for ev := range session.Events() {
		v, ok := ev.Body.(protocol.Frame)
		if !ok {
			continue
		}
		frames++
		if png {
			data, err := frontend.RenderPNG(v, 1)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = os.Stdout.Write(data)
			continue
		}
		for _, line := range frontend.EncodeFrame(v, false, false) {
			fmt.Println(line)
		}
	}
	if frames == 0 {
		return 1
	}
	return 0
}

// TestBoundaryDumpBaseline freezes the text output of the synthetic page. It
// must stay byte-identical across the boundary migration; refresh it only when
// a rendering change is intentional.
func TestBoundaryDumpBaseline(t *testing.T) {
	const want = "320723811c02b681c11db45eaa2860b72ef653697165b5f92bc8d55ad793a830"
	got := dumpBaseline(t, false)
	sum := sha256.Sum256(got)
	if hex.EncodeToString(sum[:]) != want {
		t.Fatalf("dump baseline changed\n got %x\nwant %s", sum, want)
	}
}

// TestBoundaryPNGBaseline freezes the PNG output of the synthetic page.
func TestBoundaryPNGBaseline(t *testing.T) {
	const want = "5a5adb26413007dc5a35abd99cd7144823fb46f2f0f946609901ec54f82256aa"
	got := dumpBaseline(t, true)
	sum := sha256.Sum256(got)
	if hex.EncodeToString(sum[:]) != want {
		t.Fatalf("PNG baseline changed\n got %x\nwant %s", sum, want)
	}
}

func BenchmarkBoundaryParse(b *testing.B) {
	src := boundaryBaselineHTML()
	b.ReportAllocs()
	b.SetBytes(int64(len(src)))
	for i := 0; i < b.N; i++ {
		_ = ParseHTML(src)
	}
}

func BenchmarkBoundaryLayoutPaint(b *testing.B) {
	root := ParseHTML(boundaryBaselineHTML())
	// Warm the computed styles so the benchmark measures the rebuild, not the
	// first style pass.
	_ = BuildLayout(root, nil, 100)
	fg, bg := RGB(0x1f, 0x23, 0x28), RGB(255, 255, 255)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ly := BuildLayout(root, nil, 100)
		g := NewGrid(100, 35, fg, bg)
		PaintLayout(g, ly, 0, 0, 35, nil, fg)
		_ = gridLines(g, true)
	}
}

// BenchmarkBoundaryDraw measures a full interactive repaint with the chrome
// and ANSI output, forcing a complete frame each iteration.
func BenchmarkBoundaryDraw(b *testing.B) {
	root := ParseHTML(boundaryBaselineHTML())
	ly := BuildLayoutViewportDeadline(root, nil, 100, 33, false, time.Time{})
	br := newTestSession(100, 35)
	br.Page = &Page{Root: root, Title: "bench"}
	br.Layout = ly
	br.color = true
	br.BigText = false
	old := os.Stdout
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		b.Fatal(err)
	}
	os.Stdout = devnull
	b.Cleanup(func() {
		os.Stdout = old
		_ = devnull.Close()
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		br.prev = nil
		br.draw()
	}
}

// BenchmarkBoundaryContentFrame measures the new hot path: reuse a content
// grid, paint the viewport, reuse the screen grid and blit the content rows.
func BenchmarkBoundaryContentFrame(b *testing.B) {
	root := ParseHTML(boundaryBaselineHTML())
	ly := BuildLayoutViewportDeadline(root, nil, 100, 33, false, time.Time{})
	fg, bg := RGB(0x1f, 0x23, 0x28), RGB(255, 255, 255)
	content := NewGrid(100, 33, fg, bg)
	screen := NewGrid(100, 35, fg, bg)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		content.Reset(fg, bg)
		PaintLayout(content, ly, 0, 0, 33, nil, fg)
		screen.Reset(fg, bg)
		BlitGrid(screen, content, 2)
	}
}

// codecSink applies the same encode/decode validation as the in-process
// transport without a goroutine, so the benchmark measures the frame boundary
// itself.
type codecSink struct {
	buf  []byte
	last protocol.Frame
}

func (s *codecSink) Send(ev protocol.Event) error {
	if f, ok := ev.Body.(protocol.Frame); ok {
		if err := f.Validate(protocol.DefaultLimits); err != nil {
			return err
		}
		// Detach the frame the way the transport does before Send returns.
		f.Cells = append([]protocol.Cell(nil), f.Cells...)
		f.Runs = append([]protocol.ScaledRun(nil), f.Runs...)
		f.Placements = append([]protocol.Placement(nil), f.Placements...)
		s.last = f
		return nil
	}
	var err error
	s.buf, err = protocol.AppendEvent(s.buf[:0], ev, protocol.DefaultLimits)
	if err != nil {
		return err
	}
	_, _, err = protocol.DecodeEvent(s.buf, protocol.DefaultLimits)
	return err
}

// BenchmarkBoundarySessionFrame measures a forced repaint plus the frame that
// crosses the in-process seam. Presentation is excluded.
func BenchmarkBoundarySessionFrame(b *testing.B) {
	root := ParseHTML(boundaryBaselineHTML())
	ly := BuildLayoutViewportDeadline(root, nil, 100, 33, false, time.Time{})
	br := newTestSession(100, 35)
	br.Page = &Page{Root: root, Title: "bench"}
	br.Layout = ly
	br.color = true
	br.BigText = false
	br.emitFrames = true
	br.sessionOut = &codecSink{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		br.prev = nil
		br.draw()
		br.emitPendingFrame()
	}
}

// BenchmarkBoundaryTextRepaint measures the complete text path: engine paint,
// validated frame handoff and front-end ANSI presentation to a discard writer.
func BenchmarkBoundaryTextRepaint(b *testing.B) {
	root := ParseHTML(boundaryBaselineHTML())
	ly := BuildLayoutViewportDeadline(root, nil, 100, 33, false, time.Time{})
	br := newTestSession(100, 35)
	br.Page = &Page{Root: root, Title: "bench"}
	br.Layout = ly
	br.color = true
	br.BigText = false
	br.emitFrames = true
	sink := &codecSink{}
	br.sessionOut = sink
	p := frontend.NewPresenter(io.Discard, true)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		br.prev = nil
		br.draw()
		br.emitPendingFrame()
		p.Reset()
		if err := p.Present(sink.last, 0, false); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBoundarySessionFrameImage measures the same repaint with one native
// image placement, after the first frame has populated the encode cache.
// BenchmarkBoundaryStateChromeRepaint measures the same complete text path
// with the front end drawing the header and footer from protocol.State.
func BenchmarkBoundaryStateChromeRepaint(b *testing.B) {
	root := ParseHTML(boundaryBaselineHTML())
	ly := BuildLayoutViewportDeadline(root, nil, 100, 33, false, time.Time{})
	br := newTestSession(100, 35)
	br.Page = &Page{Root: root, Title: "bench"}
	br.Layout = ly
	br.color = true
	br.BigText = false
	br.emitFrames = true
	br.frontendChrome = true
	sink := &codecSink{}
	br.sessionOut = sink
	p := frontend.NewPresenter(io.Discard, true)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		br.prev = nil
		br.draw()
		br.emitPendingFrame()
		p.SetState(br.stateMessage())
		p.Reset()
		if err := p.Present(sink.last, 0, false); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBoundarySessionFrameImage(b *testing.B) {
	img := image.NewNRGBA(image.Rect(0, 0, 320, 320))
	br := newTestSession(100, 35)
	br.Page = &Page{Root: ParseHTML(`<html><body></body></html>`), Title: "img"}
	br.Layout = &Layout{Height: 30, Frags: []Frag{{Kind: FragImage, X: 2, Y: 1, W: 40, H: 20, Img: img}}}
	br.color = true
	br.caps |= protocol.CapKittyGraphics
	br.emitFrames = true
	br.sessionOut = &codecSink{}
	br.draw()
	br.emitPendingFrame()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		br.draw()
		br.emitPendingFrame()
	}
}

// BenchmarkBoundaryComposedRepaint measures the split presentation path: a
// content-only frame plus a chrome layer, composed by the front end.
func BenchmarkBoundaryComposedRepaint(b *testing.B) {
	root := ParseHTML(boundaryBaselineHTML())
	ly := BuildLayoutViewportDeadline(root, nil, 100, 33, false, time.Time{})
	br := newTestSession(100, 35)
	br.Page = &Page{Root: root, Title: "bench"}
	br.Layout = ly
	br.color = true
	br.BigText = false
	br.emitFrames = true
	br.frontendChrome = true
	sink := &codecSink{}
	br.sessionOut = sink
	p := frontend.NewPresenter(io.Discard, true)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		br.prev = nil
		br.draw()
		br.emitPendingFrame()
		p.Reset()
		if err := p.Present(sink.last, 0, false); err != nil {
			b.Fatal(err)
		}
	}
}
