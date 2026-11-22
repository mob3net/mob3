package browser

import (
	"context"
	"net/url"

	"browser/internal/engine"
)

// loaderPageFetcher brokers subresources for engine page assembly.
type loaderPageFetcher struct{ l *Loader }

func (f loaderPageFetcher) FetchPageResource(ctx context.Context, u *url.URL, kind string) ([]byte, string, int, error) {
	ctx = withRequestKind(ctx, kind)
	data, ctype, _, status, err := f.l.fetchCtx(ctx, u)
	return data, ctype, status, err
}
func (f loaderPageFetcher) DecodeBody(data []byte, contentType string) string {
	return decodeBody(data, contentType)
}
func (f loaderPageFetcher) IsJavaScriptMIME(contentType string) bool {
	return isJavaScriptMIME(contentType)
}

func (l *Loader) collectScripts(ctx context.Context, root *Node, base *url.URL) []Script {
	return engine.CollectScripts(ctx, loaderPageFetcher{l}, root, base)
}
func (l *Loader) collectStylesheets(ctx context.Context, root *Node, base *url.URL) []Rule {
	return engine.CollectStylesheets(ctx, loaderPageFetcher{l}, root, base)
}
func collectImportMap(root *Node) map[string]string { return engine.CollectImportMap(root) }

var _ engine.PageFetcher = loaderPageFetcher{}
