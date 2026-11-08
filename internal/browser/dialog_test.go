package browser

import (
	"os"
	"strings"
	"testing"
	"time"
)

func withQuietStdout(t *testing.T, fn func()) {
	t.Helper()
	old := os.Stdout
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err == nil {
		os.Stdout = f
		defer func() { os.Stdout = old; f.Close() }()
	}
	fn()
}

func TestJSConfirm(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`,
		`document.title = confirm("Continue?") ? "yes" : "no";`)
	b := NewBrowser(60, 15)
	b.page = page
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
	b.page = page
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
	b.page = page
	withQuietStdout(t, func() { b.startJS(page) })
	if page.Title != "false:null" {
		t.Fatalf("title = %q", page.Title)
	}
	if len(b.jsLog) < 3 {
		t.Fatalf("dialogs were not logged: %v", b.jsLog)
	}
}

func TestHelpAlertExample(t *testing.T) {
	page, err := NewLoader().Load("about:help", nil)
	if err != nil {
		t.Fatal(err)
	}
	var link *Node
	page.Root.FindAll(func(n *Node) bool {
		if link == nil && n.Tag == "a" && strings.Contains(n.Attr("onclick"), "alert(") {
			link = n
		}
		return false
	})
	if link == nil {
		t.Fatal("alert example link not found")
	}
	b := NewBrowser(80, 24)
	b.page = page
	keys := make(chan string, 2)
	keys <- "enter"
	b.keys = keys
	withQuietStdout(t, func() {
		b.startJS(page)
		b.js.dispatchClick(link)
	})
	found := false
	for _, line := range b.jsLog {
		if strings.Contains(line, "Hello from MOB³!") {
			found = true
		}
	}
	if !found {
		t.Fatalf("alert example did not run: %v", b.jsLog)
	}
}

func TestConsolePausesTimers(t *testing.T) {
	page := newJSPage(`<html><body><p id="out">before</p></body></html>`, `
		setTimeout(function () {
			document.getElementById("out").textContent = "timer";
		}, 10);
	`)
	b := NewBrowser(60, 15)
	b.page = page
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

func TestDialogRendering(t *testing.T) {
	b := NewBrowser(60, 15)
	b.dialog = &jsDialog{kind: "confirm", message: "Continue with this action?"}
	drawQuiet(b)
	text := strings.Join(b.prev, "\n")
	if !strings.Contains(text, "JavaScript confirm") || !strings.Contains(text, "Continue with this action?") {
		t.Fatalf("dialog not rendered: %q", text)
	}
}
