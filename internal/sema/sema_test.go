package sema

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/golden"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/types"
	"github.com/tycheg/reasonr-compiler/internal/wanttest"
)

func render(fset *source.FileSet, diags []*diag.Diagnostic) string {
	var b strings.Builder
	_ = diag.WriteText(&b, fset, diags)
	return b.String()
}

// TestExamples: the examples pass the check without errors for the base and every extension.
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
			info, d := Check(p)
			diags = append(diags, d...)
			if filepath.Base(dir) == "taxi" && tn == "acme" {
				golden.Check(t, filepath.Join("testdata", "taxi-acme.symbols"), []byte(symbols(info)))
			}
		}
		if len(diags) > 0 {
			t.Errorf("%s:\n%s", dir, render(fset, diags))
		}
	}
}

// TestErrors: every testdata/*.rr file is a one-file module; the
// diagnostics are checked against # want expectations.
func TestErrors(t *testing.T) {
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
			fsys := fstest.MapFS{filepath.Base(path): {Data: src}}
			m, diags, err := module.LoadFS(fset, fsys, "testdata")
			if err != nil {
				t.Fatal(err)
			}
			p, _, err := m.Program("")
			if err != nil {
				t.Fatal(err)
			}
			_, d := Check(p)
			diags = append(diags, d...)
			wanttest.Check(t, fset, m.Base[0].Src, diags)
		})
	}
}

// TestModuleErrors: a testdata/<case> directory (except fuzz) is a module of
// several files, for example with *.fake.rr fakes; # want expectations are
// in each file.
func TestModuleErrors(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "fuzz" {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			fset := source.NewFileSet()
			m, diags, err := module.Load(fset, filepath.Join("testdata", e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			p, d, err := m.Program("")
			if err != nil {
				t.Fatal(err)
			}
			_, d2 := Check(p)
			diags = append(append(diags, d...), d2...)
			for _, f := range m.Base {
				wanttest.Check(t, fset, f.Src, diags)
			}
		})
	}
}

// symbols prints the model symbols: types, identifiers, fields, methods, access.
func symbols(info *Info) string {
	var b strings.Builder
	for _, d := range info.DeclList {
		fmt.Fprintf(&b, "%s %s", d.Kind, d.Name)
		if d.ID != nil {
			fmt.Fprintf(&b, " (id %s)", d.ID.Base)
		}
		b.WriteByte('\n')
		for _, f := range d.Fields {
			fmt.Fprintf(&b, "  %s %s", f.Name, f.Type)
			if f.Personal {
				b.WriteString(" personal")
			}
			b.WriteByte('\n')
		}
		for _, ix := range d.Indexes {
			fmt.Fprintf(&b, "  index %s\n", ix.Name)
		}
		for _, v := range d.Values {
			fmt.Fprintf(&b, "  %s\n", v.Name)
		}
		for _, m := range d.Methods {
			params := make([]string, len(m.Params))
			for i, p := range m.Params {
				params[i] = p.Name + " " + p.Type.String()
			}
			fmt.Fprintf(&b, "  %s %s(%s)", m.Kind, m.Name, strings.Join(params, ", "))
			if _, void := m.Result.(*types.Void); !void {
				fmt.Fprintf(&b, " %s", m.Result)
			}
			if acc := access(info, m.Node); acc != "" {
				fmt.Fprintf(&b, " [%s]", acc)
			}
			b.WriteByte('\n')
		}
	}
	roles := make([]string, 0, len(info.Roles))
	for r := range info.Roles {
		roles = append(roles, r)
	}
	sort.Strings(roles)
	fmt.Fprintf(&b, "roles %s\n", strings.Join(roles, ", "))
	return b.String()
}

func access(info *Info, m ast.Member) string {
	var l *ast.AccessList
	switch m := m.(type) {
	case *ast.CommandDecl:
		l = m.Access
	case *ast.QueryDecl:
		l = m.Access
	}
	if l == nil {
		return ""
	}
	var out []string
	for _, id := range l.Names {
		a := info.Access[id]
		kind := [...]string{"self", "field", "actor", "role"}[a.Kind]
		out = append(out, kind+" "+id.Name)
	}
	return strings.Join(out, ", ")
}

// FuzzCheck: checking any syntactically valid file does not crash.
func FuzzCheck(f *testing.F) {
	for _, dir := range []string{"testdata", filepath.Join(rfcblocks.Examples(), "taxi"), filepath.Join(rfcblocks.Examples(), "bank")} {
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
		Check(p)
	})
}
