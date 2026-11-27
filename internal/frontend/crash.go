package frontend

import (
	"browser/internal/movekeys"
	"browser/internal/protocol"
	"browser/internal/textwidth"
)

// CrashView is the front-end content surface for a tab whose page engine
// stopped. Message is already sanitized and bounded by the tab. The view is
// paint-only: a dead tab keeps its tab-strip and shell keys.
type CrashView struct {
	Message string
	URL     string
	Scroll  int
}

// crashTitle is the heading shared by the crash view and the footer.
const crashTitle = "Page engine stopped"

// maxCrashDetailRunes bounds the retained crash text. A panic value or a child
// stderr report is untrusted input; the view scrolls this and the footer
// truncates it to one row.
const maxCrashDetailRunes = 8 << 10

// crashLayout is the wrapped crash text and the rows available to show it.
type crashLayout struct {
	X, Inner        int
	Top, Bottom     int
	TitleY, URLY    int
	MessageY, HintY int
	Lines           []string
	Visible         int
}

func crashLayoutFor(st protocol.State, v *CrashView) crashLayout {
	if v == nil || st.Width < 12 || st.Height <= 0 {
		return crashLayout{}
	}
	top := st.HeaderRows
	if top < 0 {
		top = 0
	}
	bottom := st.Height - 1
	if top >= bottom || bottom-top < 2 {
		return crashLayout{}
	}
	x, inner := 2, st.Width-4
	if inner < 8 {
		x, inner = 1, st.Width-2
	}
	if inner < 4 {
		return crashLayout{}
	}
	l := crashLayout{
		X: x, Inner: inner, Top: top, Bottom: bottom,
		URLY: -1, TitleY: top + 1,
	}
	if l.TitleY >= bottom {
		return crashLayout{}
	}
	l.MessageY = l.TitleY + 2
	if v.URL != "" && l.MessageY < bottom-1 {
		l.URLY = l.MessageY
		l.MessageY += 2
	}
	l.HintY = bottom - 1
	l.Visible = l.HintY - l.MessageY
	if l.Visible < 1 {
		l.Visible = 1
	}
	msg := protocol.StripControls(v.Message)
	if msg == "" {
		msg = "The page engine stopped without reporting a reason."
	}
	l.Lines = textwidth.Wrap(msg, inner)
	return l
}

func (l crashLayout) maxScroll() int {
	if len(l.Lines) <= l.Visible {
		return 0
	}
	return len(l.Lines) - l.Visible
}

// crashView returns the presenter view for this tab's current crash state.
// The URL is the display form already used by the address bar.
func (l *Tab) crashView() *CrashView {
	url := ""
	if shown, ok := displayURL(l.State.Page.URL); ok {
		url = shown
	}
	return &CrashView{Message: l.CrashMessage, URL: url, Scroll: l.crashScroll}
}

// scrollCrash applies a viewport movement key to the dead-tab error view. It
// reports whether the key belongs to movement so page and tab keys stay
// unaffected.
func (l *Tab) scrollCrash(key string) bool {
	if !l.Dead {
		return false
	}
	move, ok := movekeys.Lookup(key)
	if !ok {
		return false
	}
	layout := crashLayoutFor(l.State.Page, l.crashView())
	limit := layout.maxScroll()
	before := l.crashScroll
	switch move {
	case movekeys.Top:
		l.crashScroll = 0
	case movekeys.Bottom:
		l.crashScroll = limit
	default:
		if delta, ok := move.Delta(layout.Visible); ok {
			l.crashScroll += delta
		}
	}
	if l.crashScroll < 0 {
		l.crashScroll = 0
	}
	if l.crashScroll > limit {
		l.crashScroll = limit
	}
	if l.crashScroll == before {
		return true
	}
	if l.Presenter != nil {
		l.Presenter.SetCrash(l.crashView())
		_ = l.Presenter.Refresh()
	}
	return true
}

// DrawCrash paints the dead-tab error view over the content rectangle. The
// header and footer stay visible, so the tab strip, address and shell keys are
// still usable.
func DrawCrash(c *chromeCanvas, st protocol.State, v *CrashView) {
	if c == nil || v == nil {
		return
	}
	l := crashLayoutFor(st, v)
	if l.Inner < 4 || l.Bottom > c.h {
		return
	}
	// Replace the page cells entirely: the last frame can contain wide glyphs,
	// scaled runs and stale scrollbar cells that must not show through.
	bg := rgb(0xfb, 0xf2, 0xf2)
	for y := l.Top; y < l.Bottom; y++ {
		row := y * c.w
		for x := 0; x < c.w; x++ {
			c.cells[row+x] = protocol.Cell{R: ' ', BG: bg}
		}
	}

	c.text(l.X, l.TitleY, crashTitle, colErrorFG, protocol.AttrBold)
	if l.URLY >= 0 {
		c.text(l.X, l.URLY,
			textwidth.Truncate("Page: "+v.URL, l.Inner), colFooterFG, 0)
	}

	scroll := v.Scroll
	if max := l.maxScroll(); scroll > max {
		scroll = max
	}
	if scroll < 0 {
		scroll = 0
	}
	end := scroll + l.Visible
	if end > len(l.Lines) {
		end = len(l.Lines)
	}
	for i, line := range l.Lines[scroll:end] {
		c.text(l.X, l.MessageY+i, textwidth.Truncate(line, l.Inner), colFooterFG, 0)
	}
	hint := "t: new tab   W: close tab   q: quit"
	if l.maxScroll() > 0 {
		hint = "↑/↓/PgUp/PgDn scroll   " + hint
	}
	c.text(l.X, l.HintY, textwidth.Truncate(hint, l.Inner), colWarnFG, protocol.AttrBold)
}
