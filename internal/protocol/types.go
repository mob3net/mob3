// Package protocol defines the value types, messages and ports that cross the
// boundary between the terminal front end, the page engine and the trusted
// host.
//
// Everything here is plain data. No type in this package may refer to an engine
// node, layout, style or runtime, and no message may carry an image.Image or a
// pointer into page state. Strings and byte slices are untrusted page data:
// the receiving side must render them through its own sanitizer, never write
// them to the terminal raw. Cell runes are validated on decode for the same
// reason.
package protocol

import (
	"fmt"
	"unicode/utf8"
)

// Version is the wire protocol version. Bump it when a message changes shape.
const Version = 1

// SessionID identifies one page session. It exists before tabs so a later tab
// front end does not need a protocol break.
type SessionID uint32

// Class separates the two directions of the stream. Commands travel from the
// front end to a session; events travel back.
type Class uint8

const (
	ClassCommand Class = 1
	ClassEvent   Class = 2
)

// Kind is a message discriminant. Command and event kinds occupy separate
// ranges so a corrupt message cannot be read as the wrong direction.
type Kind uint8

const (
	CmdInit Kind = 1 + iota
	CmdNavigate
	CmdResize
	CmdScroll
	CmdKey
	CmdMouse
	CmdApp
	CmdFind
	CmdAbort
	CmdShutdown
	CmdDialogResult
	CmdClientError
)

const (
	EvFrame Kind = 32 + iota
	EvState
	EvLinks
	EvDialog
	EvClipboard
	EvConsole
	EvCrash
	EvSelection
	EvFindResults
	EvDownload
	EvQuit
)

// Color is an RGBA cell color. Set distinguishes an unset color from black.
type Color struct {
	R, G, B, A uint8
	Set        bool
}

// Cell attribute bits.
const (
	AttrBold uint8 = 1 << iota
	AttrItalic
	AttrUnderline
	AttrStrike
	AttrReverse
)

// Cell flag bits.
const (
	// CellCont marks a cell covered by an earlier double-width rune.
	CellCont uint8 = 1 << iota
	// CellImg marks a cell painted as half-block image art.
	CellImg
	// CellSkip marks a cell covered by a scaled-text run.
	CellSkip
)

// Cell is one terminal cell of a content frame. The layout is fixed and plain
// data so the in-process transport can copy it and the socket transport can
// write it without reflection.
type Cell struct {
	R     rune
	FG    Color
	BG    Color
	Attrs uint8
	Flags uint8
	// Run is a 1-based index into Frame.Runs for the scaled-text run that
	// starts at this cell. Zero means no run.
	Run uint16
}

// ScaledRun is a kitty-style scaled text run. The front end emits the terminal
// sequence; the engine no longer carries one in a cell.
type ScaledRun struct {
	X, Y  int
	Cells int
	Scale int
	Text  string
	FG    Color
	BG    Color
	Attrs uint8
}

// PlacementFormat identifies the encoding of Placement.Data.
type PlacementFormat uint8

const (
	PlacementPNG PlacementFormat = 1
)

// Placement is one native terminal image placement. Data is an encoded image,
// never a decoded bitmap. The sender treats the bytes as immutable and may
// share cached blobs between frames; the front end decodes them only for
// terminal output, with its own size limits.
type Placement struct {
	Key       string
	X, Y      int
	W, H      int
	RowStart  int
	TotalRows int
	Format    PlacementFormat
	Data      []byte
}

// Rect is a content-viewport rectangle in terminal cells.
type Rect struct {
	X, Y, W, H int
}

// Link is one visible link for hints, focus and navigation. Y is relative to
// the content frame, not the terminal.
type Link struct {
	Index int
	X, Y  int
	W, H  int
	Text  string
	Href  string
}

// Frame is the engine's complete picture of the cells the front end should
// display. Until chrome composition moves to the front end, H covers the full
// terminal screen, including the rows the shell composes; after that it is only
// the content rectangle. Rows owned by the shell are always drawn above page
// content.
type Frame struct {
	// Version changes whenever the link metadata for this frame changes.
	Version    uint32
	W, H       int
	Cells      []Cell
	Runs       []ScaledRun
	Placements []Placement
}

// PageKind lets the front end label the footer without knowing the engine.
type PageKind uint8

const (
	PageHTML PageKind = iota + 1
	PagePDF
	PageAbout
	PageError
	PageDownload
)

// State flag bits.
const (
	StateDark uint32 = 1 << iota
	StateReader
	StateJSError
	StateModuleError
	StateHover
	StateImages
	StateBigText
	StateLoopStopped
	// StateNativeImages reports that the engine is writing terminal-native
	// image placements itself, so the front end must leave those frames alone.
	StateNativeImages
)

// State is the small, trusted snapshot the chrome needs. The engine sends it
// when something changes; it is never derived from page text without the front
// end sanitizing it.
type State struct {
	URL, Title, Status string
	Loading            bool
	Progress           uint8
	Scroll, MaxScroll  int
	Kind               PageKind
	Flags              uint32
}

// Limits bounds every variable-size field on decode. A hostile engine must not
// be able to make the front end allocate or paint without a cap.
type Limits struct {
	MaxMessageBytes int
	MaxFrameWidth   int
	MaxFrameHeight  int
	MaxCells        int
	MaxRuns         int
	MaxLinks        int
	MaxPlacements   int
	MaxTextBytes    int
	MaxImageBytes   int
}

// DefaultLimits are the working caps for the in-process transport.
var DefaultLimits = Limits{
	MaxMessageBytes: 16 << 20,
	MaxFrameWidth:   1024,
	MaxFrameHeight:  1024,
	MaxCells:        1 << 20,
	MaxRuns:         1<<16 - 1,
	MaxLinks:        1 << 14,
	MaxPlacements:   1 << 12,
	MaxTextBytes:    1 << 20,
	MaxImageBytes:   8 << 20,
}

func (l Limits) normalized() Limits {
	d := DefaultLimits
	if l.MaxMessageBytes <= 0 {
		l.MaxMessageBytes = d.MaxMessageBytes
	}
	if l.MaxFrameWidth <= 0 {
		l.MaxFrameWidth = d.MaxFrameWidth
	}
	if l.MaxFrameHeight <= 0 {
		l.MaxFrameHeight = d.MaxFrameHeight
	}
	if l.MaxCells <= 0 {
		l.MaxCells = d.MaxCells
	}
	if l.MaxRuns <= 0 {
		l.MaxRuns = d.MaxRuns
	}
	if l.MaxLinks <= 0 {
		l.MaxLinks = d.MaxLinks
	}
	if l.MaxPlacements <= 0 {
		l.MaxPlacements = d.MaxPlacements
	}
	if l.MaxTextBytes <= 0 {
		l.MaxTextBytes = d.MaxTextBytes
	}
	if l.MaxImageBytes <= 0 {
		l.MaxImageBytes = d.MaxImageBytes
	}
	return l
}

// Validate checks a frame against the limits and the terminal-safety rules.
func (f Frame) Validate(lim Limits) error {
	lim = lim.normalized()
	if f.W < 0 || f.H < 0 || f.W > lim.MaxFrameWidth || f.H > lim.MaxFrameHeight {
		return fmt.Errorf("protocol: frame size %dx%d out of range", f.W, f.H)
	}
	if f.W > 0 && f.H > lim.MaxCells/f.W {
		return fmt.Errorf("protocol: frame has too many cells")
	}
	if len(f.Cells) != f.W*f.H {
		return fmt.Errorf("protocol: frame has %d cells, want %d", len(f.Cells), f.W*f.H)
	}
	if len(f.Runs) > lim.MaxRuns {
		return fmt.Errorf("protocol: too many scaled runs")
	}
	for i, r := range f.Runs {
		if r.X < 0 || r.Y < 0 || r.Cells <= 0 || r.Scale < 2 || r.Scale > 32 {
			return fmt.Errorf("protocol: run %d has invalid placement", i)
		}
		if r.X+r.Cells > f.W || r.Y >= f.H {
			return fmt.Errorf("protocol: run %d is outside the frame", i)
		}
		if len(r.Text) > lim.MaxTextBytes {
			return fmt.Errorf("protocol: run %d text is too long", i)
		}
		if !SafeText(r.Text) {
			return fmt.Errorf("protocol: run %d text contains a control character", i)
		}
	}
	if len(f.Placements) > lim.MaxPlacements {
		return fmt.Errorf("protocol: too many placements")
	}
	for i, p := range f.Placements {
		if p.W <= 0 || p.H <= 0 || p.X < 0 || p.Y < 0 || p.X+p.W > f.W || p.Y+p.H > f.H {
			return fmt.Errorf("protocol: placement %d is outside the frame", i)
		}
		if p.Format != PlacementPNG {
			return fmt.Errorf("protocol: placement %d has unknown format", i)
		}
		if len(p.Data) == 0 || len(p.Data) > lim.MaxImageBytes {
			return fmt.Errorf("protocol: placement %d has invalid image data", i)
		}
	}
	for i, c := range f.Cells {
		if !SafeRune(c.R) {
			return fmt.Errorf("protocol: cell %d has unsafe rune %U", i, c.R)
		}
		if int(c.Run) > len(f.Runs) {
			return fmt.Errorf("protocol: cell %d refers to missing run %d", i, c.Run)
		}
	}
	return nil
}

// SafeRune reports whether a rune may be painted as page text. Control
// characters are rejected so a frame can never carry an escape sequence.
func SafeRune(r rune) bool {
	if r == 0 {
		return true
	}
	if !utf8.ValidRune(r) {
		return false
	}
	if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
		return false
	}
	return true
}

// SafeText reports whether s is valid UTF-8 with no control characters. It is
// used for scaled text, which the front end places directly into a terminal
// escape sequence.
func SafeText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if !SafeRune(r) {
			return false
		}
	}
	return true
}
