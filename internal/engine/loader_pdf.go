package engine

import (
	"context"
	"net/url"
)

// LoadPDFPage assembles one PDF document into a page of extracted text.
func (l *Loader) LoadPDFPage(ctx context.Context, data []byte, finalURL *url.URL, status int, local bool) *Page {
	return AssemblePDFPage(ctx, loaderPageFetcher{l}, data, finalURL, status, l.PDFPage, local)
}
