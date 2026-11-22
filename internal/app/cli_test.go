package app

import (
	"bytes"
	"image/png"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"browser/internal/browser"
)

// dataPage returns an in-memory HTML document for dump tests. The product no
// longer reads local files, so fixtures are passed as data: URLs.
func dataPage(html string) string {
	return "data:text/html;charset=utf-8," + url.PathEscape(html)
}

// runCaptured runs Run while capturing stdout.
func runCaptured(t *testing.T, args []string) (int, []byte) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := Run(args)
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	r.Close()
	return code, out
}

func TestRunVersion(t *testing.T) {
	old := Version
	Version = "v9.9.9"
	defer func() { Version = old }()

	code, out := runCaptured(t, []string{"-version"})
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if string(out) != "v9.9.9\n" {
		t.Fatalf("version output = %q", out)
	}
}

func TestRunHelpShowsVersionLicenseCopyrightAndGraphicsAlias(t *testing.T) {
	old := Version
	Version = "v9.9.9"
	defer func() { Version = old }()

	oldErr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	code := Run([]string{"-h"})
	w.Close()
	os.Stderr = oldErr
	out, _ := io.ReadAll(r)
	r.Close()
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	text := string(out)
	for _, want := range []string{"v9.9.9", "AGPL-3.0-only", "2026 Milos Stojanovic", "-graphics", "-g"} {
		if !strings.Contains(text, want) {
			t.Errorf("help output missing %q:\n%s", want, text)
		}
	}
}

func TestRunGraphicsAlias(t *testing.T) {
	src := `<html><body><p>alias</p></body></html>`
	code, out := runCaptured(t, []string{"-dump", "-no-color", "-g", "none", dataPage(src)})
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !bytes.Contains(out, []byte("alias")) {
		t.Fatalf("dump output = %q", out)
	}
}

func TestRunDump(t *testing.T) {
	src := `<html><body><p>hello from data</p></body></html>`
	code, out := runCaptured(t, []string{"-dump", "-no-color", dataPage(src)})
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !bytes.Contains(out, []byte("hello from data")) {
		t.Fatalf("dump output = %q", out)
	}
}

func TestRunDumpHeightLimit(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 20; i++ {
		sb.WriteString("<p>line")
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString("</p>")
	}
	src := "<html><body>" + sb.String() + "</body></html>"
	code, out := runCaptured(t, []string{"-dump", "-no-color", "-width", "40", "-height", "5", dataPage(src)})
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if got := strings.Count(string(out), "\n"); got > 5 {
		t.Fatalf("dump height = %d lines, want at most 5:\n%s", got, out)
	}
	if !bytes.Contains(out, []byte("line1")) {
		t.Fatalf("dump missing first text: %q", out)
	}
	if bytes.Contains(out, []byte("line20")) {
		t.Fatal("dump was not cropped by -height")
	}
}

func TestRunPNGHeight(t *testing.T) {
	src := `<html><body><p>png height</p></body></html>`
	code, out := runCaptured(t, []string{"-png", "-width", "20", "-height", "5", dataPage(src)})
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds().Dy(); got > 5*16 {
		t.Fatalf("PNG height = %d, want at most %d", got, 5*16)
	}
}

func TestRunDumpReader(t *testing.T) {
	src := `<html><body>
		<nav>Site navigation</nav>
		<article><h1>Reader article</h1>
		<p>` + strings.Repeat("Reader mode text. ", 20) + `</p>
		<img src="/pic.png" alt="A picture">
		</article>
	</body></html>`
	code, out := runCaptured(t, []string{"-dump", "-no-color", "-reader", dataPage(src)})
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !bytes.Contains(out, []byte("Reader mode text")) {
		t.Fatalf("reader dump missing article: %q", out)
	}
	if bytes.Contains(out, []byte("Site navigation")) {
		t.Fatalf("reader dump kept navigation: %q", out)
	}
	if bytes.Contains(out, []byte("A picture")) {
		t.Fatalf("reader dump kept image alt text: %q", out)
	}
}

func TestRunDumpDarkChangesPalette(t *testing.T) {
	src := `<html><body><p>Dark palette text</p></body></html>`
	lightCode, light := runCaptured(t, []string{"-dump", "-color", dataPage(src)})
	if lightCode != 0 {
		t.Fatalf("light exit code = %d", lightCode)
	}
	darkCode, dark := runCaptured(t, []string{"-dump", "-color", "-dark", dataPage(src)})
	if darkCode != 0 {
		t.Fatalf("dark exit code = %d", darkCode)
	}
	if !bytes.Contains(dark, []byte("Dark palette text")) {
		t.Fatalf("dark dump missing page text: %q", dark)
	}
	if bytes.Equal(light, dark) {
		t.Fatal("-dark did not change the rendered palette")
	}
}

func TestRunHome(t *testing.T) {
	code, out := runCaptured(t, []string{"-dump", "-no-color", "-home", "about:help"})
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !bytes.Contains(out, []byte("MOB³ Help")) {
		t.Fatalf("home dump output = %q", out)
	}
}

func TestRunRejectsLocalFile(t *testing.T) {
	code, _ := runCaptured(t, []string{"-dump", "file:///etc/passwd"})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestRunZoomFlag(t *testing.T) {
	defer browser.SetLayoutZoom(1)
	src := `<html><body style="margin:0"><p style="margin:0 0 0 64px">indent</p></body></html>`
	page := dataPage(src)
	indentOf := func(out []byte) int {
		for _, line := range strings.Split(string(out), "\n") {
			if i := strings.Index(line, "indent"); i >= 0 {
				return i
			}
		}
		return -1
	}
	code, baseOut := runCaptured(t, []string{"-dump", "-no-color", "-width", "40", page})
	if code != 0 {
		t.Fatalf("dump exit code = %d", code)
	}
	code, zoomOut := runCaptured(t, []string{"-dump", "-no-color", "-width", "40", "-zoom", "200", page})
	if code != 0 {
		t.Fatalf("zoomed dump exit code = %d", code)
	}
	baseIndent, zoomIndent := indentOf(baseOut), indentOf(zoomOut)
	if baseIndent < 0 || zoomIndent < 0 {
		t.Fatalf("indented text missing: base=%d zoom=%d", baseIndent, zoomIndent)
	}
	// 64px is 8 cells at 100% and 16 cells at 200%.
	if baseIndent != 8 || zoomIndent != 16 {
		t.Fatalf("indent = %d at 100%% and %d at 200%%, want 8 and 16", baseIndent, zoomIndent)
	}

	code, _ = runCaptured(t, []string{"-dump", "-no-color", "-zoom", "10", page})
	if code != 2 {
		t.Fatalf("invalid -zoom exit code = %d, want 2", code)
	}
}

func TestRunCertsFlag(t *testing.T) {
	code, out := runCaptured(t, []string{
		"-certs", "embedded", "-dump", "-no-color", "-width", "40",
		"data:text/html,<p>certs ok</p>",
	})
	if code != 0 {
		t.Fatalf("embedded certs dump exit code = %d", code)
	}
	if !strings.Contains(string(out), "certs ok") {
		t.Fatalf("embedded certs dump output = %q", out)
	}

	code, _ = runCaptured(t, []string{
		"-certs", "bundle.pem",
		"-dump", "data:text/html,<p>x</p>",
	})
	if code != 2 {
		t.Fatalf("certificate file exit code = %d, want 2", code)
	}
}
