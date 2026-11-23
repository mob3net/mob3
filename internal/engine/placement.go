package engine

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"reflect"
	"strconv"

	"browser/internal/protocol"
)

// placementKey and placementEntry are the bounded encode cache used by native
// image placement.
type placementKey struct {
	ptr  uintptr
	src  string
	w, h int
}

type placementEntry struct {
	id   int
	data []byte
}

// EncodePNG encodes an image for a protocol placement or an iTerm2 payload.
func EncodePNG(img image.Image) ([]byte, error) {
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

// ScaleImage resizes src to w x h with area sampling. The result is always
// straight-alpha NRGBA so it can be encoded as PNG.
func ScaleImage(src image.Image, w, h int) *image.NRGBA {
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
			sx := MaxInt(1, (x1-x0)/4)
			sy := MaxInt(1, (y1-y0)/4)
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

// placementImage returns the stable id and encoded bytes for one image,
// encoding it once and caching the result across frames.
func (s *Session) placementImage(p ImagePlacement) *placementEntry {
	if p.Img == nil || p.W <= 0 || p.H <= 0 || p.TotalRows <= 0 {
		return nil
	}
	bounds := p.Img.Bounds()
	key := placementKey{ptr: imagePointer(p.Img), src: p.Src, w: bounds.Dx(), h: bounds.Dy()}
	if s.placementCache == nil {
		s.placementCache = make(map[placementKey]*placementEntry)
	}
	if e := s.placementCache[key]; e != nil {
		return e
	}
	data, err := EncodePNG(p.Img)
	if err != nil || len(data) > protocol.DefaultLimits.MaxImageBytes {
		// An image that cannot fit one placement is omitted rather than
		// failing the whole frame.
		return nil
	}
	if s.nextPlacementID == 0 {
		s.nextPlacementID = 1
	}
	e := &placementEntry{id: s.nextPlacementID, data: data}
	s.nextPlacementID++
	if len(data) > maxPlacementCacheBytes {
		return e
	}
	s.placementCache[key] = e
	s.placementBytes += len(data)
	s.evictPlacements()
	return e
}

func (s *Session) evictPlacements() {
	for s.placementBytes > maxPlacementCacheBytes || len(s.placementCache) > maxPlacementCacheEntries {
		for key, e := range s.placementCache {
			delete(s.placementCache, key)
			s.placementBytes -= len(e.data)
			break
		}
	}
}

// ProtocolPlacements maps engine image fragments to wire placements and clips
// them to the frame. Layout can place an image partly outside the viewport
// (negative offsets or a box wider than the terminal); the wire contract
// requires every placement to be inside the frame.
func (s *Session) ProtocolPlacements(dst []protocol.Placement, placements []ImagePlacement, w, h int) []protocol.Placement {
	dst = dst[:0]
	for _, p := range placements {
		if p.W <= 0 || p.H <= 0 || p.TotalRows <= 0 {
			continue
		}
		x0, y0 := p.X, p.Y
		x1, y1 := p.X+p.W, p.Y+p.H
		if x0 < 0 {
			x0 = 0
		}
		if y0 < 0 {
			y0 = 0
		}
		if x1 > w {
			x1 = w
		}
		if y1 > h {
			y1 = h
		}
		if x1 <= x0 || y1 <= y0 {
			continue
		}
		e := s.placementImage(p)
		if e == nil {
			continue
		}
		rowStart := p.RowStart
		if p.Y < 0 {
			rowStart += -p.Y
		}
		if rowStart < 0 || rowStart >= p.TotalRows {
			continue
		}
		dst = append(dst, protocol.Placement{
			Key:       strconv.Itoa(e.id),
			X:         x0,
			Y:         y0,
			W:         x1 - x0,
			H:         y1 - y0,
			RowStart:  rowStart,
			TotalRows: p.TotalRows,
			Format:    protocol.PlacementPNG,
			Data:      e.data,
		})
	}
	return dst
}
