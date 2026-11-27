package frontend

// surfaceID identifies one base surface in a tab. Windows paint above the
// surface; the focused surface owns input when no window does.
type surfaceID uint8

const (
	surfacePage surfaceID = iota
	surfaceConsole
)

// paneRect is a terminal-cell rectangle. X and Y are zero-based.
type paneRect struct {
	X, Y, W, H int
}

func (r paneRect) empty() bool { return r.W <= 0 || r.H <= 0 }

func (r paneRect) contains(x, y int) bool {
	return !r.empty() && x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// covers reports whether the pane owns the whole terminal, so out-of-range
// pointer reports still belong to its surface.
func (r paneRect) covers(width, height int) bool {
	return r.X == 0 && r.Y == 0 && r.W >= width && r.H >= height
}

// paneLayout holds the base surface rectangles for one tab.
type paneLayout struct {
	page, console paneRect
}

// layoutPanes gives both surfaces the full terminal until split panes land.
// The page keeps the tab chrome; the console overlays the whole viewport.
func layoutPanes(width, height int) paneLayout {
	full := paneRect{W: width, H: height}
	return paneLayout{page: full, console: full}
}

func (l *Tab) panes() paneLayout {
	return layoutPanes(l.State.Page.Width, l.State.Page.Height)
}

func (l *Tab) paneFor(id surfaceID) paneRect {
	panes := l.panes()
	if id == surfaceConsole {
		return panes.console
	}
	return panes.page
}

// focusedSurface reports which base surface owns input. Until panes are
// split, an open console owns the tab and every other state belongs to the page.
func (l *Tab) focusedSurface() surfaceID {
	if l.consoleOverlay.isOpen() {
		return surfaceConsole
	}
	return surfacePage
}
