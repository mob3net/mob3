package engine

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"browser/internal/protocol"
)

// Session holds one page's content and view state. The terminal shell and
// trusted navigation state stay in the front end; this type is the engine-side
// half of the session split.
type Session struct {
	Page   *Page
	Layout *Layout
	URL    *url.URL
	Scroll int

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
	ScaledLayout    bool

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
	HintConfirm     bool
	HintConfirmLink int
	HintsFrontend   bool

	// JavaScript runtime and page tracking.
	JS        *JSRuntime
	PDFStarts []int

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
	NextScroll          int
	NextScrollSet       bool
	PendingDownloadName string

	// Deferred image loading.
	DeferredImages  bool
	ImagesPaused    bool
	ImageTotal      int
	ImageDone       int
	PendingImageRow int
}

// NewSession creates an empty page session.
func NewSession() *Session { return &Session{} }

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
