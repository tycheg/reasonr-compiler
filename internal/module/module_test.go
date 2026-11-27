package module

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/golden"
	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/wanttest"
)

// load loads a module and builds the base model and the models of all extensions.
func load(t *testing.T, dir string) (*source.FileSet, *Module, []*Program, []*diag.Diagnostic) {
	t.Helper()
	fset := source.NewFileSet()
	m, diags, err := Load(fset, dir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{""}
	for _, tn := range m.Extensions {
		names = append(names, tn.Name)
	}
	var progs []*Program
	for _, name := range names {
		p, d, err := m.Program(name)
		if err != nil {
			t.Fatal(err)
		}
		progs = append(progs, p)
		diags = append(diags, d...)
	}
	return fset, m, progs, diags
}

// TestModules: every testdata/<case> directory is a module. Diagnostics are
// checked against # want expectations, the model summary against testdata/<case>.golden.
func TestModules(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			dir := filepath.Join("testdata", e.Name())
			fset, m, progs, diags := load(t, dir)
			for _, f := range allFiles(m) {
				wanttest.Check(t, fset, f.Src, diags)
			}
			var b strings.Builder
			for _, p := range progs {
				b.WriteString(summary(p))
			}
			golden.Check(t, dir+".golden", []byte(b.String()))
		})
	}
}

// TestExamples: the examples build without errors for the base and every extension.
func TestExamples(t *testing.T) {
	dirs, err := Find(rfcblocks.Examples())
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) < 2 {
		t.Fatalf("modules found: %v", dirs)
	}
	for _, dir := range dirs {
		fset, _, progs, diags := load(t, dir)
		if len(diags) > 0 {
			var b strings.Builder
			_ = diag.WriteText(&b, fset, diags)
			t.Errorf("%s:\n%s", dir, b.String())
		}
		if filepath.Base(dir) == "taxi" {
			golden.Check(t, filepath.Join("testdata", "taxi.golden"), []byte(summary(progs[len(progs)-1])))
		}
	}
}

func TestUnknownExtension(t *testing.T) {
	_, m, _, _ := load(t, filepath.Join("testdata", "ok"))
	if _, _, err := m.Program("nope"); err == nil {
		t.Error("no error for a missing extension")
	}
}

// TestBaseUnchanged: building a model with an extension does not change the base trees.
func TestBaseUnchanged(t *testing.T) {
	fset := source.NewFileSet()
	m, _, err := Load(fset, filepath.Join("testdata", "ok"))
	if err != nil {
		t.Fatal(err)
	}
	before := ast.Dump(m.Base[0].AST)
	if _, _, err := m.Program("acme"); err != nil {
		t.Fatal(err)
	}
	if after := ast.Dump(m.Base[0].AST); after != before {
		t.Error("building a model with an extension changed the base tree")
	}
}

func allFiles(m *Module) []*File {
	files := append([]*File(nil), m.Base...)
	for _, tn := range m.Extensions {
		files = append(files, tn.Files...)
	}
	return files
}

// summary prints the declarations and members of a model; "+" marks the extension's.
func summary(p *Program) string {
	var b strings.Builder
	name := p.Extension
	if name == "" {
		name = "(base)"
	}
	fmt.Fprintf(&b, "== %s\n", name)
	mark := func(n ast.Node) string {
		if p.FromExtension(n) {
			return "+ "
		}
		return "  "
	}
	for _, d := range p.Decls {
		fmt.Fprintf(&b, "%s%s\n", mark(d), declTitle(d))
		var items []ast.Node
		switch d := d.(type) {
		case *ast.EntityDecl:
			for _, m := range d.Members {
				items = append(items, m)
			}
		case *ast.ProjectionDecl:
			for _, m := range d.Members {
				items = append(items, m)
			}
		}
		for _, it := range items {
			fmt.Fprintf(&b, "  %s%s\n", mark(it), itemTitle(it))
		}
	}
	for _, x := range p.Excluded {
		fmt.Fprintf(&b, "exclude %s\n", x.Test.Raw)
	}
	return b.String()
}

func declTitle(d ast.Decl) string {
	kind := strings.TrimPrefix(strings.TrimSuffix(fmt.Sprintf("%T", d), "Decl"), "*ast.")
	if t, ok := d.(*ast.TestDecl); ok {
		return kind + " " + t.Name.Raw
	}
	if n := declName(d); n != nil {
		return kind + " " + n.Name
	}
	return kind
}

func itemTitle(n ast.Node) string {
	kind := strings.TrimPrefix(strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%T", n), "Decl"), "Item"), "*ast.")
	switch n := n.(type) {
	case *ast.OnDecl:
		return kind + " " + n.Event.Name
	case *ast.RuleDecl:
		// A rule body shows whose it is: the base's or the extension's.
		return fmt.Sprintf("%s %s (%d instr.)", kind, n.Name.Name, len(n.Body.List))
	case ast.Member:
		if name := memberName(n); name != "" {
			return kind + " " + name
		}
	}
	return kind
}

// TestCache: unchanged files come from the cache, changed ones are parsed
// again, deleted ones leave the cache; parse diagnostics are kept.
func TestCache(t *testing.T) {
	fsys := fstest.MapFS{
		"a.rr":                    {Data: []byte("aggregate A\n  id uuid\n")},
		"b.rr":                    {Data: []byte("aggregate B\n  id uuid\n  x int =\n")},
		"extensions/acme/a.rr":    {Data: []byte("extend A\n  y int\n")},
		"extensions/acme/note.rr": {Data: []byte("")},
	}
	fset := source.NewFileSet()
	cache := NewCache()
	m1, d1, err := LoadCached(fset, fsys, "m", cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(d1) == 0 {
		t.Fatal("no syntax error in b.rr")
	}
	fsys["b.rr"] = &fstest.MapFile{Data: []byte("aggregate B\n  id uuid\n")}
	delete(fsys, "extensions/acme/note.rr")
	m2, d2, err := LoadCached(fset, fsys, "m", cache)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Base[0] != m1.Base[0] || m2.Extensions[0].Files[0] != m1.Extensions[0].Files[0] {
		t.Error("unchanged files were parsed again")
	}
	if m2.Base[1] == m1.Base[1] || len(d2) != 0 {
		t.Errorf("a changed file came from the cache; diagnostics %v", d2)
	}
	if len(cache.files) != 3 {
		t.Errorf("%d files in the cache, expected 3", len(cache.files))
	}
	fsys["b.rr"] = &fstest.MapFile{Data: []byte("aggregate B\n  id uuid\n  x int =\n")}
	if _, d3, _ := LoadCached(fset, fsys, "m", cache); len(d3) != len(d1) {
		t.Errorf("diagnostics after the text came back: %d, expected %d", len(d3), len(d1))
	}
	if _, d4, _ := LoadCached(fset, fsys, "m", cache); len(d4) != len(d1) {
		t.Errorf("diagnostics from the cache: %d, expected %d", len(d4), len(d1))
	}
}

func TestFileModule(t *testing.T) {
	for _, c := range []struct{ file, dir, ext string }{
		{"m/order.rr", "m", ""},
		{"m/contexts/Ordering/order.rr", "m", ""},
		{"m/apps/Admin/app.rr", "m", ""},
		{"m/primitives/primitives.rr", "m", ""},
		{"m/contexts/loose.rr", "m", ""},
		{"m/extensions/acme/order.rr", "m", "acme"},
		{"m/extensions/acme/contexts/Ordering/order.rr", "m", "acme"},
		{"m/extensions/acme/apps/Admin/app.rr", "m", "acme"},
	} {
		dir, ext := FileModule(filepath.FromSlash(c.file))
		if dir != filepath.FromSlash(c.dir) || ext != c.ext {
			t.Errorf("FileModule(%s) = %s, %q; expected %s, %q", c.file, dir, ext, c.dir, c.ext)
		}
	}
}
