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
	download      *DownloadView
	notice        *NoticeView
	hint          *HintView
	console       *protocol.ConsoleView
	consoleActive bool
	lastFrame     protocol.Frame
	hasFrame      bool
}

// NewPresenter creates a presenter for one terminal. color selects SGR output;
// monochrome output keeps text attributes so links stay visible.
func NewPresenter(out io.Writer, color bool) *Presenter {
	return &Presenter{out: out, color: color}
}

// SetGraphics installs the terminal image renderer for this presenter. Modes
// without native graphics leave it nil.
func (p *Presenter) SetGraphics(mode GraphicsMode, cellW, cellH int) {
	p.gfx = NewGFXRenderer(mode, cellW, cellH)
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
	if p.state.FrontendChrome != st.FrontendChrome ||
		p.state.HeaderRows != st.HeaderRows ||
		p.state.Width != st.Width || p.state.Height != st.Height {
		p.prev = nil
	}
	p.state = st
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
	if !p.state.FrontendChrome || p.state.Width <= 0 || p.state.HeaderRows <= 0 ||
		p.state.Height <= p.state.HeaderRows+1 {
		return nil
	}
	// A modal can arrive before the first page frame. Compose it over a blank
	// content rectangle so the download/notice/dialog is still visible.
	h := p.state.Height - p.state.HeaderRows - 1
	f := protocol.Frame{W: p.state.Width, H: h, Cells: make([]protocol.Cell, p.state.Width*h)}
	bg := rgb(255, 255, 255)
	for i := range f.Cells {
		f.Cells[i] = protocol.Cell{R: ' ', BG: bg}
	}
	return p.Present(f, p.state.Scroll, p.state.Flags&protocol.StateNativeImages != 0)
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
	p.lastFrame = f
	p.hasFrame = true
	if p.state.FrontendChrome {
		DrawScrollBarFrame(&f, p.state)
	}
	if p.hint != nil {
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
		layout := LayoutChrome(p.state)
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
	if native && p.gfx != nil {
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
	vh := f.H
	thumbH := vh * vh / (vh + max)
	if thumbH < 1 {
		thumbH = 1
	}
	if thumbH > vh {
		thumbH = vh
	}
	scroll := st.Scroll
	if scroll < 0 {
		scroll = 0
	}
	if scroll > max {
		scroll = max
	}
	thumbTop := 0
	if vh > thumbH {
		thumbTop = (vh - thumbH) * scroll / max
	}
	track := rgb(0xdd, 0xe3, 0xea)
	thumb := rgb(0x7a, 0x8a, 0x9a)
	for y := 0; y < vh; y++ {
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
