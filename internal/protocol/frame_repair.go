package protocol

// repairFrame salvages a frame that failed validation. It is a containment
// path, not a hot path: invalid placements are dropped, unsafe glyphs become
// spaces, invalid scaled runs are removed and cells that referenced them fall
// back to normal text. The result must pass Validate before it is delivered.
//
// A frame whose dimensions or cell count are inconsistent cannot be repaired
// safely; the caller reports that as Crash without a frame.
func repairFrame(f Frame, lim Limits) (Frame, bool) {
	lim = lim.normalized()
	if f.W <= 0 || f.H <= 0 || f.W > lim.MaxFrameWidth || f.H > lim.MaxFrameHeight {
		return Frame{}, false
	}
	if f.W > 0 && f.H > lim.MaxCells/f.W {
		return Frame{}, false
	}
	if len(f.Cells) != f.W*f.H {
		return Frame{}, false
	}
	out := Frame{Version: f.Version, W: f.W, H: f.H}
	out.Cells = make([]Cell, len(f.Cells))
	copy(out.Cells, f.Cells)

	// Keep only valid runs and remember the old-to-new index mapping. Cells
	// run indices start at 1; index 0 means normal text.
	remap := make([]uint16, len(f.Runs)+1)
	for i, r := range f.Runs {
		if len(out.Runs) >= lim.MaxRuns {
			break
		}
		if r.X < 0 || r.Y < 0 || r.Cells <= 0 || r.Scale < 2 || r.Scale > 32 ||
			r.X+r.Cells > f.W || r.Y >= f.H ||
			len(r.Text) > lim.MaxTextBytes || !SafeText(r.Text) {
			continue
		}
		out.Runs = append(out.Runs, r)
		remap[i+1] = uint16(len(out.Runs))
	}
	for i := range out.Cells {
		c := &out.Cells[i]
		if !SafeRune(c.R) {
			c.R = ' '
		}
		if c.Run != 0 {
			if int(c.Run) >= len(remap) || remap[c.Run] == 0 {
				c.Run = 0
			} else {
				c.Run = remap[c.Run]
			}
		}
	}

	for _, p := range f.Placements {
		if len(out.Placements) >= lim.MaxPlacements {
			break
		}
		if p.W <= 0 || p.H <= 0 || p.X < 0 || p.Y < 0 ||
			p.X+p.W > f.W || p.Y+p.H > f.H {
			continue
		}
		if p.Format != PlacementPNG || len(p.Data) == 0 || len(p.Data) > lim.MaxImageBytes {
			continue
		}
		// This is an error path, so detach the blob even though normal frames
		// share immutable cached image data.
		p.Data = append([]byte(nil), p.Data...)
		out.Placements = append(out.Placements, p)
	}
	if err := out.Validate(lim); err != nil {
		return Frame{}, false
	}
	return out, true
}
