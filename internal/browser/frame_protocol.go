package browser

import "browser/internal/protocol"

// toProtocolFrame converts a painted content grid into the value that crosses
// the engine/frontend seam. It is the only place internal cell fields are
// mapped to the wire contract; image placements are added when native graphics
// move to the front end.
func toProtocolFrame(g *Grid, version uint32) protocol.Frame {
	var f protocol.Frame
	return toProtocolFrameInto(&f, g, version)
}

// toProtocolFrameInto is toProtocolFrame with a reusable destination, used on
// the draw path so a repaint does not allocate a new frame.
func toProtocolFrameInto(dst *protocol.Frame, g *Grid, version uint32) protocol.Frame {
	if g == nil {
		*dst = protocol.Frame{Version: version}
		return *dst
	}
	dst.Version = version
	dst.W = g.W
	dst.H = g.H
	if cap(dst.Cells) < len(g.Cells) {
		dst.Cells = make([]protocol.Cell, len(g.Cells))
	} else {
		dst.Cells = dst.Cells[:len(g.Cells)]
	}
	for i, c := range g.Cells {
		pc := protocol.Cell{
			R:   c.R,
			FG:  protocol.Color{R: c.FG.R, G: c.FG.G, B: c.FG.B, A: c.FG.A, Set: c.FG.Set},
			BG:  protocol.Color{R: c.BG.R, G: c.BG.G, B: c.BG.B, A: c.BG.A, Set: c.BG.Set},
			Run: c.Run,
		}
		if c.Bold {
			pc.Attrs |= protocol.AttrBold
		}
		if c.Italic {
			pc.Attrs |= protocol.AttrItalic
		}
		if c.Underline {
			pc.Attrs |= protocol.AttrUnderline
		}
		if c.Strike {
			pc.Attrs |= protocol.AttrStrike
		}
		if c.Reverse {
			pc.Attrs |= protocol.AttrReverse
		}
		if c.Cont {
			pc.Flags |= protocol.CellCont
		}
		if c.Img {
			pc.Flags |= protocol.CellImg
		}
		if c.Skip {
			pc.Flags |= protocol.CellSkip
		}
		dst.Cells[i] = pc
	}
	if cap(dst.Runs) < len(g.Runs) {
		dst.Runs = make([]protocol.ScaledRun, len(g.Runs))
	} else {
		dst.Runs = dst.Runs[:len(g.Runs)]
	}
	for i, r := range g.Runs {
		dst.Runs[i] = protocol.ScaledRun{
			X:     r.X,
			Y:     r.Y,
			Cells: r.Cells,
			Scale: r.Scale,
			Text:  r.Text,
			FG:    protoColor(r.FG),
			BG:    protoColor(r.BG),
			Attrs: protoAttrs(r.Bold, r.Italic, r.Underline, r.Strike, r.Reverse),
		}
	}
	dst.Placements = dst.Placements[:0]
	return *dst
}

func protoColor(c Color) protocol.Color {
	return protocol.Color{R: c.R, G: c.G, B: c.B, A: c.A, Set: c.Set}
}

func protoAttrs(bold, italic, underline, strike, reverse bool) uint8 {
	var a uint8
	if bold {
		a |= protocol.AttrBold
	}
	if italic {
		a |= protocol.AttrItalic
	}
	if underline {
		a |= protocol.AttrUnderline
	}
	if strike {
		a |= protocol.AttrStrike
	}
	if reverse {
		a |= protocol.AttrReverse
	}
	return a
}

func protoFlags(cont, img, skip bool) uint8 {
	var f uint8
	if cont {
		f |= protocol.CellCont
	}
	if img {
		f |= protocol.CellImg
	}
	if skip {
		f |= protocol.CellSkip
	}
	return f
}
