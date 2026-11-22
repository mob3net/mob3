package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Wire format (little endian):
//
//	0  magic    uint32  "MOB3"
//	4  version  uint8
//	5  class    uint8   command or event
//	6  kind     uint8
//	7  flags    uint8   reserved, must be zero
//	8  session  uint32
//	12 seq      uint64
//	20 length   uint32  body bytes
//	24 body
const (
	headerSize    = 24
	wireMagic     = 0x33424f4d // bytes 'M','O','B','3'
	minCellSize   = 20
	minRunSize    = 31
	minLinkSize   = 28
	minRectSize   = 16
	minPlaceBytes = 33
	minInt32      = -1 << 31
	maxInt32      = 1<<31 - 1
)

var (
	// ErrTruncated means the buffer ended before a complete message arrived.
	ErrTruncated = errors.New("protocol: truncated message")
	// ErrInvalid means the message header or body is malformed.
	ErrInvalid = errors.New("protocol: invalid message")
	// ErrLimit means a field exceeded a protocol limit.
	ErrLimit = errors.New("protocol: message exceeds limit")
)

type wireHeader struct {
	kind    Kind
	session SessionID
	seq     uint64
	body    []byte
}

// AppendCommand encodes one command and appends it to dst. The returned slice
// includes any bytes already in dst, so callers can reuse a stream buffer.
func AppendCommand(dst []byte, c Command, lim Limits) ([]byte, error) {
	lim = lim.normalized()
	start := len(dst)
	dst = beginMessage(dst)
	e := &encoder{b: dst, lim: lim}
	var kind Kind
	switch v := c.Body.(type) {
	case Init:
		kind = CmdInit
		e.i32(v.Width)
		e.i32(v.Height)
		e.i32(v.CellW)
		e.i32(v.CellH)
		e.f64(v.Zoom)
		e.u32(uint32(v.Caps))
		e.u32(v.Flags)
	case Navigate:
		kind = CmdNavigate
		e.str(v.URL)
		e.bool(v.Push)
		e.i32(v.RestoreScroll)
		e.bool(v.HasRestoreScroll)
	case Resize:
		kind = CmdResize
		e.i32(v.Width)
		e.i32(v.Height)
		e.i32(v.CellW)
		e.i32(v.CellH)
	case Scroll:
		kind = CmdScroll
		e.i32(v.Delta)
		e.i32(v.To)
		e.bool(v.HasTo)
		e.bool(v.Top)
		e.bool(v.Bottom)
	case Key:
		kind = CmdKey
		e.str(v.Token)
	case Mouse:
		kind = CmdMouse
		e.i32(v.X)
		e.i32(v.Y)
		e.i32(v.Button)
		e.i32(v.Wheel)
		e.bool(v.Press)
		e.bool(v.Motion)
		e.bool(v.Shift)
	case App:
		kind = CmdApp
		e.u8(uint8(v.Action))
		e.i32(v.Arg)
	case Find:
		kind = CmdFind
		e.str(v.Query)
		e.bool(v.Next)
		e.bool(v.Prev)
		e.bool(v.Close)
	case Abort:
		kind = CmdAbort
		e.str(v.Reason)
	case Shutdown:
		kind = CmdShutdown
	case DialogResult:
		kind = CmdDialogResult
		e.u32(v.ID)
		e.bool(v.OK)
		e.str(v.Text)
	case SelectResult:
		kind = CmdSelectResult
		e.u32(v.ID)
		e.i32(v.Index)
		e.bool(v.Cancel)
	case ClientError:
		kind = CmdClientError
		e.str(v.Message)
	case Automation:
		kind = CmdAutomation
		e.u8(uint8(v.Op))
		e.str(v.URL)
		e.i32(v.Scale)
		e.str(v.Name)
		e.blob(v.Args, lim.MaxTextBytes)
	default:
		return dst[:start], fmt.Errorf("%w: unknown command body %T", ErrInvalid, c.Body)
	}
	if e.err != nil {
		return dst[:start], e.err
	}
	return finishMessage(e.b, start, ClassCommand, kind, c.Session, c.Seq, lim)
}

// AppendEvent encodes one event and appends it to dst.
func AppendEvent(dst []byte, ev Event, lim Limits) ([]byte, error) {
	lim = lim.normalized()
	start := len(dst)
	dst = beginMessage(dst)
	e := &encoder{b: dst, lim: lim}
	var kind Kind
	switch v := ev.Body.(type) {
	case Frame:
		if err := v.Validate(lim); err != nil {
			return dst, err
		}
		if len(v.Cells) > lim.MaxMessageBytes/minCellSize {
			return dst, fmt.Errorf("%w: frame cells do not fit the message limit", ErrLimit)
		}
		kind = EvFrame
		e.u32(v.Version)
		e.i32(v.W)
		e.i32(v.H)
		e.count(len(v.Cells))
		for _, c := range v.Cells {
			e.cell(c)
		}
		e.count(len(v.Runs))
		for _, r := range v.Runs {
			e.run(r)
		}
		e.count(len(v.Placements))
		for _, p := range v.Placements {
			e.placement(p)
		}
	case State:
		kind = EvState
		e.str(v.URL)
		e.str(v.Title)
		e.str(v.Status)
		e.bool(v.Loading)
		e.u8(v.Progress)
		e.i32(v.Scroll)
		e.i32(v.MaxScroll)
		e.u8(uint8(v.Kind))
		e.u32(v.Flags)
		e.i32(v.Width)
		e.i32(v.Height)
		e.i32(v.HeaderRows)
		e.bool(v.FrontendChrome)
		e.str(v.FooterLeft)
		e.str(v.FooterRight)
		e.u8(uint8(v.FooterLevel))
		e.bool(v.FooterStatusHit)
		e.str(v.PDFHint)
		e.i32(v.PDFHintX)
		e.bool(v.PromptActive)
		e.str(v.PromptText)
		e.i32(v.PromptCursor)
		e.bool(v.PromptSelectAll)
		e.str(v.ZoomLabel)
		e.bool(v.FindActive)
		e.str(v.FindText)
		e.i32(v.FindCursor)
		e.i32(v.FindCurrent)
		e.i32(v.FindTotal)
		e.u8(uint8(v.Menu))
		e.i32(v.MenuIndex)
		e.u32(v.ViewFlags)
		e.i32(v.ZoomPercent)
		e.bool(v.QuitPending)
		e.bool(v.KeysEngine)
	case Links:
		kind = EvLinks
		e.u32(v.Version)
		e.count(len(v.Items))
		for _, l := range v.Items {
			e.link(l)
		}
	case Dialog:
		kind = EvDialog
		e.u32(v.ID)
		e.u8(uint8(v.Kind))
		e.str(v.Text)
		e.str(v.Default)
	case Select:
		kind = EvSelect
		e.u32(v.ID)
		e.count(len(v.Options))
		for _, opt := range v.Options {
			e.str(opt)
		}
		e.i32(v.Selected)
	case Clipboard:
		kind = EvClipboard
		e.str(v.Text)
	case Console:
		kind = EvConsole
		e.u8(uint8(v.Level))
		e.str(v.Text)
	case Crash:
		kind = EvCrash
		e.str(v.Message)
	case Selection:
		kind = EvSelection
		e.str(v.Text)
	case FindResults:
		kind = EvFindResults
		e.str(v.Query)
		e.i32(v.Current)
		e.i32(v.Total)
		e.count(len(v.Rects))
		for _, r := range v.Rects {
			e.rect(r)
		}
	case Download:
		kind = EvDownload
		e.str(v.URL)
		e.str(v.Name)
	case Quit:
		kind = EvQuit
	case AutomationResult:
		kind = EvAutomationResult
		e.u8(uint8(v.Op))
		e.str(v.Text)
		e.blob(v.Data, lim.MaxImageBytes)
		e.str(v.Err)
	case Chrome:
		if err := v.Validate(lim); err != nil {
			return dst, err
		}
		kind = EvChrome
		e.bool(v.Show)
		e.i32(v.Width)
		e.i32(v.HeaderRows)
		e.count(len(v.Header))
		for _, c := range v.Header {
			e.cell(c)
		}
		e.count(len(v.Footer))
		for _, c := range v.Footer {
			e.cell(c)
		}
	default:
		return dst[:start], fmt.Errorf("%w: unknown event body %T", ErrInvalid, ev.Body)
	}
	if e.err != nil {
		return dst[:start], e.err
	}
	return finishMessage(e.b, start, ClassEvent, kind, ev.Session, ev.Seq, lim)
}

// DecodeCommand decodes one command and returns the unconsumed tail. It is safe
// on arbitrary bytes: malformed input returns an error and never panics.
func DecodeCommand(src []byte, lim Limits) (Command, []byte, error) {
	lim = lim.normalized()
	h, rest, err := readHeader(src, ClassCommand, lim)
	if err != nil {
		return Command{}, nil, err
	}
	d := &decoder{b: h.body, lim: lim}
	var body CommandBody
	switch h.kind {
	case CmdInit:
		body = Init{
			Width:  d.i32(),
			Height: d.i32(),
			CellW:  d.i32(),
			CellH:  d.i32(),
			Zoom:   d.f64(),
			Caps:   Capabilities(d.u32()),
			Flags:  d.u32(),
		}
	case CmdNavigate:
		body = Navigate{
			URL:              d.str(),
			Push:             d.bool(),
			RestoreScroll:    d.i32(),
			HasRestoreScroll: d.bool(),
		}
	case CmdResize:
		body = Resize{Width: d.i32(), Height: d.i32(), CellW: d.i32(), CellH: d.i32()}
	case CmdScroll:
		body = Scroll{
			Delta:  d.i32(),
			To:     d.i32(),
			HasTo:  d.bool(),
			Top:    d.bool(),
			Bottom: d.bool(),
		}
	case CmdKey:
		body = Key{Token: d.str()}
	case CmdMouse:
		body = Mouse{
			X:      d.i32(),
			Y:      d.i32(),
			Button: d.i32(),
			Wheel:  d.i32(),
			Press:  d.bool(),
			Motion: d.bool(),
			Shift:  d.bool(),
		}
	case CmdApp:
		body = App{Action: AppAction(d.u8()), Arg: d.i32()}
	case CmdFind:
		body = Find{Query: d.str(), Next: d.bool(), Prev: d.bool(), Close: d.bool()}
	case CmdAbort:
		body = Abort{Reason: d.str()}
	case CmdShutdown:
		body = Shutdown{}
	case CmdDialogResult:
		body = DialogResult{ID: d.u32(), OK: d.bool(), Text: d.str()}
	case CmdSelectResult:
		body = SelectResult{ID: d.u32(), Index: d.i32(), Cancel: d.bool()}
	case CmdClientError:
		body = ClientError{Message: d.str()}
	case CmdAutomation:
		body = Automation{
			Op:    AutomationOp(d.u8()),
			URL:   d.str(),
			Scale: d.i32(),
			Name:  d.str(),
			Args:  d.blob(lim.MaxTextBytes),
		}
	default:
		return Command{}, nil, fmt.Errorf("%w: unknown command kind %d", ErrInvalid, h.kind)
	}
	if d.err != nil {
		return Command{}, nil, d.err
	}
	if d.off != len(d.b) {
		return Command{}, nil, fmt.Errorf("%w: %d trailing command bytes", ErrInvalid, len(d.b)-d.off)
	}
	return Command{Session: h.session, Seq: h.seq, Body: body}, rest, nil
}

// DecodeEvent decodes one event and returns the unconsumed tail.
func DecodeEvent(src []byte, lim Limits) (Event, []byte, error) {
	lim = lim.normalized()
	h, rest, err := readHeader(src, ClassEvent, lim)
	if err != nil {
		return Event{}, nil, err
	}
	d := &decoder{b: h.body, lim: lim}
	var body EventBody
	switch h.kind {
	case EvFrame:
		version := d.u32()
		w, height := d.i32(), d.i32()
		nc := d.count(lim.MaxCells)
		if d.err == nil && !d.need(nc*minCellSize) {
			break
		}
		cells := make([]Cell, nc)
		for i := range cells {
			cells[i] = d.cell()
		}
		nr := d.count(lim.MaxRuns)
		if d.err == nil && !d.need(nr*minRunSize) {
			break
		}
		runs := make([]ScaledRun, nr)
		for i := range runs {
			runs[i] = d.run()
		}
		np := d.count(lim.MaxPlacements)
		if d.err == nil && !d.need(np*minPlaceBytes) {
			break
		}
		placements := make([]Placement, np)
		for i := range placements {
			placements[i] = d.placement()
		}
		f := Frame{Version: version, W: w, H: height, Cells: cells, Runs: runs, Placements: placements}
		if d.err == nil {
			if err := f.Validate(lim); err != nil {
				return Event{}, nil, err
			}
		}
		body = f
	case EvState:
		body = State{
			URL:       d.str(),
			Title:     d.str(),
			Status:    d.str(),
			Loading:   d.bool(),
			Progress:  d.u8(),
			Scroll:    d.i32(),
			MaxScroll: d.i32(),
			Kind:      PageKind(d.u8()),
			Flags:     d.u32(),

			Width:           d.i32(),
			Height:          d.i32(),
			HeaderRows:      d.i32(),
			FrontendChrome:  d.bool(),
			FooterLeft:      d.str(),
			FooterRight:     d.str(),
			FooterLevel:     FooterLevel(d.u8()),
			FooterStatusHit: d.bool(),
			PDFHint:         d.str(),
			PDFHintX:        d.i32(),
			PromptActive:    d.bool(),
			PromptText:      d.str(),
			PromptCursor:    d.i32(),
			PromptSelectAll: d.bool(),
			ZoomLabel:       d.str(),
			FindActive:      d.bool(),
			FindText:        d.str(),
			FindCursor:      d.i32(),
			FindCurrent:     d.i32(),
			FindTotal:       d.i32(),
			Menu:            MenuKind(d.u8()),
			MenuIndex:       d.i32(),
			ViewFlags:       d.u32(),
			ZoomPercent:     d.i32(),
			QuitPending:     d.bool(),
			KeysEngine:      d.bool(),
		}
	case EvLinks:
		version := d.u32()
		n := d.count(lim.MaxLinks)
		if d.err == nil && !d.need(n*minLinkSize) {
			break
		}
		items := make([]Link, n)
		for i := range items {
			items[i] = d.link()
		}
		body = Links{Version: version, Items: items}
	case EvDialog:
		body = Dialog{ID: d.u32(), Kind: DialogKind(d.u8()), Text: d.str(), Default: d.str()}
	case EvSelect:
		id := d.u32()
		n := d.count(lim.MaxLinks)
		opts := make([]string, n)
		for i := range opts {
			opts[i] = d.str()
		}
		body = Select{ID: id, Options: opts, Selected: d.i32()}
	case EvClipboard:
		body = Clipboard{Text: d.str()}
	case EvConsole:
		body = Console{Level: ConsoleLevel(d.u8()), Text: d.str()}
	case EvCrash:
		body = Crash{Message: d.str()}
	case EvSelection:
		body = Selection{Text: d.str()}
	case EvFindResults:
		query := d.str()
		current, total := d.i32(), d.i32()
		n := d.count(lim.MaxLinks)
		if d.err == nil && !d.need(n*minRectSize) {
			break
		}
		rects := make([]Rect, n)
		for i := range rects {
			rects[i] = d.rect()
		}
		body = FindResults{Query: query, Current: current, Total: total, Rects: rects}
	case EvDownload:
		body = Download{URL: d.str(), Name: d.str()}
	case EvQuit:
		body = Quit{}
	case EvAutomationResult:
		body = AutomationResult{
			Op:   AutomationOp(d.u8()),
			Text: d.str(),
			Data: d.blob(lim.MaxImageBytes),
			Err:  d.str(),
		}
	case EvChrome:
		show := d.bool()
		w := d.i32()
		rows := d.i32()
		nc := d.count(lim.MaxCells)
		if d.err == nil && !d.need(nc*minCellSize) {
			break
		}
		header := make([]Cell, nc)
		for i := range header {
			header[i] = d.cell()
		}
		nf := d.count(lim.MaxCells)
		if d.err == nil && !d.need(nf*minCellSize) {
			break
		}
		footer := make([]Cell, nf)
		for i := range footer {
			footer[i] = d.cell()
		}
		c := Chrome{Show: show, Width: w, HeaderRows: rows, Header: header, Footer: footer}
		if d.err == nil {
			if err := c.Validate(lim); err != nil {
				return Event{}, nil, err
			}
		}
		body = c
	default:
		return Event{}, nil, fmt.Errorf("%w: unknown event kind %d", ErrInvalid, h.kind)
	}
	if d.err != nil {
		return Event{}, nil, d.err
	}
	if d.off != len(d.b) {
		return Event{}, nil, fmt.Errorf("%w: %d trailing event bytes", ErrInvalid, len(d.b)-d.off)
	}
	return Event{Session: h.session, Seq: h.seq, Body: body}, rest, nil
}

func beginMessage(dst []byte) []byte {
	var h [headerSize]byte
	return append(dst, h[:]...)
}

// finishMessage fills the reserved header in place. The body has already been
// appended to dst, so no second buffer or copy is needed.
func finishMessage(dst []byte, start int, class Class, kind Kind, session SessionID, seq uint64, lim Limits) ([]byte, error) {
	bodyLen := len(dst) - start - headerSize
	if bodyLen < 0 || bodyLen > lim.MaxMessageBytes {
		return dst[:start], fmt.Errorf("%w: body is %d bytes", ErrLimit, bodyLen)
	}
	h := dst[start : start+headerSize]
	binary.LittleEndian.PutUint32(h[0:4], wireMagic)
	h[4] = Version
	h[5] = byte(class)
	h[6] = byte(kind)
	h[7] = 0
	binary.LittleEndian.PutUint32(h[8:12], uint32(session))
	binary.LittleEndian.PutUint64(h[12:20], seq)
	binary.LittleEndian.PutUint32(h[20:24], uint32(bodyLen))
	return dst, nil
}

func readHeader(src []byte, want Class, lim Limits) (wireHeader, []byte, error) {
	if len(src) < headerSize {
		return wireHeader{}, nil, ErrTruncated
	}
	if binary.LittleEndian.Uint32(src[0:4]) != wireMagic {
		return wireHeader{}, nil, fmt.Errorf("%w: bad magic", ErrInvalid)
	}
	if src[4] != Version {
		return wireHeader{}, nil, fmt.Errorf("%w: version %d, want %d", ErrInvalid, src[4], Version)
	}
	if Class(src[5]) != want {
		return wireHeader{}, nil, fmt.Errorf("%w: class %d, want %d", ErrInvalid, src[5], want)
	}
	if src[7] != 0 {
		return wireHeader{}, nil, fmt.Errorf("%w: unknown flags", ErrInvalid)
	}
	length := int64(binary.LittleEndian.Uint32(src[20:24]))
	if length < 0 || length > int64(lim.MaxMessageBytes) {
		return wireHeader{}, nil, fmt.Errorf("%w: body is %d bytes", ErrLimit, length)
	}
	if length > int64(len(src)-headerSize) {
		return wireHeader{}, nil, ErrTruncated
	}
	return wireHeader{
		kind:    Kind(src[6]),
		session: SessionID(binary.LittleEndian.Uint32(src[8:12])),
		seq:     binary.LittleEndian.Uint64(src[12:20]),
		body:    src[headerSize : headerSize+length],
	}, src[headerSize+length:], nil
}

type encoder struct {
	b   []byte
	lim Limits
	err error
}

func (e *encoder) fail(err error) {
	if e.err == nil {
		e.err = err
	}
}

func (e *encoder) u8(v uint8) {
	if e.err == nil {
		e.b = append(e.b, v)
	}
}

func (e *encoder) u32(v uint32) {
	if e.err == nil {
		e.b = binary.LittleEndian.AppendUint32(e.b, v)
	}
}

func (e *encoder) u64(v uint64) {
	if e.err == nil {
		e.b = binary.LittleEndian.AppendUint64(e.b, v)
	}
}

func (e *encoder) i32(v int) {
	if int64(v) < minInt32 || int64(v) > maxInt32 {
		e.fail(fmt.Errorf("%w: integer %d out of range", ErrLimit, v))
		return
	}
	e.u32(uint32(int32(v)))
}

func (e *encoder) f64(v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		e.fail(fmt.Errorf("%w: non-finite float", ErrInvalid))
		return
	}
	e.u64(math.Float64bits(v))
}

func (e *encoder) bool(v bool) {
	if v {
		e.u8(1)
		return
	}
	e.u8(0)
}

func (e *encoder) count(n int) {
	if n < 0 || int64(n) > maxInt32 {
		e.fail(fmt.Errorf("%w: count %d", ErrLimit, n))
		return
	}
	e.u32(uint32(n))
}

func (e *encoder) str(s string) {
	if len(s) > e.lim.MaxTextBytes {
		e.fail(fmt.Errorf("%w: string is %d bytes", ErrLimit, len(s)))
		return
	}
	e.count(len(s))
	if e.err == nil {
		e.b = append(e.b, s...)
	}
}

func (e *encoder) blob(p []byte, max int) {
	if len(p) > max {
		e.fail(fmt.Errorf("%w: blob is %d bytes", ErrLimit, len(p)))
		return
	}
	e.count(len(p))
	if e.err == nil {
		e.b = append(e.b, p...)
	}
}

func (e *encoder) color(c Color) {
	e.u8(c.R)
	e.u8(c.G)
	e.u8(c.B)
	e.u8(c.A)
	var flags uint8
	if c.Set {
		flags = 1
	}
	e.u8(flags)
}

func (e *encoder) cell(c Cell) {
	e.u32(uint32(c.R))
	e.color(c.FG)
	e.color(c.BG)
	e.u8(c.Attrs)
	e.u8(c.Flags)
	e.u32(uint32(c.Run))
}

func (e *encoder) rect(r Rect) {
	e.i32(r.X)
	e.i32(r.Y)
	e.i32(r.W)
	e.i32(r.H)
}

func (e *encoder) run(r ScaledRun) {
	e.i32(r.X)
	e.i32(r.Y)
	e.i32(r.Cells)
	e.i32(r.Scale)
	e.color(r.FG)
	e.color(r.BG)
	e.u8(r.Attrs)
	e.str(r.Text)
}

func (e *encoder) link(l Link) {
	e.i32(l.Index)
	e.i32(l.X)
	e.i32(l.Y)
	e.i32(l.W)
	e.i32(l.H)
	e.str(l.Text)
	e.str(l.Href)
}

func (e *encoder) placement(p Placement) {
	e.str(p.Key)
	e.i32(p.X)
	e.i32(p.Y)
	e.i32(p.W)
	e.i32(p.H)
	e.i32(p.RowStart)
	e.i32(p.TotalRows)
	e.u8(uint8(p.Format))
	e.blob(p.Data, e.lim.MaxImageBytes)
}

type decoder struct {
	b   []byte
	off int
	lim Limits
	err error
}

func (d *decoder) fail(err error) {
	if d.err == nil {
		d.err = err
	}
}

func (d *decoder) u8() uint8 {
	if d.err != nil {
		return 0
	}
	if d.off+1 > len(d.b) {
		d.fail(ErrTruncated)
		return 0
	}
	v := d.b[d.off]
	d.off++
	return v
}

func (d *decoder) u32() uint32 {
	if d.err != nil {
		return 0
	}
	if d.off+4 > len(d.b) {
		d.fail(ErrTruncated)
		return 0
	}
	v := binary.LittleEndian.Uint32(d.b[d.off : d.off+4])
	d.off += 4
	return v
}

func (d *decoder) u64() uint64 {
	if d.err != nil {
		return 0
	}
	if d.off+8 > len(d.b) {
		d.fail(ErrTruncated)
		return 0
	}
	v := binary.LittleEndian.Uint64(d.b[d.off : d.off+8])
	d.off += 8
	return v
}

func (d *decoder) i32() int {
	return int(int32(d.u32()))
}

func (d *decoder) f64() float64 {
	v := math.Float64frombits(d.u64())
	if d.err == nil && (math.IsNaN(v) || math.IsInf(v, 0)) {
		d.fail(fmt.Errorf("%w: non-finite float", ErrInvalid))
	}
	return v
}

func (d *decoder) bool() bool {
	return d.u8() != 0
}

func (d *decoder) count(max int) int {
	n := int(d.u32())
	if d.err != nil {
		return 0
	}
	if n < 0 || n > max {
		d.fail(fmt.Errorf("%w: count %d > %d", ErrLimit, n, max))
		return 0
	}
	return n
}

func (d *decoder) need(n int) bool {
	if d.err != nil {
		return false
	}
	if n < 0 || n > len(d.b)-d.off {
		d.fail(ErrTruncated)
		return false
	}
	return true
}

func (d *decoder) str() string {
	n := int(d.u32())
	if d.err != nil {
		return ""
	}
	if n > d.lim.MaxTextBytes {
		d.fail(fmt.Errorf("%w: string is %d bytes", ErrLimit, n))
		return ""
	}
	if !d.need(n) {
		return ""
	}
	s := string(d.b[d.off : d.off+n])
	d.off += n
	return s
}

func (d *decoder) blob(max int) []byte {
	n := int(d.u32())
	if d.err != nil {
		return nil
	}
	if n > max {
		d.fail(fmt.Errorf("%w: blob is %d bytes", ErrLimit, n))
		return nil
	}
	if !d.need(n) {
		return nil
	}
	p := append([]byte(nil), d.b[d.off:d.off+n]...)
	d.off += n
	return p
}

func (d *decoder) color() Color {
	c := Color{R: d.u8(), G: d.u8(), B: d.u8(), A: d.u8()}
	flags := d.u8()
	if flags&^1 != 0 {
		d.fail(fmt.Errorf("%w: unknown color flags %#x", ErrInvalid, flags))
	}
	c.Set = flags&1 != 0
	return c
}

func (d *decoder) cell() Cell {
	c := Cell{}
	c.R = rune(d.u32())
	c.FG = d.color()
	c.BG = d.color()
	c.Attrs = d.u8()
	c.Flags = d.u8()
	run := d.u32()
	if run > 0xffff {
		d.fail(fmt.Errorf("%w: run index %d", ErrLimit, run))
	} else {
		c.Run = uint16(run)
	}
	return c
}

func (d *decoder) rect() Rect {
	return Rect{X: d.i32(), Y: d.i32(), W: d.i32(), H: d.i32()}
}

func (d *decoder) run() ScaledRun {
	return ScaledRun{
		X:     d.i32(),
		Y:     d.i32(),
		Cells: d.i32(),
		Scale: d.i32(),
		FG:    d.color(),
		BG:    d.color(),
		Attrs: d.u8(),
		Text:  d.str(),
	}
}

func (d *decoder) link() Link {
	return Link{
		Index: d.i32(),
		X:     d.i32(),
		Y:     d.i32(),
		W:     d.i32(),
		H:     d.i32(),
		Text:  d.str(),
		Href:  d.str(),
	}
}

func (d *decoder) placement() Placement {
	return Placement{
		Key:       d.str(),
		X:         d.i32(),
		Y:         d.i32(),
		W:         d.i32(),
		H:         d.i32(),
		RowStart:  d.i32(),
		TotalRows: d.i32(),
		Format:    PlacementFormat(d.u8()),
		Data:      d.blob(d.lim.MaxImageBytes),
	}
}
