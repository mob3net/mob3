package browser

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"reflect"
	"strconv"

	"browser/internal/protocol"
)

// encodePNG encodes an image for a protocol placement or an iTerm2 payload.
func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// imagePointer returns a stable identity for a decoded image value. It is only
// used as a cache key; pointer reuse is paired with the source discriminator.
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

// maxPlacementCacheBytes bounds encoded image data kept for later frames. The
// cache is a speed aid, not page storage: an image larger than the cap is
// re-encoded when it reappears instead of being retained.
const maxPlacementCacheBytes = 8 << 20

// maxPlacementCacheEntries bounds the number of distinct images.
const maxPlacementCacheEntries = 64

// placementKey identifies one decoded image. ptr is a best-effort identity;
// src and bounds keep a reused address from aliasing another page's image.
type placementKey struct {
	ptr  uintptr
	src  string
	w, h int
}

type placementEntry struct {
	id   int
	data []byte
}

// placementImage returns the stable id and encoded bytes for one image,
// encoding it once and caching the result across frames.
func (b *Browser) placementImage(p ImagePlacement) *placementEntry {
	if p.Img == nil || p.W <= 0 || p.H <= 0 || p.TotalRows <= 0 {
		return nil
	}
	bounds := p.Img.Bounds()
	key := placementKey{ptr: imagePointer(p.Img), src: p.Src, w: bounds.Dx(), h: bounds.Dy()}
	if b.placementCache == nil {
		b.placementCache = make(map[placementKey]*placementEntry)
	}
	if e := b.placementCache[key]; e != nil {
		return e
	}
	data, err := encodePNG(p.Img)
	if err != nil || len(data) > protocol.DefaultLimits.MaxImageBytes {
		// An image that cannot fit one placement is omitted rather than
		// failing the whole frame.
		return nil
	}
	if b.nextPlacementID == 0 {
		b.nextPlacementID = 1
	}
	e := &placementEntry{id: b.nextPlacementID, data: data}
	b.nextPlacementID++
	if len(data) > maxPlacementCacheBytes {
		return e
	}
	b.placementCache[key] = e
	b.placementBytes += len(data)
	b.evictPlacements()
	return e
}

func (b *Browser) evictPlacements() {
	for b.placementBytes > maxPlacementCacheBytes || len(b.placementCache) > maxPlacementCacheEntries {
		for key, e := range b.placementCache {
			delete(b.placementCache, key)
			b.placementBytes -= len(e.data)
			break
		}
	}
}

// protocolPlacements maps engine image fragments to wire placements. The image
// bytes are immutable cached blobs; a frame may share them with the front end.
func (b *Browser) protocolPlacements(dst []protocol.Placement, placements []ImagePlacement) []protocol.Placement {
	dst = dst[:0]
	for _, p := range placements {
		e := b.placementImage(p)
		if e == nil {
			continue
		}
		dst = append(dst, protocol.Placement{
			Key:       strconv.Itoa(e.id),
			X:         p.X,
			Y:         p.Y,
			W:         p.W,
			H:         p.H,
			RowStart:  p.RowStart,
			TotalRows: p.TotalRows,
			Format:    protocol.PlacementPNG,
			Data:      e.data,
		})
	}
	return dst
}
