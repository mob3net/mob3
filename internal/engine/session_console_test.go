package engine

import (
	"browser/internal/protocol"
	"browser/internal/textwidth"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestJSDialogsNonInteractive(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`,
		`alert("hello"); document.title = String(confirm("c")) + ":" + String(prompt("p", "d"));`)
	b := newTestSession(60, 15)
	b.Page = page
	withQuietStdout(t, func() { b.startJS(page) })
	if page.Title != "false:null" {
		t.Fatalf("title = %q", page.Title)
	}
	if len(b.jsLog) < 3 {
		t.Fatalf("dialogs were not logged: %v", b.jsLog)
	}
}

func TestJSLogByteCaps(t *testing.T) {
	b := newTestSession(40, 10)
	b.logJS("log", strings.Repeat("x", maxJSLogLine*2))
	if len(b.jsLog) != 1 || len(b.jsLog[0]) > maxJSLogLine+len("[log] ") {
		t.Fatalf("long line = %d bytes, want at most %d", len(b.jsLog[0]), maxJSLogLine+len("[log] "))
	}
	for i := 0; i < jsLogCap+20; i++ {
		b.logJS("log", strings.Repeat("y", maxJSLogLine))
	}
	if len(b.jsLog) > jsLogCap {
		t.Fatalf("lines = %d, want at most %d", len(b.jsLog), jsLogCap)
	}
	if b.jsLogBytes > maxJSLogBytes {
		t.Fatalf("log bytes = %d, want at most %d", b.jsLogBytes, maxJSLogBytes)
	}
	b.clearJSLog()
	if len(b.jsLog) != 0 || b.jsLogBytes != 0 {
		t.Fatal("clearJSLog left state behind")
	}
}

func TestConsoleWindowSlicesStatelessly(t *testing.T) {
	b := newTestSession(40, 12)
	b.Page = &Page{RawHTML: strings.Repeat("line\n", 100)}
	sink := &consoleSink{}
	b.sessionOut = sink
	b.consoleOpen = true
	b.applyConsoleAction(protocol.ConsoleAction{Op: protocol.ConsoleWindow, Pane: consoleHTML, Start: 0, Rows: 5, Width: b.Width})
	b.draw()
	if !sink.got || sink.view.Start != 0 || len(sink.view.Lines) != 5 || sink.view.Total < 100 {
		t.Fatalf("top window = %+v", sink.view)
	}
	b.applyConsoleAction(protocol.ConsoleAction{Op: protocol.ConsoleWindow, Pane: consoleHTML, Start: -1, Rows: 5, Width: b.Width})
	b.draw()
	wantBottom := sink.view.Total - 5
	if sink.view.Start != wantBottom {
		t.Fatalf("bottom start = %d, want %d", sink.view.Start, wantBottom)
	}
	b.applyConsoleAction(protocol.ConsoleAction{Op: protocol.ConsoleWindow, Pane: consoleHTML, Start: 1_000_000, Rows: 5, Width: b.Width})
	b.draw()
	if sink.view.Start != wantBottom {
		t.Fatalf("overscroll start = %d, want %d", sink.view.Start, wantBottom)
	}
	// A new request is independent of the previous one: no scroll state sticks.
	b.applyConsoleAction(protocol.ConsoleAction{Op: protocol.ConsoleWindow, Pane: consoleHTML, Start: 7, Rows: 5, Width: b.Width})
	b.draw()
	if sink.view.Start != 7 || b.window.start != 7 {
		t.Fatalf("requested window = %+v cache=%+v", sink.view, b.window)
	}
}

func TestConsoleWindowLogs(t *testing.T) {
	b := newTestSession(40, 12)
	for i := 0; i < 100; i++ {
		b.jsLog = append(b.jsLog, fmt.Sprintf("[log] line %d", i))
	}
	sink := &consoleSink{}
	b.sessionOut = sink
	b.consoleOpen = true
	b.applyConsoleAction(protocol.ConsoleAction{Op: protocol.ConsoleWindow, Pane: consoleLogs, Start: 10, Rows: 4, Width: b.Width})
	b.draw()
	if sink.view.Total != 100 || sink.view.Start != 10 || len(sink.view.Lines) != 4 {
		t.Fatalf("logs window = %+v", sink.view)
	}
	if sink.view.Lines[0] != "[log] line 10" {
		t.Fatalf("first line = %q", sink.view.Lines[0])
	}
}

func TestConsoleToolsHelpSeedsOnEntry(t *testing.T) {
	b := newTestSession(60, 20)
	b.consoleOpen = true
	b.applyConsoleAction(protocol.ConsoleAction{Op: protocol.ConsoleWindow, Pane: consoleTools, Start: -1, Rows: 10, Width: 60, Enter: true})
	if len(b.toolLog) == 0 {
		t.Fatal("Tools help was not seeded on first entry")
	}
	seeded := len(b.toolLog)
	b.applyConsoleAction(protocol.ConsoleAction{Op: protocol.ConsoleWindow, Pane: consoleTools, Start: 0, Rows: 10, Width: 60})
	if len(b.toolLog) != seeded {
		t.Fatalf("scrolling re-seeded help: %d -> %d", seeded, len(b.toolLog))
	}
	b.clearToolLog()
	b.applyConsoleAction(protocol.ConsoleAction{Op: protocol.ConsoleWindow, Pane: consoleTools, Start: -1, Rows: 10, Width: 60, Enter: true})
	if len(b.toolLog) != seeded {
		t.Fatalf("re-entry help = %d lines, want %d", len(b.toolLog), seeded)
	}
}

func TestConsoleStorageSection(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`,
		`localStorage.setItem("theme", "dark"); sessionStorage.setItem("tab", "1");`)
	b := newTestSession(60, 20)
	b.Page = page
	b.URL = mustParse(t, "https://example.com/")
	withQuietStdout(t, func() { b.startJS(page) })
	testHost(b.loader).Cookies().SetCookies(b.URL, []*http.Cookie{
		{Name: "sid", Value: "abc", Path: "/", HttpOnly: true},
		{Name: "seen", Value: "yes", Path: "/"},
	})
	joined := strings.Join(b.storageLines(), "\n")
	for _, want := range []string{
		"Cookies", "sid=abc", "[HttpOnly]", "seen=yes",
		"localStorage", "theme = dark",
		"sessionStorage", "tab = 1",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("storage lines missing %q:\n%s", want, joined)
		}
	}
}

func TestConsoleStorageCookieDetails(t *testing.T) {
	b := newTestSession(60, 20)
	b.URL = mustParse(t, "https://example.com/api/page")
	testHost(b.loader).Cookies().SetCookies(b.URL, []*http.Cookie{
		{Name: "sid", Value: "abc", Domain: ".example.com", Path: "/api", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode},
		{Name: "plain", Value: "v", Path: "/"},
		{Name: "old", Value: "x", Path: "/", Expires: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Name: "later", Value: "y", Path: "/", Expires: time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)},
	})
	joined := strings.Join(b.storageLines(), "\n")
	for _, want := range []string{
		"sid=abc  [HttpOnly]  [Secure]  [SameSite=Lax]  .example.com  /api  session",
		"plain=v  example.com  /  session",
		"later=y  example.com  /  expires 2030-01-02",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("storage lines missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "old=x") {
		t.Errorf("expired cookie was listed:\n%s", joined)
	}
}

func TestConsoleRequestLinesWrap(t *testing.T) {
	b := newTestSession(60, 20)
	gen := b.loader.CurrentPageGeneration()
	id, ok := b.loader.StartRequest(gen, "GET", "https://example.com/"+strings.Repeat("a", 200), "doc")
	if !ok {
		t.Fatal("request was not recorded")
	}
	b.loader.FinishRequest(id, gen, 200, 1024, time.Millisecond, nil)
	const width = 40
	lines := b.requestLinesWidth(width)
	if len(lines) < 2 {
		t.Fatalf("long request did not wrap: %q", lines)
	}
	for _, line := range lines {
		if w := textwidth.StringWidth(line); w > width {
			t.Fatalf("request line is %d cells, want <= %d: %q", w, width, line)
		}
	}
	if !strings.HasPrefix(lines[1], "    ") {
		t.Fatalf("request continuation is not indented: %q", lines[1])
	}
}

func TestConsoleToolLinesWrap(t *testing.T) {
	b := newTestSession(60, 20)
	b.appendToolLine(strings.Repeat("z", 200))
	const width = 40
	lines := b.toolLines(width)
	if len(lines) < 2 {
		t.Fatalf("long tool output did not wrap: %q", lines)
	}
	for _, line := range lines {
		if w := textwidth.StringWidth(line); w > width {
			t.Fatalf("tool line is %d cells, want <= %d: %q", w, width, line)
		}
	}
	if !strings.HasPrefix(lines[1], "    ") {
		t.Fatalf("tool continuation is not indented: %q", lines[1])
	}
}

func TestConsoleStorageWrapsLongValues(t *testing.T) {
	b := newTestSession(60, 20)
	b.URL = mustParse(t, "https://example.com/")
	testHost(b.loader).Cookies().SetCookies(b.URL, []*http.Cookie{
		{Name: "long", Value: strings.Repeat("y", 200), Path: "/"},
	})
	b.JS = newJSRuntime(b, nil)
	if err := b.JS.storageLocalView.Set("config", strings.Repeat("x", 200)); err != nil {
		t.Fatalf("seed localStorage: %v", err)
	}
	const width = 40
	lines := b.storageLinesWidth(width)
	for _, line := range lines {
		if w := textwidth.StringWidth(line); w > width {
			t.Fatalf("storage line is %d cells, want <= %d: %q", w, width, line)
		}
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "config = xx") || !strings.Contains(joined, "long=yy") {
		t.Fatalf("storage entries missing after wrapping:\n%s", joined)
	}
	if !strings.Contains(joined, "\n    xx") {
		t.Fatalf("storage value continuation is not indented:\n%s", joined)
	}
	// Copying keeps each entry on one unwrapped line.
	if !strings.Contains(strings.Join(b.storageLines(), "\n"), strings.Repeat("x", 200)) {
		t.Fatal("unwrapped storage copy lost the full value")
	}
}

func TestConsoleHTMLShowsRawHTML(t *testing.T) {
	b := newTestSession(40, 20)
	b.Page = &Page{RawHTML: "<html>\n<body><p>Hello raw page</p></body>\n</html>"}
	joined := strings.Join(b.htmlLines(20), "\n")
	if !strings.Contains(joined, "<html>") || !strings.Contains(joined, "Hello raw") {
		t.Fatalf("HTML lines = %q", joined)
	}
	// A different page invalidates the cached wrapping.
	b.Page = &Page{RawHTML: "<html><body>second</body></html>"}
	if got := strings.Join(b.htmlLines(20), "\n"); !strings.Contains(got, "second") {
		t.Fatalf("HTML cache was not refreshed: %q", got)
	}
	// Long source lines wrap to the console width.
	b.Page = &Page{RawHTML: strings.Repeat("x", 45)}
	for _, line := range b.htmlLines(20) {
		if textwidth.StringWidth(line) > 20 {
			t.Fatalf("HTML line wider than requested: %q", line)
		}
	}
}

func TestConsolePausesTimers(t *testing.T) {
	page := newJSPage(`<html><body><p id="out">before</p></body></html>`, `
		setTimeout(function () {
			document.getElementById("out").textContent = "timer";
		}, 10);
	`)
	b := newTestSession(60, 15)
	b.Page = page
	withQuietStdout(t, func() { b.startJS(page) })
	b.consoleOpen = true
	if b.runJSTimers() {
		t.Fatal("timers ran while the console was open")
	}
	time.Sleep(20 * time.Millisecond)
	if b.runJSTimers() {
		t.Fatal("timers ran while the console was open")
	}
	if got := page.Root.FindFirst("p").TextContent(); got != "before" {
		t.Fatalf("timer updated the page while console open: %q", got)
	}
}

func TestConsoleResetsOnSuccessfulNavigation(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `console.log("fresh");`)
	b := newTestSession(40, 10)
	b.jsLog = []string{"[error] old"}
	b.jsErr = true
	b.FindActive = true
	b.FindScope = findScopeHTML
	b.FindText = "old"
	withQuietStdout(t, func() {
		b.finishLoad(LoadResult{Page: page, URL: page.URL})
	})
	if len(b.jsLog) != 1 || !strings.Contains(b.jsLog[0], "fresh") {
		t.Fatalf("console log = %v, want only the new page log", b.jsLog)
	}
	if b.jsErr {
		t.Fatal("JS error state survived navigation")
	}
	if b.FindActive || b.FindText != "" {
		t.Fatalf("console find survived navigation: active=%v text=%q", b.FindActive, b.FindText)
	}
}

func TestConsoleKeepsLogOnFailedNavigation(t *testing.T) {
	u := mustParse(t, "https://example.com/")
	b := newTestSession(40, 10)
	b.jsLog = []string{"[error] old"}
	withQuietStdout(t, func() {
		b.finishLoad(LoadResult{Err: errors.New("boom"), URL: u})
	})
	if len(b.jsLog) != 1 || b.jsLog[0] != "[error] old" {
		t.Fatalf("failed navigation cleared the log: %v", b.jsLog)
	}
}

func TestConsoleYCopiesActiveSection(t *testing.T) {
	b := newTestSession(60, 20)
	b.consoleOpen = true
	b.jsLog = []string{"[log] hello", "[error] boom"}
	sink := &clipboardSink{}
	b.sessionOut = sink
	b.applyConsoleAction(protocol.ConsoleAction{Op: protocol.ConsoleCopy, Pane: consoleLogs})
	if sink.text != "[log] hello\n[error] boom" {
		t.Fatalf("log copy = %q", sink.text)
	}
	if b.status != "Copied 24 chars" {
		t.Fatalf("status = %q", b.status)
	}

	// The HTML section copies the raw source, not the width-wrapped display lines.
	b.Page = &Page{RawHTML: "<html>\n<body>raw source</body>\n</html>"}
	sink.text = ""
	b.applyConsoleAction(protocol.ConsoleAction{Op: protocol.ConsoleCopy, Pane: consoleHTML})
	if sink.text != b.Page.RawHTML {
		t.Fatalf("HTML copy = %q", sink.text)
	}
}
