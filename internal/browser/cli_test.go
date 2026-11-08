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

func TestRunMissingFile(t *testing.T) {
	code, _ := runCaptured(t, []string{"-dump", filepath.Join(t.TempDir(), "missing.html")})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}
