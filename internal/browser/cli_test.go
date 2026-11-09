package browser

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

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

func TestRunDump(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.html")
	src := `<html><body><p>hello from file</p></body></html>`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := runCaptured(t, []string{"-dump", "-no-color", path})
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !bytes.Contains(out, []byte("hello from file")) {
		t.Fatalf("dump output = %q", out)
	}
}

func TestRunBookmarks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bookmarks.html")
	src := `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<DL><p><DT><H3>Reading</H3><DL><p>
<DT><A HREF="https://example.com/bookmark">Example Bookmark</A>
</DL><p></DL>`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := runCaptured(t, []string{"-dump", "-no-color", "-bookmarks", path, "about:bookmarks"})
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !bytes.Contains(out, []byte("Example Bookmark")) ||
		!bytes.Contains(out, []byte("https://example.com/bookmark")) {
		t.Fatalf("dump output = %q", out)
	}
}

func TestRunMissingFile(t *testing.T) {
	code, _ := runCaptured(t, []string{"-dump", filepath.Join(t.TempDir(), "missing.html")})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}
