package engine

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Listing output is bounded by item count and text size. The character budget
// leaves room under the MCP links/form response clip so the Showing/Next header
// always survives and the next offset is accurate.
const (
	automationListCap   = 2000
	automationListChars = 49000
)

// Links lists every anchor in document order, including off-screen links, with
// the [n] index click accepts, its text, resolved href and CSS selector.
func (a *Automation) Links(selector string) (string, error) {
	return a.linksPage(selector, 0, 0)
}

// linksPage returns one page of matching links. offset is a zero-based index
// among the matches; limit <= 0 fills the response budget.
func (a *Automation) linksPage(selector string, offset, limit int) (string, error) {
	if a == nil || a.b == nil {
		return "", errors.New("automation is closed")
	}
	if offset < 0 {
		return "", errors.New("offset must be zero or positive")
	}
	if limit < 0 {
		return "", errors.New("limit must be zero or positive")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.Page == nil || b.Page.Root == nil || b.Layout == nil {
		return "No page loaded", nil
	}
	var filter *Selector
	if selector != "" {
		s, err := ParseSelector(selector)
		if err != nil {
			return "", fmt.Errorf("bad selector: %w", err)
		}
		filter = s
	}
	maxItems := automationListCap
	if limit > 0 && limit < maxItems {
		maxItems = limit
	}
	total := 0
	chars := 0
	full := false
	var lines []string
	for i := range b.Layout.Links {
		li := b.Layout.Links[i]
		if li.Node == nil || filter != nil && !filter.Match(li.Node) {
			continue
		}
		pos := total
		total++
		if pos < offset || full {
			continue
		}
		if len(lines) >= maxItems {
			full = true
			continue
		}
		text := collapseWhitespace(li.Node.TextContent())
		if text == "" {
			text = li.Node.Attr("aria-label")
		}
		if text == "" {
			text = li.Node.Attr("alt")
		}
		if text == "" {
			text = "(no text)"
		}
		line := fmt.Sprintf("[%d] %s -> %s selector=%s",
			i+1, clipText(text, 100), b.resolveHref(li.Href), strconv.Quote(selectorForNode(b.Page.Root, li.Node)))
		if len(lines) > 0 && chars+len(line)+1 > automationListChars {
			full = true
			continue
		}
		lines = append(lines, line)
		chars += len(line) + 1
	}
	if total == 0 {
		if selector != "" {
			return "No links match " + strconv.Quote(selector), nil
		}
		return "No links found", nil
	}
	if offset >= total {
		return "", fmt.Errorf("offset %d is past the last link (%d links)", offset, total)
	}
	end := offset + len(lines)
	var sb strings.Builder
	fmt.Fprintf(&sb, "Links: %d\n", total)
	fmt.Fprintf(&sb, "Showing: %d-%d of %d", offset+1, end, total)
	if end < total {
		fmt.Fprintf(&sb, "  Next: %d", end)
	}
	sb.WriteString("\n")
	sb.WriteString(strings.Join(lines, "\n"))
	return sb.String(), nil
}

// Requests lists the in-memory request log for the current page. limit 0 shows
// everything; the newest entries are kept when a limit is given.
func (a *Automation) Requests(limit int) string {
	if a == nil || a.b == nil {
		return "(requests unavailable)"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.loader == nil {
		return "(requests unavailable)"
	}
	records := b.loader.RequestSnapshot()
	lines := b.requestLines()
	header := fmt.Sprintf("Requests: %d", len(records))
	if len(records) == 0 {
		lines = nil
	}
	if limit > 0 && len(lines) > limit {
		header += fmt.Sprintf(" (showing last %d)", limit)
		lines = lines[len(lines)-limit:]
	}
	var sb strings.Builder
	sb.WriteString(header)
	sb.WriteString("\n")
	if len(lines) == 0 {
		sb.WriteString("(no requests for this page)\n")
	} else {
		sb.WriteString(strings.Join(lines, "\n"))
		sb.WriteString("\n")
	}
	sb.WriteString(b.requestCacheSummary())
	return sb.String()
}

// Logs returns JavaScript console messages and uncaught errors for the current
// page, newest last. limit 0 shows every retained entry.
func (a *Automation) Logs(limit int) string {
	if a == nil || a.b == nil {
		return "(no JavaScript messages)"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	entries := b.jsLog
	header := fmt.Sprintf("JS logs: %d", len(entries))
	if limit > 0 && len(entries) > limit {
		header += fmt.Sprintf(" (showing last %d)", limit)
		entries = entries[len(entries)-limit:]
	}
	if b.jsErr {
		header += " · error"
	}
	var sb strings.Builder
	sb.WriteString(header)
	sb.WriteString("\n")
	if b.NoJS {
		sb.WriteString("(JavaScript is disabled)\n")
	}
	if len(entries) == 0 && !b.NoJS {
		sb.WriteString("(no JavaScript messages)\n")
	}
	for _, entry := range entries {
		for _, line := range strings.Split(entry, "\n") {
			sb.WriteString(StripTerminalControls(line))
			sb.WriteString("\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// Source returns the raw decoded page source (pre-scripting DOM), or the
// serialized HTML of one element when selector is set. Terminal control
// characters are removed; newlines and tabs are kept.
func (a *Automation) Source(selector string) (string, error) {
	if a == nil || a.b == nil {
		return "", errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.Page == nil || b.Page.Root == nil {
		return "No page loaded", nil
	}
	src := b.Page.RawHTML
	if selector != "" {
		n, err := findSelectorNode(b.Page.Root, selector)
		if err != nil {
			return "", err
		}
		var sb strings.Builder
		SerializeNode(&sb, n)
		src = sb.String()
	}
	if src == "" {
		return "(empty source)", nil
	}
	return sanitizeSource(src), nil
}

// sanitizeSource removes terminal control bytes from source text while keeping
// line structure.
func sanitizeSource(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 32 || (r >= 0x7f && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}
