package browser

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"reflect"
	"strings"
)

// GraphicsMode selects how images are drawn in interactive mode.
type GraphicsMode int

const (
	// GraphicsAuto detects the best protocol from the environment.
	GraphicsAuto GraphicsMode = iota
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
	case "blocks", "block", "terminal", "text", "none", "off":
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

func inMultiplexer() bool {
	term := strings.ToLower(os.Getenv("TERM"))
	return os.Getenv("TMUX") != "" || os.Getenv("ZELLIJ") != "" ||
		strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux")
}

func kittyTerminal() bool {
	term := strings.ToLower(os.Getenv("TERM"))
	program := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	if strings.Contains(term, "kitty") || os.Getenv("KITTY_WINDOW_ID") != "" {
		return true
	}
	if strings.Contains(program, "wezterm") || os.Getenv("WEZTERM_PANE") != "" {
		return true
	}
	if strings.Contains(program, "ghostty") || os.Getenv("GHOSTTY_RESOURCES_DIR") != "" {
		return true
	}
	return false
}

func itermTerminal() bool {
	program := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	return strings.Contains(program, "iterm") || strings.EqualFold(os.Getenv("LC_TERMINAL"), "iTerm2")
}

// gfxKey identifies a scaled image. ptr is a best-effort object identity while
// src keeps images from different pages apart if the address is reused.
type gfxKey struct {
	ptr  uintptr
	src  string
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

// gfxRenderer writes images with a terminal graphics protocol.
type gfxRenderer struct {
	mode         GraphicsMode
	cellW, cellH int
	nextID       int
	kitty        map[gfxKey]*kittyImage
	scaled       map[gfxKey]*image.NRGBA
	iterm        map[itermKey]string
}

// NewGFXRenderer creates a renderer for the given mode. It returns nil when
// half-block art should be used.
func NewGFXRenderer(mode GraphicsMode, cellW, cellH int) *gfxRenderer {
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
	return &gfxRenderer{
		mode:   mode,
		cellW:  cellW,
		cellH:  cellH,
		nextID: 1,
		kitty:  make(map[gfxKey]*kittyImage),
		scaled: make(map[gfxKey]*image.NRGBA),
		iterm:  make(map[itermKey]string),
	}
}

// Mode reports the active protocol (GraphicsBlocks when inactive).
func (r *gfxRenderer) Mode() GraphicsMode {
	if r == nil {
		return GraphicsBlocks
	}
	return r.mode
}

// TextSizing reports whether kitty's scaled-text protocol can be used. It is
// limited to real kitty because other kitty-graphics terminals do not
// implement OSC 66.
func (r *gfxRenderer) TextSizing() bool {
	if r == nil || r.mode != GraphicsKitty {
		return false
	}
	term := strings.ToLower(os.Getenv("TERM"))
	return strings.Contains(term, "kitty") || os.Getenv("KITTY_WINDOW_ID") != ""
}

// BeginFrame emits protocol bookkeeping before the text frame is written.
func (r *gfxRenderer) BeginFrame(sb *strings.Builder) {
	if r == nil || r.mode != GraphicsKitty {
		return
	}
	// Delete every visible placement but keep the transmitted image data so
	// the next frame can re-place images without re-uploading them.
	sb.WriteString("\x1b_Ga=d,q=2\x1b\\")
}

// Reset frees terminal image storage and forgets cached transmissions. It is
// required after a full-screen clear, because clearing the screen also clears
// kitty images; re-placing cached ids would silently fail.
func (r *gfxRenderer) Reset(sb *strings.Builder) {
	if r == nil {
		return
	}
	if r.mode == GraphicsKitty && sb != nil {
		sb.WriteString("\x1b_Ga=d,d=A,q=2\x1b\\")
	}
	r.kitty = make(map[gfxKey]*kittyImage)
	r.scaled = make(map[gfxKey]*image.NRGBA)
	r.iterm = make(map[itermKey]string)
}

// Clear removes any images this renderer placed.
func (r *gfxRenderer) Clear(sb *strings.Builder) {
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

// PlaceAll emits every image placement for the current frame.
func (r *gfxRenderer) PlaceAll(sb *strings.Builder, placements []ImagePlacement) {
	if r == nil || len(placements) == 0 {
		return
	}
	if len(r.kitty) > 64 || len(r.scaled) > 64 {
		// Reclaim terminal image storage and start fresh.
		if r.mode == GraphicsKitty {
			sb.WriteString("\x1b_Ga=d,d=A,q=2\x1b\\")
		}
		r.kitty = make(map[gfxKey]*kittyImage)
		r.scaled = make(map[gfxKey]*image.NRGBA)
		r.iterm = make(map[itermKey]string)
	}
	for _, p := range placements {
		if p.Img == nil || p.W <= 0 || p.H <= 0 || p.TotalRows <= 0 {
			continue
		}
		switch r.mode {
		case GraphicsKitty:
			r.placeKitty(sb, p)
		case GraphicsITerm:
			r.placeITerm(sb, p)
		}
	}
}

func (r *gfxRenderer) placeKitty(sb *strings.Builder, p ImagePlacement) {
	key, img := r.scaledImage(p)
	entry := r.kitty[key]
	if entry == nil {
		entry = &kittyImage{id: r.nextID, w: img.Bounds().Dx(), h: img.Bounds().Dy()}
		r.nextID++
		data, err := encodePNG(img)
		if err != nil {
			return
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

func (r *gfxRenderer) placeITerm(sb *strings.Builder, p ImagePlacement) {
	key, img := r.scaledImage(p)
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
func (r *gfxRenderer) scaledImage(p ImagePlacement) (gfxKey, *image.NRGBA) {
	pxW, pxH := r.targetPixels(p.W, p.TotalRows)
	key := gfxKey{
		ptr:  imagePointer(p.Img),
		src:  p.Src,
		cols: p.W,
		rows: p.TotalRows,
		pxW:  pxW,
		pxH:  pxH,
	}
	if img, ok := r.scaled[key]; ok {
		return key, img
	}
	img := scaleImage(p.Img, pxW, pxH)
	r.scaled[key] = img
	return key, img
}

// targetPixels chooses a bounded pixel size for a cell box while preserving its
// aspect ratio, so cropping stays proportional.
func (r *gfxRenderer) targetPixels(cols, rows int) (int, int) {
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

func (r *gfxRenderer) kittyTransmit(sb *strings.Builder, id int, data []byte) {
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

func imagePointer(img image.Image) uintptr {
	if img == nil {
		return 0
	}
	v := reflect.ValueOf(img)
	switch v.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return v.Pointer()
	}
	return 0
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
