package browser

import (
	"net/url"
	"testing"

	"browser/internal/frontend"
	"browser/internal/protocol"
)

// TestFrontendChromeMatchesEngine keeps the front-end chrome renderer in step
// with the engine's fallback until the engine composition is deleted.
func TestFrontendChromeMatchesEngine(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*Browser)
	}{
		{"ready", func(b *Browser) {}},
		{"loading", func(b *Browser) {
			b.loading = true
			b.progress = 0.5
			b.status = "Loading"
		}},
		{"prompt", func(b *Browser) {
			b.prompt = true
			b.promptText = "example.com"
			b.promptCursor = 4
		}},
		{"prompt-select-all", func(b *Browser) {
			b.prompt = true
			b.promptText = "example.com"
			b.promptSelectAll = true
		}},
		{"warning", func(b *Browser) {
			b.status = "Site blocked"
		}},
		{"error", func(b *Browser) {
			b.status = "Error: page engine — boom"
		}},
		{"success", func(b *Browser) {
			b.status = "Copied 12 chars"
		}},
		{"hover-link", func(b *Browser) {
			b.page = &Page{Title: "Example", Root: &Node{}, Base: b.url, URL: b.url}
			b.layout = &Layout{Links: []*LinkInfo{{Href: "/next"}}}
			b.hover = 0
		}},
		{"zoom", func(b *Browser) {
			b.zoom = 1.5
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBrowser(100, 30)
			b.emitFrames = true
			b.frontendChrome = true
			b.url, _ = url.Parse("https://example.com/")
			b.page = &Page{Title: "Example", Root: &Node{}}
			b.usage.rss = 42 << 20
			tc.setup(b)

			st := b.stateMessage()
			layout := frontend.LayoutChrome(st)
			g := NewGrid(b.width, b.height, RGB(0, 0, 0), RGB(255, 255, 255))
			b.drawChrome(g)

			rows := b.headerRows()
			if len(layout.Header) != rows*b.width {
				t.Fatalf("header cells = %d, want %d", len(layout.Header), rows*b.width)
			}
			for i := range layout.Header {
				got, want := layout.Header[i], protoCell(g.Cells[i])
				if !sameChromeCell(got, want) {
					t.Fatalf("header cell %d = %+v, want %+v", i, got, want)
				}
			}
			if len(layout.Footer) != b.width {
				t.Fatalf("footer cells = %d, want %d", len(layout.Footer), b.width)
			}
			fy := b.height - 1
			for x := 0; x < b.width; x++ {
				got, want := layout.Footer[x], protoCell(g.Cells[fy*b.width+x])
				if !sameChromeCell(got, want) {
					t.Fatalf("footer cell %d = %+v, want %+v", x, got, want)
				}
			}
		})
	}
}

// sameChromeCell compares the visible cell fields. A blank cell's foreground
// is irrelevant; the engine grid keeps the page foreground there while the
// front-end canvas leaves it unset.
func sameChromeCell(a, b protocol.Cell) bool {
	if a.R == ' ' && b.R == ' ' {
		a.FG, b.FG = protocol.Color{}, protocol.Color{}
	}
	return a == b
}

func TestFrontendChromeMatchesEngineSizes(t *testing.T) {
	for _, w := range []int{20, 30, 60, 80, 100, 140, 180} {
		for _, h := range []int{3, 4, 5, 24} {
			b := NewBrowser(w, h)
			b.emitFrames = true
			b.frontendChrome = true
			b.url, _ = url.Parse("https://example.com/a/very/long/path/that/should/truncate")
			b.page = &Page{Title: "A rather long page title for the toolbar", Root: &Node{}}
			b.usage.rss = 84 << 20
			st := b.stateMessage()
			if !st.FrontendChrome {
				t.Fatalf("%dx%d: state did not mark frontend chrome", w, h)
			}
			layout := frontend.LayoutChrome(st)
			g := NewGrid(w, h, RGB(0, 0, 0), RGB(255, 255, 255))
			b.drawChrome(g)
			rows := b.headerRows()
			if len(layout.Header) != rows*w || len(layout.Footer) != w {
				t.Fatalf("%dx%d: header %d footer %d", w, h, len(layout.Header), len(layout.Footer))
			}
			for i := range layout.Header {
				if got, want := layout.Header[i], protoCell(g.Cells[i]); !sameChromeCell(got, want) {
					t.Fatalf("%dx%d header cell %d = %+v, want %+v", w, h, i, got, want)
				}
			}
			fy := h - 1
			for x := 0; x < w; x++ {
				if got, want := layout.Footer[x], protoCell(g.Cells[fy*w+x]); !sameChromeCell(got, want) {
					t.Fatalf("%dx%d footer cell %d = %+v, want %+v", w, h, x, got, want)
				}
			}
		}
	}
}

func TestFrontendScrollBarMatchesEngine(t *testing.T) {
	b := NewBrowser(100, 35)
	b.layout = &Layout{Height: 200}
	b.scroll = 50
	contentH := b.viewportHeight()
	g := NewGrid(b.width, contentH, RGB(0, 0, 0), RGB(255, 255, 255))
	b.drawScrollBar(g, 0, contentH)
	f := protocol.Frame{W: b.width, H: contentH, Cells: make([]protocol.Cell, b.width*contentH)}
	for i := range f.Cells {
		f.Cells[i].R = ' '
	}
	frontend.DrawScrollBarFrame(&f, protocol.State{
		FrontendChrome: true, MaxScroll: b.maxScroll(), Scroll: b.scroll,
	})
	for y := 0; y < contentH; y++ {
		got := f.Cells[y*b.width+b.width-1]
		want := protoCell(g.Cells[y*b.width+b.width-1])
		if !sameChromeCell(got, want) {
			t.Fatalf("scrollbar row %d = %+v, want %+v", y, got, want)
		}
	}
}

func TestStateKeysEngineFlag(t *testing.T) {
	b := NewBrowser(80, 24)
	b.emitFrames = true
	b.frontendChrome = true
	if st := b.stateMessage(); st.KeysEngine {
		t.Fatal("base page marked engine keys")
	}
	b.prompt = true
	if st := b.stateMessage(); !st.KeysEngine {
		t.Fatal("prompt did not mark engine keys")
	}
	b.prompt = false
	b.findActive = true
	if st := b.stateMessage(); !st.KeysEngine {
		t.Fatal("find did not mark engine keys")
	}
	b.findActive = false
	b.inputFocus = &Node{}
	if st := b.stateMessage(); !st.KeysEngine {
		t.Fatal("input focus did not mark engine keys")
	}
}
