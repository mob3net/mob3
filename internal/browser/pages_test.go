package browser

import (
	"browser/internal/engine"
	"net/url"
	"strings"
	"testing"
)

// TestBuiltinPagesShareShell checks that every generated app page uses the same
// page shell and reading column. The blocked page keeps its warning and retry
// action.
func TestBuiltinPagesShareShell(t *testing.T) {
	u, _ := url.Parse("https://example.com/file.bin")
	pages := map[string]string{
		"help":           helpPageHTML(),
		"history":        historyPageHTML([]string{"https://example.com/"}, 0),
		"blocked":        blockedPageHTML("https://example.com/", "anti-bot challenge", 403),
		"config":         configPageHTML(nil, false, "", "", 80, 24, 8, 16),
		"cannot display": engine.CannotDisplayPageHTML("image/png", u),
		"load error":     engine.LoadErrorPageHTML("dial tcp: connection refused"),
	}
	for name, doc := range pages {
		if !strings.Contains(doc, engine.AppPageCSS) {
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
}

func TestAppPageHTML(t *testing.T) {
	doc := engine.AppPageHTML("T & X", ".x { color: red; }\n", "<h1>Body</h1>")
	for _, want := range []string{
		"<title>T &amp; X</title>",
		engine.AppPageCSS,
		".x { color: red; }",
		"<h1>Body</h1>",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("page is missing %q", want)
		}
	}
}

// TestLoadErrorPageUsesShellStyles checks the load-error placeholder attaches
// the generated page's stylesheet, since it is built outside the loader.
func TestLoadErrorPageUsesShellStyles(t *testing.T) {
	root := ParseHTML(engine.LoadErrorPageHTML("dial tcp: connection refused"))
	page := &Page{Root: root}
	engine.ApplyPageStyles(root, page)
	if len(page.Author) == 0 {
		t.Fatal("load error page stylesheet was not applied")
	}
	ly := BuildLayout(root, page.Author, 152)
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Kind == fragText && f.Node != nil && f.Node.Parent != nil &&
			f.Node.Parent.Tag == "h1" && f.X >= 8 {
			return
		}
	}
	t.Fatal("load error heading is not in the centered page column")
}
