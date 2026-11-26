package engine

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"browser/internal/textwidth"
)

// pdfBuild assembles a PDF with a classic cross-reference table. Objects are
// given as bodies without their "N 0 obj" header, in object-number order.
func pdfBuild(trailer string, objs ...string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(objs))
	for i, body := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n", len(objs)+1)
	b.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R %s>>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, trailer, xref)
	return b.Bytes()
}

// pdfStreamBody wraps data as an indirect stream object with an extra dict
// fragment such as "/Filter /FlateDecode ".
func pdfStreamBody(extra string, data []byte) string {
	return fmt.Sprintf("<< %s/Length %d >>\nstream\n%s\nendstream", extra, len(data), data)
}

func pdfSimpleDoc(content string, extraTrailer string, extraObjs ...string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		pdfStreamBody("", []byte(content)),
	}
	objs = append(objs, extraObjs...)
	return pdfBuild(extraTrailer, objs...)
}

func TestPDFExtractsText(t *testing.T) {
	data := pdfSimpleDoc("BT /F1 12 Tf 20 180 Td (Hello PDF) Tj ET",
		"/Info 6 0 R ",
		"<< /Title (Quarterly report) >>")
	doc := BuildPDFDocument(data)
	if len(doc.Pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(doc.Pages))
	}
	if got := strings.TrimSpace(doc.Text()); got != "Hello PDF" {
		t.Fatalf("text = %q", got)
	}
	if doc.Title != "Quarterly report" {
		t.Fatalf("title = %q", doc.Title)
	}
	if doc.Note != "" {
		t.Fatalf("unexpected note %q", doc.Note)
	}
}

func TestPDFKeepsColumnAlignment(t *testing.T) {
	// Two runs share a baseline; the second starts 120pt to the right, which
	// is 20 terminal cells at 6pt per cell.
	data := pdfSimpleDoc("BT /F1 10 Tf 20 180 Td (Name) Tj 120 0 Td (Value) Tj ET", "")
	doc := BuildPDFDocument(data)
	text := doc.Text()
	line := strings.SplitN(text, "\n", 2)[0]
	if !strings.HasPrefix(line, "Name") {
		t.Fatalf("line = %q", line)
	}
	idx := strings.Index(line, "Value")
	if idx < 0 {
		t.Fatalf("value run missing: %q", line)
	}
	if idx != 20 {
		t.Fatalf("value column = %d, want 20 (%q)", idx, line)
	}
}

func TestPDFMultiplePagesAndStyles(t *testing.T) {
	data := pdfBuild("",
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>",
		pdfStreamBody("", []byte("BT /F1 12 Tf 20 180 Td (First page) Tj ET")),
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 7 0 R >>",
		pdfStreamBody("", []byte("BT /F1 12 Tf 20 180 Td (Second page) Tj ET")),
	)
	doc := BuildPDFDocument(data)
	if len(doc.Pages) != 2 {
		t.Fatalf("pages = %d, want 2", len(doc.Pages))
	}
	if !strings.Contains(doc.Pages[0].Spans[0].Text, "First page") {
		t.Fatalf("page 1 = %q", doc.Pages[0].Spans[0].Text)
	}
	if !doc.Pages[0].Spans[0].Bold {
		t.Fatal("bold font name was not detected")
	}
	if !strings.Contains(doc.Text(), "Second page") {
		t.Fatalf("text = %q", doc.Text())
	}
}

func TestPDFFlateContent(t *testing.T) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	zw.Write([]byte("BT /F1 12 Tf 20 180 Td (Compressed text) Tj ET"))
	zw.Close()
	data := pdfBuild("",
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		pdfStreamBody("/Filter /FlateDecode ", buf.Bytes()),
	)
	doc := BuildPDFDocument(data)
	if !strings.Contains(doc.Text(), "Compressed text") {
		t.Fatalf("text = %q", doc.Text())
	}
}

// pdfXrefStreamDoc builds a PDF 1.5 document whose catalog and page tree live
// in an object stream and whose cross-reference is an xref stream.
func pdfXrefStreamDoc(content string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.5\n%\xe2\xe3\xcf\xd3\n")
	off5 := b.Len()
	fmt.Fprintf(&b, "5 0 obj\n%s\nendobj\n", pdfStreamBody("", []byte(content)))

	members := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var header, body strings.Builder
	for i, m := range members {
		fmt.Fprintf(&header, "%d %d ", i+1, body.Len())
		body.WriteString(m)
		body.WriteString("\n")
	}
	objStm := header.String() + body.String()
	off6 := b.Len()
	fmt.Fprintf(&b, "6 0 obj\n%s\nendobj\n",
		pdfStreamBody(fmt.Sprintf("/Type /ObjStm /N %d /First %d ", len(members), header.Len()), []byte(objStm)))

	off7 := b.Len()
	row := func(typ byte, f2 uint32, f3 uint16) []byte {
		return []byte{typ, byte(f2 >> 24), byte(f2 >> 16), byte(f2 >> 8), byte(f2), byte(f3 >> 8), byte(f3)}
	}
	var x []byte
	x = append(x, row(0, 0, 65535)...)
	x = append(x, row(2, 6, 0)...)
	x = append(x, row(2, 6, 1)...)
	x = append(x, row(2, 6, 2)...)
	x = append(x, row(2, 6, 3)...)
	x = append(x, row(1, uint32(off5), 0)...)
	x = append(x, row(1, uint32(off6), 0)...)
	x = append(x, row(1, uint32(off7), 0)...)
	fmt.Fprintf(&b, "7 0 obj\n%s\nendobj\n",
		pdfStreamBody("/Type /XRef /Size 8 /W [1 4 2] /Root 1 0 R ", x))
	fmt.Fprintf(&b, "startxref\n%d\n%%%%EOF\n", off7)
	return b.Bytes()
}

func TestPDFObjectStreamAndXrefStream(t *testing.T) {
	doc := BuildPDFDocument(pdfXrefStreamDoc("BT /F1 12 Tf 20 180 Td (Packed objects) Tj ET"))
	if len(doc.Pages) != 1 {
		t.Fatalf("pages = %d, note=%q", len(doc.Pages), doc.Note)
	}
	if !strings.Contains(doc.Text(), "Packed objects") {
		t.Fatalf("text = %q", doc.Text())
	}
}

func TestPDFRecoversDamagedXref(t *testing.T) {
	data := pdfSimpleDoc("BT /F1 12 Tf 20 180 Td (Recovered) Tj ET", "")
	// Drop the cross-reference table and trailer entirely; the reader must
	// find the objects by scanning and locate the catalog.
	cut := bytes.Index(data, []byte("xref\n0 "))
	if cut < 0 {
		t.Fatal("test PDF has no xref")
	}
	doc := BuildPDFDocument(data[:cut])
	if !strings.Contains(doc.Text(), "Recovered") {
		t.Fatalf("text = %q, note=%q", doc.Text(), doc.Note)
	}
}

func TestPDFEncrypted(t *testing.T) {
	data := pdfSimpleDoc("BT /F1 12 Tf 20 180 Td (Hidden) Tj ET",
		"/Encrypt 6 0 R ",
		"<< /Filter /Standard /V 1 /R 2 >>")
	doc := BuildPDFDocument(data)
	if !doc.Encrypted {
		t.Fatal("encrypted flag not set")
	}
	if len(doc.Pages) != 0 {
		t.Fatalf("encrypted document produced %d pages", len(doc.Pages))
	}
	if !strings.Contains(doc.Note, "encrypted") {
		t.Fatalf("note = %q", doc.Note)
	}
}

func TestPDFToUnicode(t *testing.T) {
	cmap := `/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
1 begincodespacerange
<0000> <FFFF>
endcodespacerange
1 beginbfchar
<0003> <0021>
endbfchar
1 beginbfrange
<0001> <0002> <0048>
endbfrange
endcmap
CMapName currentdict /CMap defineresource pop
end
end`
	data := pdfBuild("",
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /Identity-H /DescendantFonts [6 0 R] /ToUnicode 7 0 R >>",
		pdfStreamBody("", []byte("BT /F1 12 Tf 20 180 Td <000100020003> Tj ET")),
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /DW 500 /W [1 [600]] >>",
		pdfStreamBody("", []byte(cmap)),
	)
	doc := BuildPDFDocument(data)
	if got := strings.TrimSpace(doc.Text()); got != "HI!" {
		t.Fatalf("text = %q, note=%q", got, doc.Note)
	}
}

func TestPDFSkipsInlineImages(t *testing.T) {
	// The inline image payload contains text operators; the extractor must
	// skip them rather than treating the binary data as page text.
	content := "BT /F1 12 Tf 20 180 Td (Before) Tj ET\n" +
		"BI /W 4 /H 4 /BPC 8 /CS /G ID \x00\x01BT /F1 12 Tf 0 0 Td (GARBAGE) Tj ET\x02 EI\n" +
		"BT /F1 12 Tf 20 150 Td (After) Tj ET"
	doc := BuildPDFDocument(pdfSimpleDoc(content, ""))
	text := doc.Text()
	if !strings.Contains(text, "Before") || !strings.Contains(text, "After") {
		t.Fatalf("text = %q", text)
	}
	if strings.Contains(text, "GARBAGE") {
		t.Fatalf("inline image bytes were extracted as text: %q", text)
	}
}

func TestPDFNoExtractableText(t *testing.T) {
	doc := BuildPDFDocument(pdfSimpleDoc("0 0 m 100 100 l S", ""))
	if len(doc.Pages) != 1 {
		t.Fatalf("pages = %d", len(doc.Pages))
	}
	if !strings.Contains(doc.Note, "no extractable text") {
		t.Fatalf("note = %q", doc.Note)
	}
}

func TestPDFUnsupportedFilter(t *testing.T) {
	data := pdfBuild("",
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R >>",
		pdfStreamBody("/Filter /JPXDecode ", []byte("junk")),
	)
	doc := BuildPDFDocument(data)
	if !strings.Contains(doc.Note, "no extractable text") {
		t.Fatalf("note = %q", doc.Note)
	}
}

func TestPDFLoadEndToEnd(t *testing.T) {
	data := pdfSimpleDoc("BT /F1 12 Tf 20 180 Td (Loaded from a data URL) Tj ET", "")
	loader, target := fixtureLoader(t, []byte(data), "application/pdf")
	loader.NoImages = true
	page, err := loader.Load(target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.PDF == nil {
		t.Fatal("page was not opened in the PDF viewer")
	}
	if page.Download {
		t.Fatal("PDF was marked as a download")
	}
	if !strings.Contains(page.PDF.Text(), "Loaded from a data URL") {
		t.Fatalf("text = %q", page.PDF.Text())
	}
	ly := BuildLayout(page.Root, page.Author, 100)
	var text strings.Builder
	for _, f := range ly.Frags {
		if f.Kind == FragText {
			text.WriteString(f.Text)
			text.WriteString(" ")
		}
	}
	rendered := strings.Join(strings.Fields(text.String()), " ")
	if strings.Contains(rendered, "PDF ·") || strings.Contains(rendered, "Page 1 of 1") {
		t.Fatalf("page chrome leaked into the viewport: %q", rendered)
	}
	if !strings.Contains(rendered, "Loaded from a data URL") {
		t.Fatalf("rendered layout lacks the page text: %q", rendered)
	}
	if len(page.PDF.PageNodes) != 1 {
		t.Fatalf("page nodes = %d, want 1", len(page.PDF.PageNodes))
	}
}

func TestPDFDisabledFallsBackToDownload(t *testing.T) {
	data := pdfSimpleDoc("BT /F1 12 Tf 20 180 Td (Ignored) Tj ET", "")
	loader, docURL := fixtureLoader(t, []byte(data), "application/pdf")
	loader.NoImages = true
	loader.NoPDF = true
	page, err := loader.Load(docURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.PDF != nil {
		t.Fatal("PDF viewer ran with -no-pdf")
	}
	ly := BuildLayout(page.Root, page.Author, 100)
	var text strings.Builder
	for _, f := range ly.Frags {
		if f.Kind == FragText {
			text.WriteString(f.Text)
			text.WriteString(" ")
		}
	}
	rendered := strings.Join(strings.Fields(text.String()), " ")
	if !strings.Contains(rendered, "Cannot display") {
		t.Fatalf("fallback page = %.160q", rendered)
	}
}

func TestPDFInfoTextString(t *testing.T) {
	// UTF-16BE with BOM, as written by many generators.
	title := "\xfe\xff\x00H\x00i\x00 \x00P\x00D\x00F"
	if got := PDFTextString(title); got != "Hi PDF" {
		t.Fatalf("title = %q", got)
	}
	if got := PDFTextString("Plain title"); got != "Plain title" {
		t.Fatalf("title = %q", got)
	}
}

func TestPDFLineSpacingDoesNotSplitGlyphs(t *testing.T) {
	// Per-glyph runs that touch must join without spaces, while a real gap
	// becomes one space and a column jump becomes padding.
	runs := []PDFRun{
		{Text: "T", X: 0, Y: 100, W: 5, Size: 10},
		{Text: "h", X: 5, Y: 100, W: 5, Size: 10},
		{Text: "e", X: 10, Y: 100, W: 5, Size: 10},
	}
	if got := PDFSpansText(PDFPageLines(runs, 0)); got != "The\n" {
		t.Fatalf("touching runs = %q", got)
	}
	runs = append(runs, PDFRun{Text: "word", X: 20, Y: 100, W: 20, Size: 10})
	if got := PDFSpansText(PDFPageLines(runs, 0)); got != "The word\n" {
		t.Fatalf("gap run = %q", got)
	}
	runs = append(runs, PDFRun{Text: "cell", X: 120, Y: 100, W: 20, Size: 10})
	if got := PDFSpansText(PDFPageLines(runs, 0)); got != "The word            cell\n" {
		t.Fatalf("column run = %q", got)
	}
}

func TestPDFRejectsDeepNesting(t *testing.T) {
	deep := strings.Repeat("[", 200) + strings.Repeat("]", 200)
	doc, err := ParsePDF(pdfBuild("",
		"<< /Type /Catalog /Pages 2 0 R >>",
		deep))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Object(2); err == nil {
		t.Fatal("deeply nested array was accepted")
	}
}

func TestPDFMalformedInputDoesNotPanic(t *testing.T) {
	for _, data := range [][]byte{
		[]byte("%PDF-1.7\n"),
		[]byte("%PDF-1.7\n1 0 obj << /Type /Catalog /Pages 2 0 R"),
		[]byte("%PDF-1.4\n%%EOF\n"),
		[]byte("%PDF-1.7\n1 0 obj << /Length 999999999 >>\nstream\nx\nendstream\nendobj\n"),
	} {
		doc := BuildPDFDocument(data)
		if doc == nil {
			t.Fatal("nil document")
		}
		if len(doc.Pages) != 0 && doc.Note == "" && doc.Text() == "" {
			t.Fatalf("silent failure for %.40q", data)
		}
	}
}

func TestPDFReaderModeKeepsDocument(t *testing.T) {
	data := pdfSimpleDoc("BT /F1 12 Tf 20 180 Td (Reader keeps pages) Tj ET", "")
	loader, docURL := fixtureLoader(t, []byte(data), "application/pdf")
	page, err := loader.Load(docURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	b := newTestSession(80, 24)
	b.setLoader(loader)
	b.Reader = true
	b.finishLoad(LoadResult{Page: page, URL: page.URL})
	if b.ReaderRoot != nil {
		t.Fatal("reader mode extracted the PDF page")
	}
	if b.status != "Reader mode unavailable" {
		t.Fatalf("status = %q", b.status)
	}
	if text := strings.Join(strings.Fields(layoutText(b.Layout)), " "); !strings.Contains(text, "Reader keeps pages") {
		t.Fatalf("PDF text missing from layout: %q", text)
	}
}

func TestPDFEscapesMarkup(t *testing.T) {
	doc := BuildPDFDocument(pdfSimpleDoc("BT /F1 12 Tf 20 180 Td (a < b & c) Tj ET", ""))
	html := PDFPageHTML(doc, nil)
	if strings.Contains(html, "a < b & c") {
		t.Fatal("page text was not escaped")
	}
	if !strings.Contains(html, "a &lt; b &amp; c") {
		t.Fatalf("escaped text missing: %.200q", html)
	}
}

func TestPDFFooterLineAndPageTracking(t *testing.T) {
	u, _ := url.Parse("https://example.com/files/file-example_PDF_500_kB.pdf")
	doc := &PDFDocument{Pages: make([]PDFPageText, 5)}
	for i := range doc.Pages {
		doc.Pages[i].Number = i + 1
	}
	root := ParseHTML(`<html><body></body></html>`)
	b := newTestSession(100, 20)
	b.Page = &Page{URL: u, Base: u, Root: root, PDF: doc}
	b.PDFStarts = []int{0, 40, 80, 120, 160}

	text, hint, cell, ok := b.pdfFooterLine(120)
	if !ok {
		t.Fatal("no footer line for a PDF page")
	}
	want := " Page 1/5 (S -> Download file-example_PDF_500_kB.pdf)"
	if text != want {
		t.Fatalf("footer = %q, want %q", text, want)
	}
	if hint != "(S -> Download file-example_PDF_500_kB.pdf)" {
		t.Fatalf("hint = %q", hint)
	}
	if cell != textwidth.StringWidth(" Page 1/5")+1 {
		t.Fatalf("hint cell = %d", cell)
	}

	// Scrolling into the third page moves the counter.
	b.Scroll = 90
	if got := b.PDFPageNumber(); got != 3 {
		t.Fatalf("page at scroll 90 = %d, want 3", got)
	}
	b.Scroll = 1000
	if got := b.PDFPageNumber(); got != 5 {
		t.Fatalf("page at bottom = %d, want 5", got)
	}

	// Narrow footers shorten the name, then fall back to a compact hint.
	b.Scroll = 0
	text, _, _, _ = b.pdfFooterLine(45)
	if text != " Page 1/5 (S -> Download file-example_PDF_5…)" {
		t.Fatalf("narrow footer = %q", text)
	}
	text, hint, _, _ = b.pdfFooterLine(20)
	if hint != "(S)" || text != " Page 1/5 (S)" {
		t.Fatalf("compact footer = %q (hint %q)", text, hint)
	}
	text, hint, _, _ = b.pdfFooterLine(10)
	if hint != "" || text != " Page 1/5" {
		t.Fatalf("very narrow footer = %q (hint %q)", text, hint)
	}
}

func TestPDFDownloadKey(t *testing.T) {
	u, _ := url.Parse("https://example.com/files/report.pdf")
	b := newTestSession(100, 20)
	b.Page = &Page{URL: u, Base: u, Root: ParseHTML(`<html><body></body></html>`), PDF: &PDFDocument{}}
	b.downloadCurrentPDF()
	if b.download == nil {
		t.Fatal("d did not open the download dialog")
	}
	if !strings.Contains(b.download.command, "curl -L -o") ||
		!strings.Contains(b.download.command, "https://example.com/files/report.pdf") {
		t.Fatalf("command = %q", b.download.command)
	}
}

func TestPDFDownloadKeyOnlyWorksInViewer(t *testing.T) {
	b := newTestSession(100, 20)
	b.Page = &Page{URL: mustParse(t, "https://example.com/page"), Root: ParseHTML(`<html><body>text</body></html>`)}
	b.status = ""
	b.downloadCurrentPDF()
	if b.download != nil {
		t.Fatal("d opened the download dialog outside the PDF viewer")
	}
	if b.status != "" {
		t.Fatalf("d changed the status outside the PDF viewer: %q", b.status)
	}
}

func TestPDFFooterDownloadHit(t *testing.T) {
	u, _ := url.Parse("https://example.com/files/report.pdf")
	root := ParseHTML(`<html><body><div class="pdf-page"><pre>line</pre></div></body></html>`)
	b := newTestSession(100, 12)
	b.Page = &Page{URL: u, Base: u, Root: root, PDF: &PDFDocument{Pages: []PDFPageText{{Number: 1}}}}
	b.Layout = &Layout{Height: 20}
	b.PDFStarts = []int{0}
	fm := b.footerModel()
	if !strings.Contains(fm.left, " Page 1/1 ") {
		t.Fatalf("PDF footer left = %q", fm.left)
	}
	if fm.pdfHint != "(S -> Download report.pdf)" || fm.pdfHintX < 0 {
		t.Fatalf("PDF footer hint = %q at %d", fm.pdfHint, fm.pdfHintX)
	}
}

func TestPDFDisplayName(t *testing.T) {
	u, _ := url.Parse("https://example.com/docs/quarterly%20report.pdf")
	if got := PDFDisplayName(u); got != "quarterly report.pdf" {
		t.Fatalf("name = %q", got)
	}
}

// TestPDFFixtures extracts checked-in PDF fixtures and compares the result
// with goldens, so changes to the reader's layout reconstruction are visible.
// Run with -update after an intentional change and review the diff.
func TestPDFFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/pdf/*.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no PDF fixtures found")
	}
	for _, file := range files {
		file := file
		name := strings.TrimSuffix(filepath.Base(file), ".pdf")
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			doc := BuildPDFDocument(data)
			var b strings.Builder
			for _, p := range doc.Pages {
				fmt.Fprintf(&b, "=== page %d\n", p.Number)
				for _, s := range p.Spans {
					b.WriteString(s.Text)
				}
				b.WriteString("\n")
			}
			got := b.String()
			textGolden := strings.TrimSuffix(file, ".pdf") + ".txt"

			// The rendered page must hold the text and separators only: no
			// in-page header or per-page labels.
			root := ParseHTML(PDFPageHTML(doc, nil))
			rendered := renderFixture(root, fixtureAuthorRules(root), 70)
			if strings.Contains(rendered, "Page 1 of") || strings.Contains(rendered, "PDF ·") {
				t.Errorf("%s: page chrome leaked into the viewport:\n%s", name, rendered)
			}
			layoutGolden := strings.TrimSuffix(file, ".pdf") + ".w70.txt"

			if *updateFixtures {
				for golden, content := range map[string]string{
					textGolden:   got,
					layoutGolden: rendered,
				} {
					if err := os.WriteFile(golden, []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				return
			}
			for golden, content := range map[string]string{
				textGolden:   got,
				layoutGolden: rendered,
			} {
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatalf("%s: %v (run go test -run TestPDFFixtures -update)", golden, err)
				}
				if content != string(want) {
					t.Errorf("%s mismatch\n--- want ---\n%s\n--- got ---\n%s", golden, want, content)
				}
			}
		})
	}
}

func TestPDFPagesShareHeight(t *testing.T) {
	data := pdfBuild("",
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		pdfStreamBody("", []byte("BT /F1 11 Tf 20 200 Td (Short page) Tj ET")),
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] /Resources << /Font << /F1 4 0 R >> >> /Contents 7 0 R >>",
		pdfStreamBody("", []byte("BT /F1 11 Tf 20 200 Td (Line one) Tj 0 -15 Td (Line two) Tj 0 -15 Td (Line three) Tj ET")),
	)
	doc := BuildPDFDocument(data)
	if len(doc.Pages) != 2 {
		t.Fatalf("pages = %d", len(doc.Pages))
	}
	if doc.PageRows != 3 {
		t.Fatalf("uniform page rows = %d, want 3", doc.PageRows)
	}
	html := PDFPageHTML(doc, nil)
	parts := strings.Split(html, `<pre class="pdf-text">`)
	if len(parts) != 3 {
		t.Fatalf("page blocks = %d", len(parts)-1)
	}
	for i, part := range parts[1:] {
		body := part[:strings.Index(part, "</pre>")]
		if got := strings.Count(body, "\n"); got != doc.PageRows {
			t.Fatalf("page %d has %d rows, want %d", i+1, got, doc.PageRows)
		}
	}
	// The page column is centered, not pinned to the left edge.
	if !strings.Contains(html, "margin: 0 auto") || !strings.Contains(html, "max-width: ") {
		t.Fatal("page column is not centered")
	}
	// Each separator carries its page number and an underscore rule.
	for i := 1; i <= 2; i++ {
		label := fmt.Sprintf(`<p class="pdf-page-no">Page %d</p>`, i)
		if !strings.Contains(html, label) {
			t.Fatalf("missing separator label %q", label)
		}
	}
	if got := strings.Count(html, `<pre class="pdf-rule">`); got != 2 {
		t.Fatalf("underscore rules = %d, want 2", got)
	}
	if !strings.Contains(html, strings.Repeat("_", 40)) {
		t.Fatal("separator rule is too short")
	}
	if strings.Contains(html, "border-bottom") {
		t.Fatal("page separator still uses a border")
	}
}

func TestPDFPageBox(t *testing.T) {
	page := func(lines ...string) PDFPageText {
		var spans []PDFSpan
		for _, l := range lines {
			spans = append(spans, PDFSpan{Text: l}, PDFSpan{Text: "\n"})
		}
		return PDFPageText{Spans: spans}
	}
	rows, cells := PDFPageBox([]PDFPageText{
		page("short"),
		page("a longer line here", "two", "three"),
	})
	if rows != 3 {
		t.Fatalf("rows = %d, want 3", rows)
	}
	if cells != len("a longer line here") {
		t.Fatalf("cells = %d", cells)
	}
}

func TestPDFBlankFigurePagesStayBlank(t *testing.T) {
	// A text document with a figure-only page renders that page blank instead
	// of stamping a placeholder on every illustration.
	data := pdfBuild("",
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		pdfStreamBody("", []byte("BT /F1 11 Tf 20 200 Td (Text page) Tj ET")),
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] /Contents 7 0 R >>",
		pdfStreamBody("", []byte("0 0 m 100 100 l S")),
	)
	doc := BuildPDFDocument(data)
	if !doc.HasText {
		t.Fatal("document with text was marked as scan")
	}
	html := PDFPageHTML(doc, nil)
	if strings.Count(html, "[no extractable text on this page]") != 0 {
		t.Fatalf("figure page got a placeholder:\n%s", html)
	}
}

func TestPDFHeadlinesGetHeadlineSize(t *testing.T) {
	data := pdfBuild("",
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 400] /Resources << /Font << /F1 4 0 R /F2 6 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		pdfStreamBody("", []byte(
			"BT /F2 17 Tf 20 280 Td (Big title) Tj ET "+
				"BT /F1 11 Tf 20 230 Td (Body text) Tj 0 -13 Td (More body) Tj 0 -13 Td (Last body) Tj ET")),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>",
	)
	doc := BuildPDFDocument(data)
	var spans []PDFSpan
	for _, page := range doc.Pages {
		spans = append(spans, page.Spans...)
	}
	headline := false
	for _, s := range spans {
		if strings.Contains(s.Text, "Big title") {
			headline = s.Size == pdfHeadlinePx
		}
	}
	if !headline {
		t.Fatal("large PDF heading was not given the headline size")
	}
	// The heading is followed by the blank lines its layout gap implies.
	text := PDFSpansText(spans)
	if !strings.Contains(text, "Big title\n\n") {
		t.Fatalf("no spacing after heading: %q", text)
	}
	if strings.Contains(text, "Body text\n\n") {
		t.Fatal("body lines gained spurious blank lines")
	}
	// A headline line counts as two rows.
	rows := PDFPageRows(spans)
	lines := strings.Count(text, "\n")
	if rows <= lines {
		t.Fatalf("headline rows = %d, plain lines = %d", rows, lines)
	}
}

func TestPDFDedupesDoubleDrawnText(t *testing.T) {
	// A shadowed/fake-bold heading is drawn twice at a sub-point offset; only
	// one copy belongs in the text.
	content := "BT /F1 33 Tf 57.35 700 Td (Invoice) Tj ET " +
		"BT /F1 33 Tf 56.69 700 Td (Invoice) Tj ET"
	doc := BuildPDFDocument(pdfSimpleDoc(content, ""))
	if got := strings.TrimSpace(doc.Text()); got != "Invoice" {
		t.Fatalf("text = %q", got)
	}
}

func TestPDFSpaceGlyphAndGapDoNotDouble(t *testing.T) {
	// Justified text often carries a space glyph plus a TJ gap; the gap must
	// not add a second space.
	doc := BuildPDFDocument(pdfSimpleDoc("BT /F1 11 Tf 20 700 Td [(Hello ) -500 (world)] TJ ET", ""))
	if got := strings.TrimSpace(doc.Text()); got != "Hello world" {
		t.Fatalf("text = %q", got)
	}
}

func TestPDFTableKeepsRowOrder(t *testing.T) {
	var content strings.Builder
	for row := 0; row < 6; row++ {
		y := 700 - row*14
		for col, x := range []int{40, 110, 260} {
			fmt.Fprintf(&content, "BT /F1 11 Tf %d %d Td (R%dC%d) Tj ET ", x, y, row, col)
		}
	}
	doc := BuildPDFDocument(pdfSimpleDoc(content.String(), ""))
	text := doc.Text()
	for row := 0; row < 6; row++ {
		line := fmt.Sprintf("R%dC0", row)
		if !strings.Contains(text, line) {
			t.Fatalf("row %d missing: %q", row, text)
		}
	}
	// Cells stay in row order: C0 before C1 before C2 on the same line.
	lines := strings.Split(text, "\n")
	for _, l := range lines {
		if !strings.Contains(l, "R0C0") {
			continue
		}
		if strings.Index(l, "R0C0") > strings.Index(l, "R0C1") ||
			strings.Index(l, "R0C1") > strings.Index(l, "R0C2") {
			t.Fatalf("table row was reordered: %q", l)
		}
	}
}

// pdfTwoPageFixture builds a two-page document whose pages have different
// text lengths, so page-box and selection behaviour can be checked.
func pdfTwoPageFixture() []byte {
	return pdfBuild("",
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		pdfStreamBody("", []byte("BT /F1 11 Tf 20 200 Td (Short page) Tj ET")),
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] /Resources << /Font << /F1 4 0 R >> >> /Contents 7 0 R >>",
		pdfStreamBody("", []byte("BT /F1 11 Tf 20 200 Td (Line one) Tj 0 -15 Td (Line two) Tj 0 -15 Td (Line three) Tj ET")),
	)
}

func TestPDFPageSelection(t *testing.T) {
	load := func(page int) *Page {
		loader, target := fixtureLoader(t, []byte(pdfTwoPageFixture()), "application/pdf")
		loader.NoImages = true
		loader.PDFPage = page
		p, err := loader.Load(target, nil)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	full := load(0)
	if full.PDF == nil || full.PDF.TotalPages != 2 || len(full.PDF.Pages) != 2 {
		t.Fatalf("full document: total=%v pages=%d", full.PDF.TotalPages, len(full.PDF.Pages))
	}
	if full.PDF.PageRows != 3 {
		t.Fatalf("page rows = %d, want 3", full.PDF.PageRows)
	}

	second := load(2)
	if second.PDF.TotalPages != 2 || len(second.PDF.Pages) != 1 || second.PDF.Pages[0].Number != 2 {
		t.Fatalf("page 2 selection: total=%d pages=%d", second.PDF.TotalPages, len(second.PDF.Pages))
	}
	if second.PDF.PageRows != full.PDF.PageRows {
		t.Fatalf("selected page changed geometry: %d vs %d", second.PDF.PageRows, full.PDF.PageRows)
	}
	if text := second.PDF.Text(); !strings.Contains(text, "Line three") || strings.Contains(text, "Short page") {
		t.Fatalf("page 2 text = %q", text)
	}
	if len(second.PDF.PageNodes) != 1 {
		t.Fatalf("page nodes = %d", len(second.PDF.PageNodes))
	}

	first := load(1)
	if text := first.PDF.Text(); !strings.Contains(text, "Short page") || strings.Contains(text, "Line three") {
		t.Fatalf("page 1 text = %q", text)
	}

	missing := load(9)
	if missing.PDF == nil || len(missing.PDF.Pages) != 0 || !strings.Contains(missing.PDF.Note, "does not exist") {
		t.Fatalf("out-of-range note = %q pages=%d", missing.PDF.Note, len(missing.PDF.Pages))
	}
}

func TestPDFContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := &PDFDoc{ctx: ctx, deadline: time.Now().Add(time.Hour)}
	if !d.expired() {
		t.Fatal("cancelled context did not expire the parser")
	}
	doc := BuildPDFDocumentContext(ctx, pdfSimpleDoc("BT /F1 12 Tf 20 180 Td (text) Tj ET", ""))
	if doc == nil {
		t.Fatal("cancelled parse returned nil document")
	}
	if doc.Note == "" {
		t.Fatalf("cancelled parse note = %q", doc.Note)
	}
}

func TestPDFPredictorHonoursAbort(t *testing.T) {
	parms := pdfDict{"Predictor": float64(2), "Columns": float64(8)}
	_, err := pdfPredictor([]byte{1, 2, 3, 4, 5, 6, 7, 8}, parms, func() bool { return true })
	if !errors.Is(err, errPDFTimeout) {
		t.Fatalf("predictor error = %v, want errPDFTimeout", err)
	}
}

func TestPDFContentBudgetStopsPages(t *testing.T) {
	d := &PDFDoc{contentBytes: maxPDFContentTotal}
	if data := d.pageContent(pdfDict{}); len(data) != 0 {
		t.Fatalf("content beyond budget = %d bytes", len(data))
	}
}
