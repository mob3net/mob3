package protocol

import "testing"

// BenchmarkCodecFrame measures the steady-state cost of turning one full
// 152x35 content frame into bytes and back. Phase 0 records it as the baseline
// for the later transport split.
func BenchmarkCodecFrame(b *testing.B) {
	const w, h = 152, 35
	f := Frame{
		Version:    1,
		W:          w,
		H:          h,
		Cells:      make([]Cell, w*h),
		Runs:       []ScaledRun{},
		Placements: []Placement{},
	}
	for i := range f.Cells {
		f.Cells[i] = Cell{
			R:     rune('a' + i%26),
			FG:    Color{R: 200, G: 210, B: 220, A: 255, Set: true},
			BG:    Color{R: 10, G: 20, B: 30, A: 255, Set: true},
			Attrs: AttrBold,
			Flags: 0,
		}
	}
	ev := Event{Session: 1, Body: f}
	buf := make([]byte, 0, 160<<10)
	b.ReportAllocs()
	b.SetBytes(int64(w * h))
	for i := 0; i < b.N; i++ {
		var err error
		buf, err = AppendEvent(buf[:0], ev, DefaultLimits)
		if err != nil {
			b.Fatal(err)
		}
		if _, _, err := DecodeEvent(buf, DefaultLimits); err != nil {
			b.Fatal(err)
		}
	}
}
