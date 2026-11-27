package engine

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/grafana/sobek"

	"browser/internal/protocol"
	"browser/internal/textwidth"
)

// consoleWindow is the latest visible-window request from the front end. It is
// a request cache, not UI state: the front end owns the pane, scroll, input and
// history, and the engine only slices the section it asks for.
type consoleWindow struct {
	pane  int
	start int
	rows  int
	width int
	valid bool
}

// consoleState is the JavaScript console data: the per-page log, the Tools
// scrollback and the last requested window.
type consoleState struct {
	consoleOpen bool
	window      consoleWindow
	toolLog     []string

	htmlPage   *Page
	htmlWidth  int
	htmlCache  []string
	jsLog      []string
	jsLogBytes int
}

// JS console bounds. Each page-controlled line and the whole log are capped in
// bytes, not just line count, so a page cannot exhaust the process through the
// console.
const (
	maxJSLogLine  = 8 << 10
	maxJSLogBytes = 512 << 10
	jsLogCap      = 200
	// maxPageDialogs bounds interactive alert/confirm/prompt dialogs per page
	// load so a script cannot reopen them indefinitely.
	maxPageDialogs = 20
	// maxDialogBytes bounds one dialog message or prompt default.
	maxDialogBytes = 8 << 10
)

// truncateLogBytes returns a bounded, rune-safe prefix with a marker.
func truncateLogBytes(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	cut := limit - len("…")
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// appendJSLog adds one line and evicts from the front until both the line and
// byte bounds hold.
func (b *Session) appendJSLog(entry string) {
	b.jsLog = append(b.jsLog, entry)
	b.jsLogBytes += len(entry)
	for len(b.jsLog) > jsLogCap || (b.jsLogBytes > maxJSLogBytes && len(b.jsLog) > 1) {
		b.jsLogBytes -= len(b.jsLog[0])
		b.jsLog = b.jsLog[1:]
	}
}

// clearJSLog drops the log and its byte account.
func (b *Session) clearJSLog() {
	b.jsLog = nil
	b.jsLogBytes = 0
}

// modalState is the JS dialog, select popup and download/notice state.
type modalState struct {
	dialog           *jsDialog
	nextDialogID     uint32
	nextSelectID     uint32
	selectFrontend   bool
	selectID         uint32
	downloadFrontend bool
	noticeFrontend   bool
	hintsFrontend    bool
	// download holds the modal curl hint shown for download links and
	// non-renderable responses.
	download *downloadDialog
	// notice holds a small informational modal, such as the unsupported
	// file-upload message.
	notice *noticeDialog
	// selectMenu is the pending single-select choice awaiting the front end.
	selectMenu *selectMenuDialog
}

// Console sections, ordered as they are stacked in the console view.
const (
	consoleLogs = iota
	consoleRequests
	consoleStorage
	consoleHTML
	consoleTools
	consoleSectionCount
)

// toolLogCap bounds the Tools tab scrollback; the newest lines are kept.
const toolLogCap = 500

// logEngineError records a contained engine failure for the console view. It
// sanitizes the message, keeps duplicate consecutive reports out of the log,
// and points the footer at the console so the failure is discoverable.
func (b *Session) logEngineError(msg string) {
	msg = StripTerminalControls(msg)
	if msg == "" {
		return
	}
	entry := "[engine] " + msg
	if n := len(b.jsLog); n > 0 && b.jsLog[n-1] == entry {
		return
	}
	b.logJS("engine", msg)
	b.status = "Error: page engine — see ! console"
}

// logJS records a JavaScript console/error message for the console view.
func (b *Session) logJS(kind, msg string) {
	msg = strings.TrimRight(msg, "\n")
	if msg == "" {
		return
	}
	msg = truncateLogBytes(msg, maxJSLogLine)
	b.appendJSLog("[" + kind + "] " + msg)
	if kind == "error" {
		b.jsErr = true
		if strings.HasPrefix(b.status, "JS:") {
			b.status = ""
		}
	}
}

func (b *Session) openConsole() {
	if b.consoleOpen {
		return
	}
	b.consoleOpen = true
	b.jsErr = false
	b.window = consoleWindow{}
}

// closeConsole closes the console like Escape does, leaving the usual exit hint.
func (b *Session) closeConsole() {
	if !b.consoleOpen {
		return
	}
	b.consoleOpen = false
	b.window = consoleWindow{}
	if b.FindScope == findScopeHTML {
		b.clearFind()
	}
	b.status = exitHint
	b.draw()
}

func (b *Session) applyConsoleAction(v protocol.ConsoleAction) {
	switch v.Op {
	case protocol.ConsoleOpen:
		b.openConsole()
		return
	case protocol.ConsoleClose:
		b.closeConsole()
		return
	}
	if !b.consoleOpen {
		return
	}
	switch v.Op {
	case protocol.ConsoleWindow:
		b.applyConsoleWindow(v)
	case protocol.ConsoleInput:
		b.runConsoleInput(v.Pane, v.Text)
	case protocol.ConsoleCopy:
		b.copyConsole(v.Pane)
	}
}

// applyConsoleWindow records the front-end window request and runs the
// first-entry side effects the front end cannot perform itself. It never stores
// a pane or scroll position beyond this request.
func (b *Session) applyConsoleWindow(v protocol.ConsoleAction) {
	pane := v.Pane
	if pane < 0 || pane >= consoleSectionCount {
		pane = consoleLogs
	}
	if v.Enter {
		if pane == consoleTools && len(b.toolLog) == 0 {
			for _, line := range toolsHelpLines() {
				b.appendToolLine(line)
			}
		}
		if pane != consoleHTML && b.FindScope == findScopeHTML {
			b.clearFind()
		}
	}
	rows := v.Rows
	if rows < 0 {
		rows = 0
	}
	width := v.Width
	if width < 1 {
		width = 1
	}
	b.window = consoleWindow{pane: pane, start: v.Start, rows: rows, width: width, valid: true}
}

// consoleCopyText returns the full text of one console section. The HTML
// section copies its raw source rather than the display-wrapped lines.
func (b *Session) consoleCopyText(pane int) string {
	switch pane {
	case consoleRequests:
		return strings.Join(b.requestLines(), "\n")
	case consoleStorage:
		return strings.Join(b.storageLines(), "\n")
	case consoleHTML:
		if b.Page != nil {
			return b.Page.RawHTML
		}
	case consoleTools:
		return strings.Join(b.toolLog, "\n")
	}
	return strings.Join(b.jsLog, "\n")
}

// copyConsole copies the whole active console section. Terminal text
// selection needs Shift+drag, so y gives one key that copies all of the log,
// the HTML source or the other section bodies.
func (b *Session) copyConsole(pane int) {
	b.copyText(strings.TrimRight(b.consoleCopyText(pane), "\n"))
}

// runConsoleInput evaluates one line typed in the JavaScript or Tools input
// row. The front end owns the input text, caret and history; the engine only
// runs the command and appends its output.
func (b *Session) runConsoleInput(pane int, src string) {
	src = strings.TrimSpace(src)
	if src == "" {
		return
	}
	if pane == consoleTools {
		b.runToolInput(src)
		return
	}
	if isClearCommand(src) {
		b.clearJSLog()
		b.jsErr = false
		return
	}
	b.logJS("input", src)
	if b.JS == nil {
		b.logJS("error", "JavaScript is disabled — press J to enable")
		return
	}
	v, err := b.JS.RunCode("console", src)
	if err != nil {
		b.logJS("error", err.Error())
	} else if v != nil && !sobek.IsUndefined(v) {
		s := v.String()
		if r := []rune(s); len(r) > 2000 {
			s = string(r[:2000]) + "…"
		}
		b.logJS("result", s)
	}
	if b.JS.ConsumeDirty() {
		b.rebuild()
	}
	b.consumePendingJS()
}

// resetConsole clears the JS log for a newly loaded page. It is called before
// the new page's scripts run so stale errors cannot mix with the fresh
// document. The front end keeps its pane, scroll and input state.
func (b *Session) resetConsole() {
	b.clearJSLog()
	b.jsErr = false
	b.clearFind()
}

func (b *Session) consoleLines(width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, entry := range b.jsLog {
		for _, raw := range strings.Split(entry, "\n") {
			raw = StripTerminalControls(ExpandTabs(strings.TrimRight(raw, "\r")))
			if raw == "" {
				out = append(out, "")
				continue
			}
			out = append(out, textwidth.Wrap(raw, width)...)
		}
	}
	if len(out) == 0 {
		out = append(out, "(no JavaScript messages)")
	}
	return out
}

func (b *Session) requestLines() []string {
	return b.requestLinesWidth(0)
}

// requestLinesWidth formats the request log, wrapping long URLs to width cells.
func (b *Session) requestLinesWidth(width int) []string {
	if b.loader == nil {
		return []string{"(requests unavailable)"}
	}
	records := b.loader.RequestSnapshot()
	out := make([]string, 0, len(records))
	for _, r := range records {
		status := "..."
		size := "-"
		if r.Done {
			status = fmt.Sprintf("%d", r.Status)
			size = humanSize(r.Size)
		}
		if r.Cached {
			size = "cached"
		}
		if r.Err != "" {
			status = "ERR"
		}
		kind := r.Kind
		if kind == "" {
			kind = "req"
		}
		line := fmt.Sprintf("%3s %-5s %6s %-5s %s", status, r.Method, size, kind, StripTerminalControls(r.URL))
		if r.Err != "" {
			line += "  " + StripTerminalControls(r.Err)
		}
		out = appendWrappedHanging(out, line, width)
	}
	if len(out) == 0 {
		out = append(out, "(no requests for this page)")
	}
	return out
}

// requestCacheSummary is the pinned cache usage line shown below the request
// list.
func (b *Session) requestCacheSummary() string {
	if b.loader == nil || b.loader.Host == nil {
		return "Cache: unavailable"
	}
	used, limit, entries := b.loader.Host.CacheStats()
	if limit <= 0 {
		return fmt.Sprintf("Cache: disabled · %d entries", entries)
	}
	return fmt.Sprintf("Cache: %s / %s · %d entries", humanSize(int(used)), humanSize(int(limit)), entries)
}

func humanSize(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fK", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/(1024*1024))
	}
}

// toolLines returns the Tools tab scrollback wrapped to width.
func (b *Session) toolLines(width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, entry := range b.toolLog {
		if entry == "" {
			out = append(out, "")
			continue
		}
		out = append(out, textwidth.WrapHanging(entry, width, 4)...)
	}
	return out
}

// appendToolLine adds one line to the Tools tab scrollback, keeping the newest
// lines so a large result can never block later output.
func (b *Session) appendToolLine(line string) {
	b.toolLog = append(b.toolLog, sanitizeToolLine(line))
	b.trimToolLog()
}

func (b *Session) appendToolLines(text string) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > toolLogCap {
		// Keep the beginning of one very large result (for example source),
		// where the useful markup is, and mark it instead of flooding the tab.
		kept := append([]string(nil), lines[:toolLogCap-1]...)
		kept = append(kept, "… (result truncated; use a selector or smaller limit)")
		lines = kept
	}
	for _, line := range lines {
		b.toolLog = append(b.toolLog, sanitizeToolLine(strings.TrimRight(line, "\r")))
	}
	b.trimToolLog()
}

func (b *Session) trimToolLog() {
	if len(b.toolLog) > toolLogCap {
		b.toolLog = append([]string(nil), b.toolLog[len(b.toolLog)-toolLogCap:]...)
	}
}

func sanitizeToolLine(line string) string {
	line = StripTerminalControls(line)
	if r := []rune(line); len(r) > 4096 {
		line = string(r[:4096]) + "…"
	}
	return line
}

// clearToolLog empties the Tools tab scrollback.
func (b *Session) clearToolLog() {
	b.toolLog = nil
}

// isClearCommand reports the console shortcut that empties the active log.
func isClearCommand(s string) bool {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, ":")
	return strings.EqualFold(s, "clear") || strings.EqualFold(s, "cls")
}

// runToolInput interprets one command typed in the Tools tab.
func (b *Session) runToolInput(src string) {
	if isClearCommand(src) {
		b.clearToolLog()
		return
	}
	b.appendToolLine("[tool] " + src)
	b.runToolCommand(src)
}

// runToolCommand executes the read-only command surface that is safe inside
// the interactive event loop: it never starts or waits for a navigation.
func (b *Session) runToolCommand(src string) {
	fields := strings.Fields(src)
	if len(fields) == 0 {
		return
	}
	cmd := strings.ToLower(fields[0])
	arg := strings.TrimSpace(src[len(fields[0]):])
	a := &Automation{b: b}
	switch cmd {
	case "help", "?":
		for _, line := range toolsHelpLines() {
			b.appendToolLine(line)
		}
	case "form":
		out, err := a.FormFields(arg)
		b.appendToolResult(out, err)
	case "links":
		out, err := a.Links(arg)
		b.appendToolResult(out, err)
	case "source":
		out, err := a.Source(arg)
		b.appendToolResult(out, err)
	case "snapshot":
		b.appendToolLines(a.Snapshot())
	case "history":
		out := a.History()
		if out == "" {
			out = "(no history)"
		}
		b.appendToolLines(out)
	case "requests":
		limit, err := toolLimit(arg, 100)
		if err != nil {
			b.appendToolLine("[error] " + err.Error())
			return
		}
		b.appendToolLines(a.Requests(limit))
	case "logs":
		limit, err := toolLimit(arg, 200)
		if err != nil {
			b.appendToolLine("[error] " + err.Error())
			return
		}
		b.appendToolLines(a.Logs(limit))
	default:
		b.appendToolLine("[error] unknown tool " + strconv.Quote(cmd) + " (type help)")
	}
}

func (b *Session) appendToolResult(out string, err error) {
	if err != nil {
		b.appendToolLine("[error] " + err.Error())
		return
	}
	if strings.TrimSpace(out) == "" {
		b.appendToolLine("(empty result)")
		return
	}
	b.appendToolLines(out)
}

func toolLimit(arg string, def int) (int, error) {
	if arg == "" {
		return def, nil
	}
	n, err := strconv.Atoi(arg)
	if err != nil || n < 0 {
		return 0, errors.New("limit must be a non-negative number")
	}
	return n, nil
}

// toolsHelpLines is the Tools tab reference. It stays short enough to read on
// an 80-column terminal.
func toolsHelpLines() []string {
	return []string{
		"Tools: read-only inspection of the current page",
		"  help                 this list",
		"  form [selector]      forms and controls with selectors",
		"  links [selector]     every link with index and href",
		"  requests [limit]     request log and cache usage",
		"  logs [limit]         JavaScript messages and errors",
		"  source [selector]    raw HTML or one element",
		"  snapshot             viewport text, links and inputs",
		"  history              session history",
		"  clear                clear this scrollback (also cls)",
		"",
		"[selector] is an optional CSS selector:",
		"  #content  .card  form#search  input[name=q]  main a",
		"Omit it for all forms/links or the full page source.",
		"Examples:",
		"  form form#search     controls inside that form",
		"  links main a         links inside <main>",
		"  source #content      serialized HTML of that element",
		"  form input[name=q]   form containing that field",
		"",
		"Type a command above and press Enter.",
		"Up/Down or Ctrl-P/Ctrl-N recalls commands.",
		"Navigation, form editing, waiting and screenshots run in the",
		"headless MCP server: mob3 -mcp.",
	}
}

// storageLines summarises the current page's cookies, localStorage and
// sessionStorage. Everything is in memory only. A non-positive width keeps each
// entry on one line for copying.
func (b *Session) storageLines() []string {
	return b.storageLinesWidth(0)
}

func (b *Session) storageLinesWidth(width int) []string {
	out := []string{"Cookies"}
	if b.URL == nil || b.loader == nil || (b.URL.Scheme != "http" && b.URL.Scheme != "https") {
		out = append(out, "  (not available for this page)")
	} else {
		cookies := b.loader.Host.CookieViews(b.URL)
		sort.Slice(cookies, func(i, j int) bool {
			if cookies[i].Name != cookies[j].Name {
				return cookies[i].Name < cookies[j].Name
			}
			if cookies[i].Domain != cookies[j].Domain {
				return cookies[i].Domain < cookies[j].Domain
			}
			return cookies[i].Path < cookies[j].Path
		})
		if len(cookies) == 0 {
			out = append(out, "  (none)")
		}
		for _, c := range cookies {
			line := "  " + sanitizeStorageText(c.Name) + "=" + sanitizeStorageText(c.Value)
			if c.HttpOnly {
				line += "  [HttpOnly]"
			}
			if c.Secure {
				line += "  [Secure]"
			}
			switch c.SameSite {
			case "lax":
				line += "  [SameSite=Lax]"
			case "strict":
				line += "  [SameSite=Strict]"
			case "none":
				line += "  [SameSite=None]"
			}
			line += "  " + sanitizeStorageText(c.Domain) + "  " + sanitizeStorageText(c.Path)
			if c.Expires.IsZero() {
				line += "  session"
			} else {
				line += "  expires " + c.Expires.UTC().Format("2006-01-02")
			}
			out = appendStorageWrapped(out, line, width)
		}
	}
	var local, session map[string]string
	if b.JS != nil {
		local = b.JS.StorageLocalData()
		session = b.JS.StorageSessionData()
	}
	out = append(out, "")
	out = appendStorageSection(out, "localStorage", local, b.JS != nil, width)
	out = append(out, "")
	out = appendStorageSection(out, "sessionStorage", session, b.JS != nil, width)
	return out
}

// appendStorageWrapped appends a preformatted line, hard-wrapping it to width
// cells with a hanging indent when width is positive.
func appendStorageWrapped(out []string, line string, width int) []string {
	return appendWrappedHanging(out, line, width)
}

// appendWrappedHanging appends line wrapped to width with a four-cell hanging
// indent. A non-positive width keeps it on one line for copying.
func appendWrappedHanging(out []string, line string, width int) []string {
	if width <= 0 {
		return append(out, line)
	}
	return append(out, textwidth.WrapHanging(line, width, 4)...)
}

// htmlLines returns the raw HTML source of the current page, wrapped to
// the console width. The result is cached per page and width because the source
// can be large and the console redraws periodically.
func (b *Session) htmlLines(width int) []string {
	if width < 1 {
		width = 1
	}
	src := ""
	if b.Page != nil {
		src = b.Page.RawHTML
	}
	if b.htmlPage == b.Page && b.htmlWidth == width && b.htmlCache != nil {
		return b.htmlCache
	}
	var out []string
	for _, raw := range strings.Split(src, "\n") {
		raw = StripTerminalControls(ExpandTabs(strings.TrimRight(raw, "\r")))
		if raw == "" {
			out = append(out, "")
			continue
		}
		out = append(out, textwidth.Wrap(raw, width)...)
	}
	if len(out) == 0 {
		out = append(out, "(no HTML source)")
	}
	b.htmlPage = b.Page
	b.htmlWidth = width
	b.htmlCache = out
	return out
}

func appendStorageSection(out []string, label string, data map[string]string, available bool, width int) []string {
	out = append(out, label)
	if !available {
		return append(out, "  (JavaScript disabled)")
	}
	if len(data) == 0 {
		return append(out, "  (empty)")
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = appendStorageEntry(out, k, data[k], width)
	}
	return out
}

// appendStorageEntry appends "  key = value", wrapping the value to width with
// a hanging indent so a long JSON value stays readable. A non-positive width
// keeps the entry on one line for copying.
func appendStorageEntry(out []string, key, value string, width int) []string {
	line := "  " + sanitizeStorageText(key) + " = " + sanitizeStorageText(value)
	return appendWrappedHanging(out, line, width)
}

// sanitizeStorageText keeps a single-cell line per value so one huge or
// multiline storage entry cannot disturb the console layout.
func sanitizeStorageText(s string) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	s = StripTerminalControls(s)
	if r := []rune(s); len(r) > 4096 {
		s = string(r[:4096]) + "…"
	}
	return s
}

// consoleTabTitles returns the top-menu labels for the console sections.
func (b *Session) consoleTabTitles() [consoleSectionCount]string {
	reqCount := 0
	if b.loader != nil {
		reqCount = b.loader.RequestCount()
	}
	return [consoleSectionCount]string{
		fmt.Sprintf(" JS logs (%d) ", len(b.jsLog)),
		fmt.Sprintf(" Requests (%d) ", reqCount),
		" Storage ",
		" HTML ",
		" Tools ",
	}
}

// clampConsoleStart resolves an absolute start line. A negative request means
// the bottom of the section; the result is the first line the window shows.
func clampConsoleStart(total, start, rows int) int {
	if rows < 1 {
		return 0
	}
	maxStart := total - rows
	if maxStart < 0 {
		maxStart = 0
	}
	if start < 0 {
		return maxStart
	}
	if start > maxStart {
		return maxStart
	}
	return start
}

// windowLines returns the requested slice, truncated to the content width.
func windowLines(items []string, start, rows, width int) []string {
	if rows < 1 || start >= len(items) {
		return nil
	}
	if start < 0 {
		start = 0
	}
	out := make([]string, 0, rows)
	for i := 0; i < rows && start+i < len(items); i++ {
		out = append(out, textwidth.Truncate(items[start+i], width))
	}
	return out
}

// emitConsoleView slices the requested console window and sends it to the
// front end. The engine keeps only the last request; the front end owns pane,
// scroll, input, history and quit state.
func (b *Session) emitConsoleView() {
	req := b.window
	if !req.valid || b.sessionOut == nil {
		return
	}
	w := req.width
	if w < 1 {
		w = 1
	}
	h := b.Height
	if h < 1 {
		h = 1
	}
	contentWidth := w - 2
	if contentWidth < 1 {
		contentWidth = 1
	}
	titles := b.consoleTabTitles()
	v := protocol.ConsoleView{
		Width: w, Height: h,
		Pane: req.pane,
		Tabs: append([]string(nil), titles[:]...),
	}
	rows := req.rows
	if rows < 0 {
		rows = 0
	}
	if rows > 0 {
		var lines []string
		switch req.pane {
		case consoleRequests:
			lines = b.requestLinesWidth(contentWidth)
			v.Summary = textwidth.Truncate(b.requestCacheSummary(), contentWidth)
		case consoleStorage:
			lines = b.storageLinesWidth(contentWidth)
		case consoleHTML:
			lines = b.htmlLines(contentWidth)
		case consoleTools:
			lines = b.toolLines(contentWidth)
		default:
			lines = b.consoleLines(contentWidth)
		}
		v.Total = len(lines)
		v.Start = clampConsoleStart(v.Total, req.start, rows)
		v.Lines = windowLines(lines, v.Start, rows, contentWidth)
	}
	if b.FindScope == findScopeHTML && b.FindText != "" {
		v.FindCurrent = b.FindIdx + 1
		if v.FindCurrent < 1 {
			v.FindCurrent = 1
		}
		v.FindTotal = len(b.FindMatches)
		if b.FindIdx >= 0 && b.FindIdx < len(b.FindMatches) {
			v.FindLine = b.FindMatches[b.FindIdx].Line
			v.FindLineValid = true
		}
		if rows > 0 && req.pane == consoleHTML {
			for i, m := range b.FindMatches {
				rects := m.Rects
				if len(rects) == 0 {
					rects = []findRect{{X: m.X, Line: m.Line, W: m.W}}
				}
				for _, r := range rects {
					rel := r.Line - v.Start
					if rel < 0 || rel >= rows {
						continue
					}
					v.Highlights = append(v.Highlights, protocol.ConsoleHighlight{
						Line: rel, X: 1 + r.X, W: r.W, Current: i == b.FindIdx,
					})
				}
			}
		}
	}
	_ = b.sessionOut.Send(protocol.Event{Body: v})
	b.frameReady = false
}

// noticeDialog is a small informational modal, such as the unsupported
// file-upload message.
type noticeDialog struct {
	title   string
	message string
}

// selectMenuDialog is the popup listing every option of a single select.
type selectMenuDialog struct {
	node    *Node
	options []*Node
}

// openSelectMenu shows all options of a single select with the current value
// highlighted.
func (b *Session) openSelectMenu(sel *Node) {
	opts := SelectOptions(sel)
	if len(opts) == 0 {
		return
	}
	idx := 0
	for i, opt := range opts {
		if opt.HasAttr("selected") {
			idx = i
			break
		}
	}
	b.selectMenu = &selectMenuDialog{node: sel, options: opts}
	b.status = ""
	if b.sessionOut != nil {
		b.nextSelectID++
		b.selectID = b.nextSelectID
		b.selectFrontend = true
		labels := make([]string, len(opts))
		for i, opt := range opts {
			labels[i] = OptionLabel(opt)
		}
		if err := b.sessionOut.Send(protocol.Event{Body: protocol.Select{
			ID: b.selectID, Options: labels, Selected: idx,
		}}); err != nil {
			b.selectMenu = nil
			b.selectFrontend = false
			b.selectID = 0
		}
	}
}

// commitSelectOption applies the chosen option, closes the popup and fires the
// same input/change events a native select would.
func (b *Session) commitSelectOption(opt *Node) {
	m := b.selectMenu
	if m == nil || opt == nil {
		return
	}
	for _, o := range m.options {
		RemoveAttr(o, "selected")
	}
	SetAttr(opt, "selected", "")
	sel := m.node
	b.selectMenu = nil
	b.status = ""
	b.fireSelectChange(sel)
}

// downloadDialog is the modal curl hint offered for download links and
// non-renderable responses.
type downloadDialog struct {
	url     string
	name    string
	command string
}

// jsDialog is a modal JavaScript alert/confirm/prompt.
type jsDialog struct {
	kind      string // alert, confirm, prompt
	message   string
	text      string
	cursor    int
	selectAll bool
}

// showAlert displays an alert dialog. Non-interactive sessions only log it.
func (b *Session) showAlert(msg string) {
	d := &jsDialog{kind: "alert", message: msg}
	b.runDialog(d)
}

// showConfirm returns the confirm result; the default is false when no
// interactive input is available.
func (b *Session) showConfirm(msg string) bool {
	d := &jsDialog{kind: "confirm", message: msg}
	_, ok := b.runDialog(d)
	return ok
}

// showPrompt returns the entered text and whether OK was chosen. Cancel
// returns false (JS null).
func (b *Session) showPrompt(msg, def string) (string, bool) {
	d := &jsDialog{kind: "prompt", message: msg, text: def, cursor: len(def), selectAll: true}
	return b.runDialog(d)
}

// runDialog hands an interactive dialog to the front end and blocks the JS
// caller until the answer arrives. Headless callers (automation, dump and
// direct tests) use the non-interactive defaults.
func (b *Session) runDialog(d *jsDialog) (string, bool) {
	d.message = truncateLogBytes(d.message, maxDialogBytes)
	if d.kind == "prompt" {
		d.text = truncateLogBytes(d.text, maxDialogBytes)
	}
	if b.sessionOut == nil || b.sessionIn == nil {
		return d.text, d.kind == "alert"
	}
	if b.Dialogs >= maxPageDialogs {
		// Suppress further dialogs with the non-interactive defaults, and say
		// so once, instead of letting a page reopen a modal forever.
		if !b.DialogsBlocked {
			b.DialogsBlocked = true
			b.logJS("warn", "Additional dialogs blocked for this page")
			b.status = "Dialogs blocked — too many for this page"
		}
		return d.text, d.kind == "alert"
	}
	b.Dialogs++
	return b.runTransportDialog(d)
}

// runTransportDialog hands a dialog to the front end and waits for its answer.
// The engine loop stays blocked in the JS caller; the front end owns the modal
// drawing and input.
func (b *Session) runTransportDialog(d *jsDialog) (string, bool) {
	// A dialog takes over from an open select popup; the front end cancels its
	// local copy when it starts the dialog.
	b.selectMenu = nil
	b.selectFrontend = false
	b.selectID = 0
	b.download = nil
	b.downloadFrontend = false
	b.notice = nil
	b.noticeFrontend = false
	b.clearLinkHints()
	kind := protocol.DialogAlert
	switch d.kind {
	case "confirm":
		kind = protocol.DialogConfirm
	case "prompt":
		kind = protocol.DialogPrompt
	}
	b.nextDialogID++
	id := b.nextDialogID
	b.dialog = d
	defer func() { b.dialog = nil }()
	// Kitty scaled text composites above the cell grid, and the frontend dims
	// the page behind the modal. Push a page frame without scaled text before
	// the dialog event so large glyphs and their backgrounds cannot survive
	// the dim layer. The engine loop is blocked here, so it cannot do this on
	// the next draw.
	if !b.NoDraw {
		b.draw()
		b.emitPendingFrame()
	}
	if err := b.sessionOut.Send(protocol.Event{Body: protocol.Dialog{
		ID: id, Kind: kind, Text: d.message, Default: d.text,
	}}); err != nil {
		return d.text, kind == protocol.DialogAlert
	}
	for {
		select {
		case <-b.sessionCtx.Done():
			return "", false
		case <-b.sessionAbort:
			// The front end answers via DialogResult; an abort only wakes us.
		case cmd, ok := <-b.sessionIn:
			if !ok {
				return "", false
			}
			switch v := cmd.Body.(type) {
			case protocol.DialogResult:
				if v.ID != id {
					continue
				}
				if v.OK {
					return v.Text, true
				}
				return "", false
			case protocol.ConsoleAction:
				// The front end can open or close the console while the dialog
				// blocks the engine loop. Apply those window changes now so the
				// two sides cannot disagree after the dialog returns. Console
				// input must wait: evaluating it here would re-enter JavaScript.
				switch v.Op {
				case protocol.ConsoleOpen, protocol.ConsoleClose, protocol.ConsoleWindow:
					b.applyConsoleAction(v)
				}
			case protocol.Resize:
				b.resize(v.Width, v.Height)
				if v.CellW > 0 {
					b.CellW = v.CellW
				}
				if v.CellH > 0 {
					b.CellH = v.CellH
				}
				b.applyImageCap()
			case protocol.Shutdown:
				b.sessionQuit = true
				return "", false
			}
		}
	}
}
