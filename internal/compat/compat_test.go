package compat

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tycheg/reasonr-compiler/internal/check"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
	"github.com/tycheg/reasonr-compiler/internal/schemagen"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/wanttest"
)

// load builds a one-file module; it must have no errors.
func load(t *testing.T, fset *source.FileSet, path string) (*module.Module, *module.Program, *sema.Info) {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, diags, err := module.LoadFS(fset, fstest.MapFS{"m.rr": {Data: src}}, "m")
	if err != nil {
		t.Fatal(err)
	}
	p, d, err := m.Program("")
	if err != nil {
		t.Fatal(err)
	}
	info, d2 := sema.Check(p)
	all := append(append(append(diags, d...), d2...), check.Run(p, info, check.All)...)
	if diag.HasErrors(all) {
		t.Fatalf("%s: model error: %s", path, all[0].Message())
	}
	return m, p, info
}

// TestCompat: every testdata directory is a pair old.rr and new.rr; the
// comparison diagnostics are checked against # want expectations in new.rr.
func TestCompat(t *testing.T) {
	dirs, _ := filepath.Glob(filepath.Join("testdata", "*", "old.rr"))
	for _, old := range dirs {
		dir := filepath.Dir(old)
		t.Run(filepath.Base(dir), func(t *testing.T) {
			_, op, oinfo := load(t, source.NewFileSet(), filepath.Join(dir, "old.rr"))
			old := schemagen.Generate(op, oinfo)
			fset := source.NewFileSet()
			m, p, info := load(t, fset, filepath.Join(dir, "new.rr"))
			wanttest.Check(t, fset, m.Base[0].Src, Check(old, p, info))
		})
	}
}

// TestSame: an example schema is compatible with itself.
func TestSame(t *testing.T) {
	for _, name := range []string{"taxi", "bank"} {
		fset := source.NewFileSet()
		m, _, err := module.Load(fset, filepath.Join(rfcblocks.Examples(), name))
		if err != nil {
			t.Fatal(err)
		}
		p, _, _ := m.Program("")
		info, _ := sema.Check(p)
		if d := Check(schemagen.Generate(p, info), p, info); len(d) > 0 {
			t.Errorf("%s: %s", name, d[0].Message())
		}
	}
}

// TestFakes: the schema of a test program with fakes, written by reasonrd
// --test, is compatible with the production program without fakes: an
// external system stores no model data.
func TestFakes(t *testing.T) {
	m, _, err := module.Load(source.NewFileSet(), filepath.Join(rfcblocks.Examples(), "taxi"))
	if err != nil {
		t.Fatal(err)
	}
	tp, _, _ := m.Program("")
	tinfo, _ := sema.Check(tp)
	withFakes := schemagen.Generate(tp, tinfo)
	if ti := withFakes.Lookup("Matching"); ti == nil || len(ti.Fields) == 0 {
		t.Fatal("the test schema has no Matching fake fields")
	}
	pp, _, _ := m.Production("")
	pinfo, _ := sema.Check(pp)
	if d := Check(withFakes, pp, pinfo); len(d) > 0 {
		t.Errorf("the production program is incompatible with the test schema: %s", d[0].Message())
	}
}

// FuzzCompat: for any model without errors the schema reads back unchanged,
// the model is compatible with itself, and comparing with the example
// schemas does not crash.
func FuzzCompat(f *testing.F) {
	var bases []*model.Schema
	for _, dir := range []string{
		filepath.Join(rfcblocks.Examples(), "taxi"),
		filepath.Join(rfcblocks.Examples(), "bank"),
		filepath.Join("..", "lower", "testdata", "constructs"),
	} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.rr"))
		var src []byte
		for _, path := range files {
			data, err := os.ReadFile(path)
			if err != nil {
				f.Fatal(err)
			}
			src = append(append(src, data...), '\n')
		}
		f.Add(src)
		if p, info, ok := loadModel(src); ok {
			bases = append(bases, schemagen.Generate(p, info))
		}
	}
	for _, path := range []string{"basic", "migrated"} {
		for _, name := range []string{"old.rr", "new.rr"} {
			data, err := os.ReadFile(filepath.Join("testdata", path, name))
			if err != nil {
				f.Fatal(err)
			}
			f.Add(data)
		}
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		p, info, ok := loadModel(src)
		if !ok {
			return
		}
		s := schemagen.Generate(p, info)
		data, err := s.MarshalIndent()
		if err != nil {
			t.Fatal(err)
		}
		back, err := model.ParseSchema(data)
		if err != nil {
			t.Fatal(err)
		}
		if again, _ := back.MarshalIndent(); !bytes.Equal(again, data) {
			t.Fatal("the schema changes after a read and a write")
		}
		if d := Check(back, p, info); len(d) > 0 {
			t.Fatalf("the model is incompatible with itself: %s", d[0].Message())
		}
		for _, b := range bases {
			Check(b, p, info)
		}
	})
}

// model builds a one-file module; ok == false means the model has errors.
func loadModel(src []byte) (*module.Program, *sema.Info, bool) {
	m, diags, err := module.LoadFS(source.NewFileSet(), fstest.MapFS{"m.rr": {Data: src}}, "m")
	if err != nil || diag.HasErrors(diags) {
		return nil, nil, false
	}
	p, d, err := m.Program("")
	if err != nil || diag.HasErrors(d) {
		return nil, nil, false
	}
	info, d := sema.Check(p)
	if diag.HasErrors(d) || diag.HasErrors(check.Run(p, info, check.All)) {
		return nil, nil, false
	}
	return p, info, true
}

// TestAPI: module testdata/api is a model after releasing version
// 2026-01-01 (api/versions); API compatibility diagnostics are checked
// against # want.
func TestAPI(t *testing.T) {
	fset := source.NewFileSet()
	dir := filepath.Join("testdata", "api")
	m, diags, err := module.LoadFS(fset, os.DirFS(dir), dir)
	if err != nil {
		t.Fatal(err)
	}
	p, d, err := m.Program("")
	if err != nil {
		t.Fatal(err)
	}
	info, d2 := sema.Check(p)
	if all := append(append(diags, d...), d2...); diag.HasErrors(all) {
		t.Fatalf("model error: %s", all[0].Message())
	}
	got := API(p.API(), schemagen.Generate(p, info), info, "2026-06-01")
	for _, f := range m.Base {
		if !strings.HasSuffix(f.Name, "contract.note.rr") { // E1107 has no position
			wanttest.Check(t, fset, f.Src, got)
		}
	}
	unknown := 0
	for _, dg := range got {
		if dg.Code() == diag.E1107.Code {
			unknown++
		}
	}
	if unknown != 1 {
		t.Errorf("E1107 (Order.Missing): %d", unknown)
	}
}
