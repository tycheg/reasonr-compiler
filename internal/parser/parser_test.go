package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/golden"
	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/wanttest"
)

func parsePath(t *testing.T, path string) (*source.FileSet, *source.File, *ast.File, []*diag.Diagnostic) {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fset := source.NewFileSet()
	f := fset.AddFile(filepath.ToSlash(path), src)
	file, diags := Parse(f)
	return fset, f, file, diags
}

func render(fset *source.FileSet, diags []*diag.Diagnostic) string {
	var b strings.Builder
	_ = diag.WriteText(&b, fset, diags)
	return b.String()
}

// TestGolden: every construct parses without errors and the AST dump matches the golden file.
func TestGolden(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "*.rr"))
	if len(files) == 0 {
		t.Fatal("no test files")
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			fset, _, file, diags := parsePath(t, path)
			if len(diags) > 0 {
				t.Errorf("unexpected errors:\n%s", render(fset, diags))
			}
			checkPositions(t, fset, file)
			golden.Check(t, strings.TrimSuffix(path, ".rr")+".ast", []byte(ast.Dump(file)))
		})
	}
}

// TestErrors checks syntax errors against # want expectations.
func TestErrors(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "errors", "*.rr"))
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			fset, f, file, diags := parsePath(t, path)
			wanttest.Check(t, fset, f, diags)
			checkPositions(t, fset, file)
		})
	}
}

func FuzzParser(f *testing.F) {
	var seeds []string
	for _, pattern := range []string{"testdata/*.rr", "testdata/errors/*.rr", filepath.Join(rfcblocks.Examples(), "*", "*.rr")} {
		m, _ := filepath.Glob(filepath.FromSlash(pattern))
		seeds = append(seeds, m...)
	}
	for _, p := range seeds {
		if src, err := os.ReadFile(p); err == nil {
			f.Add(src)
		}
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		fset := source.NewFileSet()
		file, _ := ParseFile(fset, "fuzz.rr", src)
		if file == nil {
			t.Fatal("nil AST")
		}
		if err := positionsError(file); err != nil {
			t.Fatal(err)
		}
	})
}
