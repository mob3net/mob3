package engine

import (
	"net/url"
	"strings"

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
