package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestConsoleWordEditing(t *testing.T) {
	b := NewBrowser(80, 20)
	b.consoleOpen = true
	b.consoleEdit = true
	b.consoleText = "https://en.wikipedia.org/wiki/Wyoming"
	b.consoleCursor = len(b.consoleText)

	b.HandleKey("alt-backspace")
	if b.consoleText != "https://en.wikipedia.org/wiki/" {
		t.Fatalf("alt-backspace = %q", b.consoleText)
	}
	b.HandleKey("ctrl-w")
	if b.consoleText != "https://en.wikipedia.org/" {
		t.Fatalf("ctrl-w = %q", b.consoleText)
	}
	b.HandleKey("alt-left")
	b.HandleKey("Z")
	if b.consoleText != "https://en.wikipedia.Zorg/" {
		t.Fatalf("word motion = %q", b.consoleText)
	}
}

func TestConsoleQuitConfirmation(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	if b.handleConsoleKey("q") {
		t.Fatal("console q quit without confirmation")
	}
	if !b.quitPending || b.status != quitConfirmText {
		t.Fatalf("console quit prompt = pending %v status %q", b.quitPending, b.status)
	}
	if !b.HandleKey("y") {
		t.Fatal("y did not confirm console quit")
	}
}

func TestJSConfirm(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`,
		`document.title = confirm("Continue?") ? "yes" : "no";`)
	b := NewBrowser(60, 15)
	b.Page = page
	keys := make(chan string, 4)
	keys <- "y"
	b.keys = keys
	withQuietStdout(t, func() { b.startJS(page) })
	if page.Title != "yes" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSPrompt(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`,
		`document.title = prompt("Name", "bob") || "cancel";`)
	b := NewBrowser(60, 15)
	b.Page = page
	keys := make(chan string, 4)
	keys <- "x"
	keys <- "enter"
	b.keys = keys
	withQuietStdout(t, func() { b.startJS(page) })
	if page.Title != "x" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSDialogsNonInteractive(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`,
		`alert("hello"); document.title = String(confirm("c")) + ":" + String(prompt("p", "d"));`)
	b := NewBrowser(60, 15)
	b.Page = page
	withQuietStdout(t, func() { b.startJS(page) })
	if page.Title != "false:null" {
		t.Fatalf("title = %q", page.Title)
	}
	if len(b.jsLog) < 3 {
		t.Fatalf("dialogs were not logged: %v", b.jsLog)
	}
}

func TestConsoleRequestPane(t *testing.T) {
	b := NewBrowser(60, 20)
	gen := b.loader.beginPage()
	id, ok := b.loader.startRequest(gen, "GET", "https://example.com/a.css", "css")
	if !ok {
		t.Fatal("request was not started")
	}
	b.loader.finishRequest(id, gen, 200, 2048, 10*time.Millisecond, nil)
	b.loader.recordCacheHit(gen, "GET", "https://example.com/b.JS", "js", 512)
	lines := b.requestLines()
	if len(lines) != 2 || !strings.Contains(lines[0], "200") ||
		!strings.Contains(lines[0], "2.0K") || !strings.Contains(lines[0], "css") ||
		!strings.Contains(lines[0], "https://example.com/a.css") {
		t.Fatalf("request lines = %q", lines)
	}
	if !strings.Contains(lines[1], "js") || !strings.Contains(lines[1], "cached") || strings.Contains(lines[1], "512B") {
		t.Fatalf("cached request line = %q", lines[1])
	}
	if summary := b.requestCacheSummary(); !strings.Contains(summary, "/") || !strings.Contains(summary, "entries") {
		t.Fatalf("cache summary = %q", summary)
	}
	if got := humanSize(512); got != "512B" {
		t.Fatalf("humanSize = %q", got)
	}

	b.consoleOpen = true
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	b.handleConsoleKey("tab")
	if b.consolePane != 1 {
		t.Fatalf("console pane = %d, want requests", b.consolePane)
	}
	b.handleConsoleKey("k")
	if b.reqScroll != 1 {
		t.Fatalf("request scroll = %d", b.reqScroll)
	}
	b.handleConsoleKey("G")
	if b.reqScroll != 0 {
		t.Fatalf("request scroll after G = %d", b.reqScroll)
	}
}

func TestConsoleRequestsHeaderCountsRealRequests(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	b.URL = mustParse(t, "about:home")
	b.loader.beginPage()
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	l := newConsoleLayout(20, consoleLogs)
	line := plainLine(b.prev[l.tabsY])
	if !strings.Contains(line, "Requests (0)") {
		t.Fatalf("empty request tab = %q, want Requests (0)", line)
	}
}

func TestConsoleStorageSection(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`,
		`localStorage.setItem("theme", "dark"); sessionStorage.setItem("tab", "1");`)
	b := NewBrowser(60, 20)
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
	b := NewBrowser(60, 20)
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
	b := NewBrowser(60, 20)
	gen := b.loader.currentPageGeneration()
	id, ok := b.loader.startRequest(gen, "GET", "https://example.com/"+strings.Repeat("a", 200), "doc")
	if !ok {
		t.Fatal("request was not recorded")
	}
	b.loader.finishRequest(id, gen, 200, 1024, time.Millisecond, nil)
	const width = 40
	lines := b.requestLinesWidth(width)
	if len(lines) < 2 {
		t.Fatalf("long request did not wrap: %q", lines)
	}
	for _, line := range lines {
		if w := stringWidth(line); w > width {
			t.Fatalf("request line is %d cells, want <= %d: %q", w, width, line)
		}
	}
	if !strings.HasPrefix(lines[1], "    ") {
		t.Fatalf("request continuation is not indented: %q", lines[1])
	}
}

func TestConsoleToolLinesWrap(t *testing.T) {
	b := NewBrowser(60, 20)
	b.appendToolLine(strings.Repeat("z", 200))
	const width = 40
	lines := b.toolLines(width)
	if len(lines) < 2 {
		t.Fatalf("long tool output did not wrap: %q", lines)
	}
	for _, line := range lines {
		if w := stringWidth(line); w > width {
			t.Fatalf("tool line is %d cells, want <= %d: %q", w, width, line)
		}
	}
	if !strings.HasPrefix(lines[1], "    ") {
		t.Fatalf("tool continuation is not indented: %q", lines[1])
	}
}

func TestConsoleStorageWrapsLongValues(t *testing.T) {
	b := NewBrowser(60, 20)
	b.URL = mustParse(t, "https://example.com/")
	testHost(b.loader).Cookies().SetCookies(b.URL, []*http.Cookie{
		{Name: "long", Value: strings.Repeat("y", 200), Path: "/"},
	})
	b.JS = newJSRuntime(b, nil)
	b.JS.SetStorageLocalData(map[string]string{
		"config": strings.Repeat("x", 200),
	})
	const width = 40
	lines := b.storageLinesWidth(width)
	for _, line := range lines {
		if w := stringWidth(line); w > width {
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

func TestConsoleOnlyOpenSectionHasBody(t *testing.T) {
	b := NewBrowser(60, 20)
	b.color = true
	b.consoleOpen = true
	b.jsLog = []string{"[log] hidden when requests open"}
	b.consolePane = consoleRequests
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	text := strings.Join(b.prev, "\n")
	if strings.Contains(text, "hidden when requests open") {
		t.Fatal("logs body was rendered while requests was open")
	}
	l := newConsoleLayout(20, consoleRequests)
	if !strings.Contains(plainLine(b.prev[l.tabsY]), "JS logs") {
		t.Fatalf("logs tab missing: %q", b.prev[l.tabsY])
	}
	if !strings.Contains(plainLine(b.prev[l.tabsY]), "Requests") {
		t.Fatalf("requests tab missing: %q", b.prev[l.tabsY])
	}
}

func TestConsoleScrollReachesFirstWrappedLine(t *testing.T) {
	b := NewBrowser(40, 20)
	b.jsLog = []string{strings.Repeat("x", 400)}
	b.consoleOpen = true
	b.handleConsoleKey("g")
	want := len(b.consoleLines(b.Width - 2))
	if b.consoleScroll != want {
		t.Fatalf("console scroll = %d, want %d", b.consoleScroll, want)
	}
	if want <= 1 {
		t.Fatalf("test line did not wrap: %d lines", want)
	}
}

func TestConsoleHTMLShowsRawHTML(t *testing.T) {
	b := NewBrowser(40, 20)
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
		if stringWidth(line) > 20 {
			t.Fatalf("HTML line wider than requested: %q", line)
		}
	}
}

func TestConsoleHTMLOpensAtTop(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	b.Page = &Page{RawHTML: "<html>\n" + strings.Repeat("<p>line</p>\n", 100) + "</html>"}
	for i := 0; i < 3; i++ {
		b.handleConsoleKey("tab")
	}
	if b.consolePane != consoleHTML {
		t.Fatalf("console pane = %d, want HTML", b.consolePane)
	}
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	text := strings.Join(b.prev, "\n")
	if !strings.Contains(text, "<html>") {
		t.Fatalf("HTML section did not start at the top: %q", text)
	}
	if strings.Contains(text, "</html>") {
		t.Fatalf("HTML section opened at the bottom: %q", text)
	}
}

func TestConsoleClickSwitchesPane(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	l := newConsoleLayout(20, consoleLogs)
	tabX := func(pane int) int {
		for _, hit := range b.consoleTabHits {
			if hit.pane == pane {
				return hit.x + 1
			}
		}
		t.Fatalf("no clickable tab for pane %d", pane)
		return 0
	}
	// Clicking the log body keeps the logs section open.
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: l.bodyY + 1, Press: true})
	if b.consolePane != consoleLogs {
		t.Fatalf("console pane after log click = %d, want logs", b.consolePane)
	}
	// Clicking the Requests tab opens requests.
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: tabX(consoleRequests), Y: l.tabsY + 1, Press: true})
	if b.consolePane != consoleRequests {
		t.Fatalf("console pane after tab click = %d, want requests", b.consolePane)
	}
	// A body row in the requests section keeps it open.
	reqLayout := newConsoleLayout(20, consoleRequests)
	if reqLayout.bodyY < 0 {
		t.Fatal("requests section has no body")
	}
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: reqLayout.bodyY + 1, Press: true})
	if b.consolePane != consoleRequests {
		t.Fatalf("console pane after request click = %d, want requests", b.consolePane)
	}
	// Clicking the Logs tab returns to the logs section.
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: tabX(consoleLogs), Y: l.tabsY + 1, Press: true})
	if b.consolePane != consoleLogs {
		t.Fatalf("console pane after tab click = %d, want logs", b.consolePane)
	}
}

func TestConsoleFooterIconClosesConsole(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	if !strings.HasSuffix(plainLine(b.prev[19]), ">_") {
		t.Fatalf("console footer missing >_ icon: %q", b.prev[19])
	}
	found := false
	for _, hit := range b.footerHits {
		if hit.kind == headerConsole && hit.y == 19 && hit.x+hit.w == 60 {
			found = true
		}
	}
	if !found {
		t.Fatalf("console footer icon has no click region: %+v", b.footerHits)
	}
	withQuietStdout(t, func() {
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 60, Y: 20, Press: true})
	})
	if b.consoleOpen {
		t.Fatal("clicking the footer icon left the console open")
	}
	if b.status != exitHint {
		t.Fatalf("status = %q, want %q", b.status, exitHint)
	}
}

func TestConsoleTabsHighlightActiveSection(t *testing.T) {
	b := NewBrowser(60, 20)
	normal := RGB(0x1f, 0x29, 0x37)
	active := RGB(0x2f, 0x6f, 0xbf)
	style := &Style{Bold: true, Color: RGB(0xdd, 0xe6, 0xee), FontSize: 16, LineHeight: 1}
	check := func() {
		g := NewGrid(60, 1, RGB(0, 0, 0), RGB(0, 0, 0))
		b.drawConsoleTabs(g, 0, 60, normal, active, style, RGB(0xdd, 0xe6, 0xee))
		seen := 0
		for _, hit := range b.consoleTabHits {
			want := normal
			if hit.pane == b.consolePane {
				want = active
			}
			if got := g.Cells[hit.x].BG; got != want {
				t.Fatalf("tab %d background = %+v, want %+v", hit.pane, got, want)
			}
			seen++
		}
		if seen != consoleSectionCount {
			t.Fatalf("tab hits = %d, want %d", seen, consoleSectionCount)
		}
	}
	check()
	b.consolePane = consoleRequests
	check()
}

func TestConsoleMouseWheelScrollsHoveredPane(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	b.consolePane = consoleRequests
	l := newConsoleLayout(20, consoleRequests)
	b.HandleMouse(MouseEvent{X: 5, Y: l.bodyY + 2, Wheel: -1})
	if b.reqScroll != 3 || b.consoleScroll != 0 {
		t.Fatalf("wheel scroll = logs %d, requests %d", b.consoleScroll, b.reqScroll)
	}
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	logsX := 1
	for _, hit := range b.consoleTabHits {
		if hit.pane == consoleLogs {
			logsX = hit.x + 1
		}
	}
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: logsX, Y: l.tabsY + 1, Press: true})
	if b.consolePane != consoleLogs {
		t.Fatalf("console pane after logs tab = %d, want logs", b.consolePane)
	}
	b.HandleMouse(MouseEvent{X: 5, Y: 2, Wheel: -1})
	if b.reqScroll != 3 || b.consoleScroll != 3 {
		t.Fatalf("wheel scroll = logs %d, requests %d", b.consoleScroll, b.reqScroll)
	}
	b.HandleMouse(MouseEvent{X: 5, Y: 2, Wheel: 1})
	if b.consoleScroll != 0 {
		t.Fatalf("wheel down scroll = logs %d", b.consoleScroll)
	}
}

func TestConsolePausesTimers(t *testing.T) {
	page := newJSPage(`<html><body><p id="out">before</p></body></html>`, `
		setTimeout(function () {
			document.getElementById("out").textContent = "timer";
		}, 10);
	`)
	b := NewBrowser(60, 15)
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

func TestConsoleContentCannotInjectTerminalControls(t *testing.T) {
	b := NewBrowser(60, 20)
	b.color = true
	b.consoleOpen = true
	b.jsLog = []string{"[log] \x1b]52;c;evil\x07\x1b[2J"}
	b.Page = &Page{RawHTML: "<html>\x1b]0;pwn\x07</html>"}
	check := func(where string) {
		joined := strings.Join(b.prev, "\n")
		for _, bad := range []string{"\x1b]52", "\x1b[2J", "\x07", "\x1b]0", "\u009b"} {
			if strings.Contains(joined, bad) {
				t.Fatalf("%s injected terminal control %q: %q", where, bad, joined)
			}
		}
	}
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	check("log")

	gen := b.loader.beginPage()
	id, ok := b.loader.startRequest(gen, "GET", "https://example.com/\x1b]52;c;evil\x07", "doc")
	if !ok {
		t.Fatal("request was not started")
	}
	b.loader.finishRequest(id, gen, 200, 1, 0, nil)
	b.selectConsolePane(consoleRequests)
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	check("request")

	b.JS = newJSRuntime(b, nil)
	b.JS.SetStorageLocalData(map[string]string{"k\x1b[2J": "v\x1b]52;c;x\x07"})
	b.JS.SetStorageSessionData(map[string]string{})
	b.selectConsolePane(consoleStorage)
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	check("storage")

	b.selectConsolePane(consoleHTML)
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	check("HTML")
}

func TestJSDialogMouseClose(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `alert("Continue?");`)
	b := NewBrowser(60, 15)
	b.Page = page
	// The alert is one line at width 60: box 56 wide, 7 tall, centered at
	// x=2, y=4. The close button sits on the top-right border at columns
	// 55-57 (zero-based); mouse coordinates are one-based.
	keys := make(chan string, 2)
	keys <- mouseToken(MouseEvent{Button: MouseButtonLeft, X: 57, Y: 5, Press: true})
	keys <- "esc"
	b.keys = keys
	withQuietStdout(t, func() { b.startJS(page) })
	if b.dialog != nil {
		t.Fatal("dialog is still open")
	}
	if len(keys) != 1 {
		t.Fatal("mouse click did not close the JavaScript dialog")
	}
}

func TestConsoleJavaScriptInputRow(t *testing.T) {
	page := newJSPage(`<html><body><p id="out">before</p></body></html>`, ``)
	b := NewBrowser(60, 20)
	b.Page = page
	withQuietStdout(t, func() { b.startJS(page) })
	b.consoleOpen = true
	b.handleConsoleKey("i")
	if b.consoleEdit {
		t.Fatal("i should not focus the console input")
	}
	b.handleConsoleKey(":")
	if b.consolePane != consoleLogs || !b.consoleEdit {
		t.Fatalf("console state = pane %d edit %v, want logs input", b.consolePane, b.consoleEdit)
	}
	b.consoleText = `document.getElementById("out").textContent = "after"`
	b.consoleCursor = len(b.consoleText)
	b.handleConsoleKey("enter")
	if got := page.Root.FindFirst("p").TextContent(); got != "after" {
		t.Fatalf("input did not run: %q", got)
	}
	if b.consoleText != "" || b.consoleCursor != 0 {
		t.Fatalf("input was not cleared: %q cursor=%d", b.consoleText, b.consoleCursor)
	}
	joined := strings.Join(b.jsLog, "\n")
	if !strings.Contains(joined, "[input]") || !strings.Contains(joined, "[result] after") {
		t.Fatalf("console log = %q", joined)
	}
	if b.consolePane != consoleLogs {
		t.Fatalf("console pane after running input = %d, want logs", b.consolePane)
	}
}

func TestConsoleInputKeepsNavigationKeysAsText(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	b.consolePane = consoleLogs
	b.consoleEdit = true
	b.handleConsoleKey("q")
	b.handleConsoleKey("j")
	b.handleConsoleKey("!")
	if !b.consoleOpen {
		t.Fatal("typing q closed or quit the console")
	}
	if b.consoleText != "qj!" {
		t.Fatalf("console input = %q", b.consoleText)
	}
}

func TestConsoleTabCyclesSections(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	b.handleConsoleKey("tab")
	if b.consolePane != consoleRequests {
		t.Fatalf("pane after first Tab = %d, want requests", b.consolePane)
	}
	b.handleConsoleKey("tab")
	if b.consolePane != consoleStorage {
		t.Fatalf("pane after second Tab = %d, want storage", b.consolePane)
	}
	b.handleConsoleKey("tab")
	if b.consolePane != consoleHTML {
		t.Fatalf("pane after third Tab = %d, want HTML", b.consolePane)
	}
	b.handleConsoleKey("tab")
	if b.consolePane != consoleTools {
		t.Fatalf("pane after fourth Tab = %d, want tools", b.consolePane)
	}
	b.handleConsoleKey("tab")
	if b.consolePane != consoleLogs {
		t.Fatalf("pane after fifth Tab = %d, want logs", b.consolePane)
	}
}

func TestConsoleClickInputRowFocusesIt(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	l := newConsoleLayout(20, consoleLogs)
	if l.inputY < 0 {
		t.Fatal("expected an input row inside the logs section")
	}
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: l.inputY + 1, Press: true})
	if b.consolePane != consoleLogs || !b.consoleEdit {
		t.Fatalf("console state = pane %d edit %v, want logs input", b.consolePane, b.consoleEdit)
	}
	// Clicking the log body above the input row stops editing but keeps logs open.
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: l.bodyY + 1, Press: true})
	if b.consolePane != consoleLogs || b.consoleEdit {
		t.Fatalf("console state after log click = pane %d edit %v", b.consolePane, b.consoleEdit)
	}
}

func TestConsoleInputRowStaysGrayWhenFocused(t *testing.T) {
	b := NewBrowser(60, 20)
	b.color = true
	b.consoleOpen = true
	b.consoleEdit = true
	withQuietStdout(t, func() { b.drawConsole(60, 20) })
	l := newConsoleLayout(20, consoleLogs)
	if l.inputY < 0 {
		t.Fatal("expected an input row")
	}
	const activeBlue = "48;2;47;111;191"
	if strings.Contains(b.prev[l.inputY], activeBlue) {
		t.Fatalf("input row turned blue when focused: %q", b.prev[l.inputY])
	}
	if !strings.Contains(b.prev[l.tabsY], activeBlue) {
		t.Fatalf("logs tab is not highlighted: %q", b.prev[l.tabsY])
	}
}

func TestConsoleInputHistory(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	b.consoleEdit = true
	for _, src := range []string{"one", "two"} {
		b.consoleText = src
		b.consoleCursor = len(src)
		b.handleConsoleKey("enter")
	}
	b.consoleText = "draft"
	b.consoleCursor = len(b.consoleText)
	b.handleConsoleKey("up")
	if b.consoleText != "two" {
		t.Fatalf("first Up = %q, want two", b.consoleText)
	}
	b.handleConsoleKey("up")
	if b.consoleText != "one" {
		t.Fatalf("second Up = %q, want one", b.consoleText)
	}
	b.handleConsoleKey("down")
	if b.consoleText != "two" {
		t.Fatalf("first Down = %q, want two", b.consoleText)
	}
	b.handleConsoleKey("down")
	if b.consoleText != "draft" {
		t.Fatalf("second Down = %q, want the draft", b.consoleText)
	}
	if b.consoleCursor != len(b.consoleText) {
		t.Fatalf("cursor = %d, want end of %q", b.consoleCursor, b.consoleText)
	}
	// Re-running the newest entry does not duplicate it in history.
	b.consoleText = "two"
	b.consoleCursor = len(b.consoleText)
	b.handleConsoleKey("enter")
	if len(b.consoleHist.entries) != 2 {
		t.Fatalf("history = %v, want two entries", b.consoleHist.entries)
	}
}

func TestConsoleResetsOnSuccessfulNavigation(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `console.log("fresh");`)
	b := NewBrowser(40, 10)
	b.jsLog = []string{"[error] old"}
	b.jsErr = true
	b.consoleText = "draft"
	b.consoleCursor = 5
	b.consoleScroll = 3
	b.storageScroll = 2
	b.consolePane = consoleStorage
	b.consoleEdit = true
	withQuietStdout(t, func() {
		b.finishLoad(loadResult{page: page, u: page.URL})
	})
	if len(b.jsLog) != 1 || !strings.Contains(b.jsLog[0], "fresh") {
		t.Fatalf("console log = %v, want only the new page log", b.jsLog)
	}
	if b.jsErr {
		t.Fatal("JS error state survived navigation")
	}
	if b.consoleText != "" || b.consoleCursor != 0 || b.consoleScroll != 0 ||
		b.storageScroll != 0 || b.consolePane != consoleLogs || b.consoleEdit {
		t.Fatalf("console input state survived navigation: %q cursor=%d scroll=%d storage=%d pane=%d edit=%v",
			b.consoleText, b.consoleCursor, b.consoleScroll, b.storageScroll, b.consolePane, b.consoleEdit)
	}
}

func TestConsoleKeepsLogOnFailedNavigation(t *testing.T) {
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(40, 10)
	b.jsLog = []string{"[error] old"}
	withQuietStdout(t, func() {
		b.finishLoad(loadResult{err: errors.New("boom"), u: u})
	})
	if len(b.jsLog) != 1 || b.jsLog[0] != "[error] old" {
		t.Fatalf("failed navigation cleared the log: %v", b.jsLog)
	}
}

func TestConsoleToolsTab(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><body>
			<form id="f" action="/next"><input name="q" value="x"></form>
			<a href="/next">Next page</a>
		</body></html>`,
		"/next": `<html><body>next</body></html>`,
	})
	ctx := context.Background()
	if err := a.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	b := a.b
	b.consoleOpen = true
	b.selectConsolePane(consoleTools)
	if b.consolePane != consoleTools {
		t.Fatalf("pane = %d, want tools", b.consolePane)
	}
	help := strings.Join(b.toolLog, "\n")
	if !strings.Contains(help, "Tools: read-only inspection") || !strings.Contains(help, "mob3 -mcp") {
		t.Fatalf("tools help = %q", help)
	}
	if !strings.Contains(help, "[selector] is an optional CSS selector") ||
		!strings.Contains(help, "links main a") || !strings.Contains(help, "source #content") {
		t.Fatalf("tools help lacks selector examples = %q", help)
	}
	l := newConsoleLayout(b.Height, consoleTools)
	if l.inputY < 0 {
		t.Fatal("tools tab has no input row")
	}

	run := func(cmd string) string {
		t.Helper()
		b.consolePane = consoleTools
		b.consoleEdit = true
		b.consoleText = cmd
		b.consoleCursor = len(cmd)
		b.handleConsoleKey("enter")
		return strings.Join(b.toolLog, "\n")
	}
	if out := run("form"); !strings.Contains(out, "[tool] form") || !strings.Contains(out, "Forms: 1") {
		t.Fatalf("form output = %q", out)
	}
	if b.consoleText != "" || b.consoleCursor != 0 {
		t.Fatalf("tools input not cleared: %q", b.consoleText)
	}
	if out := run("source #f"); !strings.Contains(out, `<form id="f"`) {
		t.Fatalf("source output missing element: %q", out)
	}
	if out := run("links"); !strings.Contains(out, "Next page -> https://example.com/next") {
		t.Fatalf("links output = %q", out)
	}
	if out := run("requests"); !strings.Contains(out, "Requests: 0") {
		t.Fatalf("requests output = %q", out)
	}
	if out := run("nope"); !strings.Contains(out, "[error] unknown tool") {
		t.Fatalf("unknown tool output = %q", out)
	}

	// Clicking the tools input row focuses it without switching panes.
	b.consoleEdit = false
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: l.inputY + 1, Press: true})
	if b.consolePane != consoleTools || !b.consoleEdit {
		t.Fatalf("tools input focus = pane %d edit %v", b.consolePane, b.consoleEdit)
	}
	// A page load keeps the tools scrollback but resets the pane to logs.
	b.resetConsole()
	if len(b.toolLog) == 0 {
		t.Fatal("tools scrollback was cleared by a page load")
	}
	if b.consolePane != consoleLogs {
		t.Fatalf("pane after reset = %d, want logs", b.consolePane)
	}

	b.appendToolLine(strings.Repeat("x", 5000))
	if got := len([]rune(b.toolLog[len(b.toolLog)-1])); got > 4097 {
		t.Fatalf("tool log line = %d runes, want capped at 4097", got)
	}
}

func TestConsoleHistoryCtrlKeysAndFocus(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	b.consoleEdit = true
	for _, src := range []string{"one", "two"} {
		b.consoleText = src
		b.consoleCursor = len(src)
		b.handleConsoleKey("enter")
	}
	b.consoleText = "draft"
	b.consoleCursor = len(b.consoleText)
	b.handleConsoleKey("ctrl-p")
	if b.consoleText != "two" {
		t.Fatalf("first Ctrl-P = %q, want two", b.consoleText)
	}
	b.handleConsoleKey("ctrl-p")
	if b.consoleText != "one" {
		t.Fatalf("second Ctrl-P = %q, want one", b.consoleText)
	}
	b.handleConsoleKey("ctrl-n")
	if b.consoleText != "two" {
		t.Fatalf("first Ctrl-N = %q, want two", b.consoleText)
	}
	// Refocusing the input must not reset the history position or the draft.
	b.focusConsoleInput()
	b.handleConsoleKey("ctrl-n")
	if b.consoleText != "draft" {
		t.Fatalf("Ctrl-N after refocus = %q, want the draft", b.consoleText)
	}

	// Editing a recalled entry preserves the edit as the draft.
	b.consoleText = "two edited"
	b.consoleCursor = len(b.consoleText)
	b.handleConsoleKey("up")
	if b.consoleText != "two" {
		t.Fatalf("Up from edited draft = %q, want two", b.consoleText)
	}
	b.consoleText = "two v2"
	b.consoleCursor = len(b.consoleText)
	b.handleConsoleKey("up")
	b.handleConsoleKey("down")
	b.handleConsoleKey("down")
	if b.consoleText != "two v2" {
		t.Fatalf("edited draft = %q, want two v2", b.consoleText)
	}
}

func TestConsoleToolsHistorySeparateAndStable(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true

	run := func(pane int, src string) {
		t.Helper()
		b.consolePane = pane
		b.consoleEdit = true
		b.consoleText = src
		b.consoleCursor = len(src)
		b.handleConsoleKey("enter")
	}
	// A JS command and two Tools commands must not share one history.
	run(consoleLogs, `1+1`)
	run(consoleTools, "help")
	run(consoleTools, "history")
	if len(b.consoleHist.entries) != 1 || b.consoleHist.entries[0] != "1+1" {
		t.Fatalf("JS history = %v", b.consoleHist.entries)
	}
	if len(b.toolHist.entries) != 2 || b.toolHist.entries[0] != "help" {
		t.Fatalf("tools history = %v", b.toolHist.entries)
	}

	b.consolePane = consoleTools
	b.consoleEdit = true
	b.consoleText = "draft"
	b.consoleCursor = len(b.consoleText)
	b.handleConsoleKey("up") // history
	if b.consoleText != "history" {
		t.Fatalf("first Up = %q", b.consoleText)
	}

	// Clicking the input row keeps the history position instead of resetting it.
	l := newConsoleLayout(b.Height, consoleTools)
	b.handleConsoleMouse(MouseEvent{Button: MouseButtonLeft, X: 5, Y: l.inputY + 1, Press: true})
	b.handleConsoleKey("up")
	if b.consoleText != "help" {
		t.Fatalf("Up after click = %q, want help (position preserved)", b.consoleText)
	}
	b.handleConsoleKey("down")
	b.handleConsoleKey("down")
	if b.consoleText != "draft" {
		t.Fatalf("Down to draft = %q, want draft", b.consoleText)
	}

	// Tools text is not recalled in the JS input history.
	b.consoleText = ""
	b.consoleCursor = 0
	b.selectConsolePane(consoleLogs)
	b.consoleEdit = true
	b.handleConsoleKey("up")
	if b.consoleText != "1+1" {
		t.Fatalf("JS Up = %q, want the JS entry", b.consoleText)
	}
}

func TestConsoleClearCommandAndToolLogRing(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true

	// The Tools scrollback keeps the newest lines instead of freezing at the cap.
	for i := 0; i < toolLogCap+50; i++ {
		b.appendToolLine(fmt.Sprintf("line %d", i))
	}
	if len(b.toolLog) != toolLogCap {
		t.Fatalf("tool log length = %d, want %d", len(b.toolLog), toolLogCap)
	}
	if b.toolLog[0] != "line 50" || b.toolLog[len(b.toolLog)-1] != "line 549" {
		t.Fatalf("tool log range = %q .. %q", b.toolLog[0], b.toolLog[len(b.toolLog)-1])
	}
	b.appendToolLine("fresh output")
	if got := b.toolLog[len(b.toolLog)-1]; got != "fresh output" {
		t.Fatalf("new output after cap = %q", got)
	}
	if strings.Contains(strings.Join(b.toolLog, "\n"), "truncated") {
		t.Fatal("tool log still reports hard truncation")
	}

	// One oversized result keeps its beginning and marks the cut; later output
	// still replaces the oldest lines instead of being dropped.
	var many []string
	for i := 0; i < toolLogCap+100; i++ {
		many = append(many, fmt.Sprintf("big %d", i))
	}
	b.appendToolLines(strings.Join(many, "\n"))
	if len(b.toolLog) != toolLogCap {
		t.Fatalf("large result length = %d, want %d", len(b.toolLog), toolLogCap)
	}
	if b.toolLog[0] != "big 0" || !strings.Contains(b.toolLog[len(b.toolLog)-1], "result truncated") {
		t.Fatalf("large result = %q .. %q", b.toolLog[0], b.toolLog[len(b.toolLog)-1])
	}
	b.appendToolLine("after big result")
	if got := b.toolLog[len(b.toolLog)-1]; got != "after big result" {
		t.Fatalf("output after large result = %q", got)
	}

	run := func(pane int, src string) {
		t.Helper()
		b.consolePane = pane
		b.consoleEdit = true
		b.consoleText = src
		b.consoleCursor = len(src)
		b.handleConsoleKey("enter")
	}
	run(consoleTools, "help")
	if len(b.toolLog) == 0 {
		t.Fatal("tools command produced no output")
	}
	run(consoleTools, "clear")
	if len(b.toolLog) != 0 || b.toolScroll != 0 {
		t.Fatalf("tools clear left %d lines, scroll %d", len(b.toolLog), b.toolScroll)
	}
	run(consoleTools, "help")
	run(consoleTools, "cls")
	if len(b.toolLog) != 0 {
		t.Fatalf("cls left %d lines", len(b.toolLog))
	}

	run(consoleLogs, "1+1")
	if len(b.jsLog) == 0 {
		t.Fatal("JS command produced no log")
	}
	run(consoleLogs, "clear")
	if len(b.jsLog) != 0 || b.jsErr {
		t.Fatalf("JS clear left %d messages, error=%v", len(b.jsLog), b.jsErr)
	}
}

func TestConsoleEscLeavesInputBeforeClosing(t *testing.T) {
	b := NewBrowser(60, 20)
	withQuietStdout(t, func() {
		b.consoleOpen = true
		b.consoleEdit = true
		b.handleConsoleKey("esc")
		if !b.consoleOpen {
			t.Fatal("first Esc closed the console")
		}
		if b.consoleEdit {
			t.Fatal("first Esc did not leave the input row")
		}
		b.handleConsoleKey("esc")
		if b.consoleOpen {
			t.Fatal("second Esc did not close the console")
		}
	})
}

func TestConsoleHTabsAndLTabs(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	b.consolePane = consoleLogs
	b.handleConsoleKey("l")
	if b.consolePane != consoleRequests {
		t.Fatalf("l moved to pane %d, want requests", b.consolePane)
	}
	b.handleConsoleKey("h")
	if b.consolePane != consoleLogs {
		t.Fatalf("h moved to pane %d, want logs", b.consolePane)
	}
	b.handleConsoleKey("backtab")
	if b.consolePane != consoleTools {
		t.Fatalf("Shift-Tab moved to pane %d, want tools", b.consolePane)
	}
	// Letters stay literal while the input row has focus.
	b.consoleEdit = true
	b.consoleText = ""
	b.consoleCursor = 0
	b.handleConsoleKey("h")
	if b.consoleText != "h" || b.consolePane != consoleTools {
		t.Fatalf("focused input: text=%q pane=%d", b.consoleText, b.consolePane)
	}
}

func consoleClipboardText(t *testing.T, buf *bytes.Buffer) string {
	t.Helper()
	out := buf.String()
	if !strings.HasPrefix(out, "\x1b]52;c;") || !strings.HasSuffix(out, "\x07") {
		t.Fatalf("clipboard sequence = %q", out)
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(out, "\x1b]52;c;"), "\x07")
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("clipboard payload: %v", err)
	}
	return string(decoded)
}

func TestConsoleYCopiesActiveSection(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	b.jsLog = []string{"[log] hello", "[error] boom"}
	var buf bytes.Buffer
	b.clipboardWriter = &buf
	b.HandleKey("y")
	if got := consoleClipboardText(t, &buf); got != "[log] hello\n[error] boom" {
		t.Fatalf("log copy = %q", got)
	}
	if b.status != "Copied 24 chars" {
		t.Fatalf("status = %q", b.status)
	}

	// The HTML tab copies the raw source, not the width-wrapped display lines.
	b.Page = &Page{RawHTML: "<html>\n<body>raw source</body>\n</html>"}
	b.selectConsolePane(consoleHTML)
	buf.Reset()
	b.HandleKey("y")
	if got := consoleClipboardText(t, &buf); got != b.Page.RawHTML {
		t.Fatalf("HTML copy = %q", got)
	}
}

func TestSelectPopupActiveRowContrastsWithFrame(t *testing.T) {
	doc := ParseHTML(`<html><body><form>
		<select name="choice"><option>One</option><option selected>Two</option><option>Three</option></select>
	</form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 20)
	b.Page = &Page{URL: u, Base: u, Root: doc, Title: "form"}
	b.URL = u
	b.rebuild()
	sel := doc.FindFirst("select")
	b.openSelectMenu(sel)
	if b.selectMenu == nil {
		t.Fatal("select popup did not open")
	}
	g := NewGrid(60, 20, RGB(0, 0, 0), RGB(255, 255, 255))
	b.paintSelectMenu(g)
	if b.modalBox.W == 0 || len(b.selectHits) == 0 {
		t.Fatalf("select popup was not painted: box=%+v hits=%d", b.modalBox, len(b.selectHits))
	}
	box := b.modalBox
	borderBG := g.Cells[box.Y*g.W+box.X].BG
	titleBG := g.Cells[(box.Y+1)*g.W+box.X+1].BG
	row := b.selectHits[b.selectMenu.idx]
	rowBG := g.Cells[row.rect.Y*g.W+row.rect.X+1].BG
	if rowBG == borderBG || rowBG == titleBG {
		t.Fatalf("active option background %+v matches the window frame, border %+v title %+v",
			rowBG, borderBG, titleBG)
	}
	if rowBG == modalBG {
		t.Fatalf("active option background %+v is the plain modal body", rowBG)
	}
}

func TestConsoleYTypesWhileEditingInput(t *testing.T) {
	b := NewBrowser(60, 20)
	b.consoleOpen = true
	b.consoleEdit = true
	b.consoleText = ""
	b.consoleCursor = 0
	var buf bytes.Buffer
	b.clipboardWriter = &buf
	b.HandleKey("y")
	if b.consoleText != "y" {
		t.Fatalf("console input = %q, want the typed letter", b.consoleText)
	}
	if buf.Len() != 0 {
		t.Fatalf("typing copied text: %q", buf.String())
	}
}

func TestConsoleKeepsDialogOverlay(t *testing.T) {
	b := NewBrowser(100, 12)
	b.consoleOpen = true
	b.dialog = &jsDialog{kind: "alert", message: "blocked"}
	drawQuiet(b)
	if b.modalBox.W <= 0 {
		t.Fatal("console draw did not paint the script dialog")
	}
}

func TestConsoleFooterHintAdvertisesCopyKey(t *testing.T) {
	b := NewBrowser(100, 12)
	b.consoleOpen = true
	withQuietStdout(t, func() { b.drawConsole(100, 12) })
	text := strings.Join(b.prev, "\n")
	if !strings.Contains(text, "y: copy all") {
		t.Fatalf("console hint does not advertise y: %q", text)
	}
	if strings.Contains(text, "Shift+drag") {
		t.Fatalf("console hint still mentions Shift+drag: %q", text)
	}
}
