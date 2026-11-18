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
	"math"
	"net/url"
	"path"
	"sort"
	"strings"
)

// PDFDocument is the extracted text of one PDF response.
type PDFDocument struct {
	Title string
	Pages []PDFPageText
	// PageNodes are the rendered page blocks, in order. The browser uses them
	// to report which page is under the viewport in the footer.
	PageNodes []*Node
	// PageRows is the uniform text height of a page in terminal rows, and
	// PageCells the widest line. Together they make every page the same size
	// and center the reading column like a printed page.
	PageRows  int
	PageCells int
	// HasText reports that at least one page yielded text. When it is false the
	// collection is a scan or uses unmapped fonts, so each page keeps a
	// placeholder; a text document renders figure-only pages as blank pages.
	HasText bool
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
	// Extract every page first so the document's body font size is known
	// before spans are classified as headings.
	empty := true
	var allRuns [][]pdfRun
	var numbers []int
	for _, p := range pages {
		runs := pdf.extract(p)
		allRuns = append(allRuns, runs)
		numbers = append(numbers, p.Number)
		for _, r := range runs {
			if strings.TrimSpace(r.Text) != "" {
				empty = false
			}
		}
	}
	bodySize := pdfBodySize(allRuns)
	for i, runs := range allRuns {
		spans := pdfPageLines(runs, bodySize)
		doc.Pages = append(doc.Pages, PDFPageText{Number: numbers[i], Spans: spans})
	}
	doc.PageRows, doc.PageCells = pdfPageBox(doc.Pages)
	if doc.Partial {
		doc.Note = fmt.Sprintf("Document truncated after %d pages.", len(pages))
	}
	if pdf.textLimit {
		doc.Partial = true
		doc.Note = "Document text truncated at the extraction limit."
	}
	doc.HasText = !empty
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

// pdfStyle is the generated page's stylesheet. The viewport holds the page
// text and a separator after each page; the page count and file name live in
// the browser footer instead. The %d is the page width in CSS pixels, derived
// from the widest line so the reading column is centered like a printed page.
const pdfStyle = `
body { margin: 12px 16px; }
.pdf-note { color: #8a4b00; font-size: 14px; margin: 4px 0 10px; }
.pdf-page { width: 100%%; max-width: %dpx; margin: 0 auto 12px; padding: 0; }
pre.pdf-text { white-space: pre-wrap; font-size: 14px; line-height: 1.3; margin: 0; padding: 0; background: transparent; }
.pdf-page-no { color: #9a9a9a; font-size: 12px; text-align: right; margin: 4px 0 0; }
pre.pdf-rule { color: #b3b3b3; font-size: 14px; line-height: 1.3; margin: 0; padding: 0; background: transparent; white-space: nowrap; overflow: hidden; }
`

// pdfPageHTML renders the extracted document as a normal HTML page.
func pdfPageHTML(doc *PDFDocument, finalURL *url.URL) string {
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
	b.WriteString(fmt.Sprintf(pdfStyle, pdfPageWidth(doc.PageCells)))
	b.WriteString("</style></head><body>")

	// With pages present the body holds only page text: the per-page
	// placeholder already explains empty pages, and the footer carries the
	// page count, file name and download hint. Truncation still gets a note so
	// a cap is never silent.
	if doc.Note != "" && (len(doc.Pages) == 0 || doc.Partial) {
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
		b.WriteString(`<div class="pdf-page"><pre class="pdf-text">`)
		placeholder := !doc.HasText && strings.TrimSpace(pdfSpansText(page.Spans)) == ""
		if placeholder {
			b.WriteString(`[no extractable text on this page]`)
		} else {
			b.WriteString(pdfSpansHTML(page.Spans))
		}
		// Pad every page to the same number of rows so the document scrolls
		// like a book instead of jumping between differently sized pages.
		pad := doc.PageRows - pdfPageRows(page.Spans)
		if placeholder {
			pad--
		}
		if pad > 0 {
			b.WriteString(strings.Repeat("\n", pad))
		}
		b.WriteString(`</pre><p class="pdf-page-no">Page `)
		b.WriteString(fmt.Sprint(page.Number))
		b.WriteString(`</p><pre class="pdf-rule">`)
		b.WriteString(strings.Repeat("_", pdfPageRuleCells(doc.PageCells)))
		b.WriteString(`</pre></div>`)
	}
	b.WriteString(`</body></html>`)
	return b.String()
}

func pdfSpansText(spans []pdfSpan) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

// pdfSpansHTML writes spans with font-size and bold/italic markup. Spans were
// merged by style when the line was reconstructed, so tags stay balanced and
// compact.
func pdfSpansHTML(spans []pdfSpan) string {
	var b strings.Builder
	for _, s := range spans {
		if s.Text == "" {
			continue
		}
		var opens, closes []string
		if s.Size > 0 {
			opens = append(opens, fmt.Sprintf(`<span style="font-size:%dpx">`, s.Size))
			closes = append(closes, "</span>")
		}
		if s.Bold {
			opens = append(opens, "<b>")
			closes = append(closes, "</b>")
		}
		if s.Italic {
			opens = append(opens, "<i>")
			closes = append(closes, "</i>")
		}
		b.WriteString(strings.Join(opens, ""))
		b.WriteString(EscapeHTML(s.Text))
		for i := len(closes) - 1; i >= 0; i-- {
			b.WriteString(closes[i])
		}
	}
	return b.String()
}

// pdfPageWidth converts the widest extracted line into a centered page width.
func pdfPageWidth(cells int) int {
	width := cells*8 + 32
	if width < 320 {
		width = 320
	}
	if width > 1600 {
		width = 1600
	}
	return width
}

// pdfPageRows counts the terminal rows a page's spans occupy, including the
// extra rows a 32px headline line takes.
func pdfPageRows(spans []pdfSpan) int {
	rows, line := 0, 1
	for _, s := range spans {
		if s.Text == "\n" {
			rows += line
			line = 1
			continue
		}
		px := s.Size
		if px == 0 {
			px = pdfBodyPx
		}
		if r := int(float64(px) * 1.3 / 16); r > line {
			line = r
		}
	}
	if line > 1 {
		rows += line
	}
	return rows
}

// pdfBodySize returns the text-weighted median font size of the document.
func pdfBodySize(pages [][]pdfRun) float64 {
	weights := map[float64]int{}
	total := 0
	for _, runs := range pages {
		for _, r := range runs {
			n := len([]rune(r.Text))
			if n == 0 {
				continue
			}
			key := math.Round(r.Size*4) / 4
			weights[key] += n
			total += n
		}
	}
	if total == 0 {
		return 11
	}
	keys := make([]float64, 0, len(weights))
	for k := range weights {
		keys = append(keys, k)
	}
	sort.Float64s(keys)
	half, acc := total/2, 0
	for _, k := range keys {
		acc += weights[k]
		if acc >= half {
			return k
		}
	}
	return keys[len(keys)-1]
}

// pdfPageBox returns the uniform page height and the widest line of a
// document, both in terminal cells. The height is capped so one pathological
// page cannot make every other page hundreds of rows tall.
func pdfPageBox(pages []PDFPageText) (rows, cells int) {
	for _, page := range pages {
		width, cur := 0, 0
		for _, s := range page.Spans {
			text := s.Text
			for {
				i := strings.IndexByte(text, '\n')
				if i < 0 {
					cur += stringWidth(text)
					break
				}
				cur += stringWidth(text[:i])
				if cur > width {
					width = cur
				}
				cur = 0
				text = text[i+1:]
			}
		}
		if cur > width {
			width = cur
		}
		if r := pdfPageRows(page.Spans); r > rows {
			rows = r
		}
		if width > cells {
			cells = width
		}
	}
	if rows > 200 {
		rows = 200
	}
	if cells > 200 {
		cells = 200
	}
	return rows, cells
}

// pdfPageRuleCells returns the length of the underscore rule under each page,
// matching the page box width in terminal cells.
func pdfPageRuleCells(cells int) int {
	n := cells + 4
	if n < 40 {
		n = 40
	}
	if n > 204 {
		n = 204
	}
	return n
}
