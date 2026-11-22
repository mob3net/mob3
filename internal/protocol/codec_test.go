package protocol

import (
	"bytes"
	"math"
	"reflect"
	"strings"
	"testing"
)

func testFrame() Frame {
	f := Frame{
		Version:    7,
		W:          4,
		H:          3,
		Cells:      make([]Cell, 12),
		Runs:       []ScaledRun{},
		Placements: []Placement{},
	}
	for i := range f.Cells {
		f.Cells[i] = Cell{R: ' ', FG: Color{R: 1, G: 2, B: 3, A: 255, Set: true}, BG: Color{Set: true}}
	}
	return f
}

func testCommands() []Command {
	return []Command{
		{Session: 1, Seq: 1, Body: Init{Width: 100, Height: 30, CellW: 8, CellH: 16, Zoom: 1.25, Caps: CapColor | CapKittyText, Flags: InitNoJS}},
		{Session: 2, Seq: 2, Body: Navigate{URL: "https://example.test/a?b=c", Push: true}},
		{Session: 3, Seq: 3, Body: Resize{Width: 80, Height: 24, CellW: 9, CellH: 18}},
		{Session: 4, Seq: 4, Body: Scroll{Delta: -3, To: 42, HasTo: true, Top: false, Bottom: true}},
		{Session: 5, Seq: 5, Body: Key{Token: "ctrl-c"}},
		{Session: 6, Seq: 6, Body: Mouse{X: 3, Y: 4, Button: 0, Wheel: -1, Press: true, Motion: true, Shift: true}},
		{Session: 7, Seq: 7, Body: App{Action: AppToggleDark, Arg: 2}},
		{Session: 11, Seq: 11, Body: SelectResult{ID: 9, Index: 2, Cancel: true}},
		{Session: 12, Seq: 12, Body: ModalClose{Kind: ModalDownload, Copied: true, Chars: 12}},
		{Session: 13, Seq: 13, Body: HintResult{Target: 2}},
		{Session: 8, Seq: 8, Body: Find{Query: "needle", Next: true, Prev: true, Close: true}},
		{Session: 9, Seq: 9, Body: Abort{Reason: "escape"}},
		{Session: 10, Seq: 10, Body: Shutdown{}},
		{Session: 11, Seq: 11, Body: DialogResult{ID: 77, OK: true, Text: "yes"}},
		{Session: 12, Seq: 12, Body: ClientError{Message: "invalid engine event: test"}},
		{Session: 13, Seq: 13, Body: Automation{Op: AutomationRunCommand, Name: "snapshot", Args: []byte(`{"x":1}`)}},
	}
}

func testEvents() []Event {
	f := testFrame()
	f.Runs = []ScaledRun{{X: 0, Y: 0, Cells: 4, Scale: 2, Text: "Hi", FG: Color{Set: true}, BG: Color{Set: true}, Attrs: AttrBold}}
	f.Placements = []Placement{{Key: "k", X: 0, Y: 1, W: 2, H: 2, RowStart: 0, TotalRows: 2, Format: PlacementPNG, Data: []byte{1, 2, 3}}}
	return []Event{
		{Session: 1, Seq: 1, Body: f},
		{Session: 2, Seq: 2, Body: State{
			URL: "https://example.test/", Title: "T", Status: "ok", Loading: true,
			Progress: 50, Scroll: 3, MaxScroll: 9, Kind: PageHTML, Flags: StateDark,
			Width: 100, Height: 30, HeaderRows: 2, FrontendChrome: true,
			FooterLeft: " Ready", FooterRight: "Graphics:blocks   MEM 42MB",
			FooterLevel: FooterWarning, FooterStatusHit: true,
			PDFHint: "(d -> Download f.pdf)", PDFHintX: 3,
			PromptActive: true, PromptText: "example.com", PromptCursor: 4, PromptSelectAll: true,
			ZoomLabel: " 150% ", FindActive: true, QuitPending: true,
		}},
		{Session: 3, Seq: 3, Body: Links{Version: 9, Items: []Link{{Index: 1, X: 0, Y: 1, W: 4, H: 1, Text: "go", Href: "/x"}}}},
		{Session: 4, Seq: 4, Body: Dialog{ID: 5, Kind: DialogPrompt, Text: "name?", Default: "x"}},
		{Session: 5, Seq: 5, Body: Clipboard{Text: "copied"}},
		{Session: 6, Seq: 6, Body: Console{Level: ConsoleError, Text: "boom"}},
		{Session: 7, Seq: 7, Body: Crash{Message: "contained"}},
		{Session: 8, Seq: 8, Body: Selection{Text: "selected"}},
		{Session: 9, Seq: 9, Body: FindResults{Query: "q", Current: 2, Total: 3, Rects: []Rect{{X: 1, Y: 2, W: 3, H: 1}}}},
		{Session: 10, Seq: 10, Body: Download{URL: "https://example.test/f.bin", Name: "f.bin", Command: "curl -L -o f.bin -- https://example.test/f.bin"}},
		{Session: 11, Seq: 11, Body: Select{ID: 9, Options: []string{"One", "Two"}, Selected: 1}},
		{Session: 12, Seq: 12, Body: Notice{Title: "Notice", Message: "Body"}},
		{Session: 13, Seq: 13, Body: Hints{Ask: true, Prefix: "d", Items: []HintItem{{
			Target: 2, Label: "d", X: 1, Y: 2, W: 3, H: 1,
			Chars: []HintChar{{R: 'D', X: 1, Y: 2}}, Link: true, Href: "/next",
		}}}},
		{Session: 14, Seq: 14, Body: ConsoleView{
			Width: 80, Height: 24, Pane: 0, Tabs: []string{" JS logs (1) ", " Requests (0) "},
			Lines: []string{"[log] hi"}, Input: "1+1", Cursor: 3, Edit: true,
			Hint: "JavaScript input", Footer: " help ", Quit: false,
		}},
		{Session: 11, Seq: 11, Body: Quit{}},
		{Session: 12, Seq: 12, Body: AutomationResult{Op: AutomationSnapshot, Text: "page"}},
		{Session: 13, Seq: 13, Body: Chrome{Show: true, Width: 3, HeaderRows: 1, Header: []Cell{{R: 'H'}, {R: 'i'}, {R: '!'}}, Footer: []Cell{{R: 'F'}, {R: 'o'}, {R: 'o'}}}},
	}
}

func TestCommandRoundTrip(t *testing.T) {
	for _, in := range testCommands() {
		msg, err := AppendCommand(nil, in, DefaultLimits)
		if err != nil {
			t.Fatalf("%T: encode: %v", in.Body, err)
		}
		out, rest, err := DecodeCommand(msg, DefaultLimits)
		if err != nil {
			t.Fatalf("%T: decode: %v", in.Body, err)
		}
		if len(rest) != 0 {
			t.Fatalf("%T: %d trailing bytes", in.Body, len(rest))
		}
		if !reflect.DeepEqual(in, out) {
			t.Fatalf("%T round trip:\n in: %#v\nout: %#v", in.Body, in, out)
		}
	}
}

func TestEventRoundTrip(t *testing.T) {
	for _, in := range testEvents() {
		msg, err := AppendEvent(nil, in, DefaultLimits)
		if err != nil {
			t.Fatalf("%T: encode: %v", in.Body, err)
		}
		out, rest, err := DecodeEvent(msg, DefaultLimits)
		if err != nil {
			t.Fatalf("%T: decode: %v", in.Body, err)
		}
		if len(rest) != 0 {
			t.Fatalf("%T: %d trailing bytes", in.Body, len(rest))
		}
		if !reflect.DeepEqual(in, out) {
			t.Fatalf("%T round trip:\n in: %#v\nout: %#v", in.Body, in, out)
		}
	}
}

func TestStreamAppendAndTail(t *testing.T) {
	prefix := []byte{9, 8, 7}
	buf, err := AppendCommand(prefix, testCommands()[0], DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf[:len(prefix)], prefix) {
		t.Fatal("append did not preserve the prefix")
	}
	buf, err = AppendCommand(buf, testCommands()[1], DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	first, rest, err := DecodeCommand(buf[len(prefix):], DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if first.Seq != 1 {
		t.Fatalf("first seq = %d", first.Seq)
	}
	second, rest, err := DecodeCommand(rest, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if second.Seq != 2 {
		t.Fatalf("second seq = %d", second.Seq)
	}
	if len(rest) != 0 {
		t.Fatalf("%d trailing bytes", len(rest))
	}
}

func TestTruncatedFrameNeverPanics(t *testing.T) {
	msg, err := AppendEvent(nil, testEvents()[0], DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(msg); i++ {
		if _, _, err := DecodeEvent(msg[:i], DefaultLimits); err == nil {
			t.Fatalf("prefix %d decoded without error", i)
		}
	}
}

func TestWrongClassRejected(t *testing.T) {
	msg, err := AppendCommand(nil, testCommands()[0], DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := DecodeEvent(msg, DefaultLimits); err == nil {
		t.Fatal("command decoded as an event")
	}
}

func TestMessageLimitEnforced(t *testing.T) {
	lim := DefaultLimits
	lim.MaxMessageBytes = 16
	if _, err := AppendCommand(nil, testCommands()[0], lim); err == nil {
		t.Fatal("oversized command encoded")
	}
	// A header claiming a large body is rejected before reading it.
	msg, err := AppendCommand(nil, testCommands()[0], DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := DecodeCommand(msg, lim); err == nil {
		t.Fatal("oversized header accepted")
	}
}

func TestStringLimitEnforced(t *testing.T) {
	lim := DefaultLimits
	lim.MaxTextBytes = 4
	if _, err := AppendCommand(nil, Command{Body: Key{Token: "longer"}}, lim); err == nil {
		t.Fatal("oversized string encoded")
	}
}

func TestNonFiniteZoomRejected(t *testing.T) {
	for _, z := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := AppendCommand(nil, Command{Body: Init{Zoom: z}}, DefaultLimits); err == nil {
			t.Fatalf("zoom %v encoded", z)
		}
	}
}

func TestFrameValidation(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Frame)
	}{
		{"cell count", func(f *Frame) { f.Cells = f.Cells[:len(f.Cells)-1] }},
		{"escape rune", func(f *Frame) { f.Cells[0].R = '\x1b' }},
		{"run bounds", func(f *Frame) {
			f.Runs = []ScaledRun{{X: 3, Y: 0, Cells: 4, Scale: 2, Text: "x"}}
		}},
		{"run index", func(f *Frame) { f.Cells[0].Run = 1 }},
		{"run control text", func(f *Frame) {
			f.Runs = []ScaledRun{{X: 0, Y: 0, Cells: 4, Scale: 2, Text: "\x1b[2J"}}
		}},
		{"placement bounds", func(f *Frame) {
			f.Placements = []Placement{{X: 3, Y: 2, W: 2, H: 1, Format: PlacementPNG, Data: []byte{1}}}
		}},
		{"placement format", func(f *Frame) {
			f.Placements = []Placement{{X: 0, Y: 0, W: 1, H: 1, Format: 99, Data: []byte{1}}}
		}},
		{"placement data", func(f *Frame) {
			f.Placements = []Placement{{X: 0, Y: 0, W: 1, H: 1, Format: PlacementPNG}}
		}},
	}
	for _, tc := range cases {
		f := testFrame()
		tc.edit(&f)
		if err := f.Validate(DefaultLimits); err == nil {
			t.Fatalf("%s: frame validated", tc.name)
		}
	}
}

func TestFrameOversizeCellsRejected(t *testing.T) {
	lim := DefaultLimits
	lim.MaxCells = 4
	f := testFrame()
	if err := f.Validate(lim); err == nil {
		t.Fatal("oversized frame validated")
	}
}

func TestSafeRune(t *testing.T) {
	for _, r := range []rune{0, ' ', 'a', 'é', '世', '▀'} {
		if !SafeRune(r) {
			t.Fatalf("%U rejected", r)
		}
	}
	for _, r := range []rune{'\x1b', '\n', '\x7f', 0x85, 0xd800, 0x110000} {
		if SafeRune(r) {
			t.Fatalf("%U accepted", r)
		}
	}
}

func TestSafeText(t *testing.T) {
	for _, s := range []string{"", "plain", "héllo 世界", "▀▄"} {
		if !SafeText(s) {
			t.Fatalf("%q rejected", s)
		}
	}
	for _, s := range []string{"a\x1bb", "a\nb", "a\x7fb", "a\xc2\x85b"} {
		if SafeText(s) {
			t.Fatalf("%q accepted", s)
		}
	}
	if SafeText("\xff") {
		t.Fatal("invalid UTF-8 accepted")
	}
}

func TestUnknownKindRejected(t *testing.T) {
	msg, err := AppendCommand(nil, testCommands()[0], DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	msg[6] = 250 // kind
	if _, _, err := DecodeCommand(msg, DefaultLimits); err == nil {
		t.Fatal("unknown kind decoded")
	}
}

func TestTrailingBodyRejected(t *testing.T) {
	msg, err := AppendCommand(nil, testCommands()[0], DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	// Grow the declared body and append one byte, producing a well-framed
	// message with an extra body byte.
	msg = append(msg, 0)
	msg[20]++
	if _, _, err := DecodeCommand(msg, DefaultLimits); err == nil {
		t.Fatal("trailing body byte accepted")
	}
}

func TestStateTextIsCapable(t *testing.T) {
	// Long status strings are legal up to the text cap; the front end sanitizes.
	s := State{Status: strings.Repeat("x", 128)}
	if _, err := AppendEvent(nil, Event{Body: s}, DefaultLimits); err != nil {
		t.Fatal(err)
	}
}
