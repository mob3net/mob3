package browser

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var updateFixtures = flag.Bool("update", false, "rewrite layout fixture goldens")

// fixtureWidths is the pair of layout widths every fixture is rendered at.
var fixtureWidths = []int{80, 120}

// TestLayoutFixtures renders small, synthetic pages and compares the terminal
// text with checked-in goldens. The fixtures are network-free and deliberately
// mix layout features (flex headers, floated sidebars, inline code, indented
// markup, tables) so interactions between style, intrinsic sizing and inline
// flow are covered. Run with -update after an intentional layout change.
func TestLayoutFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/pages/*.html")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no layout fixtures found")
	}
	for _, file := range files {
		file := file
		name := strings.TrimSuffix(filepath.Base(file), ".html")
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			root := ParseHTML(string(data))
			author := fixtureAuthorRules(root)
			for _, width := range fixtureWidths {
				got := renderFixture(root, author, width)
				golden := file + ".w" + itoaFixture(width) + ".txt"
				if *updateFixtures {
					if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
						t.Fatal(err)
					}
					continue
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatalf("%s: %v (run go test -run TestLayoutFixtures -update)", golden, err)
				}
				if got != string(want) {
					t.Errorf("%s mismatch at width %d\n--- want ---\n%s\n--- got ---\n%s", name, width, want, got)
				}
			}
		})
	}
}

// fixtureAuthorRules collects the inline <style> blocks of a fixture. Fixtures
// must be self-contained; external stylesheets are not fetched.
func fixtureAuthorRules(root *Node) []Rule {
	var out []Rule
	order := 0
	for _, n := range root.FindAll(func(n *Node) bool { return n.Tag == "style" }) {
		for _, r := range ParseStylesheet(n.TextContent()) {
			r.Order = order
			order++
			out = append(out, r)
		}
	}
	return out
}

// renderFixture builds a fixture without a browser chrome and returns its
// plain-text rows, capped so goldens stay readable.
func renderFixture(root *Node, author []Rule, width int) string {
	const maxRows = 40
	ly := BuildLayoutBudgetDeadline(root, author, width, 0, false, false, time.Time{}, DefaultLayoutBudget)
	rows := ly.Height
	if rows > maxRows {
		rows = maxRows
	}
	if rows < 1 {
		rows = 1
	}
	pageFG := RGB(0x1f, 0x23, 0x28)
	pageBG := ly.PageBG
	if !pageBG.Set {
		pageBG = RGB(255, 255, 255)
	}
	g := NewGrid(width, rows, pageFG, pageBG)
	PaintLayout(g, ly, 0, 0, rows, nil, pageFG)
	return strings.Join(g.Lines(false), "\n") + "\n"
}

func itoaFixture(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
