package browser

// Built-in text-only PDF viewer.
//
// A PDF response is parsed, its text is extracted page by page, and the
// result becomes an ordinary page document. That keeps find, selection, dark
// mode, reader mode, -dump, -png and the automation snapshot working without
// PDF-specific code paths. Rasterization, figures, annotations and links are
// deliberately out of scope.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
)

// PDFDocument is the extracted text of one PDF response.
type PDFDocument struct {
	Title string
	Pages []PDFPageText
	// Note explains why a document is empty or partial; empty when all pages
	// were read.
	Note string
	// Partial marks a document whose page or text cap was reached.
	Partial bool
	// Encrypted marks a document that could not be read without a password.
	Encrypted bool
}

// PDFPageText is one page of extracted text.
type PDFPageText struct {
	Number int
	Spans  []pdfSpan
}

// Text returns the plain text of the document, with form feeds between pages.
func (d *PDFDocument) Text() string {
	var b strings.Builder
	for i, p := range d.Pages {
		if i > 0 {
			b.WriteByte('\f')
		}
		for _, s := range p.Spans {
			b.WriteString(s.Text)
		}
	}
	return b.String()
}

// looksLikePDF reports whether a response is a PDF the viewer should open.
// Unknown binary types are sniffed for the %PDF- header so a PDF served as
// application/octet-stream still opens instead of prompting a download.
func looksLikePDF(mediaType string, data []byte) bool {
	switch mediaType {
	case "application/pdf", "application/x-pdf", "application/acrobat":
		return true
	case "", "application/octet-stream", "binary/octet-stream":
		head := data
		if len(head) > 1024 {
			head = head[:1024]
		}
		return strings.HasPrefix(strings.TrimSpace(string(head)), "%PDF-")
	}
	return false
}

// loadPDFPage builds a readable page from a PDF response.
func (l *Loader) loadPDFPage(ctx context.Context, data []byte, finalURL *url.URL, status int) *Page {
	doc := buildPDFDocument(data)
	html := pdfPageHTML(doc, finalURL, status)
	root := ParseHTML(html)
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
	page.Author = l.collectStylesheets(ctx, root, page.Base, false)
	return page
}

// buildPDFDocument parses and extracts a PDF, containing panics so malformed
// media can never crash the browser.
func buildPDFDocument(data []byte) (doc *PDFDocument) {
	defer func() {
		if recover() != nil {
			doc = &PDFDocument{
				Title: "PDF document",
				Note:  "This PDF could not be read safely.",
			}
		}
	}()
	pdf, err := parsePDF(data)
	if err != nil {
		doc = &PDFDocument{Title: "PDF document"}
		switch {
		case errors.Is(err, errPDFUnsupported):
			doc.Note = "This PDF uses features MOB³ does not support: " + err.Error() + "."
		default:
			doc.Note = "This PDF could not be parsed: " + err.Error() + "."
		}
		return doc
	}
	doc = &PDFDocument{Title: pdfInfoTitle(pdf)}
	if pdf.encrypted {
		doc.Encrypted = true
		doc.Note = "This PDF is encrypted. MOB³ cannot open password-protected documents."
		return doc
	}
	pages, err := pdf.pages()
	if err != nil || len(pages) == 0 {
		doc.Note = "This PDF has no readable pages."
		return doc
	}
	if pdf.truncated {
		doc.Partial = true
	}
	empty := true
	for _, p := range pages {
		runs := pdf.extract(p)
		spans := pdfPageLines(runs)
		for _, s := range spans {
			if strings.TrimSpace(s.Text) != "" {
				empty = false
				break
			}
		}
		doc.Pages = append(doc.Pages, PDFPageText{Number: p.Number, Spans: spans})
	}
	if doc.Partial {
		doc.Note = fmt.Sprintf("Document truncated after %d pages.", len(pages))
	}
	if pdf.textLimit {
		doc.Partial = true
		doc.Note = "Document text truncated at the extraction limit."
	}
	if empty {
		doc.Note = "This PDF has no extractable text. It may be a scan, or it may use fonts without a text mapping."
	} else if pdf.expired() {
		doc.Partial = true
		doc.Note = "Document extraction stopped at the time limit."
	}
	return doc
}

// pdfInfoTitle reads the document information dictionary's title.
func pdfInfoTitle(d *pdfDoc) string {
	info := d.dict(d.trailer["Info"])
	if info == nil {
		return ""
	}
	title, _ := d.resolve(info["Title"]).(string)
	return pdfTextString(title)
}

// pdfTextString decodes a PDF text string, which is either UTF-16BE with a
// byte-order mark or a single-byte encoding.
func pdfTextString(s string) string {
	if len(s) >= 2 && s[0] == 0xFE && s[1] == 0xFF {
		return sanitizePDFText(pdfUnicodeValue(s[2:]))
	}
	if len(s) >= 2 && s[0] == 0xFF && s[1] == 0xFE {
		var b strings.Builder
		for i := 2; i+1 < len(s); i += 2 {
			b.WriteRune(rune(uint16(s[i]) | uint16(s[i+1])<<8))
		}
		return sanitizePDFText(b.String())
	}
	return strings.Join(strings.Fields(sanitizePDFText(s)), " ")
}

// pdfDisplayName derives a short label from the URL's last path segment.
func pdfDisplayName(u *url.URL) string {
	if u == nil {
		return ""
	}
	base := path.Base(u.Path)
	if base == "." || base == "/" || base == "" {
		return ""
	}
	if unescaped, err := url.PathUnescape(base); err == nil {
		base = unescaped
	}
	return base
}

// pdfStyle is the generated page's stylesheet. The reading column follows the
// terminal width, and page rules separate the extracted pages.
const pdfStyle = `
body { margin: 12px 16px; }
.pdf-head { color: #666666; font-size: 12px; margin: 0 0 8px; }
.pdf-note { color: #8a4b00; font-size: 14px; margin: 4px 0 10px; }
.pdf-page { border-top: 1px solid #d8d8d8; margin-top: 12px; padding-top: 6px; }
.pdf-label { color: #888888; font-size: 12px; margin: 0 0 4px; }
pre.pdf-text { white-space: pre-wrap; font-size: 14px; line-height: 1.3; margin: 0; }
`

// pdfPageHTML renders the extracted document as a normal HTML page.
func pdfPageHTML(doc *PDFDocument, finalURL *url.URL, status int) string {
	var b strings.Builder
	title := doc.Title
	if title == "" {
		title = pdfDisplayName(finalURL)
	}
	if title == "" {
		title = "PDF document"
	}
	b.WriteString("<!DOCTYPE html><html><head><title>")
	b.WriteString(EscapeHTML(title))
	b.WriteString("</title><style>")
	b.WriteString(pdfStyle)
	b.WriteString("</style></head><body>")

	b.WriteString(`<p class="pdf-head">PDF · `)
	b.WriteString(plural(len(doc.Pages), "page", "pages"))
	b.WriteString(" · text only")
	if name := pdfDisplayName(finalURL); name != "" {
		b.WriteString(" · ")
		b.WriteString(EscapeHTML(name))
	}
	if status >= 400 {
		b.WriteString(" · HTTP ")
		b.WriteString(fmt.Sprint(status))
	}
	b.WriteString(`</p>`)

	if doc.Note != "" {
		b.WriteString(`<p class="pdf-note">`)
		b.WriteString(EscapeHTML(doc.Note))
		b.WriteString(`</p>`)
	}
	if (doc.Encrypted || len(doc.Pages) == 0) && finalURL != nil &&
		(finalURL.Scheme == "http" || finalURL.Scheme == "https") {
		b.WriteString(`<p class="pdf-note"><a href="`)
		b.WriteString(EscapeHTML(finalURL.String()))
		b.WriteString(`" download>Copy a curl command to download this PDF</a></p>`)
	}

	for _, page := range doc.Pages {
		b.WriteString(`<div class="pdf-page"><p class="pdf-label">Page `)
		b.WriteString(fmt.Sprint(page.Number))
		b.WriteString(" of ")
		b.WriteString(fmt.Sprint(len(doc.Pages)))
		b.WriteString(`</p><pre class="pdf-text">`)
		if strings.TrimSpace(pdfSpansText(page.Spans)) == "" {
			b.WriteString(`[no extractable text on this page]`)
		} else {
			b.WriteString(pdfSpansHTML(page.Spans))
		}
		b.WriteString(`</pre></div>`)
	}
	b.WriteString(`</body></html>`)
	return b.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprint(n) + " " + many
}

func pdfSpansText(spans []pdfSpan) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

// pdfSpansHTML writes spans with bold/italic markup. Spans were merged by
// style when the line was reconstructed, so tags stay balanced and compact.
func pdfSpansHTML(spans []pdfSpan) string {
	var b strings.Builder
	for _, s := range spans {
		if s.Text == "" {
			continue
		}
		switch {
		case s.Bold && s.Italic:
			b.WriteString("<b><i>")
		case s.Bold:
			b.WriteString("<b>")
		case s.Italic:
			b.WriteString("<i>")
		}
		b.WriteString(EscapeHTML(s.Text))
		switch {
		case s.Bold && s.Italic:
			b.WriteString("</i></b>")
		case s.Bold:
			b.WriteString("</b>")
		case s.Italic:
			b.WriteString("</i>")
		}
	}
	return b.String()
}
