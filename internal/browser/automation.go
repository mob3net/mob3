package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Automation drives one headless Browser for scripted clients. Commands are
// serialized; the browser never writes terminal output.
type Automation struct {
	mu sync.Mutex
	b  *Browser
}

// ClickTarget selects what to click. Exactly one of Link, Text, Selector or
// HasCoords should be set. Coordinates are zero-based cells in the page
// viewport (row 0 is the first page row below the chrome).
type ClickTarget struct {
	Link      int
	Text      string
	Selector  string
	X, Y      int
	HasCoords bool
}

// NewAutomation creates a headless browser with a virtual text viewport.
func NewAutomation(width, height int) *Automation {
	b := NewBrowser(width, height)
	b.noDraw = true
	if b.loader != nil {
		// Automation exposes text; image downloads would only slow it down.
		b.loader.noImages = true
	}
	return &Automation{b: b}
}

// Close stops pending work and releases the page runtime.
func (a *Automation) Close() {
	if a == nil || a.b == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.b.cancelLoadNow()
	a.b.stopJS()
}

// Navigate loads a URL or resolves typed input exactly like the URL prompt,
// then waits for the page, meta refresh and scripted redirects.
func (a *Automation) Navigate(ctx context.Context, target string) error {
	if a == nil || a.b == nil {
		return errors.New("automation is closed")
	}
	if strings.TrimSpace(target) == "" {
		return errors.New("empty URL")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	before := a.b.loadSeq
	a.b.Navigate(target, true)
	if err := a.waitLoad(ctx); err != nil {
		return err
	}
	if a.b.page == nil {
		return fmt.Errorf("navigation failed: %s", nonEmptyStatus(a.b.status))
	}
	if a.b.loadSeq == before {
		switch {
		case strings.HasPrefix(a.b.status, "Invalid URL:"),
			strings.HasPrefix(a.b.status, "Blocked "),
			strings.HasPrefix(a.b.status, "Nothing to retry"):
			return errors.New(a.b.status)
		}
	}
	return nil
}

// waitLoad applies asynchronous load results. finishLoad can queue another
// navigation (meta refresh or location changes), so keep waiting while the
// browser reports a load.
func (a *Automation) waitLoad(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for a.b.loading {
		select {
		case res := <-a.b.loadCh:
			a.b.finishLoad(res)
		case <-ctx.Done():
			a.b.cancelLoadNow()
			return ctx.Err()
		}
	}
	return nil
}

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
	if b.url != nil {
		u = b.url.String()
	}
	title := ""
	if b.page != nil {
		title = b.page.Title
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
	fmt.Fprintf(&sb, "Viewport: %dx%d  Scroll: %d%%\n\n", b.width, vh, b.scrollPercent())
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
	if b.layout == nil || b.page == nil {
		return nil
	}
	b.layout.SetFixedScroll(b.scroll)
	g := NewGrid(b.width, vh, b.pageFG(), b.pageBG())
	PaintLayout(g, b.layout, b.scroll, 0, vh, nil, b.pageFG())
	return g
}

// pageText renders the current page viewport without browser chrome.
func (a *Automation) pageTextLocked(vh int) string {
	g := a.viewportGridLocked(vh)
	if g == nil {
		return ""
	}
	lines := g.Lines(false)
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
	return g.PNG(scale)
}

func (a *Automation) linkLinesLocked(vh int) []string {
	b := a.b
	if b.layout == nil {
		return nil
	}
	var out []string
	for i := range b.layout.Links {
		li := b.layout.Links[i]
		if !rectVisible(li.Rects, b.scroll, vh) {
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
	if b.page == nil || b.page.Root == nil || b.layout == nil {
		return nil
	}
	var out []string
	for _, n := range b.page.Root.FindAll(func(n *Node) bool {
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
		if typ == "hidden" || isDisabledControl(n) {
			continue
		}
		f := firstVisibleFrag(b, n, b.scroll, vh)
		if f == nil {
			continue
		}
		name := n.Attr("name")
		if name == "" {
			name = n.Attr("id")
		}
		value := inputValue(n)
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
			f.X, f.Y-b.scroll)
		out = append(out, line)
		if len(out) >= 40 {
			break
		}
	}
	return out
}

func firstVisibleFrag(b *Browser, n *Node, scroll, vh int) *Frag {
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Kind != fragText || f.Node == nil {
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

// Click activates a link, a selector, or a viewport cell. It waits for any
// navigation the click causes.
func (a *Automation) Click(ctx context.Context, target ClickTarget) error {
	if a == nil || a.b == nil {
		return errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.page == nil || b.layout == nil {
		return errors.New("no page loaded")
	}
	switch {
	case target.Link > 0:
		if target.Link > len(b.layout.Links) {
			return fmt.Errorf("link %d out of range (%d links)", target.Link, len(b.layout.Links))
		}
		b.focus = target.Link - 1
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
		n, err := findSelectorNode(b.page.Root, target.Selector)
		if err != nil {
			return err
		}
		if err := a.clickNodeLocked(n); err != nil {
			return err
		}
		return a.waitLoad(ctx)
	case target.HasCoords:
		top, _ := b.viewport()
		if target.X < 0 || target.X >= b.width || target.Y < 0 || target.Y >= b.viewportHeight() {
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
	for i := range b.layout.Links {
		li := b.layout.Links[i]
		if li.Node == anchor {
			b.focus = i
			b.followFocused()
			return nil
		}
	}
	f := firstVisibleFrag(b, n, b.scroll, b.viewportHeight())
	if f == nil {
		// Selector clicks scroll the element into view, like a browser.
		revealNode(b, n)
		f = firstVisibleFrag(b, n, b.scroll, b.viewportHeight())
	}
	if f == nil {
		return fmt.Errorf("element %s is not visible", describeNode(n))
	}
	top, _ := b.viewport()
	x, y := f.X+1, top+f.Y-b.scroll+1
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: true})
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: false})
	return nil
}

// findNodeByText returns the most specific rendered element whose visible text
// matches query, preferring exact matches and interactive elements.
func findNodeByText(b *Browser, query string) (*Node, error) {
	needle := strings.ToLower(collapseWhitespace(query))
	if needle == "" {
		return nil, errors.New("click text is empty")
	}
	if b.page == nil || b.page.Root == nil || b.layout == nil {
		return nil, errors.New("no page loaded")
	}
	var best *Node
	bestExact := false
	bestPrio, bestLen := 0, 0
	b.page.Root.FindAll(func(n *Node) bool {
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
func hasAnyFrag(b *Browser, n *Node) bool {
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
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

// Type sends text to the focused input, textarea or URL prompt.
func (a *Automation) Type(ctx context.Context, text string) error {
	if a == nil || a.b == nil {
		return errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.inputFocus == nil && !b.prompt {
		return errors.New("no focused input; click a text field first")
	}
	if utf8LenString(text) > 20000 {
		return errors.New("text is too long")
	}
	for _, r := range text {
		var key string
		switch r {
		case '\n':
			key = "enter"
		case '\t':
			key = "tab"
		default:
			key = string(r)
		}
		if b.HandleKey(key) {
			return errors.New("browser requested quit")
		}
	}
	return a.waitLoad(ctx)
}

func utf8LenString(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// Press sends one named key (Enter, Tab, Escape, arrows, PageDown, …) to the
// browser and waits for any resulting navigation.
func (a *Automation) Press(ctx context.Context, key string) error {
	if a == nil || a.b == nil {
		return errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	token := keyToken(key)
	if token == "" {
		return fmt.Errorf("unknown key %q", key)
	}
	if a.b.HandleKey(token) {
		return errors.New("browser requested quit")
	}
	return a.waitLoad(ctx)
}

func keyToken(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "enter", "return":
		return "enter"
	case "tab":
		return "tab"
	case "escape", "esc":
		return "esc"
	case "backspace":
		return "backspace"
	case "delete", "del":
		return "delete"
	case "arrowup", "up":
		return "up"
	case "arrowdown", "down":
		return "down"
	case "arrowleft", "left":
		return "left"
	case "arrowright", "right":
		return "right"
	case "home":
		return "home"
	case "end":
		return "end"
	case "pageup", "pgup":
		return "pgup"
	case "pagedown", "pgdn":
		return "pgdn"
	case "space":
		return " "
	}
	if len([]rune(key)) == 1 {
		return key
	}
	return ""
}

// Scroll moves the page by lines, or to the top/bottom.
func (a *Automation) Scroll(lines, pages int, top, bottom bool) {
	if a == nil || a.b == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case top:
		a.b.scroll = 0
		a.b.clampScroll()
	case bottom:
		a.b.scroll = a.b.maxScroll()
	case pages != 0:
		a.b.scrollBy(pages * a.b.viewportHeight())
	default:
		a.b.scrollBy(lines)
	}
}

// Back navigates backward in session history.
func (a *Automation) Back(ctx context.Context) error {
	return a.historyAction(ctx, func(b *Browser) { b.back() })
}

// Forward navigates forward in session history.
func (a *Automation) Forward(ctx context.Context) error {
	return a.historyAction(ctx, func(b *Browser) { b.forward() })
}

// Reload reloads the current page.
func (a *Automation) Reload(ctx context.Context) error {
	return a.historyAction(ctx, func(b *Browser) { b.reload() })
}

func (a *Automation) historyAction(ctx context.Context, action func(*Browser)) error {
	if a == nil || a.b == nil {
		return errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	action(a.b)
	return a.waitLoad(ctx)
}

// Evaluate runs JavaScript in the current page realm and returns the result's
// string form. Objects appear as "[object Object]"; use JSON.stringify to get
// structured data.
func (a *Automation) Evaluate(ctx context.Context, expression string) (string, error) {
	if a == nil || a.b == nil {
		return "", errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.js == nil {
		return "", errors.New("JavaScript is disabled or no page is loaded")
	}
	v, err := b.js.runCode("automation", expression)
	if b.consumePendingJS() {
		if err := a.waitLoad(ctx); err != nil {
			return "", err
		}
	}
	if b.js.consumeDirty() {
		b.rebuild()
	}
	if err != nil {
		return "", err
	}
	if v == nil {
		return "", nil
	}
	return clipText(v.String(), 20000), nil
}

// FormFields lists every form and form control in the document, including
// off-screen ones. Each control carries a stable CSS selector and its current
// value or state so fill, select, toggle and submit can target it directly.
func (a *Automation) FormFields(selector string) (string, error) {
	if a == nil || a.b == nil {
		return "", errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.page == nil || b.page.Root == nil {
		return "No page loaded", nil
	}
	root := b.page.Root
	var forms []*Node
	if selector != "" {
		n, err := findSelectorNode(root, selector)
		if err != nil {
			return "", err
		}
		orig := n
		if n.Tag != "form" {
			n = nearestForm(n)
		}
		if n == nil {
			return "", fmt.Errorf("%s is not inside a form", describeNode(orig))
		}
		forms = []*Node{n}
	} else {
		forms = root.FindAll(func(n *Node) bool { return n.Tag == "form" })
	}

	var sb strings.Builder
	total := 0
	truncated := false
	if len(forms) > 0 {
		fmt.Fprintf(&sb, "Forms: %d\n", len(forms))
		for i, f := range forms {
			fmt.Fprintf(&sb, "[%d] form method=%q action=%q selector=%s\n",
				i+1, formMethod(f), f.Attr("action"), strconv.Quote(selectorForNode(root, f)))
			lines, more := controlLines(root, f, "  ", 200-total)
			for _, line := range lines {
				sb.WriteString(line)
				sb.WriteString("\n")
			}
			total += len(lines)
			if more {
				truncated = true
				break
			}
		}
	}
	if !truncated {
		var loose []*Node
		for _, n := range root.FindAll(func(n *Node) bool { return isFormControl(n) }) {
			if isHiddenInput(n) || nearestForm(n) != nil {
				continue
			}
			loose = append(loose, n)
		}
		if len(loose) > 0 {
			if len(forms) > 0 {
				sb.WriteString("\n")
			}
			fmt.Fprintf(&sb, "Controls outside any form: %d\n", len(loose))
			for _, n := range loose {
				if total >= 200 {
					truncated = true
					break
				}
				sb.WriteString("  ")
				sb.WriteString(controlLine(root, n))
				sb.WriteString("\n")
				total++
			}
		}
	}
	if truncated {
		sb.WriteString("(truncated at 200 controls)\n")
	}
	out := strings.TrimRight(sb.String(), "\n")
	if out == "" {
		return "No forms or form controls found", nil
	}
	return out, nil
}

func formMethod(form *Node) string {
	m := strings.ToUpper(strings.TrimSpace(form.Attr("method")))
	if m == "" {
		return "GET"
	}
	return m
}

func isHiddenInput(n *Node) bool {
	return n.Tag == "input" && strings.EqualFold(strings.TrimSpace(n.Attr("type")), "hidden")
}

// controlLines renders at most max controls of one container. It reports
// whether more controls were left out.
func controlLines(root, container *Node, indent string, max int) ([]string, bool) {
	var lines []string
	for _, n := range container.FindAll(func(n *Node) bool { return isFormControl(n) }) {
		if isHiddenInput(n) {
			continue
		}
		if len(lines) >= max {
			return lines, true
		}
		lines = append(lines, indent+controlLine(root, n))
	}
	return lines, false
}

func controlLine(root, n *Node) string {
	typ := ""
	if n.Tag == "input" {
		typ = strings.ToLower(strings.TrimSpace(n.Attr("type")))
		if typ == "" {
			typ = "text"
		}
	}
	parts := []string{n.Tag}
	if name := n.Attr("name"); name != "" {
		parts = append(parts, "name="+strconv.Quote(name))
	}
	switch {
	case typ != "":
		parts = append(parts, "type="+strconv.Quote(typ))
	case n.Tag == "button":
		parts = append(parts, "type="+strconv.Quote("submit"))
	}
	switch {
	case typ == "checkbox" || typ == "radio":
		state := "unchecked"
		if n.HasAttr("checked") {
			state = "checked"
		}
		value := n.Attr("value")
		if value == "" {
			value = "on"
		}
		parts = append(parts, "state="+state, "value="+strconv.Quote(value))
	case n.Tag == "select":
		opts := selectOptions(n)
		var values []string
		for _, o := range opts {
			values = append(values, optionValue(o))
		}
		if n.HasAttr("multiple") {
			parts = append(parts, "multiple", "values="+quotedList(selectedOptionValues(n)))
		} else {
			parts = append(parts, "value="+strconv.Quote(inputValue(n)))
		}
		parts = append(parts, "options="+quotedList(values))
	case n.Tag == "textarea":
		parts = append(parts, "value="+strconv.Quote(clipText(collapseWhitespace(inputValue(n)), 80)))
	case n.Tag == "button":
		label := collapseWhitespace(n.TextContent())
		if label == "" {
			label = n.Attr("value")
		}
		parts = append(parts, "text="+strconv.Quote(clipText(label, 80)))
	default:
		parts = append(parts, "value="+strconv.Quote(clipText(collapseWhitespace(inputValue(n)), 80)))
	}
	if n.HasAttr("required") {
		parts = append(parts, "required")
	}
	if isReadOnlyControl(n) {
		parts = append(parts, "readonly")
	}
	if isDisabledControl(n) {
		parts = append(parts, "disabled")
	}
	parts = append(parts, "selector="+strconv.Quote(selectorForNode(root, n)))
	return "- " + strings.Join(parts, " ")
}

// Fill sets text-like controls. Keys are CSS selectors; each value replaces the
// control's current value. It does not submit anything.
func (a *Automation) Fill(fields map[string]string) (int, error) {
	if a == nil || a.b == nil {
		return 0, errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.page == nil || b.page.Root == nil {
		return 0, errors.New("no page loaded")
	}
	if len(fields) == 0 {
		return 0, errors.New("fill needs at least one selector")
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	filled := 0
	for _, sel := range keys {
		n, err := findSelectorNode(b.page.Root, sel)
		if err != nil {
			return filled, err
		}
		if err := a.fillControlLocked(n, fields[sel]); err != nil {
			return filled, fmt.Errorf("%s: %w", sel, err)
		}
		filled++
	}
	return filled, nil
}

func (a *Automation) fillControlLocked(n *Node, value string) error {
	b := a.b
	if n == nil || n.Type != ElementNode {
		return errors.New("not an element")
	}
	if isDisabledControl(n) {
		return fmt.Errorf("%s is disabled", describeNode(n))
	}
	if n.Tag == "textarea" {
		if isReadOnlyControl(n) {
			return fmt.Errorf("%s is read-only", describeNode(n))
		}
		b.setInputValue(n, truncateRunes(value, maxLength(n)))
		a.fireChangeLocked(n)
		return nil
	}
	if n.Tag != "input" {
		return fmt.Errorf("%s is not an input or textarea; use select or toggle", describeNode(n))
	}
	typ := strings.ToLower(strings.TrimSpace(n.Attr("type")))
	switch typ {
	case "checkbox", "radio":
		return errors.New("use toggle for checkboxes and radios")
	case "file":
		return errors.New("file uploads are not supported")
	case "submit", "button", "reset", "image", "hidden":
		return fmt.Errorf("%s is not an editable field", describeNode(n))
	case "range":
		f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return errors.New("range value must be a number")
		}
		b.setRangeValue(n, f)
		return nil
	}
	if isReadOnlyControl(n) {
		return fmt.Errorf("%s is read-only", describeNode(n))
	}
	if !isTextInput(n) {
		return fmt.Errorf("%s is not an editable text field", describeNode(n))
	}
	b.setInputValue(n, truncateRunes(value, maxLength(n)))
	a.fireChangeLocked(n)
	return nil
}

// fireChangeLocked fires the change event a browser sends when a value is
// committed and applies any DOM mutations the handler made.
func (a *Automation) fireChangeLocked(n *Node) {
	b := a.b
	if b.js == nil {
		return
	}
	b.js.fireNodeEvent(n, "change")
	if b.js.consumeDirty() {
		b.rebuild()
	}
}

func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit])
}

// Select chooses one or more options in a select by value, label, or zero-based
// index. For a multiple select, values selects the whole set.
func (a *Automation) Select(selector string, value *string, values []string, label string, index *int) (string, error) {
	if a == nil || a.b == nil {
		return "", errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.page == nil || b.page.Root == nil {
		return "", errors.New("no page loaded")
	}
	n, err := findSelectorNode(b.page.Root, selector)
	if err != nil {
		return "", err
	}
	if n.Tag != "select" {
		return "", fmt.Errorf("%s is not a <select>", describeNode(n))
	}
	if isDisabledControl(n) {
		return "", fmt.Errorf("%s is disabled", describeNode(n))
	}
	opts := selectOptions(n)
	var enabled []*Node
	for _, o := range opts {
		if !isDisabledControl(o) {
			enabled = append(enabled, o)
		}
	}
	if len(enabled) == 0 {
		return "", errors.New("select has no enabled options")
	}
	multi := n.HasAttr("multiple")
	var chosen []*Node
	switch {
	case len(values) > 0:
		if !multi && len(values) != 1 {
			return "", errors.New("values needs a multiple select; use value for a single select")
		}
		seen := map[*Node]bool{}
		for _, v := range values {
			o := optionByValue(enabled, v)
			if o == nil {
				return "", fmt.Errorf("no option with value %q (options: %s)", v, optionValueList(enabled))
			}
			if !seen[o] {
				seen[o] = true
				chosen = append(chosen, o)
			}
		}
	case index != nil:
		if *index < 0 || *index >= len(opts) {
			return "", fmt.Errorf("option index %d out of range (0-%d)", *index, len(opts)-1)
		}
		o := opts[*index]
		if isDisabledControl(o) {
			return "", errors.New("option is disabled")
		}
		chosen = []*Node{o}
	case label != "":
		o := optionByLabel(enabled, label)
		if o == nil {
			return "", fmt.Errorf("no option for %q (options: %s)", label, optionValueList(enabled))
		}
		chosen = []*Node{o}
	case value != nil:
		o := optionByValue(enabled, *value)
		if o == nil {
			return "", fmt.Errorf("no option with value %q (options: %s)", *value, optionValueList(enabled))
		}
		chosen = []*Node{o}
	default:
		return "", errors.New("select needs value, values, text or index")
	}
	for _, o := range opts {
		removeAttr(o, "selected")
	}
	var picked []string
	for _, o := range chosen {
		setAttr(o, "selected", "")
		picked = append(picked, optionValue(o))
	}
	// A select's value is derived from its selected option attributes; drop any
	// stale direct value a script may have set.
	n.ValueSet = false
	n.Value = ""
	b.fireSelectChange(n)
	return fmt.Sprintf("selected %s = %s", describeNode(n), quotedList(picked)), nil
}

func optionByValue(opts []*Node, value string) *Node {
	for _, o := range opts {
		if optionValue(o) == value {
			return o
		}
	}
	return nil
}

func optionByLabel(opts []*Node, label string) *Node {
	for _, o := range opts {
		if optionLabel(o) == label {
			return o
		}
	}
	var match *Node
	for _, o := range opts {
		if strings.EqualFold(optionLabel(o), label) {
			if match != nil {
				return match // ambiguous case-only match; take the first
			}
			match = o
		}
	}
	return match
}

func optionValueList(opts []*Node) string {
	var values []string
	for _, o := range opts {
		values = append(values, optionValue(o))
	}
	return clipText(quotedList(values), 160)
}

// Toggle checks or unchecks a checkbox, radio, or multi-select option. Without
// an explicit state it flips the current one.
func (a *Automation) Toggle(selector string, checked *bool) (string, error) {
	if a == nil || a.b == nil {
		return "", errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.page == nil || b.page.Root == nil {
		return "", errors.New("no page loaded")
	}
	n, err := findSelectorNode(b.page.Root, selector)
	if err != nil {
		return "", err
	}
	if isDisabledControl(n) {
		return "", fmt.Errorf("%s is disabled", describeNode(n))
	}
	if n.Tag == "option" {
		sel := nearestSelect(n)
		if sel == nil || !sel.HasAttr("multiple") {
			return "", errors.New("use select for <select> options")
		}
		current := n.HasAttr("selected")
		want := !current
		if checked != nil {
			want = *checked
		}
		if current == want {
			return toggleState(n, want), nil
		}
		if want {
			setAttr(n, "selected", "")
		} else {
			removeAttr(n, "selected")
		}
		b.fireSelectChange(sel)
		return toggleState(n, want), nil
	}
	if n.Tag != "input" {
		return "", fmt.Errorf("%s is not a checkbox, radio or option", describeNode(n))
	}
	typ := strings.ToLower(strings.TrimSpace(n.Attr("type")))
	if typ != "checkbox" && typ != "radio" {
		return "", fmt.Errorf("%s is not a checkbox or radio; use select for <select>", describeNode(n))
	}
	current := n.HasAttr("checked")
	want := !current
	if checked != nil {
		want = *checked
	}
	if typ == "radio" && !want {
		if current {
			return "", errors.New("a radio cannot be unchecked; choose another radio instead")
		}
		return toggleState(n, false), nil
	}
	if current != want {
		b.toggleControl(n)
	}
	return toggleState(n, want), nil
}

func toggleState(n *Node, checked bool) string {
	state := "unchecked"
	if checked {
		state = "checked"
	}
	return fmt.Sprintf("%s is %s", describeNode(n), state)
}

// Submit resolves a form, runs constraint validation and submit handlers, and
// waits for the navigation the submission starts. Without a selector it uses
// the focused control's form or the page's only form.
func (a *Automation) Submit(ctx context.Context, selector string) error {
	if a == nil || a.b == nil {
		return errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.page == nil || b.page.Root == nil {
		return errors.New("no page loaded")
	}
	var form *Node
	if selector == "" {
		if b.inputFocus != nil {
			form = nearestForm(b.inputFocus)
		}
		if form == nil {
			forms := b.page.Root.FindAll(func(n *Node) bool { return n.Tag == "form" })
			switch len(forms) {
			case 0:
				return errors.New("no form on the page")
			case 1:
				form = forms[0]
			default:
				return fmt.Errorf("submit needs a selector (%d forms on the page)", len(forms))
			}
		}
	} else {
		n, err := findSelectorNode(b.page.Root, selector)
		if err != nil {
			return err
		}
		if n.Tag == "form" {
			form = n
		} else {
			form = nearestForm(n)
		}
		if form == nil {
			return fmt.Errorf("%s is not inside a form", describeNode(n))
		}
	}
	started, reason := b.submitForm(form)
	if !started {
		if reason == "" {
			reason = "form was not submitted"
		}
		return errors.New(reason)
	}
	return a.waitPendingLocked(ctx)
}

// waitPendingLocked applies any navigation queued by input/change/submit
// handlers and waits for the resulting load.
func (a *Automation) waitPendingLocked(ctx context.Context) error {
	if a.b.consumePendingJS() || a.b.loading {
		return a.waitLoad(ctx)
	}
	return nil
}

// revealNode scrolls the page until one of n's text fragments is in view.
func revealNode(b *Browser, n *Node) {
	if b.layout == nil {
		return
	}
	vh := b.viewportHeight()
	var first *Frag
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Kind != fragText || f.Node == nil {
			continue
		}
		if f.Node != n && !isDescendant(f.Node, n) {
			continue
		}
		if first == nil {
			first = f
		}
		if f.Y >= b.scroll && f.Y < b.scroll+vh {
			return
		}
	}
	if first == nil {
		return
	}
	if first.Y < b.scroll {
		b.scroll = first.Y
	} else {
		b.scroll = first.Y - vh + 1
	}
	b.clampScroll()
}

// selectorForNode returns a unique CSS selector for n: an id, a name qualified
// by its form, or an nth-child path as a last resort.
func selectorForNode(root, n *Node) string {
	if n == nil {
		return ""
	}
	if id := n.Attr("id"); id != "" && safeCSSIdent(id) {
		if sel := "#" + id; selectorMatchesNode(root, n, sel) {
			return sel
		}
	}
	if name := n.Attr("name"); name != "" && safeAttrText(name) {
		base := n.Tag + "[name=" + strconv.Quote(name) + "]"
		if form := nearestForm(n); form != nil {
			if id := form.Attr("id"); id != "" && safeCSSIdent(id) {
				if sel := "#" + id + " " + base; selectorMatchesNode(root, n, sel) {
					return sel
				}
			}
		}
		if selectorMatchesNode(root, n, base) {
			return base
		}
	}
	if n.Tag == "a" {
		if href := n.Attr("href"); href != "" && safeAttrText(href) {
			if sel := "a[href=" + strconv.Quote(href) + "]"; selectorMatchesNode(root, n, sel) {
				return sel
			}
		}
	}
	return cssNodePath(n)
}

func selectorMatchesNode(root, want *Node, sel string) bool {
	s, err := ParseSelector(sel)
	if err != nil {
		return false
	}
	match, count := false, 0
	root.FindAll(func(x *Node) bool {
		if x.Type == ElementNode && s.Match(x) {
			count++
			if x == want {
				match = true
			}
		}
		return false
	})
	return match && count == 1
}

func cssNodePath(n *Node) string {
	var parts []string
	for x := n; x != nil && x.Type == ElementNode; x = x.Parent {
		idx, _ := elementIndex(x)
		if idx == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:nth-child(%d)", x.Tag, idx))
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, " > ")
}

func safeCSSIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

// safeAttrText reports an attribute value the selector parser can read back
// from a double-quoted selector without escape processing.
func safeAttrText(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f || c == '"' || c == '\\' {
			return false
		}
	}
	return true
}

func quotedList(items []string) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, s := range items {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(strconv.Quote(s))
	}
	sb.WriteByte(']')
	return sb.String()
}

// waitPollInterval bounds how often wait and wait_for re-check the page and
// pump due timers.
const waitPollInterval = 20 * time.Millisecond

// Wait advances the page for ms milliseconds, running due JavaScript timers,
// applying the rebuilds they cause, and waiting for navigations they start.
func (a *Automation) Wait(ctx context.Context, ms int) (int, error) {
	if a == nil || a.b == nil {
		return 0, errors.New("automation is closed")
	}
	if ms < 0 {
		ms = 0
	}
	if ms > 10000 {
		ms = 10000
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	deadline := time.Now().Add(time.Duration(ms) * time.Millisecond)
	for {
		if err := a.waitStepLocked(ctx, deadline); err != nil {
			return ms, err
		}
		if !time.Now().Before(deadline) {
			break
		}
	}
	a.flushDirtyLocked()
	return ms, nil
}

// WaitFor pumps timers until a selector matches an element and/or text appears
// in the rendered page, or the timeout expires.
func (a *Automation) WaitFor(ctx context.Context, selector, text string, timeoutMS int) (string, error) {
	if a == nil || a.b == nil {
		return "", errors.New("automation is closed")
	}
	if selector == "" && strings.TrimSpace(text) == "" {
		return "", errors.New("wait_for needs a selector or text")
	}
	var sel *Selector
	if selector != "" {
		s, err := ParseSelector(selector)
		if err != nil {
			return "", fmt.Errorf("bad selector: %w", err)
		}
		sel = s
	}
	if timeoutMS <= 0 {
		timeoutMS = 5000
	}
	if timeoutMS > 30000 {
		timeoutMS = 30000
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	start := time.Now()
	deadline := start.Add(time.Duration(timeoutMS) * time.Millisecond)
	needle := strings.ToLower(collapseWhitespace(text))
	for {
		if a.conditionLocked(sel, needle) {
			a.flushDirtyLocked()
			return fmt.Sprintf("found %s after %dms", waitTarget(selector, text), time.Since(start).Milliseconds()), nil
		}
		if !time.Now().Before(deadline) {
			return "", fmt.Errorf("timed out after %dms waiting for %s", timeoutMS, waitTarget(selector, text))
		}
		if err := a.waitStepLocked(ctx, deadline); err != nil {
			return "", err
		}
	}
}

func waitTarget(selector, text string) string {
	switch {
	case selector != "" && strings.TrimSpace(text) != "":
		return fmt.Sprintf("selector %q and text %q", selector, text)
	case selector != "":
		return fmt.Sprintf("selector %q", selector)
	default:
		return fmt.Sprintf("text %q", text)
	}
}

func (a *Automation) conditionLocked(sel *Selector, needle string) bool {
	b := a.b
	if b.page == nil || b.page.Root == nil {
		return false
	}
	if sel != nil {
		found := false
		b.page.Root.FindAll(func(n *Node) bool {
			if !found && n.Type == ElementNode && sel.Match(n) {
				found = true
			}
			return false
		})
		if !found {
			return false
		}
	}
	if needle != "" {
		return strings.Contains(strings.ToLower(a.renderedTextLocked()), needle)
	}
	return true
}

// renderedTextLocked returns the laid-out page text, falling back to DOM text
// when no layout exists yet. Script and style contents are never included.
func (a *Automation) renderedTextLocked() string {
	b := a.b
	var sb strings.Builder
	if b.layout != nil {
		for i := range b.layout.Frags {
			f := &b.layout.Frags[i]
			if f.Kind == fragText && f.Text != "" {
				sb.WriteString(f.Text)
				sb.WriteByte(' ')
			}
		}
	}
	if sb.Len() == 0 && b.page != nil && b.page.Root != nil {
		writeDOMText(&sb, b.page.Root)
	}
	return collapseWhitespace(sb.String())
}

func writeDOMText(sb *strings.Builder, n *Node) {
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
			writeDOMText(sb, c)
		}
	}
}

// waitStepLocked applies a pending load, runs due timers, forces any delayed
// rebuild, and sleeps briefly.
func (a *Automation) waitStepLocked(ctx context.Context, deadline time.Time) error {
	b := a.b
	if b.loading {
		if err := a.waitLoad(ctx); err != nil {
			return err
		}
	}
	if b.js != nil {
		b.runJSTimers()
		if b.consumePendingJS() || b.loading {
			if err := a.waitLoad(ctx); err != nil {
				return err
			}
		}
	}
	a.flushDirtyLocked()
	d := waitPollInterval
	if nd, ok := b.nextJSTimerDelay(); ok && nd < d {
		d = nd
	}
	if rem := time.Until(deadline); rem < d {
		d = rem
	}
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// flushDirtyLocked rebuilds immediately when a timer changed the DOM but the
// interactive redraw coalescing deferred it.
func (a *Automation) flushDirtyLocked() {
	if a.b.js != nil && a.b.js.consumeDirty() {
		a.b.rebuild()
	}
}

// Links lists every anchor in document order, including off-screen links, with
// the [n] index click accepts, its text, resolved href and CSS selector.
func (a *Automation) Links(selector string) (string, error) {
	if a == nil || a.b == nil {
		return "", errors.New("automation is closed")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.b
	if b.page == nil || b.page.Root == nil || b.layout == nil {
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
	for i := range b.layout.Links {
		li := b.layout.Links[i]
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
			i+1, clipText(text, 100), b.resolveHref(li.Href), strconv.Quote(selectorForNode(b.page.Root, li.Node))))
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
	records := b.loader.requestSnapshot()
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
	if b.noJS {
		sb.WriteString("(JavaScript is disabled)\n")
	}
	if len(entries) == 0 && !b.noJS {
		sb.WriteString("(no JavaScript messages)\n")
	}
	for _, entry := range entries {
		for _, line := range strings.Split(entry, "\n") {
			sb.WriteString(stripTerminalControls(line))
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
	if b.page == nil || b.page.Root == nil {
		return "No page loaded", nil
	}
	src := b.page.RawHTML
	if selector != "" {
		n, err := findSelectorNode(b.page.Root, selector)
		if err != nil {
			return "", err
		}
		var sb strings.Builder
		serializeNode(&sb, n)
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

// RunCommand executes one named automation command with JSON arguments. It is
// shared by protocol front ends so each transport only owns framing. Command
// names and argument shapes are the MCP tool definitions in mcp.go.
func (a *Automation) RunCommand(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a == nil || a.b == nil {
		return "", errors.New("automation is closed")
	}
	switch name {
	case "navigate":
		var p struct {
			URL string `json:"url"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		if strings.TrimSpace(p.URL) == "" {
			return "", errors.New("url is required")
		}
		if err := a.Navigate(ctx, p.URL); err != nil {
			return "", err
		}
		return a.Snapshot(), nil
	case "snapshot":
		return a.Snapshot(), nil
	case "click":
		var p struct {
			Link     int    `json:"link"`
			Text     string `json:"text"`
			Selector string `json:"selector"`
			X        *int   `json:"x"`
			Y        *int   `json:"y"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		target := ClickTarget{Link: p.Link, Text: p.Text, Selector: p.Selector}
		targets := 0
		if target.Link > 0 {
			targets++
		}
		if target.Text != "" {
			targets++
		}
		if target.Selector != "" {
			targets++
		}
		if p.X != nil || p.Y != nil {
			if p.X == nil || p.Y == nil {
				return "", errors.New("click x and y must be provided together")
			}
			target.X, target.Y, target.HasCoords = *p.X, *p.Y, true
			targets++
		}
		if targets == 0 {
			return "", errors.New("click needs link, text, selector, or x/y")
		}
		if targets > 1 {
			return "", errors.New("click takes only one of link, text, selector, or x/y")
		}
		if err := a.Click(ctx, target); err != nil {
			return "", err
		}
		return a.Snapshot(), nil
	case "wait":
		var p struct {
			MS *int `json:"ms"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		ms := 100
		if p.MS != nil {
			ms = *p.MS
		}
		waited, err := a.Wait(ctx, ms)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("waited %dms", waited), nil
	case "wait_for":
		var p struct {
			Selector  string `json:"selector"`
			Text      string `json:"text"`
			TimeoutMS int    `json:"timeout_ms"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		return a.WaitFor(ctx, p.Selector, p.Text, p.TimeoutMS)
	case "requests":
		var p struct {
			Limit *int `json:"limit"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		limit := 100
		if p.Limit != nil {
			limit = *p.Limit
		}
		return a.Requests(limit), nil
	case "logs":
		var p struct {
			Limit *int `json:"limit"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		limit := 200
		if p.Limit != nil {
			limit = *p.Limit
		}
		return a.Logs(limit), nil
	case "source":
		var p struct {
			Selector string `json:"selector"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		return a.Source(p.Selector)
	case "links":
		var p struct {
			Selector string `json:"selector"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		return a.Links(p.Selector)
	case "form":
		var p struct {
			Selector string `json:"selector"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		return a.FormFields(p.Selector)
	case "fill":
		var p struct {
			Selector string            `json:"selector"`
			Value    *string           `json:"value"`
			Fields   map[string]string `json:"fields"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		fields := p.Fields
		if p.Selector != "" {
			if len(p.Fields) > 0 {
				return "", errors.New("fill takes selector/value or fields, not both")
			}
			if p.Value == nil {
				return "", errors.New("fill selector needs a value")
			}
			fields = map[string]string{p.Selector: *p.Value}
		}
		if len(fields) == 0 {
			return "", errors.New("fill needs selector/value or fields")
		}
		count, err := a.Fill(fields)
		if err != nil {
			return "", err
		}
		if err := a.waitPendingLocked(ctx); err != nil {
			return "", err
		}
		return fmt.Sprintf("filled %d field(s)", count), nil
	case "select":
		var p struct {
			Selector string   `json:"selector"`
			Value    *string  `json:"value"`
			Values   []string `json:"values"`
			Text     string   `json:"text"`
			Index    *int     `json:"index"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		if p.Selector == "" {
			return "", errors.New("select needs a selector")
		}
		msg, err := a.Select(p.Selector, p.Value, p.Values, p.Text, p.Index)
		if err != nil {
			return "", err
		}
		if err := a.waitPendingLocked(ctx); err != nil {
			return "", err
		}
		return msg, nil
	case "toggle":
		var p struct {
			Selector string `json:"selector"`
			Checked  *bool  `json:"checked"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		if p.Selector == "" {
			return "", errors.New("toggle needs a selector")
		}
		msg, err := a.Toggle(p.Selector, p.Checked)
		if err != nil {
			return "", err
		}
		if err := a.waitPendingLocked(ctx); err != nil {
			return "", err
		}
		return msg, nil
	case "submit":
		var p struct {
			Selector string `json:"selector"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		if err := a.Submit(ctx, p.Selector); err != nil {
			return "", err
		}
		return a.Snapshot(), nil
	case "type":
		var p struct {
			Text string `json:"text"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		if err := a.Type(ctx, p.Text); err != nil {
			return "", err
		}
		return "typed", nil
	case "press":
		var p struct {
			Key string `json:"key"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		if err := a.Press(ctx, p.Key); err != nil {
			return "", err
		}
		return "pressed " + p.Key, nil
	case "scroll":
		var p struct {
			Lines  int  `json:"lines"`
			Pages  int  `json:"pages"`
			Top    bool `json:"top"`
			Bottom bool `json:"bottom"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		if p.Lines == 0 && p.Pages == 0 && !p.Top && !p.Bottom {
			p.Lines = 1
		}
		a.Scroll(p.Lines, p.Pages, p.Top, p.Bottom)
		return fmt.Sprintf("scrolled (scroll %d%%)", a.b.scrollPercent()), nil
	case "back":
		if err := a.Back(ctx); err != nil {
			return "", err
		}
		return a.Snapshot(), nil
	case "forward":
		if err := a.Forward(ctx); err != nil {
			return "", err
		}
		return a.Snapshot(), nil
	case "reload":
		if err := a.Reload(ctx); err != nil {
			return "", err
		}
		return a.Snapshot(), nil
	case "evaluate":
		var p struct {
			Expression string `json:"expression"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		v, err := a.Evaluate(ctx, p.Expression)
		if err != nil {
			return "", err
		}
		if v == "" {
			return "undefined", nil
		}
		return v, nil
	case "history":
		h := a.History()
		if h == "" {
			return "(no history)", nil
		}
		return h, nil
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

func decodeArgs(raw json.RawMessage, dst any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

// History lists session history, most recent first, with the current entry
// marked by a leading "*".
func (a *Automation) History() string {
	if a == nil || a.b == nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	urls, idx := a.b.historySnapshot()
	var sb strings.Builder
	for i := len(urls) - 1; i >= 0; i-- {
		mark := "  "
		if i == idx {
			mark = "* "
		}
		fmt.Fprintf(&sb, "%s%s\n", mark, urls[i])
	}
	return strings.TrimRight(sb.String(), "\n")
}
