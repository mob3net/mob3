package engine

import (
	"errors"
	"fmt"
	"strings"
)

// Snapshot text is bounded by painted rows, cells and bytes so one tool call
// cannot allocate an unbounded grid or exceed the transport's text clip. The
// character budget keeps the Rows/Next header and the whole page window inside
// one response, so the next offset is accurate.
const (
	automationSnapshotRows  = 2000
	automationSnapshotCells = 100000
	automationSnapshotChars = 19000
)

// Snapshot returns the first readable window of the whole page, followed by the
// visible links and form controls. The text is not limited to the interaction
// viewport, so an agent does not have to scroll to read a page. Use the
// snapshot command's offset/rows arguments for later windows.
func (a *Automation) Snapshot() string {
	s, _ := a.snapshotAt(0, 0)
	return s
}

// snapshotAt returns one window of the laid-out page. offset is a zero-based
// layout row; rows <= 0 means as many rows as fit the response budget.
func (a *Automation) snapshotAt(offset, rows int) (string, error) {
	if a == nil || a.b == nil {
		return "No page loaded", nil
	}
	if offset < 0 {
		return "", errors.New("offset must be zero or positive")
	}
	if rows < 0 {
		return "", errors.New("rows must be zero or positive")
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
	if offset > 0 && offset >= pageH {
		return "", fmt.Errorf("offset %d is past the end of the page (%d rows)", offset, pageH)
	}
	text, end := a.snapshotWindowLocked(offset, rows)
	if pageH > 0 {
		fmt.Fprintf(&sb, "Page: %dx%d  Rows: %d-%d of %d", b.Width, pageH, offset, end, pageH)
		if end < pageH {
			fmt.Fprintf(&sb, "  Next: %d", end)
		}
		fmt.Fprintf(&sb, "  ")
	}
	fmt.Fprintf(&sb, "Viewport: %dx%d  Scroll: %d%%\n\n", b.Width, vh, b.scrollPercent())
	sb.WriteString(text)
	if offset == 0 {
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
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// snapshotWindowLocked paints one laid-out window and trims it to the response
// text budget. It returns the text and the exclusive end row.
func (a *Automation) snapshotWindowLocked(start, maxRows int) (string, int) {
	b := a.b
	if b.Layout == nil || b.Page == nil || start >= b.Layout.Height {
		return "", start
	}
	pageH := b.Layout.Height
	n := pageH - start
	if n > automationSnapshotRows {
		n = automationSnapshotRows
	}
	if b.Width > 0 {
		if cellRows := automationSnapshotCells / b.Width; n > cellRows {
			n = cellRows
		}
	}
	if maxRows > 0 && n > maxRows {
		n = maxRows
	}
	if n < 1 {
		n = 1
	}
	b.Layout.SetFixedScroll(0)
	g := NewGrid(b.Width, n, b.pageFG(), b.pageBG())
	PaintLayout(g, b.Layout, start, 0, n, nil, b.pageFG())
	lines := GridLines(g)
	keep := 0
	total := 0
	for i, line := range lines {
		add := len(line)
		if i > 0 {
			add++
		}
		if keep > 0 && total+add > automationSnapshotChars {
			break
		}
		total += add
		keep = i + 1
	}
	if keep == 0 {
		return "", start
	}
	return strings.Join(lines[:keep], "\n"), start + keep
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
