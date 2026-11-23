package engine

import (
	"context"
	"net/url"

	"browser/internal/protocol"
)

// loaderPageFetcher brokers subresources for page assembly.
type loaderPageFetcher struct{ l *Loader }

func (f loaderPageFetcher) FetchPageResource(ctx context.Context, u *url.URL, kind string) ([]byte, string, int, error) {
	ctx = protocol.WithRequestKind(ctx, kind)
	data, ctype, _, status, err := f.l.FetchCtx(ctx, u)
	return data, ctype, status, err
}

func (f loaderPageFetcher) DecodeBody(data []byte, contentType string) string {
	return decodeBody(data, contentType)
}

func (f loaderPageFetcher) IsJavaScriptMIME(contentType string) bool {
	return isJavaScriptMIME(contentType)
}

// CollectScripts collects the page's classic and module scripts.
func (l *Loader) CollectScripts(ctx context.Context, root *Node, base *url.URL) []Script {
	return CollectScripts(ctx, loaderPageFetcher{l}, root, base)
}

// CollectStylesheets collects the page's stylesheets.
func (l *Loader) CollectStylesheets(ctx context.Context, root *Node, base *url.URL) []Rule {
	return CollectStylesheets(ctx, loaderPageFetcher{l}, root, base)
}

var _ PageFetcher = loaderPageFetcher{}

// DecodeBody decodes a response body using the Content-Type charset.
func DecodeBody(data []byte, contentType string) string { return decodeBody(data, contentType) }

// IsJavaScriptMIME reports whether a script response may execute.
func IsJavaScriptMIME(contentType string) bool { return isJavaScriptMIME(contentType) }

// MIMEEssence returns the lowercased type/subtype of a Content-Type header.
func MIMEEssence(contentType string) string { return mimeEssence(contentType) }
