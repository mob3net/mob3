package browser

import (
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
)

// loadBookmarksFile reads a Netscape bookmark export with the same size cap as
// page resources.
func loadBookmarksFile(path string) ([]Bookmark, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxResourceSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResourceSize {
		return nil, errors.New("bookmark file is too large")
	}
	return parseBookmarks(data), nil
}

// Bookmark is one imported browser bookmark.
type Bookmark struct {
	Title  string
	URL    string
	Folder string
}

// parseBookmarks reads a Netscape bookmark export (the HTML format used by
// Chrome, Firefox, Safari and Edge). Folder names are flattened into a
// "A / B" path per bookmark.
func parseBookmarks(data []byte) []Bookmark {
	root := ParseHTML(string(data))
	var out []Bookmark
	var walk func(*Node, []string)
	walk = func(list *Node, folder []string) {
		for _, item := range list.Children {
			if item.Type != ElementNode {
				continue
			}
			switch item.Tag {
			case "dt":
				var h3, a, sub *Node
				for _, c := range item.Children {
					switch c.Tag {
					case "h3":
						h3 = c
					case "a":
						a = c
					case "dl":
						sub = c
					}
				}
				if a != nil {
					addBookmark(&out, a, folder)
				}
				if h3 != nil {
					name := strings.TrimSpace(h3.TextContent())
					if sub != nil {
						walk(sub, appendFolder(folder, name))
					}
				} else if sub != nil {
					walk(sub, folder)
				}
			case "dl":
				walk(item, folder)
			case "a":
				addBookmark(&out, item, folder)
			case "h3":
				name := strings.TrimSpace(item.TextContent())
				for s := nextSiblingNode(item); s != nil; s = nextSiblingNode(s) {
					if s.Tag == "dl" {
						walk(s, appendFolder(folder, name))
						break
					}
				}
			}
		}
	}
	if body := root.FindFirst("body"); body != nil {
		walk(body, nil)
	} else {
		walk(root, nil)
	}
	return out
}

func appendFolder(folder []string, name string) []string {
	if name == "" {
		return folder
	}
	next := make([]string, len(folder), len(folder)+1)
	copy(next, folder)
	return append(next, name)
}

func addBookmark(out *[]Bookmark, a *Node, folder []string) {
	href := strings.TrimSpace(a.Attr("href"))
	if href == "" || strings.HasPrefix(strings.ToLower(href), "javascript:") {
		return
	}
	title := strings.TrimSpace(a.TextContent())
	if title == "" {
		title = href
	}
	*out = append(*out, Bookmark{
		Title:  title,
		URL:    href,
		Folder: strings.Join(folder, " / "),
	})
}

// bookmarksPageHTML renders imported bookmarks grouped by folder.
func bookmarksPageHTML(bookmarks []Bookmark) string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><title>Bookmarks</title>
<style>
body { margin: 16px; max-width: 900px; }
h1 { font-size: 32px; }
h3 { margin: 16px 0 4px; }
li { margin: 4px 0; }
.url { color: #57606a; font-size: 14px; }
</style></head><body>
<h1>Bookmarks</h1>
<p>Imported from a Netscape bookmark file. Bookmarks live in memory only and
disappear when MOB³ exits.</p>
`)
	if len(bookmarks) == 0 {
		b.WriteString(`<p>No bookmarks imported. Start MOB³ with <b>-bookmarks file.html</b>.</p></body></html>`)
		return b.String()
	}
	order := make([]string, 0, 8)
	groups := make(map[string][]Bookmark)
	for _, bm := range bookmarks {
		if _, ok := groups[bm.Folder]; !ok {
			order = append(order, bm.Folder)
		}
		groups[bm.Folder] = append(groups[bm.Folder], bm)
	}
	b.WriteString(`<p>`)
	b.WriteString(strconv.Itoa(len(bookmarks)))
	b.WriteString(` bookmark(s).</p>`)
	for _, folder := range order {
		if folder != "" {
			b.WriteString(`<h3>`)
			b.WriteString(EscapeHTML(folder))
			b.WriteString(`</h3>`)
		}
		b.WriteString(`<ul>`)
		for _, bm := range groups[folder] {
			b.WriteString(`<li><a href="`)
			b.WriteString(EscapeHTML(bm.URL))
			b.WriteString(`">`)
			b.WriteString(EscapeHTML(bm.Title))
			b.WriteString(`</a> <span class="url">`)
			b.WriteString(EscapeHTML(bm.URL))
			b.WriteString(`</span></li>`)
		}
		b.WriteString(`</ul>`)
	}
	b.WriteString(`</body></html>`)
	return b.String()
}
