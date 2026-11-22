package browser

import (
	"net/url"

	"browser/internal/engine"
)

// Temporary PDF aliases. The parser lives in internal/engine; the page
// assembly and HTML rendering stay in the browser.

type pdfDoc = engine.PDFDoc
type PDFDocument = engine.PDFDocument
type PDFPageText = engine.PDFPageText
type pdfPage = engine.PDFPage
type pdfRun = engine.PDFRun
type pdfSpan = engine.PDFSpan

func parsePDF(data []byte) (*pdfDoc, error) { return engine.ParsePDF(data) }
func pdfPageLines(runs []pdfRun, bodySize float64) []pdfSpan {
	return engine.PDFPageLines(runs, bodySize)
}

func looksLikePDF(mediaType string, data []byte) bool { return engine.LooksLikePDF(mediaType, data) }
func buildPDFDocument(data []byte) *PDFDocument       { return engine.BuildPDFDocument(data) }
func pdfPageHTML(doc *PDFDocument, u *url.URL) string { return engine.PDFPageHTML(doc, u) }
func pdfSpansText(spans []pdfSpan) string             { return engine.PDFSpansText(spans) }

func pdfDisplayName(u *url.URL) string { return engine.PDFDisplayName(u) }

func pdfTextString(s string) string                    { return engine.PDFTextString(s) }
func pdfPageBox(pages []PDFPageText) (rows, cells int) { return engine.PDFPageBox(pages) }
func pdfPageRows(spans []pdfSpan) int                  { return engine.PDFPageRows(spans) }

const pdfHeadlinePx = engine.PDFHeadlinePx
