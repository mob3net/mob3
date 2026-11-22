package frontend

import (
	"bytes"
	"strings"
	"testing"

	"browser/internal/protocol"
)

func chromeState() protocol.State {
	return protocol.State{
		Width: 80, Height: 24, HeaderRows: 2, FrontendChrome: true,
		URL: "https://example.com/", Title: "Example", Loading: false,
		FooterLeft: " Ready", FooterRight: "Graphics:blocks   JavaScript:on   Dark:off   Reader:off   MEM 42MB",
		FooterLevel: protocol.FooterNormal, ZoomLabel: " 100% ",
	}
}

func TestLayoutChromeHeaderAndFooter(t *testing.T) {
	st := chromeState()
	layout := LayoutChrome(st)
	if len(layout.Header) != st.Width*st.HeaderRows {
		t.Fatalf("header cells = %d", len(layout.Header))
	}
	if len(layout.Footer) != st.Width {
		t.Fatalf("footer cells = %d", len(layout.Footer))
	}
	var header, footer strings.Builder
	for _, c := range layout.Header {
		if c.R != 0 {
			header.WriteRune(c.R)
		}
	}
	for _, c := range layout.Footer {
		if c.R != 0 {
			footer.WriteRune(c.R)
		}
	}
	for _, want := range []string{"Example", "example.com", "100%"} {
		if !strings.Contains(header.String(), want) {
			t.Errorf("header %q does not contain %q", header.String(), want)
		}
	}
	for _, want := range []string{"Ready", "Graphics:blocks", ">_"} {
		if !strings.Contains(footer.String(), want) {
			t.Errorf("footer %q does not contain %q", footer.String(), want)
		}
	}
}

func TestLayoutChromeHits(t *testing.T) {
	hits := LayoutChrome(chromeState()).Hits
	want := []ChromeHitKind{ChromeHitBack, ChromeHitForward, ChromeHitReload, ChromeHitHome, ChromeHitURL, ChromeHitZoom, ChromeHitGraphics, ChromeHitJS, ChromeHitDark, ChromeHitReader, ChromeHitConsole}
	seen := map[ChromeHitKind]bool{}
	for _, h := range hits {
		seen[h.Kind] = true
	}
	for _, kind := range want {
		if !seen[kind] {
			t.Errorf("missing chrome hit %v", kind)
		}
	}
}

type recordSession struct{ cmds []protocol.Command }

func (s *recordSession) Send(c protocol.Command) error { s.cmds = append(s.cmds, c); return nil }
func (s *recordSession) Events() <-chan protocol.Event { return nil }
func (s *recordSession) Abort()                        {}
func (s *recordSession) Close() error                  { return nil }

func TestHandleChromeMouseSendsHeaderActions(t *testing.T) {
	cases := []struct {
		name string
		kind ChromeHitKind
		body any
	}{
		{"back", ChromeHitBack, protocol.App{Action: protocol.AppBack}},
		{"reload", ChromeHitReload, protocol.App{Action: protocol.AppReload}},
		{"graphics", ChromeHitGraphics, protocol.App{Action: protocol.AppToggleImages}},
		{"console", ChromeHitConsole, protocol.App{Action: protocol.AppToggleConsole}},
	}
	st := chromeState()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &recordSession{}
			loop := &SessionLoop{Session: s}
			loop.State.Page = st
			var hit ChromeHit
			for _, h := range LayoutChrome(st).Hits {
				if h.Kind == tc.kind {
					hit = h
					break
				}
			}
			if hit.W == 0 {
				t.Fatalf("no hit %v", tc.kind)
			}
			if !loop.handleChromeMouse(MouseEvent{X: hit.X + 1, Y: hit.Y + 1, Press: true}) {
				t.Fatalf("hit %v was not consumed", tc.kind)
			}
			if len(s.cmds) != 1 {
				t.Fatalf("commands = %d, want 1", len(s.cmds))
			}
			if s.cmds[0].Body != tc.body {
				t.Fatalf("command = %#v, want %#v", s.cmds[0].Body, tc.body)
			}
		})
	}
}

func TestChromeURLHitStartsPrompt(t *testing.T) {
	s := &recordSession{}
	loop := &SessionLoop{Session: s}
	loop.State.Page = chromeState()
	var hit ChromeHit
	for _, h := range LayoutChrome(loop.State.Page).Hits {
		if h.Kind == ChromeHitURL {
			hit = h
			break
		}
	}
	if hit.W == 0 {
		t.Fatal("no URL hit")
	}
	if !loop.handleChromeMouse(MouseEvent{X: hit.X + 1, Y: hit.Y + 1, Press: true}) {
		t.Fatal("URL click not consumed")
	}
	if !loop.promptActive || loop.promptText != loop.State.Page.URL {
		t.Fatalf("prompt = %v %q", loop.promptActive, loop.promptText)
	}
	if len(s.cmds) != 0 {
		t.Fatalf("commands = %#v", s.cmds)
	}
}

func TestHandleChromeMouseLeavesContentAndPromptAlone(t *testing.T) {
	st := chromeState()
	st.PromptActive = true
	s := &recordSession{}
	loop := &SessionLoop{Session: s}
	loop.State.Page = st
	if loop.handleChromeMouse(MouseEvent{X: 1, Y: 1, Press: true}) {
		t.Fatal("prompt header mouse was consumed")
	}
	st.PromptActive = false
	st.FrontendChrome = false
	loop.State.Page = st
	if loop.handleChromeMouse(MouseEvent{X: 1, Y: 1, Press: true}) {
		t.Fatal("non-frontend chrome mouse was consumed")
	}
	loop.State.Page = chromeState()
	if loop.handleChromeMouse(MouseEvent{X: 10, Y: 10, Press: true}) {
		t.Fatal("content mouse was consumed")
	}
}

func TestPresenterComposesFrontendChrome(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, false)
	st := protocol.State{
		Width: 20, Height: 3, HeaderRows: 1, FrontendChrome: true,
		URL: "x", FooterLeft: " Ready", FooterRight: "MEM 1MB",
	}
	p.SetState(st)
	content := protocol.Frame{W: 20, H: 1, Cells: make([]protocol.Cell, 20)}
	for i := range content.Cells {
		content.Cells[i].R = 'p'
	}
	if err := p.Present(content, 0, false); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"Ready", "MEM 1MB", "pppp"} {
		if !strings.Contains(got, want) {
			t.Errorf("presented output %q does not contain %q", got, want)
		}
	}
	if strings.Index(got, "Ready") < strings.Index(got, "pppp") {
		t.Error("footer was not composed below the content")
	}
}

func TestDrawScrollBarFrame(t *testing.T) {
	f := protocol.Frame{W: 20, H: 10, Cells: make([]protocol.Cell, 200)}
	for i := range f.Cells {
		f.Cells[i].R = 'x'
	}
	DrawScrollBarFrame(&f, protocol.State{FrontendChrome: true, MaxScroll: 100, Scroll: 50})
	thumb := rgb(0x7a, 0x8a, 0x9a)
	track := rgb(0xdd, 0xe3, 0xea)
	foundThumb, foundTrack := false, false
	for y := 0; y < f.H; y++ {
		c := f.Cells[y*f.W+f.W-1]
		if c.R != ' ' {
			t.Fatalf("scrollbar row %d kept glyph %q", y, c.R)
		}
		switch c.BG {
		case thumb:
			foundThumb = true
		case track:
			foundTrack = true
		default:
			t.Fatalf("scrollbar row %d has background %+v", y, c.BG)
		}
	}
	if !foundThumb || !foundTrack {
		t.Fatalf("thumb=%v track=%v", foundThumb, foundTrack)
	}
}

func TestFindBarLayout(t *testing.T) {
	st := chromeState()
	st.FindActive = true
	st.FindText = "alpha"
	st.FindCursor = 5
	st.FindCurrent = 1
	st.FindTotal = 2
	layout := LayoutChrome(st)
	var footer strings.Builder
	for _, c := range layout.Footer {
		if c.R != 0 {
			footer.WriteRune(c.R)
		}
	}
	for _, want := range []string{"Find:", "alpha", "1/2"} {
		if !strings.Contains(footer.String(), want) {
			t.Errorf("find bar %q does not contain %q", footer.String(), want)
		}
	}
}

func TestChromeZoomHitStartsMenu(t *testing.T) {
	s := &recordSession{}
	loop := &SessionLoop{Session: s}
	loop.State.Page = chromeState()
	var hit ChromeHit
	for _, h := range LayoutChrome(loop.State.Page).Hits {
		if h.Kind == ChromeHitZoom {
			hit = h
			break
		}
	}
	if hit.W == 0 {
		t.Fatal("no zoom hit")
	}
	if !loop.handleChromeMouse(MouseEvent{X: hit.X + 1, Y: hit.Y + 1, Press: true}) {
		t.Fatal("zoom click not consumed")
	}
	if loop.menuKind != protocol.MenuZoom {
		t.Fatalf("menu = %v", loop.menuKind)
	}
}

func TestMenuLayoutAndDraw(t *testing.T) {
	st := chromeState()
	st.Menu = protocol.MenuView
	st.MenuIndex = 0
	st.ViewFlags = protocol.ViewFlagFixed | protocol.ViewFlagSticky
	layout := LayoutMenu(st)
	if len(layout.Rows) != 3 || layout.BoxW <= 0 {
		t.Fatalf("layout = %+v", layout)
	}
	if layout.Rows[0].Marker != "☑ " || layout.Rows[2].Marker != "☐ " {
		t.Fatalf("markers = %q %q", layout.Rows[0].Marker, layout.Rows[2].Marker)
	}
	c := newCanvas(st.Width, st.Height)
	DrawMenu(c, st)
	var text strings.Builder
	for _, cell := range c.cells {
		if cell.R != 0 {
			text.WriteRune(cell.R)
		}
	}
	for _, want := range []string{"View options", "Fixed elements", "Background images", "Space toggle"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("menu %q does not contain %q", text.String(), want)
		}
	}
}

func TestDialogLayoutAndDraw(t *testing.T) {
	st := chromeState()
	d := &DialogView{Kind: protocol.DialogPrompt, Text: "name?", Value: "alice", Cursor: 5, SelectAll: true}
	layout := LayoutDialog(st, d)
	if layout.BoxW <= 0 || layout.BoxH <= 0 {
		t.Fatalf("layout = %+v", layout)
	}
	c := newCanvas(st.Width, st.Height)
	DrawDialog(c, st, d)
	var text strings.Builder
	for _, cell := range c.cells {
		if cell.R != 0 {
			text.WriteRune(cell.R)
		}
	}
	for _, want := range []string{"JavaScript prompt", "name?", "alice", "Enter: OK"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("dialog %q does not contain %q", text.String(), want)
		}
	}
}
