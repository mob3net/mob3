package engine

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"browser/internal/protocol"
)

// Session is one tab: the top-level frame, shell glue and view state. The
// front end owns the trusted navigation list and terminal presentation; this
// type keeps the memory-only page cache. The embedded Frame owns the page,
// layout, URL and scroll position so the tab and frame model can grow without
// moving the tab-level state.
type Session struct {
	*Frame

	// Terminal/content geometry and rendering configuration.
	Width, Height   int
	ContentTop      int
	ContentHeight   int
	CellW, CellH    int
	Zoom            float64
	NoJS            bool
	NoPDF           bool
	NoBigText       bool
	JSMemLimit      int64
	JSPageBudget    time.Duration
	LayoutBudget    LayoutBudget
	HideFixed       bool
	NoSticky        bool
	HideBackgrounds bool
	Interactive     bool
	NoDraw          bool
	// active marks the tab the front end is presenting. Direct callers and
	// tests start active; the tab manager sends Visibility when it switches.
	active       bool
	zoomGen      uint64
	ScaledLayout bool
	LastBuild    time.Duration
	LastRebuild  time.Time
	LastState    protocol.State

	// View and page-display state.
	Focus             int
	Hover             int
	HoverNode         *Node
	PageHasHover      bool
	Reader            bool
	ReaderRoot        *Node
	ReaderSavedScroll int
	BigText           bool
	BigTextBlocked    bool
	Dark              bool

	// Interaction state.
	Forms     FormState
	Selection Selection
	Selecting bool

	// Find state.
	FindActive  bool
	FindText    string
	FindCursor  int
	FindScope   int
	FindMatches []FindMatch
	FindIdx     int

	// Link-hint state.
	LinkHints       bool
	HintItems       []LinkHint
	HintTargets     []HintTarget
	HintPrefix      string
	HintAsk         bool
	HintMode        protocol.HintMode
	HintConfirm     bool
	HintConfirmLink int
	HintsFrontend   bool

	// Page tracking; the frame owns the JavaScript runtime.
	PDFStarts []int

	// Memory-only back/forward page cache.
	Cache *PageCache
	// scavengePending asks the event loop for one debug.FreeOSMemory call.
	scavengePending atomic.Bool

	// Load lifecycle.
	Loading             bool
	Progress            float64
	ProgressHold        int
	AutoNavigations     int
	LoopStopped         bool
	LastMethod          string
	LastBody            []byte
	LastContentType     string
	LoadSeq             int
	LoadCh              chan LoadResult
	CancelLoad          context.CancelFunc
	LoadFn              LoadFunc
	ImportCh            chan ImportTask
	EntryID             uint64
	NextScroll          int
	NextScrollSet       bool
	PendingDownloadName string

	// Deferred image loading.
	DeferredImages  bool
	ImagesPaused    bool
	ImageTotal      int
	ImageDone       int
	PendingImageRow int
	Images          ImageLoader
	ImageCh         chan ImageBatch
	ImageCancel     context.CancelFunc

	// Native-image placement cache. Encoded blobs stay stable across frames so
	// a repaint does not re-encode every picture.
	placementCache  map[placementKey]*placementEntry
	placementBytes  int
	nextPlacementID int

	loader      *Loader
	browser     *BrowserContext
	tabID       uint64
	router      *FrameRouter
	budgets     *BudgetManager
	budgetShare budgetShare

	// contentTop/contentHeight are the front-end-owned page rectangle. A
	// negative top means direct callers/tests use the legacy derivation.
	// pdfStarts holds the layout row where each PDF page begins, so the footer
	// can name the page under the viewport.
	pdfStarts []int
	// hoverRebuildPending coalesces the style pass for pointer hover changes so
	// moving across a large page cannot rebuild the whole document per cell.
	hoverRebuildPending bool
	hoverRebuildAt      time.Time
	jsErr               bool
	// moduleErr records that an ES module script failed to load, link or
	// evaluate, so the footer can point at the console.
	moduleErr bool
	status    string

	// Reader mode renders an extracted article document instead of the page.

	// Dark mode remaps the rendered page palette.

	// History cache. The front end owns the ordered entry list and scroll
	// offsets; the engine only keeps a bounded memory-only page cache keyed by
	// the front-end entry ID.
	historyView     []string
	historyViewCur  int
	historyObserver func(protocol.HistoryCommit)
	// memLimitBytes is the process soft heap limit in bytes; zero leaves the
	// runtime default in place.
	memLimitBytes int64
	// nextScroll is consumed by the next navigation started from history.
	// autoNavigations counts consecutive scripted/meta redirects so a
	// self-redirect loop can be stopped. loopStopped reports that the cap was
	// hit while applying the current page.
	home      string
	cliConfig map[string]string
	// configNotice shows why an edited about:config value was rejected, such
	// as an unreadable -certs bundle.
	configNotice string
	// configToken is the one-shot token embedded in the current about:config
	// form. A config submission applies only when it presents this token, so
	// page content cannot rewrite session settings by navigating to
	// about:config with a query.
	configToken protocol.ConfigToken

	color      bool
	caps       protocol.Capabilities
	pngEncoder func(protocol.Frame, int) ([]byte, error)
	// contentGrid is reused between frames and is exactly the page viewport.
	contentGrid *Grid
	frameBuf    frameScratch
	// zoom is the layout zoom factor: 1 is normal. It changes the CSS pixels
	// per terminal cell for everything except text metrics.
	// scaledLayout records whether the current layout was built with kitty
	// scaled text, so draw can rebuild when an overlay toggles it.
	// bigTextBlocked disables kitty scaled text for a page whose scaled runs
	// overlap neighbouring columns.

	// Graphics modes available to the i key, in cycle order, and the current
	// one. The list starts with native inline graphics when available.
	graphicsModes []GraphicsMode
	graphicsMode  GraphicsMode

	// hideFixed suppresses position:fixed boxes for the current page.
	// noSticky lays position:sticky boxes out as normal flow, so they scroll
	// with the page instead of following a top offset.
	// hideBackgrounds paints CSS background images as a neutral placeholder
	// instead of their pixels; the images stay loaded and laid out.

	// frontendOverlay is true while a front-end-owned window (the view or zoom
	// menu) covers the page. Native images and scaled text are disabled so
	// neither can composite above the window.
	frontendOverlay bool

	// noPDF disables the built-in PDF text viewer, restoring the download
	// prompt for PDF responses.
	// pendingStatus replaces the computed load status once, so a JavaScript
	// toggle can report its new state instead of clearing the footer.
	pendingStatus string
	// jsPageBudget caps one page's load-time script phase. It defaults to
	// jsPageBudget and exists as a field so tests can shorten it.

	// interactive marks the terminal UI, where a background dynamic import can
	// wait for the event loop instead of blocking it.
	// imageTotal and imageDone drive the loading bar and footer count while a
	// deferred image batch is in flight.
	// pendingImageRow is the topmost laid-out row of images whose decoded data
	// arrived while they sat below the viewport. Their layout is deferred until
	// the viewport approaches that row or the end of the document, so a batch
	// far down the page cannot reflow and repaint the visible frame. Zero means
	// nothing is pending.

	// pendingDownloadName carries a download attribute from the clicked link
	// into the next load, so a non-renderable response keeps that filename.

	consoleOpen   bool
	consoleScroll int
	reqScroll     int
	storageScroll int
	htmlScroll    int
	toolScroll    int
	toolLog       []string
	consolePane   int // 0 = JS logs, 1 = requests, 2 = storage, 3 = HTML, 4 = tools
	consoleText   string
	consoleCursor int
	consoleEdit   bool // the input row inside the logs or tools section has focus
	consoleHist   consoleHistory
	toolHist      consoleHistory

	htmlPage  *Page
	htmlWidth int
	htmlCache []string
	jsLog     []string

	dialog *jsDialog
	// sessionCtx, sessionIn and sessionAbort are set while Serve owns the
	// event loop. A modal dialog then reads keys from the command stream
	// instead of a direct terminal channel.
	sessionCtx   context.Context
	sessionIn    <-chan protocol.Command
	sessionAbort <-chan struct{}
	sessionQuit  bool
	// emitFrames switches presentation to the front end: draw stashes the
	// screen frame and sends it over the transport instead of writing stdout.
	// Native image modes keep the engine-side writer until placements cross the
	// seam.
	emitFrames bool
	frameReady bool
	sessionOut protocol.EventSink
	// frontendChrome lets the normal path emit a content-only frame and a
	// State snapshot; the front end draws the header/footer from it.
	frontendChrome   bool
	nextDialogID     uint32
	nextSelectID     uint32
	selectFrontend   bool
	selectID         uint32
	downloadFrontend bool
	noticeFrontend   bool
	hintsFrontend    bool
	// abortGen counts user escape requests (Esc or Ctrl-C). A guarded script
	// compares it with the value captured at entry, so a press interrupts the
	// call in flight without leaking into later timer callbacks.
	abortGen atomic.Uint64
	// download holds the modal curl hint shown for download links and
	// non-renderable responses.
	download *downloadDialog
	// notice holds a small informational modal, such as the unsupported
	// file-upload message.
	notice *noticeDialog
	// selectMenu is the pending single-select choice awaiting the front end.
	selectMenu  *selectMenuDialog
	usage       procUsage
	quitPending bool
}

// NewSession creates an empty page session.
func NewEmptySession() *Session {
	bc := NewBrowserContext()
	s := &Session{
		Frame: NewFrame(), ImageCh: make(chan ImageBatch, 1),
		LoadCh: make(chan LoadResult, 1), ImportCh: make(chan ImportTask, 16),
		browser: bc, tabID: bc.NextTabID(), active: true, zoomGen: zoomGen(),
	}
	s.router = newFrameRouter()
	s.router.Register(s.Frame)
	s.budgets = bc.Budgets()
	if s.budgets != nil {
		s.budgets.Register(s)
	}
	s.usage.sample()
	return s
}

// PageKind classifies the current page for the shell.
func (s *Session) PageKind() protocol.PageKind {
	if s.Page == nil {
		return protocol.PageHTML
	}
	switch {
	case s.Page.PDF != nil:
		return protocol.PagePDF
	case s.Page.Download:
		return protocol.PageDownload
	case s.URL != nil && s.URL.Scheme == "about":
		return protocol.PageAbout
	}
	return protocol.PageHTML
}

// PageFG returns the page's foreground color.
func (s *Session) PageFG() Color {
	if s.Layout != nil && s.Layout.Body != nil && s.Layout.Body.Style != nil && s.Layout.Body.Style.Color.Set {
		return s.Layout.Body.Style.Color
	}
	return RGB(0x1f, 0x23, 0x28)
}

// PageBG returns the page's background color.
func (s *Session) PageBG() Color {
	if s.Layout != nil && s.Layout.PageBG.Set {
		return s.Layout.PageBG
	}
	return RGB(255, 255, 255)
}

// FocusedLink returns the currently focused link.
func (s *Session) FocusedLink() *LinkInfo {
	if s.Layout == nil || s.Focus < 0 || s.Focus >= len(s.Layout.Links) {
		return nil
	}
	return s.Layout.Links[s.Focus]
}

// HoveredLink returns the currently hovered link.
func (s *Session) HoveredLink() *LinkInfo {
	if s.Layout == nil || s.Hover < 0 || s.Hover >= len(s.Layout.Links) {
		return nil
	}
	return s.Layout.Links[s.Hover]
}

// SameDocument reports whether two URLs differ only by fragment.
func SameDocument(a, b *url.URL) bool {
	return a.Scheme == b.Scheme && a.Host == b.Host && a.Path == b.Path && a.RawQuery == b.RawQuery
}

// MetaRefreshTarget returns the target of the first meta refresh, if any.
func MetaRefreshTarget(page *Page) string {
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

// Find scopes.
const (
	FindScopePage = iota
	FindScopeHTML
)

// OpenFind starts a fresh find in the given scope.
func (s *Session) OpenFind(scope int) {
	s.FindActive = true
	s.FindScope = scope
	s.FindText = ""
	s.FindCursor = 0
	s.FindMatches = nil
	s.FindIdx = -1
}

// ClearFind removes the query, matches and highlights.
func (s *Session) ClearFind() {
	s.FindActive = false
	s.FindText = ""
	s.FindCursor = 0
	s.FindMatches = nil
	s.FindIdx = -1
}

// CurrentFindLink returns the link containing the current page find match.
func (s *Session) CurrentFindLink() *LinkInfo {
	if s.FindScope != FindScopePage || s.Layout == nil || len(s.FindMatches) == 0 {
		return nil
	}
	idx := s.FindIdx
	if idx < 0 || idx >= len(s.FindMatches) {
		idx = 0
	}
	m := s.FindMatches[idx]
	if m.Frag < 0 || m.Frag >= len(s.Layout.Frags) {
		return nil
	}
	return s.Layout.Frags[m.Frag].Link
}

// FindBarCount formats the current/total find count.
func (s *Session) FindBarCount() string {
	if s.FindText == "" || len(s.FindMatches) == 0 {
		return " 0/0 "
	}
	idx := s.FindIdx
	if idx < 0 || idx >= len(s.FindMatches) {
		idx = 0
	}
	return fmt.Sprintf(" %d/%d ", idx+1, len(s.FindMatches))
}

// ClearHints resets hint mode and reports whether it was active.
func (s *Session) ClearHints() bool {
	if !s.LinkHints {
		return false
	}
	s.LinkHints = false
	s.HintsFrontend = false
	s.HintItems = nil
	s.HintTargets = nil
	s.HintPrefix = ""
	s.HintAsk = false
	return true
}

// HintTargetAt resolves a hint target index, falling back to a link index.
func (s *Session) HintTargetAt(i int) HintTarget {
	if i >= 0 && i < len(s.HintTargets) {
		return s.HintTargets[i]
	}
	if s.Layout != nil && i >= 0 && i < len(s.Layout.Links) {
		return HintTarget{Link: i}
	}
	return HintTarget{Link: -1}
}

// HasSelection reports whether a page selection exists.
func (s *Session) HasSelection() bool { return s.Selection.Has() }

// ClearSelection resets the page selection and drag state.
func (s *Session) ClearSelection() {
	s.Selection.Clear()
	s.Selecting = false
}

// UpdatePDFPageStarts records the layout row where each PDF page begins.
func (s *Session) UpdatePDFPageStarts() {
	s.PDFStarts = s.PDFStarts[:0]
	if s.Page == nil || s.Page.PDF == nil || s.Layout == nil {
		return
	}
	nodes := s.Page.PDF.PageNodes
	if len(nodes) == 0 {
		return
	}
	index := make(map[*Node]int, len(nodes))
	for i, n := range nodes {
		index[n] = i
	}
	starts := make([]int, len(nodes))
	for i := range starts {
		starts[i] = -1
	}
	for _, f := range s.Layout.Frags {
		for n := f.Node; n != nil; n = n.Parent {
			i, ok := index[n]
			if !ok {
				continue
			}
			if starts[i] < 0 || f.Y < starts[i] {
				starts[i] = f.Y
			}
			break
		}
	}
	last := 0
	for i := range starts {
		if starts[i] < 0 {
			starts[i] = last
			continue
		}
		last = starts[i]
	}
	s.PDFStarts = starts
}

// PDFPageNumber returns the page shown at the top of the viewport.
func (s *Session) PDFPageNumber() int {
	if s.Page == nil || s.Page.PDF == nil {
		return 0
	}
	n := len(s.Page.PDF.Pages)
	if n == 0 {
		return 0
	}
	cur := 1
	for i, y := range s.PDFStarts {
		if i >= n {
			break
		}
		if y <= s.Scroll {
			cur = i + 1
		}
	}
	if cur > n {
		cur = n
	}
	if cur < 1 {
		cur = 1
	}
	return cur
}

// MaxScroll is the largest scroll offset for the given viewport height.
func (s *Session) MaxScroll(viewportH int) int {
	if s.Layout == nil {
		return 0
	}
	m := s.Layout.Height - viewportH
	if m < 0 {
		return 0
	}
	return m
}

// ClampScroll keeps the scroll offset inside the page.
func (s *Session) ClampScroll(viewportH int) {
	max := s.MaxScroll(viewportH)
	if s.Scroll < 0 {
		s.Scroll = 0
	}
	if s.Scroll > max {
		s.Scroll = max
	}
}

// ScrollPercent is the rounded scroll position as a percentage.
func (s *Session) ScrollPercent(viewportH int) int {
	if s.Layout == nil || s.Layout.Height <= 0 {
		return 0
	}
	view := s.Layout.Height - viewportH
	if view <= 0 {
		return 0
	}
	pct := s.Scroll * 100 / view
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}

// ScrollToAnchor moves to the first fragment with the given id or name.
func (s *Session) ScrollToAnchor(frag string, viewportH int) bool {
	if s.Layout == nil || frag == "" {
		return false
	}
	if unescaped, err := url.PathUnescape(frag); err == nil {
		frag = unescaped
	}
	best := -1
	for i := range s.Layout.Frags {
		f := &s.Layout.Frags[i]
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
		s.Scroll = best - 1
		s.ClampScroll(viewportH)
		return true
	}
	return false
}
