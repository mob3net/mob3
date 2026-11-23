package engine

import (
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
	root, ok := ExtractReaderPage(doc, "Article title")
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
	root, ok := ExtractReaderPage(doc, "Page title")
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
	root, ok := ExtractReaderPage(doc, "")
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
	root, ok := ExtractReaderPage(doc, "Article")
	if !ok {
		t.Fatal("extraction failed on structural menu/sidebar classes")
	}
	if !strings.Contains(root.TextContent(), "Structural classes") {
		t.Fatal("article content missing")
	}
}

func TestExtractReaderNoContent(t *testing.T) {
	doc := ParseHTML(`<html><body><nav>Menu only</nav><script>var x = 1;</script></body></html>`)
	if _, ok := ExtractReaderPage(doc, ""); ok {
		t.Fatal("extraction accepted a page with no article content")
	}
	images := ParseHTML(`<html><body><article><img src="/only.png" alt="Only image"></article></body></html>`)
	if _, ok := ExtractReaderPage(images, "Images"); ok {
		t.Fatal("extraction accepted an image-only page")
	}
}
