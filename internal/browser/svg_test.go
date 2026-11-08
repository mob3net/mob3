package browser

import (
	"strings"
	"testing"
)

func TestDecodeSVG(t *testing.T) {
	img, err := decodeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">
		<rect width="10" height="10" fill="#ff0000"/>
	</svg>`))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 10 || img.Bounds().Dy() != 10 {
		t.Fatalf("bounds = %v", img.Bounds())
	}
	r, g, b, a := img.At(5, 5).RGBA()
	if r < 0x8000 || g > 0x2000 || b > 0x2000 || a < 0x8000 {
		t.Fatalf("center pixel = %04x %04x %04x %04x", r, g, b, a)
	}
}

func TestSerializeSVGRestoresCamelCase(t *testing.T) {
	root := ParseHTML(`<svg viewbox="0 0 10 10"><lineargradient id="g"></lineargradient></svg>`)
	svg := root.FindFirst("svg")
	out := serializeSVG(svg)
	if !strings.Contains(out, `viewBox="0 0 10 10"`) {
		t.Fatalf("viewBox not restored: %s", out)
	}
	if !strings.Contains(out, "linearGradient") {
		t.Fatalf("linearGradient tag not restored: %s", out)
	}
	if _, err := decodeSVG([]byte(out)); err != nil {
		t.Fatalf("serialized SVG did not decode: %v", err)
	}
}

func TestInlineSVGDecoded(t *testing.T) {
	root := ParseHTML(`<html><body><svg width="20" height="20" viewBox="0 0 10 10">
		<rect width="10" height="10" fill="#00ff00"/>
	</svg></body></html>`)
	NewLoader().loadInlineSVGs(root)
	svg := root.FindFirst("svg")
	if svg == nil || svg.Img == nil {
		t.Fatal("inline SVG was not rasterized")
	}
	ly := BuildLayout(root, nil, 40)
	found := false
	for _, f := range ly.Frags {
		if f.Kind == fragImage && f.Img == svg.Img {
			found = true
		}
	}
	if !found {
		t.Fatal("inline SVG image was not laid out")
	}
}

func TestIsSVG(t *testing.T) {
	if !isSVG("image/svg+xml", nil) || !isSVG("", []byte(`<svg></svg>`)) {
		t.Fatal("SVG detection failed")
	}
	if isSVG("image/png", []byte("not svg")) {
		t.Fatal("PNG misdetected as SVG")
	}
}
