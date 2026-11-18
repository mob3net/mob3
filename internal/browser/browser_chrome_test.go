package browser

import (
	"context"
	"errors"
	"image"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestHeaderHitRegions(t *testing.T) {
	b := NewBrowser(60, 10)
	b.loader.bookmarks = []Bookmark{{Title: "x", URL: "https://example.com/"}}
	u, _ := url.Parse("https://example.com/a")
	b.url = u
	drawQuiet(b)

	if len(b.headerHits) != 9 {
		t.Fatalf("header hits = %+v", b.headerHits)
	}
	bookW := stringWidth(" ★ ")
	histW := stringWidth(" ≡ ")
	helpW := stringWidth(" ? ")
	cfgW := stringWidth(" ⚙ ")
	want := []headerHit{
		{headerBack, 0, 0, 3, 1},
		{headerForward, 3, 0, 3, 1},
		{headerReload, 6, 0, 3, 1},
		{headerHome, 9, 0, 3, 1},
		{headerBookmarks, b.width - cfgW - helpW - histW - bookW, 0, bookW, 1},
		{headerHistory, b.width - cfgW - helpW - histW, 0, histW, 1},
		{headerHelp, b.width - cfgW - helpW, 0, helpW, 1},
		{headerConfig, b.width - cfgW, 0, cfgW, 1},
		{headerURL, 0, 1, 60, 1},
	}
	for i := range want {
		if b.headerHits[i] != want[i] {
			t.Errorf("hit %d = %+v, want %+v", i, b.headerHits[i], want[i])
		}
	}
	if b.headerHits[7].x+b.headerHits[7].w != b.width {
		t.Error("config is not at the far right")
	}
	if top, _ := b.viewport(); top != 2 {
		t.Errorf("viewport top = %d, want 2 for a two-row bar", top)
	}
}

func TestBookmarksButtonHiddenWithoutFile(t *testing.T) {
	b := NewBrowser(100, 10)
	drawQuiet(b)
	for _, hit := range b.headerHits {
		if hit.kind == headerBookmarks {
			t.Fatalf("bookmarks button shown without -bookmarks: %+v", b.headerHits)
		}
	}
	b.loader.bookmarksSet = true
	drawQuiet(b)
	found := false
	for _, hit := range b.headerHits {
		if hit.kind == headerBookmarks {
			found = true
		}
	}
	if !found {
		t.Fatalf("bookmarks button missing after -bookmarks: %+v", b.headerHits)
	}
}

func TestHeaderWideIcons(t *testing.T) {
	b := NewBrowser(100, 10)
	drawQuiet(b)
	widths := map[headerKind]int{}
	for _, hit := range b.headerHits {
		widths[hit.kind] = hit.w
	}
	if widths[headerForward] != stringWidth(" → ") || widths[headerHome] != stringWidth(" ⌂ ") ||
		widths[headerReload] != stringWidth(" ⟳ ") {
		t.Fatalf("forward/reload/home widths = %d/%d/%d",
			widths[headerForward], widths[headerReload], widths[headerHome])
	}
	if widths[headerBack] != stringWidth(" ← Back ") {
		t.Fatalf("back width = %d", widths[headerBack])
	}
}

func TestCenteredLabel(t *testing.T) {
	x, s := centeredLabel(10, 21, "abc")
	if x != 19 || s != "abc" {
		t.Fatalf("centeredLabel = %d,%q", x, s)
	}
	x, s = centeredLabel(10, 2, "abcdef")
	if x != 10 || s != "ab" {
		t.Fatalf("truncated centeredLabel = %d,%q", x, s)
	}
}

func TestHeaderNarrow(t *testing.T) {
	b := NewBrowser(20, 10)
	b.loader.bookmarks = []Bookmark{{Title: "x", URL: "https://example.com/"}}
	drawQuiet(b)
	if len(b.headerHits) != 6 || b.headerHits[0].kind != headerHome ||
		b.headerHits[1].kind != headerBookmarks || b.headerHits[2].kind != headerHistory ||
		b.headerHits[3].kind != headerHelp || b.headerHits[4].kind != headerConfig ||
		b.headerHits[5].kind != headerURL {
		t.Fatalf("narrow header hits = %+v", b.headerHits)
	}
	if b.headerHits[4].x+b.headerHits[4].w != b.width {
		t.Error("narrow config is not at the far right")
	}
	b2 := NewBrowser(20, 10)
	b2.width = 4
	drawQuiet(b2)
	if len(b2.headerHits) != 1 || b2.headerHits[0].kind != headerURL {
		t.Fatalf("tiny header hits = %+v", b2.headerHits)
	}
	b3 := NewBrowser(60, 10)
	b3.height = 4
	drawQuiet(b3)
	if top, _ := b3.viewport(); top != 1 {
		t.Errorf("short terminal viewport top = %d, want 1", top)
	}
	for _, hit := range b3.headerHits {
		if hit.y != 0 {
			t.Errorf("short terminal used more than one header row: %+v", hit)
		}
	}
}

func TestLoadingProgressPaintsURLRow(t *testing.T) {
	b := NewBrowser(80, 10)
	b.color = true
	b.loading = true
	b.startProgress()
	b.progress = 0.5
	drawQuiet(b)
	if !strings.Contains(b.prev[1], "48;2;31;79;138") {
		t.Fatalf("URL row has no progress fill: %q", b.prev[1])
	}
}

func TestTextScaleCanBeDisabled(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("KITTY_WINDOW_ID", "1")
	b := NewBrowser(40, 10)
	b.gfx = NewGFXRenderer(GraphicsKitty, 8, 16)
	b.page = &Page{URL: mustParse(t, "https://example.com"), Root: ParseHTML(`<h1>big</h1>`)}
	b.noBigText = true
	b.rebuild()
	if b.bigText {
		t.Fatal("scaled text was not disabled")
	}
	b.noBigText = false
	b.rebuild()
	if !b.bigText {
		t.Fatal("scaled text should be enabled in kitty by default")
	}
}

func TestFooterOmitsScrollPercent(t *testing.T) {
	b := NewBrowser(80, 10)
	b.layout = &Layout{Height: 107}
	b.scroll = 50
	drawQuiet(b)
	line := plainLine(b.prev[len(b.prev)-1])
	if strings.Contains(line, "%") {
		t.Fatalf("footer still shows a scroll percentage: %q", line)
	}
	if !strings.HasSuffix(line, ">_") {
		t.Fatalf("footer = %q", line)
	}
}

func TestFooterShowsUsage(t *testing.T) {
	b := NewBrowser(120, 10)
	b.layout = &Layout{Height: 100}
	b.usage = procUsage{rss: 18 << 20}
	drawQuiet(b)
	line := plainLine(b.prev[len(b.prev)-1])
	if !strings.Contains(line, "MEM 18MB") {
		t.Errorf("footer = %q, want MEM 18MB", line)
	}
	for _, unwanted := range []string{"CPU", "Tab:", "q: quit", "h/l", "q Quit", "? Help"} {
		if strings.Contains(line, unwanted) {
			t.Errorf("footer still contains %q: %q", unwanted, line)
		}
	}
}

func TestFooterUsageDropsOnNarrow(t *testing.T) {
	b := NewBrowser(20, 10)
	b.layout = &Layout{Height: 100}
	b.usage = procUsage{rss: 18 << 20}
	drawQuiet(b)
	line := plainLine(b.prev[len(b.prev)-1])
	if strings.Contains(line, "MEM") {
		t.Fatalf("narrow footer still shows usage: %q", line)
	}
	if !strings.HasSuffix(line, ">_") {
		t.Fatalf("narrow footer = %q", line)
	}
}

func TestPageScrollbarMouse(t *testing.T) {
	b := NewBrowser(20, 10)
	b.layout = &Layout{Height: 100}
	drawQuiet(b)
	if b.scrollBar == nil {
		t.Fatal("no scrollbar for a long page")
	}
	info := b.scrollBar
	if info.trackTop != 2 || info.trackH != 7 {
		t.Fatalf("scrollbar track = %+v", info)
	}
	b.HandleMouse(MouseEvent{Button: 0, X: b.width, Y: info.trackTop + info.trackH, Press: true})
	if b.scroll != b.maxScroll() || b.scroll == 0 {
		t.Fatalf("bottom click scroll = %d, max = %d", b.scroll, b.maxScroll())
	}
	b.HandleMouse(MouseEvent{Button: 0, X: b.width, Y: info.trackTop + 1, Press: true})
	if b.scroll != 0 {
		t.Fatalf("top click scroll = %d", b.scroll)
	}

	b.HandleMouse(MouseEvent{Button: 0, X: b.width, Y: info.trackTop + 3, Press: true})
	b.HandleMouse(MouseEvent{Button: 0, X: b.width, Y: info.trackTop + info.trackH, Press: true, Motion: true})
	b.HandleMouse(MouseEvent{Button: 0, X: b.width, Y: info.trackTop + info.trackH, Press: false})
	if b.scroll != b.maxScroll() {
		t.Fatalf("drag scroll = %d, max = %d", b.scroll, b.maxScroll())
	}
	if b.scrollDrag {
		t.Fatal("scrollbar drag did not end")
	}

	// A release report may carry the motion bit; it must still end the drag.
	b.HandleMouse(MouseEvent{Button: 0, X: b.width, Y: info.trackTop + 1, Press: true})
	b.HandleMouse(MouseEvent{Button: 0, X: b.width, Y: info.trackTop + 3, Press: true, Motion: true})
	b.HandleMouse(MouseEvent{Button: 0, X: b.width, Y: info.trackTop + 3, Press: false, Motion: true})
	if b.scrollDrag {
		t.Fatal("release with motion bit kept the scrollbar drag active")
	}
	dragEnd := b.scroll
	b.HandleMouse(MouseEvent{Button: 0, X: b.width, Y: info.trackTop + info.trackH, Press: false, Motion: true})
	if b.scroll != dragEnd {
		t.Fatalf("pointer motion after release scrolled: %d -> %d", dragEnd, b.scroll)
	}

	// Releasing over the toolbar also ends the drag.
	b.HandleMouse(MouseEvent{Button: 0, X: b.width, Y: info.trackTop + 1, Press: true})
	b.HandleMouse(MouseEvent{Button: 0, X: b.width, Y: info.trackTop + 2, Press: true, Motion: true})
	b.HandleMouse(MouseEvent{Button: 0, X: b.width, Y: 0, Press: false})
	if b.scrollDrag {
		t.Fatal("release over the toolbar kept the scrollbar drag active")
	}

	short := NewBrowser(20, 10)
	short.layout = &Layout{Height: 5}
	drawQuiet(short)
	if short.scrollBar != nil {
		t.Fatal("scrollbar shown for a page that fits")
	}
}

func TestFooterConsoleIcon(t *testing.T) {
	b := NewBrowser(80, 10)
	b.layout = &Layout{Height: 100}
	drawQuiet(b)
	var hit *headerHit
	for i := range b.footerHits {
		if b.footerHits[i].kind == headerConsole {
			hit = &b.footerHits[i]
			break
		}
	}
	if hit == nil || hit.x+hit.w != b.width {
		t.Fatalf("console hit = %+v", b.footerHits)
	}
	withQuietStdout(t, func() {
		b.HandleMouse(MouseEvent{Button: 0, X: hit.x + 1, Y: hit.y + 1, Press: true})
	})
	if !b.consoleOpen {
		t.Fatal("clicking the console icon did not open the console")
	}
}

func TestScrollRepaintsChrome(t *testing.T) {
	b := NewBrowser(40, 10)
	b.layout = &Layout{Height: 100}
	drawQuiet(b)
	b.prev[0] = "stale header"
	b.scrollBy(1)
	drawQuiet(b)
	if b.prev[0] == "stale header" {
		t.Fatal("scrolling did not force the header to be repainted")
	}
	if !strings.Contains(b.prev[0], "←") {
		t.Fatalf("header line = %q", b.prev[0])
	}

	// The chrome is rewritten even when nothing changed.
	b.prev[0] = "stale header"
	drawQuiet(b)
	if b.prev[0] == "stale header" || !strings.Contains(b.prev[0], "←") {
		t.Fatal("unchanged header was not repainted")
	}
}

func TestHeaderClicks(t *testing.T) {
	b := NewBrowser(60, 10)
	b.loader.bookmarks = []Bookmark{{Title: "x", URL: "https://example.com/"}}
	u, _ := url.Parse("https://example.com/a")
	b.url = u
	drawQuiet(b)

	b.HandleMouse(MouseEvent{Button: 0, X: 7, Y: 2, Press: true})
	if !b.prompt || b.promptText != "https://example.com/a" || !b.promptSelectAll {
		t.Fatalf("URL prompt = %q selectAll=%v", b.promptText, b.promptSelectAll)
	}
	b.prompt = false
	b.promptSelectAll = false

	b.HandleMouse(MouseEvent{Button: 0, X: 2, Y: 1, Press: true})
	if b.status != "No previous page" {
		t.Fatalf("back button status = %q", b.status)
	}
	b.HandleMouse(MouseEvent{Button: 0, X: 4, Y: 1, Press: true})
	if b.status != "No next page" {
		t.Fatalf("forward button status = %q", b.status)
	}

	targetCh := make(chan string, 1)
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		select {
		case targetCh <- target:
		default:
		}
		return &Page{}, nil
	}
	b.HandleMouse(MouseEvent{Button: 0, X: 55, Y: 1, Press: true})
	select {
	case target := <-targetCh:
		if target != "about:help" {
			t.Fatalf("help target = %q", target)
		}
	case <-time.After(time.Second):
		t.Fatal("help button did not navigate")
	}

	b.home = "https://example.com/home"
	b.HandleMouse(MouseEvent{Button: 0, X: 10, Y: 1, Press: true})
	select {
	case target := <-targetCh:
		if target != "https://example.com/home" {
			t.Fatalf("home target = %q", target)
		}
	case <-time.After(time.Second):
		t.Fatal("home button did not navigate")
	}

	b.HandleMouse(MouseEvent{Button: 0, X: 52, Y: 1, Press: true})
	select {
	case target := <-targetCh:
		if target != "about:history" {
			t.Fatalf("history target = %q", target)
		}
	case <-time.After(time.Second):
		t.Fatal("history button did not navigate")
	}

	b.HandleMouse(MouseEvent{Button: 0, X: 49, Y: 1, Press: true})
	select {
	case target := <-targetCh:
		if target != "about:bookmarks" {
			t.Fatalf("bookmarks target = %q", target)
		}
	case <-time.After(time.Second):
		t.Fatal("bookmarks button did not navigate")
	}

	b.HandleMouse(MouseEvent{Button: 0, X: 58, Y: 1, Press: true})
	if b.page == nil || b.page.Title != "Config — MOB³" {
		t.Fatalf("config page = %+v", b.page)
	}
}

func TestJSErrorFooterLabel(t *testing.T) {
	b := NewBrowser(200, 10)
	b.color = true
	b.usage = procUsage{rss: 18 << 20}
	if strings.Contains(b.footerRight(140), "Error") {
		t.Fatal("error label shown without a JS error")
	}
	b.logJS("error", "TypeError: boom")
	if !b.jsErr {
		t.Fatal("JS error was not recorded")
	}
	right := b.footerRight(140)
	if !strings.Contains(right, "JavaScript:Error") {
		t.Fatalf("footer right = %q", right)
	}
	if strings.Contains(right, "JS: Error") {
		t.Fatalf("separate error hint still present: %q", right)
	}
	for _, want := range []string{"Graphics:none", "Reader:off", "MEM 18MB"} {
		if !strings.Contains(right, want) {
			t.Fatalf("footer right missing %q: %q", want, right)
		}
	}
	if strings.Index(right, "JavaScript:Error") > strings.Index(right, "Reader:off") {
		t.Fatalf("JS state is not before reader state: %q", right)
	}
	if strings.Index(right, "Reader:off") > strings.Index(right, "MEM") {
		t.Fatalf("reader state is not before memory: %q", right)
	}
	drawQuiet(b)
	line := b.prev[len(b.prev)-1]
	if !strings.Contains(line, "38;2;179;29;29") {
		t.Fatalf("JavaScript:Error is not red: %q", line)
	}
	if !strings.Contains(line, "0;1;4;38;2;179;29;29") {
		t.Fatalf("JavaScript:Error J is not underlined: %q", line)
	}
	var hit *headerHit
	for i := range b.footerHits {
		if b.footerHits[i].kind == headerJS {
			hit = &b.footerHits[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("JavaScript label has no click region: %+v", b.footerHits)
	}
	withQuietStdout(t, func() {
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: hit.x + 1, Y: hit.y + 1, Press: true})
	})
	if !b.noJS {
		t.Fatal("clicking JavaScript state did not turn scripts off")
	}
	if b.consoleOpen {
		t.Fatal("clicking JavaScript state opened the console")
	}
}

func TestJavaScriptFooterLabelClick(t *testing.T) {
	b := NewBrowser(180, 10)
	drawQuiet(b)
	var hit *headerHit
	for i := range b.footerHits {
		if b.footerHits[i].kind == headerJS {
			hit = &b.footerHits[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("JavaScript label has no click region: %+v", b.footerHits)
	}
	withQuietStdout(t, func() {
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: hit.x + 1, Y: hit.y + 1, Press: true})
	})
	if !b.noJS {
		t.Fatal("clicking JavaScript:on did not disable scripts")
	}
	if b.loader == nil || !b.loader.noScripts {
		t.Fatal("loader still fetches scripts after the toggle")
	}
	if b.consoleOpen {
		t.Fatal("clicking JavaScript:on opened the console")
	}
}

func TestGraphicsFooterLabelClick(t *testing.T) {
	b := NewBrowser(180, 10)
	b.cellW, b.cellH = 8, 16
	b.graphicsModes = []GraphicsMode{GraphicsKitty, GraphicsBlocks, GraphicsNone}
	b.graphicsMode = GraphicsKitty
	b.gfx = NewGFXRenderer(GraphicsKitty, 8, 16)
	drawQuiet(b)
	var hit *headerHit
	for i := range b.footerHits {
		if b.footerHits[i].kind == headerGraphics {
			hit = &b.footerHits[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("Graphics label has no click region: %+v", b.footerHits)
	}
	withQuietStdout(t, func() {
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: hit.x + 1, Y: hit.y + 1, Press: true})
	})
	if b.graphicsMode != GraphicsBlocks || b.gfx != nil {
		t.Fatalf("clicking Graphics did not cycle modes: mode=%v gfx=%v", b.graphicsMode, b.gfx)
	}
}

func TestGraphicsLabelUnderlinesShortcut(t *testing.T) {
	g := NewGrid(30, 1, RGB(0, 0, 0), RGB(255, 255, 255))
	st := &Style{FontSize: 16, LineHeight: 1}
	drawUnderlinedRune(g, 0, 0, "Graphics:none", 'i', st, RGB(0xff, 0xff, 0xff))
	idx := strings.Index("Graphics:none", "i")
	if !g.Cells[idx].Underline {
		t.Fatal("i in Graphics is not underlined")
	}
	if g.Cells[idx-1].Underline {
		t.Fatal("wrong letter underlined before i")
	}
}

func TestFooterGapBeforeRightState(t *testing.T) {
	b := NewBrowser(100, 10)
	drawQuiet(b)
	line := plainLine(b.prev[len(b.prev)-1])
	marker := "Dark:off"
	i := strings.Index(line, marker)
	if i < footerGap || line[i-footerGap:i] != strings.Repeat(" ", footerGap) {
		t.Fatalf("footer has no %d-cell gap before %q: %q", footerGap, marker, line)
	}
}

func TestFooterRightCollapsesLeftToRight(t *testing.T) {
	b := NewBrowser(160, 10)
	b.usage = procUsage{rss: 18 << 20}
	full := b.footerRight(100)
	for _, want := range []string{"JavaScript:on", "Graphics:none", "Dark:off", "Reader:off", "MEM 18MB"} {
		if !strings.Contains(full, want) {
			t.Fatalf("wide footer missing %q: %q", want, full)
		}
	}
	cases := []struct {
		width int
		want  string
		gone  []string
	}{
		{60, "JavaScript:on", []string{"Graphics:none"}},
		{40, "Dark:off", []string{"JavaScript:on"}},
		{25, "Reader:off", []string{"Dark:off"}},
		{20, "MEM 18MB", []string{"Reader:off"}},
		{10, "MEM 18MB", []string{"Reader:off", "Dark:off", "JavaScript:on", "Graphics:none"}},
	}
	for _, tc := range cases {
		got := b.footerRight(tc.width)
		if !strings.Contains(got, tc.want) {
			t.Errorf("footerRight(%d) = %q, want %q kept", tc.width, got, tc.want)
		}
		for _, gone := range tc.gone {
			if strings.Contains(got, gone) {
				t.Errorf("footerRight(%d) = %q, still has %q", tc.width, got, gone)
			}
		}
	}
}

func TestFooterRightDoesNotShrinkForLongLeftText(t *testing.T) {
	b := NewBrowser(100, 10)
	drawQuiet(b)
	right := b.footerRight(100/2 - 7)
	if right == "" {
		t.Fatal("no right footer state")
	}
	if !strings.Contains(plainLine(b.prev[len(b.prev)-1]), right) {
		t.Fatalf("short footer = %q, missing right state %q", plainLine(b.prev[len(b.prev)-1]), right)
	}
	b.status = strings.Repeat("long status ", 30)
	drawQuiet(b)
	if !strings.Contains(plainLine(b.prev[len(b.prev)-1]), right) {
		t.Fatalf("long footer collapsed the right state: %q, want %q", plainLine(b.prev[len(b.prev)-1]), right)
	}
}

func TestFooterShortcutUnderline(t *testing.T) {
	g := NewGrid(40, 1, RGB(0, 0, 0), RGB(255, 255, 255))
	st := &Style{Color: RGB(0x24, 0x29, 0x2f), FontSize: 16, LineHeight: 1}
	drawShortcutLabel(g, 0, 0, "Dark:off", st, st.Color)
	if !g.Cells[0].Underline {
		t.Fatal("Dark shortcut is not underlined")
	}
	if g.Cells[1].Underline || g.Cells[4].Underline {
		t.Fatal("non-shortcut letters or punctuation were underlined")
	}
	g2 := NewGrid(40, 1, RGB(0, 0, 0), RGB(255, 255, 255))
	drawShortcutLabel(g2, 0, 0, "Reader:off", st, st.Color)
	if !g2.Cells[0].Underline || g2.Cells[1].Underline {
		t.Fatal("Reader shortcut underline is wrong")
	}
}

func TestHistoryHeaderShortcutUnderline(t *testing.T) {
	b := NewBrowser(60, 10)
	g := NewGrid(40, 1, RGB(0, 0, 0), RGB(255, 255, 255))
	st := &Style{Color: RGB(0xf6, 0xf8, 0xfa), FontSize: 16, LineHeight: 1}
	b.drawHeaderButton(g, 0, 0, "≡ History", headerHistory, RGB(0x2b, 0x3a, 0x4c), st.Color, st)
	// "≡ " occupies two cells, so the H is cell 2.
	if g.Cells[0].Underline || g.Cells[1].Underline {
		t.Fatal("symbol prefix was underlined")
	}
	if !g.Cells[2].Underline {
		t.Fatal("History shortcut H is not underlined")
	}
	if g.Cells[3].Underline {
		t.Fatal("non-shortcut letter was underlined")
	}
}

func TestFooterShowsJSAndGraphicsState(t *testing.T) {
	b := NewBrowser(120, 10)
	right := b.footerRight(120)
	if !strings.Contains(right, "JavaScript:on") || !strings.Contains(right, "Graphics:none") {
		t.Fatalf("footer right = %q", right)
	}
	b.loader.noImages = false
	b.gfx = &gfxRenderer{mode: GraphicsKitty}
	if got := b.footerRight(120); !strings.Contains(got, "Graphics:kitty") {
		t.Fatalf("resolved graphics footer = %q", got)
	}
	b.noJS = true
	if got := b.footerRight(120); !strings.Contains(got, "JavaScript:off") {
		t.Fatalf("JavaScript-off footer = %q", got)
	}
}

func TestJSConsoleDoesNotUseFooterText(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		console.log("noise");
		console.warn("warning");
		console.error("boom");
	`)
	b := NewBrowser(80, 10)
	b.startJS(page)
	if b.status != "" {
		t.Fatalf("console output set footer status: %q", b.status)
	}
	if !b.jsErr {
		t.Fatal("console.error did not set the footer hint")
	}
}

func TestJSReportErrorUsesFooterHint(t *testing.T) {
	b := NewBrowser(80, 10)
	b.status = "keep me"
	j := &jsRuntime{b: b}
	j.reportError(errors.New("boom"))
	if b.status != "keep me" {
		t.Fatalf("status = %q", b.status)
	}
	if !b.jsErr {
		t.Fatal("reportError did not set the footer hint")
	}
	if len(b.jsLog) == 0 || !strings.Contains(b.jsLog[0], "boom") {
		t.Fatalf("log = %v", b.jsLog)
	}
}

func TestProcUsageFooterRoundsMemory(t *testing.T) {
	u := procUsage{rss: 18<<20 + 256<<10}
	if got, want := u.footer(), "MEM 18MB"; got != want {
		t.Fatalf("footer = %q, want %q", got, want)
	}
	u.rss = 18<<20 + 600<<10
	if got, want := u.footer(), "MEM 19MB"; got != want {
		t.Fatalf("footer = %q, want %q", got, want)
	}
}

func TestDialogRendering(t *testing.T) {
	b := NewBrowser(60, 15)
	b.dialog = &jsDialog{kind: "confirm", message: "Continue with this action?"}
	drawQuiet(b)
	text := strings.Join(b.prev, "\n")
	if !strings.Contains(text, "JavaScript confirm") || !strings.Contains(text, "Continue with this action?") {
		t.Fatalf("dialog not rendered: %q", text)
	}
}

func TestModalCloseButtonClosesWithMouse(t *testing.T) {
	u := mustParse(t, "https://example.com/file.zip")
	b := NewBrowser(60, 15)
	b.page = &Page{URL: u, Base: u, Root: ParseHTML(`<p>x</p>`)}
	b.url = u
	b.showDownload(u.String(), "")
	drawQuiet(b)
	r := b.modalClose
	if r.W <= 0 {
		t.Fatal("download modal close button was not recorded")
	}
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: r.X + r.W/2 + 1, Y: r.Y + 1, Press: true})
	if b.download != nil {
		t.Fatal("clicking the modal close button did not close the download popup")
	}

	b.showDownload(u.String(), "")
	drawQuiet(b)
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: 1, Y: 1, Press: true})
	if b.download != nil {
		t.Fatal("clicking the backdrop did not close the download popup")
	}

	b.showNotice("Upload not supported", "not supported")
	drawQuiet(b)
	r = b.modalClose
	if r.W <= 0 {
		t.Fatal("notice modal close button was not recorded")
	}
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: r.X + r.W/2 + 1, Y: r.Y + 1, Press: true})
	if b.notice != nil {
		t.Fatal("clicking the modal close button did not close the notice")
	}
}

func TestModalClearsPageGlyphs(t *testing.T) {
	u := mustParse(t, "https://example.com/file.zip")
	b := NewBrowser(60, 15)
	b.page = &Page{URL: u, Base: u, Root: ParseHTML(`<p>x</p>`)}
	b.url = u
	b.showDownload(u.String(), "")
	g := NewGrid(60, 15, RGB(0, 0, 0), RGB(255, 255, 255))
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			g.Put(x, y, 'X', &Style{Underline: true, Strike: true}, RGB(0, 0, 0), Color{})
		}
	}
	b.paintDownload(g)
	r := b.modalBox
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			c := g.Cells[y*g.W+x]
			if c.R == 'X' {
				t.Fatalf("page glyph survived inside the modal at (%d,%d)", x, y)
			}
			if c.Underline || c.Strike {
				t.Fatalf("page text decoration survived inside the modal at (%d,%d)", x, y)
			}
		}
	}
	// The page backdrop outside the box is dimmed rather than left readable,
	// so page text does not appear to leak into the modal edges.
	c := g.Cells[0]
	if c.Underline {
		t.Fatal("page link underline survived on the dimmed backdrop")
	}
	if c.BG != modalDimBG || c.FG != modalDimFG {
		t.Fatalf("page backdrop cell = fg %+v bg %+v, want dimmed fg %+v bg %+v", c.FG, c.BG, modalDimFG, modalDimBG)
	}
}

func TestFooterStatusColors(t *testing.T) {
	b := NewBrowser(80, 10)
	b.color = true
	b.layout = &Layout{Height: 10}
	cases := []struct {
		status string
		code   string
	}{
		{moduleUnsupportedStatus, "38;2;154;91;0"},
		{layoutTruncatedStatus, "38;2;154;91;0"},
		{"HTTP 404 Not Found", "38;2;154;91;0"},
		{"Error: boom", "38;2;179;29;29"},
		{"Copied 3 chars", "38;2;23;107;53"},
	}
	for _, tc := range cases {
		b.status = tc.status
		drawQuiet(b)
		line := b.prev[len(b.prev)-1]
		if !strings.Contains(line, tc.code) {
			t.Errorf("status %q footer = %q, want color %s", tc.status, line, tc.code)
		}
	}
}

func TestCycleGraphicsModes(t *testing.T) {
	b := NewBrowser(80, 20)
	b.cellW, b.cellH = 8, 16
	b.graphicsModes = []GraphicsMode{GraphicsKitty, GraphicsBlocks, GraphicsNone}
	b.graphicsMode = GraphicsKitty
	b.gfx = NewGFXRenderer(GraphicsKitty, 8, 16)
	if b.loader != nil {
		b.loader.noImages = false
	}

	b.cycleGraphics()
	if b.graphicsMode != GraphicsBlocks || b.gfx != nil {
		t.Fatalf("after full graphics: mode=%v gfx=%v", b.graphicsMode, b.gfx)
	}
	if b.loader != nil && b.loader.noImages {
		t.Fatal("blocks mode should keep image loading enabled")
	}

	b.cycleGraphics()
	if b.graphicsMode != GraphicsNone || b.gfx != nil {
		t.Fatalf("after blocks: mode=%v gfx=%v", b.graphicsMode, b.gfx)
	}
	if b.loader != nil && !b.loader.noImages {
		t.Fatal("none mode should disable image loading")
	}

	b.cycleGraphics()
	if b.graphicsMode != GraphicsKitty || b.gfx == nil {
		t.Fatalf("after none: mode=%v gfx=%v", b.graphicsMode, b.gfx)
	}
	if b.loader != nil && b.loader.noImages {
		t.Fatal("full graphics should enable image loading")
	}
}

func TestGraphicsModeChangeRebuildsWithoutReload(t *testing.T) {
	b := NewBrowser(80, 20)
	b.cellW, b.cellH = 8, 16
	u := mustParse(t, "https://example.com/")
	doc := ParseHTML(`<html><body><p>before</p><img alt="pic" src="pic.png"><p>after</p></body></html>`)
	for _, img := range doc.FindAll(func(n *Node) bool { return n.Tag == "img" }) {
		img.Img = image.NewRGBA(image.Rect(0, 0, 160, 80))
		img.ImgSrc = "pic.png"
	}
	b.loader = NewLoader()
	b.loader.noImages = false
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.graphicsModes = []GraphicsMode{GraphicsKitty, GraphicsBlocks, GraphicsNone}
	b.graphicsMode = GraphicsKitty
	b.gfx = NewGFXRenderer(GraphicsKitty, 8, 16)
	b.rebuild()
	page := b.page
	seq := b.loadSeq
	hasImage := func() bool {
		for _, f := range b.layout.Frags {
			if f.Kind == fragImage {
				return true
			}
		}
		return false
	}
	if !hasImage() {
		t.Fatal("initial layout has no image fragment")
	}

	b.cycleGraphics() // native -> blocks
	if b.page != page || b.loadSeq != seq {
		t.Fatal("changing to blocks reloaded the page")
	}
	if b.gfx != nil || !hasImage() {
		t.Fatalf("blocks mode gfx=%v image=%v", b.gfx, hasImage())
	}

	b.cycleGraphics() // blocks -> none
	if b.page != page || b.loadSeq != seq {
		t.Fatal("changing to none reloaded the page")
	}
	if !b.loader.noImages {
		t.Fatal("none mode should disable image loading")
	}
	if hasImage() {
		t.Fatal("none mode still laid out an image fragment")
	}

	b.cycleGraphics() // none -> native
	if b.page != page || b.loadSeq != seq {
		t.Fatal("changing back to native graphics reloaded the page")
	}
	if !hasImage() {
		t.Fatal("image fragment did not return after enabling graphics")
	}
}

func TestStatusShortcutUnderlinesErrorE(t *testing.T) {
	st := &Style{Color: RGB(0xb3, 0x1d, 0x1d), FontSize: 16, LineHeight: 1}
	g := NewGrid(40, 1, RGB(0, 0, 0), RGB(255, 255, 255))
	drawStatusShortcut(g, 0, 0, 40, " Error: Invalid URL", st, st.Color)
	if !g.Cells[1].Underline {
		t.Fatal("E in Error: is not underlined")
	}
	if g.Cells[0].Underline || g.Cells[2].Underline {
		t.Fatal("only the E should be underlined")
	}

	g2 := NewGrid(40, 1, RGB(0, 0, 0), RGB(255, 255, 255))
	drawStatusShortcut(g2, 0, 0, 40, " Site blocked (CAPTCHA)", st, st.Color)
	for i := range g2.Cells {
		if g2.Cells[i].Underline {
			t.Fatal("warning status advertised an underline")
		}
	}
}
