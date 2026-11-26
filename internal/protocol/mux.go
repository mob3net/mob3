package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

// Portable stream framing
//
// A Mux carries typed frames over one bidirectional byte stream. It is the
// transport envelope for a page session, so the same protocol works over an
// in-process pipe, a pair of stdin/stdout pipes, or a child process. The page
// command and event codecs remain unchanged inside a MuxCommand or MuxEvent
// frame; later host RPC traffic uses the other kinds without changing the
// envelope.
//
// Wire format (little endian):
//
//	0  magic    uint32  "M3MX"
//	4  version  uint8
//	5  kind     uint8
//	6  flags    uint8   reserved, must be zero
//	7  reserved uint8   must be zero
//	8  id       uint64  request or message id; zero when unused
//	16 length   uint32  payload bytes
//	20 payload
const (
	muxHeaderSize = 20
	muxMagic      = 0x584d334d // bytes 'M','3','M','X'
)

// MuxKind identifies one class of frame on a Mux stream.
type MuxKind uint8

const (
	// MuxCommand carries one encoded Command.
	MuxCommand MuxKind = iota + 1
	// MuxEvent carries one encoded Event.
	MuxEvent
	// MuxHostCall and MuxHostResult carry host-broker requests.
	MuxHostCall
	MuxHostResult
	// MuxStorageCall and MuxStorageResult carry shared storage-broker calls.
	MuxStorageCall
	MuxStorageResult
	// MuxPing and MuxPong are liveness frames for a watchdog.
	MuxPing
	MuxPong
	// MuxClose asks the peer to stop the session without an encoded command.
	MuxClose
)

func (k MuxKind) valid() bool { return k >= MuxCommand && k <= MuxClose }

// MuxFrame is one framed message. Body is owned by the caller after ReadFrame
// returns; the next read does not reuse it. WriteFrame does not retain Body.
type MuxFrame struct {
	Kind MuxKind
	ID   uint64
	Body []byte
}

// Mux reads and writes typed frames over one stream. One goroutine may read
// while another writes; writes are serialized internally.
type Mux struct {
	r   io.Reader
	w   io.Writer
	lim Limits

	writeMu sync.Mutex
	buf     []byte
}

// NewMux creates a Mux over separate reader and writer streams. Separate
// streams let a child process use its stdin and stdout with no extra file
// descriptors or platform-specific sockets.
func NewMux(r io.Reader, w io.Writer, lim Limits) *Mux {
	return &Mux{r: r, w: w, lim: lim.normalized()}
}

// NewMuxReadWriter creates a Mux over one bidirectional stream.
func NewMuxReadWriter(rw io.ReadWriter, lim Limits) *Mux {
	return NewMux(rw, rw, lim)
}

// ReadFrame reads one frame. It returns io.EOF when the stream ends cleanly.
func (m *Mux) ReadFrame() (MuxFrame, error) {
	var h [muxHeaderSize]byte
	if _, err := io.ReadFull(m.r, h[:]); err != nil {
		return MuxFrame{}, err
	}
	if binary.LittleEndian.Uint32(h[0:4]) != muxMagic {
		return MuxFrame{}, fmt.Errorf("%w: bad mux magic", ErrInvalid)
	}
	if h[4] != Version {
		return MuxFrame{}, fmt.Errorf("%w: mux version %d, want %d", ErrInvalid, h[4], Version)
	}
	if h[6] != 0 || h[7] != 0 {
		return MuxFrame{}, fmt.Errorf("%w: unknown mux flags", ErrInvalid)
	}
	kind := MuxKind(h[5])
	if !kind.valid() {
		return MuxFrame{}, fmt.Errorf("%w: mux kind %d", ErrInvalid, kind)
	}
	length := int64(binary.LittleEndian.Uint32(h[16:20]))
	if length < 0 || length > int64(m.lim.MaxMessageBytes) {
		return MuxFrame{}, fmt.Errorf("%w: mux payload is %d bytes", ErrLimit, length)
	}
	body := make([]byte, int(length))
	if _, err := io.ReadFull(m.r, body); err != nil {
		return MuxFrame{}, err
	}
	return MuxFrame{
		Kind: kind,
		ID:   binary.LittleEndian.Uint64(h[8:16]),
		Body: body,
	}, nil
}

// WriteFrame writes one framed message. The payload is bounded by MaxMessageBytes.
func (m *Mux) WriteFrame(kind MuxKind, id uint64, body []byte) error {
	if !kind.valid() {
		return fmt.Errorf("%w: mux kind %d", ErrInvalid, kind)
	}
	if len(body) > m.lim.MaxMessageBytes {
		return fmt.Errorf("%w: mux payload is %d bytes", ErrLimit, len(body))
	}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	return m.writeFrameLocked(kind, id, body)
}

// WriteCommand encodes and writes one command frame.
func (m *Mux) WriteCommand(c Command) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	var err error
	m.buf, err = AppendCommand(m.buf[:0], c, m.lim)
	if err != nil {
		return err
	}
	return m.writeFrameLocked(MuxCommand, 0, m.buf)
}

// WriteEvent encodes and writes one event frame.
func (m *Mux) WriteEvent(ev Event) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	var err error
	m.buf, err = AppendEvent(m.buf[:0], ev, m.lim)
	if err != nil {
		return err
	}
	return m.writeFrameLocked(MuxEvent, 0, m.buf)
}

// ReadCommand reads one command frame. Any other frame kind is a protocol
// error at the command loop.
func (m *Mux) ReadCommand() (Command, error) {
	f, err := m.ReadFrame()
	if err != nil {
		return Command{}, err
	}
	if f.Kind != MuxCommand {
		return Command{}, fmt.Errorf("%w: mux kind %d, want command", ErrInvalid, f.Kind)
	}
	c, rest, err := DecodeCommand(f.Body, m.lim)
	if err != nil {
		return Command{}, err
	}
	if len(rest) != 0 {
		return Command{}, fmt.Errorf("%w: %d trailing command bytes", ErrInvalid, len(rest))
	}
	return c, nil
}

// ReadEvent reads one event frame. Any other frame kind is a protocol error at
// the event loop.
func (m *Mux) ReadEvent() (Event, error) {
	f, err := m.ReadFrame()
	if err != nil {
		return Event{}, err
	}
	if f.Kind != MuxEvent {
		return Event{}, fmt.Errorf("%w: mux kind %d, want event", ErrInvalid, f.Kind)
	}
	ev, rest, err := DecodeEvent(f.Body, m.lim)
	if err != nil {
		return Event{}, err
	}
	if len(rest) != 0 {
		return Event{}, fmt.Errorf("%w: %d trailing event bytes", ErrInvalid, len(rest))
	}
	return ev, nil
}

func (m *Mux) writeFrameLocked(kind MuxKind, id uint64, body []byte) error {
	if !kind.valid() {
		return fmt.Errorf("%w: mux kind %d", ErrInvalid, kind)
	}
	if len(body) > m.lim.MaxMessageBytes {
		return fmt.Errorf("%w: mux payload is %d bytes", ErrLimit, len(body))
	}
	var h [muxHeaderSize]byte
	binary.LittleEndian.PutUint32(h[0:4], muxMagic)
	h[4] = Version
	h[5] = byte(kind)
	binary.LittleEndian.PutUint64(h[8:16], id)
	binary.LittleEndian.PutUint32(h[16:20], uint32(len(body)))
	if err := writeAll(m.w, h[:]); err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	return writeAll(m.w, body)
}

// writeAll writes the whole buffer, rejecting a writer that accepts no bytes.
func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}
