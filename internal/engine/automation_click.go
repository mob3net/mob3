package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Click activates a link, a selector, or a viewport cell. It waits for any
// navigation the click causes.
func (a *Automation) Click(ctx context.Context, target ClickTarget) error {
	if a == nil || a.b == nil {
		return errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.Page == nil || b.Layout == nil {
		return errors.New("no page loaded")
	}
	switch {
	case target.Link > 0:
		if target.Link > len(b.Layout.Links) {
			return fmt.Errorf("link %d out of range (%d links)", target.Link, len(b.Layout.Links))
		}
		b.Focus = target.Link - 1
		b.followFocused()
		return a.waitLoad(ctx)
	case target.Text != "":
		n, err := findNodeByText(b, target.Text)
		if err != nil {
			return err
		}
		if err := a.clickNodeLocked(n); err != nil {
			return err
		}
		return a.waitLoad(ctx)
	case target.Selector != "":
		n, err := findSelectorNode(b.Page.Root, target.Selector)
		if err != nil {
			return err
		}
		if err := a.clickNodeLocked(n); err != nil {
			return err
		}
		return a.waitLoad(ctx)
	case target.HasCoords:
		top, _ := b.viewport()
		if target.X < 0 || target.X >= b.Width || target.Y < 0 || target.Y >= b.viewportHeight() {
			return fmt.Errorf("coordinates (%d,%d) outside the page viewport", target.X, target.Y)
		}
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: target.X + 1, Y: top + target.Y + 1, Press: true})
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: target.X + 1, Y: top + target.Y + 1, Press: false})
		return a.waitLoad(ctx)
	default:
		return errors.New("click needs a link number, text, selector or coordinates")
	}
}

// clickNodeLocked clicks the first rendered cell belonging to n. Links use the
// link path so JavaScript click handlers and download attributes behave.
func (a *Automation) clickNodeLocked(n *Node) error {
	b := a.b
	anchor := n
	for anchor != nil && anchor.Tag != "a" {
		anchor = anchor.Parent
	}
	for i := range b.Layout.Links {
		li := b.Layout.Links[i]
		if li.Node == anchor {
			b.Focus = i
			b.followFocused()
			return nil
		}
	}
	f := firstVisibleFrag(b, n, b.Scroll, b.viewportHeight())
	if f == nil {
		// Selector clicks scroll the element into view, like a browser.
		revealNode(b, n)
		f = firstVisibleFrag(b, n, b.Scroll, b.viewportHeight())
	}
	if f == nil {
		return fmt.Errorf("element %s is not visible", describeNode(n))
	}
	top, _ := b.viewport()
	x, y := f.X+1, top+f.Y-b.Scroll+1
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: true})
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: false})
	return nil
}

// findNodeByText returns the most specific rendered element whose visible text
// matches query, preferring exact matches and interactive elements.
func findNodeByText(b *Session, query string) (*Node, error) {
	needle := strings.ToLower(collapseWhitespace(query))
	if needle == "" {
		return nil, errors.New("click text is empty")
	}
	if b.Page == nil || b.Page.Root == nil || b.Layout == nil {
		return nil, errors.New("no page loaded")
	}
	var best *Node
	bestExact := false
	bestPrio, bestLen := 0, 0
	b.Page.Root.FindAll(func(n *Node) bool {
		if n.Type != ElementNode {
			return false
		}
		switch n.Tag {
		case "html", "body", "head", "script", "style", "noscript", "template":
			return false
		}
		text := nodeMatchText(n)
		if text == "" {
			return false
		}
		low := strings.ToLower(text)
		exact := low == needle
		if !exact && !strings.Contains(low, needle) {
			return false
		}
		if !hasAnyFrag(b, n) {
			return false
		}
		prio, length := clickTextPriority(n), len([]rune(low))
		better := false
		switch {
		case best == nil:
			better = true
		case exact != bestExact:
			better = exact
		case prio != bestPrio:
			better = prio < bestPrio
		case length != bestLen:
			better = length < bestLen
		}
		if better {
			best, bestExact, bestPrio, bestLen = n, exact, prio, length
		}
		return false
	})
	if best == nil {
		return nil, fmt.Errorf("no clickable element with text %q", query)
	}
	return best, nil
}

// hasAnyFrag reports whether layout produced a fragment for n or its subtree,
// including content outside the current viewport.
func hasAnyFrag(b *Session, n *Node) bool {
	for i := range b.Layout.Frags {
		f := &b.Layout.Frags[i]
		if f.Node == n || (f.Node != nil && isDescendant(f.Node, n)) {
			return true
		}
	}
	return false
}

func nodeMatchText(n *Node) string {
	if n.Tag == "input" {
		switch strings.ToLower(strings.TrimSpace(n.Attr("type"))) {
		case "submit", "button", "reset":
			if v := n.Attr("value"); v != "" {
				return collapseWhitespace(v)
			}
		}
	}
	var sb strings.Builder
	writeMatchText(&sb, n)
	if text := collapseWhitespace(sb.String()); text != "" {
		return text
	}
	for _, key := range []string{"aria-label", "alt", "title"} {
		if v := n.Attr(key); v != "" {
			return collapseWhitespace(v)
		}
	}
	return ""
}

func writeMatchText(sb *strings.Builder, n *Node) {
	for _, c := range n.Children {
		switch c.Type {
		case TextNode:
			sb.WriteString(c.Text)
			sb.WriteByte(' ')
		case ElementNode:
			switch c.Tag {
			case "script", "style", "noscript", "template":
				continue
			}
			if c.HasAttr("hidden") || c.Style != nil && c.Style.Display == "none" {
				continue
			}
			writeMatchText(sb, c)
		}
	}
}

func clickTextPriority(n *Node) int {
	switch n.Tag {
	case "a":
		return 0
	case "button", "summary":
		return 1
	case "label":
		return 2
	case "input", "select", "textarea":
		return 3
	}
	if strings.EqualFold(n.Attr("role"), "button") || n.Attr("onclick") != "" {
		return 1
	}
	return 10
}

func findSelectorNode(root *Node, sel string) (*Node, error) {
	s, err := ParseSelector(sel)
	if err != nil {
		return nil, fmt.Errorf("bad selector: %w", err)
	}
	nodes := root.FindAll(func(n *Node) bool { return n.Type == ElementNode })
	for _, n := range nodes {
		if s.Match(n) {
			return n, nil
		}
	}
	return nil, fmt.Errorf("no element matches %q", sel)
}

func describeNode(n *Node) string {
	if n == nil {
		return "node"
	}
	s := n.Tag
	if id := n.Attr("id"); id != "" {
		s += "#" + id
	} else if name := n.Attr("name"); name != "" {
		s += "[name=" + strconv.Quote(name) + "]"
	}
	return s
}
