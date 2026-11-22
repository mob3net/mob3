package frontend

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"

	"browser/internal/protocol"
)

type GraphicsMode int

const (
	// GraphicsAuto detects the best protocol from the environment.
	GraphicsAuto GraphicsMode = iota
	// GraphicsNone loads and draws no images, which is the fastest default.
	GraphicsNone
	// GraphicsBlocks always uses Unicode half-block art.
	GraphicsBlocks
	// GraphicsKitty uses the kitty graphics protocol.
	GraphicsKitty
	// GraphicsITerm uses the iTerm2 OSC 1337 inline-image protocol.
	GraphicsITerm
)

// ParseGraphicsMode turns a -graphics flag value into a mode.
func ParseGraphicsMode(s string) GraphicsMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return GraphicsAuto
	case "none", "off", "no", "disabled":
		return GraphicsNone
	case "blocks", "block", "terminal", "text":
		return GraphicsBlocks
	case "kitty":
		return GraphicsKitty
	case "iterm", "iterm2", "iterm-2":
		return GraphicsITerm
	}
	return GraphicsAuto
}

// DetectGraphics resolves an auto mode to a concrete protocol.
func DetectGraphics(mode GraphicsMode) GraphicsMode {
	if mode != GraphicsAuto {
		return mode
	}
	if inMultiplexer() {
		// Escapes would be consumed by the multiplexer unless explicit
		// passthrough is configured, so prefer the always-safe fallback.
		return GraphicsBlocks
	}
	if kittyTerminal() {
		return GraphicsKitty
	}
	if itermTerminal() {
		return GraphicsITerm
	}
	return GraphicsBlocks
}

// gfxKey identifies a scaled image. key is the engine's stable placement key;
// the box and pixel dimensions keep different crops of one image apart.
type gfxKey struct {
	key  string
	cols int
	rows int
	pxW  int
	pxH  int
}

type itermKey struct {
	gfxKey
	rowStart int
	cropRows int
}

type kittyImage struct {
	id   int
	w, h int
}

// GFX writes images with a terminal graphics protocol.
type GFX struct {
	mode         GraphicsMode
	cellW, cellH int
	nextID       int
	decoded      map[string]image.Image
	kitty        map[gfxKey]*kittyImage
	scaled       map[gfxKey]*image.NRGBA
	encoded      map[gfxKey][]byte
	iterm        map[itermKey]string
}

// NewGFXRenderer creates a renderer for the given mode. It returns nil when
// half-block art should be used.
func NewGFXRenderer(mode GraphicsMode, cellW, cellH int) *GFX {
	mode = DetectGraphics(mode)
	if mode != GraphicsKitty && mode != GraphicsITerm {
		return nil
	}
	if cellW <= 0 {
		cellW = 8
	}
	if cellH <= 0 {
		cellH = 16
	}
	return &GFX{
		mode:    mode,
		cellW:   cellW,
		cellH:   cellH,
		nextID:  1,
		decoded: make(map[string]image.Image),
		kitty:   make(map[gfxKey]*kittyImage),
		scaled:  make(map[gfxKey]*image.NRGBA),
		encoded: make(map[gfxKey][]byte),
		iterm:   make(map[itermKey]string),
	}
}

// Mode reports the active protocol (GraphicsBlocks when inactive).
func (r *GFX) Mode() GraphicsMode {
	if r == nil {
		return GraphicsBlocks
	}
	return r.mode
}

// TextSizing reports whether kitty's scaled-text protocol can be used. It is
// limited to real kitty because other kitty-graphics terminals do not
// implement OSC 66.
func (r *GFX) TextSizing() bool {
	if r == nil || r.mode != GraphicsKitty {
		return false
	}
	return kittyTerminal()
}

// BeginFrame emits protocol bookkeeping before the text frame is written.
func (r *GFX) BeginFrame(sb *strings.Builder) {
	if r == nil || r.mode != GraphicsKitty {
		return
	}
	// Delete every visible placement but keep the transmitted image data so
	// the next frame can re-place images without re-uploading them.
	sb.WriteString("\x1b_Ga=d,q=2\x1b\\")
}

// ClearScreen clears screen text for a full repaint while preserving what can
// be reused. Kitty keeps transmitted image data across ED 1/0 (only ED 2/3
// frees it), so using those instead of ED 2 lets the next frame re-place cached
// images by id instead of re-scaling, re-encoding and re-uploading them after
// every scroll-triggered clear.
func (r *GFX) ClearScreen(sb *strings.Builder) {
	if sb == nil {
		return
	}
	if r == nil {
		sb.WriteString("\x1b[2J")
		return
	}
	if r.mode == GraphicsKitty {
		sb.WriteString("\x1b[1J\x1b[0J")
		return
	}
	r.Reset(sb)
	sb.WriteString("\x1b[2J")
}

// Reset frees terminal image storage and forgets cached transmissions. Use it
// when image data really becomes invalid (for example an explicit 2J clear);
// scroll-triggered repaints use ClearScreen, which keeps kitty data.
func (r *GFX) Reset(sb *strings.Builder) {
	if r == nil {
		return
	}
	if r.mode == GraphicsKitty && sb != nil {
		sb.WriteString("\x1b_Ga=d,d=A,q=2\x1b\\")
	}
	r.kitty = make(map[gfxKey]*kittyImage)
	r.iterm = make(map[itermKey]string)
}

// Clear removes any images this renderer placed.
func (r *GFX) Clear(sb *strings.Builder) {
	if r == nil {
		return
	}
	switch r.mode {
	case GraphicsKitty:
		sb.WriteString("\x1b_Ga=d,d=A,q=2\x1b\\")
	case GraphicsITerm:
		// Inline images are ordinary screen content and are cleared by the
		// caller's screen reset / alternate-screen switch.
	}
}

// maxCachedImages bounds the client-side image caches. Eviction never drops an
// image used by the frame being drawn, so a viewport that shows more images
// than this keeps them all instead of resetting and rebuilding the cache on
// every frame.
const maxCachedImages = 64

// PlaceAll emits every image placement for the current frame.
func (r *GFX) PlaceAll(sb *strings.Builder, placements []protocol.Placement) {
	if r == nil || len(placements) == 0 {
		return
	}
	keep := make(map[gfxKey]struct{}, len(placements))
	keepSrc := make(map[string]struct{}, len(placements))
	for _, p := range placements {
		if p.Key == "" || len(p.Data) == 0 || p.W <= 0 || p.H <= 0 || p.TotalRows <= 0 || p.Format != protocol.PlacementPNG {
			continue
		}
		keepSrc[p.Key] = struct{}{}
		switch r.mode {
		case GraphicsKitty:
			r.placeKitty(sb, p, keep)
		case GraphicsITerm:
			r.placeITerm(sb, p, keep)
		}
	}
	r.prune(sb, keep, keepSrc)
}

// prune drops cached images that are not in the current frame once the cache
// exceeds its soft cap. Visible images are always retained.
func (r *GFX) prune(sb *strings.Builder, keep map[gfxKey]struct{}, keepSrc map[string]struct{}) {
	if len(r.scaled) <= maxCachedImages && len(r.kitty) <= maxCachedImages && len(r.encoded) <= maxCachedImages && len(r.decoded) <= maxCachedImages {
		return
	}
	for key, entry := range r.kitty {
		if _, ok := keep[key]; ok {
			continue
		}
		if r.mode == GraphicsKitty && sb != nil {
			fmt.Fprintf(sb, "\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", entry.id)
		}
		delete(r.kitty, key)
	}
	for key := range r.scaled {
		if _, ok := keep[key]; !ok {
			delete(r.scaled, key)
		}
	}
	for key := range r.encoded {
		if _, ok := keep[key]; !ok {
			delete(r.encoded, key)
		}
	}
	for key := range r.decoded {
		if _, ok := keepSrc[key]; !ok {
			delete(r.decoded, key)
		}
	}
}

func (r *GFX) placeKitty(sb *strings.Builder, p protocol.Placement, keep map[gfxKey]struct{}) {
	key, img, ok := r.scaledImage(p)
	if !ok {
		return
	}
	keep[key] = struct{}{}
	entry := r.kitty[key]
	if entry == nil {
		entry = &kittyImage{id: r.nextID, w: img.Bounds().Dx(), h: img.Bounds().Dy()}
		r.nextID++
		data := r.encoded[key]
		if data == nil {
			var err error
			data, err = encodePNG(img)
			if err != nil {
				return
			}
			r.encoded[key] = data
		}
		r.kittyTransmit(sb, entry.id, data)
		r.kitty[key] = entry
	}
	if entry.h <= 0 || p.TotalRows <= 0 {
		return
	}
	y0 := p.RowStart * entry.h / p.TotalRows
	y1 := (p.RowStart + p.H) * entry.h / p.TotalRows
	if y1 <= y0 {
		y1 = y0 + 1
	}
	if y1 > entry.h {
		y1 = entry.h
	}
	fmt.Fprintf(sb, "\x1b[%d;%dH", p.Y+1, p.X+1)
	fmt.Fprintf(sb, "\x1b_Ga=p,i=%d,q=2,c=%d,r=%d,x=0,y=%d,w=%d,h=%d,C=1\x1b\\",
		entry.id, p.W, p.H, y0, entry.w, y1-y0)
}

func (r *GFX) placeITerm(sb *strings.Builder, p protocol.Placement, keep map[gfxKey]struct{}) {
	key, img, ok := r.scaledImage(p)
	if !ok {
		return
	}
	keep[key] = struct{}{}
	ik := itermKey{gfxKey: key, rowStart: p.RowStart, cropRows: p.H}
	payload, ok := r.iterm[ik]
	if !ok {
		b := img.Bounds()
		y0 := p.RowStart * b.Dy() / p.TotalRows
		y1 := (p.RowStart + p.H) * b.Dy() / p.TotalRows
		if y1 <= y0 {
			y1 = y0 + 1
		}
		if y1 > b.Dy() {
			y1 = b.Dy()
		}
		crop := img.SubImage(image.Rect(0, y0, b.Dx(), y1)).(*image.NRGBA)
		data, err := encodePNG(crop)
		if err != nil {
			return
		}
		payload = base64.StdEncoding.EncodeToString(data)
		r.iterm[ik] = payload
	}
	fmt.Fprintf(sb, "\x1b7\x1b[%d;%dH\x1b]1337;File=inline=1;width=%d;height=%d;preserveAspectRatio=0;doNotMoveCursor=1:%s\x07\x1b8",
		p.Y+1, p.X+1, p.W, p.H, payload)
}

// scaledImage returns the cache key and a downscaled NRGBA image sized for the
// placement's full cell box.
func (r *GFX) scaledImage(p protocol.Placement) (gfxKey, *image.NRGBA, bool) {
	src := r.decodedImage(p)
	if src == nil {
		return gfxKey{}, nil, false
	}
	pxW, pxH := r.targetPixels(p.W, p.TotalRows)
	key := gfxKey{key: p.Key, cols: p.W, rows: p.TotalRows, pxW: pxW, pxH: pxH}
	if img, ok := r.scaled[key]; ok {
		return key, img, true
	}
	img := scaleImage(src, pxW, pxH)
	r.scaled[key] = img
	return key, img, true
}

// maxPlacementPixels bounds a single decoded placement image. The engine's
// image loader uses the same ceiling; applying it here keeps a hostile or
// buggy engine from turning a small PNG into a huge bitmap in the front end.
const maxPlacementPixels = 25_000_000

// decodedImage decodes and caches the PNG a placement carries. The engine
// treats the bytes as immutable, so the cached image can outlive one frame.
func (r *GFX) decodedImage(p protocol.Placement) image.Image {
	if img, ok := r.decoded[p.Key]; ok {
		return img
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(p.Data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 ||
		int64(cfg.Width)*int64(cfg.Height) > maxPlacementPixels {
		return nil
	}
	img, err := png.Decode(bytes.NewReader(p.Data))
	if err != nil {
		return nil
	}
	r.decoded[p.Key] = img
	return img
}

// targetPixels chooses a bounded pixel size for a cell box while preserving its
// aspect ratio, so cropping stays proportional.
func (r *GFX) targetPixels(cols, rows int) (int, int) {
	const maxDim = 2048
	w := cols * r.cellW
	h := rows * r.cellH
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	scale := 1.0
	if w > maxDim {
		scale = float64(maxDim) / float64(w)
	}
	if h > maxDim {
		if s := float64(maxDim) / float64(h); s < scale {
			scale = s
		}
	}
	if scale < 1 {
		w = maxInt(1, int(float64(w)*scale))
		h = maxInt(1, int(float64(h)*scale))
	}
	return w, h
}

func (r *GFX) kittyTransmit(sb *strings.Builder, id int, data []byte) {
	const chunk = 4096
	encoded := base64.StdEncoding.EncodeToString(data)
	first := true
	for len(encoded) > 0 {
		n := chunk
		if n > len(encoded) {
			n = len(encoded)
		}
		part := encoded[:n]
		encoded = encoded[n:]
		more := 0
		if len(encoded) > 0 {
			more = 1
		}
		if first {
			fmt.Fprintf(sb, "\x1b_Ga=t,f=100,q=2,i=%d,m=%d;%s\x1b\\", id, more, part)
			first = false
		} else {
			fmt.Fprintf(sb, "\x1b_Gm=%d;%s\x1b\\", more, part)
		}
	}
	if first {
		fmt.Fprintf(sb, "\x1b_Ga=t,f=100,q=2,i=%d,m=0;\x1b\\", id)
	}
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// scaleImage resizes src to w x h with area sampling. The result is always
// straight-alpha NRGBA so it can be encoded as PNG.
func scaleImage(src image.Image, w, h int) *image.NRGBA {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 {
		return dst
	}
	for dy := 0; dy < h; dy++ {
		y0 := b.Min.Y + dy*sh/h
		y1 := b.Min.Y + (dy+1)*sh/h
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for dx := 0; dx < w; dx++ {
			x0 := b.Min.X + dx*sw/w
			x1 := b.Min.X + (dx+1)*sw/w
			if x1 <= x0 {
				x1 = x0 + 1
			}
			sx := maxInt(1, (x1-x0)/4)
			sy := maxInt(1, (y1-y0)/4)
			var sr, sg, sb, sa uint64
			var n uint64
			for yy := y0; yy < y1; yy += sy {
				for xx := x0; xx < x1; xx += sx {
					r, g, bl, a := src.At(xx, yy).RGBA()
					sr += uint64(r)
					sg += uint64(g)
					sb += uint64(bl)
					sa += uint64(a)
					n++
				}
			}
			if n == 0 {
				continue
			}
			alpha := uint8(sa / n / 257)
			if alpha == 0 {
				continue
			}
			unpremul := func(v uint64) uint8 {
				x := v / n / 257
				y := x * 255 / uint64(alpha)
				if y > 255 {
					y = 255
				}
				return uint8(y)
			}
			dst.SetNRGBA(dx, dy, color.NRGBA{
				R: unpremul(sr),
				G: unpremul(sg),
				B: unpremul(sb),
				A: alpha,
			})
		}
	}
	return dst
}

// inMultiplexer reports whether a terminal multiplexer sits between MOB³ and
// the terminal. Escapes may be consumed unless explicit passthrough is set.
