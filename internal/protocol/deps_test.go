package protocol

import (
	"bufio"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// importRule is the allowed dependency shape for one boundary package. The
// table is empty for directories that do not exist yet, so the test starts
// enforcing a package as soon as it is created.
type importRule struct {
	// stdlibOnly rejects third-party imports as well as project imports.
	stdlibOnly bool
	// allowAnyProject permits any import inside this module.
	allowAnyProject bool
	// allowProject lists the project packages this one may import.
	allowProject map[string]bool
	// forbidStd rejects specific standard-library packages.
	forbidStd map[string]bool
	// forbidOS rejects the os package; terminal capability detection and file
	// access belong on the trusted side.
	forbidOS bool
}

var importBoundary = map[string]importRule{
	"internal/protocol": {
		stdlibOnly: true,
	},
	"internal/engine": {
		allowProject: map[string]bool{"internal/protocol": true},
		forbidStd:    map[string]bool{"net": true, "syscall": true},
		forbidOS:     true,
	},
	"internal/host": {
		allowProject: map[string]bool{"internal/protocol": true},
	},
	"internal/frontend": {
		allowProject: map[string]bool{"internal/protocol": true},
	},
	"internal/app": {
		allowAnyProject: true,
	},
}

func TestImportBoundary(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	module := modulePath(t, filepath.Join(root, "go.mod"))
	internal := filepath.Join(root, "internal")

	err := filepath.WalkDir(internal, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			switch d.Name() {
			case "testdata", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		dir := filepath.Dir(path)
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return err
		}
		rule, ok := importBoundary[filepath.ToSlash(rel)]
		if !ok {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		for _, spec := range file.Imports {
			imp := strings.Trim(spec.Path.Value, `"`)
			if err := checkImport(rule, module, imp); err != nil {
				t.Errorf("%s imports %q: %v", filepath.ToSlash(path), imp, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func checkImport(rule importRule, module, imp string) error {
	if strings.HasPrefix(imp, module+"/") {
		if rule.allowAnyProject {
			return nil
		}
		target := strings.TrimPrefix(imp, module+"/")
		if !rule.allowProject[target] {
			return fmt.Errorf("project import %q is not allowed by the boundary", target)
		}
		return nil
	}
	if rule.stdlibOnly {
		first := strings.SplitN(imp, "/", 2)[0]
		if strings.Contains(first, ".") {
			return fmt.Errorf("protocol must remain standard-library only")
		}
	}
	if rule.forbidStd[imp] {
		return fmt.Errorf("%q is forbidden on this side of the boundary", imp)
	}
	if rule.forbidOS && imp == "os" {
		return fmt.Errorf("os belongs on the trusted side; move terminal or file access out")
	}
	return nil
}

func modulePath(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("%s has no module line", path)
	return ""
}
