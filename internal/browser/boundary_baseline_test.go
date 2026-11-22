package browser

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
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

func boundaryBaselineURL() string {
	html := boundaryBaselineHTML()
	return "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(html))
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
		status := dumpPage(NewLoader(), boundaryBaselineURL(), 100, h, true, false, true, false, false, png, 0, 0, DefaultLayoutBudget, 1)
		if status != 0 {
			t.Fatalf("dumpPage status %d", status)
		}
	})
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
	br := NewBrowser(100, 35)
	br.page = &Page{Root: root, Title: "bench"}
	br.layout = ly
	br.color = true
	br.bigText = false
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
		blitGrid(screen, content, 2)
	}
}
