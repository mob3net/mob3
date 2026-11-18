package browser

import (
	"strings"
	"testing"
)

func TestParseBookmarks(t *testing.T) {
	data := `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<DL><p>
<DT><H3>Work</H3>
<DL><p>
<DT><A HREF="https://a.example/">A &amp; B</A>
<DT><A HREF="https://b.example/">B</A>
</DL><p>
<DT><A HREF="https://c.example/">C</A>
</DL>`
	marks := parseBookmarks([]byte(data))
	if len(marks) != 3 {
		t.Fatalf("bookmarks = %+v", marks)
	}
	if marks[0].Title != "A & B" || marks[0].URL != "https://a.example/" || marks[0].Folder != "Work" {
		t.Errorf("first bookmark = %+v", marks[0])
	}
	if marks[1].Folder != "Work" {
		t.Errorf("second folder = %q", marks[1].Folder)
	}
	if marks[2].Folder != "" || marks[2].URL != "https://c.example/" {
		t.Errorf("root bookmark = %+v", marks[2])
	}
}

func TestBookmarksPage(t *testing.T) {
	html := bookmarksPageHTML([]Bookmark{
		{Title: "A & B", URL: "https://a.example/?x=1&y=2", Folder: "Work"},
		{Title: "Root", URL: "https://c.example/"},
	})
	if !strings.Contains(html, "Work") || !strings.Contains(html, "A &amp; B") ||
		!strings.Contains(html, "https://a.example/?x=1&amp;y=2") {
		t.Fatalf("page = %s", html)
	}
	empty := bookmarksPageHTML(nil)
	if !strings.Contains(empty, "No bookmarks imported") {
		t.Fatalf("empty page = %s", empty)
	}
}
