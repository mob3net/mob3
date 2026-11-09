package browser

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// maxFrameTime bounds the style+layout work for one rebuild. A page that
	// cannot render within this budget produces a partial layout instead of
	// freezing the UI.
	maxFrameTime = 2 * time.Second
	// jsRedrawInterval is the minimum gap between timer-driven rebuilds. The
	// previous rebuild cost is added so a slow page cannot spend all its time
	// re-laying out from timers.
	jsRedrawInterval = 250 * time.Millisecond

	layoutTruncatedStatus   = "Page truncated (layout limit)"
	moduleUnsupportedStatus = "Page requires ES modules (unsupported)"
	maxClipboardBytes       = 100 * 1024
)

// Browser holds navigation and view state.
type Browser struct {
	loader *Loader
	url    *url.URL
	page   *Page

	width, height int
	layout        *Layout
	scroll        int
	focus         int
	hover         int
	jsErr         bool
	status        string

	// Reader mode renders an extracted article document instead of the page.
	reader            bool
	readerRoot        *Node
	readerSavedScroll int

	// History and bookmarks.
	history   []string
	histIdx   int
	histMu    sync.RWMutex
	home      string
	cliConfig map[string]string

	prompt          bool
	promptText      string
	promptCursor    int
	promptSelectAll bool
	skipDraw        bool
	clipboardWriter io.Writer

	color   bool
	prev    []string
	gfx     *gfxRenderer
	bigText bool

	noJS       bool
	noBigText  bool
	jsMemLimit int64
	js         *jsRuntime

	loadCh     chan loadResult
	loadSeq    int
	cancelLoad context.CancelFunc
	loading    bool
	loadFn     func(context.Context, string, string, []byte, string) (*Page, error)

	lastMethod      string
	lastBody        []byte
	lastContentType string

	lastRebuild time.Time
	lastBuild   time.Duration

	inputFocus    *Node
	consoleOpen   bool
	consoleScroll int
	reqScroll     int
	storageScroll int
	htmlScroll    int
	consolePane   int // 0 = JS logs, 1 = requests, 2 = storage, 3 = HTML
	consoleText   string
	consoleCursor int
	consoleEdit   bool // the JavaScript input row inside the logs section has focus
	consoleHist   []string
	consoleHistAt int
	consoleDraft  string

	findActive  bool
	findText    string
	findCursor  int
	findScope   int
	findMatches []findMatch
	findIdx     int

	htmlPage  *Page
	htmlWidth int
	htmlCache []string
	jsLog     []string

	keys   <-chan string
	dialog *jsDialog

	lastScroll int
	usage      procUsage
	scrollBar  *scrollBarInfo
	scrollDrag bool

	headerHits     []headerHit
	footerHits     []headerHit
	consoleTabHits []consoleTabHit

	selAnchor cellPos
	selCursor cellPos
	selecting bool
}

type loadResult struct {
	seq         int
	page        *Page
	err         error
	u           *url.URL
	push        bool
	method      string
	body        []byte
	contentType string
}

// cellPos is a cell in document coordinates (row 0 is the top of the page).
type cellPos struct{ X, Y int }

// scrollBarInfo records the page scrollbar track/thumb for mouse hit testing.
type scrollBarInfo struct {
	trackTop, trackH int
	thumbTop, thumbH int
}

type headerKind int

const (
	headerBack headerKind = iota
	headerForward
	headerReload
	headerHome
	headerBookmarks
	headerHistory
	headerURL
	headerHelp
	headerConfig
	headerConsole
)

// headerHit is a clickable region in the top bar, in zero-based screen cells.
type headerHit struct {
	kind headerKind
	x, y int
	w, h int
}

// consoleTabHit is a clickable console menu tab.
type consoleTabHit struct {
	pane int
	x, w int
}

// NewBrowser creates a browser with a viewport size.
func NewBrowser(w, h int) *Browser {
	if w < 20 {
		w = 80
	}
	if h < 5 {
		h = 24
	}
	b := &Browser{
		loader: NewLoader(), width: w, height: h, focus: -1, hover: -1, histIdx: -1,
		jsMemLimit: defaultJSMemoryLimit,
		loadCh:     make(chan loadResult, 1),
		home:       "about:home",
	}
	b.setLoader(b.loader)
	return b
}

// setLoader installs a loader and wires it to this browser's session history so
// about:history reflects the current browser even when the CLI replaces the
// default loader.
func (b *Browser) setLoader(l *Loader) {
	b.loader = l
	if l != nil {
		l.historyFn = b.historySnapshot
	}
}

func (b *Browser) historySnapshot() ([]string, int) {
	b.histMu.RLock()
	defer b.histMu.RUnlock()
	return append([]string(nil), b.history...), b.histIdx
}

// hasBookmarks reports whether a bookmark file was supplied to the browser.
func (b *Browser) hasBookmarks() bool {
	return b.loader != nil && (b.loader.bookmarksSet || len(b.loader.bookmarks) > 0)
}

// searchTemplate returns the configured search URL template.
func (b *Browser) searchTemplate() string {
	if b.loader != nil {
		return b.loader.searchURL
	}
	return DefaultSearchURL
}

// headerRows returns the number of rows used by the top bar.
func (b *Browser) headerRows() int {
	if b.height < 3 {
		return 0
	}
	if b.height < 5 {
		return 1
	}
	return 2
}

// viewport returns the first and last content row (exclusive end).
func (b *Browser) viewport() (int, int) {
	if b.height < 3 {
		return 0, b.height
	}
	return b.headerRows(), b.height - 1
}

func (b *Browser) viewportHeight() int {
	top, bottom := b.viewport()
	if bottom < top {
		return 1
	}
	return bottom - top
}

// Navigate starts loading input in the background so the UI and Esc stay
// responsive. Results are applied by finishLoad from the main loop.
func (b *Browser) Navigate(input string, push bool) {
	b.navigateRequest(http.MethodGet, input, nil, "", push)
}

func (b *Browser) navigateRequest(method, input string, body []byte, contentType string, push bool) {
	b.navigateRequestForce(method, input, body, contentType, push, false)
}

// navigateRequestForce is navigateRequest with an optional one-shot block
// bypass. It backs the "Try anyway" link (about:retry) on the blocked page.
func (b *Browser) navigateRequestForce(method, input string, body []byte, contentType string, push bool, forceBlocks bool) {
	if strings.EqualFold(strings.TrimSpace(input), "about:retry") {
		if b.url == nil {
			b.status = "Nothing to retry"
			b.draw()
			return
		}
		input = b.url.String()
		forceBlocks = true
		push = false
	}
	u, err := ResolveInput(input, b.url, b.searchTemplate())
	if err != nil {
		b.status = "Invalid URL: " + err.Error()
		b.draw()
		return
	}
	if method == "" {
		method = http.MethodGet
	}
	if isAboutConfigURL(u) {
		b.clearInputFocus()
		b.stopJS()
		b.cancelLoadNow()
		if b.loader != nil {
			b.loader.beginPage()
		}
		b.loadSeq++
		b.applyConfig(u.Query())
		page := b.configPage(u, len(u.Query()) > 0)
		b.finishLoad(loadResult{seq: b.loadSeq, page: page, u: u, push: push, method: http.MethodGet})
		return
	}
	if u.Scheme == "file" && b.url != nil {
		switch b.url.Scheme {
		case "http", "https", "data":
			b.status = "Blocked local file navigation from a remote page"
			b.draw()
			return
		}
	}
	b.clearInputFocus()
	b.stopJS()
	b.cancelLoadNow()
	if b.loader != nil {
		b.loader.beginPage()
	}
	b.loadSeq++
	seq := b.loadSeq
	ctx, cancel := context.WithCancel(context.Background())
	b.cancelLoad = cancel
	b.loading = true
	b.status = "Loading " + u.String() + " … (Esc stops)"
	b.clearSelection()

	target := u.String()
	load := b.loadFn
	if load == nil {
		loader := b.loader
		load = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
			if forceBlocks {
				return loader.LoadRequestBypass(ctx, method, target, body, contentType, nil)
			}
			return loader.LoadRequest(ctx, method, target, body, contentType, nil)
		}
	}
	go func() {
		page, loadErr := load(ctx, method, target, body, contentType)
		select {
		case b.loadCh <- loadResult{
			seq: seq, page: page, err: loadErr, u: u, push: push,
			method: method, body: body, contentType: contentType,
		}:
		case <-ctx.Done():
		}
	}()
}

func (b *Browser) cancelLoadNow() {
	if b.cancelLoad != nil {
		b.cancelLoad()
		b.cancelLoad = nil
	}
	b.loading = false
}

// stopLoading aborts the in-flight page load and ignores its late result.
func (b *Browser) stopLoading() {
	if !b.loading {
		return
	}
	b.loadSeq++
	b.cancelLoadNow()
	b.status = "Stopped loading"
}

// finishLoad applies a completed load. Stale results from canceled navigations
// are ignored.
func (b *Browser) finishLoad(res loadResult) {
	if res.seq != b.loadSeq {
		return
	}
	b.jsErr = false
	b.cancelLoad = nil
	b.loading = false
	b.reader = false
	b.readerRoot = nil
	b.readerSavedScroll = 0
	if res.err != nil {
		b.status = "Error: " + res.err.Error()
		if b.page == nil {
			doc := "<html><body><h2>Could not load page</h2><p>" + EscapeHTML(res.err.Error()) + "</p></body></html>"
			root := ParseHTML(doc)
			b.page = &Page{URL: res.u, Base: res.u, Root: root, RawHTML: doc, Title: "Error", Status: 0}
			b.url = res.u
			b.rebuild()
		}
		return
	}
	page := res.page
	b.page = page
	b.url = page.URL
	b.lastMethod = res.method
	b.lastBody = res.body
	b.lastContentType = res.contentType
	b.layout = nil
	// <noscript> is not active when scripting is enabled. Remove it before
	// evaluating meta refresh, otherwise no-JS bypass pages redirect us to
	// their "enable JavaScript" endpoint.
	if !b.noJS {
		removeNoscript(page.Root)
	}
	if target := metaRefreshTarget(page); target != "" && target != page.URL.String() {
		b.Navigate(target, true)
		return
	}
	b.resetConsole()
	b.startJS(page)
	if b.consumePendingJS() {
		return
	}
	b.rebuild()
	b.scroll = 0
	b.focus = -1
	b.status = ""
	if page.Blocked {
		b.status = "Site blocked — browser check not supported"
	} else if page.Status >= 400 {
		b.status = fmt.Sprintf("HTTP %d %s", page.Status, http.StatusText(page.Status))
	} else if page.HasModules {
		b.status = moduleUnsupportedStatus
	} else if b.layout != nil && b.layout.Truncated {
		b.status = layoutTruncatedStatus
	}
	if res.push {
		b.pushHistory(page.URL.String())
	}
	if frag := res.u.Fragment; frag != "" {
		b.scrollToAnchor(frag)
	}
	b.prev = nil
}

// consumePendingJS applies a queued location.href/assign navigation after the
// current script or callback has finished.
func (b *Browser) consumePendingJS() bool {
	if b.js == nil || b.js.pendingNav == "" {
		return false
	}
	target := b.js.pendingNav
	b.js.pendingNav = ""
	b.Navigate(target, true)
	return true
}

func metaRefreshTarget(page *Page) string {
	if page == nil || page.Root == nil {
		return ""
	}
	metas := page.Root.FindAll(func(n *Node) bool {
		return n.Tag == "meta" && strings.EqualFold(n.Attr("http-equiv"), "refresh")
	})
	for _, m := range metas {
		content := strings.TrimSpace(m.Attr("content"))
		if content == "" {
			continue
		}
		rest := content
		if i := strings.IndexByte(content, ';'); i >= 0 {
			rest = content[i+1:]
		}
		rest = strings.TrimSpace(rest)
		if len(rest) >= 4 && strings.EqualFold(rest[:4], "url=") {
			rest = rest[4:]
		}
		rest = strings.Trim(strings.TrimSpace(rest), `"'`)
		if rest == "" {
			continue
		}
		base := page.Base
		if base == nil {
			base = page.URL
		}
		if base == nil {
			return ""
		}
		if u, err := base.Parse(rest); err == nil {
			return u.String()
		}
	}
	return ""
}

func (b *Browser) pushHistory(u string) {
	b.histMu.Lock()
	defer b.histMu.Unlock()
	if b.histIdx >= 0 && b.histIdx < len(b.history) && b.history[b.histIdx] == u {
		return
	}
	if b.histIdx+1 < len(b.history) {
		b.history = b.history[:b.histIdx+1]
	}
	b.history = append(b.history, u)
	b.histIdx = len(b.history) - 1
}

func (b *Browser) back() {
	b.histMu.Lock()
	if b.histIdx <= 0 {
		b.histMu.Unlock()
		b.status = "No previous page"
		return
	}
	b.histIdx--
	u := b.history[b.histIdx]
	b.histMu.Unlock()
	b.Navigate(u, false)
}

func (b *Browser) forward() {
	b.histMu.Lock()
	if b.histIdx < 0 || b.histIdx+1 >= len(b.history) {
		b.histMu.Unlock()
		b.status = "No next page"
		return
	}
	b.histIdx++
	u := b.history[b.histIdx]
	b.histMu.Unlock()
	b.Navigate(u, false)
}

func (b *Browser) reload() {
	if b.url == nil {
		return
	}
	method := b.lastMethod
	if method == "" {
		method = http.MethodGet
	}
	b.navigateRequest(method, b.url.String(), b.lastBody, b.lastContentType, false)
}

func (b *Browser) rebuild() {
	if b.page == nil {
		return
	}
	start := time.Now()
	b.bigText = b.gfx != nil && b.gfx.TextSizing() && !b.noBigText
	root, author := b.page.Root, b.page.Author
	if b.reader && b.readerRoot != nil {
		root, author = b.readerRoot, readerRules
	}
	b.layout = BuildLayoutDeadline(root, author, b.width, b.bigText, start.Add(maxFrameTime))
	b.lastBuild = time.Since(start)
	b.lastRebuild = time.Now()
	b.reindexFind()
	if b.layout.Truncated {
		if b.status == "" {
			b.status = layoutTruncatedStatus
		}
	} else if b.status == layoutTruncatedStatus {
		b.status = ""
	}
	b.hover = -1
	b.clampScroll()
}

// toggleReader switches between the page and an extracted article view without
// touching history or the live page DOM.
func (b *Browser) toggleReader() {
	if b.page == nil || b.page.Root == nil {
		b.status = "Reader mode unavailable"
		return
	}
	if b.reader {
		b.reader = false
		b.readerRoot = nil
		b.rebuild()
		b.scroll = b.readerSavedScroll
		b.readerSavedScroll = 0
		b.clampScroll()
		b.status = "Reader mode off"
		return
	}
	root, ok := extractReaderPage(b.page.Root, b.page.Title)
	if !ok {
		b.status = "Reader mode unavailable"
		return
	}
	b.readerSavedScroll = b.scroll
	b.reader = true
	b.readerRoot = root
	b.scroll = 0
	b.focus = -1
	b.clearSelection()
	b.rebuild()
	b.status = "Reader mode — R to exit"
}

func (b *Browser) resize(w, h int) {
	if w < 10 {
		w = 10
	}
	if h < 3 {
		h = 3
	}
	if w == b.width && h == b.height {
		return
	}
	b.width, b.height = w, h
	b.prev = nil
	b.clearSelection()
	b.rebuild()
}

func (b *Browser) maxScroll() int {
	if b.layout == nil {
		return 0
	}
	m := b.layout.Height - b.viewportHeight()
	if m < 0 {
		return 0
	}
	return m
}

func (b *Browser) clampScroll() {
	max := b.maxScroll()
	if b.scroll < 0 {
		b.scroll = 0
	}
	if b.scroll > max {
		b.scroll = max
	}
}

func (b *Browser) scrollBy(n int) {
	b.hover = -1
	b.scroll += n
	b.clampScroll()
	if !strings.HasPrefix(b.status, "HTTP ") && !strings.HasPrefix(b.status, "Error") {
		b.status = ""
	}
}

// drawScrollBar paints the page scrollbar on the rightmost content column.
func (b *Browser) drawScrollBar(g *Grid, top, bottom int) {
	b.scrollBar = nil
	max := b.maxScroll()
	vh := bottom - top
	if max <= 0 || vh < 2 || g.W < 2 {
		return
	}
	thumbH := vh * vh / (vh + max)
	if thumbH < 1 {
		thumbH = 1
	}
	if thumbH > vh {
		thumbH = vh
	}
	thumbTop := top
	if vh > thumbH {
		thumbTop = top + (vh-thumbH)*b.scroll/max
	}
	g.Fill(g.W-1, top, 1, vh, RGB(0xdd, 0xe3, 0xea))
	g.Fill(g.W-1, thumbTop, 1, thumbH, RGB(0x7a, 0x8a, 0x9a))
	b.scrollBar = &scrollBarInfo{trackTop: top, trackH: vh, thumbTop: thumbTop, thumbH: thumbH}
}

// scrollBarTo maps a track row to a scroll position, centering the thumb.
func (b *Browser) scrollBarTo(y int) {
	info := b.scrollBar
	if info == nil {
		return
	}
	max := b.maxScroll()
	rel := y - info.trackTop - info.thumbH/2
	span := info.trackH - info.thumbH
	if span <= 0 || max <= 0 {
		b.scrollBy(-b.scroll)
		return
	}
	if rel < 0 {
		rel = 0
	}
	if rel > span {
		rel = span
	}
	target := rel * max / span
	b.scrollBy(target - b.scroll)
}

func (b *Browser) focusedLink() *LinkInfo {
	if b.layout == nil || b.focus < 0 || b.focus >= len(b.layout.Links) {
		return nil
	}
	return b.layout.Links[b.focus]
}

func (b *Browser) hoveredLink() *LinkInfo {
	if b.layout == nil || b.hover < 0 || b.hover >= len(b.layout.Links) {
		return nil
	}
	return b.layout.Links[b.hover]
}

// updateHover records the link under the pointer in content coordinates for
// the footer readout.
func (b *Browser) updateHover(x, y, top, bottom int) {
	idx := -1
	if y >= top && y < bottom {
		idx = b.linkAt(x, b.scroll+(y-top))
	}
	b.hover = idx
}

func (b *Browser) focusStep(delta int) {
	if b.layout == nil || len(b.layout.Links) == 0 {
		b.status = "No links on this page"
		return
	}
	n := len(b.layout.Links)
	if b.focus < 0 {
		if delta > 0 {
			b.focus = 0
		} else {
			b.focus = n - 1
		}
	} else {
		b.focus = (b.focus + delta + n) % n
	}
	b.ensureFocusVisible()
	li := b.layout.Links[b.focus]
	b.status = fmt.Sprintf("%d/%d  %s", b.focus+1, n, b.resolveHref(li.Href))
}

func (b *Browser) ensureFocusVisible() {
	li := b.focusedLink()
	if li == nil || len(li.Rects) == 0 {
		return
	}
	top, bottom := b.viewport()
	vh := bottom - top
	if vh < 1 {
		vh = 1
	}
	r := li.Rects[0]
	if r.Y < b.scroll {
		b.scroll = r.Y - 1
	} else if r.Y+r.H >= b.scroll+vh {
		b.scroll = r.Y + r.H - vh + 1
	}
	b.clampScroll()
}

func (b *Browser) resolveHref(href string) string {
	if b.page == nil || b.page.Base == nil {
		return href
	}
	if u, err := b.page.Base.Parse(href); err == nil {
		return u.String()
	}
	return href
}

func (b *Browser) followFocused() {
	li := b.focusedLink()
	if li == nil {
		b.status = "No link selected (press Tab)"
		return
	}
	href := li.Href
	low := strings.ToLower(strings.TrimSpace(href))
	if strings.HasPrefix(low, "javascript:") {
		b.status = "JavaScript links are not supported"
		return
	}
	if strings.HasPrefix(low, "mailto:") || strings.HasPrefix(low, "tel:") {
		b.status = "External protocol: " + href
		return
	}
	if b.page != nil && b.page.Base != nil {
		if u, err := b.page.Base.Parse(href); err == nil {
			if b.url != nil && u.Fragment != "" && sameDocument(u, b.url) {
				b.scrollToAnchor(u.Fragment)
				return
			}
			b.Navigate(u.String(), true)
			return
		}
	}
	b.Navigate(href, true)
}

func sameDocument(a, b *url.URL) bool {
	return a.Scheme == b.Scheme && a.Host == b.Host && a.Path == b.Path && a.RawQuery == b.RawQuery
}

func (b *Browser) scrollToAnchor(frag string) {
	if b.layout == nil || frag == "" {
		return
	}
	if unescaped, err := url.PathUnescape(frag); err == nil {
		frag = unescaped
	}
	best := -1
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		for p := f.Node; p != nil; p = p.Parent {
			if p.Type != ElementNode {
				continue
			}
			if p.Attr("id") == frag || p.Attr("name") == frag {
				if best < 0 || f.Y < best {
					best = f.Y
				}
				break
			}
		}
	}
	if best >= 0 {
		b.scroll = best - 1
		b.clampScroll()
	}
}

// linkAt returns the index of the link covering document cell (x, y), or -1.
func (b *Browser) linkAt(x, y int) int {
	if b.layout == nil {
		return -1
	}
	for i, li := range b.layout.Links {
		for _, r := range li.Rects {
			if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
				return i
			}
		}
	}
	return -1
}

// selectionOrder returns the selection endpoints in reading order.
func (b *Browser) selectionOrder() (cellPos, cellPos) {
	a, c := b.selAnchor, b.selCursor
	if a.Y > c.Y || (a.Y == c.Y && a.X > c.X) {
		a, c = c, a
	}
	return a, c
}

func (b *Browser) hasSelection() bool {
	return b.selAnchor != b.selCursor
}

func (b *Browser) clearSelection() {
	b.selAnchor = cellPos{}
	b.selCursor = cellPos{}
	b.selecting = false
}

// updateSelection moves the drag endpoint, auto-scrolling when it leaves the
// content viewport.
func (b *Browser) updateSelection(x, y, top, bottom int) {
	if y < top {
		b.scrollBy(-1)
		y = top
	} else if y >= bottom {
		b.scrollBy(1)
		y = bottom - 1
	}
	if x < 0 {
		x = 0
	}
	if x >= b.width {
		x = b.width - 1
	}
	docY := b.scroll + (y - top)
	if docY < 0 {
		docY = 0
	}
	b.selCursor = cellPos{X: x, Y: docY}
}

func isTextInput(n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	if n.Tag == "textarea" {
		return true
	}
	if n.Tag != "input" {
		return false
	}
	switch strings.ToLower(n.Attr("type")) {
	case "", "text", "search", "email", "url", "tel", "number", "password":
		return true
	}
	return false
}

func isSubmitControl(n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	if n.Tag == "button" {
		switch strings.ToLower(strings.TrimSpace(n.Attr("type"))) {
		case "", "submit":
			return true
		}
		return false
	}
	if n.Tag != "input" {
		return false
	}
	switch strings.ToLower(n.Attr("type")) {
	case "submit", "image":
		return true
	}
	return false
}

func isToggleControl(n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	if n.Tag == "select" {
		return true
	}
	if n.Tag != "input" {
		return false
	}
	switch strings.ToLower(n.Attr("type")) {
	case "checkbox", "radio":
		return true
	}
	return false
}

// toggleControl flips a checkbox or radio and rebuilds the form labels.
func (b *Browser) toggleControl(n *Node) {
	if n == nil {
		return
	}
	if n.Tag == "select" {
		var opts []*Node
		for _, opt := range n.FindAll(func(x *Node) bool { return x.Tag == "option" }) {
			opts = append(opts, opt)
		}
		if len(opts) > 1 {
			next := 0
			for i, opt := range opts {
				if opt.HasAttr("selected") {
					next = (i + 1) % len(opts)
					break
				}
			}
			for i, opt := range opts {
				if i == next {
					setAttr(opt, "selected", "")
				} else {
					removeAttr(opt, "selected")
				}
			}
		}
	} else {
		typ := strings.ToLower(n.Attr("type"))
		if typ == "radio" {
			name := n.Attr("name")
			if form := nearestForm(n); form != nil && name != "" {
				form.FindAll(func(x *Node) bool {
					if x != n && x.Tag == "input" && strings.EqualFold(x.Attr("type"), "radio") && x.Attr("name") == name {
						removeAttr(x, "checked")
					}
					return false
				})
			}
			setAttr(n, "checked", "")
		} else if n.HasAttr("checked") {
			removeAttr(n, "checked")
		} else {
			setAttr(n, "checked", "")
		}
	}
	if b.js != nil {
		b.js.fireNodeEvent(n, "input")
		b.js.fireNodeEvent(n, "change")
	}
	b.rebuild()
}

func nearestForm(n *Node) *Node {
	for p := n; p != nil; p = p.Parent {
		if p.Tag == "form" {
			return p
		}
	}
	return nil
}

func (b *Browser) clearInputFocus() {
	if b.inputFocus != nil {
		b.inputFocus.InputFocused = false
		b.inputFocus = nil
	}
}

func (b *Browser) setInputFocus(n *Node) {
	if b.inputFocus == n {
		return
	}
	if old := b.inputFocus; old != nil {
		old.InputFocused = false
		if b.js != nil {
			b.js.fireNodeEvent(old, "change")
		}
	}
	b.inputFocus = n
	if n != nil {
		n.InputFocused = true
		b.status = "Typing · Enter submits · Esc finishes"
	} else {
		b.status = ""
	}
	b.rebuild()
}

func (b *Browser) setInputValue(n *Node, v string) {
	if n == nil {
		return
	}
	n.Value = v
	n.ValueSet = true
	if b.js != nil {
		b.js.fireNodeEvent(n, "input")
	}
	if b.consumePendingJS() {
		return
	}
	b.rebuild()
}

// openURLPrompt opens the URL field prefilled with the current URL, selected
// so typing replaces it like a normal browser address bar.
func (b *Browser) openURLPrompt() {
	b.prompt = true
	b.promptText = ""
	if b.url != nil {
		b.promptText = b.url.String()
	}
	b.promptCursor = len(b.promptText)
	b.promptSelectAll = true
	b.status = ""
}

func prevRuneBoundary(s string, i int) int {
	if i <= 0 {
		return 0
	}
	if i > len(s) {
		i = len(s)
	}
	_, size := utf8.DecodeLastRuneInString(s[:i])
	return i - size
}

func nextRuneBoundary(s string, i int) int {
	if i >= len(s) {
		return len(s)
	}
	if i < 0 {
		i = 0
	}
	_, size := utf8.DecodeRuneInString(s[i:])
	return i + size
}

func (b *Browser) handlePromptKey(k string) bool {
	text := b.promptText
	cur := b.promptCursor
	if cur < 0 {
		cur = 0
	}
	if cur > len(text) {
		cur = len(text)
	}
	switch k {
	case "enter":
		text = strings.TrimSpace(text)
		b.prompt = false
		b.promptText = ""
		b.promptCursor = 0
		b.promptSelectAll = false
		if text != "" {
			b.Navigate(text, true)
		}
		return false
	case "esc":
		b.closeURLPrompt()
		b.status = exitHint
		return false
	case "left":
		if b.promptSelectAll {
			cur = 0
			b.promptSelectAll = false
		} else {
			cur = prevRuneBoundary(text, cur)
		}
	case "right":
		if b.promptSelectAll {
			cur = len(text)
			b.promptSelectAll = false
		} else {
			cur = nextRuneBoundary(text, cur)
		}
	case "home":
		cur = 0
		b.promptSelectAll = false
	case "end":
		cur = len(text)
		b.promptSelectAll = false
	case "ctrl-a":
		b.promptSelectAll = true
		cur = len(text)
	case "ctrl-e":
		cur = len(text)
		b.promptSelectAll = false
	case "backspace":
		if b.promptSelectAll {
			text = ""
			cur = 0
			b.promptSelectAll = false
		} else if cur > 0 {
			prev := prevRuneBoundary(text, cur)
			text = text[:prev] + text[cur:]
			cur = prev
		}
	case "delete":
		if b.promptSelectAll {
			text = ""
			cur = 0
			b.promptSelectAll = false
		} else if cur < len(text) {
			next := nextRuneBoundary(text, cur)
			text = text[:cur] + text[next:]
		}
	case "ctrl-u":
		text = ""
		cur = 0
		b.promptSelectAll = false
	case "ctrl-k":
		if b.promptSelectAll {
			text = ""
			cur = 0
			b.promptSelectAll = false
		} else {
			text = text[:cur]
		}
	case "ctrl-w":
		if b.promptSelectAll {
			text = ""
			cur = 0
			b.promptSelectAll = false
			break
		}
		before := strings.TrimRight(text[:cur], " ")
		cut := 0
		if i := strings.LastIndexAny(before, " /"); i >= 0 {
			cut = i + 1
		}
		text = text[:cut] + text[cur:]
		cur = cut
	case "ctrl-c":
		b.closeURLPrompt()
		return false
	default:
		if utf8.RuneCountInString(k) == 1 {
			r, _ := utf8.DecodeRuneInString(k)
			if r >= 32 && r != 127 {
				if b.promptSelectAll {
					text = k
					cur = len(k)
					b.promptSelectAll = false
				} else {
					text = text[:cur] + k + text[cur:]
					cur += len(k)
				}
			}
		}
	}
	b.promptText = text
	if cur > len(text) {
		cur = len(text)
	}
	b.promptCursor = cur
	return false
}

// closeURLPrompt dismisses the URL prompt and clears its editing state.
func (b *Browser) closeURLPrompt() {
	b.prompt = false
	b.promptText = ""
	b.promptCursor = 0
	b.promptSelectAll = false
}

// promptWindow returns the visible rune range for the URL text and the cursor
// rune position. cur is -1 for the full-selection view, which shows the tail of
// a long URL without a cursor.
func promptWindow(text string, cursor, contentW int, selectAll bool) (runes []rune, start, end, cur int) {
	runes = []rune(text)
	if contentW < 1 {
		return runes, 0, 0, 0
	}
	if selectAll {
		visible := text
		if stringWidth(visible) > contentW {
			visible = tailWidth(visible, contentW)
		}
		start = len(runes) - utf8.RuneCountInString(visible)
		return runes, start, len(runes), -1
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(text) {
		cursor = len(text)
	}
	cur = utf8.RuneCountInString(text[:cursor])
	if cur < 0 {
		cur = 0
	}
	if cur > len(runes) {
		cur = len(runes)
	}
	start = cur
	used := 0
	for start > 0 {
		rw := runeWidth(runes[start-1])
		if used+rw > contentW-1 {
			break
		}
		used += rw
		start--
	}
	after := cur
	if cur < len(runes) {
		after = cur + 1
	}
	used++
	end = after
	for end < len(runes) {
		rw := runeWidth(runes[end])
		if used+rw > contentW {
			break
		}
		used += rw
		end++
	}
	return runes, start, end, cur
}

func runeIndexToByte(runes []rune, i int) int {
	if i <= 0 {
		return 0
	}
	if i > len(runes) {
		i = len(runes)
	}
	return len(string(runes[:i]))
}

// promptOffsetAt maps a cell inside the URL field to a byte offset in the
// prompt text, using the same visible window that is currently drawn.
func (b *Browser) promptOffsetAt(x, width int) int {
	prefixW := stringWidth(" Open: ")
	contentW := width - prefixW
	if contentW < 1 {
		return 0
	}
	rel := x - prefixW
	if rel <= 0 {
		return 0
	}
	runes, start, end, cur := promptWindow(b.promptText, b.promptCursor, contentW, b.promptSelectAll)
	used := 0
	for i := start; i < end; i++ {
		if i == cur {
			if rel < used+1 {
				return runeIndexToByte(runes, i)
			}
			used++
			continue
		}
		rw := runeWidth(runes[i])
		if rel < used+rw {
			return runeIndexToByte(runes, i)
		}
		used += rw
	}
	return runeIndexToByte(runes, end)
}

// promptParts splits the visible URL field around the cursor. The cursor is an
// overlay cell rather than an inserted character, so moving it does not shift
// the text.
func promptParts(prefix, text string, cursor int, selectAll bool, width int) (head string, at rune, tail string, show bool) {
	prefixW := stringWidth(prefix)
	if width <= prefixW {
		return truncateWidth(prefix, width), 0, "", false
	}
	contentW := width - prefixW
	runes := []rune(text)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(text) {
		cursor = len(text)
	}
	cur := utf8.RuneCountInString(text[:cursor])
	if cur < 0 {
		cur = 0
	}
	if cur > len(runes) {
		cur = len(runes)
	}
	if selectAll {
		visible := text
		if stringWidth(visible) > contentW {
			visible = tailWidth(visible, contentW)
		}
		return prefix + visible, 0, "", false
	}
	start := cur
	used := 0
	for start > 0 {
		rw := runeWidth(runes[start-1])
		if used+rw > contentW-1 {
			break
		}
		used += rw
		start--
	}
	at = ' '
	after := cur
	if cur < len(runes) {
		at = runes[cur]
		if runeWidth(at) != 1 {
			at = ' '
		}
		after = cur + 1
	}
	used++
	end := after
	for end < len(runes) {
		rw := runeWidth(runes[end])
		if used+rw > contentW {
			break
		}
		used += rw
		end++
	}
	return prefix + string(runes[start:cur]), at, string(runes[after:end]), true
}

func (b *Browser) submitInput() {
	n := b.inputFocus
	if n == nil {
		return
	}
	form := nearestForm(n)
	b.setInputFocus(nil)
	if form != nil {
		b.submitForm(form)
	}
}

func (b *Browser) submitNode(n *Node) {
	b.clearInputFocus()
	if form := nearestForm(n); form != nil {
		b.submitForm(form)
	}
}

func (b *Browser) submitForm(form *Node) {
	if form == nil {
		return
	}
	prevented := false
	if b.js != nil {
		prevented = b.js.fireNodeEvent(form, "submit")
	}
	if b.consumePendingJS() {
		return
	}
	if prevented {
		return
	}
	method := strings.ToUpper(strings.TrimSpace(form.Attr("method")))
	if method == "" {
		method = http.MethodGet
	}
	action := strings.TrimSpace(form.Attr("action"))
	var u *url.URL
	var err error
	if action == "" {
		u = b.url
	} else if b.page != nil {
		u, err = ResolveURL(action, b.page.Base)
	}
	if err != nil || u == nil {
		b.status = "Invalid form action"
		return
	}
	vals := url.Values{}
	form.FindAll(func(x *Node) bool {
		if x.Tag != "input" && x.Tag != "textarea" && x.Tag != "select" {
			return false
		}
		name := x.Attr("name")
		if name == "" {
			return false
		}
		if x.Tag == "select" {
			vals.Add(name, inputValue(x))
			return false
		}
		typ := strings.ToLower(x.Attr("type"))
		switch typ {
		case "submit", "button", "reset", "image", "file":
			return false
		case "checkbox", "radio":
			if x.HasAttr("checked") {
				v := x.Attr("value")
				if v == "" {
					v = "on"
				}
				vals.Add(name, v)
			}
		default:
			vals.Add(name, inputValue(x))
		}
		return false
	})
	if method == http.MethodPost {
		b.navigateRequest(http.MethodPost, u.String(), []byte(vals.Encode()), "application/x-www-form-urlencoded", true)
		return
	}
	q := u.Query()
	for k, values := range vals {
		q[k] = values
	}
	u.RawQuery = q.Encode()
	b.Navigate(u.String(), true)
}

// nodeAt returns the nearest element under a document cell, or nil.
func (b *Browser) nodeAt(x, y int) *Node {
	if b.layout == nil {
		return nil
	}
	for i := len(b.layout.Frags) - 1; i >= 0; i-- {
		f := &b.layout.Frags[i]
		if f.Node == nil || (f.Kind != fragText && f.Kind != fragImage) {
			continue
		}
		if x < f.X || x >= f.X+f.W || y < f.Y || y >= f.Y+f.H {
			continue
		}
		for p := f.Node; p != nil; p = p.Parent {
			if p.Type == ElementNode {
				return p
			}
		}
	}
	return nil
}

// clickContent follows a link under the pointer, or clears link focus.
// JavaScript click handlers run first and may prevent navigation.
func (b *Browser) clickContent(x, docY int) {
	if idx := b.linkAt(x, docY); idx >= 0 {
		li := b.layout.Links[idx]
		if b.js != nil && li.Node != nil && b.js.dispatchClick(li.Node) {
			if b.js.consumeDirty() {
				b.rebuild()
			}
			b.consumePendingJS()
			return
		}
		if b.consumePendingJS() {
			return
		}
		b.focus = idx
		b.followFocused()
		return
	}
	n := b.nodeAt(x, docY)
	prevented := false
	if b.js != nil && n != nil {
		prevented = b.js.dispatchClick(n)
		if b.js.consumeDirty() {
			b.rebuild()
		}
	}
	if b.consumePendingJS() {
		return
	}
	// Submit-style controls act on release so click handlers on the button run
	// first, and type=button/reset controls are not mistaken for submits.
	if n != nil && isSubmitControl(n) && !prevented {
		b.submitNode(n)
		return
	}
	b.focus = -1
	b.status = ""
}

// HandleMouse processes a decoded mouse event. It returns true when the
// browser should quit.
func (b *Browser) HandleMouse(ev MouseEvent) bool {
	if ev.Shift {
		// Most terminals handle Shift+drag themselves and never send this. If
		// one does forward it, leave the event alone and write nothing so a
		// terminal-native selection is not cleared by a redraw.
		b.skipDraw = true
		return false
	}
	if b.prompt {
		if ev.Motion || ev.Wheel != 0 || !ev.Press {
			return false
		}
		x := ev.X - 1
		y := ev.Y - 1
		for _, hit := range b.headerHits {
			if hit.kind == headerURL && x >= hit.x && x < hit.x+hit.w && y >= hit.y && y < hit.y+hit.h {
				b.promptCursor = b.promptOffsetAt(x-hit.x, hit.w)
				b.promptSelectAll = false
				b.status = ""
				return false
			}
		}
		b.closeURLPrompt()
		b.status = exitHint
		return false
	}
	if b.consoleOpen {
		b.handleConsoleMouse(ev)
		return false
	}
	x := ev.X - 1
	y := ev.Y - 1
	if x < 0 || y < 0 || x >= b.width || y >= b.height {
		return false
	}
	if ev.Wheel != 0 {
		step := 3
		if ev.Wheel < 0 {
			step = -step
		}
		b.scrollBy(step)
		top, bottom := b.viewport()
		b.updateHover(x, y, top, bottom)
		return false
	}
	if y < b.headerRows() && b.height >= 2 {
		if ev.Motion {
			b.hover = -1
		}
		if !ev.Press || ev.Motion {
			return false
		}
		for _, hit := range b.headerHits {
			if x < hit.x || x >= hit.x+hit.w || y < hit.y || y >= hit.y+hit.h {
				continue
			}
			switch hit.kind {
			case headerBack:
				b.back()
			case headerForward:
				b.forward()
			case headerReload:
				b.reload()
			case headerHome:
				target := b.home
				if target == "" {
					target = "about:home"
				}
				b.Navigate(target, true)
			case headerBookmarks:
				b.Navigate("about:bookmarks", true)
			case headerHistory:
				b.Navigate("about:history", true)
			case headerHelp:
				b.Navigate("about:help", true)
			case headerConfig:
				b.Navigate("about:config", true)
			case headerURL:
				if b.inputFocus != nil {
					b.setInputFocus(nil)
				}
				b.openURLPrompt()
			}
			return false
		}
		return false
	}
	if y == b.height-1 {
		if ev.Motion {
			b.hover = -1
			return false
		}
		if ev.Press {
			for _, hit := range b.footerHits {
				if x < hit.x || x >= hit.x+hit.w || y < hit.y || y >= hit.y+hit.h {
					continue
				}
				if hit.kind == headerConsole {
					b.toggleConsole()
					return false
				}
			}
		}
		return false
	}
	top, bottom := b.viewport()
	if info := b.scrollBar; info != nil && x == b.width-1 && y >= info.trackTop && y < info.trackTop+info.trackH {
		if ev.Press && ev.Button == 0 {
			b.scrollDrag = true
			b.scrollBarTo(y)
			return false
		}
		if ev.Motion && b.scrollDrag {
			b.scrollBarTo(y)
			return false
		}
	}
	if b.scrollDrag {
		if ev.Motion {
			b.scrollBarTo(y)
			return false
		}
		b.scrollDrag = false
		return false
	}
	if ev.Motion {
		if b.selecting {
			b.updateSelection(x, y, top, bottom)
		} else {
			b.updateHover(x, y, top, bottom)
		}
		return false
	}
	if ev.Press {
		switch ev.Button {
		case MouseButtonBack:
			b.clearInputFocus()
			b.back()
			return false
		case MouseButtonForward:
			b.clearInputFocus()
			b.forward()
			return false
		case MouseButtonRight:
			b.clearInputFocus()
			b.back()
		case MouseButtonLeft:
			if y >= top && y < bottom {
				docY := b.scroll + (y - top)
				if n := b.nodeAt(x, docY); n != nil {
					if isToggleControl(n) {
						b.selecting = false
						b.toggleControl(n)
						return false
					}
					if isTextInput(n) {
						b.selecting = false
						b.setInputFocus(n)
						return false
					}
				}
				b.setInputFocus(nil)
				p := cellPos{X: x, Y: docY}
				b.selAnchor = p
				b.selCursor = p
				b.selecting = true
				b.status = ""
			}
		}
		return false
	}
	// Button release: a drag keeps the selection, a plain click acts.
	if b.selecting {
		moved := b.hasSelection()
		b.selecting = false
		if moved {
			b.status = b.selectionSummary()
		} else if y >= top && y < bottom {
			b.clickContent(x, b.scroll+(y-top))
		}
	}
	b.updateHover(x, y, top, bottom)
	return false
}

// exitHint is shown after Escape so users can find the quit and help keys.
const exitHint = "q Quit / ? Help"

// HandleKey processes one key. It returns true when the browser should quit.
func (b *Browser) HandleKey(k string) bool {
	if ev, ok := ParseMouseToken(k); ok {
		return b.HandleMouse(ev)
	}
	if b.consoleOpen {
		return b.handleConsoleKey(k)
	}
	if b.prompt {
		return b.handlePromptKey(k)
	}
	if b.findActive {
		return b.handleFindKey(k)
	}

	if b.inputFocus != nil {
		return b.handleInputKey(k)
	}
	if k == "/" {
		b.openFind(findScopePage)
		return false
	}
	if k == "esc" && b.loading {
		b.stopLoading()
		return false
	}

	switch k {
	case "q", "ctrl-c":
		return true
	case "j", "down", "ctrl-n":
		b.scrollBy(1)
	case "k", "up", "ctrl-p":
		b.scrollBy(-1)
	case " ", "space", "pgdn", "ctrl-v":
		b.scrollBy(b.viewportHeight() - 1)
	case "b", "pgup":
		b.scrollBy(-(b.viewportHeight() - 1))
	case "ctrl-d":
		b.scrollBy(b.viewportHeight() / 2)
	case "ctrl-u":
		b.scrollBy(-b.viewportHeight() / 2)
	case "g", "home":
		b.scroll = 0
	case "G", "end":
		b.scroll = b.maxScroll()
	case "tab":
		b.focusStep(1)
	case "backtab":
		b.focusStep(-1)
	case "enter":
		b.followFocused()
	case "h", "left":
		b.back()
	case "l", "right":
		b.forward()
	case "r":
		b.reload()
	case "R":
		b.toggleReader()
	case "o":
		b.openURLPrompt()
	case "u":
		if b.url != nil {
			b.status = b.url.String()
		}
	case "n":
		if b.findText != "" && b.findScope == findScopePage {
			b.findNext(1)
		}
	case "N":
		if b.findText != "" && b.findScope == findScopePage {
			b.findNext(-1)
		}
	case "y":
		b.copySelection()
	case "esc":
		if b.findText != "" {
			b.clearFind()
		}
		b.focus = -1
		b.clearSelection()
		b.status = exitHint
	case "!":
		b.toggleConsole()
	case "?":
		b.Navigate("about:help", true)
	}
	return false
}

func (b *Browser) handleInputKey(k string) bool {
	n := b.inputFocus
	switch k {
	case "esc":
		b.setInputFocus(nil)
		b.status = exitHint
		return false
	case "enter":
		b.submitInput()
		return false
	case "backspace":
		v := inputValue(n)
		if v != "" {
			_, size := utf8.DecodeLastRuneInString(v)
			b.setInputValue(n, v[:len(v)-size])
		}
		return false
	case "ctrl-u":
		b.setInputValue(n, "")
		return false
	case "tab", "backtab":
		b.setInputFocus(nil)
		return false
	case "ctrl-c":
		return true
	}
	if utf8.RuneCountInString(k) == 1 {
		r, _ := utf8.DecodeRuneInString(k)
		if r >= 32 && r != 127 && len([]rune(inputValue(n))) < 2048 {
			b.setInputValue(n, inputValue(n)+k)
		}
	}
	return false
}

func (b *Browser) pageFG() Color {
	if b.layout != nil && b.layout.Body != nil && b.layout.Body.Style != nil && b.layout.Body.Style.Color.Set {
		return b.layout.Body.Style.Color
	}
	return RGB(0x1f, 0x23, 0x28)
}

func (b *Browser) pageBG() Color {
	if b.layout != nil && b.layout.PageBG.Set {
		return b.layout.PageBG
	}
	return RGB(255, 255, 255)
}

// selectedText reconstructs the text covered by the current selection.
func (b *Browser) selectedText() string {
	if b.layout == nil || !b.hasSelection() {
		return ""
	}
	a, c := b.selectionOrder()
	var sb strings.Builder
	lastY := -1
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Kind != fragText || f.Text == "" {
			continue
		}
		if f.Y < a.Y || f.Y > c.Y {
			continue
		}
		x0, x1 := f.X, f.X+f.W-1
		if f.Y == a.Y && a.X > x0 {
			x0 = a.X
		}
		if f.Y == c.Y && c.X < x1 {
			x1 = c.X
		}
		if x1 < x0 {
			continue
		}
		if lastY >= 0 && f.Y != lastY {
			sb.WriteByte('\n')
		}
		lastY = f.Y
		cx := f.X
		for _, r := range f.Text {
			rw := runeWidth(r)
			if rw == 0 {
				continue
			}
			if cx >= x0 && cx <= x1 {
				sb.WriteRune(r)
			}
			cx += rw
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// copySelection sends the selected page text to the terminal clipboard with
// OSC 52. Terminals and multiplexers must support it for the copy to arrive.
func (b *Browser) copySelection() {
	text := b.selectedText()
	if text == "" {
		b.status = "No selection"
		return
	}
	truncated := false
	if len(text) > maxClipboardBytes {
		cut := maxClipboardBytes
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut]
		truncated = true
	}
	payload := base64.StdEncoding.EncodeToString([]byte(text))
	w := b.clipboardWriter
	if w == nil {
		w = os.Stdout
	}
	if _, err := fmt.Fprintf(w, "\x1b]52;c;%s\x07", payload); err != nil {
		b.status = "Copy failed"
		return
	}
	n := utf8.RuneCountInString(text)
	if truncated {
		b.status = fmt.Sprintf("Copied %d chars (truncated)", n)
	} else {
		b.status = fmt.Sprintf("Copied %d chars", n)
	}
	b.clearSelection()
}

func (b *Browser) selectionSummary() string {
	n := utf8.RuneCountInString(b.selectedText())
	if n == 0 {
		return "Selection"
	}
	return fmt.Sprintf("Selected %d chars (y copies)", n)
}

// paintSelection highlights selected cells in the content viewport.
func (b *Browser) paintSelection(g *Grid, top, bottom int) {
	if !b.hasSelection() {
		return
	}
	a, c := b.selectionOrder()
	selBG := RGB(0x2f, 0x6f, 0xbf)
	selFG := RGB(0xf8, 0xfa, 0xfc)
	for y := top; y < bottom && y < g.H; y++ {
		docY := b.scroll + (y - top)
		if docY < a.Y || docY > c.Y {
			continue
		}
		x0, x1 := 0, g.W-1
		if docY == a.Y {
			x0 = a.X
		}
		if docY == c.Y {
			x1 = c.X
		}
		if x0 < 0 {
			x0 = 0
		}
		if x1 >= g.W {
			x1 = g.W - 1
		}
		for x := x0; x <= x1; x++ {
			cell := &g.Cells[y*g.W+x]
			if cell.Img {
				continue
			}
			cell.BG = selBG
			if cell.R != ' ' && cell.R != 0 {
				cell.FG = selFG
			}
		}
	}
}

// drawHeaderButton paints one top-bar button and records its click region.
func (b *Browser) drawHeaderButton(g *Grid, x, y int, label string, kind headerKind, bg, fg Color, st *Style) int {
	bw := stringWidth(label)
	if x+bw > g.W || y < 0 || y >= g.H {
		return x
	}
	g.Fill(x, y, bw, 1, bg)
	g.Text(x, y, label, st, fg)
	b.headerHits = append(b.headerHits, headerHit{kind: kind, x: x, y: y, w: bw, h: 1})
	return x + bw
}

// draw renders the UI and writes changed lines to the terminal.
func (b *Browser) draw() {
	w, h := b.width, b.height
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if b.consoleOpen {
		b.drawConsole(w, h)
		return
	}
	fg, bg := b.pageFG(), b.pageBG()
	g := NewGrid(w, h, fg, bg)
	top, bottom := b.viewport()
	var placements []ImagePlacement
	var collect *[]ImagePlacement
	if b.gfx != nil {
		collect = &placements
	}
	PaintLayoutBig(g, b.layout, b.scroll, top, bottom, b.focusedLink(), fg, collect, b.bigText)
	b.paintSelection(g, top, bottom)
	b.paintFindPage(g, top, bottom)
	b.drawScrollBar(g, top, bottom)

	headerBG := RGB(0x1f, 0x29, 0x37)
	footerBG := RGB(0xea, 0xee, 0xf2)
	headerFG := RGB(0xf6, 0xf8, 0xfa)
	footerFG := RGB(0x24, 0x29, 0x2f)

	if h >= 2 {
		// Header: a two-row toolbar on normal terminals, one row when short.
		rows := b.headerRows()
		if rows < 1 {
			rows = 1
		}
		b.headerHits = b.headerHits[:0]
		b.footerHits = b.footerHits[:0]
		hs := &Style{Bold: true, Color: headerFG, FontSize: 16, LineHeight: 1}
		us := &Style{Color: headerFG, FontSize: 16, LineHeight: 1}
		btnBG := RGB(0x2b, 0x3a, 0x4c)
		urlBG := RGB(0x13, 0x1d, 0x29)
		promptBG := RGB(0x27, 0x44, 0x63)
		for ry := 0; ry < rows; ry++ {
			g.Fill(0, ry, w, 1, headerBG)
		}
		if rows >= 2 {
			g.FillHGradient(0, 0, w, 1, RGB(0x1f, 0x29, 0x37), RGB(0x31, 0x43, 0x5c))
		}

		backLabel, helpLabel := " ← Back ", " ? Help "
		forwardLabel := " → "
		reloadLabel := " ⟳ "
		homeLabel := " ⌂ "
		historyLabel := " ≡ History "
		bookmarksLabel := " ★ Bookmarks "
		configLabel := " ⚙ "
		if w < 80 {
			backLabel = " ← "
			helpLabel, historyLabel, bookmarksLabel = " ? ", " ≡ ", " ★ "
		}
		fieldBG := urlBG
		if b.prompt {
			fieldBG = promptBG
			if b.promptSelectAll {
				fieldBG = RGB(0x2f, 0x6f, 0xbf)
			}
		}

		drawField := func(x, y, width int) {
			if width < 1 {
				return
			}
			g.Fill(x, y, width, 1, fieldBG)
			if b.prompt {
				head, at, tail, show := promptParts(" Open: ", b.promptText, b.promptCursor, b.promptSelectAll, width)
				g.Text(x, y, head, hs, headerFG)
				cx := x + stringWidth(head)
				if show {
					cursorFG := RGB(0xff, 0xff, 0xff)
					cursorBG := RGB(0x2f, 0x6f, 0xbf)
					g.Put(cx, y, at, &Style{Bold: true}, cursorFG, cursorBG)
					cx++
				}
				g.Text(cx, y, tail, hs, headerFG)
			} else {
				u := ""
				if b.url != nil {
					u = b.url.String()
				}
				g.Text(x, y, fitWidth(" "+u+" ", width), us, headerFG)
			}
			b.headerHits = append(b.headerHits, headerHit{kind: headerURL, x: x, y: y, w: width, h: 1})
		}

		if rows >= 2 {
			// Toolbar row: buttons, page title, help.
			x := 0
			if w >= 24 {
				x = b.drawHeaderButton(g, x, 0, backLabel, headerBack, btnBG, headerFG, hs)
				x = b.drawHeaderButton(g, x, 0, forwardLabel, headerForward, btnBG, headerFG, hs)
				x = b.drawHeaderButton(g, x, 0, reloadLabel, headerReload, btnBG, headerFG, hs)
				x = b.drawHeaderButton(g, x, 0, homeLabel, headerHome, btnBG, headerFG, hs)
			} else if w >= 12 {
				x = b.drawHeaderButton(g, x, 0, homeLabel, headerHome, btnBG, headerFG, hs)
			}
			helpW, historyW, bookmarksW, configW := 0, 0, 0, 0
			if w >= 6 {
				helpW = stringWidth(helpLabel)
				configW = stringWidth(configLabel)
			}
			if w >= 12 {
				historyW = stringWidth(historyLabel)
			}
			if w >= 18 && b.hasBookmarks() {
				bookmarksW = stringWidth(bookmarksLabel)
			}
			for x+helpW+historyW+bookmarksW+configW > w {
				switch {
				case bookmarksW > 0:
					bookmarksW = 0
				case historyW > 0:
					historyW = 0
				case helpW > 0:
					helpW = 0
				default:
					configW = 0
				}
			}
			configX := w - configW
			helpX := configX - helpW
			histX := helpX - historyW
			bookX := histX - bookmarksW
			title := "MOB³"
			if b.page != nil && b.page.Title != "" {
				title = b.page.Title
			}
			if room := bookX - x; room > 0 {
				titleFG := RGB(0xff, 0xd7, 0x66)
				titleStyle := &Style{Bold: true, Color: titleFG, FontSize: 16, LineHeight: 1}
				tx, label := centeredLabel(x, room, title)
				g.Text(tx, 0, label, titleStyle, titleFG)
			}
			if bookmarksW > 0 {
				b.drawHeaderButton(g, bookX, 0, bookmarksLabel, headerBookmarks, btnBG, headerFG, hs)
			}
			if historyW > 0 {
				b.drawHeaderButton(g, histX, 0, historyLabel, headerHistory, btnBG, headerFG, hs)
			}
			if helpW > 0 {
				b.drawHeaderButton(g, helpX, 0, helpLabel, headerHelp, btnBG, headerFG, hs)
			}
			if configW > 0 {
				b.drawHeaderButton(g, configX, 0, configLabel, headerConfig, btnBG, headerFG, hs)
			}
			// URL row.
			drawField(0, 1, w)
		} else {
			x := 0
			if w >= 24 {
				x = b.drawHeaderButton(g, x, 0, backLabel, headerBack, btnBG, headerFG, hs)
				x = b.drawHeaderButton(g, x, 0, forwardLabel, headerForward, btnBG, headerFG, hs)
				x = b.drawHeaderButton(g, x, 0, reloadLabel, headerReload, btnBG, headerFG, hs)
				x = b.drawHeaderButton(g, x, 0, homeLabel, headerHome, btnBG, headerFG, hs)
			} else if w >= 12 {
				x = b.drawHeaderButton(g, x, 0, homeLabel, headerHome, btnBG, headerFG, hs)
			}
			helpW, historyW, bookmarksW, configW := 0, 0, 0, 0
			if w >= 6 {
				helpW = stringWidth(helpLabel)
				configW = stringWidth(configLabel)
			}
			if w >= 12 {
				historyW = stringWidth(historyLabel)
			}
			if w >= 18 && b.hasBookmarks() {
				bookmarksW = stringWidth(bookmarksLabel)
			}
			for x+helpW+historyW+bookmarksW+configW > w {
				switch {
				case bookmarksW > 0:
					bookmarksW = 0
				case historyW > 0:
					historyW = 0
				case helpW > 0:
					helpW = 0
				default:
					configW = 0
				}
			}
			urlW := w - x - helpW - historyW - bookmarksW - configW
			if urlW < 1 {
				bookmarksW = 0
				urlW = w - x - helpW - historyW - configW
			}
			if urlW < 1 {
				historyW = 0
				urlW = w - x - helpW - configW
			}
			if urlW < 1 {
				helpW = 0
				urlW = w - x - configW
			}
			if urlW < 1 {
				configW = 0
				urlW = w - x
			}
			drawField(x, 0, urlW)
			if bookmarksW > 0 {
				b.drawHeaderButton(g, w-configW-helpW-historyW-bookmarksW, 0, bookmarksLabel, headerBookmarks, btnBG, headerFG, hs)
			}
			if historyW > 0 {
				b.drawHeaderButton(g, w-configW-helpW-historyW, 0, historyLabel, headerHistory, btnBG, headerFG, hs)
			}
			if helpW > 0 {
				b.drawHeaderButton(g, w-configW-helpW, 0, helpLabel, headerHelp, btnBG, headerFG, hs)
			}
			if configW > 0 {
				b.drawHeaderButton(g, w-configW, 0, configLabel, headerConfig, btnBG, headerFG, hs)
			}
		}

		// Footer.
		fy := h - 1
		g.Fill(0, fy, w, 1, footerBG)
		if b.findActive {
			b.drawFindBar(g, fy, w)
		} else {
			fs := &Style{Color: footerFG, FontSize: 16, LineHeight: 1}
			leftText := b.footerLeft()
			icon := ">_"
			iconW := stringWidth(icon)
			showIcon := w >= 6
			reserve := 0
			if showIcon {
				reserve = iconW + 2
			}
			rightText := b.footerRight(w - reserve)
			avail := w - stringWidth(rightText) - reserve
			if avail < 8 {
				pctText := fmt.Sprintf("%3d%%", b.scrollPercent())
				rightText = truncateWidth(pctText, w-reserve)
				if b.jsErr && stringWidth(jsErrorShort+"  "+pctText) <= w-reserve {
					rightText = jsErrorShort + "  " + pctText
				}
				avail = w - stringWidth(rightText) - reserve
			}
			if avail < 0 {
				avail = 0
			}
			leftStyle := fs
			leftColor := footerFG
			if b.status != "" && b.hoveredLink() == nil {
				switch statusLevelFor(b.status) {
				case statusWarning:
					leftColor = RGB(0x9a, 0x5b, 0x00)
					leftStyle = &Style{Bold: true, Color: leftColor, FontSize: 16, LineHeight: 1}
				case statusError:
					leftColor = RGB(0xb3, 0x1d, 0x1d)
					leftStyle = &Style{Bold: true, Color: leftColor, FontSize: 16, LineHeight: 1}
				case statusSuccess:
					leftColor = RGB(0x17, 0x6b, 0x35)
					leftStyle = &Style{Color: leftColor, FontSize: 16, LineHeight: 1}
				}
			}
			g.Text(0, fy, padWidth(truncateWidth(leftText, avail), avail), leftStyle, leftColor)
			if rightText != "" {
				g.Text(w-reserve-stringWidth(rightText), fy, rightText, fs, footerFG)
			}
			if showIcon {
				iconStyle := &Style{Bold: true, Color: RGB(0x1f, 0x4f, 0x9f), FontSize: 16, LineHeight: 1}
				g.Text(w-iconW, fy, icon, iconStyle, iconStyle.Color)
				b.footerHits = append(b.footerHits, headerHit{kind: headerConsole, x: w - iconW, y: fy, w: iconW, h: 1})
			}
		}
	}

	b.flushGrid(g, placements)
}

// flushGrid writes changed cells to the terminal.
func (b *Browser) flushGrid(g *Grid, placements []ImagePlacement) {
	if b.dialog != nil {
		b.paintDialog(g)
	}
	lines := g.Lines(b.color)
	// Scrolling can leave the terminal in a shifted state (for example after a
	// stray wrap), and cells covered by scaled text are skipped when painting.
	// Clear the whole screen and repaint on any scroll change so stale content
	// can never show through the skipped cells.
	if b.scroll != b.lastScroll {
		b.lastScroll = b.scroll
		b.prev = nil
	}
	needClear := b.prev == nil || len(b.prev) != len(lines)
	if needClear {
		b.prev = make([]string, len(lines))
	}
	var sb strings.Builder
	if b.gfx != nil {
		b.gfx.BeginFrame(&sb)
	}
	if needClear {
		if b.gfx != nil {
			b.gfx.Reset(&sb)
		}
		sb.WriteString("\x1b[2J")
	}
	chrome := func(i int) bool {
		if b.height >= 2 && i == b.height-1 {
			return true
		}
		rows := b.headerRows()
		if i < 0 || i >= rows {
			return false
		}
		// Leave the URL row to normal line diffing once it has been drawn.
		// Periodic memory/JS redraws must not overwrite it, otherwise a
		// terminal-native selection over the URL would be cleared.
		if rows >= 2 && i == 1 {
			return false
		}
		return true
	}
	for i := range lines {
		if !chrome(i) && b.prev[i] == lines[i] {
			continue
		}
		sb.WriteString(fmt.Sprintf("\x1b[%d;1H", i+1))
		sb.WriteString(lines[i])
		b.prev[i] = lines[i]
	}
	if b.gfx != nil {
		b.gfx.PlaceAll(&sb, placements)
	}
	if sb.Len() > 0 {
		_, _ = os.Stdout.WriteString(sb.String())
	}
}

// tailWidth returns the trailing part of s that fits in max cells.
func tailWidth(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	w := 0
	start := len(runes)
	for start > 0 {
		rw := runeWidth(runes[start-1])
		if w+rw > max {
			break
		}
		w += rw
		start--
	}
	return string(runes[start:])
}

func (b *Browser) scrollPercent() int {
	if b.layout == nil || b.layout.Height <= 0 {
		return 0
	}
	view := b.layout.Height - b.viewportHeight()
	if view <= 0 {
		return 0
	}
	pct := b.scroll * 100 / view
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}

type statusLevel int

const (
	statusNormal statusLevel = iota
	statusWarning
	statusError
	statusSuccess
)

// statusLevelFor classifies footer status text so warnings and errors stand
// out without every caller having to tag its message.
func statusLevelFor(s string) statusLevel {
	switch {
	case strings.HasPrefix(s, "Error:"),
		strings.HasPrefix(s, "Invalid URL:"),
		s == "Invalid form action",
		s == "Copy failed":
		return statusError
	case strings.HasPrefix(s, "Copied "):
		return statusSuccess
	case s == moduleUnsupportedStatus,
		s == layoutTruncatedStatus,
		strings.HasPrefix(s, "Site blocked"),
		strings.HasPrefix(s, "HTTP "),
		strings.HasPrefix(s, "Blocked local file navigation"),
		strings.HasPrefix(s, "External protocol:"),
		s == "JavaScript links are not supported",
		s == "Invalid JavaScript URL",
		s == "JavaScript history navigation is not supported":
		return statusWarning
	}
	return statusNormal
}

func (b *Browser) footerLeft() string {
	if li := b.hoveredLink(); li != nil {
		return " → " + b.resolveHref(li.Href)
	}
	if b.status != "" {
		return " " + b.status
	}
	if li := b.focusedLink(); li != nil {
		return " → " + b.resolveHref(li.Href)
	}
	return " Ready"
}

const (
	jsErrorHint  = "JS: Error (! console)"
	jsErrorShort = "JS: Error"
)

// footerRight builds the right-aligned footer text: an optional JS error hint,
// resident memory and the scroll percentage.
func (b *Browser) footerRight(width int) string {
	pctText := fmt.Sprintf("%3d%%", b.scrollPercent())
	withMem := b.usage.footer() + "   " + pctText
	if b.jsErr {
		withMem = jsErrorHint + "   " + withMem
	}
	if stringWidth(withMem) <= width-8 {
		return withMem
	}
	noMem := pctText
	if b.jsErr {
		noMem = jsErrorHint + "   " + pctText
	}
	if stringWidth(noMem) <= width {
		return noMem
	}
	if b.jsErr && stringWidth(jsErrorShort+"  "+pctText) <= width {
		return jsErrorShort + "  " + pctText
	}
	return truncateWidth(pctText, width)
}

// centeredLabel fits label into room cells and returns the starting cell plus
// the fitted text, centered within [x, x+room).
func centeredLabel(x, room int, label string) (int, string) {
	if room < 1 {
		return x, ""
	}
	label = truncateWidth(label, room)
	pad := (room - stringWidth(label)) / 2
	return x + pad, label
}

// EscapeHTML escapes text for error pages.
func EscapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// procUsage tracks the browser's memory footprint for the footer.
type procUsage struct {
	rss uint64
}

func (u *procUsage) sample() {
	u.rss = processRSS()
}

func (b *Browser) sampleUsage() {
	b.usage.sample()
}

// footer returns the compact footer readout.
func (u procUsage) footer() string {
	return fmt.Sprintf("MEM %d MB", (u.rss+(1<<19))>>20)
}

// processRSS returns the current resident set size, falling back to Go's total
// OS memory when the platform has no /proc filesystem.
func processRSS() uint64 {
	if data, err := os.ReadFile("/proc/self/statm"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 2 {
			if pages, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				return pages * uint64(os.Getpagesize())
			}
		}
	}
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.Sys
}

// MouseEvent is a decoded terminal mouse report.
type MouseEvent struct {
	Button int // 0 left, 1 middle, 2 right, 8 back, 9 forward
	X, Y   int // 1-based terminal cell coordinates
	Press  bool
	Motion bool // drag report while a button is held
	Wheel  int  // -1 wheel up, +1 wheel down, 0 otherwise
	Shift  bool // modifier bit; reserved for terminal-native selection
}

const (
	MouseButtonLeft    = 0
	MouseButtonMiddle  = 1
	MouseButtonRight   = 2
	MouseButtonBack    = 8
	MouseButtonForward = 9
)

// decodeMouseButton strips modifier/motion/wheel bits and normalizes xterm's
// 128-131 encoding for buttons 8-11. Some terminals report 8-11 directly.
func decodeMouseButton(code int) int {
	if code&128 != 0 {
		return 8 + (code & 3)
	}
	switch code {
	case 8, 9, 10, 11:
		return code
	}
	return code & 3
}

const (
	mouseWheelUp   = 64
	mouseWheelDown = 65
)

// parseSGRMouse decodes the parameters of an SGR mouse report. params is the
// text between CSI and the final 'M'/'m' (for example "<0;12;5"); release is
// true for the lowercase final byte.
func parseSGRMouse(params string, release bool) (MouseEvent, bool) {
	params = strings.TrimPrefix(params, "<")
	parts := strings.Split(params, ";")
	if len(parts) != 3 {
		return MouseEvent{}, false
	}
	b, err1 := strconv.Atoi(parts[0])
	x, err2 := strconv.Atoi(parts[1])
	y, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return MouseEvent{}, false
	}
	ev := MouseEvent{X: x, Y: y, Press: !release, Motion: b&32 != 0, Shift: b&4 != 0}
	switch {
	case b&64 != 0:
		ev.Button = b & 3
		if ev.Button == 1 {
			ev.Wheel = 1
		} else {
			ev.Wheel = -1
		}
	default:
		ev.Button = decodeMouseButton(b)
	}
	return ev, true
}

// parseX10Mouse decodes a legacy 3-byte X10 mouse report.
func parseX10Mouse(b, x, y byte) (MouseEvent, bool) {
	btn := int(b) - 32
	x -= 32
	y -= 32
	if x < 1 || y < 1 || btn < 0 {
		return MouseEvent{}, false
	}
	ev := MouseEvent{X: int(x), Y: int(y), Motion: btn&32 != 0, Shift: btn&4 != 0}
	if btn&64 != 0 {
		ev.Press = true
		ev.Button = btn & 3
		if ev.Button == 1 {
			ev.Wheel = 1
		} else {
			ev.Wheel = -1
		}
		return ev, true
	}
	button := decodeMouseButton(btn)
	if button == 3 {
		ev.Button = 0
		ev.Press = false
		return ev, true
	}
	ev.Button = button
	ev.Press = true
	return ev, true
}

// mouseToken encodes a mouse event as an input token.
func mouseToken(ev MouseEvent) string {
	mod := ""
	if ev.Shift {
		mod = ":shift"
	}
	if ev.Wheel != 0 {
		dir := "up"
		if ev.Wheel > 0 {
			dir = "down"
		}
		return "mouse:wheel:" + dir + ":" + strconv.Itoa(ev.X) + ":" + strconv.Itoa(ev.Y) + mod
	}
	kind := "release"
	if ev.Press {
		kind = "press"
	}
	if ev.Motion {
		kind = "drag"
	}
	return "mouse:" + kind + ":" + strconv.Itoa(ev.Button) + ":" + strconv.Itoa(ev.X) + ":" + strconv.Itoa(ev.Y) + mod
}

// ParseMouseToken decodes a token produced by mouseToken.
func ParseMouseToken(tok string) (MouseEvent, bool) {
	if !strings.HasPrefix(tok, "mouse:") {
		return MouseEvent{}, false
	}
	parts := strings.Split(strings.TrimPrefix(tok, "mouse:"), ":")
	shift := false
	if len(parts) > 1 && parts[len(parts)-1] == "shift" {
		shift = true
		parts = parts[:len(parts)-1]
	}
	switch {
	case len(parts) == 4 && parts[0] == "wheel":
		x, err1 := strconv.Atoi(parts[2])
		y, err2 := strconv.Atoi(parts[3])
		if err1 != nil || err2 != nil {
			return MouseEvent{}, false
		}
		ev := MouseEvent{X: x, Y: y, Shift: shift}
		if parts[1] == "up" {
			ev.Wheel = -1
		} else {
			ev.Wheel = 1
		}
		return ev, true
	case len(parts) == 4 && (parts[0] == "press" || parts[0] == "release" || parts[0] == "drag"):
		btn, err1 := strconv.Atoi(parts[1])
		x, err2 := strconv.Atoi(parts[2])
		y, err3 := strconv.Atoi(parts[3])
		if err1 != nil || err2 != nil || err3 != nil {
			return MouseEvent{}, false
		}
		return MouseEvent{
			Button: btn, X: x, Y: y, Press: parts[0] != "release",
			Motion: parts[0] == "drag", Shift: shift,
		}, true
	}
	return MouseEvent{}, false
}
