package browser

import (
	"net/url"

	"browser/internal/engine"
)

// Built-in page rendering lives in the engine; these thin names keep the
// session-side call sites and tests on the existing API during the migration.
const appPageCSS = engine.AppPageCSS

func appPageHTML(title, extraCSS, body string) string {
	return engine.AppPageHTML(title, extraCSS, body)
}

func cannotDisplayPageHTML(mediaType string, u *url.URL) string {
	return engine.CannotDisplayPageHTML(mediaType, u)
}

func loadErrorPageHTML(message string) string {
	return engine.LoadErrorPageHTML(message)
}

func applyPageStyles(root *Node, page *Page) {
	engine.ApplyPageStyles(root, page)
}
