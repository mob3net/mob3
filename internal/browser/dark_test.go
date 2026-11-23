package browser

import (
	"net/url"
	"strings"
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

func TestDarkModeUsesAuthorDarkCSS(t *testing.T) {
	rules := ParseStylesheet(`@media (prefers-color-scheme: dark) {
		body { background: #101010; color: #eeeeee; }
	}`)
	root := ParseHTML(`<html><body><p>site dark</p></body></html>`)
	light := BuildLayout(root, rules, 40)
	if colorLum(light.PageBG) < 0.8 {
		t.Fatalf("light page background = %+v", light.PageBG)
	}
	dark := BuildLayoutDarkDeadline(root, rules, 40, false, time.Time{})
	if dark.PageBG != RGB(0x10, 0x10, 0x10) {
		t.Fatalf("author dark background was remapped: %+v", dark.PageBG)
	}
	for i := range dark.Frags {
		f := &dark.Frags[i]
		if f.Kind == fragText && f.Style != nil && f.Text != "" {
			if f.Style.Color != RGB(0xee, 0xee, 0xee) {
				t.Fatalf("author dark text was remapped: %+v", f.Style.Color)
			}
			return
		}
	}
	t.Fatal("no text fragment")
}

func TestDarkReaderMode(t *testing.T) {
	doc := ParseHTML(`<html><body><article><p>` +
		`Dark reader article text that is long enough for extraction.` +
		`</p></article></body></html>`)
	u, _ := url.Parse("https://example.com/article")
	b := NewBrowser(60, 20)
	b.Page = &Page{URL: u, Base: u, Root: doc, Title: "Article"}
	b.Dark = true
	b.toggleReader()
	if !b.Reader {
		t.Fatal("reader mode did not turn on")
	}
	if colorLum(b.Layout.PageBG) > 0.2 {
		t.Fatalf("dark reader background = %+v", b.Layout.PageBG)
	}
	for i := range b.Layout.Frags {
		f := &b.Layout.Frags[i]
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
	// Wide enough that the right-side dark label has not collapsed.
	b := NewBrowser(120, 20)
	b.Page = &Page{URL: u, Base: u, Root: doc, Title: "Dark"}
	b.rebuild()
	b.toggleDark()
	if !b.Dark {
		t.Fatal("dark mode did not enable")
	}
	if b.status != "Dark mode on" {
		t.Fatalf("status = %q", b.status)
	}
	if colorLum(b.Layout.PageBG) > 0.2 {
		t.Fatalf("dark layout background = %+v", b.Layout.PageBG)
	}
	if got := b.darkStatusText(); got != "Dark:on" {
		t.Fatalf("dark status text = %q", got)
	}
	if !strings.Contains(b.footerRight(120), "Dark:on") {
		t.Fatalf("footer right = %q", b.footerRight(120))
	}
	b.toggleDark()
	if b.Dark {
		t.Fatal("dark mode did not disable")
	}
	if b.status != "Dark mode off" {
		t.Fatalf("status = %q", b.status)
	}
}
