package engine

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Links lists every anchor in document order, including off-screen links, with
// the [n] index click accepts, its text, resolved href and CSS selector.
func (a *Automation) Links(selector string) (string, error) {
	if a == nil || a.b == nil {
		return "", errors.New("automation is closed")
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
	var lines []string
	total := 0
	truncated := false
	for i := range b.Layout.Links {
		li := b.Layout.Links[i]
		if li.Node == nil || filter != nil && !filter.Match(li.Node) {
			continue
		}
		total++
		if len(lines) >= 200 {
			truncated = true
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
		lines = append(lines, fmt.Sprintf("[%d] %s -> %s selector=%s",
			i+1, clipText(text, 100), b.resolveHref(li.Href), strconv.Quote(selectorForNode(b.Page.Root, li.Node))))
	}
	if total == 0 {
		if selector != "" {
			return "No links match " + strconv.Quote(selector), nil
		}
		return "No links found", nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Links: %d\n", total)
	sb.WriteString(strings.Join(lines, "\n"))
	if truncated {
		fmt.Fprintf(&sb, "\n(truncated at %d links)", len(lines))
	}
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
