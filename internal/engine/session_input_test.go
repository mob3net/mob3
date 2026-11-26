package engine

import (
	"strings"
	"testing"

	"browser/internal/movekeys"
	"browser/internal/protocol"
)

type openTabSink struct {
	url           string
	previewURL    string
	previewTarget int
}

func (s *openTabSink) Send(ev protocol.Event) error {
	switch v := ev.Body.(type) {
	case protocol.OpenTab:
		s.url = v.URL
	case protocol.LinkPreview:
		s.previewURL, s.previewTarget = v.URL, v.Target
	}
	return nil
}

func TestHalfPageAliases(t *testing.T) {
	b := newTestSession(80, 24)
	b.Layout = &Layout{Height: 100}
	b.HandleKey("d")
	if b.Scroll <= 0 {
		t.Fatalf("d did not scroll down: %d", b.Scroll)
	}
	down := b.Scroll
	b.HandleKey("u")
	if b.Scroll >= down || b.Scroll < 0 {
		t.Fatalf("u did not scroll up: %d -> %d", down, b.Scroll)
	}
}

func TestPageMovementAliases(t *testing.T) {
	b := newTestSession(80, 24)
	b.Layout = &Layout{Height: 100}
	rows := b.viewportHeight()
	half := rows / 2
	page := max(rows-1, 0)
	maxScroll := b.maxScroll()
	cases := []struct {
		key  string
		want int
	}{
		{"j", 51}, {"down", 51}, {"ctrl-n", 51},
		{"k", 49}, {"up", 49}, {"ctrl-p", 49},
		{"d", 50 + half}, {"ctrl-d", 50 + half},
		{"u", 50 - half}, {"ctrl-u", 50 - half},
		{" ", 50 + page}, {"space", 50 + page}, {"pgdn", 50 + page}, {"ctrl-v", 50 + page},
		{"b", 50 - page}, {"pgup", 50 - page},
		{"g", 0}, {"home", 0},
		{"G", maxScroll}, {"end", maxScroll},
	}
	for _, c := range cases {
		b.Scroll = 50
		b.HandleKey(c.key)
		if b.Scroll != c.want {
			t.Errorf("%q scrolled to %d, want %d", c.key, b.Scroll, c.want)
		}
	}
}

func TestWheelScrollDirection(t *testing.T) {
	b := newTestSession(80, 24)
	b.Layout = &Layout{Height: 100}
	b.HandleMouse(MouseEvent{X: 5, Y: 5, Wheel: 1})
	if b.Scroll != movekeys.WheelLines {
		t.Fatalf("wheel down scrolled to %d, want %d", b.Scroll, movekeys.WheelLines)
	}
	b.HandleMouse(MouseEvent{X: 5, Y: 5, Wheel: -1})
	if b.Scroll != 0 {
		t.Fatalf("wheel up scrolled to %d, want 0", b.Scroll)
	}
}

func TestMiddleClickRequestsBackgroundTab(t *testing.T) {
	doc := ParseHTML(`<p><a href="/next">Next page</a></p>`)
	u := mustParse(t, "https://example.com/start/")
	b := newTestSession(40, 10)
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.URL = u
	b.Layout = BuildLayout(doc, nil, b.Width)
	var frag *Frag
	for i := range b.Layout.Frags {
		if b.Layout.Frags[i].Kind == FragText && strings.Contains(b.Layout.Frags[i].Text, "Next") {
			frag = &b.Layout.Frags[i]
			break
		}
	}
	if frag == nil {
		t.Fatal("link fragment not found")
	}
	sink := &openTabSink{}
	b.sessionOut = sink
	top, _ := b.viewport()
	b.HandleMouse(MouseEvent{Button: MouseButtonMiddle, X: frag.X + 1, Y: top + frag.Y + 1, Press: true})
	if sink.url != "https://example.com/next" {
		t.Fatalf("opened URL = %q", sink.url)
	}
	if b.URL != u {
		t.Fatal("middle click navigated the current tab")
	}
}

func TestEscapeShowsExitHint(t *testing.T) {
	b := newTestSession(80, 10)
	b.Layout = &Layout{Height: 10}
	b.HandleKey("esc")
	if b.status != exitHint {
		t.Fatalf("status = %q, want %q", b.status, exitHint)
	}
	left := b.footerModel().left
	for _, want := range []string{"q Quit", "? Help"} {
		if !strings.Contains(left, want) {
			t.Errorf("footer = %q, want %q", left, want)
		}
	}
}

func TestEscapeFromInputShowsExitHint(t *testing.T) {
	doc := ParseHTML(`<html><body><input name="q"></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := newTestSession(80, 10)
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.rebuild()
	q := inputByName(doc, "q")
	if q == nil {
		t.Fatal("input not found")
	}
	b.setInputFocus(q)
	b.HandleKey("esc")
	if b.Forms.InputFocus != nil {
		t.Fatal("escape did not leave the input")
	}
	if b.status != exitHint {
		t.Fatalf("status = %q, want %q", b.status, exitHint)
	}
}

func TestParseSGRMouse(t *testing.T) {
	cases := []struct {
		params  string
		release bool
		want    MouseEvent
		ok      bool
	}{
		{"<0;10;5", false, MouseEvent{Button: 0, X: 10, Y: 5, Press: true}, true},
		{"<0;10;5", true, MouseEvent{Button: 0, X: 10, Y: 5, Press: false}, true},
		{"<2;1;1", false, MouseEvent{Button: 2, X: 1, Y: 1, Press: true}, true},
		{"<8;1;1", false, MouseEvent{Button: 8, X: 1, Y: 1, Press: true}, true},
		{"<9;1;1", false, MouseEvent{Button: 9, X: 1, Y: 1, Press: true}, true},
		{"<128;1;1", false, MouseEvent{Button: 8, X: 1, Y: 1, Press: true}, true},
		{"<129;1;1", false, MouseEvent{Button: 9, X: 1, Y: 1, Press: true}, true},
		{"<136;1;1", false, MouseEvent{Button: 8, X: 1, Y: 1, Press: true}, true},
		{"<64;3;4", false, MouseEvent{Button: 0, X: 3, Y: 4, Press: true, Wheel: -1}, true},
		{"<65;3;4", false, MouseEvent{Button: 1, X: 3, Y: 4, Press: true, Wheel: 1}, true},
		{"<32;3;4", false, MouseEvent{Button: 0, X: 3, Y: 4, Press: true, Motion: true}, true},
		{"bad", false, MouseEvent{}, false},
	}
	for _, c := range cases {
		got, ok := parseSGRMouse(c.params, c.release)
		if ok != c.ok || got != c.want {
			t.Errorf("parseSGRMouse(%q,%v) = %+v,%v want %+v,%v", c.params, c.release, got, ok, c.want, c.ok)
		}
	}
}

func TestParseX10Mouse(t *testing.T) {
	ev, ok := parseX10Mouse(32, 32+10, 32+5)
	if !ok || ev != (MouseEvent{Button: 0, X: 10, Y: 5, Press: true}) {
		t.Fatalf("left press = %+v,%v", ev, ok)
	}
	ev, ok = parseX10Mouse(35, 32+2, 32+3)
	if !ok || ev != (MouseEvent{Button: 0, X: 2, Y: 3, Press: false}) {
		t.Fatalf("release = %+v,%v", ev, ok)
	}
	ev, ok = parseX10Mouse(32+64, 32+1, 32+1)
	if !ok || ev.Wheel != -1 {
		t.Fatalf("wheel up = %+v,%v", ev, ok)
	}
	ev, ok = parseX10Mouse(32+8, 32+1, 32+1)
	if !ok || ev != (MouseEvent{Button: MouseButtonBack, X: 1, Y: 1, Press: true}) {
		t.Fatalf("back button = %+v,%v", ev, ok)
	}
	ev, ok = parseX10Mouse(32+128, 32+1, 32+1)
	if !ok || ev != (MouseEvent{Button: MouseButtonBack, X: 1, Y: 1, Press: true}) {
		t.Fatalf("xterm back button = %+v,%v", ev, ok)
	}
}

func TestMouseSideButtonsAreFrontendOwned(t *testing.T) {
	b := newTestSession(40, 10)
	b.HandleMouse(MouseEvent{Button: MouseButtonBack, X: 5, Y: 5, Press: true})
	b.HandleMouse(MouseEvent{Button: MouseButtonForward, X: 5, Y: 5, Press: true})
	if b.status != "" {
		t.Fatalf("engine handled side buttons: status = %q", b.status)
	}
}

func TestMouseTokenRoundTrip(t *testing.T) {
	cases := []MouseEvent{
		{Button: 0, X: 3, Y: 7, Press: true},
		{Button: 2, X: 1, Y: 2, Press: false},
		{Button: MouseButtonBack, X: 2, Y: 3, Press: true},
		{Button: MouseButtonForward, X: 2, Y: 3, Press: true},
		{Button: 0, X: 4, Y: 8, Press: true, Motion: true},
		{Wheel: -1, X: 9, Y: 4},
		{Wheel: 1, X: 5, Y: 6},
	}
	for _, want := range cases {
		got, ok := ParseMouseToken(mouseToken(want))
		if !ok || got != want {
			t.Errorf("round trip %+v -> %q -> %+v,%v", want, mouseToken(want), got, ok)
		}
	}
	if _, ok := ParseMouseToken("j"); ok {
		t.Error("plain key decoded as mouse")
	}
}

func TestShiftMouseTokenRoundTrip(t *testing.T) {
	want := MouseEvent{Button: MouseButtonLeft, X: 4, Y: 9, Press: true, Shift: true}
	got, ok := ParseMouseToken(mouseToken(want))
	if !ok || got != want {
		t.Fatalf("round trip = %+v, %v; want %+v", got, ok, want)
	}
}

// TestLinkClickRunsHandlerOnce guards the mouse path: clickContent runs the
// link's click handlers and then follows the link, which must not dispatch the
// same click again.
func TestLinkClickRunsHandlerOnce(t *testing.T) {
	page := newJSPage(`<html><body><a href="javascript:void(0)" onclick="window.__clicks = (window.__clicks || 0) + 1">go</a></body></html>`, ``)
	b := newTestSession(60, 10)
	b.NoDraw = true
	b.Page = page
	b.URL = page.URL
	withQuietStdout(t, func() { b.startJS(page) })
	b.rebuild()
	if len(b.Layout.Links) == 0 {
		t.Fatal("link was not laid out")
	}
	rect := b.Layout.Links[0].Rects[0]
	b.clickContent(rect.X, rect.Y)
	v, err := b.JS.RunCode("clicks", "String(window.__clicks)")
	if err != nil {
		t.Fatal(err)
	}
	if got := v.String(); got != "1" {
		t.Fatalf("click handler ran %s times, want 1", got)
	}
}
