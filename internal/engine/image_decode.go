package engine

import (
	"bytes"
	"context"
	"fmt"
	"image"
)

// MaxImagePixels bounds decoded raster dimensions before pixel allocation.
const MaxImagePixels = 25_000_000 // 25 MP

// ImageConfigOK rejects decompression bombs before decoding pixel data.
func ImageConfigOK(cfg image.Config) bool {
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return false
	}
	return int64(cfg.Width)*int64(cfg.Height) <= MaxImagePixels
}

// RecoverImage runs a decoder and converts a panic into an error. Raster and
// SVG decoders parse untrusted bytes, and a panic in one of them would take
// down the whole process; the browser shows a broken image instead.
func RecoverImage(fn func() (image.Image, error)) (img image.Image, err error) {
	defer func() {
		if r := recover(); r != nil {
			img, err = nil, fmt.Errorf("image decode failed: %v", r)
		}
	}()
	return fn()
}

// SafeDecodeConfig decodes image dimensions with panic containment.
func SafeDecodeConfig(data []byte) (cfg image.Config, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("image config decode failed: %v", r)
		}
	}()
	cfg, _, err = image.DecodeConfig(bytes.NewReader(data))
	return cfg, err
}

// SafeDecodeImage decodes a raster image with panic containment.
func SafeDecodeImage(data []byte) (image.Image, error) {
	return RecoverImage(func() (image.Image, error) {
		img, _, err := image.Decode(bytes.NewReader(data))
		return img, err
	})
}

// DecodeMedia decodes one image response: SVG through the vector rasterizer,
// everything else through the raster decoders with the megapixel limit
// applied. Both paths contain panics.
func DecodeMedia(ctx context.Context, ctype string, data []byte) (image.Image, error) {
	if IsSVG(ctype, data) {
		return DecodeSVG(data)
	}
	cfg, err := SafeDecodeConfig(data)
	if err != nil || !ImageConfigOK(cfg) {
		return nil, fmt.Errorf("unsupported image")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return SafeDecodeImage(data)
}

// ImageDecodeWeight estimates the RGBA bytes a decode will hold so the budget
// can admit several small images but only one or two large ones.
func ImageDecodeWeight(ctype string, data []byte) int64 {
	if IsSVG(ctype, data) {
		return int64(MaxSVGDim) * int64(MaxSVGDim) * 4
	}
	cfg, err := SafeDecodeConfig(data)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return 1 << 20
	}
	n := int64(cfg.Width) * int64(cfg.Height) * 4
	max := int64(MaxImagePixels) * 4
	if n > max {
		n = max
	}
	if n < 1 {
		n = 1
	}
	return n
}

// FitImage downscales a decoded image so neither side exceeds the cap,
// preserving aspect ratio. Images already inside the cap are returned
// unchanged, so only oversized photos pay for a resample.
func FitImage(img image.Image, maxW, maxH int) image.Image {
	if img == nil || (maxW <= 0 && maxH <= 0) {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 {
		return img
	}
	nw, nh := w, h
	if maxW > 0 && nw > maxW {
		f := float64(maxW) / float64(nw)
		nw = maxW
		nh = int(float64(nh)*f + 0.5)
	}
	if maxH > 0 && nh > maxH {
		f := float64(maxH) / float64(nh)
		nh = maxH
		nw = int(float64(nw)*f + 0.5)
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	if nw == w && nh == h {
		return img
	}
	return ScaleImage(img, nw, nh)
}
