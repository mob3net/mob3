package browser

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/dop251/goja"
)

// Console sections, ordered as they are stacked in the console view.
const (
	consoleLogs = iota
	consoleRequests
	consoleStorage
	consoleHTML
	consoleSectionCount
)

// logJS records a JavaScript console/error message for the console view.
func (b *Browser) logJS(kind, msg string) {
	msg = strings.TrimRight(msg, "\n")
	if msg == "" {
		return
	}
	b.jsLog = append(b.jsLog, "["+kind+"] "+msg)
	if len(b.jsLog) > 200 {
		b.jsLog = b.jsLog[len(b.jsLog)-200:]
	}
	if kind == "error" {
		b.jsErr = true
		if strings.HasPrefix(b.status, "JS:") {
			b.status = ""
		}
	}
}

func setMouseReporting(on bool) {
	if on {
		_, _ = os.Stdout.WriteString("\x1b[?1000h\x1b[?1003h\x1b[?1006h")
	} else {
		_, _ = os.Stdout.WriteString("\x1b[?1003l\x1b[?1000l\x1b[?1006l")
	}
}

func (b *Browser) toggleConsole() {
	b.consoleOpen = !b.consoleOpen
	if !b.consoleOpen && b.findScope == findScopeHTML {
		b.clearFind()
	}
	b.consoleScroll = 0
	b.reqScroll = 0
	b.storageScroll = 0
	b.htmlScroll = 0
	b.consolePane = consoleLogs
	b.consoleEdit = false
	b.consoleHistAt = len(b.consoleHist)
	b.consoleDraft = ""
	b.prev = nil
	if b.consoleOpen {
		b.jsErr = false
	}
	// Keep mouse reporting on so the sections can be selected by clicking.
	// Terminal text selection needs the usual Shift-drag override.
	setMouseReporting(true)
	b.draw()
}

// closeConsole closes the console like Escape does, leaving the usual exit hint.
func (b *Browser) closeConsole() {
	if !b.consoleOpen {
		return
	}
	b.status = exitHint
	b.toggleConsole()
}

func (b *Browser) handleConsoleKey(k string) bool {
	if b.findActive {
		return b.handleFindKey(k)
	}
	if b.consoleEdit {
		return b.handleConsoleInputKey(k)
	}
	if k == "/" && b.consolePane == consoleHTML {
		b.openFind(findScopeHTML)
		return false
	}
	switch k {
	case "esc":
		b.toggleConsole()
		b.status = exitHint
	case "!":
		b.toggleConsole()
	case "q", "ctrl-c":
		return true
	case "tab":
		b.consoleCyclePane(1)
	case "backtab":
		b.consoleCyclePane(-1)
	case "i", "enter":
		b.focusConsoleInput()
	case "n":
		if b.findText != "" && b.findScope == findScopeHTML {
			b.findNext(1)
		}
	case "N":
		if b.findText != "" && b.findScope == findScopeHTML {
			b.findNext(-1)
		}
	case "k", "up":
		b.scrollConsolePane(b.consolePane, 1)
	case "j", "down":
		b.scrollConsolePane(b.consolePane, -1)
	case "g":
		b.scrollConsoleToEnd(b.consolePane, true)
	case "G":
		b.scrollConsoleToEnd(b.consolePane, false)
	}
	return false
}

// scrollConsolePane scrolls the open console section.
func (b *Browser) scrollConsolePane(pane, delta int) {
	p := b.consoleScrollFor(pane)
	if p == nil || delta == 0 {
		return
	}
	*p += delta
	if *p < 0 {
		*p = 0
	}
}

func (b *Browser) consoleScrollFor(pane int) *int {
	switch pane {
	case consoleRequests:
		return &b.reqScroll
	case consoleStorage:
		return &b.storageScroll
	case consoleHTML:
		return &b.htmlScroll
	case consoleLogs:
		return &b.consoleScroll
	}
	return nil
}

func (b *Browser) scrollConsoleToEnd(pane int, end bool) {
	p := b.consoleScrollFor(pane)
	if p == nil {
		return
	}
	if end {
		*p = len(b.consolePaneLines(pane, b.width-2))
	} else {
		*p = 0
	}
}

func (b *Browser) focusConsoleInput() {
	b.consolePane = consoleLogs
	b.consoleEdit = true
	b.consoleCursor = len(b.consoleText)
	b.consoleHistAt = len(b.consoleHist)
	b.consoleDraft = ""
}

// selectConsolePane opens a console section, expanding the HTML section at its
// first line rather than at the end of the source.
func (b *Browser) selectConsolePane(pane int) {
	if pane < 0 || pane >= consoleSectionCount {
		pane = consoleLogs
	}
	if pane == consoleHTML && b.consolePane != consoleHTML {
		b.htmlScroll = len(b.htmlLines(b.width - 2))
	}
	if b.findActive && b.findScope == findScopeHTML && pane != consoleHTML {
		b.findActive = false
	}
	b.consolePane = pane
}

// consoleCyclePane moves the open section through logs, requests, storage and
// HTML. Only the selected section stays expanded; the input row belongs to the
// logs section, so cycling away also ends input editing.
func (b *Browser) consoleCyclePane(dir int) {
	if b.consolePane < 0 || b.consolePane >= consoleSectionCount {
		b.consolePane = consoleLogs
	}
	b.selectConsolePane((b.consolePane + dir + consoleSectionCount) % consoleSectionCount)
	b.consoleEdit = false
}

// handleConsoleInputKey edits the one-line JavaScript input row.
func (b *Browser) handleConsoleInputKey(k string) bool {
	text := b.consoleText
	cur := b.consoleCursor
	if cur < 0 {
		cur = 0
	}
	if cur > len(text) {
		cur = len(text)
	}
	switch k {
	case "esc":
		b.toggleConsole()
		b.status = exitHint
		return false
	case "ctrl-c":
		return true
	case "tab":
		b.consoleCyclePane(1)
		return false
	case "backtab":
		b.consoleCyclePane(-1)
		return false
	case "enter":
		b.runConsoleInput()
		return false
	case "up":
		if b.consoleHistAt == len(b.consoleHist) {
			b.consoleDraft = text
		}
		if b.consoleHistAt > 0 {
			b.consoleHistAt--
			text = b.consoleHist[b.consoleHistAt]
			cur = len(text)
		}
	case "down":
		if b.consoleHistAt < len(b.consoleHist) {
			b.consoleHistAt++
			if b.consoleHistAt == len(b.consoleHist) {
				text = b.consoleDraft
			} else {
				text = b.consoleHist[b.consoleHistAt]
			}
			cur = len(text)
		}
	case "left":
		cur = prevRuneBoundary(text, cur)
	case "right":
		cur = nextRuneBoundary(text, cur)
	case "home", "ctrl-a":
		cur = 0
	case "end", "ctrl-e":
		cur = len(text)
	case "backspace":
		if cur > 0 {
			prev := prevRuneBoundary(text, cur)
			text = text[:prev] + text[cur:]
			cur = prev
		}
	case "delete":
		if cur < len(text) {
			next := nextRuneBoundary(text, cur)
			text = text[:cur] + text[next:]
		}
	case "ctrl-u":
		text = ""
		cur = 0
	case "ctrl-k":
		text = text[:cur]
	case "ctrl-w":
		before := strings.TrimRight(text[:cur], " ")
		cut := 0
		if i := strings.LastIndexAny(before, " /"); i >= 0 {
			cut = i + 1
		}
		text = text[:cut] + text[cur:]
		cur = cut
	default:
		if utf8.RuneCountInString(k) == 1 {
			r, _ := utf8.DecodeRuneInString(k)
			if r >= 32 && r != 127 && len([]rune(text)) < 8192 {
				text = text[:cur] + k + text[cur:]
				cur += len(k)
			}
		}
	}
	b.consoleText = text
	if cur > len(text) {
		cur = len(text)
	}
	b.consoleCursor = cur
	return false
}

// runConsoleInput evaluates the JavaScript input row and logs its result.
func (b *Browser) runConsoleInput() {
	src := strings.TrimSpace(b.consoleText)
	if src == "" {
		return
	}
	if len(b.consoleHist) == 0 || b.consoleHist[len(b.consoleHist)-1] != src {
		b.consoleHist = append(b.consoleHist, src)
		if len(b.consoleHist) > 200 {
			b.consoleHist = append([]string(nil), b.consoleHist[len(b.consoleHist)-200:]...)
		}
	}
	b.consoleHistAt = len(b.consoleHist)
	b.consoleDraft = ""
	b.logJS("input", src)
	b.consoleText = ""
	b.consoleCursor = 0
	if b.js == nil {
		b.logJS("error", "JavaScript is disabled (-no-js)")
		return
	}
	v, err := b.js.runCode("console", src)
	if err != nil {
		b.logJS("error", err.Error())
	} else if v != nil && !goja.IsUndefined(v) {
		s := v.String()
		if r := []rune(s); len(r) > 2000 {
			s = string(r[:2000]) + "…"
		}
		b.logJS("result", s)
	}
	if b.js.consumeDirty() {
		b.rebuild()
	}
	b.consumePendingJS()
	b.consolePane = consoleLogs
	b.consoleScroll = 0
}

// resetConsole clears the JS log and input for a newly loaded page. It is
// called before the new page's scripts run so stale errors cannot mix with the
// fresh document.
func (b *Browser) resetConsole() {
	b.jsLog = nil
	b.jsErr = false
	b.consoleText = ""
	b.consoleCursor = 0
	b.consoleScroll = 0
	b.reqScroll = 0
	b.storageScroll = 0
	b.htmlScroll = 0
	b.consolePane = consoleLogs
	b.consoleEdit = false
	b.consoleHistAt = len(b.consoleHist)
	b.consoleDraft = ""
	b.clearFind()
}

func (b *Browser) consoleLines(width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, entry := range b.jsLog {
		for _, raw := range strings.Split(entry, "\n") {
			raw = stripTerminalControls(expandTabs(strings.TrimRight(raw, "\r")))
			if raw == "" {
				out = append(out, "")
				continue
			}
			out = append(out, wrapCells(raw, width)...)
		}
	}
	if len(out) == 0 {
		out = append(out, "(no JavaScript messages)")
	}
	return out
}

// wrapCells hard-wraps a string to width cells.
func wrapCells(s string, width int) []string {
	var out []string
	var line strings.Builder
	col := 0
	for _, r := range s {
		rw := runeWidth(r)
		if rw == 0 {
			line.WriteRune(r)
			continue
		}
		if col+rw > width {
			out = append(out, line.String())
			line.Reset()
			col = 0
		}
		line.WriteRune(r)
		col += rw
	}
	if line.Len() > 0 {
		out = append(out, line.String())
	}
	if len(out) == 0 {
		out = append(out, "")
	}
	return out
}

func (b *Browser) requestLines() []string {
	if b.loader == nil {
		return []string{"(requests unavailable)"}
	}
	records := b.loader.requestSnapshot()
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
		line := fmt.Sprintf("%3s %-5s %6s %-5s %s", status, r.Method, size, kind, stripTerminalControls(r.URL))
		if r.Err != "" {
			line += "  " + stripTerminalControls(r.Err)
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		out = append(out, "(no requests for this page)")
	}
	return out
}

// requestCacheSummary is the pinned cache usage line shown below the request
// list.
func (b *Browser) requestCacheSummary() string {
	if b.loader == nil || b.loader.cache == nil {
		return "Cache: unavailable"
	}
	used, limit, entries := b.loader.cache.stats()
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
func (b *Browser) consolePaneLines(pane, width int) []string {
	switch pane {
	case consoleRequests:
		return b.requestLines()
	case consoleStorage:
		return b.storageLines()
	case consoleHTML:
		return b.htmlLines(width)
	default:
		return b.consoleLines(width)
	}
}

// storageLines summarises the current page's cookies, localStorage and
// sessionStorage. Everything is in memory only.
func (b *Browser) storageLines() []string {
	out := []string{"Cookies"}
	if b.url == nil || b.loader == nil || (b.url.Scheme != "http" && b.url.Scheme != "https") {
		out = append(out, "  (not available for this page)")
	} else {
		cookies := b.loader.cookies.cookiesForDisplay(b.url)
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
			if c.Domain != "" || c.Path != "" {
				line += "  (" + sanitizeStorageText(c.Domain) + sanitizeStorageText(c.Path) + ")"
			}
			out = append(out, line)
		}
	}
	var local, session map[string]string
	if b.js != nil {
		local = b.js.storageLocalData
		session = b.js.storageSessionData
	}
	out = append(out, "")
	out = appendStorageSection(out, "localStorage", local, b.js != nil)
	out = append(out, "")
	out = appendStorageSection(out, "sessionStorage", session, b.js != nil)
	return out
}

// htmlLines returns the raw HTML source of the current page, wrapped to
// the console width. The result is cached per page and width because the source
// can be large and the console redraws periodically.
func (b *Browser) htmlLines(width int) []string {
	if width < 1 {
		width = 1
	}
	src := ""
	if b.page != nil {
		src = b.page.RawHTML
	}
	if b.htmlPage == b.page && b.htmlWidth == width && b.htmlCache != nil {
		return b.htmlCache
	}
	var out []string
	for _, raw := range strings.Split(src, "\n") {
		raw = stripTerminalControls(expandTabs(strings.TrimRight(raw, "\r")))
		if raw == "" {
			out = append(out, "")
			continue
		}
		out = append(out, wrapCells(raw, width)...)
	}
	if len(out) == 0 {
		out = append(out, "(no HTML source)")
	}
	b.htmlPage = b.page
	b.htmlWidth = width
	b.htmlCache = out
	return out
}

func appendStorageSection(out []string, label string, data map[string]string, available bool) []string {
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
		out = append(out, "  "+sanitizeStorageText(k)+" = "+sanitizeStorageText(data[k]))
	}
	return out
}

// sanitizeStorageText keeps a single-cell line per value so one huge or
// multiline storage entry cannot disturb the console layout.
func sanitizeStorageText(s string) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	s = stripTerminalControls(s)
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
	if open == consoleLogs && l.bodyRows > 0 {
		l.inputY = l.bodyY + l.bodyRows - 1
	}
	return l
}

// consoleTabTitles returns the top-menu labels for the console sections.
func (b *Browser) consoleTabTitles() [consoleSectionCount]string {
	reqCount := 0
	if b.loader != nil {
		reqCount = b.loader.requestCount()
	}
	return [consoleSectionCount]string{
		fmt.Sprintf(" JS logs (%d) ", len(b.jsLog)),
		fmt.Sprintf(" Requests (%d) ", reqCount),
		" Storage ",
		" HTML ",
	}
}

// drawConsoleTabs paints the top menu and records clickable tab regions.
func (b *Browser) drawConsoleTabs(g *Grid, y, w int, normal, active Color, st *Style, fg Color) {
	b.consoleTabHits = b.consoleTabHits[:0]
	if y < 0 || w < 1 {
		return
	}
	titles := b.consoleTabTitles()
	x := 0
	for pane, title := range titles {
		if x >= w {
			break
		}
		tabW := stringWidth(title)
		if tabW > w-x {
			tabW = w - x
		}
		tabBG := normal
		if pane == b.consolePane {
			tabBG = active
		}
		g.Fill(x, y, tabW, 1, tabBG)
		g.Text(x, y, truncateWidth(title, tabW), st, fg)
		b.consoleTabHits = append(b.consoleTabHits, consoleTabHit{pane: pane, x: x, w: tabW})
		x += tabW
	}
	if x < w {
		g.Fill(x, y, w-x, 1, normal)
	}
}

// handleConsoleMouse opens the clicked tab and scrolls the active section
// under the wheel. Plain left clicks only; dragging is left to the terminal.
func (b *Browser) handleConsoleMouse(ev MouseEvent) {
	if b.width < 1 || b.height < 1 {
		return
	}
	x := ev.X - 1
	y := ev.Y - 1
	if x < 0 || y < 0 || x >= b.width || y >= b.height {
		return
	}
	l := newConsoleLayout(b.height, b.consolePane)
	if ev.Wheel != 0 {
		if l.bodyY < 0 || y < l.bodyY || y >= l.bodyY+l.bodyRows {
			return
		}
		if b.consolePane == consoleLogs && y == l.inputY {
			return
		}
		step := -3
		if ev.Wheel < 0 {
			step = 3
		}
		b.scrollConsolePane(b.consolePane, step)
		return
	}
	if ev.Motion || !ev.Press || ev.Button != 0 {
		return
	}
	if y == l.footerY {
		for _, hit := range b.footerHits {
			if x < hit.x || x >= hit.x+hit.w || y < hit.y || y >= hit.y+hit.h {
				continue
			}
			if hit.kind == headerConsole {
				b.closeConsole()
				return
			}
		}
		return
	}
	if y == l.tabsY {
		for _, hit := range b.consoleTabHits {
			if x >= hit.x && x < hit.x+hit.w {
				b.selectConsolePane(hit.pane)
				b.consoleEdit = false
				return
			}
		}
		return
	}
	if l.bodyY >= 0 && y >= l.bodyY && y < l.bodyY+l.bodyRows {
		b.consoleEdit = b.consolePane == consoleLogs && y == l.inputY
		if b.consoleEdit {
			b.consoleCursor = len(b.consoleText)
			b.consoleHistAt = len(b.consoleHist)
			b.consoleDraft = ""
		}
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

func (b *Browser) drawConsole(w, h int) {
	fg := RGB(0xdd, 0xe6, 0xee)
	bg := RGB(0x0f, 0x14, 0x1a)
	g := NewGrid(w, h, fg, bg)
	hdrBG := RGB(0x1f, 0x29, 0x37)
	ftrBG := RGB(0x1b, 0x24, 0x30)
	activeBG := RGB(0x2f, 0x6f, 0xbf)
	b.footerHits = b.footerHits[:0]
	if h >= 2 {
		g.Fill(0, h-1, w, 1, ftrBG)
	}
	l := newConsoleLayout(h, b.consolePane)
	reqs := b.requestLines()
	headStyle := &Style{Bold: true, Color: fg, FontSize: 16, LineHeight: 1}
	b.drawConsoleTabs(g, l.tabsY, w, hdrBG, activeBG, headStyle, fg)

	style := &Style{Color: fg, FontSize: 16, LineHeight: 1}
	drawPane := func(items []string, scroll, y, rows int) {
		if rows < 1 {
			return
		}
		start := paneStart(len(items), scroll, rows)
		for i := 0; i < rows && start+i < len(items); i++ {
			g.Text(1, y+i, truncateWidth(items[start+i], w-2), style, fg)
		}
	}
	if l.bodyY >= 0 && l.bodyRows > 0 {
		switch b.consolePane {
		case consoleRequests:
			reqRows := l.bodyRows
			if reqRows > 1 {
				reqRows--
			}
			drawPane(reqs, b.reqScroll, l.bodyY, reqRows)
			summaryY := l.bodyY + l.bodyRows - 1
			summary := truncateWidth(b.requestCacheSummary(), w-2)
			g.Fill(1, summaryY, maxInt(0, w-2), 1, ftrBG)
			g.Text(1, summaryY, summary, &Style{Color: RGB(0x8a, 0x9a, 0xaa), FontSize: 16, LineHeight: 1}, RGB(0x8a, 0x9a, 0xaa))
		case consoleStorage:
			drawPane(b.storageLines(), b.storageScroll, l.bodyY, l.bodyRows)
		case consoleHTML:
			htmlLines := b.htmlLines(w - 2)
			drawPane(htmlLines, b.htmlScroll, l.bodyY, l.bodyRows)
			b.paintFindHTML(g, paneStart(len(htmlLines), b.htmlScroll, l.bodyRows), l.bodyY, l.bodyRows)
		default:
			logRows := l.bodyRows
			if l.inputY >= 0 {
				logRows = l.inputY - l.bodyY
			}
			drawPane(b.consoleLines(w-2), b.consoleScroll, l.bodyY, logRows)
			if l.inputY >= 0 {
				inputY := l.inputY
				rowBG := ftrBG
				g.Fill(0, inputY, w, 1, rowBG)
				g.Text(0, inputY, "> ", style, fg)
				contentW := w - 2
				if contentW > 0 {
					if b.consoleEdit {
						runes, start, end, cur := promptWindow(b.consoleText, b.consoleCursor, contentW, false)
						xx := 2
						for i := start; i < end; i++ {
							if i == cur {
								g.Put(xx, inputY, ' ', &Style{Bold: true}, bg, RGB(0xf8, 0xfa, 0xfc))
								xx++
								continue
							}
							g.Put(xx, inputY, runes[i], style, fg, rowBG)
							xx += runeWidth(runes[i])
						}
						if cur == len(runes) && cur >= start {
							g.Put(xx, inputY, ' ', &Style{Bold: true}, bg, RGB(0xf8, 0xfa, 0xfc))
						}
					} else if b.consoleText == "" {
						g.Text(2, inputY, truncateWidth("JavaScript input: i or Enter", contentW), style, RGB(0x8a, 0x9a, 0xaa))
					} else {
						g.Text(2, inputY, truncateWidth(b.consoleText, contentW), style, fg)
					}
				}
			}
		}
	}
	if h >= 2 {
		if b.findActive && b.findScope == findScopeHTML {
			b.drawFindBar(g, h-1, w)
			b.flushGrid(g, nil)
			return
		}
		icon := ">_"
		iconW := stringWidth(icon)
		reserve := 0
		if w >= 6 {
			reserve = iconW + 2
		}
		hintW := w - reserve
		if hintW < 1 {
			hintW = 1
		}
		hint := " Click/Tab: switch section · i: JS input · j/k: scroll · g/G: ends · Shift+drag: select · q: quit · Esc: close "
		g.Text(0, h-1, padWidth(truncateWidth(hint, hintW), hintW),
			&Style{Color: fg, FontSize: 16, LineHeight: 1}, fg)
		if reserve > 0 {
			iconStyle := &Style{Bold: true, Color: RGB(0x8a, 0xc4, 0xff), FontSize: 16, LineHeight: 1}
			g.Text(w-iconW, h-1, icon, iconStyle, iconStyle.Color)
			b.footerHits = append(b.footerHits, headerHit{kind: headerConsole, x: w - iconW, y: h - 1, w: iconW, h: 1})
		}
	}
	b.flushGrid(g, nil)
}

// noticeDialog is a small informational modal, such as the unsupported
// file-upload message.
type noticeDialog struct {
	title   string
	message string
}

// selectHit is one clickable option row in the open select popup.
type selectHit struct {
	rect   Rect
	option *Node
}

// selectMenuDialog is the popup listing every option of a single select.
type selectMenuDialog struct {
	node    *Node
	options []*Node
	idx     int
	scroll  int
}

// openSelectMenu shows all options of a single select with the current value
// highlighted.
func (b *Browser) openSelectMenu(sel *Node) {
	opts := selectOptions(sel)
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
	b.selectMenu = &selectMenuDialog{node: sel, options: opts, idx: idx}
	b.selectHits = nil
	b.status = ""
}

// commitSelectOption applies the chosen option, closes the popup and fires the
// same input/change events a native select would.
func (b *Browser) commitSelectOption(opt *Node) {
	m := b.selectMenu
	if m == nil || opt == nil {
		return
	}
	for _, o := range m.options {
		removeAttr(o, "selected")
	}
	setAttr(opt, "selected", "")
	sel := m.node
	b.selectMenu = nil
	b.selectHits = nil
	b.status = ""
	b.fireSelectChange(sel)
}

// handleSelectMenuKey drives the option popup from the keyboard.
func (b *Browser) handleSelectMenuKey(k string) bool {
	m := b.selectMenu
	if m == nil || len(m.options) == 0 {
		return false
	}
	switch k {
	case "esc", "q", "ctrl-c":
		b.selectMenu = nil
		b.selectHits = nil
	case "up", "k", "ctrl-p":
		if m.idx > 0 {
			m.idx--
		}
	case "down", "j", "ctrl-n":
		if m.idx < len(m.options)-1 {
			m.idx++
		}
	case "home", "g":
		m.idx = 0
	case "end", "G":
		m.idx = len(m.options) - 1
	case "enter", " ":
		b.commitSelectOption(m.options[m.idx])
	}
	return false
}

// handleSelectMenuMouse chooses a clicked option or closes the popup when the
// dimmed backdrop or the close button is clicked.
func (b *Browser) handleSelectMenuMouse(ev MouseEvent) {
	if !ev.Press || ev.Motion || ev.Wheel != 0 || ev.Button != MouseButtonLeft {
		return
	}
	x, y := ev.X-1, ev.Y-1
	for _, hit := range b.selectHits {
		r := hit.rect
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			b.commitSelectOption(hit.option)
			return
		}
	}
	switch b.modalMouseAction(ev) {
	case "close", "outside":
		b.selectMenu = nil
		b.selectHits = nil
		b.status = ""
	}
}

// paintSelectMenu overlays the open single-select option popup.
func (b *Browser) paintSelectMenu(g *Grid) {
	m := b.selectMenu
	if m == nil || len(m.options) == 0 || g.W < 8 || g.H < 4 {
		return
	}
	maxLabel := 0
	for _, opt := range m.options {
		if w := stringWidth("✓ " + optionLabel(opt)); w > maxLabel {
			maxLabel = w
		}
	}
	hint := " ↑↓ choose · Enter select · Esc close "
	boxW := maxLabel + 6
	if w := stringWidth(hint) + 4; w > boxW {
		boxW = w
	}
	if boxW < 40 {
		boxW = 40
	}
	if boxW > g.W {
		boxW = g.W
	}
	if boxW < 8 {
		return
	}
	inner := boxW - 4
	maxRows := g.H - 4
	if maxRows < 1 {
		maxRows = 1
	}
	visible := len(m.options)
	if visible > maxRows {
		visible = maxRows
	}
	if m.idx < m.scroll {
		m.scroll = m.idx
	}
	if m.idx >= m.scroll+visible {
		m.scroll = m.idx - visible + 1
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	if m.scroll+visible > len(m.options) {
		m.scroll = len(m.options) - visible
	}
	boxH := visible + 4
	if boxH > g.H {
		boxH = g.H
	}
	x, y := b.modalFrame(g, boxW, boxH, "Select an option")
	fg := RGB(0x1f, 0x23, 0x28)
	style := &Style{Bold: true, Color: fg, FontSize: 16, LineHeight: 1}
	activeBG := RGB(0x2f, 0x6f, 0xbf)
	activeFG := RGB(0xff, 0xff, 0xff)
	b.selectHits = b.selectHits[:0]
	for i := 0; i < visible; i++ {
		idx := m.scroll + i
		opt := m.options[idx]
		marker := "  "
		if opt.HasAttr("selected") {
			marker = "✓ "
		}
		rowBG, rowFG := modalBG, fg
		rowStyle := style
		if idx == m.idx {
			rowBG, rowFG = activeBG, activeFG
		}
		g.Fill(x+1, y+2+i, boxW-2, 1, rowBG)
		text := padWidth(truncateWidth(marker+optionLabel(opt), inner), inner)
		g.Text(x+2, y+2+i, text, rowStyle, rowFG)
		b.selectHits = append(b.selectHits, selectHit{
			rect:   Rect{X: x + 1, Y: y + 2 + i, W: boxW - 2, H: 1},
			option: opt,
		})
	}
	g.Text(x+2, y+boxH-2, padWidth(truncateWidth(hint, boxW-4), boxW-4), style, fg)
	modalDim(g, x, y, boxW, boxH)
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
func (b *Browser) showAlert(msg string) {
	d := &jsDialog{kind: "alert", message: msg}
	b.runDialog(d)
}

// showConfirm returns the confirm result; the default is false when no
// interactive input is available.
func (b *Browser) showConfirm(msg string) bool {
	d := &jsDialog{kind: "confirm", message: msg}
	_, ok := b.runDialog(d)
	return ok
}

// showPrompt returns the entered text and whether OK was chosen. Cancel
// returns false (JS null).
func (b *Browser) showPrompt(msg, def string) (string, bool) {
	d := &jsDialog{kind: "prompt", message: msg, text: def, cursor: len(def), selectAll: true}
	return b.runDialog(d)
}

// runDialog blocks the JS caller while the modal owns the keyboard.
func (b *Browser) runDialog(d *jsDialog) (string, bool) {
	if b.keys == nil {
		return d.text, d.kind == "alert"
	}
	b.dialog = d
	defer func() {
		b.dialog = nil
		b.draw()
	}()
	for {
		b.draw()
		k, ok := <-b.keys
		if !ok || k == "eof" {
			return "", false
		}
		if ev, isMouse := ParseMouseToken(k); isMouse {
			switch b.modalMouseAction(ev) {
			case "close", "outside":
				return "", d.kind == "alert"
			}
			continue
		}
		switch d.kind {
		case "alert":
			switch k {
			case "enter", " ", "esc":
				return "", true
			}
		case "confirm":
			switch k {
			case "enter", "y", "Y":
				return "", true
			case "esc", "n", "N", "ctrl-c":
				return "", false
			}
		case "prompt":
			text, cur, done, ok := dialogPromptKey(d, k)
			d.text, d.cursor = text, cur
			if done {
				return d.text, ok
			}
		}
	}
}

func dialogPromptKey(d *jsDialog, k string) (text string, cursor int, done, ok bool) {
	text = d.text
	cursor = d.cursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(text) {
		cursor = len(text)
	}
	switch k {
	case "enter":
		return text, cursor, true, true
	case "esc", "ctrl-c":
		return text, cursor, true, false
	case "left":
		if d.selectAll {
			cursor = 0
			d.selectAll = false
		} else {
			cursor = prevRuneBoundary(text, cursor)
		}
	case "right":
		if d.selectAll {
			cursor = len(text)
			d.selectAll = false
		} else {
			cursor = nextRuneBoundary(text, cursor)
		}
	case "home":
		cursor = 0
		d.selectAll = false
	case "end":
		cursor = len(text)
		d.selectAll = false
	case "ctrl-a":
		d.selectAll = true
		cursor = len(text)
	case "backspace":
		if d.selectAll {
			text = ""
			cursor = 0
			d.selectAll = false
		} else if cursor > 0 {
			prev := prevRuneBoundary(text, cursor)
			text = text[:prev] + text[cursor:]
			cursor = prev
		}
	case "delete":
		if d.selectAll {
			text = ""
			cursor = 0
			d.selectAll = false
		} else if cursor < len(text) {
			next := nextRuneBoundary(text, cursor)
			text = text[:cursor] + text[next:]
		}
	case "ctrl-u":
		text = ""
		cursor = 0
		d.selectAll = false
	case "ctrl-w":
		if d.selectAll {
			text = ""
			cursor = 0
			d.selectAll = false
			break
		}
		before := strings.TrimRight(text[:cursor], " ")
		cut := 0
		if i := strings.LastIndexAny(before, " /"); i >= 0 {
			cut = i + 1
		}
		text = text[:cut] + text[cursor:]
		cursor = cut
	default:
		if utf8.RuneCountInString(k) == 1 {
			r, _ := utf8.DecodeRuneInString(k)
			if r >= 32 && r != 127 {
				if d.selectAll {
					text = k
					cursor = len(k)
					d.selectAll = false
				} else {
					text = text[:cursor] + k + text[cursor:]
					cursor += len(k)
				}
			}
		}
	}
	return text, cursor, false, false
}

// modalBG is the solid background used for modal bodies. It stays fully opaque
// over the dimmed page backdrop.
var modalBG = RGB(0xff, 0xff, 0xff)

// modalFrame draws the solid modal box, title bar and mouse close button, and
// records the close hit region. It returns the box origin.
func (b *Browser) modalFrame(g *Grid, boxW, boxH int, title string) (int, int) {
	x := (g.W - boxW) / 2
	y := (g.H - boxH) / 2
	border := RGB(0x2f, 0x6f, 0xbf)
	titleBG := RGB(0x2f, 0x6f, 0xbf)
	titleFG := RGB(0xff, 0xff, 0xff)
	titleStyle := &Style{Bold: true, Color: titleFG, FontSize: 16, LineHeight: 1}
	g.Fill(x, y, boxW, boxH, modalBG)
	// Fill only replaces background colors; blank the glyphs and their
	// attributes too so page text, link underlines and scaled text cannot
	// show through the solid modal body.
	for yy := maxInt(y, 0); yy < minInt(y+boxH, g.H); yy++ {
		for xx := maxInt(x, 0); xx < minInt(x+boxW, g.W); xx++ {
			c := &g.Cells[yy*g.W+xx]
			c.R = 0
			c.FG = RGB(0x1f, 0x23, 0x28)
			c.Bold, c.Italic, c.Underline, c.Strike = false, false, false, false
			c.Cont = false
			c.Img = false
			c.Raw = ""
			c.RawW = 0
			c.Skip = false
		}
	}
	g.Fill(x, y, boxW, 1, border)
	g.Fill(x, y+boxH-1, boxW, 1, border)
	g.Fill(x, y, 1, boxH, border)
	g.Fill(x+boxW-1, y, 1, boxH, border)
	g.Fill(x+1, y+1, boxW-2, 1, titleBG)
	g.Text(x+2, y+1, truncateWidth(" "+title+" ", boxW-8), titleStyle, titleFG)
	closeX := x + boxW - 2
	g.Put(closeX, y, '×', &Style{Bold: true}, titleFG, border)
	b.modalClose = Rect{X: closeX - 1, Y: y, W: 3, H: 1}
	b.modalBox = Rect{X: x, Y: y, W: boxW, H: boxH}
	return x, y
}

var (
	modalDimBG = RGB(0xdf, 0xe4, 0xea)
	modalDimFG = RGB(0xb2, 0xbc, 0xc8)
)

// modalDim darkens everything outside the modal rectangle so the solid box
// stands out against the page behind it. Page glyphs are muted as well as the
// background, otherwise dark text and link underlines stay legible and appear
// to leak into the modal's edges.
func modalDim(g *Grid, x, y, boxW, boxH int) {
	for yy := 0; yy < g.H; yy++ {
		for xx := 0; xx < g.W; xx++ {
			if xx >= x && xx < x+boxW && yy >= y && yy < y+boxH {
				continue
			}
			c := &g.Cells[yy*g.W+xx]
			c.BG = modalDimBG
			c.FG = modalDimFG
			c.Underline = false
			c.Strike = false
		}
	}
}

// paintDialog overlays the active JavaScript alert/confirm/prompt modal.
func (b *Browser) paintDialog(g *Grid) {
	d := b.dialog
	if d == nil {
		return
	}
	boxW := minInt(g.W-4, 68)
	if boxW < 20 {
		boxW = g.W
	}
	if boxW < 8 {
		return
	}
	inner := boxW - 4
	promptRow := 0
	if d.kind == "prompt" {
		promptRow = 1
	}
	msg := wrapCells(d.message, inner)
	maxLines := g.H - 6 - promptRow
	if maxLines < 1 {
		maxLines = 1
	}
	if len(msg) > maxLines {
		msg = msg[:maxLines]
	}
	boxH := 6 + len(msg) + promptRow
	if boxH > g.H {
		boxH = g.H
	}
	x, y := b.modalFrame(g, boxW, boxH, "JavaScript "+d.kind)
	fg := RGB(0x1f, 0x23, 0x28)
	style := &Style{Bold: true, Color: fg, FontSize: 16, LineHeight: 1}
	for i, line := range msg {
		g.Text(x+2, y+3+i, padWidth(truncateWidth(line, inner), inner), style, fg)
	}
	row := y + 3 + len(msg)
	if d.kind == "prompt" {
		g.Fill(x+2, row, inner, 1, RGB(0xe8, 0xf0, 0xfe))
		head, at, tail, show := promptParts("", d.text, d.cursor, d.selectAll, inner)
		g.Text(x+2, row, head, style, fg)
		cx := x + 2 + stringWidth(head)
		if show {
			g.Put(cx, row, at, &Style{Bold: true}, RGB(0xff, 0xff, 0xff), RGB(0x2f, 0x6f, 0xbf))
			cx++
		}
		g.Text(cx, row, tail, style, fg)
	}
	hint := " Enter: OK · Esc: Cancel "
	if d.kind == "alert" {
		hint = " Enter / Space / Esc: OK "
	} else if d.kind == "confirm" {
		hint = " Enter/Y: Yes · N/Esc: No "
	}
	g.Text(x+2, y+boxH-2, padWidth(truncateWidth(hint, boxW-4), boxW-4), style, fg)
	modalDim(g, x, y, boxW, boxH)
}

// paintDownload overlays the download curl hint. The command is shown as plain
// text; y copies it.
func (b *Browser) paintDownload(g *Grid) {
	d := b.download
	if d == nil {
		return
	}
	boxW := minInt(g.W-4, 82)
	if boxW >= g.W {
		boxW = g.W
	}
	if boxW < 8 {
		return
	}
	inner := boxW - 4
	lines := wrapCells(d.command, inner)
	maxLines := g.H - 6
	if maxLines < 1 {
		maxLines = 1
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	boxH := 6 + len(lines)
	if boxH > g.H {
		boxH = g.H
	}
	x, y := b.modalFrame(g, boxW, boxH, "Download "+d.name)
	fg := RGB(0x1f, 0x23, 0x28)
	style := &Style{Bold: true, Color: fg, FontSize: 16, LineHeight: 1}
	for i, line := range lines {
		g.Text(x+2, y+3+i, padWidth(truncateWidth(line, inner), inner), style, fg)
	}
	hint := " y: copy curl command · Esc: close "
	g.Text(x+2, y+boxH-2, padWidth(truncateWidth(hint, boxW-4), boxW-4), style, fg)
	modalDim(g, x, y, boxW, boxH)
}

// paintNotice overlays a small informational modal.
func (b *Browser) paintNotice(g *Grid) {
	d := b.notice
	if d == nil {
		return
	}
	boxW := minInt(g.W-4, 68)
	if boxW >= g.W {
		boxW = g.W
	}
	if boxW < 8 {
		return
	}
	inner := boxW - 4
	lines := wrapCells(d.message, inner)
	maxLines := g.H - 6
	if maxLines < 1 {
		maxLines = 1
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	boxH := 6 + len(lines)
	if boxH > g.H {
		boxH = g.H
	}
	x, y := b.modalFrame(g, boxW, boxH, d.title)
	fg := RGB(0x1f, 0x23, 0x28)
	style := &Style{Bold: true, Color: fg, FontSize: 16, LineHeight: 1}
	for i, line := range lines {
		g.Text(x+2, y+3+i, padWidth(truncateWidth(line, inner), inner), style, fg)
	}
	hint := " Enter / Esc: close "
	g.Text(x+2, y+boxH-2, padWidth(truncateWidth(hint, boxW-4), boxW-4), style, fg)
	modalDim(g, x, y, boxW, boxH)
}
