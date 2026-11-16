package browser

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestPickSrcset(t *testing.T) {
	cases := []struct {
		srcset string
		pref   int
		want   string
	}{
		{"a.jpg 240w, b.jpg 480w, c.jpg 800w", 320, "b.jpg"},
		{"a.jpg 240w, b.jpg 480w, c.jpg 800w", 1024, "c.jpg"},
		{"a.jpg 1x, b.jpg 2x", 320, "a.jpg"},
		{"a.jpg", 320, "a.jpg"},
		{"", 320, ""},
	}
	for _, c := range cases {
		if got := pickSrcset(c.srcset, c.pref); got != c.want {
			t.Errorf("pickSrcset(%q, %d) = %q, want %q", c.srcset, c.pref, got, c.want)
		}
	}
}

func TestImageSourcePictureAndDataAttrs(t *testing.T) {
	pic := &Node{Type: ElementNode, Tag: "picture"}
	source := &Node{Type: ElementNode, Tag: "source", Parent: pic,
		Attrs: []Attr{{Key: "srcset", Value: "mobile.jpg 480w"}, {Key: "media", Value: "(max-width: 500px)"}}}
	img := &Node{Type: ElementNode, Tag: "img", Parent: pic,
		Attrs: []Attr{{Key: "src", Value: "fallback.jpg"}}}
	pic.Children = []*Node{source, img}

	if got := imageSource(img, 400); got != "mobile.jpg" {
		t.Fatalf("mobile picture = %q", got)
	}
	if got := imageSource(img, 1600); got != "fallback.jpg" {
		t.Fatalf("desktop picture = %q", got)
	}

	lazy := &Node{Type: ElementNode, Tag: "img",
		Attrs: []Attr{{Key: "data-src", Value: "lazy.jpg"}}}
	if got := imageSource(lazy, 800); got != "lazy.jpg" {
		t.Fatalf("data-src = %q", got)
	}

	// srcset wins over src.
	both := &Node{Type: ElementNode, Tag: "img",
		Attrs: []Attr{{Key: "src", Value: "small.jpg"}, {Key: "srcset", Value: "big.jpg 800w"}}}
	if got := imageSource(both, 800); got != "big.jpg" {
		t.Fatalf("srcset preference = %q", got)
	}
}

func TestRefreshImagesAfterScript(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	page := newJSPage(`<html><body><img id="x" alt="lazy"></body></html>`,
		`document.getElementById("x").src = "`+dataURL+`";`)
	b := NewBrowser(60, 20)
	b.loader.noImages = false
	b.page = page
	b.url = page.URL
	b.startJS(page)
	if !b.refreshImages() {
		t.Fatal("post-script images were not discovered")
	}
	img := page.Root.FindFirst("img")
	if img == nil || img.Img == nil {
		t.Fatal("image set by script was not loaded")
	}
	if !strings.HasPrefix(img.ImgSrc, "data:image/png;base64,") {
		t.Fatalf("image source = %q", img.ImgSrc)
	}
	if b.refreshImages() {
		t.Fatal("second refresh reloaded an unchanged image")
	}
}
