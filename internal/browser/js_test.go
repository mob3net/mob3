package browser

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newJSPage(html, code string) *Page {
	doc := ParseHTML(html)
	u, _ := url.Parse("https://example.com/")
	return &Page{
		URL: u, Base: u, Root: doc, Title: "test",
		Scripts: []Script{{Code: code, Src: "inline script"}},
	}
}

func TestSelectorParseCache(t *testing.T) {
	page := &Page{Root: ParseHTML(`<html><body><p class="x">hi</p></body></html>`)}
	b := NewBrowser(40, 10)
	j := newJSRuntime(b, page)
	a := j.parsedSelectors(".x")
	c := j.parsedSelectors(".x")
	if len(a) != 1 || len(c) != 1 || a[0] != c[0] {
		t.Fatal("selector parsing was not cached")
	}
}

func TestJSScriptMutatesDOM(t *testing.T) {
	page := newJSPage(`<html><body><p id="out">before</p></body></html>`,
		`document.getElementById("out").textContent = "after";`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if b.js == nil {
		t.Fatal("runtime not created")
	}
	if got := page.Root.FindFirst("p").TextContent(); got != "after" {
		t.Fatalf("text = %q", got)
	}
}

func TestJSInputValue(t *testing.T) {
	page := newJSPage(`<html><body><input id="q" value="before"></body></html>`,
		`document.getElementById("q").value = "after";`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	input := page.Root.FindFirst("input")
	if got := inputValue(input); got != "after" {
		t.Fatalf("value = %q", got)
	}
}

func TestJSWindowIsGlobal(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		window.fromWindow = 41;
		var fromVar = 1;
		document.title = String(fromWindow + fromVar) + ":" +
			String(window.fromVar) + ":" + String(window === globalThis);
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if page.Title != "42:1:true" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSDOMInsertBefore(t *testing.T) {
	page := newJSPage(`<html><body><div id="a"><span id="b">b</span><span id="c">c</span></div></body></html>`, `
		var a = document.getElementById("a");
		var b = document.getElementById("b");
		var c = document.getElementById("c");
		var n = document.createElement("i");
		n.textContent = "x";
		a.insertBefore(n, c);
		var frag = document.createDocumentFragment();
		var em = document.createElement("em");
		frag.appendChild(em);
		a.appendChild(frag);
		var clone = a.cloneNode(true);
		document.title = a.children.length + ":" + a.children[1].tagName + ":" +
			clone.children.length + ":" + a.contains(b) + ":" + b.matches("span#b");
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	want := "4:I:4:true:true"
	if page.Title != want {
		t.Fatalf("title = %q, want %q", page.Title, want)
	}
}

func TestJSWebAPIs(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		var u = new URL("/search?q=x", "https://example.com/base");
		var p = new URLSearchParams(u.search);
		document.cookie = "a=1";
		document.write("<p id='w'>written</p>");
		localStorage.setItem("k", "v");
		document.title = [
			u.hostname, u.pathname, p.get("q"), document.cookie,
			document.getElementById("w").textContent, localStorage.getItem("k")
		].join("|");
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	want := "example.com|/search|x|a=1|written|v"
	if page.Title != want {
		t.Fatalf("title = %q, want %q", page.Title, want)
	}
}

func TestJSXMLHTTPRequest(t *testing.T) {
	loader := NewLoader()
	var gotMethod, gotPath, gotBody string
	loader.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotMethod = req.Method
		gotPath = req.URL.Path
		b, _ := io.ReadAll(req.Body)
		gotBody = string(b)
		h := make(http.Header)
		h.Set("Content-Type", "text/plain")
		h.Set("X-Test", "yes")
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(strings.NewReader("ok")), Request: req,
		}, nil
	})}
	page := newJSPage(`<html><body></body></html>`, `
		var x = new XMLHttpRequest();
		x.open("POST", "/api");
		x.setRequestHeader("Content-Type", "text/plain");
		x.onload = function () {
			document.title = x.status + "|" + x.responseText + "|" + x.getResponseHeader("X-Test");
		};
		x.send("abc");
	`)
	b := NewBrowser(40, 10)
	b.loader = loader
	b.startJS(page)
	if page.Title != "200|ok|yes" {
		t.Fatalf("title = %q", page.Title)
	}
	if gotMethod != "POST" || gotPath != "/api" || gotBody != "abc" {
		t.Fatalf("request = %s %s body %q", gotMethod, gotPath, gotBody)
	}
}

func TestJSXHRCrossOriginBlocked(t *testing.T) {
	loader := NewLoader()
	calls := 0
	loader.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		h := make(http.Header)
		h.Set("Content-Type", "text/plain")
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(strings.NewReader("secret")), Request: req,
		}, nil
	})}
	page := newJSPage(`<html><body></body></html>`, `
		var x = new XMLHttpRequest();
		x.open("GET", "https://other.example/account");
		x.onerror = function () { document.title = "blocked:" + x.status; };
		x.send();
	`)
	b := NewBrowser(40, 10)
	b.loader = loader
	b.startJS(page)
	if calls != 0 {
		t.Fatalf("cross-origin XHR made %d requests", calls)
	}
	if page.Title != "blocked:0" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSXHRCrossOriginRedirectBlocked(t *testing.T) {
	loader := NewLoader()
	calls := 0
	loader.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		h := make(http.Header)
		h.Set("Location", "https://other.example/secret")
		return &http.Response{
			StatusCode: 302, Status: "302 Found", Header: h,
			Body: io.NopCloser(strings.NewReader("")), Request: req,
		}, nil
	})}
	page := newJSPage(`<html><body></body></html>`, `
		var x = new XMLHttpRequest();
		x.open("GET", "/start");
		x.onerror = function () { document.title = "blocked:" + x.status; };
		x.send();
	`)
	b := NewBrowser(40, 10)
	b.loader = loader
	b.startJS(page)
	if calls != 1 {
		t.Fatalf("redirect was followed: %d requests", calls)
	}
	if page.Title != "blocked:0" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestRuntimeWithoutScripts(t *testing.T) {
	page := &Page{URL: mustParse(t, "https://example.com/"), Root: ParseHTML(`<html><body><a href="#" onclick="alert('x'); return false;">x</a></body></html>`)}
	b := NewBrowser(40, 10)
	b.page = page
	b.startJS(page)
	if b.js == nil {
		t.Fatal("runtime was not created for a page with only inline handlers")
	}
}

func TestJSNavigator(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`,
		`document.title = navigator.userAgent + "|" + navigator.language;`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if !strings.HasPrefix(page.Title, "MOB3/1.0") ||
		!strings.HasSuffix(page.Title, "|en-US") {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSLocationRedirect(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `location.href = "/next";`)
	b := NewBrowser(40, 10)
	b.page = page
	b.startJS(page)
	if b.js == nil || b.js.pendingNav != "https://example.com/next" {
		t.Fatalf("pending navigation = %q", func() string {
			if b.js == nil {
				return "<nil>"
			}
			return b.js.pendingNav
		}())
	}
}

func TestMetaRefreshTarget(t *testing.T) {
	u := mustParse(t, "https://example.com/old")
	page := &Page{URL: u, Base: u, Root: ParseHTML(`<html><head><meta http-equiv="refresh" content="0; url=/next"></head></html>`)}
	if got := metaRefreshTarget(page); got != "https://example.com/next" {
		t.Fatalf("meta refresh target = %q", got)
	}
}

func TestJSBrowserGlobals(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		var img = new Image();
		img.src = "x.png";
		document.title = typeof performance.now() + ":" + img.tagName + ":" +
			img.src + ":" + location.hostname + ":" + typeof requestAnimationFrame;
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	want := "number:IMG:x.png:example.com:function"
	if page.Title != want {
		t.Fatalf("title = %q, want %q", page.Title, want)
	}
}

func TestJSConsoleLogAndView(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `console.error("navigator is not defined");`)
	b := NewBrowser(40, 10)
	b.page = page
	b.startJS(page)
	if len(b.jsLog) != 1 || !strings.Contains(b.jsLog[0], "navigator is not defined") {
		t.Fatalf("log = %v", b.jsLog)
	}
	b.consoleOpen = true
	drawQuiet(b)
	if !strings.Contains(strings.Join(b.prev, "\n"), "navigator is not defined") {
		t.Fatal("console view did not show the log")
	}
}

func TestJSDOMContentLoaded(t *testing.T) {
	page := newJSPage(`<html><body><p id="out">x</p></body></html>`,
		`document.addEventListener("DOMContentLoaded", function () {
			document.body.appendChild(document.createElement("p")).textContent = "loaded";
		});`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if got := page.Root.FindFirst("body").TextContent(); !strings.Contains(got, "loaded") {
		t.Fatalf("body text = %q", got)
	}
}

func TestJSClassStyleAndInnerHTML(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		var d = document.createElement("div");
		d.innerHTML = "<b>hi</b>";
		d.className = "one";
		d.classList.add("two");
		d.style.color = "red";
		document.body.appendChild(d);
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	body := page.Root.FindFirst("body")
	d := body.FindFirst("div")
	if d == nil {
		t.Fatal("div was not appended")
	}
	if !strings.Contains(attrValue(d, "class"), "two") {
		t.Errorf("class = %q", attrValue(d, "class"))
	}
	if !strings.Contains(attrValue(d, "style"), "color: red") {
		t.Errorf("style = %q", attrValue(d, "style"))
	}
	if d.FindFirst("b") == nil {
		t.Error("innerHTML did not create <b>")
	}
	if got := d.FindFirst("b").TextContent(); got != "hi" {
		t.Errorf("innerHTML text = %q", got)
	}
}

func TestJSClassListToggleForce(t *testing.T) {
	page := newJSPage(`<html><body><div id="d">x</div></body></html>`, `
		var d = document.getElementById("d");
		d.classList.add("one");
		d.classList.toggle("one", true);
		d.classList.toggle("two", true);
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	div := page.Root.FindFirst("div")
	if got := attrValue(div, "class"); got != "one two" {
		t.Fatalf("class = %q, want %q", got, "one two")
	}
}

func TestJSClickHandlerPreventsDefault(t *testing.T) {
	page := newJSPage(`<html><body><a id="l" href="/next">go</a></body></html>`, `
		var link = document.getElementById("l");
		link.addEventListener("click", function (e) {
			e.preventDefault();
			link.textContent = "clicked";
		});
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	link := page.Root.FindFirst("a")
	if !b.js.dispatchClick(link) {
		t.Fatal("click default was not prevented")
	}
	if got := link.TextContent(); got != "clicked" {
		t.Fatalf("link text = %q", got)
	}
}

func TestJSListenerOnce(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		var n = 0;
		document.addEventListener("tick", function () {
			n++;
			document.title = "n" + n;
		}, { once: true });
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	b.js.fireDocumentEvent("tick")
	b.js.fireDocumentEvent("tick")
	if page.Title != "n1" {
		t.Fatalf("title = %q, want n1", page.Title)
	}
}

func TestJSTimers(t *testing.T) {
	page := newJSPage(`<html><body><p id="out">before</p></body></html>`, `
		setTimeout(function () {
			document.getElementById("out").textContent = "timer";
		}, 10);
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if b.js.tick(time.Now()) {
		t.Fatal("timer fired immediately")
	}
	if !b.js.tick(time.Now().Add(time.Second)) {
		t.Fatal("timer did not fire")
	}
	if got := page.Root.FindFirst("p").TextContent(); got != "timer" {
		t.Fatalf("text = %q", got)
	}
	if len(b.js.timers) != 0 {
		t.Error("one-shot timer was not removed")
	}
}

func TestNoscriptHiddenWhenJSEnabled(t *testing.T) {
	page := &Page{Root: ParseHTML(`<html><body><noscript>fallback</noscript><p>ok</p></body></html>`)}
	b := NewBrowser(40, 10)
	b.startJS(page)
	if page.Root.FindFirst("noscript") != nil {
		t.Fatal("noscript should be removed when JS is enabled")
	}
	page2 := &Page{Root: ParseHTML(`<html><body><noscript>fallback</noscript></body></html>`)}
	b2 := NewBrowser(40, 10)
	b2.noJS = true
	b2.startJS(page2)
	if page2.Root.FindFirst("noscript") == nil {
		t.Fatal("noscript should remain visible with -no-js")
	}
}

func TestNoJSDisablesRuntime(t *testing.T) {
	page := newJSPage(`<html><body><p id="out">before</p></body></html>`,
		`document.getElementById("out").textContent = "after";`)
	b := NewBrowser(40, 10)
	b.noJS = true
	b.startJS(page)
	if b.js != nil {
		t.Fatal("runtime created despite -no-js")
	}
	if got := page.Root.FindFirst("p").TextContent(); got != "before" {
		t.Fatalf("text = %q", got)
	}
}

func TestExternalScriptCollected(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "page.html"),
		[]byte(`<html><body><p id="out">before</p><script src="app.js"></script></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"),
		[]byte(`document.getElementById("out").textContent = "external";`), 0o644); err != nil {
		t.Fatal(err)
	}
	u := mustParse(t, "file://"+filepath.Join(dir, "page.html"))
	page, err := NewLoader().Load(u.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Scripts) != 1 {
		t.Fatalf("scripts = %d", len(page.Scripts))
	}
	b := NewBrowser(40, 10)
	b.startJS(page)
	if got := page.Root.FindFirst("p").TextContent(); got != "external" {
		t.Fatalf("text = %q", got)
	}
}
