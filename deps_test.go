package reasonr

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNoEngineImports: the compiler does not depend on the engine — of the
// ReasonR modules (github.com/tycheg/reasonr-*) its packages import only the
// compiler module itself. The engine depends on the compiler's abstraction
// (reasonr, runtime), not the other way round.
func TestNoEngineImports(t *testing.T) {
	const repo, self = "github.com/tycheg/reasonr-", "github.com/tycheg/reasonr-compiler"
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		// Nested modules (docs) are not part of the compiler module.
		if d.IsDir() && path != "." {
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			}
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(p, repo) && p != self && !strings.HasPrefix(p, self+"/") {
				t.Errorf("%s: imports %s — the compiler must not depend on other ReasonR modules", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestOnePublicPackage: the module root is the compiler's only public
// package (plan 0021): every other package with Go code is under internal/
// or is a command (cmd/), so that no refactoring of the compiler breaks
// another module.
func TestOnePublicPackage(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || path == "." {
			return nil
		}
		switch {
		case d.Name() == "node_modules" || d.Name() == "testdata" || strings.HasPrefix(d.Name(), "."):
			return filepath.SkipDir
		case path == "internal" || path == "cmd":
			return filepath.SkipDir
		}
		if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
			return filepath.SkipDir // a nested module (docs)
		}
		if gos, _ := filepath.Glob(filepath.Join(path, "*.go")); len(gos) > 0 {
			t.Errorf("%s: a public package — move it under internal/", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
