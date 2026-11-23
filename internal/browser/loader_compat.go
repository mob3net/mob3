package browser

import (
	"image"
	"net/url"

	"browser/internal/engine"
	"browser/internal/protocol"
)

// The loader implementation lives in the engine. These aliases and thin
// wrappers keep the browser test surface and the app's existing call sites
// stable during the session split.
type Loader = engine.Loader
type LoaderConfig = engine.LoaderConfig

const DefaultSearchURL = engine.DefaultSearchURL

const defaultUserAgent = "MOB3/1.0 (+terminal; +https://mob3.net)"

const startPageHTML = engine.StartPageHTML

// NewLoader creates a loader over the injected host session.
func NewLoader() *Loader { return engine.NewLoader(newLoaderHost()) }

// NewConfiguredLoader creates the loader used by one app invocation.
func NewConfiguredLoader(c LoaderConfig) (*Loader, error) {
	return engine.NewConfiguredLoader(newLoaderHost(), c)
}

func imageConfigOK(cfg image.Config) bool { return engine.ImageConfigOK(cfg) }

func imageSource(n *Node, viewportPx int) string { return engine.ImageSource(n, viewportPx) }

func pickSrcset(s string, preferred int) string { return engine.PickSrcset(s, preferred) }

func recoverImage(fn func() (image.Image, error)) (image.Image, error) {
	return engine.RecoverImage(fn)
}

func safeDecodeConfig(data []byte) (image.Config, error) { return engine.SafeDecodeConfig(data) }

func safeDecodeImage(data []byte) (image.Image, error) { return engine.SafeDecodeImage(data) }

func blockedPageHTML(rawURL, reason string, status int) string {
	return engine.BlockedPageHTML(rawURL, reason, status)
}

func aboutPageName(u *url.URL) string { return engine.AboutPageName(u) }

func ResolveInput(input string, base *url.URL, searchURL string) (*url.URL, error) {
	return engine.ResolveInput(input, base, searchURL)
}

func ResolveURL(input string, base *url.URL) (*url.URL, error) {
	return engine.ResolveURL(input, base)
}

func helpPageHTML() string { return engine.HelpPageHTML(engine.BuildVersion) }

func historyPageHTML(urls []string, current int) string {
	return engine.HistoryPageHTML(urls, current)
}

func collectImportMap(root *Node) map[string]string { return engine.CollectImportMap(root) }

// requestRecord keeps the browser test surface for the host request row.
type requestRecord = protocol.RequestRecord

const maxResourceSize = 12 << 20

var errResourceTooLarge = protocol.ErrResourceTooLarge

func decodeBody(data []byte, contentType string) string {
	return engine.DecodeBody(data, contentType)
}

func isJavaScriptMIME(contentType string) bool {
	return engine.IsJavaScriptMIME(contentType)
}

func mimeEssence(contentType string) string {
	return engine.MIMEEssence(contentType)
}
