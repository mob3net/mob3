package engine

import (
	"errors"
	"image"
	"strings"
	"testing"
)

func TestTextScaleCanBeDisabled(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("KITTY_WINDOW_ID", "1")
	b := newTestSession(40, 10)
	b.gfx = NewGFXRenderer(GraphicsKitty, 8, 16)
	b.Page = &Page{URL: mustParse(t, "https://example.com"), Root: ParseHTML(`<h1>big</h1>`)}
	b.NoBigText = true
	b.rebuild()
	if b.BigText {
		t.Fatal("scaled text was not disabled")
	}
	b.NoBigText = false
	b.rebuild()
	if !b.BigText {
		t.Fatal("scaled text should be enabled in kitty by default")
	}
}

func TestJSErrorFooterLabel(t *testing.T) {
	b := newTestSession(200, 10)
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

}

func TestFooterRightCollapsesLeftToRight(t *testing.T) {
	b := newTestSession(160, 10)
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
	b := newTestSession(100, 10)
	right := b.footerRight(100/2 - 7)
	if right == "" {
		t.Fatal("no right footer state")
	}
	b.status = strings.Repeat("long status ", 30)
	if got := b.footerModel().right; got != right {
		t.Fatalf("long footer collapsed the right state: %q, want %q", got, right)
	}
}

func TestFooterShowsJSAndGraphicsState(t *testing.T) {
	b := newTestSession(120, 10)
	right := b.footerRight(120)
	if !strings.Contains(right, "JavaScript:on") || !strings.Contains(right, "Graphics:none") {
		t.Fatalf("footer right = %q", right)
	}
	b.loader.NoImages = false
	b.gfx = NewGFXRenderer(GraphicsKitty, 8, 16)
	if got := b.footerRight(120); !strings.Contains(got, "Graphics:kitty") {
		t.Fatalf("resolved graphics footer = %q", got)
	}
	b.NoJS = true
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
	b := newTestSession(80, 10)
	b.startJS(page)
	if b.status != "" {
		t.Fatalf("console output set footer status: %q", b.status)
	}
	if !b.jsErr {
		t.Fatal("console.error did not set the footer hint")
	}
}

func TestJSReportErrorUsesFooterHint(t *testing.T) {
	b := newTestSession(80, 10)
	b.status = "keep me"
	j := newJSRuntime(b, nil)
	j.ReportError(errors.New("boom"))
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

func TestCycleGraphicsModes(t *testing.T) {
	b := newTestSession(80, 20)
	b.CellW, b.CellH = 8, 16
	b.graphicsModes = []GraphicsMode{GraphicsKitty, GraphicsBlocks, GraphicsNone}
	b.newGraphics = newTestGraphics
	b.graphicsMode = GraphicsKitty
	b.gfx = NewGFXRenderer(GraphicsKitty, 8, 16)
	if b.loader != nil {
		b.loader.NoImages = false
	}

	b.cycleGraphics()
	if b.graphicsMode != GraphicsBlocks || b.gfx != nil {
		t.Fatalf("after full graphics: mode=%v gfx=%v", b.graphicsMode, b.gfx)
	}
	if b.loader != nil && b.loader.NoImages {
		t.Fatal("blocks mode should keep image loading enabled")
	}

	b.cycleGraphics()
	if b.graphicsMode != GraphicsNone || b.gfx != nil {
		t.Fatalf("after blocks: mode=%v gfx=%v", b.graphicsMode, b.gfx)
	}
	if b.loader != nil && !b.loader.NoImages {
		t.Fatal("none mode should disable image loading")
	}

	b.cycleGraphics()
	if b.graphicsMode != GraphicsKitty || b.gfx == nil {
		t.Fatalf("after none: mode=%v gfx=%v", b.graphicsMode, b.gfx)
	}
	if b.loader != nil && b.loader.NoImages {
		t.Fatal("full graphics should enable image loading")
	}
}

func TestGraphicsModeChangeRebuildsWithoutReload(t *testing.T) {
	b := newTestSession(80, 20)
	b.CellW, b.CellH = 8, 16
	u := mustParse(t, "https://example.com/")
	doc := ParseHTML(`<html><body><p>before</p><img alt="pic" src="pic.png"><p>after</p></body></html>`)
	for _, img := range doc.FindAll(func(n *Node) bool { return n.Tag == "img" }) {
		img.Img = image.NewRGBA(image.Rect(0, 0, 160, 80))
		img.ImgSrc = "pic.png"
	}
	b.loader = newTestLoader()
	b.loader.NoImages = false
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.URL = u
	b.graphicsModes = []GraphicsMode{GraphicsKitty, GraphicsBlocks, GraphicsNone}
	b.newGraphics = newTestGraphics
	b.graphicsMode = GraphicsKitty
	b.gfx = NewGFXRenderer(GraphicsKitty, 8, 16)
	b.rebuild()
	page := b.Page
	seq := b.LoadSeq
	hasImage := func() bool {
		for _, f := range b.Layout.Frags {
			if f.Kind == FragImage {
				return true
			}
		}
		return false
	}
	if !hasImage() {
		t.Fatal("initial layout has no image fragment")
	}

	b.cycleGraphics() // native -> blocks
	if b.Page != page || b.LoadSeq != seq {
		t.Fatal("changing to blocks reloaded the page")
	}
	if b.gfx != nil || !hasImage() {
		t.Fatalf("blocks mode gfx=%v image=%v", b.gfx, hasImage())
	}

	b.cycleGraphics() // blocks -> none
	if b.Page != page || b.LoadSeq != seq {
		t.Fatal("changing to none reloaded the page")
	}
	if !b.loader.NoImages {
		t.Fatal("none mode should disable image loading")
	}
	if hasImage() {
		t.Fatal("none mode still laid out an image fragment")
	}

	b.cycleGraphics() // none -> native
	if b.Page != page || b.LoadSeq != seq {
		t.Fatal("changing back to native graphics reloaded the page")
	}
	if !hasImage() {
		t.Fatal("image fragment did not return after enabling graphics")
	}
	if b.gfx == nil || !b.inlineImagesEnabled() {
		t.Fatalf("native renderer did not return: gfx=%v", b.gfx)
	}
}

func TestModalDisablesScaledText(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("KITTY_WINDOW_ID", "1")
	b := newTestSession(40, 10)
	b.gfx = NewGFXRenderer(GraphicsKitty, 8, 16)
	b.Page = &Page{URL: mustParse(t, "https://example.com"), Root: ParseHTML(`<h1>big</h1><p>text</p>`)}
	b.rebuild()
	if !b.BigText {
		t.Fatal("scaled text should be enabled in kitty by default")
	}
	b.selectMenu = &selectMenuDialog{}
	drawQuiet(b)
	if b.BigText {
		t.Fatal("scaled text stayed on while a modal was open")
	}
	b.selectMenu = nil
	drawQuiet(b)
	if !b.BigText {
		t.Fatal("scaled text did not return after the modal closed")
	}
}

func TestZoomBadgeShows(t *testing.T) {
	b := newTestSession(80, 10)
	b.Zoom = 1.5
	if got, want := b.zoomBadge(), " 150% "; got != want {
		t.Fatalf("zoom badge = %q, want %q", got, want)
	}
	if got := b.stateMessage().ZoomLabel; got != " 150% " {
		t.Fatalf("state zoom label = %q", got)
	}
	if got := b.stateMessage().ZoomPercent; got != 150 {
		t.Fatalf("state zoom percent = %d", got)
	}
}

func TestGraphicsDowngradeAndRepaintPlacements(t *testing.T) {
	u := mustParse(t, "https://example.com/pic")
	doc := ParseHTML(`<html><body><p>intro</p><img src="/pic.png"></body></html>`)
	if n := doc.FindFirst("img"); n != nil {
		n.Img = image.NewRGBA(image.Rect(0, 0, 40, 20))
		n.ImgSrc = "/pic.png"
	}
	b := newTestSession(80, 20)
	b.loader = newTestLoader()
	b.loader.NoImages = false
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.URL = u
	b.emitFrames = true
	b.frontendChrome = true
	b.graphicsModes = []GraphicsMode{GraphicsKitty, GraphicsBlocks, GraphicsNone}
	b.newGraphics = newTestGraphics
	b.setGraphicsMode(GraphicsKitty)
	b.draw()
	if len(b.frameBuf.Placements) == 0 {
		t.Fatal("no native placements in kitty mode")
	}
	b.setGraphicsMode(GraphicsBlocks)
	b.draw()
	if len(b.frameBuf.Placements) != 0 {
		t.Fatal("native placements survived blocks mode")
	}
	b.setGraphicsMode(GraphicsKitty)
	b.draw()
	if len(b.frameBuf.Placements) == 0 {
		t.Fatal("native placements did not return after re-enabling kitty")
	}
}
