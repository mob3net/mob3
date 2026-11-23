package engine

import "strings"

// GridLines returns the visible grid as plain text rows, one string per row.
// It is used for headless snapshots and copy-page output; terminal styling is
// deliberately left to the front end. The result matches the front end's
// monochrome encoding: run cells become spaces, image cells a block marker,
// and trailing spaces are trimmed.
func GridLines(g *Grid) []string {
	if g == nil || g.W < 0 || g.H < 0 || len(g.Cells) != g.W*g.H {
		return nil
	}
	out := make([]string, g.H)
	for y := 0; y < g.H; y++ {
		var sb strings.Builder
		for x := 0; x < g.W; x++ {
			c := &g.Cells[y*g.W+x]
			if c.Skip {
				n := 0
				for x < g.W && g.Cells[y*g.W+x].Skip {
					n++
					x++
				}
				x--
				sb.WriteString(strings.Repeat(" ", n))
				continue
			}
			if c.Cont {
				continue
			}
			if c.Run != 0 && int(c.Run) <= len(g.Runs) {
				r := g.Runs[int(c.Run)-1]
				sb.WriteString(strings.Repeat(" ", max(1, r.Cells)))
				x += r.Cells - 1
				continue
			}
			r := c.R
			if r == 0 {
				r = ' '
			}
			if c.Img {
				sb.WriteRune('▒')
			} else {
				sb.WriteRune(r)
			}
		}
		out[y] = strings.TrimRight(sb.String(), " ")
	}
	return out
}
