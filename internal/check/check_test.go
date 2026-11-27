package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/wanttest"
)

// TestAnalyzers: every testdata/*.rr file is a one-file module; the
// diagnostics of all phases are checked against # want expectations.
func TestAnalyzers(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "*.rr"))
	if len(files) == 0 {
		t.Fatal("no test files")
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			fset := source.NewFileSet()
			m, diags, err := module.LoadFS(fset, fstest.MapFS{filepath.Base(path): {Data: src}}, "testdata")
			if err != nil {
				t.Fatal(err)
			}
			p, _, err := m.Program("")
			if err != nil {
				t.Fatal(err)
			}
			info, d := sema.Check(p)
			diags = append(diags, d...)
			diags = append(diags, Run(p, info, All)...)
			wanttest.Check(t, fset, m.Base[0].Src, diags)
		})
	}
}

// TestExamples: the examples pass all checks without errors or warnings.
func TestExamples(t *testing.T) {
	dirs, err := module.Find(rfcblocks.Examples())
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		fset := source.NewFileSet()
		m, diags, err := module.Load(fset, dir)
		if err != nil {
			t.Fatal(err)
		}
		exts := []string{""}
		for _, tn := range m.Extensions {
			exts = append(exts, tn.Name)
		}
		for _, tn := range exts {
			p, d, err := m.Program(tn)
			if err != nil {
				t.Fatal(err)
			}
			diags = append(diags, d...)
			info, d := sema.Check(p)
			diags = append(diags, d...)
			diags = append(diags, Run(p, info, All)...)
		}
		if len(diags) > 0 {
			var b strings.Builder
			_ = diag.WriteText(&b, fset, diags)
			t.Errorf("%s:\n%s", dir, b.String())
		}
	}
}

// FuzzAnalyzers: model checks do not crash on any syntactically valid file.
func FuzzAnalyzers(f *testing.F) {
	for _, dir := range []string{"testdata", filepath.Join("..", "sema", "testdata"), filepath.Join(rfcblocks.Examples(), "taxi"), filepath.Join(rfcblocks.Examples(), "bank")} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.rr"))
		for _, path := range files {
			src, err := os.ReadFile(path)
			if err != nil {
				f.Fatal(err)
			}
			f.Add(src)
		}
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		fset := source.NewFileSet()
		m, diags, err := module.LoadFS(fset, fstest.MapFS{"m.rr": {Data: src}}, "fuzz")
		if err != nil || diag.HasErrors(diags) {
			return
		}
		p, _, err := m.Program("")
		if err != nil {
			t.Fatal(err)
		}
		info, _ := sema.Check(p)
		Run(p, info, All)
	})
}
