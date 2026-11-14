package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Automation drives one headless Browser for scripted clients. Commands are
// serialized; the browser never writes terminal output.
type Automation struct {
	mu sync.Mutex
	b  *Browser
}

// ClickTarget selects what to click. Exactly one of Link, Selector or
// HasCoords should be set. Coordinates are zero-based cells in the page
// viewport (row 0 is the first page row below the chrome).
type ClickTarget struct {
	Link      int
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
		return errors.New("click needs a link number, selector or coordinates")
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
		return fmt.Errorf("element %s is not visible", describeNode(n))
	}
	top, _ := b.viewport()
	x, y := f.X+1, top+f.Y-b.scroll+1
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: true})
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: false})
	return nil
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
			Selector string `json:"selector"`
			X        *int   `json:"x"`
			Y        *int   `json:"y"`
		}
		if err := decodeArgs(args, &p); err != nil {
			return "", err
		}
		target := ClickTarget{Link: p.Link, Selector: p.Selector}
		if p.X != nil || p.Y != nil {
			if p.X == nil || p.Y == nil {
				return "", errors.New("click x and y must be provided together")
			}
			target.X, target.Y, target.HasCoords = *p.X, *p.Y, true
		}
		if target.Link == 0 && target.Selector == "" && !target.HasCoords {
			return "", errors.New("click needs link, selector, or x/y")
		}
		if err := a.Click(ctx, target); err != nil {
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
