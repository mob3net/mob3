package engine

import (
	"errors"
	"fmt"
	"strings"
)

// Snapshot returns a readable text view of the current page and viewport,
// followed by visible links and form controls with clickable coordinates.
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
	fmt.Fprintf(&sb, "Viewport: %dx%d  Scroll: %d%%\n\n", b.Width, vh, b.scrollPercent())
	sb.WriteString(a.pageTextLocked(vh))
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

// pageText renders the current page viewport without browser chrome.
func (a *Automation) pageTextLocked(vh int) string {
	g := a.viewportGridLocked(vh)
	if g == nil {
		return ""
	}
	lines := GridLines(g)
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
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
