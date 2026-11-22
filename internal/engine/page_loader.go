package engine

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// PageFetcher is the engine's brokered access to subresources while assembling
// a document. The host implements policy, cache and logging behind it.
type PageFetcher interface {
	FetchPageResource(ctx context.Context, u *url.URL, kind string) (data []byte, ctype string, status int, err error)
	DecodeBody(data []byte, contentType string) string
	IsJavaScriptMIME(contentType string) bool
}

const maxPageResourceSize = 12 << 20

// AssemblePage parses an HTML response into the Page model and collects its
// stylesheets, scripts, import map, base and title. Image and inline-SVG
// loading stay with the caller.
func AssemblePage(ctx context.Context, fetcher PageFetcher, page *Page, text string, noScripts bool) {
	if page == nil {
		return
	}
	root := ParseHTML(text)
	if !noScripts {
		RemoveNoscript(root)
	}
	page.Root = root
	if baseEl := root.FindFirst("base"); baseEl != nil {
		if href := baseEl.Attr("href"); href != "" {
			if bu, err := page.URL.Parse(href); err == nil {
				page.Base = bu
			}
		}
	}
	if page.Base == nil {
		page.Base = page.URL
	}
	if t := root.FindFirst("title"); t != nil {
		page.Title = strings.Join(strings.Fields(t.TextContent()), " ")
	}
	if page.Title == "" && page.URL != nil {
		page.Title = page.URL.String()
	}
	page.Author = CollectStylesheets(ctx, fetcher, root, page.Base)
	if !noScripts {
		page.Scripts = CollectScripts(ctx, fetcher, root, page.Base)
		page.ImportMap = CollectImportMap(root)
	}
}

// CollectScripts gathers executable scripts in document order.
func CollectScripts(ctx context.Context, fetcher PageFetcher, root *Node, base *url.URL) []Script {
	nodes := root.FindAll(func(n *Node) bool {
		if n.Tag != "script" || n.HasAttr("nomodule") {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(n.Attr("type"))) {
		case "", "text/javascript", "application/javascript", "text/ecmascript", "application/ecmascript", "module":
			return true
		}
		return false
	})
	var out []Script
	total := 0
	for _, n := range nodes {
		module := strings.EqualFold(strings.TrimSpace(n.Attr("type")), "module")
		src := strings.TrimSpace(n.Attr("src"))
		if src == "" {
			code := n.TextContent()
			if strings.TrimSpace(code) == "" {
				continue
			}
			total += len(code)
			name := "inline script"
			if module {
				name = ""
			}
			out = append(out, Script{Code: code, Src: name, Node: n, Module: module})
			if total > maxPageResourceSize {
				break
			}
			continue
		}
		u, err := base.Parse(src)
		if err != nil {
			continue
		}
		data, ctype, status, err := fetcher.FetchPageResource(ctx, u, "js")
		if err != nil || status < 200 || status >= 400 {
			continue
		}
		if !fetcher.IsJavaScriptMIME(ctype) {
			continue
		}
		code := fetcher.DecodeBody(data, ctype)
		total += len(code)
		out = append(out, Script{Code: code, Src: u.String(), Node: n, Module: module})
		if total > maxPageResourceSize {
			break
		}
	}
	return out
}

// CollectStylesheets resolves link/@import stylesheets into the cascade order.
func CollectStylesheets(ctx context.Context, fetcher PageFetcher, root *Node, base *url.URL) []Rule {
	var out []Rule
	order := 0
	layers := NewLayerRegistry()
	type sheetEntry struct{ src, media string }
	var sheets []sheetEntry
	seen := make(map[string]bool)
	imported := 0
	const maxImportDepth = 8
	const maxImportedSheets = 40
	add := func(src, media string) {
		sheets = append(sheets, sheetEntry{src: src, media: media})
	}
	var addSheet func(src string, sheetURL *url.URL, depth int, media string)
	addSheet = func(src string, sheetURL *url.URL, depth int, media string) {
		if depth < maxImportDepth && imported < maxImportedSheets {
			for _, imp := range CSSImports(src) {
				if imported >= maxImportedSheets {
					break
				}
				ref := sheetURL
				if ref == nil {
					ref = base
				}
				if ref == nil {
					continue
				}
				u, err := ref.Parse(imp.Href)
				if err != nil {
					continue
				}
				key := u.String()
				if seen[key] {
					continue
				}
				seen[key] = true
				imported++
				data, _, status, err := fetcher.FetchPageResource(ctx, u, "css")
				if err != nil || status < 200 || status >= 400 {
					continue
				}
				if final := u; final != nil {
					addSheet(string(data), final, depth+1, combineMedia(media, imp.Media))
				}
			}
		}
		add(src, media)
	}
	nodes := root.FindAll(func(n *Node) bool {
		if n.Tag == "style" {
			return true
		}
		if n.Tag == "link" {
			rel := strings.ToLower(n.Attr("rel"))
			return strings.Contains(rel, "stylesheet") && n.Attr("href") != ""
		}
		return false
	})
	for _, n := range nodes {
		media := strings.ToLower(n.Attr("media"))
		if strings.Contains(media, "print") && !strings.Contains(media, "screen") && !strings.Contains(media, "all") {
			continue
		}
		if n.Tag == "style" {
			addSheet(n.TextContent(), base, 0, media)
			continue
		}
		u, err := base.Parse(n.Attr("href"))
		if err != nil {
			continue
		}
		key := u.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		data, _, status, err := fetcher.FetchPageResource(ctx, u, "css")
		if err != nil || status < 200 || status >= 400 {
			continue
		}
		addSheet(string(data), u, 0, media)
	}
	for _, sh := range sheets {
		DeclareCSSLayers(sh.src, layers)
	}
	for _, sh := range sheets {
		for _, r := range ParseStylesheetWithLayers(ctx, sh.src, layers) {
			r.Media = combineMedia(sh.media, r.Media)
			r.Order = order
			order++
			out = append(out, r)
		}
	}
	return out
}

func combineMedia(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " and " + b
}

// CollectImportMap returns the first valid import map in the document.
func CollectImportMap(root *Node) map[string]string {
	if root == nil {
		return nil
	}
	for _, n := range root.FindAll(func(n *Node) bool { return n.Tag == "script" }) {
		if !strings.EqualFold(strings.TrimSpace(n.Attr("type")), "importmap") {
			continue
		}
		var m struct {
			Imports map[string]string `json:"imports"`
		}
		if err := json.Unmarshal([]byte(n.TextContent()), &m); err == nil && len(m.Imports) > 0 {
			return m.Imports
		}
	}
	return nil
}
