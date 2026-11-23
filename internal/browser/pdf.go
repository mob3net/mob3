package browser

import (
	"context"
	"net/url"

	"browser/internal/engine"
)

func (l *Loader) loadPDFPage(ctx context.Context, data []byte, finalURL *url.URL, status int, local bool) *Page {
	return engine.AssemblePDFPage(ctx, loaderPageFetcher{l}, data, finalURL, status, l.pdfPage, local)
}
