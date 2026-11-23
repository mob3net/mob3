package engine

import (
	"bytes"
	"flag"
	"io"
	"net/http"
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
	return strings.Join(gridLines(g, false), "\n") + "\n"
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

// TestLayoutImportedCSSGolden covers @import resolution through the real
// loader. The page links a stylesheet that imports another one; rules from both
// sheets must affect the rendered layout.
func TestLayoutImportedCSSGolden(t *testing.T) {
	dir, err := filepath.Abs("testdata/import")
	if err != nil {
		t.Fatal(err)
	}
	pageFile := filepath.Join(dir, "page.html")
	loader := newTestLoader()
	testSetHTTPClient(loader, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		rel := filepath.FromSlash(strings.TrimPrefix(req.URL.Path, "/"))
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return nil, err
		}
		h := make(http.Header)
		if strings.HasSuffix(rel, ".css") {
			h.Set("Content-Type", "text/css")
		} else {
			h.Set("Content-Type", "text/html")
		}
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: h,
			Body: io.NopCloser(bytes.NewReader(data)), Request: req,
		}, nil
	})})
	page, err := loader.Load("https://fixture.test/page.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Author) < 2 {
		t.Fatalf("author rules = %d, want imported and local rules", len(page.Author))
	}
	for _, width := range fixtureWidths {
		got := renderFixture(page.Root, page.Author, width)
		golden := pageFile + ".w" + itoaFixture(width) + ".txt"
		if *updateFixtures {
			if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("%s: %v (run go test -run 'TestLayoutImportedCSSGolden' -update)", golden, err)
		}
		if got != string(want) {
			t.Errorf("%s mismatch at width %d\n--- want ---\n%s\n--- got ---\n%s", pageFile, width, want, got)
		}
	}
}

// TestLayoutZoomFixture renders the zoom fixture at 200% so the golden shows
// that fixed pixel boxes and padding grow while glyph runs keep their cell
// width. Text metrics stay at the unzoomed cell size; only boxes and images
// scale.
func TestLayoutZoomFixture(t *testing.T) {
	file := "testdata/pages/zoom-scale.html"
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	setLayoutZoom(2)
	defer setLayoutZoom(1)
	for _, width := range fixtureWidths {
		root := ParseHTML(string(data))
		author := fixtureAuthorRules(root)
		got := renderFixture(root, author, width)
		golden := file + ".zoom2.w" + itoaFixture(width) + ".txt"
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
			t.Errorf("zoom fixture mismatch at width %d\n--- want ---\n%s\n--- got ---\n%s", width, want, got)
		}
	}
}
