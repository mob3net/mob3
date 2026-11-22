package browser

import "browser/internal/protocol"

// toProtocolFrame converts a painted content grid into the value that crosses
// the engine/frontend seam. It is the only place internal cell fields are
// mapped to the wire contract; image placements are added when native graphics
// move to the front end.
func toProtocolFrame(g *Grid, version uint32) protocol.Frame {
	if g == nil {
		return protocol.Frame{Version: version}
	}
	f := protocol.Frame{
		Version: version,
		W:       g.W,
		H:       g.H,
		Cells:   make([]protocol.Cell, len(g.Cells)),
		Runs:    make([]protocol.ScaledRun, len(g.Runs)),
	}
	for i, c := range g.Cells {
		f.Cells[i] = protocol.Cell{
			R:     c.R,
			FG:    protoColor(c.FG),
			BG:    protoColor(c.BG),
			Attrs: protoAttrs(c.Bold, c.Italic, c.Underline, c.Strike, c.Reverse),
			Flags: protoFlags(c.Cont, c.Img, c.Skip),
			Run:   c.Run,
		}
	}
	for i, r := range g.Runs {
		f.Runs[i] = protocol.ScaledRun{
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
	return f
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
