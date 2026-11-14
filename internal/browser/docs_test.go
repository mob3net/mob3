package browser

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readDoc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", name))
	if err != nil {
		t.Fatalf("read docs/%s: %v", name, err)
	}
	return string(b)
}

func TestDocsCoverFlagsAndTools(t *testing.T) {
	for _, name := range []string{
		"index.md", "install.md", "cli.md", "keys.md",
		"automation.md", "security.md", "sites.md", "development.md",
	} {
		if _, err := os.Stat(filepath.Join("..", "..", "docs", name)); err != nil {
			t.Errorf("docs/%s is missing", name)
		}
	}

	start := strings.Index(helpPageHTML, "<h2>Command line</h2>")
	if start < 0 {
		t.Fatal("help page has no command-line section")
	}
	end := strings.Index(helpPageHTML[start:], "\n<h2>")
	if end < 0 {
		t.Fatal("help page command-line section has no end")
	}
	section := helpPageHTML[start : start+end]
	flagRE := regexp.MustCompile(`<b>(-[a-z][a-z-]*)</b>`)
	cli := readDoc(t, "cli.md")
	flags := flagRE.FindAllStringSubmatch(section, -1)
	if len(flags) < 10 {
		t.Fatalf("found only %d flags in help", len(flags))
	}
	for _, m := range flags {
		if !strings.Contains(cli, "`"+m[1]+"`") && !strings.Contains(cli, m[1]) {
			t.Errorf("docs/cli.md does not mention %s", m[1])
		}
	}

	mcpDoc := readDoc(t, "automation.md")
	for _, tool := range mcpToolDefinitions() {
		name, _ := tool["name"].(string)
		if name == "" {
			t.Fatal("tool without a name")
		}
		if !strings.Contains(mcpDoc, "`"+name+"`") {
			t.Errorf("docs/automation.md does not mention tool %s", name)
		}
	}
}
