package frontend

import (
	"fmt"
	"io"
	"strings"

	"browser/internal/protocol"
)

// Presenter writes engine frames to the terminal and keeps the row diff that
// suppresses unchanged lines. It owns the ANSI output; the engine only sends
// cell values.
type Presenter struct {
	out   io.Writer
	color bool
	gfx   *GFX

	prev          []string
	lastScroll    int
	lastNative    bool
	nativeSet     bool
	state         protocol.State
	dialog        *DialogView
	selectView    *SelectView
	history       *historyView
	download      *DownloadView
	notice        *NoticeView
	hint          *HintView
	console       *protocol.ConsoleView
	consoleActive bool
	lastFrame     protocol.Frame
	hasFrame      bool
	lastOverlay   bool
	tabs          []TabView
	tabOffset     int
}

// NewPresenter creates a presenter for one terminal. color selects SGR output;
// monochrome output keeps text attributes so links stay visible.
func NewPresenter(out io.Writer, color bool) *Presenter {
	return &Presenter{out: out, color: color}
}

// SetGraphics installs the terminal image renderer for this presenter. Modes
// without native graphics leave it nil.
func (p *Presenter) SetGraphics(mode GraphicsMode, cellW, cellH int) {
	if p == nil {
		return
	}
	if p.gfx != nil && p.out != nil {
		var sb strings.Builder
		p.gfx.ResetForTab(&sb)
		if sb.Len() > 0 {
			_, _ = io.WriteString(p.out, sb.String())
		}
	}
	p.gfx = NewGFXRenderer(mode, cellW, cellH)
	p.prev = nil
	p.nativeSet = false
	p.lastNative = false
}

// ClearGraphics removes placements this presenter has shown. Call it before
// leaving the alternate screen.
func (p *Presenter) ClearGraphics(sb *strings.Builder) {
	if p.gfx != nil {
		p.gfx.Clear(sb)
	}
}

// Reset forces the next frame to repaint every row. It is used by tests and by
// callers that have written over the screen out of band.
func (p *Presenter) Reset() { p.prev = nil }

// ResetForTab prepares the shared presenter for a tab switch. Native
// placements from the old tab are removed, all overlay pointers are cleared and
// the cached frame is dropped so the new tab repaints from scratch.
func (p *Presenter) ResetForTab() {
	if p == nil {
		return
	}
	if p.gfx != nil && p.out != nil {
		var sb strings.Builder
		p.gfx.ResetForTab(&sb)
		if sb.Len() > 0 {
			_, _ = io.WriteString(p.out, sb.String())
		}
	}
	p.prev = nil
	p.lastFrame = protocol.Frame{}
	p.hasFrame = false
	p.lastScroll = 0
	p.lastNative = false
	p.nativeSet = false
	p.state = protocol.State{}
	p.dialog = nil
	p.selectView = nil
	p.history = nil
	p.download = nil
	p.notice = nil
	p.hint = nil
	p.console = nil
	p.consoleActive = false
	p.lastOverlay = false
}

// SetTabs installs the tab-bar labels. A change forces a repaint because the
// toolbar row is composed from the presenter's cached state.
func (p *Presenter) SetTabs(tabs []TabView) {
	if p == nil || tabsEqual(p.tabs, tabs) {
		return
	}
	p.tabs = append(p.tabs[:0], tabs...)
	p.prev = nil
}

// SetTabOffset selects the first visible tab when the strip has to scroll.
func (p *Presenter) SetTabOffset(offset int) {
	if p == nil || p.tabOffset == offset {
		return
	}
	p.tabOffset = offset
	p.prev = nil
}

func tabsEqual(a, b []TabView) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// SetState stores the chrome snapshot. Normal frames are composed from it when
// State.FrontendChrome is set.
// SetDialog installs or clears the modal dialog layer. The next Present or
// Refresh composes it over the content frame.
func (p *Presenter) SetDialog(d *DialogView) {
	p.dialog = d
	p.prev = nil
}

// SetSelect installs or clears the page select popup.
func (p *Presenter) SetSelect(v *SelectView) {
	p.selectView = v
	p.prev = nil
}

// SetHistory installs or clears the history popup.
func (p *Presenter) SetHistory(v *historyView) {
	p.history = v
	p.prev = nil
}

// SetDownload installs or clears the download curl hint.
func (p *Presenter) SetDownload(v *DownloadView) {
	p.download = v
	p.prev = nil
}

// SetNotice installs or clears the informational notice.
func (p *Presenter) SetNotice(v *NoticeView) {
	p.notice = v
	p.prev = nil
}

// SetHint installs or clears the link-hint overlay.
func (p *Presenter) SetHint(v *HintView) {
	p.hint = v
	p.prev = nil
}

func (p *Presenter) SetState(st protocol.State) {
	// State comes from the engine. Clamp before it can size a frame or canvas,
	// matching the protocol frame limit rather than trusting the sender.
	if st.Width > protocol.DefaultLimits.MaxFrameWidth {
		st.Width = protocol.DefaultLimits.MaxFrameWidth
	}
	if st.Height > protocol.DefaultLimits.MaxFrameHeight {
		st.Height = protocol.DefaultLimits.MaxFrameHeight
	}
	if p.state.FrontendChrome != st.FrontendChrome ||
		p.state.HeaderRows != st.HeaderRows ||
		p.state.Width != st.Width || p.state.Height != st.Height {
		p.prev = nil
	}
	p.state = st
}

// overlayOpen reports whether a front-end window covers the page. Native
// terminal images composite above the cell grid, so they must not be placed
// beneath a menu, popup or modal; the engine draws them as half-block art while
// the window is open.
func (p *Presenter) overlayOpen() bool {
	return p.state.Menu != protocol.MenuNone || p.dialog != nil ||
		p.selectView != nil || p.history != nil || p.download != nil || p.notice != nil || p.hint != nil
}

// Refresh re-presents the last frame with the current state. It is used when
// the front end changes a chrome-only value (such as a local quit prompt)
// without receiving a new engine frame.
func (p *Presenter) Refresh() error {
	if p == nil {
		return nil
	}
	if p.consoleActive && p.console != nil {
		return p.PresentConsole(p.console)
	}
	if p.hasFrame {
		return p.Present(p.lastFrame, p.lastScroll, p.lastNative)
	}
	// Defensive clamp in case a state bypassed SetState.
	w := p.state.Width
	if w > protocol.DefaultLimits.MaxFrameWidth {
		w = protocol.DefaultLimits.MaxFrameWidth
	}
	height := p.state.Height
	if height > protocol.DefaultLimits.MaxFrameHeight {
		height = protocol.DefaultLimits.MaxFrameHeight
	}
	if !p.state.FrontendChrome || w <= 0 || p.state.HeaderRows <= 0 ||
		height <= p.state.HeaderRows+1 {
		return nil
	}
	// A modal can arrive before the first page frame. Compose it over a blank
	// content rectangle so the download/notice/dialog is still visible.
	h := height - p.state.HeaderRows - 1
	f := protocol.Frame{W: w, H: h, Cells: make([]protocol.Cell, w*h)}
	bg := rgb(255, 255, 255)
	for i := range f.Cells {
		f.Cells[i] = protocol.Cell{R: ' ', BG: bg}
	}
	return p.Present(f, p.state.Scroll, p.state.Flags&protocol.StateNativeImages != 0)
}

// cloneFrameCells copies the cell buffer so front-end overlays can paint
// without mutating the base frame retained for the next Refresh.
func cloneFrameCells(f protocol.Frame) protocol.Frame {
	cells := make([]protocol.Cell, len(f.Cells))
	copy(cells, f.Cells)
	f.Cells = cells
	return f
}

// Present writes one frame. scroll is the page scroll offset; native reports
// that the engine is writing terminal-native image placements itself, so the
// presenter must leave the screen alone. A scroll, native-mode or size change
// forces a full repaint.
func (p *Presenter) Present(f protocol.Frame, scroll int, native bool) error {
	if p == nil || p.out == nil || f.W <= 0 || f.H <= 0 {
		return nil
	}
	if p.consoleActive {
		p.consoleActive = false
		p.console = nil
		p.prev = nil
	}
	if !p.nativeSet {
		p.lastNative = native
		p.nativeSet = true
		p.prev = nil
	}
	leavingNative := p.lastNative && !native
	if native != p.lastNative {
		p.lastNative = native
		p.prev = nil
	}
	if scroll != p.lastScroll {
		p.lastScroll = scroll
		p.prev = nil
	}
	// Cells covered by a scaled run are skipped when a row is painted, so a
	// moved or resized run must repaint the whole screen instead of leaving the
	// old glyphs and the text they covered in place. A layout reflow (images
	// arriving, a hover rebuild) can move runs without changing the scroll.
	if !protocol.RunsEqual(p.lastFrame.Runs, f.Runs) {
		p.prev = nil
	}
	if over := p.overlayOpen(); over != p.lastOverlay {
		p.lastOverlay = over
		p.prev = nil
	}
	p.lastFrame = f
	p.hasFrame = true
	if p.state.FrontendChrome {
		DrawScrollBarFrame(&f, p.state)
	}
	if p.hint != nil {
		// Apply hints to a copy: lastFrame is the clean base for the next
		// Refresh, which must not repaint a stale hint from the previous prefix.
		f = cloneFrameCells(f)
		ApplyHints(&f, p.hint, p.color)
	}
	lines := EncodeFrame(f, p.color, !p.color)
	if len(lines) != f.H {
		return nil
	}
	placements := f.Placements
	headerRows := 0
	if p.state.FrontendChrome && p.state.Width == f.W &&
		p.state.HeaderRows > 0 && p.state.Height == f.H+p.state.HeaderRows+1 {
		layout := LayoutChromeWithTabs(p.state, p.tabs, p.tabOffset)
		if len(layout.Header) == p.state.Width*p.state.HeaderRows &&
			len(layout.Footer) == p.state.Width {
			if p.state.Menu != protocol.MenuNone {
				if full, ok := ComposeMenuFrame(p.state, f); ok {
					f = full
					lines = EncodeFrame(f, p.color, !p.color)
					headerRows = p.state.HeaderRows
				}
			} else if p.dialog != nil {
				if full, ok := ComposeDialogFrame(p.state, f, p.dialog); ok {
					f = full
					lines = EncodeFrame(f, p.color, !p.color)
					headerRows = p.state.HeaderRows
				}
			} else if p.selectView != nil {
				if full, ok := ComposeSelectFrame(p.state, f, p.selectView); ok {
					f = full
					lines = EncodeFrame(f, p.color, !p.color)
					headerRows = p.state.HeaderRows
				}
			} else if p.history != nil {
				if full, ok := ComposeHistoryFrame(p.state, f, p.history); ok {
					f = full
					lines = EncodeFrame(f, p.color, !p.color)
					headerRows = p.state.HeaderRows
				}
			} else if p.download != nil {
				if full, ok := ComposeDownloadFrame(p.state, f, p.download); ok {
					f = full
					lines = EncodeFrame(f, p.color, !p.color)
					headerRows = p.state.HeaderRows
				}
			} else if p.notice != nil {
				if full, ok := ComposeNoticeFrame(p.state, f, p.notice); ok {
					f = full
					lines = EncodeFrame(f, p.color, !p.color)
					headerRows = p.state.HeaderRows
				}
			} else {
				header := protocol.Frame{W: p.state.Width, H: p.state.HeaderRows, Cells: layout.Header}
				footer := protocol.Frame{W: p.state.Width, H: 1, Cells: layout.Footer}
				hline := EncodeFrame(header, p.color, !p.color)
				fline := EncodeFrame(footer, p.color, !p.color)
				if len(hline) == p.state.HeaderRows && len(fline) == 1 {
					next := make([]string, 0, len(hline)+len(lines)+1)
					next = append(next, hline...)
					next = append(next, lines...)
					next = append(next, fline...)
					lines = next
					headerRows = p.state.HeaderRows
				}
			}
		}
	}
	if headerRows > 0 && len(placements) > 0 {
		shifted := make([]protocol.Placement, len(placements))
		copy(shifted, placements)
		for i := range shifted {
			shifted[i].Y += headerRows
		}
		placements = shifted
	}
	needClear := p.prev == nil || len(p.prev) != len(lines)
	if needClear {
		p.prev = make([]string, len(lines))
	}
	var sb strings.Builder
	if leavingNative && p.gfx != nil {
		p.gfx.Clear(&sb)
	}
	if native && p.gfx != nil {
		p.gfx.BeginFrame(&sb)
	}
	if needClear {
		if native && p.gfx != nil {
			p.gfx.ClearScreen(&sb)
		} else {
			sb.WriteString("\x1b[2J")
		}
	}
	for i := range lines {
		if !p.chromeRow(i, len(lines), headerRows) && p.prev[i] == lines[i] {
			continue
		}
		fmt.Fprintf(&sb, "\x1b[%d;1H", i+1)
		sb.WriteString(lines[i])
		if !p.color {
			// Monochrome lines are trimmed, so clear any longer previous cell
			// run left on the row.
			sb.WriteString("\x1b[K")
		}
		p.prev[i] = lines[i]
	}
	if native && p.gfx != nil && !p.overlayOpen() {
		p.gfx.PlaceAll(&sb, placements)
	}
	if sb.Len() == 0 {
		return nil
	}
	_, err := io.WriteString(p.out, sb.String())
	return err
}

// DrawScrollBarFrame paints the page scrollbar on the last content column from
// the state snapshot. The engine draws the same cells on its fallback path.
func DrawScrollBarFrame(f *protocol.Frame, st protocol.State) {
	max := st.MaxScroll
	if max <= 0 || f.W < 2 || f.H < 2 || len(f.Cells) < f.W*f.H {
		return
	}
	thumbTop, thumbH := scrollThumb(f.H, max, st.Scroll)
	track := rgb(0xdd, 0xe3, 0xea)
	thumb := rgb(0x7a, 0x8a, 0x9a)
	for y := 0; y < f.H; y++ {
		bg := track
		if y >= thumbTop && y < thumbTop+thumbH {
			bg = thumb
		}
		c := &f.Cells[y*f.W+f.W-1]
		c.R = ' '
		c.BG = bg
		c.Flags &^= protocol.CellCont | protocol.CellSkip
		c.Run = 0
	}
}

// scrollThumb derives the page-scrollbar thumb geometry for a content height,
// maximum scroll offset and current offset. The presenter paints it and the
// mouse handler grabs it, so both must agree on the same rows.
func scrollThumb(contentH, max, scroll int) (top, size int) {
	if contentH <= 0 || max <= 0 {
		return 0, contentH
	}
	size = contentH * contentH / (contentH + max)
	if size < 1 {
		size = 1
	}
	if size > contentH {
		size = contentH
	}
	if scroll < 0 {
		scroll = 0
	}
	if scroll > max {
		scroll = max
	}
	if span := contentH - size; span > 0 {
		top = span * scroll / max
	}
	return top, size
}

// chromeRow mirrors the shell reservation while the engine still composes
// chrome: the toolbar rows are always repainted, the URL row uses normal line
// diffing so a terminal-native selection over it is not cleared, and the
// footer row is always repainted.
func (p *Presenter) chromeRow(i, height, headerRows int) bool {
	if height >= 2 && i == height-1 {
		return true
	}
	rows := headerRows
	if rows == 0 {
		switch {
		case height < 3:
		case height < 5:
			rows = 1
		default:
			rows = 2
		}
	}
	if i < 0 || i >= rows {
		return false
	}
	if rows >= 2 && i == 1 {
		return false
	}
	return true
}
