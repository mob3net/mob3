package engine

import (
	"net/url"
	"strings"
)

// AppPageCSS is the shared stylesheet for MOB3's own pages: help, history,
// the blocked page and the error pages. It gives them one centered reading
// column, heading scale, table, secondary-text and warning style.
// Page-specific rules are appended by AppPageHTML.
const AppPageCSS = `body { max-width: 900px; margin: 8px auto; }
h1 { font-size: 32px; margin-bottom: 4px; }
h2 { margin: 16px 0 4px; }
table { border-collapse: collapse; margin: 12px 0; }
th, td { padding: 5px 10px; text-align: left; border-bottom: 1px solid #d0d7de; vertical-align: top; }
th { color: #57606a; }
.muted { color: #57606a; }
.warn { background: #fff3cd; border: 1px solid #d4a72c; padding: 8px; color: #442c00; }
pre { color: #57606a; white-space: pre-wrap; }
`

// AppPageHTML wraps body markup in the shell shared by the built-in pages.
// extraCSS is appended to AppPageCSS for page-specific rules. title and body
// are inserted as given; callers escape anything derived from a page or URL.
func AppPageHTML(title, extraCSS, body string) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html><html><head><title>")
	b.WriteString(EscapeHTML(title))
	b.WriteString("</title>\n<style>\n")
	b.WriteString(AppPageCSS)
	b.WriteString(extraCSS)
	b.WriteString("</style></head><body>\n")
	b.WriteString(body)
	b.WriteString("\n</body></html>")
	return b.String()
}

// CannotDisplayPageHTML renders the page shown for a response MOB3 cannot
// render, such as an image, an archive or a binary file.
func CannotDisplayPageHTML(mediaType string, u *url.URL) string {
	raw := ""
	if u != nil {
		raw = u.String()
	}
	title := strings.TrimSpace("Cannot display " + mediaType)
	body := `<h1>` + EscapeHTML(title) + `</h1>
<p>The browser only renders HTML pages. The URL was:</p><pre>` + EscapeHTML(raw) + `</pre>`
	return AppPageHTML(title, "", body)
}

// LoadErrorPageHTML renders the placeholder shown when navigation fails before
// any page could be applied.
func LoadErrorPageHTML(message string) string {
	return AppPageHTML("Error", "",
		`<h1>Could not load page</h1><p>`+EscapeHTML(message)+`</p>`)
}

// ApplyPageStyles copies a generated page's <style> element into the page's
// author rules. Pages built outside the loader (the load-error placeholder)
// use it; loader-built pages collect their styles through CollectStylesheets,
// which also handles linked sheets.
func ApplyPageStyles(root *Node, page *Page) {
	if root == nil || page == nil {
		return
	}
	if style := root.FindFirst("style"); style != nil {
		page.Author = ParseStylesheet(style.TextContent())
	}
}
