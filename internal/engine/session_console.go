package engine

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/grafana/sobek"

	"browser/internal/linehistory"
	"browser/internal/protocol"
	"browser/internal/textwidth"
)

// consoleState is the JavaScript console and its per-pane scroll positions.
type consoleState struct {
	consoleOpen   bool
	consoleScroll int
	reqScroll     int
	storageScroll int
	htmlScroll    int
	toolScroll    int
	toolLog       []string
	consolePane   int // 0 = JS logs, 1 = requests, 2 = storage, 3 = HTML, 4 = tools
	consoleText   string
	consoleCursor int
	consoleEdit   bool // the input row inside the logs or tools section has focus
	consoleHist   linehistory.History
	toolHist      linehistory.History

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

// consoleHistoryFor selects the per-tab history used by the active input row.
func (b *Session) consoleHistoryFor(pane int) *linehistory.History {
	if pane == consoleTools {
		return &b.toolHist
	}
	return &b.consoleHist
}

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

func (b *Session) toggleConsole() {
	b.consoleOpen = !b.consoleOpen
	if !b.consoleOpen && b.FindScope == findScopeHTML {
		b.clearFind()
	}
	b.consoleScroll = 0
	b.reqScroll = 0
	b.storageScroll = 0
	b.htmlScroll = 0
	b.toolScroll = 0
	b.consolePane = consoleLogs
	b.consoleEdit = false
	b.consoleHist.Reset()
	b.toolHist.Reset()
	if b.consoleOpen {
		b.jsErr = false
	}
	b.draw()
}

// closeConsole closes the console like Escape does, leaving the usual exit hint.
func (b *Session) closeConsole() {
	if !b.consoleOpen {
		return
	}
	b.status = exitHint
	b.toggleConsole()
}

func (b *Session) applyConsoleAction(v protocol.ConsoleAction) {
	if !b.consoleOpen {
		return
	}
	switch v.Op {
	case protocol.ConsolePane:
		b.selectConsolePane(v.Pane)
		b.consoleEdit = false
	case protocol.ConsoleCycle:
		b.consoleCyclePane(v.Delta)
	case protocol.ConsoleScroll:
		b.scrollConsolePane(b.consolePane, v.Delta)
	case protocol.ConsoleTop:
		b.scrollConsoleToEnd(b.consolePane, true)
	case protocol.ConsoleBottom:
		b.scrollConsoleToEnd(b.consolePane, false)
	case protocol.ConsoleFocus:
		b.focusConsoleInput()
	case protocol.ConsoleCancelEdit:
		b.consoleEdit = false
	case protocol.ConsoleInput:
		b.consoleText = v.Text
		b.runConsoleInput()
		b.consoleEdit = true
	case protocol.ConsoleCopy:
		b.copyConsole()
	case protocol.ConsoleQuit:
		b.quitPending = true
		b.status = quitConfirmText
	case protocol.ConsoleCancelQuit:
		b.quitPending = false
		b.status = ""
	case protocol.ConsoleClose:
		b.closeConsole()
	}
}

// consoleCopyText returns the full text of the active console section. The
// HTML section copies its raw source rather than the display-wrapped lines.
func (b *Session) consoleCopyText() string {
	switch b.consolePane {
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
func (b *Session) copyConsole() {
	b.copyText(strings.TrimRight(b.consoleCopyText(), "\n"))
}

// consoleVisibleRows returns the rows the pane actually paints, matching
// emitConsoleView: the logs and tools panes reserve their input row, and the
// requests pane reserves its summary row.
func (b *Session) consoleVisibleRows(pane int) int {
	l := newConsoleLayout(b.Height, pane)
	rows := l.bodyRows
	if rows <= 0 {
		return 0
	}
	switch pane {
	case consoleLogs, consoleTools:
		if l.inputY >= 0 {
			rows = l.inputY - l.bodyY
		}
	case consoleRequests:
		if rows > 1 {
			rows--
		}
	}
	if rows < 0 {
		return 0
	}
	return rows
}

// consoleMaxScroll is the scroll value that shows the first line of a pane.
// paneStart subtracts both the visible rows and the scroll, so setting the
// scroll to the raw line count would leave the first screenful of j presses
// clamped against the top.
func (b *Session) consoleMaxScroll(pane int) int {
	rows := b.consoleVisibleRows(pane)
	if rows < 1 {
		return 0
	}
	total := len(b.consolePaneLines(pane, b.Width-2))
	if total <= rows {
		return 0
	}
	return total - rows
}

// scrollConsolePane scrolls the open console section.
func (b *Session) scrollConsolePane(pane, delta int) {
	p := b.consoleScrollFor(pane)
	if p == nil || delta == 0 {
		return
	}
	*p += delta
	if *p < 0 {
		*p = 0
	}
	if max := b.consoleMaxScroll(pane); *p > max {
		*p = max
	}
}

func (b *Session) consoleScrollFor(pane int) *int {
	switch pane {
	case consoleRequests:
		return &b.reqScroll
	case consoleStorage:
		return &b.storageScroll
	case consoleHTML:
		return &b.htmlScroll
	case consoleTools:
		return &b.toolScroll
	case consoleLogs:
		return &b.consoleScroll
	}
	return nil
}

func (b *Session) scrollConsoleToEnd(pane int, end bool) {
	p := b.consoleScrollFor(pane)
	if p == nil {
		return
	}
	if end {
		*p = b.consoleMaxScroll(pane)
	} else {
		*p = 0
	}
}

func (b *Session) focusConsoleInput() {
	if b.consolePane != consoleTools {
		b.consolePane = consoleLogs
	}
	b.consoleEdit = true
	b.consoleCursor = len(b.consoleText)
}

// selectConsolePane opens a console section, expanding the HTML section at its
// first line rather than at the end of the source.
func (b *Session) selectConsolePane(pane int) {
	if pane < 0 || pane >= consoleSectionCount {
		pane = consoleLogs
	}
	if pane == consoleHTML && b.consolePane != consoleHTML {
		b.htmlScroll = b.consoleMaxScroll(consoleHTML)
	}
	if b.FindActive && b.FindScope == findScopeHTML && pane != consoleHTML {
		b.FindActive = false
	}
	if pane == consoleTools && len(b.toolLog) == 0 {
		for _, line := range toolsHelpLines() {
			b.appendToolLine(line)
		}
		b.toolScroll = b.consoleMaxScroll(consoleTools)
	}
	b.consolePane = pane
}

// consoleCyclePane moves the open section through logs, requests, storage,
// HTML and tools. Only the selected section stays expanded; the input row
// belongs to the logs and tools sections, so cycling away also ends input
// editing.
func (b *Session) consoleCyclePane(dir int) {
	if b.consolePane < 0 || b.consolePane >= consoleSectionCount {
		b.consolePane = consoleLogs
	}
	b.selectConsolePane((b.consolePane + dir + consoleSectionCount) % consoleSectionCount)
	b.consoleEdit = false
}

// handleConsoleInputKey edits the one-line JavaScript input row.
// handleConsoleInputKey edits the one-line JavaScript input row.
func (b *Session) runConsoleInput() {
	src := strings.TrimSpace(b.consoleText)
	if src == "" {
		return
	}
	b.consoleHistoryFor(b.consolePane).Record(src)
	b.consoleText = ""
	b.consoleCursor = 0
	if b.consolePane == consoleTools {
		b.runToolInput(src)
		return
	}
	if isClearCommand(src) {
		b.clearJSLog()
		b.jsErr = false
		b.consoleScroll = 0
		b.consolePane = consoleLogs
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
	b.consolePane = consoleLogs
	b.consoleScroll = 0
}

// resetConsole clears the JS log and input for a newly loaded page. It is
// called before the new page's scripts run so stale errors cannot mix with the
// fresh document.
func (b *Session) resetConsole() {
	b.clearJSLog()
	b.jsErr = false
	b.consoleText = ""
	b.consoleCursor = 0
	b.consoleScroll = 0
	b.reqScroll = 0
	b.storageScroll = 0
	b.htmlScroll = 0
	b.consolePane = consoleLogs
	b.consoleEdit = false
	b.consoleHist.Reset()
	b.toolHist.Reset()
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

// consolePaneLines returns the lines rendered for a console section.
func (b *Session) consolePaneLines(pane, width int) []string {
	switch pane {
	case consoleRequests:
		return b.requestLinesWidth(width)
	case consoleStorage:
		return b.storageLinesWidth(width)
	case consoleHTML:
		return b.htmlLines(width)
	case consoleTools:
		return b.toolLines(width)
	default:
		return b.consoleLines(width)
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
	b.toolScroll = 0
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
		b.consolePane = consoleTools
		return
	}
	b.appendToolLine("[tool] " + src)
	b.runToolCommand(src)
	b.toolScroll = 0
	b.consolePane = consoleTools
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

// consoleLayout describes the console menu row and the active section body.
type consoleLayout struct {
	tabsY    int
	bodyY    int // -1 when there is no body
	bodyRows int
	inputY   int // -1 unless the logs section is open with an input row
	open     int
	footerY  int // -1 when the footer row does not fit
}

func newConsoleLayout(h, open int) consoleLayout {
	l := consoleLayout{tabsY: -1, bodyY: -1, inputY: -1, open: open, footerY: -1}
	if open < 0 || open >= consoleSectionCount {
		open = consoleLogs
		l.open = open
	}
	if h <= 0 {
		return l
	}
	if h >= 2 {
		l.footerY = h - 1
	}
	avail := h
	if l.footerY >= 0 {
		avail = h - 1
	}
	if avail <= 0 {
		return l
	}
	l.tabsY = 0
	l.bodyY = 1
	l.bodyRows = avail - 1
	if l.bodyRows < 0 {
		l.bodyRows = 0
	}
	if (open == consoleLogs || open == consoleTools) && l.bodyRows > 0 {
		l.inputY = l.bodyY + l.bodyRows - 1
	}
	return l
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

func paneStart(total, scroll, rows int) int {
	if rows < 1 {
		return 0
	}
	start := total - rows - scroll
	if start < 0 {
		start = 0
	}
	if start > total-rows {
		start = total - rows
	}
	if start < 0 {
		start = 0
	}
	return start
}

func visibleConsoleLines(items []string, scroll, rows, width int) []string {
	if rows < 1 {
		return nil
	}
	start := paneStart(len(items), scroll, rows)
	out := make([]string, 0, rows)
	for i := 0; i < rows && start+i < len(items); i++ {
		out = append(out, textwidth.Truncate(items[start+i], width))
	}
	return out
}

// emitConsoleView sends the text model of the console to the front end.
func (b *Session) emitConsoleView() {
	w, h := b.Width, b.Height
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	l := newConsoleLayout(h, b.consolePane)
	titles := b.consoleTabTitles()
	v := protocol.ConsoleView{
		Width: w, Height: h, Pane: b.consolePane,
		Tabs: append([]string(nil), titles[:]...),
	}
	if l.bodyY >= 0 && l.bodyRows > 0 {
		switch b.consolePane {
		case consoleRequests:
			reqRows := l.bodyRows
			if reqRows > 1 {
				reqRows--
			}
			v.Lines = visibleConsoleLines(b.requestLinesWidth(w-2), b.reqScroll, reqRows, w-2)
			v.Summary = textwidth.Truncate(b.requestCacheSummary(), w-2)
		case consoleStorage:
			v.Lines = visibleConsoleLines(b.storageLinesWidth(w-2), b.storageScroll, l.bodyRows, w-2)
		case consoleHTML:
			v.Lines = visibleConsoleLines(b.htmlLines(w-2), b.htmlScroll, l.bodyRows, w-2)
		case consoleLogs, consoleTools:
			logRows := l.bodyRows
			if l.inputY >= 0 {
				logRows = l.inputY - l.bodyY
			}
			if b.consolePane == consoleTools {
				v.Lines = visibleConsoleLines(b.toolLines(w-2), b.toolScroll, logRows, w-2)
			} else {
				v.Lines = visibleConsoleLines(b.consoleLines(w-2), b.consoleScroll, logRows, w-2)
			}
			v.Input = b.consoleText
			v.Cursor = b.consoleCursor
			v.Edit = b.consoleEdit
			if b.consoleText == "" && !b.consoleEdit {
				v.Hint = "JavaScript input"
				if b.consolePane == consoleTools {
					v.Hint = "Tools: help · form · links · requests · logs · source · snapshot"
				}
			}
		}
	}
	if h >= 2 {
		v.Footer = " Click/Tab/h/l: switch section · : focus input · j/k: scroll · g/G: ends · y: copy all · q: quit · Esc: close "
		if b.quitPending {
			v.Footer = " " + quitConfirmText
			v.Quit = true
		}
	}
	if b.FindScope == findScopeHTML && b.FindText != "" {
		v.FindText = b.FindText
		v.FindCursor = b.FindCursor
		v.FindCurrent = b.FindIdx + 1
		if v.FindCurrent < 1 {
			v.FindCurrent = 1
		}
		v.FindTotal = len(b.FindMatches)
		lines := b.htmlLines(w - 2)
		start := paneStart(len(lines), b.htmlScroll, l.bodyRows)
		for i, m := range b.FindMatches {
			rects := m.Rects
			if len(rects) == 0 {
				rects = []findRect{{X: m.X, Line: m.Line, W: m.W}}
			}
			for _, r := range rects {
				rel := r.Line - start
				if rel < 0 || rel >= l.bodyRows {
					continue
				}
				v.Highlights = append(v.Highlights, protocol.ConsoleHighlight{
					Line: rel, X: 1 + r.X, W: r.W, Current: i == b.FindIdx,
				})
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
