package browser

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"
)

const (
	jsTimeout      = 2 * time.Second
	jsMaxTimers    = 512
	jsMaxCallbacks = 200
)

// jsRuntime hosts one page's JavaScript world. Everything runs on the main
// browser goroutine; timers are pumped from the interactive loop.
type jsRuntime struct {
	b    *Browser
	vm   *goja.Runtime
	page *Page

	nodes map[*Node]*goja.Object
	rev   map[*goja.Object]*Node

	docObj        *goja.Object
	winObj        *goja.Object
	navObj        *goja.Object
	perfObj       *goja.Object
	locObj        *goja.Object
	histObj       *goja.Object
	elementProtos map[string]*goja.Object
	elementCtors  []string
	elementProto  *goja.Object
	textProto     *goja.Object
	docProto      *goja.Object
	headerObjs    map[*goja.Object]map[string][]string
	start         time.Time
	pendingNav    string
	dialogActive  atomic.Bool

	storageLocal   *goja.Object
	storageSession *goja.Object
	currentScript  *goja.Object

	nodeListeners map[*Node]map[string][]jsListener
	propListeners map[*Node]map[string]jsListener
	docListeners  map[string][]jsListener
	winListeners  map[string][]jsListener
	winOnLoad     goja.Value

	selectors map[string][]*Selector

	eventCtors     map[string]*goja.Object
	canvasContexts map[*Node]goja.Value

	timers   map[int]*jsTimer
	nextID   int
	dirty    bool
	stopped  bool
	titleSet func(string)
}

type jsListener struct {
	value goja.Value
	call  goja.Callable
	once  bool
}

type jsTimer struct {
	id     int
	due    time.Time
	every  time.Duration
	repeat bool
	fn     goja.Callable
	args   []goja.Value
}

func newJSRuntime(b *Browser, page *Page) *jsRuntime {
	j := &jsRuntime{
		b:              b,
		vm:             goja.New(),
		page:           page,
		nodes:          make(map[*Node]*goja.Object),
		rev:            make(map[*goja.Object]*Node),
		nodeListeners:  make(map[*Node]map[string][]jsListener),
		propListeners:  make(map[*Node]map[string]jsListener),
		docListeners:   make(map[string][]jsListener),
		winListeners:   make(map[string][]jsListener),
		selectors:      make(map[string][]*Selector),
		timers:         make(map[int]*jsTimer),
		nextID:         1,
		headerObjs:     make(map[*goja.Object]map[string][]string),
		canvasContexts: make(map[*Node]goja.Value),
	}
	j.start = time.Now()
	j.installElementConstructors()
	j.installEventConstructors()
	j.installIntl()
	j.installFetch()
	j.storageLocal = j.storageObject(make(map[string]string))
	j.storageSession = j.storageObject(make(map[string]string))
	j.navObj = j.navigatorObject()
	j.perfObj = j.performanceObject()
	j.locObj = j.locationObject()
	j.histObj = j.historyObject()
	j.docObj = j.documentObject()
	if j.docProto != nil {
		_ = j.docObj.SetPrototype(j.docProto)
	}
	j.winObj = j.windowObject()
	_ = j.vm.Set("document", j.docObj)
	_ = j.vm.Set("navigator", j.navObj)
	_ = j.vm.Set("performance", j.perfObj)
	_ = j.vm.Set("location", j.locObj)
	_ = j.vm.Set("history", j.histObj)
	_ = j.vm.Set("window", j.winObj)
	_ = j.vm.Set("console", j.consoleObject())
	_ = j.vm.Set("requestAnimationFrame", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return j.setTimer(goja.FunctionCall{Arguments: []goja.Value{call.Argument(0), j.vm.ToValue(16)}}, false)
	}))
	_ = j.vm.Set("cancelAnimationFrame", j.vm.ToValue(j.clearTimer))
	_ = j.vm.Set("matchMedia", j.vm.ToValue(j.matchMediaFunc()))
	_ = j.vm.Set("requestIdleCallback", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return j.setTimer(goja.FunctionCall{Arguments: []goja.Value{call.Argument(0), j.vm.ToValue(1)}}, false)
	}))
	_ = j.vm.Set("cancelIdleCallback", j.vm.ToValue(j.clearTimer))
	_ = j.vm.Set("__mob3NewURL", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return j.newURL(call.Argument(0).String(), call.Argument(1))
	}))
	_ = j.vm.Set("__mob3NewUSP", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return j.newURLSearchParams(call.Argument(0))
	}))
	_ = j.vm.Set("__mob3NewXHR", j.vm.ToValue(func(goja.FunctionCall) goja.Value {
		return j.xmlHTTPRequestObject()
	}))
	if _, err := j.vm.RunScript("bootstrap", `
		function Image() { return document.createElement("img"); }
		function Audio() { return document.createElement("audio"); }
		const __mob3URLImpl = globalThis.__mob3NewURL;
		const __mob3USPImpl = globalThis.__mob3NewUSP;
		const __mob3XHRImpl = globalThis.__mob3NewXHR;
		delete globalThis.__mob3NewURL;
		delete globalThis.__mob3NewUSP;
		delete globalThis.__mob3NewXHR;
		function URL(input, base) {
			return __mob3URLImpl(String(input), base === undefined ? null : String(base));
		}
		function URLSearchParams(init) {
			return __mob3USPImpl(init === undefined ? null : init);
		}
		function XMLHttpRequest() {
			return __mob3XHRImpl();
		}
	`); err != nil {
		j.reportError(err)
	}
	_ = j.vm.Set("setTimeout", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return j.setTimer(call, false)
	}))
	_ = j.vm.Set("setInterval", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return j.setTimer(call, true)
	}))
	_ = j.vm.Set("clearTimeout", j.vm.ToValue(j.clearTimer))
	_ = j.vm.Set("clearInterval", j.vm.ToValue(j.clearTimer))
	_ = j.vm.Set("alert", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		msg := call.Argument(0).String()
		j.b.logJS("alert", msg)
		j.b.status = "JS alert: " + truncateWidth(msg, 120) + "  (! console)"
		j.dialogActive.Store(true)
		j.b.showAlert(msg)
		j.dialogActive.Store(false)
		j.vm.ClearInterrupt()
		return goja.Undefined()
	}))
	_ = j.vm.Set("confirm", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		msg := call.Argument(0).String()
		j.b.logJS("confirm", msg)
		j.dialogActive.Store(true)
		ok := j.b.showConfirm(msg)
		j.dialogActive.Store(false)
		j.vm.ClearInterrupt()
		return j.vm.ToValue(ok)
	}))
	_ = j.vm.Set("prompt", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		msg := call.Argument(0).String()
		def := ""
		if len(call.Arguments) > 1 {
			def = call.Argument(1).String()
		}
		j.b.logJS("prompt", msg)
		j.dialogActive.Store(true)
		text, ok := j.b.showPrompt(msg, def)
		j.dialogActive.Store(false)
		j.vm.ClearInterrupt()
		if !ok {
			return goja.Null()
		}
		return j.vm.ToValue(text)
	}))
	for _, name := range j.elementCtors {
		if v := j.vm.Get(name); v != nil {
			_ = j.winObj.Set(name, v)
		}
	}
	for _, name := range []string{"Intl", "fetch", "Headers", "Request", "Response"} {
		if v := j.vm.Get(name); v != nil {
			_ = j.winObj.Set(name, v)
		}
	}
	return j
}

// startJS creates a fresh runtime and runs the page's scripts in order.
func (b *Browser) startJS(page *Page) {
	b.stopJS()
	if b.noJS || page == nil {
		return
	}
	// With scripting enabled, <noscript> content is not displayed.
	removeNoscript(page.Root)
	j := newJSRuntime(b, page)
	j.titleSet = func(s string) {
		page.Title = s
		b.status = ""
	}
	b.js = j
	for _, sc := range page.Scripts {
		name := sc.Src
		if name == "" {
			name = "script"
		}
		j.currentScript = nil
		if sc.Node != nil {
			if v, ok := j.wrap(sc.Node).(*goja.Object); ok {
				j.currentScript = v
			}
		}
		if _, err := j.runCode(name, sc.Code); err != nil {
			j.reportError(err)
		}
	}
	j.currentScript = nil
	j.fireDocumentEvent("DOMContentLoaded")
	j.fireWindowEvent("load")
}

func removeNoscript(root *Node) {
	if root == nil {
		return
	}
	for _, n := range root.FindAll(func(n *Node) bool { return n.Tag == "noscript" }) {
		detach(n)
	}
}

func (b *Browser) stopJS() {
	if b.js != nil {
		b.js.stop()
		b.js = nil
	}
}

func (j *jsRuntime) stop() {
	j.stopped = true
	j.timers = nil
}

// runJSTimers executes due timers and reports whether the UI should redraw.
func (b *Browser) runJSTimers() bool {
	if b.js == nil || b.consoleOpen {
		// Keep the console view stable while the user selects/copies output;
		// timer-driven redraws would clear the terminal selection.
		return false
	}
	ran := b.js.tick(time.Now())
	if !ran {
		return false
	}
	if b.js.consumeDirty() {
		b.rebuild()
	}
	return !b.consumePendingJS()
}

func (j *jsRuntime) markDirty() {
	if j != nil {
		j.dirty = true
	}
}

func (j *jsRuntime) consumeDirty() bool {
	d := j.dirty
	j.dirty = false
	return d
}

func (j *jsRuntime) reportError(err error) {
	if err == nil {
		return
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		msg = "JavaScript error"
	}
	j.b.logJS("error", msg)
	j.b.status = "JS: " + truncateWidth(msg, 120) + "  (! console)"
}

func (j *jsRuntime) matchMediaFunc() func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		q := call.Argument(0).String()
		o := j.vm.NewObject()
		_ = o.Set("media", q)
		_ = o.Set("matches", mediaMatches(q, j.b.width*cellW))
		_ = o.Set("onchange", goja.Null())
		noop := j.vm.ToValue(func(goja.FunctionCall) goja.Value { return goja.Undefined() })
		_ = o.Set("addListener", noop)
		_ = o.Set("removeListener", noop)
		_ = o.Set("addEventListener", noop)
		_ = o.Set("removeEventListener", noop)
		return o
	}
}

func (j *jsRuntime) performanceObject() *goja.Object {
	o := j.vm.NewObject()
	nowFn := func(goja.FunctionCall) goja.Value {
		return j.vm.ToValue(float64(time.Since(j.start).Microseconds()) / 1000)
	}
	_ = o.Set("now", j.vm.ToValue(nowFn))
	_ = o.Set("timeOrigin", float64(j.start.UnixMilli()))
	_ = o.Set("mark", j.vm.ToValue(func(goja.FunctionCall) goja.Value { return goja.Undefined() }))
	_ = o.Set("measure", j.vm.ToValue(func(goja.FunctionCall) goja.Value { return goja.Undefined() }))
	empty := func(goja.FunctionCall) goja.Value { return j.vm.NewArray() }
	_ = o.Set("getEntries", j.vm.ToValue(empty))
	_ = o.Set("getEntriesByType", j.vm.ToValue(empty))
	_ = o.Set("getEntriesByName", j.vm.ToValue(empty))
	start := float64(j.start.UnixMilli())
	timing := j.vm.NewObject()
	for _, name := range []string{
		"navigationStart", "fetchStart", "domainLookupStart", "domainLookupEnd",
		"connectStart", "connectEnd", "requestStart", "responseStart",
		"responseEnd", "domLoading", "domInteractive", "domContentLoadedEventStart",
		"domContentLoadedEventEnd", "domComplete", "loadEventStart", "loadEventEnd",
	} {
		_ = timing.Set(name, start)
	}
	_ = o.Set("timing", timing)
	navigation := j.vm.NewObject()
	_ = navigation.Set("type", 0)
	_ = navigation.Set("redirectCount", 0)
	_ = o.Set("navigation", navigation)
	return o
}

func (j *jsRuntime) locationObject() *goja.Object {
	o := j.vm.NewObject()
	href := ""
	if j.page != nil && j.page.URL != nil {
		u := j.page.URL
		href = u.String()
		_ = o.Set("protocol", u.Scheme+":")
		_ = o.Set("host", u.Host)
		_ = o.Set("hostname", u.Hostname())
		_ = o.Set("port", u.Port())
		_ = o.Set("pathname", u.Path)
		_ = o.Set("search", prefixQuery(u.RawQuery))
		_ = o.Set("hash", prefixHash(u.Fragment))
		origin := u.Scheme + "://" + u.Host
		if u.Scheme == "about" || u.Scheme == "data" || u.Scheme == "file" {
			origin = "null"
		}
		_ = o.Set("origin", origin)
	}
	j.accessor(o, "href", func() goja.Value {
		return j.vm.ToValue(href)
	}, func(v goja.Value) {
		j.requestNavigation(v.String())
	})
	assign := func(call goja.FunctionCall) goja.Value {
		j.requestNavigation(call.Argument(0).String())
		return goja.Undefined()
	}
	_ = o.Set("assign", j.vm.ToValue(assign))
	_ = o.Set("replace", j.vm.ToValue(assign))
	_ = o.Set("reload", j.vm.ToValue(func(goja.FunctionCall) goja.Value {
		j.requestNavigation(href)
		return goja.Undefined()
	}))
	return o
}

// requestNavigation queues a JS-initiated navigation until the current script
// or callback has finished; navigating reentrantly would stop the runtime
// mid-execution.
func (j *jsRuntime) requestNavigation(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || j.page == nil {
		return
	}
	base := j.page.Base
	if base == nil {
		base = j.page.URL
	}
	if base == nil {
		return
	}
	u, err := base.Parse(raw)
	if err != nil {
		j.b.status = "Invalid JavaScript URL"
		return
	}
	j.pendingNav = u.String()
}

func (j *jsRuntime) historyObject() *goja.Object {
	o := j.vm.NewObject()
	_ = o.Set("length", 0)
	_ = o.Set("state", goja.Null())
	noop := j.vm.ToValue(func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	_ = o.Set("pushState", noop)
	_ = o.Set("replaceState", noop)
	stub := j.vm.ToValue(func(goja.FunctionCall) goja.Value {
		j.b.status = "JavaScript history navigation is not supported"
		return goja.Undefined()
	})
	_ = o.Set("back", stub)
	_ = o.Set("forward", stub)
	_ = o.Set("go", stub)
	return o
}

func prefixQuery(q string) string {
	if q == "" {
		return ""
	}
	return "?" + q
}

func prefixHash(h string) string {
	if h == "" {
		return ""
	}
	return "#" + h
}

func (j *jsRuntime) navigatorObject() *goja.Object {
	o := j.vm.NewObject()
	ua := "MOB3/1.0 (+terminal; +https://mob3.net)"
	if j.b.loader != nil && j.b.loader.userAgent != "" {
		ua = j.b.loader.userAgent
	}
	_ = o.Set("userAgent", ua)
	_ = o.Set("appName", "MOB3")
	_ = o.Set("appVersion", "1.0")
	_ = o.Set("platform", runtime.GOOS)
	_ = o.Set("language", "en-US")
	_ = o.Set("languages", j.vm.NewArray("en-US", "en"))
	_ = o.Set("cookieEnabled", true)
	_ = o.Set("onLine", true)
	return o
}

// defaultJSMemoryLimit is the default heap-delta budget for one script or
// callback. The -js-memory flag overrides it; zero disables the check.
const defaultJSMemoryLimit = 256 << 20

func (j *jsRuntime) guard(fn func() error) (err error) {
	if j == nil || j.stopped {
		return nil
	}
	done := make(chan struct{})
	timer := time.AfterFunc(jsTimeout, func() {
		select {
		case <-done:
		default:
			if !j.dialogActive.Load() {
				j.vm.Interrupt("JavaScript timed out")
			}
		}
	})
	var base runtime.MemStats
	var memDone chan struct{}
	if limit := j.b.jsMemLimit; limit > 0 {
		runtime.ReadMemStats(&base)
		memDone = make(chan struct{})
		go func() {
			t := time.NewTicker(25 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-memDone:
					return
				case <-t.C:
					if j.dialogActive.Load() {
						continue
					}
					var ms runtime.MemStats
					runtime.ReadMemStats(&ms)
					if ms.HeapAlloc > base.HeapAlloc+uint64(limit) {
						j.vm.Interrupt("JavaScript memory limit exceeded")
						return
					}
				}
			}
		}()
	}
	defer func() {
		close(done)
		if memDone != nil {
			close(memDone)
		}
		timer.Stop()
		j.vm.ClearInterrupt()
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	return fn()
}

func (j *jsRuntime) runCode(name, src string) (v goja.Value, err error) {
	err = j.guard(func() error {
		var e error
		v, e = j.vm.RunScript(name, src)
		return e
	})
	return v, err
}

func (j *jsRuntime) call(fn goja.Callable, this goja.Value, args ...goja.Value) (v goja.Value, ok bool) {
	if fn == nil || j.stopped {
		return nil, false
	}
	var callErr error
	err := j.guard(func() error {
		v, callErr = fn(this, args...)
		return callErr
	})
	if err != nil {
		j.reportError(err)
		return nil, false
	}
	return v, true
}

func (j *jsRuntime) consoleObject() *goja.Object {
	o := j.vm.NewObject()
	logf := func(level string) func(goja.FunctionCall) goja.Value {
		return func(call goja.FunctionCall) goja.Value {
			parts := make([]string, 0, len(call.Arguments))
			for _, a := range call.Arguments {
				parts = append(parts, a.String())
			}
			msg := strings.Join(parts, " ")
			j.b.logJS(level, msg)
			j.b.status = "JS " + level + ": " + truncateWidth(msg, 120) + "  (! console)"
			return goja.Undefined()
		}
	}
	_ = o.Set("log", j.vm.ToValue(logf("log")))
	_ = o.Set("info", j.vm.ToValue(logf("info")))
	_ = o.Set("warn", j.vm.ToValue(logf("warn")))
	_ = o.Set("error", j.vm.ToValue(logf("error")))
	return o
}

func (j *jsRuntime) windowObject() *goja.Object {
	// window is the global object: `window.foo = 1` must create a global
	// `foo`, and global `var` declarations must appear on window.
	o := j.vm.GlobalObject()
	_ = o.Set("document", j.docObj)
	_ = o.Set("navigator", j.navObj)
	_ = o.Set("performance", j.perfObj)
	_ = o.Set("location", j.locObj)
	_ = o.Set("history", j.histObj)
	_ = o.Set("localStorage", j.storageLocal)
	_ = o.Set("sessionStorage", j.storageSession)
	_ = o.Set("innerWidth", j.b.width*8)
	_ = o.Set("innerHeight", j.b.height*16)
	_ = o.Set("devicePixelRatio", 1)
	_ = o.Set("getComputedStyle", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return j.computedStyleObject(j.unwrap(call.Argument(0)))
	}))
	screen := j.vm.NewObject()
	_ = screen.Set("width", j.b.width*8)
	_ = screen.Set("height", j.b.height*16)
	_ = screen.Set("availWidth", j.b.width*8)
	_ = screen.Set("availHeight", j.b.height*16)
	_ = screen.Set("colorDepth", 24)
	_ = screen.Set("pixelDepth", 24)
	_ = o.Set("screen", screen)
	vv := j.vm.NewObject()
	_ = vv.Set("width", j.b.width*8)
	_ = vv.Set("height", j.b.height*16)
	_ = vv.Set("scale", 1)
	_ = vv.Set("offsetLeft", 0)
	_ = vv.Set("offsetTop", 0)
	_ = vv.Set("pageLeft", 0)
	_ = vv.Set("pageTop", 0)
	vvNoop := j.vm.ToValue(func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	_ = vv.Set("addEventListener", vvNoop)
	_ = vv.Set("removeEventListener", vvNoop)
	_ = o.Set("visualViewport", vv)
	_ = o.Set("console", j.consoleObject())
	_ = o.Set("setTimeout", j.vm.ToValue(func(call goja.FunctionCall) goja.Value { return j.setTimer(call, false) }))
	_ = o.Set("setInterval", j.vm.ToValue(func(call goja.FunctionCall) goja.Value { return j.setTimer(call, true) }))
	_ = o.Set("clearTimeout", j.vm.ToValue(j.clearTimer))
	_ = o.Set("clearInterval", j.vm.ToValue(j.clearTimer))
	_ = o.Set("requestAnimationFrame", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return j.setTimer(goja.FunctionCall{Arguments: []goja.Value{call.Argument(0), j.vm.ToValue(16)}}, false)
	}))
	_ = o.Set("cancelAnimationFrame", j.vm.ToValue(j.clearTimer))
	_ = o.Set("matchMedia", j.vm.ToValue(j.matchMediaFunc()))
	_ = o.Set("requestIdleCallback", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return j.setTimer(goja.FunctionCall{Arguments: []goja.Value{call.Argument(0), j.vm.ToValue(1)}}, false)
	}))
	_ = o.Set("cancelIdleCallback", j.vm.ToValue(j.clearTimer))
	_ = o.Set("addEventListener", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		j.addListener(&j.winListeners, call.Argument(0).String(), call.Argument(1), call.Argument(2))
		return goja.Undefined()
	}))
	_ = o.Set("removeEventListener", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		j.removeListener(j.winListeners, call.Argument(0).String(), call.Argument(1))
		return goja.Undefined()
	}))
	_ = o.Set("dispatchEvent", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return j.vm.ToValue(j.dispatchWindowEvent(call.Argument(0)))
	}))
	_ = o.DefineAccessorProperty("onload", j.vm.ToValue(func(goja.FunctionCall) goja.Value {
		if j.winOnLoad == nil {
			return goja.Null()
		}
		return j.winOnLoad
	}), j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		j.winOnLoad = call.Argument(0)
		return goja.Undefined()
	}), goja.FLAG_TRUE, goja.FLAG_FALSE)
	_ = o.Set("window", o)
	_ = o.Set("self", o)
	_ = o.Set("top", o)
	_ = o.Set("parent", o)
	return o
}

func (j *jsRuntime) setTimer(call goja.FunctionCall, repeat bool) goja.Value {
	if len(j.timers) >= jsMaxTimers {
		return j.vm.ToValue(-1)
	}
	fn, ok := j.timerFunc(call.Argument(0))
	if !ok {
		return j.vm.ToValue(-1)
	}
	ms := call.Argument(1).ToInteger()
	if ms < 0 {
		ms = 0
	}
	d := time.Duration(ms) * time.Millisecond
	if repeat && d < 10*time.Millisecond {
		d = 10 * time.Millisecond
	}
	id := j.nextID
	j.nextID++
	t := &jsTimer{id: id, due: time.Now().Add(d), every: d, repeat: repeat, fn: fn}
	if len(call.Arguments) > 2 {
		t.args = append(t.args, call.Arguments[2:]...)
	}
	j.timers[id] = t
	return j.vm.ToValue(id)
}

func (j *jsRuntime) timerFunc(v goja.Value) (goja.Callable, bool) {
	if fn, ok := goja.AssertFunction(v); ok {
		return fn, true
	}
	if s, ok := v.Export().(string); ok && strings.TrimSpace(s) != "" {
		fv, err := j.runCode("timer", "(function(event){"+s+"\n})")
		if err != nil {
			j.reportError(err)
			return nil, false
		}
		return goja.AssertFunction(fv)
	}
	return nil, false
}

func (j *jsRuntime) clearTimer(call goja.FunctionCall) goja.Value {
	id := int(call.Argument(0).ToInteger())
	delete(j.timers, id)
	return goja.Undefined()
}

// tick runs due timers and returns whether any callback ran.
func (j *jsRuntime) tick(now time.Time) bool {
	if j == nil || j.stopped || len(j.timers) == 0 {
		return false
	}
	due := make([]*jsTimer, 0, len(j.timers))
	for _, t := range j.timers {
		if !t.due.After(now) {
			due = append(due, t)
		}
	}
	if len(due) == 0 {
		return false
	}
	sort.Slice(due, func(a, b int) bool { return due[a].due.Before(due[b].due) })
	if len(due) > jsMaxCallbacks {
		due = due[:jsMaxCallbacks]
	}
	ran := false
	for _, t := range due {
		if j.stopped {
			break
		}
		if t.repeat {
			t.due = now.Add(t.every)
		} else {
			delete(j.timers, t.id)
		}
		j.call(t.fn, goja.Undefined(), t.args...)
		ran = true
	}
	return ran
}

func (j *jsRuntime) addListener(m *map[string][]jsListener, typ string, fnVal, opts goja.Value) {
	fn, ok := goja.AssertFunction(fnVal)
	if !ok {
		return
	}
	once := false
	if opts != nil && !goja.IsUndefined(opts) && !goja.IsNull(opts) {
		if o, ok := opts.(*goja.Object); ok {
			if v := o.Get("once"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
				once = v.ToBoolean()
			}
		}
	}
	(*m)[typ] = append((*m)[typ], jsListener{value: fnVal, call: fn, once: once})
}

func (j *jsRuntime) addNodeListener(n *Node, typ string, fnVal, opts goja.Value) {
	if n == nil {
		return
	}
	m := j.nodeListeners[n]
	if m == nil {
		m = make(map[string][]jsListener)
		j.nodeListeners[n] = m
	}
	j.addListener(&m, typ, fnVal, opts)
}

func (j *jsRuntime) removeListener(m map[string][]jsListener, typ string, fnVal goja.Value) {
	list := m[typ]
	out := list[:0]
	for _, l := range list {
		if !l.value.SameAs(fnVal) {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		delete(m, typ)
	} else {
		m[typ] = out
	}
}

func (j *jsRuntime) removeNodeListener(n *Node, typ string, fnVal goja.Value) {
	m := j.nodeListeners[n]
	if m == nil {
		return
	}
	j.removeListener(m, typ, fnVal)
	if len(m) == 0 {
		delete(j.nodeListeners, n)
	}
}

type jsEvent struct {
	typ       string
	target    goja.Value
	prevented bool
	stopped   bool
}

func (j *jsRuntime) eventObject(ev *jsEvent) *goja.Object {
	o := j.vm.NewObject()
	_ = o.Set("type", ev.typ)
	_ = o.Set("target", ev.target)
	_ = o.Set("bubbles", ev.typ == "click")
	_ = o.Set("cancelable", true)
	_ = o.DefineAccessorProperty("defaultPrevented", j.vm.ToValue(func(goja.FunctionCall) goja.Value {
		return j.vm.ToValue(ev.prevented)
	}), nil, goja.FLAG_TRUE, goja.FLAG_FALSE)
	_ = o.Set("preventDefault", j.vm.ToValue(func(goja.FunctionCall) goja.Value {
		ev.prevented = true
		return goja.Undefined()
	}))
	_ = o.Set("stopPropagation", j.vm.ToValue(func(goja.FunctionCall) goja.Value {
		ev.stopped = true
		return goja.Undefined()
	}))
	_ = o.Set("stopImmediatePropagation", j.vm.ToValue(func(goja.FunctionCall) goja.Value {
		ev.stopped = true
		return goja.Undefined()
	}))
	return o
}

func (j *jsRuntime) callList(m map[string][]jsListener, typ string, this, evObj goja.Value, ev *jsEvent) {
	list := m[typ]
	for i := 0; i < len(list); i++ {
		l := list[i]
		j.call(l.call, this, evObj)
		if l.once {
			list = append(list[:i], list[i+1:]...)
			m[typ] = list
			i--
		}
		if ev.stopped {
			return
		}
	}
}

// fireNodeEvent dispatches an event on an element and bubbles to document and
// window. It returns whether default behavior was prevented.
func (j *jsRuntime) fireNodeEvent(n *Node, typ string) bool {
	if j == nil || j.stopped || n == nil {
		return false
	}
	ev := &jsEvent{typ: typ, target: j.wrap(n)}
	evObj := j.eventObject(ev)
	for p := n; p != nil && !ev.stopped; p = p.Parent {
		if p.Type != ElementNode {
			continue
		}
		if m := j.nodeListeners[p]; m != nil {
			j.callList(m, typ, j.wrap(p), evObj, ev)
		}
		if m := j.propListeners[p]; m != nil {
			if l, ok := m[typ]; ok {
				j.call(l.call, j.wrap(p), evObj)
			}
		}
		if typ == "click" {
			j.inlineHandler(p, "onclick", evObj, ev)
		}
	}
	if !ev.stopped {
		j.callList(j.docListeners, typ, j.docObj, evObj, ev)
	}
	if !ev.stopped {
		j.callList(j.winListeners, typ, j.winObj, evObj, ev)
	}
	return ev.prevented
}

func (j *jsRuntime) fireDocumentEvent(typ string) {
	if j == nil || j.stopped {
		return
	}
	ev := &jsEvent{typ: typ, target: j.docObj}
	evObj := j.eventObject(ev)
	j.callList(j.docListeners, typ, j.docObj, evObj, ev)
	if !ev.stopped {
		j.callList(j.winListeners, typ, j.winObj, evObj, ev)
	}
}

func (j *jsRuntime) fireWindowEvent(typ string) {
	if j == nil || j.stopped {
		return
	}
	ev := &jsEvent{typ: typ, target: j.winObj}
	evObj := j.eventObject(ev)
	j.callList(j.winListeners, typ, j.winObj, evObj, ev)
	if typ == "load" && j.winOnLoad != nil {
		if fn, ok := goja.AssertFunction(j.winOnLoad); ok {
			j.call(fn, j.winObj, evObj)
		}
	}
}

func (j *jsRuntime) inlineHandler(n *Node, attr string, evObj goja.Value, ev *jsEvent) {
	code := strings.TrimSpace(n.Attr(attr))
	if code == "" {
		return
	}
	fv, err := j.runCode("inline "+attr, "(function(event){"+code+"\n})")
	if err != nil {
		j.reportError(err)
		return
	}
	fn, ok := goja.AssertFunction(fv)
	if !ok {
		return
	}
	res, ok := j.call(fn, j.wrap(n), evObj)
	if ok && res != nil && !goja.IsUndefined(res) && !res.ToBoolean() {
		if ev != nil {
			ev.prevented = true
		} else if o, ok := evObj.(*goja.Object); ok {
			_ = o.Set("defaultPrevented", true)
		}
	}
}

// dispatchClick runs click handlers for n. It returns true when default
// navigation should be suppressed.
func (j *jsRuntime) dispatchClick(n *Node) bool {
	return j.fireNodeEvent(n, "click")
}
