package browser

import (
	"net/url"
	"testing"
	"time"
)

func colorLum(c Color) float64 {
	return (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255
}

func TestDarkPaletteTransform(t *testing.T) {
	fgCache := make(map[Color]Color)
	bgCache := make(map[Color]Color)
	bg := darkBackground(RGB(255, 255, 255), bgCache)
	if colorLum(bg) > 0.2 {
		t.Fatalf("white background became %+v", bg)
	}
	fg := darkForeground(RGB(0, 0, 0), fgCache)
	if colorLum(fg) < 0.4 {
		t.Fatalf("black foreground became %+v", fg)
	}
	link := darkForeground(RGB(0x06, 0x45, 0xad), fgCache)
	if link.B <= link.R || colorLum(link) < 0.4 {
		t.Fatalf("blue link became %+v", link)
	}
}

func TestDarkLayoutPalette(t *testing.T) {
	root := ParseHTML(`<html><body><p>Hello <a href="/link">link</a></p></body></html>`)
	light := BuildLayout(root, nil, 40)
	if colorLum(light.PageBG) < 0.8 {
		t.Fatalf("light page background = %+v", light.PageBG)
	}
	ly := BuildLayoutDarkDeadline(root, nil, 40, false, time.Time{})
	if colorLum(ly.PageBG) > 0.2 {
		t.Fatalf("dark page background = %+v", ly.PageBG)
	}
	found := false
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind != fragText || f.Style == nil || f.Text == "" {
			continue
		}
		found = true
		if colorLum(f.Style.Color) < 0.4 {
			t.Fatalf("dark text color = %+v", f.Style.Color)
		}
		break
	}
	if !found {
		t.Fatal("no text fragments in dark layout")
	}
}

func TestDarkReaderMode(t *testing.T) {
	doc := ParseHTML(`<html><body><article><p>` +
		`Dark reader article text that is long enough for extraction.` +
		`</p></article></body></html>`)
	u, _ := url.Parse("https://example.com/article")
	b := NewBrowser(60, 20)
	b.page = &Page{URL: u, Base: u, Root: doc, Title: "Article"}
	b.dark = true
	b.HandleKey("R")
	if !b.reader {
		t.Fatal("reader mode did not turn on")
	}
	if colorLum(b.layout.PageBG) > 0.2 {
		t.Fatalf("dark reader background = %+v", b.layout.PageBG)
	}
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Kind == fragText && f.Style != nil {
			if colorLum(f.Style.Color) < 0.4 {
				t.Fatalf("dark reader text = %+v", f.Style.Color)
			}
			return
		}
	}
	t.Fatal("dark reader layout has no text")
}

func TestDarkModeToggleAndFooterLabel(t *testing.T) {
	doc := ParseHTML(`<html><body><p>Dark mode text</p></body></html>`)
	u, _ := url.Parse("https://example.com/dark")
	b := NewBrowser(80, 20)
	b.page = &Page{URL: u, Base: u, Root: doc, Title: "Dark"}
	b.rebuild()
	if b.HandleKey("D") {
		t.Fatal("D asked to quit")
	}
	if !b.dark {
		t.Fatal("D did not enable dark mode")
	}
	if b.status != "Dark mode on" {
		t.Fatalf("status = %q", b.status)
	}
	if colorLum(b.layout.PageBG) > 0.2 {
		t.Fatalf("dark layout background = %+v", b.layout.PageBG)
	}
	drawQuiet(b)
	var hit *headerHit
	for i := range b.footerHits {
		if b.footerHits[i].kind == headerDark {
			hit = &b.footerHits[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("dark footer label has no click region: %+v", b.footerHits)
	}
	withQuietStdout(t, func() {
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: hit.x + 1, Y: hit.y + 1, Press: true})
	})
	if b.dark {
		t.Fatal("clicking the dark label did not disable dark mode")
	}
	if b.status != "Dark mode off" {
		t.Fatalf("status = %q", b.status)
	}
}
