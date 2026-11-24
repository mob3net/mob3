package engine

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/grafana/sobek"

	"browser/internal/protocol"
)

const (
	jsTimeout      = 2 * time.Second
	jsMaxTimers    = 512
	jsMaxCallbacks = 200
	// jsPageBudget caps the wall time spent running a page's load-time scripts,
	// so many short scripts cannot add up to an indefinite freeze.
	jsPageBudget = 8 * time.Second
	// jsMaxScripts caps how many of a page's scripts execute. A 12 MB document
	// can hold hundreds of thousands of tiny scripts, each with its own budget.
	jsMaxScripts = 2000
)

// DefaultUserAgent is used when no host UA is configured.
const DefaultUserAgent = "MOB3/1.0 (+terminal; +https://mob3.net)"

// Stop reasons for a page's load-time script phase.
const (
	jsStopNone = iota
	jsStopUser
	jsStopBudget
)

// jsRuntime hosts one page's JavaScript world. Everything runs on the main
// browser goroutine; timers are pumped from the interactive loop.
type jsRuntime struct {
	host JSHost
	vm   *sobek.Runtime
	page *Page

	nodes map[*Node]*sobek.Object
	rev   map[*sobek.Object]*Node
	// templates maps a <template> element to the inert fragment that holds its
	// content, created on first access to template.content.
	templates map[*Node]*Node
	// customCtors holds the registered custom element classes, and building is
	// the stack of nodes whose constructor is currently running so DOM calls
	// made during construction resolve to them.
	customCtors map[string]*sobek.Object
	building    []*Node

	docObj            *sobek.Object
	winObj            *sobek.Object
	navObj            *sobek.Object
	perfObj           *sobek.Object
	locObj            *sobek.Object
	histObj           *sobek.Object
	elementProtos     map[string]*sobek.Object
	elementCtors      []string
	elementProto      *sobek.Object
	textProto         *sobek.Object
	docProto          *sobek.Object
	domStringMapProto *sobek.Object
	domTokenListProto *sobek.Object
	cssStyleProto     *sobek.Object
	mediaQueryProto   *sobek.Object
	treeWalkerProto   *sobek.Object
	nodeIteratorProto *sobek.Object
	headerObjs        map[*sobek.Object]map[string][]string
	datasetObjs       map[*Node]sobek.Value
	start             time.Time
	pendingNav        string
	pendingReplace    bool
	pendingForm       *jsFormSubmission
	dialogActive      atomic.Bool
	moduleErr         bool
	// inFlight counts guarded calls. The memory sampler only interrupts while
	// one is running, so an idle tick cannot poison the next invocation.
	inFlight atomic.Int32
	// memBase is the heap size when the runtime was created, and memLimit the
	// absolute ceiling above it. memMu guards the sampler lifecycle; one
	// sampler serves the whole runtime instead of one goroutine per call.
	memBase  uint64
	memLimit atomic.Int64
	memMu    sync.Mutex
	memStop  chan struct{}
	memWake  chan struct{}
	// timeout is the per-call CPU watchdog. It is jsTimeout except in tests
	// that need a shorter check.
	timeout time.Duration
	// pageBudget and loadDeadline enforce the page-wide load-time script
	// budget. loadDeadline is a Unix nanosecond timestamp, zero outside that
	// phase.
	pageBudget   time.Duration
	loadDeadline atomic.Int64
	// stopScripts is set when the page budget expires or the user aborts, so
	// startJS skips the remaining scripts instead of running each one until its
	// own watchdog fires. stopReason records why for the console notice.
	stopScripts atomic.Bool
	stopReason  atomic.Int32
	// ioActive marks a host-side fetch (a dynamic module import) running inside
	// a script. The 2-second watchdog must not count network time against the
	// script, the same way it ignores time spent in a modal dialog.
	ioActive atomic.Bool
	// modules loads ES modules for this page. It is nil until the first module
	// script is evaluated.
	modules *jsModuleLoader

	storageLocal       *sobek.Object
	storageSession     *sobek.Object
	storageLocalView   *StorageView
	storageSessionView *StorageView
	currentScript      *sobek.Object

	nodeListeners map[*Node]map[string][]jsListener
	propListeners map[*Node]map[string]jsListener
	docListeners  map[string][]jsListener
	winListeners  map[string][]jsListener
	winOnLoad     sobek.Value

	selectors map[string][]*Selector

	eventCtors     map[string]*sobek.Object
	canvasContexts map[*Node]sobek.Value

	selection *sobek.Object
	ranges    map[*sobek.Object]*jsRange
	blobs     map[*sobek.Object]*blobData
	blobURLs  map[string]*sobek.Object
	blobSeq   int64

	// loadDone marks that the page's own scripts have finished, so a dynamic
	// import() may be loaded in the background instead of blocking the loop.
	loadDone bool
	timers   map[int]*jsTimer
	nextID   int
	dirty    bool
	stopped  bool
	titleSet func(string)
	// eventErr records that a handler threw during the current event dispatch.
	eventErr bool
	// frozenAt records when the runtime was detached from the browser for the
	// back/forward page cache, so timers can resume where they left off.
	frozenAt time.Time
}

type jsListener struct {
	value sobek.Value
	call  sobek.Callable
	once  bool
}

type jsTimer struct {
	id     int
	due    time.Time
	every  time.Duration
	repeat bool
	fn     sobek.Callable
	args   []sobek.Value
}

// jsFormSubmission is a programmatic form.submit() queued until the current
// script/callback returns.
type jsFormSubmission struct {
	Method      string
	Target      string
	Body        []byte
	ContentType string
}

// FormSubmission is the exported form submission record.
type FormSubmission = jsFormSubmission

func NewJSRuntime(host JSHost, page *Page) *jsRuntime {
	if page == nil {
		page = &Page{Root: &Node{Type: DocumentNode}}
	}
	j := &jsRuntime{
		host:           host,
		vm:             sobek.New(),
		page:           page,
		timeout:        jsTimeout,
		pageBudget:     jsPageBudget,
		nodes:          make(map[*Node]*sobek.Object),
		rev:            make(map[*sobek.Object]*Node),
		templates:      make(map[*Node]*Node),
		customCtors:    make(map[string]*sobek.Object),
		nodeListeners:  make(map[*Node]map[string][]jsListener),
		propListeners:  make(map[*Node]map[string]jsListener),
		docListeners:   make(map[string][]jsListener),
		winListeners:   make(map[string][]jsListener),
		selectors:      make(map[string][]*Selector),
		timers:         make(map[int]*jsTimer),
		nextID:         1,
		headerObjs:     make(map[*sobek.Object]map[string][]string),
		datasetObjs:    make(map[*Node]sobek.Value),
		canvasContexts: make(map[*Node]sobek.Value),
		ranges:         make(map[*sobek.Object]*jsRange),
		blobs:          make(map[*sobek.Object]*blobData),
		blobURLs:       make(map[string]*sobek.Object),
	}
	j.start = time.Now()
	j.installElementConstructors()
	j.initTextProto(j.textProto)
	j.initElementProto(j.elementProto)
	j.installElementCompatProto(j.elementProto)
	j.installEventConstructors()
	j.installIntl()
	j.installFetch()
	localStorage, sessionStorage := newPrivateStorageView(), newPrivateStorageView()
	if host != nil {
		if v := host.LocalStorage(); v != nil {
			localStorage = v
		}
		if v := host.SessionStorage(); v != nil {
			sessionStorage = v
		}
	}
	j.storageLocalView = localStorage
	j.storageSessionView = sessionStorage
	j.storageLocal = j.storageObject(localStorage)
	j.storageSession = j.storageObject(sessionStorage)
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
	_ = j.vm.Set("requestAnimationFrame", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		return j.setTimer(sobek.FunctionCall{Arguments: []sobek.Value{call.Argument(0), j.vm.ToValue(16)}}, false)
	}))
	_ = j.vm.Set("cancelAnimationFrame", j.vm.ToValue(j.clearTimer))
	_ = j.vm.Set("matchMedia", j.vm.ToValue(j.matchMediaFunc()))
	_ = j.vm.Set("requestIdleCallback", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		return j.setTimer(sobek.FunctionCall{Arguments: []sobek.Value{call.Argument(0), j.vm.ToValue(1)}}, false)
	}))
	_ = j.vm.Set("cancelIdleCallback", j.vm.ToValue(j.clearTimer))
	// reportError() reports an exception to the console without throwing, as
	// the HTML error-reporting API does.
	reportError := j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		v := call.Argument(0)
		if v == nil || sobek.IsUndefined(v) || sobek.IsNull(v) {
			return sobek.Undefined()
		}
		j.host.LogJS("error", j.ErrorText(v))
		return sobek.Undefined()
	})
	_ = j.vm.Set("reportError", reportError)
	_ = j.vm.Set("__mob3NewURL", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		return j.newURL(call.Argument(0).String(), call.Argument(1))
	}))
	_ = j.vm.Set("__mob3NewUSP", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		return j.newURLSearchParams(call.Argument(0))
	}))
	_ = j.vm.Set("__mob3NewXHR", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
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
		j.ReportError(err)
	}
	_ = j.vm.Set("setTimeout", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		return j.setTimer(call, false)
	}))
	_ = j.vm.Set("setInterval", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		return j.setTimer(call, true)
	}))
	_ = j.vm.Set("clearTimeout", j.vm.ToValue(j.clearTimer))
	_ = j.vm.Set("clearInterval", j.vm.ToValue(j.clearTimer))
	_ = j.vm.Set("alert", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		msg := call.Argument(0).String()
		j.host.LogJS("alert", msg)
		j.host.SetStatus("JS alert: " + TruncateWidth(msg, 120) + "  (! console)")
		j.dialogActive.Store(true)
		j.host.ShowAlert(msg)
		j.dialogActive.Store(false)
		j.vm.ClearInterrupt()
		return sobek.Undefined()
	}))
	_ = j.vm.Set("confirm", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		msg := call.Argument(0).String()
		j.host.LogJS("confirm", msg)
		j.dialogActive.Store(true)
		ok := j.host.ShowConfirm(msg)
		j.dialogActive.Store(false)
		j.vm.ClearInterrupt()
		return j.vm.ToValue(ok)
	}))
	_ = j.vm.Set("prompt", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		msg := call.Argument(0).String()
		def := ""
		if len(call.Arguments) > 1 {
			def = call.Argument(1).String()
		}
		j.host.LogJS("prompt", msg)
		j.dialogActive.Store(true)
		text, ok := j.host.ShowPrompt(msg, def)
		j.dialogActive.Store(false)
		j.vm.ClearInterrupt()
		if !ok {
			return sobek.Null()
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
	_ = j.vm.Set("__mob3DefineCustomElement", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		name := strings.ToLower(strings.TrimSpace(call.Argument(0).String()))
		ctor, ok := call.Argument(1).(*sobek.Object)
		if ok && name != "" {
			j.customCtors[name] = ctor
		}
		return sobek.Undefined()
	}))
	j.installCompat()
	// Every runtime gets the module hooks, so a classic script can use
	// dynamic import() even when the page has no module script of its own.
	j.modules = newJSModuleLoader(j)
	j.memBase = heapAlloc()
	return j
}

// heapAlloc returns the current live heap size. It is called once per runtime,
// not once per script or timer callback.
func heapAlloc() uint64 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

// startJS creates a fresh runtime and runs the page's scripts in order.
func (j *jsRuntime) StartPage(page *Page) {
	if page == nil {
		return
	}
	removeNoscript(page.Root)
	page.SeedParsedStyles()
	j.titleSet = func(s string) {
		page.Title = s
		j.host.SetStatus("")
	}
	j.loadDeadline.Store(time.Now().Add(j.pageBudget).UnixNano())
	phaseAbort := j.host.AbortGen()
	executed := 0
	for _, sc := range page.Scripts {
		if j.host.AbortGen() != phaseAbort {
			j.stopScripts.Store(true)
			j.stopReason.Store(jsStopUser)
		} else if j.deadlineExpired() {
			j.stopScripts.Store(true)
			j.stopReason.Store(jsStopBudget)
		}
		if j.stopScripts.Load() {
			break
		}
		if executed >= jsMaxScripts {
			j.host.LogJS("warn", fmt.Sprintf("script limit reached (%d); skipped remaining scripts", jsMaxScripts))
			break
		}
		executed++
		if sc.Module {
			j.currentScript = nil
			if err := j.runModule(sc); err != nil {
				j.ReportError(err)
				j.moduleErr = true
			}
			continue
		}
		name := sc.Src
		if name == "" {
			name = "script"
		}
		j.currentScript = nil
		if sc.Node != nil {
			if v, ok := j.wrap(sc.Node).(*sobek.Object); ok {
				j.currentScript = v
			}
		}
		if _, err := j.RunCode(name, sc.Code); err != nil {
			j.ReportError(err)
		}
	}
	j.currentScript = nil
	switch j.stopReason.Load() {
	case jsStopUser:
		j.host.LogJS("warn", "JavaScript stopped by user")
	case jsStopBudget:
		j.host.LogJS("warn", fmt.Sprintf("JavaScript time limit reached (%s)", j.pageBudget))
	}
	if !j.stopScripts.Load() {
		j.FireDocumentEvent("DOMContentLoaded")
		j.FireWindowEvent("load")
	}
	j.loadDeadline.Store(0)
	j.loadDone = true
}

func removeNoscript(root *Node) {
	if root == nil {
		return
	}
	for _, n := range root.FindAll(func(n *Node) bool { return n.Tag == "noscript" }) {
		detach(n)
	}
}

func (j *jsRuntime) Stop() {
	j.stopped = true
	j.timers = nil
	j.stopMemSampler()
}

// freeze detaches a cached runtime from the event loop; its timers do not run
// until the page is resumed.
func (j *jsRuntime) Freeze() {
	if j == nil || !j.frozenAt.IsZero() {
		return
	}
	j.frozenAt = time.Now()
	j.stopMemSampler()
}

// resume reattaches a frozen runtime and shifts pending timers by the time the
// page spent in the back/forward cache, so a paused interval does not fire a
// burst of catch-up callbacks.
func (j *jsRuntime) Resume() {
	if j == nil || j.frozenAt.IsZero() {
		return
	}
	paused := time.Since(j.frozenAt)
	j.frozenAt = time.Time{}
	if j.memLimit.Load() > 0 {
		j.startMemSampler()
	}
	if paused <= 0 {
		return
	}
	for _, t := range j.timers {
		t.due = t.due.Add(paused)
	}
}

// nextJSTimerDelay reports how long until the earliest scheduled JS timer is
// due. It reports false when no timers are pending, so the event loop can sleep
// instead of waking on a fixed tick.

// runJSTimers executes due timers and reports whether the UI should redraw.

func (j *jsRuntime) markDirty() {
	if j != nil {
		j.dirty = true
	}
}

func (j *jsRuntime) ConsumeDirty() bool {
	d := j.dirty
	j.dirty = false
	return d
}

// errorText renders a thrown value for the console, preferring the script
// stack an Error object carries so the failing line is visible.
func (j *jsRuntime) ErrorText(v sobek.Value) string {
	if v == nil || sobek.IsUndefined(v) || sobek.IsNull(v) {
		return "undefined"
	}
	if obj, ok := v.(*sobek.Object); ok {
		if st := obj.Get("stack"); st != nil && !sobek.IsUndefined(st) && !sobek.IsNull(st) {
			if s := strings.TrimSpace(st.String()); s != "" {
				return s
			}
		}
	}
	return jsValueString(v)
}

func (j *jsRuntime) ReportError(err error) {
	if err == nil {
		return
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		msg = "JavaScript error"
	}
	j.host.LogJS("error", msg)
}

func (j *jsRuntime) matchMediaFunc() func(sobek.FunctionCall) sobek.Value {
	return func(call sobek.FunctionCall) sobek.Value {
		q := call.Argument(0).String()
		o := j.vm.NewObject()
		if j.mediaQueryProto != nil {
			_ = o.SetPrototype(j.mediaQueryProto)
		}
		_ = o.Set("media", q)
		_ = o.Set("matches", MediaMatches(q, CSSWidthPx(j.host.Width()), j.host.Dark()))
		_ = o.Set("onchange", sobek.Null())
		noop := j.vm.ToValue(func(sobek.FunctionCall) sobek.Value { return sobek.Undefined() })
		_ = o.Set("addListener", noop)
		_ = o.Set("removeListener", noop)
		_ = o.Set("addEventListener", noop)
		_ = o.Set("removeEventListener", noop)
		return o
	}
}

func (j *jsRuntime) performanceObject() *sobek.Object {
	o := j.vm.NewObject()
	nowFn := func(sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue(float64(time.Since(j.start).Microseconds()) / 1000)
	}
	_ = o.Set("now", j.vm.ToValue(nowFn))
	_ = o.Set("timeOrigin", float64(j.start.UnixMilli()))
	_ = o.Set("mark", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value { return sobek.Undefined() }))
	_ = o.Set("measure", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value { return sobek.Undefined() }))
	empty := func(sobek.FunctionCall) sobek.Value { return j.vm.NewArray() }
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

func (j *jsRuntime) locationObject() *sobek.Object {
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
		_ = o.Set("origin", protocol.OriginOf(u).String())
	}
	j.accessor(o, "href", func() sobek.Value {
		return j.vm.ToValue(href)
	}, func(v sobek.Value) {
		j.requestNavigation(v.String())
	})
	assign := func(call sobek.FunctionCall) sobek.Value {
		j.requestNavigation(call.Argument(0).String())
		return sobek.Undefined()
	}
	_ = o.Set("assign", j.vm.ToValue(assign))
	_ = o.Set("replace", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		j.requestNavigationReplace(call.Argument(0).String())
		return sobek.Undefined()
	}))
	_ = o.Set("reload", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
		j.requestNavigationReplace(href)
		return sobek.Undefined()
	}))
	stringify := j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue(href)
	})
	_ = o.Set("toString", stringify)
	_ = o.Set("valueOf", stringify)
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
		j.host.SetStatus("Invalid JavaScript URL")
		return
	}
	j.pendingNav = u.String()
	j.pendingReplace = false
}

// requestNavigationReplace queues a navigation that replaces the current
// history entry, as location.replace() and location.reload() require.
func (j *jsRuntime) requestNavigationReplace(raw string) {
	j.requestNavigation(raw)
	if j.pendingNav != "" {
		j.pendingReplace = true
	}
}

func (j *jsRuntime) historyObject() *sobek.Object {
	o := j.vm.NewObject()
	_ = o.Set("length", 0)
	_ = o.Set("state", sobek.Null())
	noop := j.vm.ToValue(func(sobek.FunctionCall) sobek.Value { return sobek.Undefined() })
	_ = o.Set("pushState", noop)
	_ = o.Set("replaceState", noop)
	stub := j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
		j.host.SetStatus("JavaScript history navigation is not supported")
		return sobek.Undefined()
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

func (j *jsRuntime) navigatorObject() *sobek.Object {
	o := j.vm.NewObject()
	ua := DefaultUserAgent
	if j.host != nil && j.host.UserAgent() != "" {
		ua = j.host.UserAgent()
	}
	_ = o.Set("userAgent", ua)
	_ = o.Set("appName", "MOB3")
	_ = o.Set("appVersion", "1.0")
	_ = o.Set("platform", runtime.GOOS)
	_ = o.Set("language", "en-US")
	_ = o.Set("languages", j.vm.NewArray("en-US", "en"))
	_ = o.Set("cookieEnabled", true)
	_ = o.Set("onLine", true)
	_ = o.Set("sendBeacon", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
		// Beacon delivery is not implemented; accepting the call keeps
		// analytics and unload handlers from throwing.
		return j.vm.ToValue(true)
	}))
	return o
}

func (j *jsRuntime) guard(fn func() error) (err error) {
	if j == nil || j.stopped {
		return nil
	}
	// A sampler or a previous aborted call may have left an interrupt behind;
	// the next call must start clean.
	j.vm.ClearInterrupt()
	j.inFlight.Add(1)
	j.wakeMemSampler()
	timeout := j.timeout
	if timeout <= 0 {
		timeout = jsTimeout
	}
	// abortGen snapshots the user's escape request at entry. Esc or Ctrl-C
	// pressed from now on interrupts this call; a press from before it started
	// does not leak into the next timer callback or page.
	abortGen := j.host.AbortGen()
	done := make(chan struct{})
	// The watchdog re-arms itself while the host is blocked in a modal dialog
	// or a synchronous fetch. Network and user time are not script CPU time,
	// and interrupting a module mid-evaluation would leave its promise pending.
	var timer atomic.Pointer[time.Timer]
	var check func()
	check = func() {
		select {
		case <-done:
			return
		default:
		}
		if j.dialogActive.Load() || j.ioActive.Load() {
			j.pauseDeadline(timeout)
			timer.Store(time.AfterFunc(timeout, check))
			return
		}
		if j.host.AbortGen() != abortGen {
			if j.loadDeadline.Load() != 0 {
				j.stopScripts.Store(true)
				j.stopReason.Store(jsStopUser)
			}
			j.vm.Interrupt("JavaScript stopped by user")
			return
		}
		if j.deadlineExpired() {
			j.stopScripts.Store(true)
			j.stopReason.Store(jsStopBudget)
			j.vm.Interrupt("JavaScript time limit exceeded")
			return
		}
		j.vm.Interrupt("JavaScript timed out")
	}
	timer.Store(time.AfterFunc(timeout, check))
	defer func() {
		close(done)
		if t := timer.Load(); t != nil {
			t.Stop()
		}
		j.vm.ClearInterrupt()
		if r := recover(); r != nil {
			j.host.LogJS("debug", "panic: "+fmt.Sprintf("%v", r)+"\n"+string(debug.Stack()))
			err = fmt.Errorf("%v", r)
		}
	}()
	// Drop inFlight before clearing the interrupt, so the sampler cannot
	// interrupt a call that has already finished.
	defer j.inFlight.Add(-1)
	return fn()
}

// setMemLimit updates the runtime's absolute heap ceiling and starts or stops
// the shared sampler.
func (j *jsRuntime) SetMemLimit(limit int64) {
	if j == nil {
		return
	}
	j.memLimit.Store(limit)
	if limit > 0 {
		j.startMemSampler()
	} else {
		j.stopMemSampler()
	}
}

func (j *jsRuntime) startMemSampler() {
	j.memMu.Lock()
	if j.stopped || j.memStop != nil || j.memLimit.Load() <= 0 {
		j.memMu.Unlock()
		return
	}
	stop := make(chan struct{})
	wake := make(chan struct{}, 1)
	j.memStop = stop
	j.memWake = wake
	j.memMu.Unlock()
	go j.sampleMemory(stop, wake)
}

func (j *jsRuntime) stopMemSampler() {
	j.memMu.Lock()
	if j.memStop != nil {
		close(j.memStop)
		j.memStop = nil
		j.memWake = nil
	}
	j.memMu.Unlock()
}

// wakeMemSampler starts a sampling burst when a guarded call begins, so an
// idle page does not keep waking for memory reads.
func (j *jsRuntime) wakeMemSampler() {
	j.memMu.Lock()
	wake := j.memWake
	j.memMu.Unlock()
	if wake == nil {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}

// sampleMemory is the single memory watchdog for a runtime. It sleeps while
// the page is idle and reads the heap at a modest interval only while a call
// is running.
func (j *jsRuntime) sampleMemory(stop <-chan struct{}, wake <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case <-wake:
		}
		ticker := time.NewTicker(75 * time.Millisecond)
		for {
			select {
			case <-stop:
				ticker.Stop()
				return
			case <-ticker.C:
				if j.inFlight.Load() == 0 {
					ticker.Stop()
					break
				}
				if j.dialogActive.Load() {
					continue
				}
				limit := j.memLimit.Load()
				if limit <= 0 {
					continue
				}
				var ms runtime.MemStats
				runtime.ReadMemStats(&ms)
				if ms.HeapAlloc > j.memBase+uint64(limit) {
					j.vm.Interrupt("JavaScript memory limit exceeded")
				}
			}
		}
	}
}

// pauseDeadline moves the page-wide deadline forward while the script waits on
// user or network time, so a modal dialog left open does not consume the
// script's CPU budget.
func (j *jsRuntime) pauseDeadline(by time.Duration) {
	for {
		old := j.loadDeadline.Load()
		if old == 0 {
			return
		}
		if j.loadDeadline.CompareAndSwap(old, old+int64(by)) {
			return
		}
	}
}

// deadlineExpired reports whether the page-wide load-time script budget is
// spent. It is false outside that phase.
func (j *jsRuntime) deadlineExpired() bool {
	dl := j.loadDeadline.Load()
	return dl != 0 && time.Now().UnixNano() >= dl
}

func (j *jsRuntime) RunCode(name, src string) (v sobek.Value, err error) {
	err = j.guard(func() error {
		var e error
		v, e = j.vm.RunScript(name, src)
		return e
	})
	return v, err
}

func (j *jsRuntime) call(fn sobek.Callable, this sobek.Value, args ...sobek.Value) (v sobek.Value, ok bool) {
	if fn == nil || j.stopped {
		return nil, false
	}
	var callErr error
	err := j.guard(func() error {
		v, callErr = fn(this, args...)
		return callErr
	})
	if err != nil {
		j.eventErr = true
		j.ReportError(err)
		return nil, false
	}
	return v, true
}

func (j *jsRuntime) consoleObject() *sobek.Object {
	o := j.vm.NewObject()
	logf := func(level string) func(sobek.FunctionCall) sobek.Value {
		return func(call sobek.FunctionCall) sobek.Value {
			parts := make([]string, 0, len(call.Arguments))
			for _, a := range call.Arguments {
				parts = append(parts, j.consoleArg(a))
			}
			msg := strings.Join(parts, " ")
			j.host.LogJS(level, msg)
			return sobek.Undefined()
		}
	}
	_ = o.Set("log", j.vm.ToValue(logf("log")))
	_ = o.Set("info", j.vm.ToValue(logf("info")))
	_ = o.Set("warn", j.vm.ToValue(logf("warn")))
	_ = o.Set("error", j.vm.ToValue(logf("error")))
	return o
}

// consoleArg renders one console argument. Error objects keep their stack so a
// caught-and-logged exception still shows where it was thrown.
func (j *jsRuntime) consoleArg(v sobek.Value) string {
	if obj, ok := v.(*sobek.Object); ok {
		if st := obj.Get("stack"); st != nil && !sobek.IsUndefined(st) && !sobek.IsNull(st) {
			if s := strings.TrimSpace(st.String()); s != "" {
				return s
			}
		}
	}
	return v.String()
}

func (j *jsRuntime) windowObject() *sobek.Object {
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
	_ = o.Set("innerWidth", CSSWidthPx(j.host.Width()))
	_ = o.Set("innerHeight", CSSHeightPx(j.host.Height()))
	_ = o.Set("outerWidth", CSSWidthPx(j.host.Width()))
	_ = o.Set("outerHeight", CSSHeightPx(j.host.Height()))
	_ = o.Set("devicePixelRatio", 1)
	scrollPx := func() int { return int(float64(j.host.Scroll()) * cellH) }
	j.accessor(o, "scrollX", func() sobek.Value { return j.vm.ToValue(0) }, nil)
	j.accessor(o, "scrollY", func() sobek.Value { return j.vm.ToValue(scrollPx()) }, nil)
	j.accessor(o, "pageXOffset", func() sobek.Value { return j.vm.ToValue(0) }, nil)
	j.accessor(o, "pageYOffset", func() sobek.Value { return j.vm.ToValue(scrollPx()) }, nil)
	origin, secure := "", false
	if j.page != nil {
		pageOrigin := protocol.OriginOf(j.page.URL)
		origin = pageOrigin.HeaderValue()
		secure = pageOrigin.Scheme == "https"
	}
	_ = o.Set("origin", origin)
	_ = o.Set("isSecureContext", secure)
	_ = o.Set("crossOriginIsolated", false)
	_ = o.Set("name", "")
	_ = o.Set("frames", o)
	_ = o.Set("length", 0)
	_ = o.Set("opener", sobek.Null())
	_ = o.Set("closed", false)
	_ = o.Set("frameElement", sobek.Null())
	scrollWindowTo := func(call sobek.FunctionCall) sobek.Value {
		top := 0.0
		if obj := jsObject(call.Argument(0)); obj != nil {
			if v := obj.Get("top"); v != nil && !sobek.IsUndefined(v) && !sobek.IsNull(v) {
				top = v.ToFloat()
			}
		} else {
			top = call.Argument(1).ToFloat()
		}
		j.host.SetScroll(int(top / cellH))
		j.host.ClampScroll()
		return sobek.Undefined()
	}
	_ = o.Set("scroll", j.vm.ToValue(scrollWindowTo))
	_ = o.Set("scrollTo", j.vm.ToValue(scrollWindowTo))
	_ = o.Set("scrollBy", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		delta := 0.0
		if obj := jsObject(call.Argument(0)); obj != nil {
			if v := obj.Get("top"); v != nil && !sobek.IsUndefined(v) && !sobek.IsNull(v) {
				delta = v.ToFloat()
			}
		} else {
			delta = call.Argument(1).ToFloat()
		}
		j.host.SetScroll(j.host.Scroll() + int(delta/cellH))
		j.host.ClampScroll()
		return sobek.Undefined()
	}))
	windowNoop := j.vm.ToValue(func(sobek.FunctionCall) sobek.Value { return sobek.Undefined() })
	for _, name := range []string{"focus", "blur", "stop", "close", "postMessage"} {
		_ = o.Set(name, windowNoop)
	}
	_ = o.Set("open", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value { return sobek.Null() }))
	_ = o.Set("getComputedStyle", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		return j.computedStyleObject(j.unwrap(call.Argument(0)))
	}))
	screen := j.vm.NewObject()
	_ = screen.Set("width", CSSWidthPx(j.host.Width()))
	_ = screen.Set("height", CSSHeightPx(j.host.Height()))
	_ = screen.Set("availWidth", CSSWidthPx(j.host.Width()))
	_ = screen.Set("availHeight", CSSHeightPx(j.host.Height()))
	_ = screen.Set("colorDepth", 24)
	_ = screen.Set("pixelDepth", 24)
	_ = o.Set("screen", screen)
	vv := j.vm.NewObject()
	_ = vv.Set("width", CSSWidthPx(j.host.Width()))
	_ = vv.Set("height", CSSHeightPx(j.host.Height()))
	_ = vv.Set("scale", 1)
	_ = vv.Set("offsetLeft", 0)
	_ = vv.Set("offsetTop", 0)
	_ = vv.Set("pageLeft", 0)
	_ = vv.Set("pageTop", 0)
	vvNoop := j.vm.ToValue(func(sobek.FunctionCall) sobek.Value { return sobek.Undefined() })
	_ = vv.Set("addEventListener", vvNoop)
	_ = vv.Set("removeEventListener", vvNoop)
	_ = o.Set("visualViewport", vv)
	_ = o.Set("console", j.consoleObject())
	_ = o.Set("setTimeout", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value { return j.setTimer(call, false) }))
	_ = o.Set("setInterval", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value { return j.setTimer(call, true) }))
	_ = o.Set("clearTimeout", j.vm.ToValue(j.clearTimer))
	_ = o.Set("clearInterval", j.vm.ToValue(j.clearTimer))
	_ = o.Set("requestAnimationFrame", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		return j.setTimer(sobek.FunctionCall{Arguments: []sobek.Value{call.Argument(0), j.vm.ToValue(16)}}, false)
	}))
	_ = o.Set("cancelAnimationFrame", j.vm.ToValue(j.clearTimer))
	_ = o.Set("matchMedia", j.vm.ToValue(j.matchMediaFunc()))
	_ = o.Set("requestIdleCallback", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		return j.setTimer(sobek.FunctionCall{Arguments: []sobek.Value{call.Argument(0), j.vm.ToValue(1)}}, false)
	}))
	_ = o.Set("cancelIdleCallback", j.vm.ToValue(j.clearTimer))
	_ = o.Set("addEventListener", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		j.addListener(&j.winListeners, call.Argument(0).String(), call.Argument(1), call.Argument(2))
		return sobek.Undefined()
	}))
	_ = o.Set("removeEventListener", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		j.removeListener(j.winListeners, call.Argument(0).String(), call.Argument(1))
		return sobek.Undefined()
	}))
	_ = o.Set("dispatchEvent", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue(j.dispatchWindowEvent(call.Argument(0)))
	}))
	_ = o.DefineAccessorProperty("onload", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
		if j.winOnLoad == nil {
			return sobek.Null()
		}
		return j.winOnLoad
	}), j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		j.winOnLoad = call.Argument(0)
		return sobek.Undefined()
	}), sobek.FLAG_TRUE, sobek.FLAG_FALSE)
	_ = o.Set("window", o)
	_ = o.Set("self", o)
	_ = o.Set("top", o)
	_ = o.Set("parent", o)
	return o
}

func (j *jsRuntime) setTimer(call sobek.FunctionCall, repeat bool) sobek.Value {
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

func (j *jsRuntime) timerFunc(v sobek.Value) (sobek.Callable, bool) {
	if fn, ok := sobek.AssertFunction(v); ok {
		return fn, true
	}
	if s, ok := v.Export().(string); ok && strings.TrimSpace(s) != "" {
		fv, err := j.RunCode("timer", "(function(event){"+s+"\n})")
		if err != nil {
			j.ReportError(err)
			return nil, false
		}
		return sobek.AssertFunction(fv)
	}
	return nil, false
}

func (j *jsRuntime) clearTimer(call sobek.FunctionCall) sobek.Value {
	id := int(call.Argument(0).ToInteger())
	delete(j.timers, id)
	return sobek.Undefined()
}

// tick runs due timers and returns whether any callback ran.
func (j *jsRuntime) Tick(now time.Time) bool {
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
		j.call(t.fn, sobek.Undefined(), t.args...)
		ran = true
	}
	return ran
}

func (j *jsRuntime) addListener(m *map[string][]jsListener, typ string, fnVal, opts sobek.Value) {
	fn, ok := sobek.AssertFunction(fnVal)
	if !ok {
		return
	}
	once := false
	if opts != nil && !sobek.IsUndefined(opts) && !sobek.IsNull(opts) {
		if o, ok := opts.(*sobek.Object); ok {
			if v := o.Get("once"); v != nil && !sobek.IsUndefined(v) && !sobek.IsNull(v) {
				once = v.ToBoolean()
			}
		}
	}
	(*m)[typ] = append((*m)[typ], jsListener{value: fnVal, call: fn, once: once})
}

func (j *jsRuntime) addNodeListener(n *Node, typ string, fnVal, opts sobek.Value) {
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

func (j *jsRuntime) removeListener(m map[string][]jsListener, typ string, fnVal sobek.Value) {
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

func (j *jsRuntime) removeNodeListener(n *Node, typ string, fnVal sobek.Value) {
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
	target    sobek.Value
	path      []sobek.Value
	prevented bool
	// stopped stops propagation to ancestors; immediate also stops the
	// remaining listeners on the element that is currently handling the event.
	stopped   bool
	immediate bool
}

func (j *jsRuntime) eventObject(ev *jsEvent) *sobek.Object {
	o := j.vm.NewObject()
	_ = o.Set("type", ev.typ)
	_ = o.Set("target", ev.target)
	_ = o.Set("bubbles", ev.typ == "click")
	_ = o.Set("cancelable", true)
	_ = o.DefineAccessorProperty("defaultPrevented", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue(ev.prevented)
	}), nil, sobek.FLAG_TRUE, sobek.FLAG_FALSE)
	_ = o.Set("preventDefault", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
		ev.prevented = true
		return sobek.Undefined()
	}))
	_ = o.Set("stopPropagation", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
		ev.stopped = true
		return sobek.Undefined()
	}))
	_ = o.Set("stopImmediatePropagation", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
		ev.stopped = true
		ev.immediate = true
		return sobek.Undefined()
	}))
	_ = o.Set("composedPath", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
		if len(ev.path) == 0 {
			return j.vm.NewArray(ev.target)
		}
		items := make([]interface{}, len(ev.path))
		for i, v := range ev.path {
			items[i] = v
		}
		return j.vm.NewArray(items...)
	}))
	return o
}

func (j *jsRuntime) callList(m map[string][]jsListener, typ string, this, evObj sobek.Value, ev *jsEvent) {
	list := m[typ]
	for i := 0; i < len(list); i++ {
		l := list[i]
		j.call(l.call, this, evObj)
		if l.once {
			list = append(list[:i], list[i+1:]...)
			m[typ] = list
			i--
		}
		if ev.immediate {
			return
		}
	}
}

// fireNodeEvent dispatches an event on an element and bubbles to document and
// window. It returns whether default behavior was prevented.
func (j *jsRuntime) FireNodeEvent(n *Node, typ string) bool {
	if j == nil || j.stopped || n == nil {
		return false
	}
	j.eventErr = false
	ev := &jsEvent{typ: typ, target: j.wrap(n)}
	for p := n; p != nil; p = p.Parent {
		if p.Type == ElementNode {
			ev.path = append(ev.path, j.wrap(p))
		}
	}
	ev.path = append(ev.path, j.docObj, j.winObj)
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

func (j *jsRuntime) FireDocumentEvent(typ string) {
	if j == nil || j.stopped {
		return
	}
	ev := &jsEvent{typ: typ, target: j.docObj, path: []sobek.Value{j.docObj, j.winObj}}
	evObj := j.eventObject(ev)
	j.callList(j.docListeners, typ, j.docObj, evObj, ev)
	if !ev.stopped {
		j.callList(j.winListeners, typ, j.winObj, evObj, ev)
	}
}

func (j *jsRuntime) FireWindowEvent(typ string) {
	if j == nil || j.stopped {
		return
	}
	ev := &jsEvent{typ: typ, target: j.winObj, path: []sobek.Value{j.winObj}}
	evObj := j.eventObject(ev)
	j.callList(j.winListeners, typ, j.winObj, evObj, ev)
	if typ == "load" && j.winOnLoad != nil {
		if fn, ok := sobek.AssertFunction(j.winOnLoad); ok {
			j.call(fn, j.winObj, evObj)
		}
	}
}

func (j *jsRuntime) inlineHandler(n *Node, attr string, evObj sobek.Value, ev *jsEvent) {
	code := strings.TrimSpace(n.Attr(attr))
	if code == "" {
		return
	}
	fv, err := j.RunCode("inline "+attr, "(function(event){"+code+"\n})")
	if err != nil {
		j.eventErr = true
		j.ReportError(err)
		return
	}
	fn, ok := sobek.AssertFunction(fv)
	if !ok {
		return
	}
	res, ok := j.call(fn, j.wrap(n), evObj)
	if ok && res != nil && !sobek.IsUndefined(res) && !res.ToBoolean() {
		if ev != nil {
			ev.prevented = true
		} else if o, ok := evObj.(*sobek.Object); ok {
			_ = o.Set("defaultPrevented", true)
		}
	}
}

// dispatchClick runs click handlers for n. It returns true when default
// navigation should be suppressed.
func (j *jsRuntime) DispatchClick(n *Node) bool {
	return j.FireNodeEvent(n, "click")
}

// NodeCount reports the number of wrapped DOM nodes for cache accounting.
func (j *jsRuntime) NodeCount() int { return len(j.nodes) }

// ModuleErr reports whether a module script failed.
func (j *jsRuntime) ModuleErr() bool { return j.moduleErr }

// Dirty reports unflushed DOM mutations.
func (j *jsRuntime) Dirty() bool     { return j.dirty }
func (j *jsRuntime) SetDirty(v bool) { j.dirty = v }

// PendingNav reports a queued scripted navigation.
func (j *jsRuntime) PendingNav() string           { return j.pendingNav }
func (j *jsRuntime) SetPendingNav(v string)       { j.pendingNav = v }
func (j *jsRuntime) PendingReplace() bool         { return j.pendingReplace }
func (j *jsRuntime) PendingForm() *FormSubmission { return j.pendingForm }

// StorageLocalData and StorageSessionData return snapshots of the frame's
// shared or tab-scoped storage areas.
func (j *jsRuntime) StorageLocalData() map[string]string {
	if j.storageLocalView == nil {
		return nil
	}
	return j.storageLocalView.Snapshot()
}

func (j *jsRuntime) StorageSessionData() map[string]string {
	if j.storageSessionView == nil {
		return nil
	}
	return j.storageSessionView.Snapshot()
}

// TimerCount reports scheduled timers.
func (j *jsRuntime) TimerCount() int { return len(j.timers) }

// NextTimerDelay reports the earliest timer deadline.
func (j *jsRuntime) NextTimerDelay() (time.Duration, bool) {
	var next time.Time
	for _, t := range j.timers {
		if next.IsZero() || t.due.Before(next) {
			next = t.due
		}
	}
	if next.IsZero() {
		return 0, false
	}
	d := time.Until(next)
	if d < 0 {
		d = 0
	}
	return d, true
}

// VM returns the sobek runtime for tests and host inspection.
func (j *jsRuntime) VM() *sobek.Runtime { return j.vm }

// EventErr reports a listener exception.
func (j *jsRuntime) EventErr() bool { return j.eventErr }

func (j *jsRuntime) SetPendingForm(f *FormSubmission) { j.pendingForm = f }
func (j *jsRuntime) SetPendingReplace(v bool)         { j.pendingReplace = v }
func (j *jsRuntime) SetPageBudget(d time.Duration) {
	if d > 0 {
		j.pageBudget = d
	}
}
func (j *jsRuntime) ModuleErrValue() bool { return j.moduleErr }

// JSRuntime is the exported runtime type.
type JSRuntime = jsRuntime

// RemoveNoscript strips <noscript> subtrees when scripting is enabled.
func RemoveNoscript(root *Node) { removeNoscript(root) }

func (j *jsRuntime) TemplateCount() int     { return len(j.templates) }
func (j *jsRuntime) DatasetCount() int      { return len(j.datasetObjs) }
func (j *jsRuntime) ModuleSourceBytes() int { return j.modules.sourceBytes() }

func (j *jsRuntime) SetTimeout(d time.Duration) { j.timeout = d }

func (j *jsRuntime) MemSamplerActive() bool { return j.memStop != nil }

func (j *jsRuntime) SetMemBase(v uint64) { j.memBase = v }
