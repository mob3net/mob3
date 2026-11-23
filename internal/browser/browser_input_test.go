package browser

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestQuitConfirmation(t *testing.T) {
	b := NewBrowser(40, 10)
	if b.HandleKey("q") {
		t.Fatal("q quit without confirmation")
	}
	if !b.quitPending || b.status != quitConfirmText {
		t.Fatalf("quit prompt = pending %v status %q", b.quitPending, b.status)
	}
	if b.HandleKey("n") {
		t.Fatal("n confirmed quit")
	}
	if b.quitPending || b.status != "" {
		t.Fatalf("cancel state = pending %v status %q", b.quitPending, b.status)
	}
	if b.HandleKey("ctrl-c") {
		t.Fatal("ctrl-c quit without confirmation")
	}
	if !b.HandleKey("y") {
		t.Fatal("y did not confirm quit")
	}
	if b.quitPending {
		t.Fatal("quit confirmation survived a confirmed quit")
	}
	if b.HandleKey("q") {
		t.Fatal("second q quit without confirmation")
	}
	if !b.HandleKey("enter") {
		t.Fatal("enter did not confirm quit")
	}
}

func TestURLPromptPrefillsAndSelects(t *testing.T) {
	b := NewBrowser(40, 10)
	b.URL = mustParse(t, "https://example.com/page")
	b.HandleKey("o")
	if b.promptText != "https://example.com/page" || !b.promptSelectAll {
		t.Fatalf("prompt = %q selectAll=%v", b.promptText, b.promptSelectAll)
	}
	b.HandleKey("x")
	if b.promptText != "x" || b.promptSelectAll {
		t.Fatalf("typing did not replace selection: %q selectAll=%v", b.promptText, b.promptSelectAll)
	}

	b.prompt = false
	b.promptText = ""
	b.HandleKey("o")
	b.HandleKey("backspace")
	if b.promptText != "" || b.promptSelectAll {
		t.Fatalf("backspace did not clear selection: %q selectAll=%v", b.promptText, b.promptSelectAll)
	}
}

func TestURLPromptCursorEditing(t *testing.T) {
	b := NewBrowser(40, 10)
	b.URL = mustParse(t, "https://example.com/abc")
	b.HandleKey("o")
	b.HandleKey("right") // collapse selection to end
	b.HandleKey("left")
	b.HandleKey("left")
	b.HandleKey("left")
	b.HandleKey("X")
	if got := b.promptText; got != "https://example.com/Xabc" {
		t.Fatalf("insert = %q", got)
	}
	b.HandleKey("delete")
	if got := b.promptText; got != "https://example.com/Xbc" {
		t.Fatalf("delete = %q", got)
	}
	b.HandleKey("home")
	b.HandleKey("delete")
	if got := b.promptText; got != "ttps://example.com/Xbc" {
		t.Fatalf("home/delete = %q", got)
	}
	b.HandleKey("end")
	head, at, tail, show := promptParts(" Open: ", b.promptText, b.promptCursor, false, 40)
	if !show || stringWidth(head)+1+stringWidth(tail) > 40 {
		t.Fatalf("bad prompt parts: head=%q at=%q tail=%q show=%v", head, at, tail, show)
	}
	if !strings.HasSuffix(head, "Xbc") || at != ' ' || tail != "" {
		t.Fatalf("cursor parts: head=%q at=%q tail=%q", head, at, tail)
	}
}

func TestURLPromptWordEditing(t *testing.T) {
	b := NewBrowser(80, 10)
	open := func(text string) {
		b.prompt = false
		b.promptText = ""
		b.URL = mustParse(t, text)
		b.HandleKey("o")
		b.HandleKey("right") // collapse the initial select-all to the end
	}
	got := func() string { return b.promptText }

	open("https://example.com/a/b")
	b.HandleKey("alt-left")
	b.HandleKey("alt-left")
	b.HandleKey("Z")
	if got() != "https://example.com/Za/b" {
		t.Fatalf("alt-left motion = %q", got())
	}

	open("https://example.com/a/b")
	b.HandleKey("ctrl-left")
	b.HandleKey("ctrl-left")
	b.HandleKey("Z")
	if got() != "https://example.com/Za/b" {
		t.Fatalf("ctrl-left motion = %q", got())
	}

	open("https://en.wikipedia.org/wiki/Wyoming")
	for _, want := range []string{
		"https://en.wikipedia.org/wiki/",
		"https://en.wikipedia.org/",
		"https://en.wikipedia.",
	} {
		b.HandleKey("alt-backspace")
		if got() != want {
			t.Fatalf("alt-backspace = %q, want %q", got(), want)
		}
	}

	open("https://example.com/path")
	b.HandleKey("ctrl-w")
	if got() != "https://example.com/" {
		t.Fatalf("ctrl-w = %q", got())
	}

	open("https://example.com/a")
	b.HandleKey("home")
	b.HandleKey("alt-d")
	if got() != "example.com/a" {
		t.Fatalf("alt-d = %q", got())
	}

	open("abc")
	b.HandleKey("ctrl-b")
	b.HandleKey("X")
	if got() != "abXc" {
		t.Fatalf("ctrl-b = %q", got())
	}
	b.HandleKey("ctrl-f")
	b.HandleKey("Y")
	if got() != "abXcY" {
		t.Fatalf("ctrl-f = %q", got())
	}
}

func TestVimURLKey(t *testing.T) {
	b := NewBrowser(40, 10)
	b.HandleKey("o")
	if !b.prompt {
		t.Fatal("o did not open the URL prompt")
	}
	b.prompt = false
	b.HandleKey("g")
	if b.prompt {
		t.Fatal("g should not open the URL prompt")
	}
}

func TestCtrlLOpensURLPrompt(t *testing.T) {
	b := NewBrowser(40, 10)
	b.URL = mustParse(t, "https://example.com/page")
	b.HandleKey("ctrl-l")
	if !b.prompt {
		t.Fatal("ctrl-l did not open the URL prompt")
	}
	if b.promptText != "https://example.com/page" || !b.promptSelectAll {
		t.Fatalf("prompt = %q selectAll=%v", b.promptText, b.promptSelectAll)
	}
	b.HandleKey("x")
	if b.promptText != "x" || b.promptSelectAll {
		t.Fatalf("typing did not replace selection: %q selectAll=%v", b.promptText, b.promptSelectAll)
	}

	doc := ParseHTML(`<html><body><input name="q"></body></html>`)
	u := mustParse(t, "https://example.com/")
	f := NewBrowser(80, 10)
	f.Page = &Page{URL: u, Base: u, Root: doc}
	f.rebuild()
	q := inputByName(doc, "q")
	if q == nil {
		t.Fatal("input not found")
	}
	f.setInputFocus(q)
	f.HandleKey("ctrl-l")
	if f.prompt {
		t.Fatal("ctrl-l opened the URL prompt from a focused field")
	}
}

func TestEscapeShowsExitHint(t *testing.T) {
	b := NewBrowser(80, 10)
	b.Layout = &Layout{Height: 10}
	b.HandleKey("esc")
	if b.status != exitHint {
		t.Fatalf("status = %q, want %q", b.status, exitHint)
	}
	drawQuiet(b)
	line := plainLine(b.prev[len(b.prev)-1])
	for _, want := range []string{"q Quit", "? Help"} {
		if !strings.Contains(line, want) {
			t.Errorf("footer = %q, want %q", line, want)
		}
	}
}

func TestEscapeFromPromptShowsExitHint(t *testing.T) {
	b := NewBrowser(80, 10)
	b.openURLPrompt()
	b.HandleKey("esc")
	if b.prompt {
		t.Fatal("escape did not close the URL prompt")
	}
	if b.status != exitHint {
		t.Fatalf("status = %q, want %q", b.status, exitHint)
	}
}

func TestEscapeFromInputShowsExitHint(t *testing.T) {
	doc := ParseHTML(`<html><body><input name="q"></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(80, 10)
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

func TestHistoryShortcutKey(t *testing.T) {
	b := NewBrowser(60, 10)
	b.HandleKey("H")
	select {
	case res := <-b.LoadCh:
		b.finishLoad(res)
	case <-time.After(2 * time.Second):
		t.Fatal("H did not load about:history")
	}
	if b.URL == nil || b.URL.String() != "about:history" {
		t.Fatalf("url after H = %v", b.URL)
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
	b := NewBrowser(40, 10)
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

func TestClickOutsideURLPromptClosesIt(t *testing.T) {
	b := NewBrowser(80, 10)
	b.Layout = &Layout{Height: 10}
	b.openURLPrompt()
	drawQuiet(b)
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: 5, Press: true})
	if b.prompt {
		t.Fatal("click outside the URL field left the prompt open")
	}
	if b.status != exitHint {
		t.Fatalf("status = %q, want %q", b.status, exitHint)
	}
}

func TestClickInURLPromptKeepsItOpen(t *testing.T) {
	b := NewBrowser(80, 10)
	b.Layout = &Layout{Height: 10}
	b.openURLPrompt()
	drawQuiet(b)
	// Row 2 (1-based) is the URL field on a normal header.
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: 2, Press: true})
	if !b.prompt {
		t.Fatal("click on the URL field closed the prompt")
	}
}

func TestShiftMouseDragLeavesURLPromptAlone(t *testing.T) {
	b := NewBrowser(80, 10)
	b.URL = mustParse(t, "https://example.com/page")
	b.Layout = &Layout{Height: 10}
	b.openURLPrompt()
	drawQuiet(b)
	before := b.promptText
	prefixW := stringWidth(" Open: ")
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: prefixW + 5 + 1, Y: 2, Press: true, Shift: true})
	if !b.skipDraw {
		t.Fatal("shift+press did not suppress the redraw")
	}
	b.skipDraw = false
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: prefixW + 10 + 1, Y: 2, Motion: true, Shift: true})
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: prefixW + 10 + 1, Y: 2, Press: false, Shift: true})
	if !b.prompt {
		t.Fatal("shift+drag closed the URL prompt")
	}
	if b.promptText != before {
		t.Fatalf("shift+drag changed the URL to %q", b.promptText)
	}
	if !b.promptSelectAll {
		t.Fatal("shift+drag cleared the prompt selection")
	}
	if !b.skipDraw {
		t.Fatal("shift+release did not suppress the redraw")
	}
}

func TestURLPromptMouseClickPlacesCursor(t *testing.T) {
	b := NewBrowser(80, 10)
	b.URL = mustParse(t, "https://example.com/page")
	b.Layout = &Layout{Height: 10}
	b.openURLPrompt()
	drawQuiet(b)

	prefixW := stringWidth(" Open: ")
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: prefixW + 4 + 1, Y: 2, Press: true})
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: prefixW + 4 + 1, Y: 2, Press: false})
	if b.promptCursor != 4 {
		t.Fatalf("cursor = %d, want 4", b.promptCursor)
	}
	if b.promptSelectAll {
		t.Fatal("click did not collapse the select-all prompt")
	}
}

func TestShiftMouseTokenRoundTrip(t *testing.T) {
	want := MouseEvent{Button: MouseButtonLeft, X: 4, Y: 9, Press: true, Shift: true}
	got, ok := ParseMouseToken(mouseToken(want))
	if !ok || got != want {
		t.Fatalf("round trip = %+v, %v; want %+v", got, ok, want)
	}
}

func TestNoColorPromptClearsOldURL(t *testing.T) {
	doc := ParseHTML(`<html><body><p>hello</p></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 12)
	b.color = false
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.URL = u
	b.rebuild()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	b.openURLPrompt()
	b.promptText = strings.Repeat("u", 50)
	b.promptCursor = len(b.promptText)
	b.draw()
	b.promptText = "x"
	b.promptCursor = 1
	b.draw()
	os.Stdout = old
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "\x1b[K") {
		t.Fatalf("no-color redraw did not clear the shortened URL row: %q", string(out))
	}
}

func TestColonPromptQuitsLikeQ(t *testing.T) {
	b := NewBrowser(60, 12)
	b.URL = mustParse(t, "https://example.com/page")
	b.HandleKey(":")
	if !b.prompt || !b.promptVim {
		t.Fatalf(": did not open a command prompt: prompt=%v vim=%v", b.prompt, b.promptVim)
	}
	b.HandleKey("q")
	if !b.HandleKey("enter") {
		t.Fatal(":q did not quit immediately")
	}
	if b.prompt {
		t.Fatal(":q left the prompt open")
	}
	if b.quitPending {
		t.Fatal(":q asked for confirmation")
	}

	// The :o-style prompt keeps q as ordinary input.
	b = NewBrowser(60, 12)
	b.URL = mustParse(t, "https://example.com/page")
	b.LoadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		return &Page{URL: b.URL, Base: b.URL, Root: ParseHTML("<html><body>ok</body></html>")}, nil
	}
	b.HandleKey("o")
	b.HandleKey("q")
	b.HandleKey("enter")
	if b.quitPending {
		t.Fatal("o prompt treated q as quit")
	}
	entries := b.promptHist.entries
	if len(entries) != 1 || entries[0] != "q" {
		t.Fatalf("prompt history = %v, want [q]", entries)
	}
}

func TestURLPromptHistoryKeys(t *testing.T) {
	b := NewBrowser(60, 12)
	b.URL = mustParse(t, "https://example.com/page")
	b.promptHist.entries = []string{"first query", "second query"}
	b.promptHist.reset()
	b.HandleKey("o")
	if b.promptText != "https://example.com/page" {
		t.Fatalf("prompt did not start at the current URL: %q", b.promptText)
	}
	for _, tc := range []struct {
		key  string
		want string
	}{
		{"ctrl-p", "second query"},
		{"up", "first query"},
		{"down", "second query"},
		{"ctrl-n", "https://example.com/page"},
	} {
		b.HandleKey(tc.key)
		if b.promptText != tc.want {
			t.Fatalf("after %s prompt = %q, want %q", tc.key, b.promptText, tc.want)
		}
	}
}

func TestQuitConfirmationBeatsHoveredLink(t *testing.T) {
	b := NewBrowser(60, 20)
	b.Layout = &Layout{Links: []*LinkInfo{{Href: "/target"}}}
	b.Hover = 0
	b.HandleKey("q")
	if !b.quitPending || b.status != quitConfirmText {
		t.Fatalf("q did not ask: pending=%v status=%q", b.quitPending, b.status)
	}
	if got := b.footerLeft(); got != " "+quitConfirmText {
		t.Fatalf("footer with hover = %q, want the quit confirmation", got)
	}
}

// TestEditLineKeys covers the shared editing core directly: caret motion, word
// motion, deletion, typing, selection replacement and the length cap.
func TestEditLineKeys(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		caret     int
		key       string
		cfg       editConfig
		value     string
		caretWant int
		handled   bool
	}{
		{name: "left", in: "abcd", caret: 2, key: "left", value: "abcd", caretWant: 1, handled: true},
		{name: "right stops at end", in: "abcd", caret: 4, key: "right", value: "abcd", caretWant: 4, handled: true},
		{name: "ctrl-b", in: "abcd", caret: 3, key: "ctrl-b", value: "abcd", caretWant: 2, handled: true},
		{name: "word right", in: "one two", caret: 0, key: "alt-f", value: "one two", caretWant: 3, handled: true},
		{name: "word left", in: "one two", caret: 7, key: "alt-b", value: "one two", caretWant: 4, handled: true},
		{name: "ctrl-left word left", in: "one two", caret: 7, key: "ctrl-left", value: "one two", caretWant: 4, handled: true},
		{name: "home", in: "abcd", caret: 2, key: "home", value: "abcd", caretWant: 0, handled: true},
		{name: "end", in: "abcd", caret: 1, key: "end", value: "abcd", caretWant: 4, handled: true},
		{name: "ctrl-e", in: "abcd", caret: 1, key: "ctrl-e", value: "abcd", caretWant: 4, handled: true},
		{name: "backspace", in: "abcd", caret: 2, key: "backspace", value: "acd", caretWant: 1, handled: true},
		{name: "delete", in: "abcd", caret: 2, key: "delete", value: "abd", caretWant: 2, handled: true},
		{name: "ctrl-w", in: "one two", caret: 7, key: "ctrl-w", value: "one ", caretWant: 4, handled: true},
		{name: "alt-d", in: "one two", caret: 3, key: "alt-d", value: "one", caretWant: 3, handled: true},
		{name: "ctrl-u", in: "abcd", caret: 2, key: "ctrl-u", value: "", caretWant: 0, handled: true},
		{name: "ctrl-k", in: "abcd", caret: 2, key: "ctrl-k", value: "ab", caretWant: 2, handled: true},
		{name: "type", in: "abcd", caret: 2, key: "X", value: "abXcd", caretWant: 3, handled: true},
		{name: "type multibyte", in: "ab", caret: 1, key: "é", value: "aéb", caretWant: 3, handled: true},
		{name: "enter belongs to the caller", in: "abcd", caret: 2, key: "enter", value: "abcd", caretWant: 2, handled: false},
		{name: "escape belongs to the caller", in: "abcd", caret: 2, key: "esc", value: "abcd", caretWant: 2, handled: false},
		{name: "up belongs to the caller", in: "abcd", caret: 2, key: "up", value: "abcd", caretWant: 2, handled: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, caret, handled := editLineKey(tc.in, tc.caret, tc.key, tc.cfg)
			if value != tc.value || caret != tc.caretWant || handled != tc.handled {
				t.Fatalf("editLineKey(%q,%d,%q) = %q,%d,%v; want %q,%d,%v",
					tc.in, tc.caret, tc.key, value, caret, handled, tc.value, tc.caretWant, tc.handled)
			}
		})
	}
}

func TestEditLineSelection(t *testing.T) {
	sel := true
	cfg := editConfig{selAll: &sel, collapseSelection: true}
	value, caret, handled := editLineKey("abcd", 4, "left", cfg)
	if value != "abcd" || caret != 0 || !handled || sel {
		t.Fatalf("left over a selection = %q,%d,%v sel=%v; want the caret at the start and no selection",
			value, caret, handled, sel)
	}

	sel = true
	cfg = editConfig{selAll: &sel}
	if _, caret, _ = editLineKey("abcd", 4, "left", cfg); caret != 3 {
		t.Fatalf("left without collapse = %d, want 3", caret)
	}

	sel = true
	value, caret, _ = editLineKey("abcd", 4, "X", editConfig{selAll: &sel})
	if value != "X" || caret != 1 || sel {
		t.Fatalf("typing over a selection = %q,%d sel=%v; want X,1,false", value, caret, sel)
	}

	sel = true
	value, caret, _ = editLineKey("abcd", 0, "backspace", editConfig{selAll: &sel})
	if value != "" || caret != 0 || sel {
		t.Fatalf("backspace over a selection = %q,%d sel=%v; want empty", value, caret, sel)
	}

	sel = false
	if _, _, handled = editLineKey("abcd", 2, "ctrl-a", editConfig{selAll: &sel}); !handled || !sel {
		t.Fatalf("ctrl-a = handled %v sel %v; want both true", handled, sel)
	}
	if _, _, handled = editLineKey("abcd", 2, "ctrl-a", editConfig{}); handled {
		t.Fatal("ctrl-a without a selection model must fall through to the caller")
	}
}

func TestEditLineMaxRunes(t *testing.T) {
	value, caret, handled := editLineKey("abcd", 4, "X", editConfig{maxRunes: 4})
	if value != "abcd" || caret != 4 || !handled {
		t.Fatalf("typing at the cap = %q,%d,%v; want the value unchanged", value, caret, handled)
	}
	if value, _, _ := editLineKey("abc", 3, "X", editConfig{maxRunes: 4}); value != "abcX" {
		t.Fatalf("typing below the cap = %q, want abcX", value)
	}
}

// TestDialogPromptWordEditing pins the JS prompt to the same editing keys as
// the URL prompt, which the help page documents.
func TestDialogPromptWordEditing(t *testing.T) {
	d := &jsDialog{kind: "prompt", text: "one two", cursor: 7}
	text, cursor, done, ok := dialogPromptKey(d, "alt-b")
	if text != "one two" || cursor != 4 || done || ok {
		t.Fatalf("alt-b = %q,%d,%v,%v; want the caret before the last word", text, cursor, done, ok)
	}
	if text, cursor, _, _ = dialogPromptKey(d, "ctrl-w"); text != "one " || cursor != 4 {
		t.Fatalf("ctrl-w = %q,%d; want the last word removed", text, cursor)
	}

	d = &jsDialog{kind: "prompt", text: "abc", cursor: 3, selectAll: true}
	if text, cursor, _, _ = dialogPromptKey(d, "x"); text != "x" || cursor != 1 {
		t.Fatalf("typing over a selected prompt = %q,%d; want x,1", text, cursor)
	}
	if _, _, done, _ = dialogPromptKey(d, "esc"); !done {
		t.Fatal("escape did not finish the prompt")
	}
}
