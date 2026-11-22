package browser

import (
	"context"
	"fmt"
	"net/url"
)

func (l *Loader) loadPDFPage(ctx context.Context, data []byte, finalURL *url.URL, status int) *Page {
	doc := buildPDFDocument(data)
	doc.TotalPages = len(doc.Pages)
	if l.pdfPage > 0 && len(doc.Pages) > 0 {
		if l.pdfPage > len(doc.Pages) {
			doc.Pages = nil
			doc.Note = fmt.Sprintf("Page %d does not exist; this document has %d pages.", l.pdfPage, doc.TotalPages)
		} else {
			// Keep PageRows/PageCells from the whole document so an exported
			// page has the same size and column it has in the book.
			doc.Pages = doc.Pages[l.pdfPage-1 : l.pdfPage]
		}
	}
	html := pdfPageHTML(doc, finalURL)
	root := ParseHTML(html)
	doc.PageNodes = root.FindAll(func(n *Node) bool { return n.Attr("class") == "pdf-page" })
	page := &Page{
		URL:     finalURL,
		Base:    finalURL,
		Root:    root,
		Title:   doc.Title,
		Status:  status,
		PDF:     doc,
		RawHTML: "[PDF document — text extracted; the original bytes are not HTML]",
	}
	if page.Title == "" {
		page.Title = pdfDisplayName(finalURL)
	}
	if page.Title == "" {
		page.Title = "PDF document"
	}
	doc.Title = page.Title
	page.Author = l.collectStylesheets(ctx, root, page.Base)
	return page
}

// buildPDFDocument parses and extracts a PDF, containing panics so malformed
// media can never crash the browser.
