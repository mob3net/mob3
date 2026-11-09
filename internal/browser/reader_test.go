package browser

import (
	"net/url"
	"strings"
	"testing"
)

func TestExtractReaderArticle(t *testing.T) {
	doc := ParseHTML(`<html><body>
		<nav><a href="/nav">Navigation link</a></nav>
		<main><article class="post-content">
			<h1>Article title</h1>
			<p>` + strings.Repeat("This is article prose. ", 12) + `</p>
			<div class="share">Share this article</div>
			<p style="color:red" class="promo">A promotional paragraph</p>
			<a href="/next">Next article</a>
			<img src="/pic.png" alt="A picture" width="20" height="10">
		</article></main>
		<aside>Sidebar text</aside>
		<footer>Footer text</footer>
	</body></html>`)
	root, ok := extractReaderPage(doc, "Article title")
	if !ok {
		t.Fatal("extraction failed")
	}
	if root.FindFirst("nav") != nil || root.FindFirst("aside") != nil || root.FindFirst("footer") != nil {
		t.Fatal("junk element survived extraction")
	}
	if got := root.FindFirst("h1").TextContent(); got != "Article title" {
		t.Fatalf("title = %q", got)
	}
	if got := root.FindFirst("a").Attr("href"); got != "/next" {
		t.Fatalf("link href = %q", got)
	}
	if root.FindFirst("img") != nil {
		t.Fatal("image survived extraction")
	}
	if strings.Contains(root.TextContent(), "A picture") || strings.Contains(root.TextContent(), "[image:") {
		t.Fatal("image alt text survived extraction")
	}
	p := root.FindFirst("p")
	if p.Attr("class") != "" || p.Attr("style") != "" {
		t.Fatalf("paragraph attributes survived: %q %q", p.Attr("class"), p.Attr("style"))
	}
	if strings.Contains(root.TextContent(), "Share this") {
		t.Fatal("share widget survived extraction")
	}
	if strings.Contains(root.TextContent(), "Sidebar") || strings.Contains(root.TextContent(), "Footer") {
		t.Fatal("page chrome survived extraction")
	}
	// The original tree must not be modified by extraction.
	if doc.FindFirst("nav") == nil || doc.FindFirst("aside") == nil {
		t.Fatal("extraction modified the original document")
	}
}

func TestExtractReaderAddsTitle(t *testing.T) {
	doc := ParseHTML(`<html><body><article>
		<p>` + strings.Repeat("Article text without a heading. ", 10) + `</p>
	</article></body></html>`)
	root, ok := extractReaderPage(doc, "Page title")
	if !ok {
		t.Fatal("extraction failed")
	}
	if got := root.FindFirst("h1").TextContent(); got != "Page title" {
		t.Fatalf("title = %q", got)
	}
}

func TestExtractReaderFallbackCandidate(t *testing.T) {
	doc := ParseHTML(`<html><body><div class="post-content">
		<p>` + strings.Repeat("Fallback prose. ", 20) + `</p>
	</div></body></html>`)
	root, ok := extractReaderPage(doc, "")
	if !ok {
		t.Fatal("fallback extraction failed")
	}
	if root.FindFirst("article") == nil {
		t.Fatal("reader wrapper missing")
	}
	if !strings.Contains(root.TextContent(), "Fallback prose") {
		t.Fatal("fallback prose missing")
	}
}

func TestExtractReaderIgnoresStructuralMenuTokens(t *testing.T) {
	doc := ParseHTML(`<html class="has-main-menu"><body class="page-with-sidebar">
		<article><h1>Article</h1><p>` +
		strings.Repeat("Structural classes must not hide the article. ", 8) + `</p></article>
	</body></html>`)
	root, ok := extractReaderPage(doc, "Article")
	if !ok {
		t.Fatal("extraction failed on structural menu/sidebar classes")
	}
	if !strings.Contains(root.TextContent(), "Structural classes") {
		t.Fatal("article content missing")
	}
}

func TestExtractReaderNoContent(t *testing.T) {
	doc := ParseHTML(`<html><body><nav>Menu only</nav><script>var x = 1;</script></body></html>`)
	if _, ok := extractReaderPage(doc, ""); ok {
		t.Fatal("extraction accepted a page with no article content")
	}
	images := ParseHTML(`<html><body><article><img src="/only.png" alt="Only image"></article></body></html>`)
	if _, ok := extractReaderPage(images, "Images"); ok {
		t.Fatal("extraction accepted an image-only page")
	}
}

func TestReaderToggle(t *testing.T) {
	doc := ParseHTML(`<html><body><nav>Navigation</nav><article>
		<h1>Long article</h1>
		` + strings.Repeat(`<p>Reader paragraph text.</p>`, 40) + `
	</article></body></html>`)
	u, _ := url.Parse("https://example.com/article")
	b := NewBrowser(60, 20)
	b.page = &Page{URL: u, Base: u, Root: doc, Title: "Long article"}
	b.rebuild()
	b.scroll = 5
	b.clampScroll()
	originalScroll := b.scroll
	if b.HandleKey("R") {
		t.Fatal("R asked to quit")
	}
	if !b.reader {
		t.Fatal("reader mode did not turn on")
	}
	if b.status != "Reader mode — R to exit" {
		t.Fatalf("status = %q", b.status)
	}
	if b.layout == nil || b.layout.Height == 0 {
		t.Fatal("reader layout is empty")
	}
	readerText := strings.Join(strings.Fields(layoutText(b.layout)), " ")
	if strings.Contains(readerText, "Navigation") {
		t.Fatal("reader layout still contains navigation")
	}
	if !strings.Contains(readerText, "Reader paragraph text") {
		t.Fatal("reader layout is missing article text")
	}
	b.scroll = 4
	b.HandleKey("R")
	if b.reader {
		t.Fatal("reader mode did not turn off")
	}
	if b.scroll != originalScroll {
		t.Fatalf("restored scroll = %d, want %d", b.scroll, originalScroll)
	}
	if b.status != "Reader mode off" {
		t.Fatalf("status = %q", b.status)
	}
}

func TestReaderPersistsAcrossNavigation(t *testing.T) {
	doc := ParseHTML(`<html><body><article><p>` +
		strings.Repeat("Old article text. ", 20) + `</p></article></body></html>`)
	u, _ := url.Parse("https://example.com/old")
	b := NewBrowser(60, 20)
	b.page = &Page{URL: u, Base: u, Root: doc, Title: "Old"}
	b.rebuild()
	b.HandleKey("R")
	if !b.reader {
		t.Fatal("reader mode did not turn on")
	}

	nextURL, _ := url.Parse("https://example.com/new")
	nextDoc := ParseHTML(`<html><body><article><p>` +
		strings.Repeat("New article text. ", 20) + `</p></article></body></html>`)
	b.finishLoad(loadResult{
		page: &Page{URL: nextURL, Base: nextURL, Root: nextDoc, Title: "New"},
		u:    nextURL,
	})
	if !b.reader || b.readerRoot == nil {
		t.Fatal("reader mode did not survive navigation")
	}
	if text := strings.Join(strings.Fields(layoutText(b.layout)), " "); !strings.Contains(text, "New article text") {
		t.Fatalf("new reader layout = %q", text)
	}

	emptyURL, _ := url.Parse("https://example.com/empty")
	emptyDoc := ParseHTML(`<html><body><nav>Menu only</nav></body></html>`)
	b.finishLoad(loadResult{
		page: &Page{URL: emptyURL, Base: emptyURL, Root: emptyDoc, Title: "Empty"},
		u:    emptyURL,
	})
	if !b.reader || b.readerRoot != nil {
		t.Fatal("reader preference did not survive an unavailable page")
	}
	if b.status != "Reader mode unavailable" {
		t.Fatalf("status = %q", b.status)
	}

	lastURL, _ := url.Parse("https://example.com/last")
	lastDoc := ParseHTML(`<html><body><article><p>` +
		strings.Repeat("Last article text. ", 20) + `</p></article></body></html>`)
	b.finishLoad(loadResult{
		page: &Page{URL: lastURL, Base: lastURL, Root: lastDoc, Title: "Last"},
		u:    lastURL,
	})
	if !b.reader || b.readerRoot == nil {
		t.Fatal("reader mode did not resume on the next article")
	}
	b.HandleKey("R")
	if b.reader || b.readerRoot != nil {
		t.Fatal("R did not turn reader mode off")
	}
}

func TestReaderFooterLabel(t *testing.T) {
	doc := ParseHTML(`<html><body><article><p>` +
		strings.Repeat("Reader icon article text. ", 20) + `</p></article></body></html>`)
	u, _ := url.Parse("https://example.com/article")
	b := NewBrowser(80, 20)
	b.page = &Page{URL: u, Base: u, Root: doc, Title: "Article"}
	b.rebuild()
	drawQuiet(b)

	var readerHit, consoleHit *headerHit
	for i := range b.footerHits {
		switch b.footerHits[i].kind {
		case headerReader:
			readerHit = &b.footerHits[i]
		case headerConsole:
			consoleHit = &b.footerHits[i]
		}
	}
	if readerHit == nil {
		t.Fatalf("reader label has no click region: %+v", b.footerHits)
	}
	if consoleHit != nil && readerHit.x+readerHit.w >= consoleHit.x {
		t.Fatalf("reader label overlaps console icon: %+v %+v", *readerHit, *consoleHit)
	}
	withQuietStdout(t, func() {
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: readerHit.x + 1, Y: readerHit.y + 1, Press: true})
	})
	if !b.reader {
		t.Fatal("clicking the reader label did not enable reader mode")
	}
	withQuietStdout(t, func() {
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: readerHit.x + 1, Y: readerHit.y + 1, Press: true})
	})
	if b.reader {
		t.Fatal("clicking the reader label did not disable reader mode")
	}
}

func layoutText(ly *Layout) string {
	if ly == nil {
		return ""
	}
	var b strings.Builder
	for i := range ly.Frags {
		if ly.Frags[i].Kind == fragText {
			b.WriteString(ly.Frags[i].Text)
			b.WriteByte(' ')
		}
	}
	return b.String()
}
