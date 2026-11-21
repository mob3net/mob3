package browser

import (
	"net/url"
	"strings"
	"testing"
)

// TestBuiltinPagesShareShell checks that every generated app page uses the same
// page shell and reading column. The blocked page keeps its warning and retry
// action, and bookmarks use the shared muted style.
func TestBuiltinPagesShareShell(t *testing.T) {
	u, _ := url.Parse("https://example.com/file.bin")
	pages := map[string]string{
		"help":            helpPageHTML(),
		"history":         historyPageHTML([]string{"https://example.com/"}, 0),
		"empty bookmarks": bookmarksPageHTML(nil),
		"bookmarks": bookmarksPageHTML([]Bookmark{
			{Title: "One", URL: "https://example.com/one", Folder: "Folder"},
		}),
		"blocked":        blockedPageHTML("https://example.com/", "anti-bot challenge", 403),
		"config":         configPageHTML(nil, false, "", 80, 24, 8, 16),
		"cannot display": cannotDisplayPageHTML("image/png", u),
		"load error":     loadErrorPageHTML("dial tcp: connection refused"),
	}
	for name, doc := range pages {
		if !strings.Contains(doc, appPageCSS) {
			t.Errorf("%s does not include the shared page style", name)
		}
		if !strings.HasPrefix(doc, "<!DOCTYPE html>") || !strings.HasSuffix(doc, "</body></html>") {
			t.Errorf("%s is not wrapped in the shared shell", name)
		}
		if strings.Contains(doc, "body { margin: 16px") {
			t.Errorf("%s keeps the old off-centre column", name)
		}
	}
	if !strings.Contains(pages["blocked"], "about:retry") ||
		!strings.Contains(pages["blocked"], `class="warn"`) ||
		!strings.Contains(pages["blocked"], `class="try"`) {
		t.Error("blocked page lost its warning or retry action")
	}
	if !strings.Contains(pages["bookmarks"], `class="muted"`) {
		t.Error("bookmark URLs do not use the shared muted style")
	}
}

func TestAppPageHTML(t *testing.T) {
	doc := appPageHTML("T & X", ".x { color: red; }\n", "<h1>Body</h1>")
	for _, want := range []string{
		"<title>T &amp; X</title>",
		appPageCSS,
		".x { color: red; }",
		"<h1>Body</h1>",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("page is missing %q", want)
		}
	}
}
