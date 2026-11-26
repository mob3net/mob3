package engine

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// AboutPageName returns the page part of an about: URL, ignoring a query.
func AboutPageName(u *url.URL) string {
	if u == nil || !strings.EqualFold(u.Scheme, "about") {
		return ""
	}
	name := u.Opaque
	if name == "" {
		name = strings.TrimPrefix(u.Path, "/")
	}
	return strings.ToLower(name)
}

// AssembleAboutPage builds one of the built-in about: pages. The caller
// supplies the version and the in-memory history snapshot; stylesheet
// collection goes through the page fetcher.
func AssembleAboutPage(ctx context.Context, fetcher PageFetcher, u *url.URL, version string, history []string, current int, noScripts bool) *Page {
	var doc string
	home := false
	switch AboutPageName(u) {
	case "blank":
		doc = "<html><body></body></html>"
	case "help":
		doc = HelpPageHTML(version)
	case "history":
		doc = HistoryPageHTML(history, current)
	default:
		doc = StartPageHTML
		home = true
	}
	root := ParseHTML(doc)
	if !noScripts {
		RemoveNoscript(root)
	}
	title := "MOB³"
	switch AboutPageName(u) {
	case "help":
		title = "Help — MOB³"
	case "history":
		title = "History — MOB³"
	}
	page := &Page{URL: u, Base: u, Root: root, Title: title, RawHTML: doc, Status: 200}
	page.Author = CollectStylesheets(ctx, fetcher, root, page.Base)
	if home {
		// The start page uses the reader-mode stylesheet exactly, so it gets the
		// same narrow article column and typography as an extracted article.
		page.Author = append(page.Author, ReaderRules...)
	}
	return page
}

// AssembleBlockedPage builds the generated warning page for an anti-bot or
// firewall response and collects its embedded stylesheet.
func AssembleBlockedPage(ctx context.Context, fetcher PageFetcher, finalURL *url.URL, rawHTML, reason string, status int, local bool) *Page {
	root := ParseHTML(BlockedPageHTML(finalURL.String(), reason, status))
	page := &Page{
		URL: finalURL, Base: finalURL, Root: root, RawHTML: rawHTML,
		Title: "Site blocked", Status: status, Blocked: true, BlockReason: reason,
		Local: local,
	}
	// The generated page carries its own stylesheet. Collect it like any other
	// document, or the blocked page falls back to bare user-agent styles.
	page.Author = CollectStylesheets(ctx, fetcher, root, page.Base)
	return page
}

// AssemblePDFPage parses a PDF response, applies the optional one-page export
// limit and builds the extracted-text page model.
func AssemblePDFPage(ctx context.Context, fetcher PageFetcher, data []byte, finalURL *url.URL, status, pdfPage int, local bool) *Page {
	doc := BuildPDFDocumentContext(ctx, data)
	doc.TotalPages = len(doc.Pages)
	if pdfPage > 0 && len(doc.Pages) > 0 {
		if pdfPage > len(doc.Pages) {
			doc.Pages = nil
			doc.Note = fmt.Sprintf("Page %d does not exist; this document has %d pages.", pdfPage, doc.TotalPages)
		} else {
			// Keep PageRows/PageCells from the whole document so an exported
			// page has the same size and column it has in the book.
			doc.Pages = doc.Pages[pdfPage-1 : pdfPage]
		}
	}
	html := PDFPageHTML(doc, finalURL)
	root := ParseHTML(html)
	doc.PageNodes = root.FindAll(func(n *Node) bool { return n.Attr("class") == "pdf-page" })
	page := &Page{
		URL:     finalURL,
		Base:    finalURL,
		Root:    root,
		Title:   doc.Title,
		Status:  status,
		PDF:     doc,
		Local:   local,
		RawHTML: "[PDF document — text extracted; the original bytes are not HTML]",
	}
	if page.Title == "" {
		page.Title = PDFDisplayName(finalURL)
	}
	if page.Title == "" {
		page.Title = "PDF document"
	}
	doc.Title = page.Title
	page.Author = CollectStylesheets(ctx, fetcher, root, page.Base)
	return page
}

// BlockedResponse recognizes anti-bot/CAPTCHA/firewall roadblocks. Vendor
// names are only used as a hint; the page shown to the user stays generic.
func BlockedResponse(status int, body string) (bool, string) {
	switch status {
	case 401, 403, 429, 503:
	default:
		return false, ""
	}
	head := strings.ToLower(body[:minInt(len(body), 24000)])
	signatures := []struct {
		key    string
		reason string
	}{
		{"cf-chl", "Cloudflare browser challenge"},
		{"challenge-platform", "Cloudflare browser challenge"},
		{"just a moment", "browser challenge"},
		{"enable javascript and cookies to continue", "browser challenge"},
		{"datadome", "DataDome bot protection"},
		{"captcha-delivery", "CAPTCHA challenge"},
		{"px-captcha", "PerimeterX bot protection"},
		{"perimeterx", "PerimeterX bot protection"},
		{"incapsula", "Imperva firewall"},
		{"sucuri website firewall", "Sucuri firewall"},
		{"pardon our interruption", "anti-bot check"},
		{"unusual traffic", "bot check"},
		{"verify you are a human", "human verification"},
		{"access denied", "access denied"},
	}
	for _, s := range signatures {
		if strings.Contains(head, s.key) {
			return true, s.reason
		}
	}
	return false, ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
