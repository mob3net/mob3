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

func TestJSDialogElement(t *testing.T) {
	page := newJSPage(`<html><body><dialog id="d"><p>body</p></dialog></body></html>`, `
		var d = document.getElementById("d");
		var closes = 0;
		d.addEventListener("close", function () { closes++; });
		var initiallyOpen = d.open;
		d.showModal();
		var opened = d.open;
		d.close("dismissed");
		var closed = d.open;
		var returnValue = d.returnValue;
		d.show();
		var reopened = d.open;
		d.removeAttribute("open");
		document.title = [initiallyOpen, opened, closed, returnValue,
			reopened, closes, d.open, d.returnValue].join("|");
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	want := "false|true|false|dismissed|true|1|false|dismissed"
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

func TestJSElementConstructors(t *testing.T) {
	page := newJSPage(`<html><body><script id="s"></script></body></html>`, `
		var s = document.getElementById("s");
		var made = document.createElement("script");
		document.title = [
			s instanceof HTMLScriptElement,
			made instanceof HTMLScriptElement,
			document.body instanceof HTMLBodyElement,
			document.body instanceof HTMLElement,
			document.body instanceof Element,
			document.body instanceof Node,
			document instanceof HTMLDocument,
			s.constructor === HTMLScriptElement
		].join("|");
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	want := "true|true|true|true|true|true|true|true"
	if page.Title != want {
		t.Fatalf("title = %q, want %q", page.Title, want)
	}
}

func TestJSFetch(t *testing.T) {
	loader := NewLoader()
	var gotMethod, gotPath, gotBody, gotHeader string
	loader.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotMethod = req.Method
		gotPath = req.URL.Path
		gotHeader = req.Header.Get("X-Test")
		b, _ := io.ReadAll(req.Body)
		gotBody = string(b)
		h := make(http.Header)
		h.Set("Content-Type", "application/json")
		h.Set("X-Answer", "42")
		return &http.Response{
			StatusCode: 201, Status: "201 Created", Header: h,
			Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Request: req,
		}, nil
	})}
	page := newJSPage(`<html><body></body></html>`, `
		fetch("/api", {method: "POST", headers: {"X-Test": "yes"}, body: "abc"})
			.then(function (r) {
				var meta = [r.status, r.ok, r.headers.get("X-Answer")].join("|");
				return r.json().then(function (j) { return meta + "|" + j.ok; });
			})
			.then(function (s) { document.title = s; })
			.catch(function (e) { document.title = "error:" + e; });
	`)
	b := NewBrowser(40, 10)
	b.loader = loader
	b.startJS(page)
	if want := "201|true|42|true"; page.Title != want {
		t.Fatalf("title = %q, want %q (logs %v, status %q)", page.Title, want, b.jsLog, b.status)
	}
	if gotMethod != "POST" || gotPath != "/api" || gotBody != "abc" || gotHeader != "yes" {
		t.Fatalf("request = %s %s body %q header %q", gotMethod, gotPath, gotBody, gotHeader)
	}
}

func TestJSFetchCrossOriginBlocked(t *testing.T) {
	loader := NewLoader()
	calls := 0
	loader.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader("secret")), Request: req,
		}, nil
	})}
	page := newJSPage(`<html><body></body></html>`, `
		fetch("https://other.example/account")
			.then(function () { document.title = "leaked"; })
			.catch(function () { document.title = "blocked"; });
	`)
	b := NewBrowser(40, 10)
	b.loader = loader
	b.startJS(page)
	if calls != 0 {
		t.Fatalf("cross-origin fetch made %d requests", calls)
	}
	if page.Title != "blocked" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSIntl(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		var n = new Intl.NumberFormat("en-US", {style: "currency", currency: "USD"});
		var d = new Intl.DateTimeFormat("en-US", {
			year: "numeric", month: "2-digit", day: "2-digit", timeZone: "UTC"
		});
		var date = new Date(Date.UTC(2020, 0, 2, 3, 4, 5));
		var rel = new Intl.RelativeTimeFormat("en-US");
		var list = new Intl.ListFormat("en-US");
		var plural = new Intl.PluralRules("en-US");
		document.title = [
			n.format(1234.5), d.format(date), rel.format(-3, "day"),
			list.format(["a", "b", "c"]), plural.select(1), plural.select(2)
		].join("|");
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	want := "$1,234.50|01/02/2020|3 days ago|a, b, and c|one|other"
	if page.Title != want {
		t.Fatalf("title = %q, want %q (logs %v, status %q)", page.Title, want, b.jsLog, b.status)
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

func TestJSFormSubmitQueuesPost(t *testing.T) {
	page := newJSPage(`<html><body><form id="f" method="POST" action="/access_keys">
		<input type="hidden" name="authenticity_token" value="abc">
		<input type="hidden" name="_method" value="POST">
		<input name="try_url" value="https://example.com">
	</form></body></html>`, `document.getElementById("f").submit();`)
	b := NewBrowser(40, 10)
	b.page = page
	b.startJS(page)
	if b.js == nil || b.js.pendingForm == nil {
		t.Fatal("form.submit() did not queue a submission")
	}
	pf := b.js.pendingForm
	if pf.method != http.MethodPost || pf.target != "https://example.com/access_keys" {
		t.Fatalf("queued form = %s %s", pf.method, pf.target)
	}
	body := string(pf.body)
	for _, want := range []string{"authenticity_token=abc", "_method=POST", "try_url=https%3A%2F%2Fexample.com"} {
		if !strings.Contains(body, want) {
			t.Fatalf("form body %q is missing %q", body, want)
		}
	}
	if pf.contentType != "application/x-www-form-urlencoded" {
		t.Fatalf("content type = %q", pf.contentType)
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

func TestStorageNamedProperties(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		localStorage.theirValue = "hello";
		var read = localStorage.theirValue;
		var viaGet = localStorage.getItem("theirValue");
		delete localStorage.theirValue;
		var afterDelete = localStorage.theirValue;
		sessionStorage.setItem("k", "v");
		var named = sessionStorage.k;
		document.title = [read, viaGet, afterDelete === undefined, named].join("|");
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	want := "hello|hello|true|v"
	if page.Title != want {
		t.Fatalf("title = %q, want %q", page.Title, want)
	}
	if _, ok := b.js.storageLocalData["theirValue"]; ok {
		t.Fatal("deleted storage key is still present")
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

func TestSelectChangeFiresJavaScript(t *testing.T) {
	page := newJSPage(`<html><body><select id="s" name="choice">
		<option value="one" selected>One</option><option value="two">Two</option>
	</select></body></html>`, `
		var sel = document.getElementById("s");
		sel.addEventListener("input", function () {
			document.title = "input:" + sel.value;
		});
		sel.addEventListener("change", function () {
			document.title = "change:" + sel.value;
		});
	`)
	b := NewBrowser(40, 10)
	b.page = page
	b.url = page.URL
	b.startJS(page)
	b.rebuild()
	sel := page.Root.FindFirst("select")
	b.toggleControl(sel)
	b.HandleKey("down")
	b.HandleKey("enter")
	if page.Title != "change:two" {
		t.Fatalf("title = %q, want change:two", page.Title)
	}
}

func TestClickContentRunsLivePageJavaScript(t *testing.T) {
	page := newJSPage(`<html><body><a id="l" href="/next">go</a></body></html>`, `
		var link = document.getElementById("l");
		link.addEventListener("click", function (e) {
			e.preventDefault();
			link.textContent = "clicked";
		});
	`)
	b := NewBrowser(40, 10)
	b.page = page
	b.url = page.URL
	b.startJS(page)
	b.rebuild()
	link := page.Root.FindFirst("a")
	if len(b.layout.Links) == 0 || len(b.layout.Links[0].Rects) == 0 {
		t.Fatal("link has no click rectangle")
	}
	r := b.layout.Links[0].Rects[0]
	b.clickContent(r.X, r.Y)
	if got := link.TextContent(); got != "clicked" {
		t.Fatalf("live page click handler did not run, text = %q", got)
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

func TestJSMemoryLimitDefault(t *testing.T) {
	b := NewBrowser(40, 10)
	if b.jsMemLimit != defaultJSMemoryLimit {
		t.Fatalf("jsMemLimit = %d, want %d", b.jsMemLimit, int64(defaultJSMemoryLimit))
	}
}

func TestJSMemoryLimitExceeded(t *testing.T) {
	b := NewBrowser(40, 10)
	b.jsMemLimit = 1
	j := newJSRuntime(b, newJSPage(`<html><body></body></html>`, ""))
	_, err := j.runCode("alloc", `var a = []; for (;;) { a.push(new Array(4096).fill("x")); }`)
	if err == nil || !strings.Contains(err.Error(), "memory limit exceeded") {
		t.Fatalf("err = %v, want memory limit exceeded", err)
	}
}

func TestJSMemoryLimitZeroDisables(t *testing.T) {
	b := NewBrowser(40, 10)
	b.jsMemLimit = 0
	j := newJSRuntime(b, newJSPage(`<html><body></body></html>`, ""))
	_, err := j.runCode("alloc", `var a = []; for (var i = 0; i < 1000; i++) { a.push(new Array(1024).fill(i)); }`)
	if err != nil {
		t.Fatalf("runCode with watchdog disabled: %v", err)
	}
}

func TestJSListenerOptionsWithoutOnce(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		var n = 0;
		function bump() { n++; }
		document.addEventListener("tick", bump, { capture: true });
		document.dispatchEvent(new Event("tick"));
		document.title = "n" + n;
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if page.Title != "n1" {
		t.Fatalf("title = %q, want n1", page.Title)
	}
}

func TestJSCreateEvent(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		var e = document.createEvent("Event");
		e.initEvent("foo", true, true);
		e.preventDefault();
		var d = document.createEvent("CustomEvent");
		d.initCustomEvent("bar", false, false, 42);
		var m = document.createEvent("MouseEvent");
		m.initMouseEvent("click", true, true, null, 0, 1, 2, 3, 4, false, false, false, false, 0, null);
		var t = document.createTextNode("x");
		document.title = [
			e.defaultPrevented ? "1" : "0",
			e instanceof Event ? "1" : "0",
			d.detail,
			m.clientX + ":" + m.clientY,
			t instanceof CharacterData ? "1" : "0"
		].join(",");
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if page.Title != "1,1,42,3:4,1" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSDispatchEventBubbles(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		var log = "";
		document.addEventListener("custom", function (e) { log += "d" + e.detail; });
		document.body.addEventListener("custom", function () { log += "b"; });
		var el = document.createElement("div");
		document.body.appendChild(el);
		el.addEventListener("custom", function () { log += "t"; });
		el.dispatchEvent(new CustomEvent("custom", { bubbles: true, detail: 3 }));
		el.dispatchEvent(new CustomEvent("custom", { bubbles: false, detail: 4 }));
		document.title = log;
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if page.Title != "tbd3t" {
		t.Fatalf("title = %q, want tbd3t", page.Title)
	}
}

func TestJSCanvasGetContext(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		var c = document.createElement("canvas");
		var ctx = c.getContext("2d");
		var img = ctx.getImageData(0, 0, 2, 3);
		var missing = c.getContext("webgl");
		ctx.fillStyle = "red";
		ctx.fillRect(0, 0, 1, 1);
		document.title = [!!ctx, missing === null, img.width, img.height, img.data.length, c.width, ctx.fillStyle].join(",");
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if page.Title != "true,true,2,3,24,300,red" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSMessageChannel(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		var mc = new MessageChannel();
		mc.port2.onmessage = function (e) { document.title = "got" + e.data; };
		mc.port1.postMessage(7);
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if page.Title != "got" && !b.js.tick(time.Now().Add(time.Second)) {
		t.Fatal("message timer did not fire")
	}
	if page.Title != "got7" {
		t.Fatalf("title = %q, want got7", page.Title)
	}
}

func TestJSPerformanceTiming(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		document.title = typeof performance.timing.responseStart + ":" +
			typeof performance.navigation.type + ":" + performance.timing.responseStart;
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if !strings.HasPrefix(page.Title, "number:number:") {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSStyleFeatureDetection(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		var s = document.documentElement.style;
		document.title = ["transition", "transform", "WebkitTransition", "WebkitTransform"].map(function (k) {
			return k + ":" + (k in s);
		}).join(",");
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if page.Title != "transition:true,transform:true,WebkitTransition:true,WebkitTransform:true" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSDocumentImplementation(t *testing.T) {
	page := newJSPage(`<html><body><p id="keep">x</p></body></html>`, `
		var d = document.implementation.createHTMLDocument("made");
		d.body.innerHTML = "<form></form><form></form>";
		var feature = document.implementation.hasFeature(
			"http://www.w3.org/TR/SVG11/feature#BasicStructure", "1.1");
		document.title = [
			feature,
			d.body.childNodes.length,
			d.documentElement.ownerDocument.defaultView === window,
			document.defaultView === window,
			d.title
		].join(",");
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if page.Title != "true,2,true,true,made" {
		t.Fatalf("title = %q", page.Title)
	}
	if page.Root.FindFirst("p") == nil {
		t.Fatal("detached document mutated the page")
	}
}

func TestJSGetComputedStyle(t *testing.T) {
	page := newJSPage(`<html><body style="margin:8px;font-size:20px"><p id="p">x</p></body></html>`, `
		var el = document.getElementById("p");
		var s = document.defaultView.getComputedStyle(el);
		document.title = [
			typeof window.getComputedStyle,
			typeof s.getPropertyValue,
			s.display,
			s.getPropertyValue("display"),
			s.getPropertyValue("font-size"),
			s.fontSize,
			typeof s.marginTop,
			s.getPropertyValue("font-weight")
		].join(",");
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if !strings.HasPrefix(page.Title, "function,function,block,block,") {
		t.Fatalf("title = %q", page.Title)
	}
	if !strings.HasSuffix(page.Title, ",400") {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSDOMCompatTier(t *testing.T) {
	page := newJSPage(`<html><body><p id="t">hello world</p></body></html>`, `
		var el = document.createElement("div");
		el.dataset.userId = "42";
		var read = el.dataset.userId;
		el.setAttribute("data-x", "y");
		var names = el.getAttributeNames().sort().join("|");
		el.toggleAttribute("hidden");
		var added = el.hasAttribute("hidden");
		el.toggleAttribute("hidden");
		var removed = !el.hasAttribute("hidden");
		document.body.appendChild(el);
		var s = document.createElement("span");
		el.insertAdjacentElement("beforebegin", s);
		var b = document.createElement("b");
		b.textContent = "t";
		el.insertAdjacentElement("beforeend", b);
		var html = el.outerHTML;
		var pos = el.compareDocumentPosition(s);
		el.appendChild(document.createTextNode("a"));
		el.appendChild(document.createTextNode("b"));
		el.normalize();
		var kids = el.childNodes.length;
		document.title = [
			read, names, added, removed, pos,
			html.indexOf("<b>t</b>") >= 0, kids,
			el.getClientRects().length
		].join(",");
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if page.Title != "42,data-user-id|data-x,true,true,2,true,2,1" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSTreeWalkerAndRange(t *testing.T) {
	page := newJSPage(`<html><body><p id="t">hello world</p></body></html>`, `
		var root = document.createElement("div");
		root.innerHTML = "<p>a</p><span>b</span>";
		document.body.appendChild(root);
		var names = [];
		var w = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT);
		var n;
		while ((n = w.nextNode())) names.push(n.tagName);
		var names2 = [];
		var it = document.createNodeIterator(root, NodeFilter.SHOW_ELEMENT);
		while ((n = it.nextNode())) names2.push(n.tagName);
		var r = document.createRange();
		r.selectNodeContents(document.getElementById("t"));
		var sel = document.getSelection();
		sel.removeAllRanges();
		sel.addRange(r);
		document.title = [
			names.join("|"), names2.join("|"),
			r.toString(), sel.rangeCount, sel.isCollapsed, sel.toString()
		].join(",");
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if page.Title != "P|SPAN,DIV|P|SPAN,hello world,1,false,hello world" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSTextCryptoAndClone(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		var enc = new TextEncoder();
		var dec = new TextDecoder();
		var bytes = enc.encode("héllo");
		var back = dec.decode(bytes);
		var arr = new Uint8Array(4);
		crypto.getRandomValues(arr);
		var uuid = crypto.randomUUID();
		var src = { a: 1, b: [1, 2] };
		src.self = src;
		var c = structuredClone(src);
		var distinct = !crypto.getRandomValues(new Uint8Array(4)).every(function (v, i) { return v === arr[i]; });
		document.title = [
			back, bytes.length, uuid.length, c.a, c.b[1], c.self === c, distinct
		].join(",");
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if page.Title != "héllo,6,36,1,2,true,true" && page.Title != "héllo,6,36,1,2,true,false" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSBase64(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		var enc = btoa("hello");
		var dec = atob(enc);
		var bin = "";
		for (var i = 0; i < 256; i++) bin += String.fromCharCode(i);
		var round = atob(btoa(bin)) === bin;
		var forgiving = atob(" aGVs\nbG8 ") + "," + atob("aGVsbG8");
		var thrown = false;
		try { btoa("\u0100"); } catch (e) { thrown = true; }
		document.title = [enc, dec, round, forgiving, thrown].join(",");
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	want := "aGVsbG8=,hello,true,hello,hello,true"
	if page.Title != want {
		t.Fatalf("title = %q, want %q", page.Title, want)
	}
}

func TestJSAbortController(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		var ac = new AbortController();
		var fired = "";
		ac.signal.addEventListener("abort", function (e) { fired = e.type; });
		var before = ac.signal.aborted;
		ac.abort();
		document.title = [before, ac.signal.aborted, fired, ac.signal.reason.name].join(",");
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if page.Title != "false,true,abort,AbortError" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSFetchAborted(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		var ac = new AbortController();
		ac.abort();
		fetch("/never", { signal: ac.signal }).then(
			function () { document.title = "resolved"; },
			function (e) { document.title = "rejected:" + e.name; }
		);
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if page.Title != "rejected:AbortError" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSBlobFileReaderFormData(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		var blob = new Blob(["hi"], { type: "text/plain" });
		var fd = new FormData();
		fd.append("a", "1");
		fd.append("b", "2");
		fd.set("a", "3");
		var entries = [];
		fd.forEach(function (v, k) { entries.push(k + "=" + v); });
		var url = URL.createObjectURL(blob);
		var fr = new FileReader();
		fr.onload = function () {
			document.title = [
				blob.size, blob.type, fd.get("a"), fd.getAll("a").length,
				fd.has("b"), entries.join("|"),
				url.indexOf("blob:mob3/") === 0, fr.result,
				blob.slice(0, 1).size
			].join(",");
		};
		fr.readAsText(blob);
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	b.js.tick(time.Now().Add(time.Second))
	if page.Title != "2,text/plain,3,1,true,a=3|b=2,true,hi,1" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSFocusAndMicrotask(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		var input = document.createElement("input");
		document.body.appendChild(input);
		input.focus();
		var focused = document.activeElement === input;
		input.blur();
		var blurred = document.activeElement !== input;
		queueMicrotask(function () { document.title = [focused, blurred].join(","); });
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	b.js.tick(time.Now().Add(time.Second))
	if page.Title != "true,true" {
		t.Fatalf("title = %q", page.Title)
	}
}

func TestJSRequestLog(t *testing.T) {
	loader := NewLoader()
	loader.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("Content-Type", "text/plain")
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(strings.NewReader("ok")), Request: req,
		}, nil
	})}
	page := newJSPage(`<html><body></body></html>`, `
		fetch("/f");
		var x = new XMLHttpRequest();
		x.open("GET", "/x");
		x.send();
	`)
	b := NewBrowser(40, 10)
	b.setLoader(loader)
	b.startJS(page)
	records := loader.requestSnapshot()
	kinds := map[string]bool{}
	for _, r := range records {
		if r.Done && r.Status == 200 {
			kinds[r.Kind] = true
		}
	}
	if !kinds["fetch"] || !kinds["xhr"] {
		t.Fatalf("request log = %+v", records)
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
