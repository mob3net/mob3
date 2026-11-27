package engine

import (
	"errors"
	"fmt"
	"strings"
)

// Snapshot text is bounded by painted rows and cells so one tool call cannot
// allocate an unbounded grid; the MCP transport also clips long text.
const (
	automationSnapshotRows  = 2000
	automationSnapshotCells = 100000
)

// Snapshot returns a readable text view of the whole page, followed by the
// visible links and form controls with clickable coordinates. The text is not
// limited to the interaction viewport, so an agent does not have to scroll to
// read a page.
func (a *Automation) Snapshot() string {
	if a == nil || a.b == nil {
		return "No page loaded"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	var sb strings.Builder
	u := ""
	if b.URL != nil {
		u = b.URL.String()
	}
	title := ""
	if b.Page != nil {
		title = b.Page.Title
	}
	sb.WriteString("URL: " + u + "\n")
	if title != "" {
		sb.WriteString("Title: " + title + "\n")
	}
	if b.status != "" {
		sb.WriteString("Status: " + b.status + "\n")
	}
	top, bottom := b.viewport()
	vh := bottom - top
	if vh < 1 {
		vh = 1
	}
	pageH := 0
	if b.Layout != nil {
		pageH = b.Layout.Height
	}
	shown := pageH
	if shown > automationSnapshotRows {
		shown = automationSnapshotRows
	}
	if b.Width > 0 {
		if maxRows := automationSnapshotCells / b.Width; shown > maxRows {
			shown = maxRows
		}
	}
	if shown < 1 {
		shown = 1
	}
	if pageH > 0 {
		fmt.Fprintf(&sb, "Page: %dx%d", b.Width, pageH)
		if shown < pageH {
			fmt.Fprintf(&sb, " (snapshot truncated to %d rows)", shown)
		}
		fmt.Fprintf(&sb, "  ")
	}
	fmt.Fprintf(&sb, "Viewport: %dx%d  Scroll: %d%%\n\n", b.Width, vh, b.scrollPercent())
	sb.WriteString(a.fullPageTextLocked(shown))
	if lines := a.linkLinesLocked(vh); len(lines) > 0 {
		sb.WriteString("\nLinks:\n")
		sb.WriteString(strings.Join(lines, "\n"))
		sb.WriteString("\n")
	}
	if lines := a.inputLinesLocked(vh); len(lines) > 0 {
		sb.WriteString("\nInputs:\n")
		sb.WriteString(strings.Join(lines, "\n"))
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// fullPageTextLocked paints the laid-out page from the top, independent of the
// interaction scroll position.
func (a *Automation) fullPageTextLocked(height int) string {
	b := a.b
	if b.Layout == nil || b.Page == nil || height < 1 {
		return ""
	}
	b.Layout.SetFixedScroll(0)
	g := NewGrid(b.Width, height, b.pageFG(), b.pageBG())
	PaintLayout(g, b.Layout, 0, 0, height, nil, b.pageFG())
	lines := GridLines(g)
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// viewportGrid renders the current page viewport without browser chrome.
func (a *Automation) viewportGridLocked(vh int) *Grid {
	b := a.b
	if b.Layout == nil || b.Page == nil {
		return nil
	}
	b.Layout.SetFixedScroll(b.Scroll)
	g := NewGrid(b.Width, vh, b.pageFG(), b.pageBG())
	PaintLayout(g, b.Layout, b.Scroll, 0, vh, nil, b.pageFG())
	return g
}

// Screenshot renders the current page viewport as a PNG. Scale 1 uses 8x16
// pixel cells; scale 2 doubles the image.
func (a *Automation) Screenshot(scale int) ([]byte, error) {
	if a == nil || a.b == nil {
		return nil, errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	top, bottom := a.b.viewport()
	vh := bottom - top
	if vh < 1 {
		vh = 1
	}
	g := a.viewportGridLocked(vh)
	if g == nil {
		return nil, errors.New("no page loaded")
	}
	return a.b.screenshotPNG(g, scale)
}

func (a *Automation) linkLinesLocked(vh int) []string {
	b := a.b
	if b.Layout == nil {
		return nil
	}
	var out []string
	for i := range b.Layout.Links {
		li := b.Layout.Links[i]
		if !rectVisible(li.Rects, b.Scroll, vh) {
			continue
		}
		text := ""
		if li.Node != nil {
			text = collapseWhitespace(li.Node.TextContent())
			if text == "" {
				text = li.Node.Attr("aria-label")
			}
		}
		if text == "" {
			text = "(no text)"
		}
		out = append(out, fmt.Sprintf("[%d] %s -> %s", i+1, clipText(text, 80), b.resolveHref(li.Href)))
		if len(out) >= 80 {
			break
		}
	}
	return out
}

func (a *Automation) inputLinesLocked(vh int) []string {
	b := a.b
	if b.Page == nil || b.Page.Root == nil || b.Layout == nil {
		return nil
	}
	var out []string
	for _, n := range b.Page.Root.FindAll(func(n *Node) bool {
		if n.Type != ElementNode {
			return false
		}
		switch n.Tag {
		case "input", "textarea", "select", "button":
			return true
		}
		return false
	}) {
		typ := strings.ToLower(n.Attr("type"))
		if typ == "hidden" || IsDisabledControl(n) {
			continue
		}
		f := firstVisibleFrag(b, n, b.Scroll, vh)
		if f == nil {
			continue
		}
		name := n.Attr("name")
		if name == "" {
			name = n.Attr("id")
		}
		value := InputValue(n)
		switch typ {
		case "checkbox", "radio":
			if n.HasAttr("checked") {
				value = "checked"
			} else {
				value = "unchecked"
			}
		}
		line := fmt.Sprintf("- %s name=%q type=%q value=%q at (%d,%d)",
			n.Tag, name, typ, clipText(collapseWhitespace(value), 40),
			f.X, f.Y-b.Scroll)
		out = append(out, line)
		if len(out) >= 40 {
			break
		}
	}
	return out
}

func firstVisibleFrag(b *Session, n *Node, scroll, vh int) *Frag {
	for i := range b.Layout.Frags {
		f := &b.Layout.Frags[i]
		if f.Kind != FragText || f.Node == nil {
			continue
		}
		if f.Node != n && !isDescendant(f.Node, n) {
			continue
		}
		if f.Y >= scroll && f.Y < scroll+vh {
			return f
		}
	}
	return nil
}

func isDescendant(n, ancestor *Node) bool {
	for n = n.Parent; n != nil; n = n.Parent {
		if n == ancestor {
			return true
		}
	}
	return false
}

func rectVisible(rects []Rect, scroll, vh int) bool {
	for _, r := range rects {
		if r.W > 0 && r.H > 0 && r.Y+r.H > scroll && r.Y < scroll+vh {
			return true
		}
	}
	return false
}

func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func clipText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func nonEmptyStatus(s string) string {
	if s == "" {
		return "unknown error"
	}
	return s
}
